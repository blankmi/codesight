package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pkg "github.com/blankbytes/codesight/pkg"
	"github.com/blankbytes/codesight/pkg/embedding"
	"github.com/blankbytes/codesight/pkg/splitter"
	"github.com/blankbytes/codesight/pkg/vectorstore"
	"github.com/spf13/cobra"
)

const (
	interactiveNetworkTimeout = 1 * time.Second
	ollamaMaxInputCharsEnv    = "CODESIGHT_OLLAMA_MAX_INPUT_CHARS"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "cs",
	Short: "codesight — semantic code search",
}

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a codebase for semantic search",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runIndex,
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search indexed codebase",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSearch,
}

var statusCmd = &cobra.Command{
	Use:   "status [path]",
	Short: "Show index status for a codebase",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStatus,
}

var clearCmd = &cobra.Command{
	Use:   "clear [path]",
	Short: "Remove index for a codebase",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runClear,
}

var (
	flagForce   bool
	flagBranch  string
	flagCommit  string
	flagLimit   int
	flagExt     string
	flagPath    string
	flagVerbose bool
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "enable debug logging")

	indexCmd.Flags().BoolVar(&flagForce, "force", false, "re-index even if already indexed")
	indexCmd.Flags().StringVar(&flagBranch, "branch", "", "branch name for metadata")
	indexCmd.Flags().StringVar(&flagCommit, "commit", "", "commit SHA to record")

	searchCmd.Flags().StringVar(&flagPath, "path", ".", "codebase path")
	searchCmd.Flags().IntVar(&flagLimit, "limit", 10, "max results")
	searchCmd.Flags().StringVar(&flagExt, "ext", "", "filter by file extensions (comma-separated, e.g. .go,.ts)")

	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(clearCmd)
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if flagVerbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func newStore() (vectorstore.Store, error) {
	dbType := envOrDefault("CODESIGHT_DB_TYPE", "milvus")
	switch dbType {
	case "milvus":
		address := envOrDefault("CODESIGHT_DB_ADDRESS", "localhost:19530")
		token := os.Getenv("CODESIGHT_DB_TOKEN")
		return vectorstore.NewMilvusStore(address, token), nil
	default:
		return nil, fmt.Errorf("unsupported db type: %s", dbType)
	}
}

func newEmbedder() embedding.Provider {
	host := envOrDefault("CODESIGHT_OLLAMA_HOST", "http://127.0.0.1:11434")
	model := envOrDefault("CODESIGHT_EMBEDDING_MODEL", "nomic-embed-text")
	return embedding.NewOllamaClient(host, model, "")
}

func runWithTimeout(timeout time.Duration, action string, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := fn(ctx); err != nil {
		return wrapTimeoutError(action, timeout, err)
	}
	return nil
}

func wrapTimeoutError(action string, timeout time.Duration, err error) error {
	if !isTimeoutError(err) {
		return err
	}

	return fmt.Errorf(
		"%s timed out after %s; network access may be blocked in this sandbox or the configured service may be unreachable: %w",
		action,
		timeout,
		err,
	)
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func parseOllamaMaxInputCharsOverride() (int, error) {
	raw := strings.TrimSpace(os.Getenv(ollamaMaxInputCharsEnv))
	if raw == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", ollamaMaxInputCharsEnv, raw)
	}
	return n, nil
}

func capMaxInputChars(base, cap int) int {
	if base <= 0 {
		return cap
	}
	if cap <= 0 {
		return base
	}
	if cap < base {
		return cap
	}
	return base
}

func resolveProjectPath(path string) (string, error) {
	return filepath.Abs(path)
}

func detectGitCommit(path string) (string, error) {
	absPath, err := resolveProjectPath(path)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	out, err := exec.Command("git", "-C", absPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("detecting git commit: %w", err)
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "", fmt.Errorf("detecting git commit: empty result")
	}
	return commit, nil
}

