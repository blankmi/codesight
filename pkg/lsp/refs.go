package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type refsClient interface {
	Call(ctx context.Context, method string, params any, result any) error
}

type refsFallback interface {
	Find(ctx context.Context, workspaceRoot string, symbol string) ([]referenceLine, error)
}

type RefsOptions struct {
	WorkspaceRoot string
	Symbol        string
	Kind          string
	FallbackLSP   string
}

type RefsEngine struct {
	client   refsClient
	fallback refsFallback
}

type lspUnavailableError struct {
	cause error
}

type resolvedSymbol struct {
	info SymbolInformation
	path string
	line int
	kind string
}

var (
	errSymbolNotFound                    = errors.New("symbol not found")
	defaultFallbackSearcher refsFallback = grepFallbackSearcher{}
	allowedRefKinds                      = map[string]struct{}{
		"function":  {},
		"method":    {},
		"class":     {},
		"interface": {},
		"type":      {},
		"constant":  {},
	}
	refKindToSymbolKinds = map[string]map[SymbolKind]struct{}{
		"function": {
			SymbolKindFunction: {},
		},
		"method": {
			SymbolKindMethod: {},
		},
		"class": {
			SymbolKindClass: {},
		},
		"interface": {
			SymbolKindInterface: {},
		},
		"type": {
			SymbolKindClass:         {},
			SymbolKindInterface:     {},
			SymbolKindEnum:          {},
			SymbolKindStruct:        {},
			SymbolKindTypeParameter: {},
		},
		"constant": {
			SymbolKindConstant: {},
		},
	}
	skippedFallbackDirs = map[string]struct{}{
		".git":         {},
		".idea":        {},
		".vscode":      {},
		"node_modules": {},
		"vendor":       {},
		"__pycache__":  {},
	}
)

type grepFallbackSearcher struct{}

func (e *lspUnavailableError) Error() string {
	return e.cause.Error()
}

func (e *lspUnavailableError) Unwrap() error {
	return e.cause
}

func NewRefsEngine(client refsClient, fallback refsFallback) *RefsEngine {
	if fallback == nil {
		fallback = defaultFallbackSearcher
	}

	return &RefsEngine{
		client:   client,
		fallback: fallback,
	}
}

func NormalizeRefKind(kind string) (string, error) {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return "", nil
	}

	normalized := strings.ToLower(trimmed)
	if _, ok := allowedRefKinds[normalized]; !ok {
		return "", fmt.Errorf(
			`invalid kind %q — allowed: %s`,
			trimmed,
			allowedRefKindsCSV,
		)
	}

	return normalized, nil
}

func (e *RefsEngine) Find(ctx context.Context, opts RefsOptions) (string, error) {
	symbol := strings.TrimSpace(opts.Symbol)
	if symbol == "" {
		return "", errors.New("symbol is required")
	}

	workspaceRoot, err := resolveWorkspaceRoot(opts.WorkspaceRoot)
	if err != nil {
		return "", err
	}

	kind, err := NormalizeRefKind(opts.Kind)
	if err != nil {
		return "", err
	}

	if e.client != nil {
		output, err := e.findWithLSP(ctx, workspaceRoot, symbol, kind)
		if err == nil {
			return output, nil
		}

		var unavailable *lspUnavailableError
		if !errors.As(err, &unavailable) {
			return "", err
		}
	}

	fallbackMatches, err := e.fallback.Find(ctx, workspaceRoot, symbol)
	if err != nil {
		return "", fmt.Errorf("grep fallback failed: %w", err)
	}
	if len(fallbackMatches) == 0 {
		return "", fmt.Errorf(`symbol %q not found`, symbol)
	}

	return formatReferencesOutput(fallbackMatches, fallbackPrecisionNote(opts.FallbackLSP)), nil
}

func (e *RefsEngine) findWithLSP(
	ctx context.Context,
	workspaceRoot string,
	symbol string,
	kind string,
) (string, error) {
	symbols, err := e.lookupSymbols(ctx, symbol)
	if err != nil {
		return "", &lspUnavailableError{
			cause: fmt.Errorf("workspace/symbol request failed: %w", err),
		}
	}

	candidates, err := resolveCandidates(symbols, workspaceRoot, symbol, kind)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf(`%w: %q`, errSymbolNotFound, symbol)
	}
	if len(candidates) > 1 {
		formatted := make([]symbolCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			formatted = append(formatted, symbolCandidate{
				Path: candidate.path,
				Line: candidate.line,
				Kind: candidate.kind,
			})
		}
		return "", formatAmbiguousSymbolError(symbol, formatted)
	}

	references, err := e.lookupReferences(ctx, candidates[0].info)
	if err != nil {
		return "", &lspUnavailableError{
			cause: fmt.Errorf("textDocument/references request failed: %w", err),
		}
	}

	refs, err := toReferenceLines(workspaceRoot, references)
	if err != nil {
		return "", err
	}

	return formatReferencesOutput(refs, ""), nil
}

func (e *RefsEngine) lookupSymbols(ctx context.Context, symbol string) ([]SymbolInformation, error) {
	var symbols []SymbolInformation
	if err := e.client.Call(
		ctx,
		MethodWorkspaceSymbol,
		WorkspaceSymbolParams{Query: symbol},
		&symbols,
	); err != nil {
		return nil, err
	}
	return symbols, nil
}

