package lsp

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RawCaller is a structured caller result for use by the engine.
type RawCaller struct {
	Symbol string
	Path   string
	Line   int
	Depth  int
}

// FindCallersRaw returns structured caller data.
// It shares the same traversal pipeline as Find but skips string formatting.
func (e *CallersEngine) FindCallersRaw(ctx context.Context, opts CallersOptions) ([]RawCaller, error) {
	symbol := strings.TrimSpace(opts.Symbol)
	if symbol == "" {
		return nil, errors.New("symbol is required")
	}
	if opts.Depth <= 0 {
		return nil, errors.New("depth must be a positive integer")
	}

	workspaceRoot, err := resolveWorkspaceRoot(opts.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	if e.client == nil {
		return nil, fmt.Errorf("LSP client required for callers")
	}

	matcher, err := newWorkspaceIgnoreMatcher(workspaceRoot)
	if err != nil {
		return nil, err
	}

	symbols, err := e.lookupSymbols(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("workspace/symbol request failed: %w", err)
	}
	if len(symbols) == 0 {
		return nil, fmt.Errorf("%w: %q", errLSPNoSymbols, symbol)
	}

	candidates, err := resolveCallersCandidates(symbols, workspaceRoot, opts.FilterPath, matcher, symbol)
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

	rootItem, ok, err := e.prepareCallHierarchy(ctx, candidates[0].info)
	if err != nil {
		return nil, fmt.Errorf("textDocument/prepareCallHierarchy request failed: %w", err)
	}
	if !ok {
		return nil, nil
	}

	rootNode, err := hierarchyNodeForItem(workspaceRoot, matcher, rootItem)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{rootNode.identity: {}}
	ancestors := map[string]struct{}{rootNode.identity: {}}
	var outputLines []callerOutputLine

	_, err = e.walkIncoming(ctx, workspaceRoot, matcher, rootItem, opts.Depth, 1, ancestors, seen, &outputLines)
	if err != nil {
		return nil, err
	}

	raw := make([]RawCaller, 0, len(outputLines))
	for _, line := range outputLines {
		raw = append(raw, RawCaller{
			Symbol: line.name,
			Path:   line.path,
			Line:   line.line,
			Depth:  line.depth,
		})
	}
	return raw, nil
}