func runIndex(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	logger := newLogger()
	commit := strings.TrimSpace(flagCommit)
	if commit == "" {
		detectedCommit, err := detectGitCommit(path)
		if err != nil {
			logger.Debug("unable to auto-detect git commit; proceeding without commit metadata", "path", path, "error", err)
		} else {
			commit = detectedCommit
			logger.Debug("auto-detected git commit", "commit", commit)
		}
	}

	overrideMaxInputChars, err := parseOllamaMaxInputCharsOverride()
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return fmt.Errorf("connecting to vector store: %w", err)
	}
	defer store.Close()

	ctx := context.Background()

	embedder := newEmbedder()

	// Auto-detect the model's context length and derive chunk limits.
	var splitterOpts []splitter.Option
	if oc, ok := embedder.(*embedding.OllamaClient); ok {
		if err := runWithTimeout(interactiveNetworkTimeout, "detecting model context length", func(ctx context.Context) error {
			contextTokens, err := oc.DetectContextLength(ctx)
			if err != nil {
				return err
			}
			logger.Debug("detected model context length", "tokens", contextTokens, "max_input_chars", oc.MaxInputChars())
			return nil
		}); err != nil {
			logger.Warn("unable to detect model context length, using default", "error", err)
		}

		effectiveMaxInputChars := capMaxInputChars(oc.MaxInputChars(), overrideMaxInputChars)
		oc.SetMaxInputChars(effectiveMaxInputChars)
		logger.Debug(
			"using ollama max input chars",
			"max_input_chars", effectiveMaxInputChars,
			"override_max_input_chars", overrideMaxInputChars,
		)
		splitterOpts = append(splitterOpts, splitter.WithMaxChunkChars(effectiveMaxInputChars))
	}

	idx := &pkg.Indexer{
		Store:    store,
		Embedder: embedder,
		Splitter: splitter.NewTreeSitterSplitter(splitterOpts...),
		Logger:   logger,
	}

	return idx.Index(ctx, pkg.IndexOptions{
		Path:      path,
		Branch:    flagBranch,
		CommitSHA: commit,
		Force:     flagForce,
	})
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	logger := newLogger()

	store, err := newStore()
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return fmt.Errorf("connecting to vector store: %w", err)
	}
	defer store.Close()

	var extensions []string
	if flagExt != "" {
		for _, ext := range strings.Split(flagExt, ",") {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				if !strings.HasPrefix(ext, ".") {
					ext = "." + ext
				}
				extensions = append(extensions, ext)
			}
		}
	}

	searcher := &pkg.Searcher{
		Store:    store,
		Embedder: newEmbedder(),
		Logger:   logger,
	}

	var output *pkg.SearchOutput
	if err := runWithTimeout(interactiveNetworkTimeout, "running search", func(ctx context.Context) error {
		var err error
		output, err = searcher.Search(ctx, pkg.SearchOptions{
			Path:       flagPath,
			Query:      query,
			Limit:      flagLimit,
			Extensions: extensions,
		})
		return err
	}); err != nil {
		return err
	}

	fmt.Print(pkg.FormatResults(output))
	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	store, err := newStore()
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return fmt.Errorf("connecting to vector store: %w", err)
	}
	defer store.Close()

	absPath, err := resolveProjectPath(path)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	collection := pkg.CollectionName(absPath)
	var exists bool
	if err := runWithTimeout(interactiveNetworkTimeout, "checking index status", func(ctx context.Context) error {
		var err error
		exists, err = store.CollectionExists(ctx, collection)
		return err
	}); err != nil {
		return fmt.Errorf("checking collection: %w", err)
	}
	if !exists {
		fmt.Println("Status: not indexed")
		return nil
	}

	var meta *vectorstore.IndexMetadata
	if err := runWithTimeout(interactiveNetworkTimeout, "reading index metadata", func(ctx context.Context) error {
		var err error
		meta, err = store.GetMetadata(ctx, collection)
		return err
	}); err != nil {
		return fmt.Errorf("reading metadata: %w", err)
	}

	if meta == nil {
		fmt.Println("Status: indexed (no metadata)")
		return nil
	}

	fmt.Printf("Collection: %s\n", collection)
	fmt.Printf("Branch:     %s\n", meta.Branch)
	fmt.Printf("Commit:     %s\n", meta.CommitSHA)
	fmt.Printf("Indexed:    %s\n", meta.IndexedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("Files:      %d\n", meta.FileCount)
	fmt.Printf("Chunks:     %d\n", meta.ChunkCount)

	return nil
}

func runClear(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	store, err := newStore()
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return fmt.Errorf("connecting to vector store: %w", err)
	}
	defer store.Close()

	absPath, err := resolveProjectPath(path)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	collection := pkg.CollectionName(absPath)
	var exists bool
	if err := runWithTimeout(interactiveNetworkTimeout, "checking whether an index exists", func(ctx context.Context) error {
		var err error
		exists, err = store.CollectionExists(ctx, collection)
		return err
	}); err != nil {
		return fmt.Errorf("checking collection: %w", err)
	}
	if !exists {
		fmt.Println("No index found.")
		return nil
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "clearing the index", func(ctx context.Context) error {
		return store.DropCollection(ctx, collection)
	}); err != nil {
		return fmt.Errorf("dropping collection: %w", err)
	}

	fmt.Printf("Cleared index for %s\n", absPath)
	return nil
}
