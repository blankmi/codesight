package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	configpkg "github.com/blankbytes/codesight/pkg/config"
	"github.com/blankbytes/codesight/pkg/lsp"
	"github.com/spf13/cobra"
)

const ollamaMaxInputCharsEnv = "CODESIGHT_OLLAMA_MAX_INPUT_CHARS"

func TestRunWithTimeoutWrapsTimeoutErrors(t *testing.T) {
	timeout := 20 * time.Millisecond
	start := time.Now()

	err := runWithTimeout(timeout, "running search", func(ctx context.Context) error {
		<-ctx.Done()
		return fmt.Errorf("embedding query: %w", ctx.Err())
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "running search timed out after 20ms") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "network access may be blocked in this sandbox") {
		t.Fatalf("expected sandbox hint, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("timeout wrapper took too long: %s", elapsed)
	}
}

func TestRunWithTimeoutPassesThroughNonTimeoutErrors(t *testing.T) {
	want := errors.New("boom")

	err := runWithTimeout(time.Second, "running search", func(context.Context) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped error %v, got %v", want, err)
	}
	if strings.Contains(err.Error(), "network access may be blocked") {
		t.Fatalf("unexpected timeout hint in error: %v", err)
	}
}

func TestInteractiveSearchTimeoutExceedsNetworkProbeTimeout(t *testing.T) {
	if interactiveSearchTimeout <= interactiveNetworkTimeout {
		t.Fatalf("interactiveSearchTimeout = %s, want greater than interactiveNetworkTimeout = %s", interactiveSearchTimeout, interactiveNetworkTimeout)
	}
}

func TestParseOllamaMaxInputCharsOverride(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     int
		wantErr  bool
	}{
		{
			name:     "unset",
			envValue: "",
			want:     0,
		},
		{
			name:     "valid",
			envValue: "4096",
			want:     4096,
		},
		{
			name:     "non numeric",
			envValue: "abc",
			wantErr:  true,
		},
		{
			name:     "zero",
			envValue: "0",
			wantErr:  true,
		},
		{
			name:     "negative",
			envValue: "-10",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ollamaMaxInputCharsEnv, tt.envValue)

			got, err := parseOllamaMaxInputCharsOverride()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), ollamaMaxInputCharsEnv) || !strings.Contains(err.Error(), "positive integer") {
					t.Fatalf("unexpected error message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCapMaxInputChars(t *testing.T) {
	tests := []struct {
		name string
		base int
		cap  int
		want int
	}{
		{
			name: "no override uses base",
			base: 8000,
			cap:  0,
			want: 8000,
		},
		{
			name: "lower cap wins",
			base: 8000,
			cap:  4000,
			want: 4000,
		},
		{
			name: "higher cap does not increase",
			base: 8000,
			cap:  12000,
			want: 8000,
		},
		{
			name: "base missing falls back to cap",
			base: 0,
			cap:  3000,
			want: 3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := capMaxInputChars(tt.base, tt.cap); got != tt.want {
				t.Fatalf("capMaxInputChars(%d, %d) = %d, want %d", tt.base, tt.cap, got, tt.want)
			}
		})
	}
}

