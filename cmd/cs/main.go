package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	pkg "github.com/blankbytes/codesight/pkg"
	"github.com/blankbytes/codesight/pkg/embedding"
	extractpkg "github.com/blankbytes/codesight/pkg/extract"
	csignore "github.com/blankbytes/codesight/pkg/ignore"
	"github.com/blankbytes/codesight/pkg/lsp"
	"github.com/blankbytes/codesight/pkg/splitter"
	"github.com/blankbytes/codesight/pkg/vectorstore"
	"github.com/spf13/cobra"
)

const (
	interactiveNetworkTimeout = 1 * time.Second
	ollamaMaxInputCharsEnv    = "CODESIGHT_OLLAMA_MAX_INPUT_CHARS"
	refsLSPShutdownTimeout    = 2 * time.Second
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

var extractCmd = &cobra.Command{
	Use:   "extract -f <file> -s <symbol>",
	Short: "Extract a named symbol from a file or directory",
	Args:  cobra.NoArgs,
	RunE:  runExtract,
}

var refsCmd = &cobra.Command{
	Use:   "refs <symbol>",
	Short: "Find references for a symbol",
	Args:  cobra.ExactArgs(1),
	RunE:  runRefs,
}

var callersCmd = &cobra.Command{
	Use:   "callers <symbol>",
	Short: "Find incoming callers for a symbol",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallers,
}

var implementsCmd = &cobra.Command{
	Use:   "implements <symbol>",
	Short: "Find implementations for a symbol",
	Args:  cobra.ExactArgs(1),
	RunE:  runImplements,
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

type refsCommandOptions struct {
	WorkspaceRoot string
	Symbol        string
	Kind          string
}

type callersCommandOptions struct {
	WorkspaceRoot string
	Symbol        string
	Depth         int
}

type implementsCommandOptions struct {
	WorkspaceRoot string
	Symbol        string
}

type refsProcessTransport struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd

	closeOnce sync.Once
	closeErr  error
}

var (
	flagForce   bool
	flagBranch  string
	flagCommit  string
	flagLimit   int
	flagExt     string
	flagPath    string
	flagVerbose bool

	runRefsCommand       = executeRefsCommand
	runCallersCommand    = executeCallersCommand
	runImplementsCommand = executeImplementsCommand

	errNoSupportedRefsLanguage = errors.New("no supported LSP language detected")
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "enable debug logging")

	indexCmd.Flags().BoolVar(&flagForce, "force", false, "re-index even if already indexed")
	indexCmd.Flags().StringVar(&flagBranch, "branch", "", "branch name for metadata")
	indexCmd.Flags().StringVar(&flagCommit, "commit", "", "commit SHA to record")

	searchCmd.Flags().StringVar(&flagPath, "path", ".", "codebase path")
	searchCmd.Flags().IntVar(&flagLimit, "limit", 10, "max results")
	searchCmd.Flags().StringVar(&flagExt, "ext", "", "filter by file extensions (comma-separated, e.g. .go,.ts)")

	extractCmd.Flags().StringP("file", "f", "", "file or directory path")
	extractCmd.Flags().StringP("symbol", "s", "", "symbol name")
	extractCmd.Flags().String("format", "raw", "output format (raw|json)")
	if err := extractCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
	if err := extractCmd.MarkFlagRequired("symbol"); err != nil {
		panic(err)
	}

	refsCmd.Flags().String("path", "", "project path")
	refsCmd.Flags().String("kind", "", "reference kind filter (function|method|class|interface|type|constant)")
	callersCmd.Flags().String("path", "", "project path")
	callersCmd.Flags().Int("depth", 1, "call hierarchy depth")
	implementsCmd.Flags().String("path", "", "project path")

	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(extractCmd)
	rootCmd.AddCommand(refsCmd)
	rootCmd.AddCommand(callersCmd)
	rootCmd.AddCommand(implementsCmd)
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

// wrapVectorStoreConnectError adds agent-friendly guidance to Milvus/Ollama connection failures.
func wrapVectorStoreConnectError(err error) error {
	if err == nil {
		return nil
	}
	address := envOrDefault("CODESIGHT_DB_ADDRESS", "localhost:19530")
	return fmt.Errorf(
		"cs: Milvus not reachable at %s. Set CODESIGHT_DB_ADDRESS or start Milvus: %w",
		address, err,
	)
}

func wrapEmbedderConnectError(err error) error {
	if err == nil {
		return nil
	}
	host := envOrDefault("CODESIGHT_OLLAMA_HOST", "http://127.0.0.1:11434")
	model := envOrDefault("CODESIGHT_EMBEDDING_MODEL", "nomic-embed-text")
	return fmt.Errorf(
		"cs: Ollama not reachable at %s (model %s). Set CODESIGHT_OLLAMA_HOST or start Ollama: %w",
		host, model, err,
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
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, nil // fall back to abs if symlink resolution fails
	}
	return resolved, nil
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
	if resolved, err := resolveProjectPath(path); err == nil {
		path = resolved
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
		return wrapVectorStoreConnectError(err)
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
		return wrapVectorStoreConnectError(err)
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

	searchPath := flagPath
	if resolved, err := resolveProjectPath(searchPath); err == nil {
		searchPath = resolved
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
			Path:       searchPath,
			Query:      query,
			Limit:      flagLimit,
			Extensions: extensions,
		})
		return err
	}); err != nil {
		return wrapEmbedderConnectError(err)
	}

	fmt.Print(pkg.FormatResults(output))
	return nil
}

