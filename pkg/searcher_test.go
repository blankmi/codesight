package pkg

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/blankbytes/codesight/pkg/vectorstore"
)

type stubSearchStore struct {
	results          []vectorstore.SearchResult
	lastCollection   string
	collectionExists bool
}

func (s *stubSearchStore) Connect(context.Context) error {
	return nil
}

func (s *stubSearchStore) Close() error {
	return nil
}

func (s *stubSearchStore) CreateCollection(context.Context, string, int) error {
	return nil
}

func (s *stubSearchStore) DropCollection(context.Context, string) error {
	return nil
}

func (s *stubSearchStore) CollectionExists(context.Context, string) (bool, error) {
	if !s.collectionExists {
		return false, nil
	}
	return true, nil
}

func (s *stubSearchStore) Insert(context.Context, string, []vectorstore.Document, [][]float32) error {
	return nil
}

func (s *stubSearchStore) Search(_ context.Context, collection string, _ []float32, _ int, _ map[string]string) ([]vectorstore.SearchResult, error) {
	s.lastCollection = collection
	return append([]vectorstore.SearchResult(nil), s.results...), nil
}

func (s *stubSearchStore) SetMetadata(context.Context, string, vectorstore.IndexMetadata) error {
	return nil
}

func (s *stubSearchStore) GetMetadata(context.Context, string) (*vectorstore.IndexMetadata, error) {
	return nil, nil
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1}, nil
}

func (stubEmbedder) EmbedBatch(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1}}, nil
}

func (stubEmbedder) Dimension() int {
	return 1
}

func (stubEmbedder) Name() string {
	return "stub"
}

func TestSearcherFiltersIgnoredResults(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".csignore"), "ignored.go\n")

	searcher := &Searcher{
		Store: &stubSearchStore{
			collectionExists: true,
			results: []vectorstore.SearchResult{
				{
					Document: vectorstore.Document{
						FilePath:  "ignored.go",
						StartLine: 1,
						EndLine:   3,
						Content:   "func Ignored() {}\n",
						Language:  "go",
						NodeType:  "function",
					},
					Score: 0.9,
				},
				{
					Document: vectorstore.Document{
						FilePath:  "visible.go",
						StartLine: 5,
						EndLine:   7,
						Content:   "func Visible() {}\n",
						Language:  "go",
						NodeType:  "function",
					},
					Score: 0.8,
				},
			},
		},
		Embedder: stubEmbedder{},
	}

	output, err := searcher.Search(context.Background(), SearchOptions{
		Path:  root,
		Query: "visible function",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(output.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(output.Results))
	}
	if output.Results[0].FilePath != "visible.go" {
		t.Fatalf("file path = %q, want %q", output.Results[0].FilePath, "visible.go")
	}
	if output.Results[0].Rank != 1 {
		t.Fatalf("rank = %d, want 1", output.Results[0].Rank)
	}
}

func TestSearcherUsesConfiguredCollectionName(t *testing.T) {
	root := t.TempDir()
	store := &stubSearchStore{collectionExists: true}
	searcher := &Searcher{
		Store:    store,
		Embedder: stubEmbedder{},
	}

	if _, err := searcher.Search(context.Background(), SearchOptions{
		Path:           root,
		CollectionName: "shared_collection",
		Query:          "visible function",
		Limit:          10,
	}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if store.lastCollection != "shared_collection" {
		t.Fatalf("collection = %q, want %q", store.lastCollection, "shared_collection")
	}
}

func TestSearcherUsesPathBasedCollectionNameByDefault(t *testing.T) {
	root := t.TempDir()
	store := &stubSearchStore{collectionExists: true}
	searcher := &Searcher{
		Store:    store,
		Embedder: stubEmbedder{},
	}

	if _, err := searcher.Search(context.Background(), SearchOptions{
		Path:  root,
		Query: "visible function",
		Limit: 10,
	}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	want := CollectionName(root)
	if store.lastCollection != want {
		t.Fatalf("collection = %q, want %q", store.lastCollection, want)
	}
}
