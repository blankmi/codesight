package lsp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	methodTextDocumentPrepareTypeHierarchy = "textDocument/prepareTypeHierarchy"
	methodTypeHierarchySubtypes            = "typeHierarchy/subtypes"
)

type implementsClient interface {
	Call(ctx context.Context, method string, params any, result any) error
}

// ImplementsOptions configures one implementations lookup.
type ImplementsOptions struct {
	WorkspaceRoot string
	Symbol        string
	LSPBinary     string
	LSPInstall    string
}

// ImplementsEngine resolves type/interface implementations via LSP type hierarchy APIs.
type ImplementsEngine struct {
	client implementsClient
}

type typeHierarchyPrepareParams struct {
	TextDocumentPositionParams
}

type typeHierarchyItem struct {
	Name           string      `json:"name"`
	Kind           SymbolKind  `json:"kind"`
	URI            DocumentURI `json:"uri"`
	Range          Range       `json:"range"`
	SelectionRange Range       `json:"selectionRange"`
	Detail         string      `json:"detail,omitempty"`
}

type typeHierarchySubtypesParams struct {
	Item typeHierarchyItem `json:"item"`
}

type implementationLine struct {
	Name string
	Path string
	Line int
}

// NewImplementsEngine creates an implementations lookup engine.
func NewImplementsEngine(client implementsClient) *ImplementsEngine {
	return &ImplementsEngine{client: client}
}

// Find resolves implementations for one symbol.
func (e *ImplementsEngine) Find(ctx context.Context, opts ImplementsOptions) (string, error) {
	symbol := strings.TrimSpace(opts.Symbol)
	if symbol == "" {
		return "", errors.New("symbol is required")
	}

	workspaceRoot, err := resolveWorkspaceRoot(opts.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	if e.client == nil {
		return "", formatMissingImplementsLSPError(opts.LSPBinary, opts.LSPInstall)
	}
	matcher, err := newWorkspaceIgnoreMatcher(workspaceRoot)
	if err != nil {
		return "", err
	}

	symbols, err := e.lookupSymbols(ctx, symbol)
	if err != nil {
		return "", fmt.Errorf("workspace/symbol request failed: %w", err)
	}

	candidates, err := resolveCandidates(symbols, workspaceRoot, matcher, symbol, "")
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

	rootItem, ok, err := e.prepareTypeHierarchy(ctx, candidates[0].info)
	if err != nil {
		return "", fmt.Errorf("textDocument/prepareTypeHierarchy request failed: %w", err)
	}
	if !ok {
		return formatImplementsOutput(nil), nil
	}

	subtypes, err := e.lookupSubtypes(ctx, rootItem)
	if err != nil {
		return "", fmt.Errorf("typeHierarchy/subtypes request failed: %w", err)
	}

	implementations, err := toImplementationLines(workspaceRoot, matcher, subtypes)
	if err != nil {
		return "", err
	}

	return formatImplementsOutput(implementations), nil
}

func (e *ImplementsEngine) lookupSymbols(ctx context.Context, symbol string) ([]SymbolInformation, error) {
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

func (e *ImplementsEngine) prepareTypeHierarchy(
	ctx context.Context,
	symbol SymbolInformation,
) (typeHierarchyItem, bool, error) {
	params := typeHierarchyPrepareParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: symbol.Location.URI},
			Position:     symbol.Location.Range.Start,
		},
	}

	var items []typeHierarchyItem
	if err := e.client.Call(ctx, methodTextDocumentPrepareTypeHierarchy, params, &items); err != nil {
		return typeHierarchyItem{}, false, err
	}
	if len(items) == 0 {
		return typeHierarchyItem{}, false, nil
	}

	sort.SliceStable(items, func(i, j int) bool {
		left := string(items[i].URI)
		right := string(items[j].URI)
		if left != right {
			return left < right
		}

		leftLine, leftChar := typeHierarchyPosition(items[i])
		rightLine, rightChar := typeHierarchyPosition(items[j])
		if leftLine != rightLine {
			return leftLine < rightLine
		}
		if leftChar != rightChar {
			return leftChar < rightChar
		}
		return items[i].Name < items[j].Name
	})

	return items[0], true, nil
}

func (e *ImplementsEngine) lookupSubtypes(
	ctx context.Context,
	item typeHierarchyItem,
) ([]typeHierarchyItem, error) {
	params := typeHierarchySubtypesParams{Item: item}

	var subtypes []typeHierarchyItem
	if err := e.client.Call(ctx, methodTypeHierarchySubtypes, params, &subtypes); err != nil {
		return nil, err
	}
	return subtypes, nil
}

func toImplementationLines(
	workspaceRoot string,
	matcher interface{ MatchesPath(string) bool },
	subtypes []typeHierarchyItem,
) ([]implementationLine, error) {
	if len(subtypes) == 0 {
		return nil, nil
	}

	implementations := make([]implementationLine, 0, len(subtypes))
	for _, subtype := range subtypes {
		path, err := documentURIToPath(subtype.URI)
		if err != nil {
			return nil, err
		}
		if matcher != nil && matcher.MatchesPath(path) {
			continue
		}

		line, _ := typeHierarchyPosition(subtype)
		implementations = append(implementations, implementationLine{
			Name: subtype.Name,
			Path: relativePath(workspaceRoot, path),
			Line: line + 1,
		})
	}

	sort.SliceStable(implementations, func(i, j int) bool {
		if implementations[i].Path != implementations[j].Path {
			return implementations[i].Path < implementations[j].Path
		}
		if implementations[i].Name != implementations[j].Name {
			return implementations[i].Name < implementations[j].Name
		}
		return implementations[i].Line < implementations[j].Line
	})

	out := make([]implementationLine, 0, len(implementations))
	seen := make(map[string]struct{}, len(implementations))
	for _, implementation := range implementations {
		key := implementation.Path + "\x00" + implementation.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, implementation)
	}

	return out, nil
}

func formatImplementsOutput(implementations []implementationLine) string {
	lines := make([]string, 0, len(implementations)+1)
	for _, implementation := range implementations {
		lines = append(lines, fmt.Sprintf("%s (%s)", implementation.Name, implementation.Path))
	}
	lines = append(lines, fmt.Sprintf("%d implementations", len(implementations)))
	return strings.Join(lines, "\n")
}

func formatMissingImplementsLSPError(binary string, install string) error {
	trimmedBinary := strings.TrimSpace(binary)
	if trimmedBinary == "" {
		trimmedBinary = "lsp"
	}

	trimmedInstall := strings.TrimSpace(install)
	if trimmedInstall == "" {
		trimmedInstall = "install language server"
	}

	return fmt.Errorf(
		"cs implements: LSP required but %s not found. Install: %s",
		trimmedBinary,
		trimmedInstall,
	)
}

func typeHierarchyPosition(item typeHierarchyItem) (int, int) {
	line := item.SelectionRange.Start.Line
	character := item.SelectionRange.Start.Character
	if line < 0 {
		line = item.Range.Start.Line
		character = item.Range.Start.Character
	}
	if line < 0 {
		line = 0
	}
	if character < 0 {
		character = 0
	}
	return line, character
}