func runExtract(cmd *cobra.Command, args []string) error {
	targetPath, err := cmd.Flags().GetString("file")
	if err != nil {
		return err
	}
	symbol, err := cmd.Flags().GetString("symbol")
	if err != nil {
		return err
	}
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}

	if resolved, err := resolveProjectPath(targetPath); err == nil {
		targetPath = resolved
	}

	output, err := extractpkg.Extract(targetPath, symbol, format)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(cmd.OutOrStdout(), output); err != nil {
		return err
	}
	return nil
}

func runRefs(cmd *cobra.Command, args []string) error {
	pathFlag, err := cmd.Flags().GetString("path")
	if err != nil {
		return err
	}
	kindFlag, err := cmd.Flags().GetString("kind")
	if err != nil {
		return err
	}

	kind, err := lsp.NormalizeRefKind(kindFlag)
	if err != nil {
		return err
	}

	workspaceRoot, err := resolveRefsWorkspaceRoot(pathFlag)
	if err != nil {
		return err
	}

	output, err := runRefsCommand(cmd.Context(), refsCommandOptions{
		WorkspaceRoot: workspaceRoot,
		Symbol:        args[0],
		Kind:          kind,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(cmd.OutOrStdout(), output); err != nil {
		return err
	}
	return nil
}

func runCallers(cmd *cobra.Command, args []string) error {
	pathFlag, err := cmd.Flags().GetString("path")
	if err != nil {
		return err
	}
	depth, err := cmd.Flags().GetInt("depth")
	if err != nil {
		return err
	}
	if depth <= 0 {
		return errors.New("depth must be a positive integer")
	}

	workspaceRoot, err := resolveRefsWorkspaceRoot(pathFlag)
	if err != nil {
		return err
	}

	output, err := runCallersCommand(cmd.Context(), callersCommandOptions{
		WorkspaceRoot: workspaceRoot,
		Symbol:        args[0],
		Depth:         depth,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(cmd.OutOrStdout(), output); err != nil {
		return err
	}
	return nil
}

func runImplements(cmd *cobra.Command, args []string) error {
	pathFlag, err := cmd.Flags().GetString("path")
	if err != nil {
		return err
	}

	workspaceRoot, err := resolveRefsWorkspaceRoot(pathFlag)
	if err != nil {
		return err
	}

	output, err := runImplementsCommand(cmd.Context(), implementsCommandOptions{
		WorkspaceRoot: workspaceRoot,
		Symbol:        args[0],
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(cmd.OutOrStdout(), output); err != nil {
		return err
	}
	return nil
}

func resolveRefsWorkspaceRoot(pathFlag string) (string, error) {
	target := strings.TrimSpace(pathFlag)
	if target == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return "", err
		}
		target = workingDirectory
	}

	workspaceRoot, err := resolveProjectPath(target)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(workspaceRoot)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return workspaceRoot, nil
	}
	return filepath.Dir(workspaceRoot), nil
}

func executeRefsCommand(ctx context.Context, opts refsCommandOptions) (string, error) {
	registry := lsp.NewRegistry()
	fallbackBinary := ""
	var client *lsp.Client

	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		if !errors.Is(err, errNoSupportedRefsLanguage) {
			return "", err
		}
		// No supported language detected — will use grep fallback below.
	} else {
		spec, err := registry.Lookup(language)
		if err == nil {
			fallbackBinary = spec.Binary
		}

		// Attempt LSP startup; on any failure, fall through to grep fallback
		// instead of hard-erroring. This covers: binary not found, binary
		// crashes on startup, client init failures.
		if err == nil {
			c, lspErr := startRefsLSPClient(ctx, spec, opts.WorkspaceRoot)
			if lspErr == nil {
				client = c
			}
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), refsLSPShutdownTimeout)
			defer cancel()
			_ = client.Shutdown(shutdownCtx)
		}()
	}

	engine := lsp.NewRefsEngine(nil, nil)
	if client != nil {
		engine = lsp.NewRefsEngine(client, nil)
	}
	return engine.Find(ctx, lsp.RefsOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Symbol:        opts.Symbol,
		Kind:          opts.Kind,
		FallbackLSP:   fallbackBinary,
	})
}

