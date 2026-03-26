package lsp

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RawImplementation is a structured implementation result for use by the engine.
type RawImplementation struct {
	Name string
	Path string
	Line int
}

// FindImplementationsRaw returns structured implementation data.
// It shares the same lookup pipeline as Find but skips string formatting.
func (e *ImplementsEngine) FindImplementationsRaw(ctx context.Context, opts ImplementsOptions) ([]RawImplementation, error) {
	symbol := strings.TrimSpace(opts.Symbol)
	if symbol == "" {
		return nil, errors.New("symbol is required")
	}

	workspaceRoot, err := resolveWorkspaceRoot(opts.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	if e.client == nil {
		return nil, fmt.Errorf("LSP client required for implements")
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

	candidates, err := resolveCandidates(symbols, workspaceRoot, opts.FilterPath, matcher, symbol, "")
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

	rootItem, ok, err := e.prepareTypeHierarchy(ctx, candidates[0].info)
	if err != nil {
		return nil, fmt.Errorf("textDocument/prepareTypeHierarchy request failed: %w", err)
	}

	var implementations []implementationLine
	if ok {
		subtypes, err := e.lookupSubtypes(ctx, rootItem)
		if err != nil {
			return nil, fmt.Errorf("typeHierarchy/subtypes request failed: %w", err)
		}
		implementations, err = toImplementationLines(workspaceRoot, matcher, subtypes)
		if err != nil {
			return nil, err
		}
	}

	// Dual lookup fallback.
	if len(implementations) == 0 {
		locations, err := e.lookupImplementation(ctx, candidates[0].info)
		if err != nil {
			return nil, nil
		}
		implementations, err = locationsToImplementationLines(workspaceRoot, matcher, locations)
		if err != nil {
			return nil, err
		}
	}

	raw := make([]RawImplementation, 0, len(implementations))
	for _, impl := range implementations {
		raw = append(raw, RawImplementation(impl))
	}
	return raw, nil
}
