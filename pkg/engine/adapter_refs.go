package engine

import (
	"context"

	"codesight/pkg/lsp"
)

// LSPRefsAdapter wraps lsp.RefsEngine to implement RefsProvider.
type LSPRefsAdapter struct {
	Engine      *lsp.RefsEngine
	FallbackLSP string
}

// FindRefs delegates to the LSP refs engine's raw method.
func (a *LSPRefsAdapter) FindRefs(ctx context.Context, workspaceRoot, filterPath, symbol, kind string) ([]SymReference, string, error) {
	raw, source, err := a.Engine.FindReferencesRaw(ctx, lsp.RefsOptions{
		WorkspaceRoot: workspaceRoot,
		FilterPath:    filterPath,
		Symbol:        symbol,
		Kind:          kind,
		FallbackLSP:   a.FallbackLSP,
	})
	if err != nil {
		return nil, "", err
	}

	refs := make([]SymReference, 0, len(raw))
	for _, r := range raw {
		refs = append(refs, SymReference{
			File:    r.Path,
			Line:    r.Line,
			Snippet: r.Snippet,
		})
	}
	return refs, source, nil
}
