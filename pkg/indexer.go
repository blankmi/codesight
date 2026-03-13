package pkg

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/blankbytes/codesight/pkg/embedding"
	"github.com/blankbytes/codesight/pkg/splitter"
	"github.com/blankbytes/codesight/pkg/vectorstore"
)

const embeddingBatchSize = 64

// IndexOptions configures the indexing pipeline.
type IndexOptions struct {
	Path      string
	Branch    string
	CommitSHA string
	Force     bool
	Walk      *WalkOptions
}

// Indexer orchestrates the code indexing pipeline.
type Indexer struct {
	Store    vectorstore.Store
	Embedder embedding.Provider
	Splitter splitter.Splitter
	Logger   *slog.Logger
}

// Index processes files at the given path, chunks them, embeds them, and stores them.
func (idx *Indexer) Index(ctx context.Context, opts IndexOptions) error {
	projectPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	collection := CollectionName(projectPath)

	if !opts.Force {
		exists, err := idx.Store.CollectionExists(ctx, collection)
		if err != nil {
			return fmt.Errorf("checking collection: %w", err)
		}
		if exists {
			meta, err := idx.Store.GetMetadata(ctx, collection)
			if err != nil {
				return fmt.Errorf("getting metadata: %w", err)
			}
			if meta != nil && !IsStale(meta, opts.CommitSHA) {
				idx.Logger.Info("index is up to date, skipping", "collection", collection, "commit", opts.CommitSHA)
				return nil
			}
		}
	}

	// Detect embedding dimension
	dimension := idx.Embedder.Dimension()
	if dimension == 0 {
		if detector, ok := idx.Embedder.(interface {
			DetectDimension(ctx context.Context) (int, error)
		}); ok {
			dimension, err = detector.DetectDimension(ctx)
			if err != nil {
				return fmt.Errorf("detecting embedding dimension: %w", err)
			}
		}
		if dimension == 0 {
			return fmt.Errorf("unable to determine embedding dimension")
		}
	}

	// Drop and recreate collection (clean slate)
	exists, err := idx.Store.CollectionExists(ctx, collection)
	if err != nil {
		return fmt.Errorf("checking collection before recreate: %w", err)
	}
	if exists {
		idx.Logger.Info("dropping existing collection", "collection", collection)
		if err := idx.Store.DropCollection(ctx, collection); err != nil {
			return fmt.Errorf("dropping collection: %w", err)
		}
	}

	idx.Logger.Info("creating collection", "collection", collection, "dimension", dimension)
	if err := idx.Store.CreateCollection(ctx, collection, dimension); err != nil {
		return fmt.Errorf("creating collection: %w", err)
	}

	// Walk files
	files, err := WalkFiles(projectPath, opts.Walk)
	if err != nil {
		return fmt.Errorf("walking files: %w", err)
	}
	idx.Logger.Info("found files to index", "count", len(files))

	// Chunk all files
	var allChunks []splitter.Chunk
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			idx.Logger.Warn("skipping unreadable file", "path", file, "error", err)
			continue
		}

		rel, _ := filepath.Rel(projectPath, file)
		ext := filepath.Ext(file)
		lang := splitter.LanguageFromExtension(ext)

		chunks, err := idx.Splitter.Split(string(data), lang, rel)
		if err != nil {
			idx.Logger.Warn("skipping file with split error", "path", rel, "error", err)
			continue
		}
		allChunks = append(allChunks, chunks...)
	}
	idx.Logger.Info("total chunks", "count", len(allChunks))

	// Embed and insert in batches
	for i := 0; i < len(allChunks); i += embeddingBatchSize {
		end := i + embeddingBatchSize
		if end > len(allChunks) {
			end = len(allChunks)
		}
		batch := allChunks[i:end]

		texts := make([]string, len(batch))
		for j, chunk := range batch {
			texts[j] = chunk.Content
		}

		vectors, err := idx.Embedder.EmbedBatch(ctx, texts)
		if err != nil {
			// Find the largest chunk to help diagnose context-length errors.
			maxLen, maxIdx := 0, 0
			for j, t := range texts {
				if len(t) > maxLen {
					maxLen = len(t)
					maxIdx = j
				}
			}
			c := batch[maxIdx]
			idx.Logger.Info("embedding failed",
				"batch", fmt.Sprintf("%d-%d", i, end),
				"largest_chunk_chars", maxLen,
				"file", c.FilePath,
				"lines", fmt.Sprintf("%d-%d", c.StartLine, c.EndLine),
			)
			return fmt.Errorf("embedding batch %d-%d: %w", i, end, err)
		}

		docs := make([]vectorstore.Document, len(batch))
		for j, chunk := range batch {
			docs[j] = vectorstore.Document{
				Content:   chunk.Content,
				FilePath:  chunk.FilePath,
				StartLine: chunk.StartLine,
				EndLine:   chunk.EndLine,
				Language:  chunk.Language,
				NodeType:  chunk.NodeType,
			}
		}

		if err := idx.Store.Insert(ctx, collection, docs, vectors); err != nil {
			return fmt.Errorf("inserting batch %d-%d: %w", i, end, err)
		}

		idx.Logger.Info("indexed batch", "progress", fmt.Sprintf("%d/%d", end, len(allChunks)))
	}

	// Save metadata
	meta := vectorstore.IndexMetadata{
		Branch:     opts.Branch,
		CommitSHA:  opts.CommitSHA,
		IndexedAt:  time.Now(),
		FileCount:  len(files),
		ChunkCount: len(allChunks),
	}
	if err := idx.Store.SetMetadata(ctx, collection, meta); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}

	idx.Logger.Info("indexing complete",
		"collection", collection,
		"files", len(files),
		"chunks", len(allChunks),
		"branch", opts.Branch,
		"commit", opts.CommitSHA,
	)

	return nil
}
