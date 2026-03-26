package pkg

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/blankbytes/codesight/pkg/splitter"
	"github.com/blankbytes/codesight/pkg/vectorstore"
)

type recordingIndexStore struct {
	checkedCollections []string
	createdCollection  string
	insertedCollection string
	metadataCollection string
}

func (s *recordingIndexStore) Connect(context.Context) error {
	return nil
}

func (s *recordingIndexStore) Close() error {
	return nil
}

func (s *recordingIndexStore) CreateCollection(_ context.Context, name string, _ int) error {
	s.createdCollection = name
	return nil
}

func (s *recordingIndexStore) DropCollection(context.Context, string) error {
	return nil
}

func (s *recordingIndexStore) CollectionExists(_ context.Context, name string) (bool, error) {
	s.checkedCollections = append(s.checkedCollections, name)
	return false, nil
}

func (s *recordingIndexStore) Insert(_ context.Context, collection string, _ []vectorstore.Document, _ [][]float32) error {
	s.insertedCollection = collection
	return nil
}

func (s *recordingIndexStore) Search(context.Context, string, []float32, int, map[string]string) ([]vectorstore.SearchResult, error) {
	return nil, nil
}

func (s *recordingIndexStore) SetMetadata(_ context.Context, collection string, _ vectorstore.IndexMetadata) error {
	s.metadataCollection = collection
	return nil
}

func (s *recordingIndexStore) GetMetadata(context.Context, string) (*vectorstore.IndexMetadata, error) {
	return nil, nil
}

type stubIndexSplitter struct{}

func (stubIndexSplitter) Split(_ string, _ string, filePath string) ([]splitter.Chunk, error) {
	return []splitter.Chunk{
		{
			Content:   "package main\nfunc main() {}\n",
			FilePath:  filePath,
			StartLine: 1,
			EndLine:   2,
			Language:  "go",
			NodeType:  "function",
		},
	}, nil
}

func (stubIndexSplitter) SupportedLanguages() []string {
	return []string{"go"}
}

type stubIndexEmbedder struct{}

func (stubIndexEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1}, nil
}

func (stubIndexEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1}
	}
	return out, nil
}

func (stubIndexEmbedder) Dimension() int {
	return 1
}

func (stubIndexEmbedder) Name() string {
	return "stub"
}

func TestIndexerUsesConfiguredCollectionName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")

	store := &recordingIndexStore{}
	idx := &Indexer{
		Store:    store,
		Embedder: stubIndexEmbedder{},
		Splitter: stubIndexSplitter{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := idx.Index(context.Background(), IndexOptions{
		Path:           root,
		CollectionName: "shared_collection",
		Branch:         "main",
		CommitSHA:      "abc123",
		Force:          true,
	}); err != nil {
		t.Fatalf("Index returned error: %v", err)
	}

	if store.createdCollection != "shared_collection" {
		t.Fatalf("created collection = %q, want %q", store.createdCollection, "shared_collection")
	}
	if store.insertedCollection != "shared_collection" {
		t.Fatalf("inserted collection = %q, want %q", store.insertedCollection, "shared_collection")
	}
	if store.metadataCollection != "shared_collection" {
		t.Fatalf("metadata collection = %q, want %q", store.metadataCollection, "shared_collection")
	}
}

func TestIndexerUsesPathBasedCollectionNameByDefault(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")

	store := &recordingIndexStore{}
	idx := &Indexer{
		Store:    store,
		Embedder: stubIndexEmbedder{},
		Splitter: stubIndexSplitter{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := idx.Index(context.Background(), IndexOptions{
		Path:      root,
		Branch:    "main",
		CommitSHA: "abc123",
		Force:     true,
	}); err != nil {
		t.Fatalf("Index returned error: %v", err)
	}

	want := CollectionName(root)
	if store.createdCollection != want {
		t.Fatalf("created collection = %q, want %q", store.createdCollection, want)
	}
	if store.insertedCollection != want {
		t.Fatalf("inserted collection = %q, want %q", store.insertedCollection, want)
	}
	if store.metadataCollection != want {
		t.Fatalf("metadata collection = %q, want %q", store.metadataCollection, want)
	}
}
