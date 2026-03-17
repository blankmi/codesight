package vectorstore

import (
	"context"
	"time"
)

// Document represents a chunk of code stored in the vector database.
type Document struct {
	ID        string
	Content   string
	FilePath  string
	StartLine int
	EndLine   int
	Language  string
	NodeType  string
	Metadata  map[string]string
}

// SearchResult is a document paired with its similarity score.
type SearchResult struct {
	Document Document
	Score    float64
}

// IndexMetadata tracks the state of an index for a project.
type IndexMetadata struct {
	Branch            string    `json:"branch"`
	CommitSHA         string    `json:"commit_sha"`
	IgnoreFingerprint string    `json:"ignore_fingerprint"`
	IndexedAt         time.Time `json:"indexed_at"`
	FileCount         int       `json:"file_count"`
	ChunkCount        int       `json:"chunk_count"`
}

// Store is the interface for vector database backends.
type Store interface {
	Connect(ctx context.Context) error
	Close() error

	CreateCollection(ctx context.Context, name string, dimension int) error
	DropCollection(ctx context.Context, name string) error
	CollectionExists(ctx context.Context, name string) (bool, error)

	Insert(ctx context.Context, collection string, docs []Document, vectors [][]float32) error
	Search(ctx context.Context, collection string, vector []float32, limit int, filters map[string]string) ([]SearchResult, error)

	SetMetadata(ctx context.Context, collection string, meta IndexMetadata) error
	GetMetadata(ctx context.Context, collection string) (*IndexMetadata, error)
}
