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
	"strings"
	"sync"
	"time"

	pkg "github.com/blankbytes/codesight/pkg"
	configpkg "github.com/blankbytes/codesight/pkg/config"
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
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if shouldSkipRuntimeConfigLoad(cmd) {
			runtimeConfig = configpkg.Defaults()
			return nil
		}
		return loadRuntimeConfig(cmd, args)
	},
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
	runIndexWarmup       = executeIndexWarmup

	detectIndexWarmupLanguage = func(workspaceRoot string, registry *lsp.Registry) (string, error) {
		return detectRefsLanguage(workspaceRoot, registry)
	}
	runWorkspaceLSPWarmup = func(ctx context.Context, workspaceRoot, language string) error {
		return executeLSPWarmup(ctx, workspaceRoot, language, runtimeConfig)
	}

	runtimeConfig = configpkg.Defaults()

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
	rootCmd.AddCommand(lspCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(clearCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(initCmd)
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if flagVerbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func loadRuntimeConfig(cmd *cobra.Command, args []string) error {
	projectPath, err := configProjectPath(cmd, args)
	if err != nil {
		return fmt.Errorf("resolve project path for config: %w", err)
	}

	cfg, err := configpkg.LoadConfig(projectPath)
	if err != nil {
		return err
	}
	runtimeConfig = cfg
	return nil
}

func shouldSkipRuntimeConfigLoad(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	return cmd.Name() == initCmd.Name()
}

func currentConfig() *configpkg.Config {
	if runtimeConfig == nil {
		return configpkg.Defaults()
	}
	return runtimeConfig
}

func configProjectPath(cmd *cobra.Command, args []string) (string, error) {
	switch cmd.Name() {
	case indexCmd.Name(), statusCmd.Name(), clearCmd.Name(), configCmd.Name():
		if len(args) > 0 {
			return args[0], nil
		}
		return ".", nil
	case searchCmd.Name():
		pathFlag, err := cmd.Flags().GetString("path")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(pathFlag) == "" {
			return ".", nil
		}
		return pathFlag, nil
	case refsCmd.Name(), callersCmd.Name(), implementsCmd.Name():
		pathFlag, err := cmd.Flags().GetString("path")
		if err != nil {
			return "", err
		}
		return resolveRefsWorkspaceRoot(pathFlag)
	case lspWarmupCmd.Name(), lspStatusCmd.Name(), lspRestartCmd.Name():
		if len(args) > 0 {
			return resolveRefsWorkspaceRoot(args[0])
		}
		return resolveRefsWorkspaceRoot("")
	default:
		return ".", nil
	}
}

func newStore(cfg *configpkg.Config) (vectorstore.Store, error) {
	switch cfg.DB.Type {
	case "milvus":
		return vectorstore.NewMilvusStore(cfg.DB.Address, cfg.DB.Token), nil
	default:
		return nil, fmt.Errorf("unsupported db type: %s", cfg.DB.Type)
	}
}

func newEmbedder(cfg *configpkg.Config) embedding.Provider {
	return embedding.NewOllamaClient(cfg.Embedding.OllamaHost, cfg.Embedding.Model, "")
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
	return wrapVectorStoreConnectErrorForConfig(currentConfig(), err)
}

func wrapVectorStoreConnectErrorForConfig(cfg *configpkg.Config, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"cs: Milvus not reachable at %s. Set CODESIGHT_DB_ADDRESS or start Milvus: %w",
		cfg.DB.Address, err,
	)
}

func wrapEmbedderConnectErrorForConfig(cfg *configpkg.Config, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"cs: Ollama not reachable at %s (model %s). Set CODESIGHT_OLLAMA_HOST or start Ollama: %w",
		cfg.Embedding.OllamaHost, cfg.Embedding.Model, err,
	)
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
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

func shouldWarmLSPOnIndex(cfg *configpkg.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.Index.WarmLSP
}

func executeIndexWarmup(ctx context.Context, cfg *configpkg.Config, workspaceRoot string) error {
	if !shouldWarmLSPOnIndex(cfg) {
		return nil
	}

	registry := lsp.NewRegistry()
	language, err := detectIndexWarmupLanguage(workspaceRoot, registry)
	if err != nil {
		if errors.Is(err, errNoSupportedRefsLanguage) {
			return nil
		}
		return err
	}
	if language != "java" {
		return nil
	}
	return runWorkspaceLSPWarmup(ctx, workspaceRoot, language)
}

func startIndexWarmupInBackground(cfg *configpkg.Config, workspaceRoot string, logger *slog.Logger) {
	if !shouldWarmLSPOnIndex(cfg) {
		return
	}

	go func() {
		if err := runIndexWarmup(context.Background(), cfg, workspaceRoot); err != nil && logger != nil {
			logger.Warn("lsp warmup failed", "error", err)
		}
	}()
}

func runIndex(cmd *cobra.Command, args []string) error {
	cfg := currentConfig()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	if resolved, err := resolveProjectPath(path); err == nil {
		path = resolved
	}

	logger := newLogger()
	startIndexWarmupInBackground(cfg, path, logger)

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

	store, err := newStore(cfg)
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return wrapVectorStoreConnectErrorForConfig(cfg, err)
	}
	defer store.Close()

	ctx := context.Background()

	embedder := newEmbedder(cfg)

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

		effectiveMaxInputChars := capMaxInputChars(oc.MaxInputChars(), cfg.Embedding.MaxInputChars)
		oc.SetMaxInputChars(effectiveMaxInputChars)
		logger.Debug(
			"using ollama max input chars",
			"max_input_chars", effectiveMaxInputChars,
			"override_max_input_chars", cfg.Embedding.MaxInputChars,
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
	cfg := currentConfig()

	query := strings.Join(args, " ")
	logger := newLogger()

	store, err := newStore(cfg)
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return wrapVectorStoreConnectErrorForConfig(cfg, err)
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
		Embedder: newEmbedder(cfg),
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
		return wrapEmbedderConnectErrorForConfig(cfg, err)
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

	params := lsp.InitializeParams{
		ProcessID:  nil,
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
		initOpts, err := jdtlsInitOptionsForWorkspace(workspaceRoot, currentConfig())
		if err != nil {
			return lsp.InitializeParams{}, err
		}
		if initOpts != nil {
			params.InitializationOptions = initOpts
		}
	}

	return params, nil
}

// jdtlsDataDir returns a persistent workspace data directory for jdtls.
// It prefers <workspace>/.codesight/lsp/java/jdtls-data when .codesight exists
// and is writable, and falls back to CODESIGHT_STATE_DIR for compatibility.
func jdtlsDataDir(workspaceRoot string) (string, error) {
	if projectDir, err := projectJdtlsDataDir(workspaceRoot); err == nil && projectDir != "" {
		return projectDir, nil
	}
	return fallbackJdtlsDataDir(workspaceRoot)
}

func projectJdtlsDataDir(workspaceRoot string) (string, error) {
	codesightDir := filepath.Join(workspaceRoot, ".codesight")
	info, err := os.Stat(codesightDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", codesightDir)
	}
	if err := ensureDirWritable(codesightDir); err != nil {
		return "", err
	}

	dir := filepath.Join(codesightDir, "lsp", "java", "jdtls-data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := ensureCodesightGitignore(codesightDir); err != nil {
		return "", err
	}

	return dir, nil
}

func ensureDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".codesight-writable-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ensureCodesightGitignore(codesightDir string) error {
	const entry = "lsp/"
	gitignorePath := filepath.Join(codesightDir, ".gitignore")

	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.WriteFile(gitignorePath, []byte(entry+"\n"), 0o644)
		}
		return err
	}

	if gitignoreContainsLine(string(content), entry) {
		return nil
	}

	file, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := file.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = file.WriteString(entry + "\n")
	return err
}

func gitignoreContainsLine(content, entry string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}
	return false
}

func fallbackJdtlsDataDir(workspaceRoot string) (string, error) {
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
func jdtlsInitOptions(cfg *configpkg.Config) map[string]any {
	if cfg == nil {
		return nil
	}
	gradleJavaHome := cfg.LSP.Java.GradleJavaHome
	return buildJDTLSInitOptions(gradleJavaHome, false)
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
	cfg := currentConfig()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	store, err := newStore(cfg)
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return wrapVectorStoreConnectErrorForConfig(cfg, err)
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
	cfg := currentConfig()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	store, err := newStore(cfg)
	if err != nil {
		return err
	}

	if err := runWithTimeout(interactiveNetworkTimeout, "connecting to the vector store", func(ctx context.Context) error {
		return store.Connect(ctx)
	}); err != nil {
		return wrapVectorStoreConnectErrorForConfig(cfg, err)
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