func (e *RefsEngine) lookupReferences(ctx context.Context, symbol SymbolInformation) ([]Location, error) {
	params := ReferenceParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: symbol.Location.URI},
			Position:     symbol.Location.Range.Start,
		},
		Context: ReferenceContext{IncludeDeclaration: false},
	}

	var references []Location
	if err := e.client.Call(ctx, MethodTextDocumentReferences, params, &references); err != nil {
		return nil, err
	}
	return references, nil
}

func resolveCandidates(
	symbols []SymbolInformation,
	workspaceRoot string,
	symbol string,
	kind string,
) ([]resolvedSymbol, error) {
	candidates := make([]resolvedSymbol, 0, len(symbols))

	for _, match := range symbols {
		if match.Name != symbol {
			continue
		}
		if kind != "" && !kindMatches(kind, match.Kind) {
			continue
		}

		path, err := documentURIToPath(match.Location.URI)
		if err != nil {
			return nil, err
		}

		candidates = append(candidates, resolvedSymbol{
			info: match,
			path: relativePath(workspaceRoot, path),
			line: match.Location.Range.Start.Line + 1,
			kind: symbolKindLabel(match.Kind),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].path != candidates[j].path {
			return candidates[i].path < candidates[j].path
		}
		if candidates[i].line != candidates[j].line {
			return candidates[i].line < candidates[j].line
		}
		return candidates[i].kind < candidates[j].kind
	})

	return candidates, nil
}

func kindMatches(kind string, symbolKind SymbolKind) bool {
	allowedKinds, ok := refKindToSymbolKinds[kind]
	if !ok {
		return false
	}

	_, ok = allowedKinds[symbolKind]
	return ok
}

func symbolKindLabel(kind SymbolKind) string {
	switch kind {
	case SymbolKindFunction:
		return "function"
	case SymbolKindMethod:
		return "method"
	case SymbolKindClass:
		return "class"
	case SymbolKindInterface:
		return "interface"
	case SymbolKindConstant:
		return "constant"
	default:
		return "type"
	}
}

func toReferenceLines(workspaceRoot string, references []Location) ([]referenceLine, error) {
	if len(references) == 0 {
		return nil, nil
	}

	fileCache := make(map[string][]string)
	lines := make([]referenceLine, 0, len(references))
	for _, location := range references {
		path, err := documentURIToPath(location.URI)
		if err != nil {
			return nil, err
		}

		lineNumber := location.Range.Start.Line + 1
		lines = append(lines, referenceLine{
			Path:    relativePath(workspaceRoot, path),
			Line:    lineNumber,
			Snippet: readSnippet(path, lineNumber, fileCache),
		})
	}

	return dedupeAndSortReferences(lines), nil
}

func readSnippet(path string, lineNumber int, fileCache map[string][]string) string {
	lines, ok := fileCache[path]
	if !ok {
		content, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		lines = strings.Split(string(content), "\n")
		fileCache[path] = lines
	}
	if lineNumber <= 0 || lineNumber > len(lines) {
		return ""
	}
	return lines[lineNumber-1]
}

func resolveWorkspaceRoot(root string) (string, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		trimmed = "."
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	return filepath.Clean(abs), nil
}

func documentURIToPath(uri DocumentURI) (string, error) {
	raw := strings.TrimSpace(string(uri))
	if raw == "" {
		return "", errors.New("document URI is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse document URI %q: %w", raw, err)
	}

	if parsed.Scheme == "" {
		return filepath.Clean(raw), nil
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported document URI scheme %q", parsed.Scheme)
	}

	decoded, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", fmt.Errorf("decode document URI %q: %w", raw, err)
	}

	if decoded == "" {
		return "", fmt.Errorf("document URI %q resolved to empty path", raw)
	}

	return filepath.Clean(decoded), nil
}

func relativePath(workspaceRoot string, path string) string {
	rel, err := filepath.Rel(workspaceRoot, path)
	if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)+"..") {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

func fallbackPrecisionNote(binary string) string {
	trimmed := strings.TrimSpace(binary)
	if trimmed == "" {
		trimmed = "lsp"
	}
	return fmt.Sprintf("(grep-based - install %s for precise results)", trimmed)
}

func (grepFallbackSearcher) Find(
	ctx context.Context,
	workspaceRoot string,
	symbol string,
) ([]referenceLine, error) {
	trimmedSymbol := strings.TrimSpace(symbol)
	if trimmedSymbol == "" {
		return nil, nil
	}

	matches := make([]referenceLine, 0, 32)
	err := filepath.WalkDir(workspaceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if entry.IsDir() {
			if shouldSkipFallbackDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(content, 0) >= 0 {
			return nil
		}

		relative := relativePath(workspaceRoot, path)
		lines := strings.Split(string(content), "\n")
		for index, line := range lines {
			if strings.Contains(line, trimmedSymbol) {
				matches = append(matches, referenceLine{
					Path:    relative,
					Line:    index + 1,
					Snippet: line,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return dedupeAndSortReferences(matches), nil
}

func shouldSkipFallbackDir(name string) bool {
	_, skip := skippedFallbackDirs[name]
	return skip
}