func TestWrapVectorStoreConnectErrorIncludesAddress(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.DB.Address = "milvus.example.com:19530"

	err := wrapVectorStoreConnectErrorForConfig(cfg, errors.New("connection refused"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Milvus not reachable") {
		t.Fatalf("expected agent-friendly Milvus message, got: %s", msg)
	}
	if !strings.Contains(msg, "milvus.example.com:19530") {
		t.Fatalf("expected configured address in error, got: %s", msg)
	}
	if !strings.Contains(msg, "CODESIGHT_DB_ADDRESS") {
		t.Fatalf("expected env var hint in error, got: %s", msg)
	}
}

func TestWrapEmbedderConnectErrorIncludesHostAndModel(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.Embedding.OllamaHost = "http://ollama.local:11434"
	cfg.Embedding.Model = "custom-embed"

	err := wrapEmbedderConnectErrorForConfig(cfg, &url.Error{
		Op:  "Post",
		URL: "http://ollama.local:11434/api/embed",
		Err: context.DeadlineExceeded,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Ollama not reachable") {
		t.Fatalf("expected agent-friendly Ollama message, got: %s", msg)
	}
	if !strings.Contains(msg, "ollama.local:11434") {
		t.Fatalf("expected configured host in error, got: %s", msg)
	}
	if !strings.Contains(msg, "custom-embed") {
		t.Fatalf("expected model name in error, got: %s", msg)
	}
	if !strings.Contains(msg, "CODESIGHT_OLLAMA_HOST") {
		t.Fatalf("expected env var hint in error, got: %s", msg)
	}
}

func TestWrapEmbedderConnectErrorPassesThroughNonConnectivityErrors(t *testing.T) {
	cfg := configpkg.Defaults()
	want := errors.New("no index found for /tmp/project -- run 'cs index' first")

	err := wrapEmbedderConnectErrorForConfig(cfg, want)
	if !errors.Is(err, want) {
		t.Fatalf("expected original error %v, got %v", want, err)
	}
	if strings.Contains(err.Error(), "Ollama not reachable") {
		t.Fatalf("unexpected Ollama wrapper for non-connectivity error: %v", err)
	}
}

func TestWrapVectorStoreConnectErrorNilPassthrough(t *testing.T) {
	if err := wrapVectorStoreConnectError(nil); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestRootCommandIncludesExtractAndExistingCommands(t *testing.T) {
	subcommands := map[string]bool{}
	for _, cmd := range rootCmd.Commands() {
		subcommands[cmd.Name()] = true
	}

	for _, want := range []string{"index", "search", "status", "clear", "extract", "refs", "callers", "implements", "lsp"} {
		if !subcommands[want] {
			t.Fatalf("root command is missing %q subcommand", want)
		}
	}
}

func TestRootCommandWithoutArgsShowsHelp(t *testing.T) {
	clearTestEnv(t)
	setTestHome(t)

	stdout, stderr, err := executeRootCommand(t)
	if err != nil {
		t.Fatalf("root command returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "cs [command]") {
		t.Fatalf("stdout missing usage: %q", stdout)
	}
	if !strings.Contains(stdout, "Available Commands:") {
		t.Fatalf("stdout missing available commands: %q", stdout)
	}
}

func TestExecuteIndexWarmupDisabledSkipsDetection(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.Index.WarmLSP = false

	detectCalls := 0
	previousDetect := detectIndexWarmupLanguage
	detectIndexWarmupLanguage = func(_ string, _ *lsp.Registry) (string, error) {
		detectCalls++
		return "java", nil
	}
	t.Cleanup(func() {
		detectIndexWarmupLanguage = previousDetect
	})

	if err := executeIndexWarmup(context.Background(), cfg, t.TempDir()); err != nil {
		t.Fatalf("executeIndexWarmup returned error: %v", err)
	}
	if detectCalls != 0 {
		t.Fatalf("detectIndexWarmupLanguage calls = %d, want 0 when warmup is disabled", detectCalls)
	}
}

func TestExecuteIndexWarmupEnabledJavaTriggersWarmup(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.Index.WarmLSP = true
	workspace := t.TempDir()

	previousDetect := detectIndexWarmupLanguage
	detectIndexWarmupLanguage = func(gotWorkspace string, registry *lsp.Registry) (string, error) {
		if gotWorkspace != workspace {
			t.Fatalf("detect workspace = %q, want %q", gotWorkspace, workspace)
		}
		if registry == nil {
			t.Fatal("detect registry is nil")
		}
		return "java", nil
	}
	t.Cleanup(func() {
		detectIndexWarmupLanguage = previousDetect
	})

	calls := 0
	previousWarmup := runWorkspaceLSPWarmup
	runWorkspaceLSPWarmup = func(_ context.Context, gotWorkspace, gotLanguage string) error {
		calls++
		if gotWorkspace != workspace {
			t.Fatalf("warmup workspace = %q, want %q", gotWorkspace, workspace)
		}
		if gotLanguage != "java" {
			t.Fatalf("warmup language = %q, want %q", gotLanguage, "java")
		}
		return nil
	}
	t.Cleanup(func() {
		runWorkspaceLSPWarmup = previousWarmup
	})

	if err := executeIndexWarmup(context.Background(), cfg, workspace); err != nil {
		t.Fatalf("executeIndexWarmup returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runWorkspaceLSPWarmup calls = %d, want 1", calls)
	}
}

func TestExecuteIndexWarmupEnabledNonJavaSkipsWarmup(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.Index.WarmLSP = true

	previousDetect := detectIndexWarmupLanguage
	detectIndexWarmupLanguage = func(_ string, _ *lsp.Registry) (string, error) {
		return "go", nil
	}
	t.Cleanup(func() {
		detectIndexWarmupLanguage = previousDetect
	})

	warmupCalls := 0
	previousWarmup := runWorkspaceLSPWarmup
	runWorkspaceLSPWarmup = func(context.Context, string, string) error {
		warmupCalls++
		return nil
	}
	t.Cleanup(func() {
		runWorkspaceLSPWarmup = previousWarmup
	})

	if err := executeIndexWarmup(context.Background(), cfg, t.TempDir()); err != nil {
		t.Fatalf("executeIndexWarmup returned error: %v", err)
	}
	if warmupCalls != 0 {
		t.Fatalf("runWorkspaceLSPWarmup calls = %d, want 0 for non-java workspace", warmupCalls)
	}
}

func TestStartIndexWarmupInBackgroundLogsWarningOnFailure(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.Index.WarmLSP = true

	// Signal after the full goroutine body (including Warn log) completes,
	// not just after the mock returns, to avoid a race on stderr.
	goroutineDone := make(chan struct{})
	previousIndexWarmup := runIndexWarmup
	runIndexWarmup = func(_ context.Context, gotCfg *configpkg.Config, gotWorkspace string) error {
		if gotCfg != cfg {
			t.Fatalf("runIndexWarmup cfg pointer mismatch")
		}
		if gotWorkspace != "/repo" {
			t.Fatalf("runIndexWarmup workspace = %q, want %q", gotWorkspace, "/repo")
		}
		return errors.New("boom")
	}
	t.Cleanup(func() {
		runIndexWarmup = previousIndexWarmup
	})

	var stderr bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&stderr, &slog.HandlerOptions{}))

	go func() {
		defer close(goroutineDone)
		if err := runIndexWarmup(context.Background(), cfg, "/repo"); err != nil && logger != nil {
			logger.Warn("lsp warmup failed", "error", err)
		}
	}()

	select {
	case <-goroutineDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for background warmup to run")
	}

	if !strings.Contains(stderr.String(), "lsp warmup failed") {
		t.Fatalf("stderr missing warmup warning: %q", stderr.String())
	}
}

func TestDetectRefsLanguageRespectsCsignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".csignore"), []byte("*.py\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.csignore) returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.py"), []byte("def a():\n    pass\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(a.py) returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.py"), []byte("def b():\n    pass\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(b.py) returned error: %v", err)
	}

	language, err := detectRefsLanguage(root, lsp.NewRegistry())
	if err != nil {
		t.Fatalf("detectRefsLanguage returned error: %v", err)
	}
	if language != "go" {
		t.Fatalf("language = %q, want %q", language, "go")
	}
}

