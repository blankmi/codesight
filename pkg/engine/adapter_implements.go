package engine

import (
	"context"

	"github.com/blankbytes/codesight/pkg/lsp"
)

// LSPImplAdapter wraps lsp.ImplementsEngine to implement ImplProvider.
type LSPImplAdapter struct {
	Engine *lsp.ImplementsEngine
}

// FindImplementations delegates to the LSP implements engine's raw method.
func (a *LSPImplAdapter) FindImplementations(ctx context.Context, workspaceRoot, filterPath, symbol string) ([]SymImpl, error) {
	raw, err := a.Engine.FindImplementationsRaw(ctx, lsp.ImplementsOptions{
		WorkspaceRoot: workspaceRoot,
		FilterPath:    filterPath,
		Symbol:        symbol,
	})
	if err != nil {
		return nil, err
	}

	impls := make([]SymImpl, 0, len(raw))
	for _, r := range raw {
		impls = append(impls, SymImpl{
			Name: r.Name,
			File: r.Path,
			Line: r.Line,
		})
	}
	return impls, nil
}