func executeCallersCommand(ctx context.Context, opts callersCommandOptions) (string, error) {
	registry := lsp.NewRegistry()

	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		return "", err
	}

	spec, err := registry.Lookup(language)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(spec.Binary); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", callersMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}

	client, err := startRefsLSPClient(ctx, spec, opts.WorkspaceRoot)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", callersMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), refsLSPShutdownTimeout)
		defer cancel()
		_ = client.Shutdown(shutdownCtx)
	}()

	return lsp.NewCallersEngine(client).Find(ctx, lsp.CallersOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Symbol:        opts.Symbol,
		Depth:         opts.Depth,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
}

func executeImplementsCommand(ctx context.Context, opts implementsCommandOptions) (string, error) {
	registry := lsp.NewRegistry()

	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		return "", err
	}

	spec, err := registry.Lookup(language)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(spec.Binary); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", implementsMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}

	client, err := startRefsLSPClient(ctx, spec, opts.WorkspaceRoot)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", implementsMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), refsLSPShutdownTimeout)
		defer cancel()
		_ = client.Shutdown(shutdownCtx)
	}()

	return lsp.NewImplementsEngine(client).Find(ctx, lsp.ImplementsOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Symbol:        opts.Symbol,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
}

func callersMissingLSPError(ctx context.Context, opts callersCommandOptions, spec lsp.ServerSpec) error {
	_, err := lsp.NewCallersEngine(nil).Find(ctx, lsp.CallersOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Symbol:        opts.Symbol,
		Depth:         opts.Depth,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
	return err
}

func implementsMissingLSPError(ctx context.Context, opts implementsCommandOptions, spec lsp.ServerSpec) error {
	_, err := lsp.NewImplementsEngine(nil).Find(ctx, lsp.ImplementsOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Symbol:        opts.Symbol,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
	return err
}

func startRefsLSPClient(
	ctx context.Context,
	spec lsp.ServerSpec,
	workspaceRoot string,
) (*lsp.Client, error) {
	transport, err := startRefsLSPTransport(ctx, spec, workspaceRoot)
	if err != nil {
		return nil, err
	}

	// jdtls needs up to 60s to import a large Gradle/Maven project on first
	// connect. Other language servers (gopls, pylsp) are fast enough at 10s.
	timeout := 10 * time.Second
	if spec.Binary == "jdtls" {
		timeout = 60 * time.Second
	}

	client, err := lsp.NewClient(transport, lsp.WithRequestTimeout(timeout))
	if err != nil {
		_ = transport.Close()
		return nil, err
	}

	initializeParams, err := refsInitializeParams(workspaceRoot, spec)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	if _, err := client.Initialize(ctx, initializeParams); err != nil {
		_ = client.Close()
		return nil, err
	}

	return client, nil
}

func startRefsLSPTransport(ctx context.Context, spec lsp.ServerSpec, workspaceRoot string) (io.ReadWriteCloser, error) {
	args := append([]string(nil), spec.Args...)

	// jdtls: persist workspace index under CODESIGHT_STATE_DIR so subsequent
	// starts reuse the project model instead of re-indexing from scratch.
	if spec.Binary == "jdtls" {
		if dataDir, err := jdtlsDataDir(workspaceRoot); err == nil {
			args = append(args, "-data", dataDir)
		}
	}

	cmd := exec.CommandContext(ctx, spec.Binary, args...)
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open language server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open language server stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("LSP required but %s not found. Install: %s", spec.Binary, spec.InstallHint)
		}
		return nil, fmt.Errorf("start language server %s: %w", spec.Binary, err)
	}

	return &refsProcessTransport{
		stdin:  stdin,
		stdout: stdout,
		cmd:    cmd,
	}, nil
}