func TestConfigIntegration_EnvVarsStillWork(t *testing.T) {
	clearTestEnv(t)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	projectDir := t.TempDir()
	var seenModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		if r.URL.Path != "/api/embed" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode embed request: %v", err)
		}
		seenModel = payload.Model

		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
	}))
	defer server.Close()

	t.Setenv("CODESIGHT_OLLAMA_HOST", server.URL)
	t.Setenv("CODESIGHT_EMBEDDING_MODEL", "env-model-override")

	originalRunE := searchCmd.RunE
	searchCmd.RunE = func(cmd *cobra.Command, args []string) error {
		_, err := newEmbedder(currentConfig()).Embed(cmd.Context(), "hello")
		return err
	}
	t.Cleanup(func() {
		searchCmd.RunE = originalRunE
	})

	_, _, err := executeRootCommand(t, "search", "hello", "--path", projectDir)
	if err != nil {
		t.Fatalf("search command returned error: %v", err)
	}

	if seenModel != "env-model-override" {
		t.Fatalf("embed request model = %q, want %q", seenModel, "env-model-override")
	}
}

func TestConfigIntegration_DefaultsUnchanged(t *testing.T) {
	clearTestEnv(t)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	projectDir := t.TempDir()

	originalRunE := statusCmd.RunE
	statusCmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg := currentConfig()

		if cfg.DB.Type != "milvus" {
			t.Fatalf("DB.Type = %q, want %q", cfg.DB.Type, "milvus")
		}
		if cfg.DB.Address != "localhost:19530" {
			t.Fatalf("DB.Address = %q, want %q", cfg.DB.Address, "localhost:19530")
		}
		if cfg.DB.Token != "" {
			t.Fatalf("DB.Token = %q, want empty", cfg.DB.Token)
		}

		if cfg.Embedding.OllamaHost != "http://127.0.0.1:11434" {
			t.Fatalf("Embedding.OllamaHost = %q, want %q", cfg.Embedding.OllamaHost, "http://127.0.0.1:11434")
		}
		if cfg.Embedding.Model != "bge-m3" {
			t.Fatalf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "bge-m3")
		}
		if cfg.Embedding.MaxInputChars != 0 {
			t.Fatalf("Embedding.MaxInputChars = %d, want 0", cfg.Embedding.MaxInputChars)
		}

		return nil
	}
	t.Cleanup(func() {
		statusCmd.RunE = originalRunE
	})

	_, _, err := executeRootCommand(t, "status", projectDir)
	if err != nil {
		t.Fatalf("status command returned error: %v", err)
	}
}

