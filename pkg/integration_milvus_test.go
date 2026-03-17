//go:build integration

package pkg

import (
	"context"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blankbytes/codesight/pkg/splitter"
	"github.com/blankbytes/codesight/pkg/vectorstore"
)

const integrationDBAddressEnv = "CODESIGHT_INTEGRATION_DB_ADDRESS"

func TestIndexerAndSearcher_MilvusIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv(integrationDBAddressEnv))
	if address == "" {
		t.Skipf("%s is not set; run scripts/test-integration.sh", integrationDBAddressEnv)
	}

	waitForMilvusReady(t, address)

	repoPath := writeFixtureCodebase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	embedder := &keywordEmbedder{}

	store := vectorstore.NewMilvusStore(address, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := store.Connect(ctx); err != nil {
		t.Fatalf("connect store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	idx := &Indexer{
		Store:    store,
		Embedder: embedder,
		Splitter: splitter.NewTreeSitterSplitter(),
		Logger:   logger,
	}
	if err := idx.Index(ctx, IndexOptions{
		Path:      repoPath,
		Branch:    "integration",
		CommitSHA: "integration-commit",
		Force:     true,
	}); err != nil {
		t.Fatalf("index codebase: %v", err)
	}

	meta, err := store.GetMetadata(ctx, CollectionName(repoPath))
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata to be present after indexing")
	}
	if meta.IgnoreFingerprint == "" {
		t.Fatal("expected ignore fingerprint to be present after indexing")
	}
	if meta.ChunkCount == 0 {
		t.Fatal("expected chunk count > 0 after indexing")
	}

	searcher := &Searcher{
		Store:    store,
		Embedder: embedder,
		Logger:   logger,
	}

	assertQueryContainsFile(t, ctx, searcher, repoPath,
		"authentication middleware bearer token check",
		"auth.go",
	)
	assertQueryContainsFile(t, ctx, searcher, repoPath,
		"open postgres database connection",
		"db.go",
	)
}

func waitForMilvusReady(t *testing.T, address string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Minute)
	var lastErr error

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		store := vectorstore.NewMilvusStore(address, "")

		connectErr := store.Connect(ctx)
		if connectErr == nil {
			_, probeErr := store.CollectionExists(ctx, "codesight_ready_probe")
			_ = store.Close()
			cancel()
			if probeErr == nil {
				return
			}
			lastErr = probeErr
		} else {
			cancel()
			lastErr = connectErr
		}

		time.Sleep(2 * time.Second)
	}

	if lastErr != nil {
		t.Fatalf("milvus did not become ready at %s: %v", address, lastErr)
	}
	t.Fatalf("milvus did not become ready at %s", address)
}

func assertQueryContainsFile(
	t *testing.T,
	ctx context.Context,
	searcher *Searcher,
	repoPath, query, wantFile string,
) {
	t.Helper()

	out, err := searcher.Search(ctx, SearchOptions{
		Path:  repoPath,
		Query: query,
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	if out == nil || len(out.Results) == 0 {
		t.Fatalf("search %q returned no results", query)
	}

	for _, result := range out.Results {
		if result.FilePath == wantFile {
			return
		}
	}

	got := make([]string, 0, len(out.Results))
	for _, result := range out.Results {
		got = append(got, result.FilePath)
	}
	t.Fatalf("search %q did not return %s; got %v", query, wantFile, got)
}

func writeFixtureCodebase(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFixtureFile(t, filepath.Join(dir, "auth.go"), `package fixture

import "strings"

func AuthMiddleware(header string) bool {
	return strings.HasPrefix(header, "Bearer ")
}
`)
	writeFixtureFile(t, filepath.Join(dir, "db.go"), `package fixture

func OpenDatabaseConnection() string {
	return "postgres://localhost:5432/fixture"
}
`)
	writeFixtureFile(t, filepath.Join(dir, "cache.go"), `package fixture

func ReadCache(key string) string {
	return "cache:" + key
}
`)

	return dir
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type keywordEmbedder struct{}

func (k *keywordEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	_ = ctx
	return keywordVector(text), nil
}

func (k *keywordEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx

	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = keywordVector(text)
	}
	return out, nil
}

func (k *keywordEmbedder) Dimension() int {
	return 6
}

func (k *keywordEmbedder) Name() string {
	return "test/keyword"
}

func keywordVector(text string) []float32 {
	lower := strings.ToLower(text)
	vec := make([]float32, 6)
	vec[5] = 0.01 // keeps cosine distance stable for chunks without matched terms

	addKeywordWeights(vec, lower, 0, "auth", "authentication", "middleware", "token", "bearer", "login")
	addKeywordWeights(vec, lower, 1, "database", "db", "query", "sql", "postgres", "connection")
	addKeywordWeights(vec, lower, 2, "cache", "redis", "memo")
	addKeywordWeights(vec, lower, 3, "http", "request", "response", "handler")
	addKeywordWeights(vec, lower, 4, "error", "panic", "recover")

	return normalize(vec)
}

func addKeywordWeights(vec []float32, text string, dim int, keywords ...string) {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			vec[dim] += 1
		}
	}
}

func normalize(vec []float32) []float32 {
	var sumSquares float64
	for _, v := range vec {
		sumSquares += float64(v * v)
	}
	if sumSquares == 0 {
		return vec
	}

	norm := float32(math.Sqrt(sumSquares))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}
