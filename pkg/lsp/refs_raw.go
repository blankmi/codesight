package lsp

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RawReference is a structured reference result for use by the engine.
type RawReference struct {
	Path    string
	Line    int
	Snippet string
}

// FindReferencesRaw returns structured reference data and the source ("lsp" or "grep").
// It shares the same lookup pipeline as Find but skips string formatting.
func (e *RefsEngine) FindReferencesRaw(ctx context.Context, opts RefsOptions) ([]RawReference, string, error) {
	symbol := strings.TrimSpace(opts.Symbol)
	if symbol == "" {
		return nil, "", errors.New("symbol is required")
	}

	workspaceRoot, err := resolveWorkspaceRoot(opts.WorkspaceRoot)
	if err != nil {
		return nil, "", err
	}

	kind, err := NormalizeRefKind(opts.Kind)
	if err != nil {
		return nil, "", err
	}

	if e.client != nil {
		refs, err := e.findRefsRawLSP(ctx, workspaceRoot, opts.FilterPath, symbol, kind)
		if err == nil {
			return refs, "lsp", nil
		}

		var unavailable *lspUnavailableError
		if !errors.As(err, &unavailable) && !errors.Is(err, errSymbolNotFound) {
			return nil, "", err
		}
	}

	fallbackMatches, err := e.fallback.Find(ctx, workspaceRoot, symbol)
	if err != nil {
		return nil, "", fmt.Errorf("grep fallback failed: %w", err)
	}

	raw := make([]RawReference, 0, len(fallbackMatches))
	for _, m := range fallbackMatches {
		raw = append(raw, RawReference{Path: m.Path, Line: m.Line, Snippet: m.Snippet})
	}
	return raw, "grep", nil
}

func (e *RefsEngine) findRefsRawLSP(
	ctx context.Context,
	workspaceRoot string,
	filterPath string,
	symbol string,
	kind string,
) ([]RawReference, error) {
	matcher, err := newWorkspaceIgnoreMatcher(workspaceRoot)
	if err != nil {
		return nil, err
	}

	symbols, err := e.lookupSymbols(ctx, symbol)
	if err != nil {
		return nil, &lspUnavailableError{
			cause: fmt.Errorf("workspace/symbol request failed: %w", err),
		}
	}
	if len(symbols) == 0 {
		return nil, &lspUnavailableError{
			cause: fmt.Errorf("LSP returned no symbols for %q", symbol),
		}
	}

	candidates, err := resolveCandidates(symbols, workspaceRoot, filterPath, matcher, symbol, kind)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf(`%w: %q`, errSymbolNotFound, symbol)
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
		return nil, formatAmbiguousSymbolError(symbol, formatted)
	}

	references, err := e.lookupReferences(ctx, candidates[0].info)
	if err != nil {
		return nil, &lspUnavailableError{
			cause: fmt.Errorf("textDocument/references request failed: %w", err),
		}
	}

	refLines, err := toReferenceLines(workspaceRoot, matcher, references)
	if err != nil {
		return nil, err
	}

	raw := make([]RawReference, 0, len(refLines))
	for _, r := range refLines {
		raw = append(raw, RawReference{Path: r.Path, Line: r.Line, Snippet: r.Snippet})
	}
	return raw, nil
}