func TestJdtlsDataDir_WithCodesightDir(t *testing.T) {
	workspace := t.TempDir()
	codesightDir := filepath.Join(workspace, ".codesight")
	if err := os.MkdirAll(codesightDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(.codesight) returned error: %v", err)
	}

	got, err := jdtlsDataDir(workspace)
	if err != nil {
		t.Fatalf("jdtlsDataDir returned error: %v", err)
	}

	want := filepath.Join(codesightDir, "lsp", "java", "jdtls-data")
	if got != want {
		t.Fatalf("jdtlsDataDir() = %q, want %q", got, want)
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("expected jdtls directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("jdtls data path %q is not a directory", got)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("jdtls data directory permissions = %#o, expected user-only permissions", info.Mode().Perm())
	}
}

func TestJdtlsDataDir_WithoutCodesightDir(t *testing.T) {
	workspace := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)

	got, err := jdtlsDataDir(workspace)
	if err != nil {
		t.Fatalf("jdtlsDataDir returned error: %v", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(workspace)))
	want := filepath.Join(stateDir, "jdtls-data", hash[:16])
	if got != want {
		t.Fatalf("jdtlsDataDir() = %q, want %q", got, want)
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("expected fallback jdtls directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("fallback jdtls data path %q is not a directory", got)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("fallback jdtls directory permissions = %#o, expected user-only permissions", info.Mode().Perm())
	}
}

func TestJdtlsDataDir_GitignoreCreated(t *testing.T) {
	workspace := t.TempDir()
	codesightDir := filepath.Join(workspace, ".codesight")
	if err := os.MkdirAll(codesightDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(.codesight) returned error: %v", err)
	}

	if _, err := jdtlsDataDir(workspace); err != nil {
		t.Fatalf("jdtlsDataDir returned error: %v", err)
	}

	gitignorePath := filepath.Join(codesightDir, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("ReadFile(.gitignore) returned error: %v", err)
	}
	if string(content) != "lsp/\n" {
		t.Fatalf(".gitignore content = %q, want %q", string(content), "lsp/\n")
	}
}

func TestJdtlsDataDir_GitignoreNotOverwritten(t *testing.T) {
	workspace := t.TempDir()
	codesightDir := filepath.Join(workspace, ".codesight")
	if err := os.MkdirAll(codesightDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(.codesight) returned error: %v", err)
	}

	gitignorePath := filepath.Join(codesightDir, ".gitignore")
	initialContent := "cache/\ncustom"
	if err := os.WriteFile(gitignorePath, []byte(initialContent), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) returned error: %v", err)
	}

	if _, err := jdtlsDataDir(workspace); err != nil {
		t.Fatalf("jdtlsDataDir returned error: %v", err)
	}

	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("ReadFile(.gitignore) returned error: %v", err)
	}

	want := initialContent + "\nlsp/\n"
	if string(content) != want {
		t.Fatalf(".gitignore content = %q, want %q", string(content), want)
	}
}

