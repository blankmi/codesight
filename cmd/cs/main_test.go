package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	err := wrapEmbedderConnectErrorForConfig(cfg, errors.New("connection refused"))
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

	for _, want := range []string{"index", "search", "status", "clear", "extract", "refs", "callers", "implements"} {
		if !subcommands[want] {
			t.Fatalf("root command is missing %q subcommand", want)
		}
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
		if cfg.Embedding.Model != "nomic-embed-text" {
			t.Fatalf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "nomic-embed-text")
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
