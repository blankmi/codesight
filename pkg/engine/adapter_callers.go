package engine

import (
	"context"

	"github.com/blankbytes/codesight/pkg/lsp"
)

// LSPCallersAdapter wraps lsp.CallersEngine to implement CallersProvider.
type LSPCallersAdapter struct {
	Engine *lsp.CallersEngine
}

// FindCallers delegates to the LSP callers engine's raw method.
func (a *LSPCallersAdapter) FindCallers(ctx context.Context, workspaceRoot, filterPath, symbol string, depth int) ([]SymCaller, error) {
	raw, err := a.Engine.FindCallersRaw(ctx, lsp.CallersOptions{
		WorkspaceRoot: workspaceRoot,
		FilterPath:    filterPath,
		Symbol:        symbol,
		Depth:         depth,
	})
	if err != nil {
		return nil, err
	}

	callers := make([]SymCaller, 0, len(raw))
	for _, r := range raw {
		callers = append(callers, SymCaller{
			Symbol: r.Symbol,
			File:   r.Path,
			Line:   r.Line,
			Depth:  r.Depth,
		})
	}
	return callers, nil
}