func refsInitializeParams(workspaceRoot string, spec lsp.ServerSpec) (lsp.InitializeParams, error) {
	rootURI, err := refsWorkspaceURI(workspaceRoot)
	if err != nil {
		return lsp.InitializeParams{}, err
	}

	workspaceName := filepath.Base(workspaceRoot)
	if strings.TrimSpace(workspaceName) == "" || workspaceName == string(filepath.Separator) {
		workspaceName = "workspace"
	}

	processID := os.Getpid()
	params := lsp.InitializeParams{
		ProcessID:  &processID,
		RootURI:    rootURI,
		ClientInfo: &lsp.ClientInfo{Name: "cs"},
		Capabilities: map[string]any{
			"textDocument": map[string]any{},
		},
		WorkspaceFolders: []lsp.WorkspaceFolder{
			{
				URI:  rootURI,
				Name: workspaceName,
			},
		},
	}

	// jdtls: configure Gradle JDK separately from the jdtls runtime JDK.
	// This allows jdtls to run on Java 21+ while building projects that
	// require an older JDK (e.g. Java 17 for hibernate enhancer).
	if spec.Binary == "jdtls" {
		if initOpts := jdtlsInitOptions(); initOpts != nil {
			params.InitializationOptions = initOpts
		}
	}

	return params, nil
}

// jdtlsDataDir returns a persistent workspace data directory for jdtls under
// CODESIGHT_STATE_DIR. This allows jdtls to cache its project index across
// restarts instead of re-syncing from scratch.
func jdtlsDataDir(workspaceRoot string) (string, error) {
	stateDir, err := lsp.ResolveStateDir()
	if err != nil {
		return "", err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(workspaceRoot)))
	dir := filepath.Join(stateDir, "jdtls-data", hash[:16])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// jdtlsInitOptions builds initializationOptions for jdtls, configuring a
// separate JDK for Gradle builds via CODESIGHT_GRADLE_JAVA_HOME.
func jdtlsInitOptions() map[string]any {
	gradleJavaHome := os.Getenv("CODESIGHT_GRADLE_JAVA_HOME")
	if gradleJavaHome == "" {
		return nil
	}
	return map[string]any{
		"settings": map[string]any{
			"java": map[string]any{
				"import": map[string]any{
					"gradle": map[string]any{
						"java": map[string]any{
							"home": gradleJavaHome,
						},
					},
				},
			},
		},
	}
}

func refsWorkspaceURI(workspaceRoot string) (lsp.DocumentURI, error) {
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	normalized := filepath.ToSlash(absPath)
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}

	return lsp.DocumentURI((&url.URL{
		Scheme: "file",
		Path:   normalized,
	}).String()), nil
}

func (t *refsProcessTransport) Read(p []byte) (int, error) {
	return t.stdout.Read(p)
}

func (t *refsProcessTransport) Write(p []byte) (int, error) {
	return t.stdin.Write(p)
}

func (t *refsProcessTransport) Close() error {
	t.closeOnce.Do(func() {
		var errs []error

		if t.stdin != nil {
			if err := t.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, err)
			}
		}
		if t.stdout != nil {
			if err := t.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, err)
			}
		}

		if t.cmd != nil && t.cmd.Process != nil {
			if err := t.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				errs = append(errs, err)
			}
			if err := t.cmd.Wait(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				errs = append(errs, err)
			}
		}

		t.closeErr = errors.Join(errs...)
	})
	return t.closeErr
}

func detectRefsLanguage(workspaceRoot string, registry *lsp.Registry) (string, error) {
	countByLanguage := map[string]int{}
	supportedLanguageCache := map[string]bool{}
	matcher, err := csignore.NewMatcher(workspaceRoot, nil)
	if err != nil {
		return "", fmt.Errorf("load ignore rules: %w", err)
	}

	err = filepath.WalkDir(workspaceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if path != workspaceRoot && matcher.MatchesPath(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if matcher.MatchesPath(path) {
			return nil
		}

		language := splitter.LanguageFromExtension(filepath.Ext(entry.Name()))
		if language == "" {
			return nil
		}

		isSupported, ok := supportedLanguageCache[language]
		if !ok {
			_, lookupErr := registry.Lookup(language)
			isSupported = lookupErr == nil
			supportedLanguageCache[language] = isSupported
		}
		if !isSupported {
			return nil
		}

		countByLanguage[language]++
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(countByLanguage) == 0 {
		return "", errNoSupportedRefsLanguage
	}

	bestLanguage := ""
	bestCount := -1
	for language, count := range countByLanguage {
		if count > bestCount || (count == bestCount && (bestLanguage == "" || language < bestLanguage)) {
			bestLanguage = language
			bestCount = count
		}
	}
	return bestLanguage, nil
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
		return wrapVectorStoreConnectError(err)
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

	currentCommit, err := detectGitCommit(path)
	if err != nil {
		currentCommit = ""
	}
	matcher, err := csignore.NewMatcher(absPath, nil)
	if err != nil {
		return fmt.Errorf("loading ignore rules: %w", err)
	}

	fmt.Printf("Collection: %s\n", collection)
	fmt.Printf("Status:     %s\n", pkg.StalenessInfo(meta, currentCommit, matcher.Fingerprint()))
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
		return wrapVectorStoreConnectError(err)
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
