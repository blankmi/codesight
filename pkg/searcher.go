package pkg

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/blankbytes/codesight/pkg/embedding"
	"github.com/blankbytes/codesight/pkg/splitter"
	"github.com/blankbytes/codesight/pkg/vectorstore"
)

// SearchOptions configures a search query.
type SearchOptions struct {
	Path       string
	Query      string
	Limit      int
	Extensions []string // e.g., [".go", ".ts"]
}

// SearchOutput is a formatted search result for display.
type SearchOutput struct {
	Results []SearchResultEntry
}

// SearchResultEntry is a single formatted result.
type SearchResultEntry struct {
	Rank        int
	FilePath    string
	StartLine   int
	EndLine     int
	Score       float64
	Description string
	Content     string
}

// Searcher handles semantic code search.
type Searcher struct {
	Store    vectorstore.Store
	Embedder embedding.Provider
	Logger   *slog.Logger
}

// Search performs a semantic search and returns formatted results.
func (s *Searcher) Search(ctx context.Context, opts SearchOptions) (*SearchOutput, error) {
	projectPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	collection := CollectionName(projectPath)
	exists, err := s.Store.CollectionExists(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("checking collection: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("no index found for %s — run 'cs index' first", projectPath)
	}

	queryVec, err := s.Embedder.Embed(ctx, opts.Query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	filters := make(map[string]string)
	if len(opts.Extensions) > 0 {
		// Map extensions to language filter
		for _, ext := range opts.Extensions {
			lang := splitter.LanguageFromExtension(ext)
			if lang != "" {
				filters["language"] = lang
				break // Milvus supports only one filter value per field
			}
		}
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	results, err := s.Store.Search(ctx, collection, queryVec, limit, filters)
	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}

	output := &SearchOutput{}
	for i, result := range results {
		chunk := splitter.Chunk{
			Content:   result.Document.Content,
			FilePath:  result.Document.FilePath,
			StartLine: result.Document.StartLine,
			EndLine:   result.Document.EndLine,
			Language:  result.Document.Language,
			NodeType:  result.Document.NodeType,
		}

		entry := SearchResultEntry{
			Rank:        i + 1,
			FilePath:    result.Document.FilePath,
			StartLine:   result.Document.StartLine,
			EndLine:     result.Document.EndLine,
			Score:       result.Score,
			Description: splitter.DescribeChunk(chunk),
			Content:     truncateContentForDisplay(result.Document.Content, 3),
		}
		output.Results = append(output.Results, entry)
	}

	return output, nil
}

// FormatResults returns a minimal plain-text representation of search results.
func FormatResults(output *SearchOutput) string {
	if output == nil || len(output.Results) == 0 {
		return "No results found."
	}

	var b strings.Builder
	for _, r := range output.Results {
		fmt.Fprintf(&b, "[%d] %s:%d-%d (score: %.2f)\n",
			r.Rank, r.FilePath, r.StartLine, r.EndLine, r.Score)
		fmt.Fprintf(&b, "    %s\n\n", r.Description)
	}
	return strings.TrimSpace(b.String())
}

func truncateContentForDisplay(content string, maxLines int) string {
	lines := strings.SplitN(content, "\n", maxLines+1)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "...")
	}
	return strings.Join(lines, "\n")
}