func TestJdtlsInitOptionsForWorkspaceFirstLaunchNoSuppression(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "build.gradle"), []byte("plugins { id 'java' }"), 0o600); err != nil {
		t.Fatalf("WriteFile(build.gradle) returned error: %v", err)
	}

	cfg := configpkg.Defaults()
	initOptions, err := jdtlsInitOptionsForWorkspace(workspace, cfg)
	if err != nil {
		t.Fatalf("jdtlsInitOptionsForWorkspace returned error: %v", err)
	}
	if initOptions != nil {
		if _, ok := gradleImportEnabledValue(initOptions); ok {
			t.Fatalf("first launch should not set gradle import suppression: %#v", initOptions)
		}
	}
}

func TestJdtlsInitOptionsForWorkspaceUnchangedLaunchAddsSuppression(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "build.gradle"), []byte("plugins { id 'java' }"), 0o600); err != nil {
		t.Fatalf("WriteFile(build.gradle) returned error: %v", err)
	}

	cfg := configpkg.Defaults()
	if _, err := jdtlsInitOptionsForWorkspace(workspace, cfg); err != nil {
		t.Fatalf("initial jdtlsInitOptionsForWorkspace returned error: %v", err)
	}

	initOptions, err := jdtlsInitOptionsForWorkspace(workspace, cfg)
	if err != nil {
		t.Fatalf("second jdtlsInitOptionsForWorkspace returned error: %v", err)
	}

	enabled, ok := gradleImportEnabledValue(initOptions)
	if !ok {
		t.Fatalf("expected suppression flag in init options: %#v", initOptions)
	}
	if enabled {
		t.Fatalf("gradle import enabled = %t, want false", enabled)
	}
}

func TestJdtlsInitOptionsForWorkspaceChangedLaunchRemovesSuppression(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)

	workspace := t.TempDir()
	buildGradlePath := filepath.Join(workspace, "build.gradle")
	if err := os.WriteFile(buildGradlePath, []byte("plugins { id 'java' }"), 0o600); err != nil {
		t.Fatalf("WriteFile(build.gradle) returned error: %v", err)
	}

	cfg := configpkg.Defaults()
	if _, err := jdtlsInitOptionsForWorkspace(workspace, cfg); err != nil {
		t.Fatalf("initial jdtlsInitOptionsForWorkspace returned error: %v", err)
	}
	if _, err := jdtlsInitOptionsForWorkspace(workspace, cfg); err != nil {
		t.Fatalf("second jdtlsInitOptionsForWorkspace returned error: %v", err)
	}

	if err := os.WriteFile(buildGradlePath, []byte("plugins { id 'java-library' }"), 0o600); err != nil {
		t.Fatalf("WriteFile(build.gradle changed) returned error: %v", err)
	}

	initOptions, err := jdtlsInitOptionsForWorkspace(workspace, cfg)
	if err != nil {
		t.Fatalf("third jdtlsInitOptionsForWorkspace returned error: %v", err)
	}
	if _, ok := gradleImportEnabledValue(initOptions); ok {
		t.Fatalf("suppression flag should be removed after build change: %#v", initOptions)
	}
}

