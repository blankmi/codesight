package engine

import (
	"context"
	"path/filepath"
	"testing"

	pkg "codesight/pkg"
	"codesight/pkg/vectorstore"
)

type recordingSearchStore struct {
	results          []vectorstore.SearchResult
	lastCollection   string
	collectionExists bool
}

func (s *recordingSearchStore) Connect(context.Context) error {
	return nil
}

func (s *recordingSearchStore) Close() error {
	return nil
}

func (s *recordingSearchStore) CreateCollection(context.Context, string, int) error {
	return nil
}

func (s *recordingSearchStore) DropCollection(context.Context, string) error {
	return nil
}

func (s *recordingSearchStore) CollectionExists(_ context.Context, collection string) (bool, error) {
	s.lastCollection = collection
	return s.collectionExists, nil
}

func (s *recordingSearchStore) Insert(context.Context, string, []vectorstore.Document, [][]float32) error {
	return nil
}

func (s *recordingSearchStore) Search(_ context.Context, collection string, _ []float32, _ int, _ map[string]string) ([]vectorstore.SearchResult, error) {
	s.lastCollection = collection
	return append([]vectorstore.SearchResult(nil), s.results...), nil
}

func (s *recordingSearchStore) SetMetadata(context.Context, string, vectorstore.IndexMetadata) error {
	return nil
}

func (s *recordingSearchStore) GetMetadata(context.Context, string) (*vectorstore.IndexMetadata, error) {
	return nil, nil
}

type recordingEmbedder struct{}

func (recordingEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1}, nil
}

func (recordingEmbedder) EmbedBatch(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1}}, nil
}

func (recordingEmbedder) Dimension() int {
	return 1
}

func (recordingEmbedder) Name() string {
	return "stub"
}

func TestSemanticSearchAdapterUsesConfiguredCollectionName(t *testing.T) {
	root := t.TempDir()
	store := &recordingSearchStore{
		collectionExists: true,
		results: []vectorstore.SearchResult{
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
	}
	adapter := &SemanticSearchAdapter{
		Searcher: &pkg.Searcher{
			Store:    store,
			Embedder: recordingEmbedder{},
		},
		CollectionName: "shared_collection",
	}

	refs, err := adapter.Search(context.Background(), root, "visible function", 10)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("result count = %d, want 1", len(refs))
	}
	if store.lastCollection != "shared_collection" {
		t.Fatalf("collection = %q, want %q", store.lastCollection, "shared_collection")
	}
}

func TestSemanticSearchAdapterUsesPathBasedCollectionNameByDefault(t *testing.T) {
	root := t.TempDir()
	store := &recordingSearchStore{
		collectionExists: true,
		results: []vectorstore.SearchResult{
			{
				Document: vectorstore.Document{
					FilePath:  filepath.Join(root, "visible.go"),
					StartLine: 5,
					EndLine:   7,
					Content:   "func Visible() {}\n",
					Language:  "go",
					NodeType:  "function",
				},
				Score: 0.8,
			},
		},
	}
	adapter := &SemanticSearchAdapter{
		Searcher: &pkg.Searcher{
			Store:    store,
			Embedder: recordingEmbedder{},
		},
	}

	if _, err := adapter.Search(context.Background(), root, "visible function", 10); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	want := pkg.CollectionName(root)
	if store.lastCollection != want {
		t.Fatalf("collection = %q, want %q", store.lastCollection, want)
	}
}
