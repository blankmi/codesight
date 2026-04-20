package engine

import (
	"context"
	"fmt"

	pkg "codesight/pkg"
)

// SemanticSearchAdapter wraps pkg.Searcher to implement SearchProvider.
type SemanticSearchAdapter struct {
	Searcher       *pkg.Searcher
	CollectionName string
}

// Search performs a semantic search and converts results to SymReference.
func (a *SemanticSearchAdapter) Search(ctx context.Context, workspaceRoot, query string, limit int) ([]SymReference, error) {
	output, err := a.Searcher.Search(ctx, pkg.SearchOptions{
		Path:           workspaceRoot,
		CollectionName: a.CollectionName,
		Query:          query,
		Limit:          limit,
	})
	if err != nil {
		return nil, err
	}

	refs := make([]SymReference, 0, len(output.Results))
	for _, r := range output.Results {
		refs = append(refs, SymReference{
			File:    r.FilePath,
			Line:    r.StartLine,
			Snippet: r.Description,
			Score:   r.Score,
			Reason:  fmt.Sprintf("semantic (score %.2f)", r.Score),
		})
	}
	return refs, nil
}