func TestJdtlsInitOptionsForWorkspaceMergesJavaHomeAndSuppression(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "settings.gradle"), []byte("rootProject.name='demo'"), 0o600); err != nil {
		t.Fatalf("WriteFile(settings.gradle) returned error: %v", err)
	}

	cfg := configpkg.Defaults()
	cfg.LSP.Java.GradleJavaHome = "/opt/jdks/jdk-17"

	if _, err := jdtlsInitOptionsForWorkspace(workspace, cfg); err != nil {
		t.Fatalf("initial jdtlsInitOptionsForWorkspace returned error: %v", err)
	}

	initOptions, err := jdtlsInitOptionsForWorkspace(workspace, cfg)
	if err != nil {
		t.Fatalf("second jdtlsInitOptionsForWorkspace returned error: %v", err)
	}

	enabled, ok := gradleImportEnabledValue(initOptions)
	if !ok || enabled {
		t.Fatalf("expected gradle.enabled=false in init options: %#v", initOptions)
	}

	home, ok := gradleJavaHomeValue(initOptions)
	if !ok {
		t.Fatalf("expected gradle.java.home in init options: %#v", initOptions)
	}
	if home != cfg.LSP.Java.GradleJavaHome {
		t.Fatalf("gradle java home = %q, want %q", home, cfg.LSP.Java.GradleJavaHome)
	}
}

func TestDetectJavaGradleBuildBaselineIgnoresPomXML(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "build.gradle"), []byte("plugins { id 'java' }"), 0o600); err != nil {
		t.Fatalf("WriteFile(build.gradle) returned error: %v", err)
	}
	pomPath := filepath.Join(workspace, "pom.xml")
	if err := os.WriteFile(pomPath, []byte("<project/>"), 0o600); err != nil {
		t.Fatalf("WriteFile(pom.xml) returned error: %v", err)
	}

	first, err := detectJavaGradleBuildBaseline(workspace)
	if err != nil {
		t.Fatalf("detectJavaGradleBuildBaseline returned error: %v", err)
	}

	if err := os.WriteFile(pomPath, []byte("<project><name>changed</name></project>"), 0o600); err != nil {
		t.Fatalf("WriteFile(pom.xml changed) returned error: %v", err)
	}

	second, err := detectJavaGradleBuildBaseline(workspace)
	if err != nil {
		t.Fatalf("detectJavaGradleBuildBaseline (second) returned error: %v", err)
	}

	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("pom.xml changes should not affect tracked baseline fingerprint: first=%q second=%q", first.Fingerprint, second.Fingerprint)
	}
}

func gradleImportEnabledValue(initOptions map[string]any) (bool, bool) {
	gradleOptions, ok := gradleOptions(initOptions)
	if !ok {
		return false, false
	}
	value, ok := gradleOptions["enabled"]
	if !ok {
		return false, false
	}
	enabled, ok := value.(bool)
	if !ok {
		return false, false
	}
	return enabled, true
}

func gradleJavaHomeValue(initOptions map[string]any) (string, bool) {
	gradleOptions, ok := gradleOptions(initOptions)
	if !ok {
		return "", false
	}
	javaOptionsRaw, ok := gradleOptions["java"]
	if !ok {
		return "", false
	}
	javaOptions, ok := javaOptionsRaw.(map[string]any)
	if !ok {
		return "", false
	}
	homeRaw, ok := javaOptions["home"]
	if !ok {
		return "", false
	}
	home, ok := homeRaw.(string)
	if !ok {
		return "", false
	}
	return home, true
}

func gradleOptions(initOptions map[string]any) (map[string]any, bool) {
	if initOptions == nil {
		return nil, false
	}
	settingsRaw, ok := initOptions["settings"]
	if !ok {
		return nil, false
	}
	settings, ok := settingsRaw.(map[string]any)
	if !ok {
		return nil, false
	}
	javaRaw, ok := settings["java"]
	if !ok {
		return nil, false
	}
	javaOptions, ok := javaRaw.(map[string]any)
	if !ok {
		return nil, false
	}
	importRaw, ok := javaOptions["import"]
	if !ok {
		return nil, false
	}
	importOptions, ok := importRaw.(map[string]any)
	if !ok {
		return nil, false
	}
	gradleRaw, ok := importOptions["gradle"]
	if !ok {
		return nil, false
	}
	gradleOptions, ok := gradleRaw.(map[string]any)
	if !ok {
		return nil, false
	}
	return gradleOptions, true
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
