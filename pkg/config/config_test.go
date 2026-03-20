package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

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

	if cfg.StateDir != "" {
		t.Fatalf("StateDir = %q, want empty", cfg.StateDir)
	}
	if cfg.LSP.Java.GradleJavaHome != "" {
		t.Fatalf("LSP.Java.GradleJavaHome = %q, want empty", cfg.LSP.Java.GradleJavaHome)
	}
	if cfg.LSP.Java.Timeout != "60s" {
		t.Fatalf("LSP.Java.Timeout = %q, want %q", cfg.LSP.Java.Timeout, "60s")
	}
	if len(cfg.LSP.Java.Args) != 0 {
		t.Fatalf("LSP.Java.Args len = %d, want 0", len(cfg.LSP.Java.Args))
	}
	if len(cfg.LSP.Go.BuildFlags) != 0 {
		t.Fatalf("LSP.Go.BuildFlags len = %d, want 0", len(cfg.LSP.Go.BuildFlags))
	}
	if cfg.LSP.Daemon.IdleTimeout != "10m" {
		t.Fatalf("LSP.Daemon.IdleTimeout = %q, want %q", cfg.LSP.Daemon.IdleTimeout, "10m")
	}
	if cfg.Index.WarmLSP {
		t.Fatal("Index.WarmLSP = true, want false")
	}

	for _, key := range allConfigKeys() {
		if got := cfg.Provenance[key]; got != "default" {
			t.Fatalf("Provenance[%q] = %q, want default", key, got)
		}
	}
}

func TestLoadConfig_NoFiles(t *testing.T) {
	projectDir := t.TempDir()
	setHomeDir(t)
	clearConfigEnv(t)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	expectDefaults(t, cfg)
}

func TestLoadConfig_UserOnly(t *testing.T) {
	homeDir := setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(homeDir, ".codesight", "config.toml"), `
[embedding]
model = "user-model"
max_input_chars = 2048

[lsp.go]
build_flags = ["-tags=integration"]
`)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Embedding.Model != "user-model" {
		t.Fatalf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "user-model")
	}
	if cfg.Embedding.MaxInputChars != 2048 {
		t.Fatalf("Embedding.MaxInputChars = %d, want %d", cfg.Embedding.MaxInputChars, 2048)
	}
	if len(cfg.LSP.Go.BuildFlags) != 1 || cfg.LSP.Go.BuildFlags[0] != "-tags=integration" {
		t.Fatalf("LSP.Go.BuildFlags = %#v, want [-tags=integration]", cfg.LSP.Go.BuildFlags)
	}
	if cfg.DB.Address != "localhost:19530" {
		t.Fatalf("DB.Address = %q, want default", cfg.DB.Address)
	}

	if cfg.Provenance[keyEmbeddingModel] != userConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyEmbeddingModel, cfg.Provenance[keyEmbeddingModel], userConfigSource)
	}
	if cfg.Provenance[keyEmbeddingMaxInput] != userConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyEmbeddingMaxInput, cfg.Provenance[keyEmbeddingMaxInput], userConfigSource)
	}
	if cfg.Provenance[keyLSPGoBuildFlags] != userConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyLSPGoBuildFlags, cfg.Provenance[keyLSPGoBuildFlags], userConfigSource)
	}
}

func TestLoadConfig_ProjectOverridesUser(t *testing.T) {
	homeDir := setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(homeDir, ".codesight", "config.toml"), `
[embedding]
model = "user-model"

[db]
address = "user:19530"
`)
	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[embedding]
model = "project-model"
`)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Embedding.Model != "project-model" {
		t.Fatalf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "project-model")
	}
	if cfg.DB.Address != "user:19530" {
		t.Fatalf("DB.Address = %q, want user value", cfg.DB.Address)
	}

	if cfg.Provenance[keyEmbeddingModel] != projectConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyEmbeddingModel, cfg.Provenance[keyEmbeddingModel], projectConfigSource)
	}
	if cfg.Provenance[keyDBAddress] != userConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyDBAddress, cfg.Provenance[keyDBAddress], userConfigSource)
	}
}

func TestLoadConfig_EnvOverridesAll(t *testing.T) {
	homeDir := setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(homeDir, ".codesight", "config.toml"), `
[db]
type = "userdb"
address = "user:19530"

[embedding]
model = "user-model"
`)
	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[db]
type = "projectdb"

[embedding]
model = "project-model"
`)

	t.Setenv("CODESIGHT_DB_TYPE", "envdb")
	t.Setenv("CODESIGHT_DB_ADDRESS", "env:19530")
	t.Setenv("CODESIGHT_DB_TOKEN", "token-123")
	t.Setenv("CODESIGHT_OLLAMA_HOST", "http://localhost:9999")
	t.Setenv("CODESIGHT_EMBEDDING_MODEL", "env-model")
	t.Setenv("CODESIGHT_OLLAMA_MAX_INPUT_CHARS", "8192")
	t.Setenv("CODESIGHT_STATE_DIR", "/tmp/codesight-state")
	t.Setenv("CODESIGHT_GRADLE_JAVA_HOME", "/usr/lib/jvm/java-21")
	t.Setenv("CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT", "45s")

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.DB.Type != "envdb" {
		t.Fatalf("DB.Type = %q, want env override", cfg.DB.Type)
	}
	if cfg.DB.Address != "env:19530" {
		t.Fatalf("DB.Address = %q, want env override", cfg.DB.Address)
	}
	if cfg.DB.Token != "token-123" {
		t.Fatalf("DB.Token = %q, want env override", cfg.DB.Token)
	}
	if cfg.Embedding.OllamaHost != "http://localhost:9999" {
		t.Fatalf("Embedding.OllamaHost = %q, want env override", cfg.Embedding.OllamaHost)
	}
	if cfg.Embedding.Model != "env-model" {
		t.Fatalf("Embedding.Model = %q, want env override", cfg.Embedding.Model)
	}
	if cfg.Embedding.MaxInputChars != 8192 {
		t.Fatalf("Embedding.MaxInputChars = %d, want env override", cfg.Embedding.MaxInputChars)
	}
	if cfg.StateDir != "/tmp/codesight-state" {
		t.Fatalf("StateDir = %q, want env override", cfg.StateDir)
	}
	if cfg.LSP.Java.GradleJavaHome != "/usr/lib/jvm/java-21" {
		t.Fatalf("LSP.Java.GradleJavaHome = %q, want env override", cfg.LSP.Java.GradleJavaHome)
	}
	if cfg.LSP.Daemon.IdleTimeout != "45s" {
		t.Fatalf("LSP.Daemon.IdleTimeout = %q, want env override", cfg.LSP.Daemon.IdleTimeout)
	}

	if cfg.Provenance[keyDBType] != "CODESIGHT_DB_TYPE" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_DB_TYPE", keyDBType, cfg.Provenance[keyDBType])
	}
	if cfg.Provenance[keyEmbeddingModel] != "CODESIGHT_EMBEDDING_MODEL" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_EMBEDDING_MODEL", keyEmbeddingModel, cfg.Provenance[keyEmbeddingModel])
	}
	if cfg.Provenance[keyEmbeddingMaxInput] != "CODESIGHT_OLLAMA_MAX_INPUT_CHARS" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_OLLAMA_MAX_INPUT_CHARS", keyEmbeddingMaxInput, cfg.Provenance[keyEmbeddingMaxInput])
	}
	if cfg.Provenance[keyLSPDaemonTimeout] != "CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT", keyLSPDaemonTimeout, cfg.Provenance[keyLSPDaemonTimeout])
	}
}

func TestLoadConfig_MalformedTOML(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), "[embedding\nmodel = \"broken\"\n")

	_, err := LoadConfig(projectDir)
	if err == nil {
		t.Fatal("LoadConfig error = nil, want malformed TOML error")
	}
	if !strings.Contains(err.Error(), "parse .codesight/config.toml") {
		t.Fatalf("LoadConfig error = %v, want parse .codesight/config.toml context", err)
	}
}

func TestLoadConfig_MissingFileIgnored(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	expectDefaults(t, cfg)
}

func TestLoadConfig_Provenance(t *testing.T) {
	homeDir := setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(homeDir, ".codesight", "config.toml"), `
[embedding]
model = "user-model"

[index]
warm_lsp = true
`)
	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[db]
address = "project:19530"
`)
	t.Setenv("CODESIGHT_EMBEDDING_MODEL", "env-model")
	t.Setenv("CODESIGHT_DB_TYPE", "envdb")

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if len(cfg.Provenance) != len(allConfigKeys()) {
		t.Fatalf("len(Provenance) = %d, want %d", len(cfg.Provenance), len(allConfigKeys()))
	}
	if cfg.Provenance[keyDBType] != "CODESIGHT_DB_TYPE" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_DB_TYPE", keyDBType, cfg.Provenance[keyDBType])
	}
	if cfg.Provenance[keyDBAddress] != projectConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyDBAddress, cfg.Provenance[keyDBAddress], projectConfigSource)
	}
	if cfg.Provenance[keyEmbeddingModel] != "CODESIGHT_EMBEDDING_MODEL" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_EMBEDDING_MODEL", keyEmbeddingModel, cfg.Provenance[keyEmbeddingModel])
	}
	if cfg.Provenance[keyIndexWarmLSP] != userConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyIndexWarmLSP, cfg.Provenance[keyIndexWarmLSP], userConfigSource)
	}
	if cfg.Provenance[keyStateDir] != "default" {
		t.Fatalf("Provenance[%q] = %q, want default", keyStateDir, cfg.Provenance[keyStateDir])
	}
}

func TestLoadConfig_PartialOverride(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[embedding]
model = "project-model"
`)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Embedding.Model != "project-model" {
		t.Fatalf("Embedding.Model = %q, want project-model", cfg.Embedding.Model)
	}
	if cfg.Embedding.OllamaHost != "http://127.0.0.1:11434" {
		t.Fatalf("Embedding.OllamaHost = %q, want default", cfg.Embedding.OllamaHost)
	}
	if cfg.Embedding.MaxInputChars != 0 {
		t.Fatalf("Embedding.MaxInputChars = %d, want default 0", cfg.Embedding.MaxInputChars)
	}
}

func TestLoadConfig_MaxInputCharsEnvParsing(t *testing.T) {
	t.Run("valid integer", func(t *testing.T) {
		_ = setHomeDir(t)
		projectDir := t.TempDir()
		clearConfigEnv(t)

		writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[embedding]
max_input_chars = 1024
`)
		t.Setenv("CODESIGHT_OLLAMA_MAX_INPUT_CHARS", "2048")

		cfg, err := LoadConfig(projectDir)
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Embedding.MaxInputChars != 2048 {
			t.Fatalf("Embedding.MaxInputChars = %d, want env value 2048", cfg.Embedding.MaxInputChars)
		}
		if cfg.Provenance[keyEmbeddingMaxInput] != "CODESIGHT_OLLAMA_MAX_INPUT_CHARS" {
			t.Fatalf("Provenance[%q] = %q, want CODESIGHT_OLLAMA_MAX_INPUT_CHARS", keyEmbeddingMaxInput, cfg.Provenance[keyEmbeddingMaxInput])
		}
	})

	t.Run("invalid integer keeps previous", func(t *testing.T) {
		_ = setHomeDir(t)
		projectDir := t.TempDir()
		clearConfigEnv(t)

		writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[embedding]
max_input_chars = 1024
`)
		t.Setenv("CODESIGHT_OLLAMA_MAX_INPUT_CHARS", "not-an-int")

		warnings := captureWarnings(t)

		cfg, err := LoadConfig(projectDir)
		if err != nil {
			t.Fatalf("LoadConfig returned error: %v", err)
		}
		if cfg.Embedding.MaxInputChars != 1024 {
			t.Fatalf("Embedding.MaxInputChars = %d, want preserved value 1024", cfg.Embedding.MaxInputChars)
		}
		if cfg.Provenance[keyEmbeddingMaxInput] != projectConfigSource {
			t.Fatalf("Provenance[%q] = %q, want %q", keyEmbeddingMaxInput, cfg.Provenance[keyEmbeddingMaxInput], projectConfigSource)
		}
		if !strings.Contains(warnings.String(), "invalid CODESIGHT_OLLAMA_MAX_INPUT_CHARS value") {
			t.Fatalf("warnings = %q, want invalid max input chars warning", warnings.String())
		}
	})
}

func TestLoadConfig_UnknownKeysWarning(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[unknown_section]
foo = "bar"

[embedding]
mystery = 1
`)

	warnings := captureWarnings(t)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Embedding.Model != "bge-m3" {
		t.Fatalf("Embedding.Model = %q, want default", cfg.Embedding.Model)
	}
	warningText := warnings.String()
	if !strings.Contains(warningText, `unknown top-level section "unknown_section"`) {
		t.Fatalf("warnings = %q, want unknown top-level section warning", warningText)
	}
	if !strings.Contains(warningText, `unknown config key "embedding.mystery"`) {
		t.Fatalf("warnings = %q, want unknown key warning", warningText)
	}
}

func TestLoadConfig_InvalidJavaTimeout(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[lsp.java]
timeout = "fast"
`)

	_, err := LoadConfig(projectDir)
	if err == nil {
		t.Fatal("LoadConfig error = nil, want timeout parse error")
	}
	if !strings.Contains(err.Error(), `invalid lsp.java.timeout "fast"`) {
		t.Fatalf("LoadConfig error = %v, want invalid lsp.java.timeout", err)
	}
}

func TestLoadConfig_DaemonIdleTimeoutPrecedence(t *testing.T) {
	homeDir := setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(homeDir, ".codesight", "config.toml"), `
[lsp.daemon]
idle_timeout = "2m"
`)
	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[lsp.daemon]
idle_timeout = "30s"
`)
	t.Setenv("CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT", "15s")

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.LSP.Daemon.IdleTimeout != "15s" {
		t.Fatalf("LSP.Daemon.IdleTimeout = %q, want env override", cfg.LSP.Daemon.IdleTimeout)
	}
	if cfg.Provenance[keyLSPDaemonTimeout] != "CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT", keyLSPDaemonTimeout, cfg.Provenance[keyLSPDaemonTimeout])
	}
}

func TestLoadConfig_InvalidDaemonIdleTimeout(t *testing.T) {
	t.Run("invalid project value", func(t *testing.T) {
		_ = setHomeDir(t)
		projectDir := t.TempDir()
		clearConfigEnv(t)

		writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[lsp.daemon]
idle_timeout = "soon"
`)

		_, err := LoadConfig(projectDir)
		if err == nil {
			t.Fatal("LoadConfig error = nil, want timeout parse error")
		}
		if !strings.Contains(err.Error(), `invalid lsp.daemon.idle_timeout`) {
			t.Fatalf("LoadConfig error = %v, want invalid lsp.daemon.idle_timeout", err)
		}
	})

	t.Run("invalid env value", func(t *testing.T) {
		_ = setHomeDir(t)
		projectDir := t.TempDir()
		clearConfigEnv(t)
		t.Setenv("CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT", "0")

		_, err := LoadConfig(projectDir)
		if err == nil {
			t.Fatal("LoadConfig error = nil, want timeout parse error")
		}
		if !strings.Contains(err.Error(), `invalid lsp.daemon.idle_timeout`) {
			t.Fatalf("LoadConfig error = %v, want invalid lsp.daemon.idle_timeout", err)
		}
	})
}

func TestResolvedProjectRoot_EmptyValueReturnsParentOfConfigDir(t *testing.T) {
	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, ".codesight")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := Defaults()
	got, err := cfg.ResolvedProjectRoot(configDir)
	if err != nil {
		t.Fatalf("ResolvedProjectRoot returned error: %v", err)
	}
	want, _ := filepath.Abs(projectDir)
	if got != want {
		t.Fatalf("ResolvedProjectRoot() = %q, want %q", got, want)
	}
}

func TestResolvedProjectRoot_RelativePath(t *testing.T) {
	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, ".codesight")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := Defaults()
	cfg.ProjectRoot = ".."
	cfg.Provenance[keyProjectRoot] = projectConfigSource

	got, err := cfg.ResolvedProjectRoot(configDir)
	if err != nil {
		t.Fatalf("ResolvedProjectRoot returned error: %v", err)
	}
	want, _ := filepath.Abs(projectDir)
	if got != want {
		t.Fatalf("ResolvedProjectRoot() = %q, want %q", got, want)
	}
}

func TestResolvedProjectRoot_InvalidPathReturnsError(t *testing.T) {
	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, ".codesight")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := Defaults()
	cfg.ProjectRoot = "../nonexistent-dir-12345"
	cfg.Provenance[keyProjectRoot] = projectConfigSource

	_, err := cfg.ResolvedProjectRoot(configDir)
	if err == nil {
		t.Fatal("ResolvedProjectRoot error = nil, want error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("ResolvedProjectRoot error = %v, want 'does not exist'", err)
	}
}

func TestResolvedProjectRoot_NoConfigDirReturnsError(t *testing.T) {
	cfg := Defaults()
	_, err := cfg.ResolvedProjectRoot("")
	if err == nil {
		t.Fatal("ResolvedProjectRoot error = nil, want error for empty configDir")
	}
}

func TestLoadConfig_SetsConfigDir(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[embedding]
model = "test"
`)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	wantConfigDir, err := filepath.Abs(filepath.Join(projectDir, ".codesight"))
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	if resolvedWantConfigDir, resolveErr := filepath.EvalSymlinks(wantConfigDir); resolveErr == nil {
		wantConfigDir = resolvedWantConfigDir
	}
	if cfg.ConfigDir != wantConfigDir {
		t.Fatalf("ConfigDir = %q, want %q", cfg.ConfigDir, wantConfigDir)
	}
}

func TestLoadConfig_NoProjectConfigLeavesConfigDirEmpty(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.ConfigDir != "" {
		t.Fatalf("ConfigDir = %q, want empty", cfg.ConfigDir)
	}
}

func TestLoadConfig_ProjectRootFromConfig(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	writeFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
project_root = ".."
`)

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.ProjectRoot != ".." {
		t.Fatalf("ProjectRoot = %q, want %q", cfg.ProjectRoot, "..")
	}
	if cfg.Provenance[keyProjectRoot] != projectConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyProjectRoot, cfg.Provenance[keyProjectRoot], projectConfigSource)
	}
}

func TestLoadConfig_ProjectRootFromEnv(t *testing.T) {
	_ = setHomeDir(t)
	projectDir := t.TempDir()
	clearConfigEnv(t)

	t.Setenv("CODESIGHT_PROJECT_ROOT", "/tmp/override-root")

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.ProjectRoot != "/tmp/override-root" {
		t.Fatalf("ProjectRoot = %q, want %q", cfg.ProjectRoot, "/tmp/override-root")
	}
	if cfg.Provenance[keyProjectRoot] != "CODESIGHT_PROJECT_ROOT" {
		t.Fatalf("Provenance[%q] = %q, want CODESIGHT_PROJECT_ROOT", keyProjectRoot, cfg.Provenance[keyProjectRoot])
	}
}

func expectDefaults(t *testing.T, cfg *Config) {
	t.Helper()
	defaults := Defaults()

	if cfg.DB != defaults.DB {
		t.Fatalf("DB = %#v, want %#v", cfg.DB, defaults.DB)
	}
	if cfg.Embedding != defaults.Embedding {
		t.Fatalf("Embedding = %#v, want %#v", cfg.Embedding, defaults.Embedding)
	}
	if cfg.StateDir != defaults.StateDir {
		t.Fatalf("StateDir = %q, want %q", cfg.StateDir, defaults.StateDir)
	}
	if cfg.LSP.Java.GradleJavaHome != defaults.LSP.Java.GradleJavaHome {
		t.Fatalf("LSP.Java.GradleJavaHome = %q, want %q", cfg.LSP.Java.GradleJavaHome, defaults.LSP.Java.GradleJavaHome)
	}
	if cfg.LSP.Java.Timeout != defaults.LSP.Java.Timeout {
		t.Fatalf("LSP.Java.Timeout = %q, want %q", cfg.LSP.Java.Timeout, defaults.LSP.Java.Timeout)
	}
	if len(cfg.LSP.Java.Args) != len(defaults.LSP.Java.Args) {
		t.Fatalf("len(LSP.Java.Args) = %d, want %d", len(cfg.LSP.Java.Args), len(defaults.LSP.Java.Args))
	}
	if len(cfg.LSP.Go.BuildFlags) != len(defaults.LSP.Go.BuildFlags) {
		t.Fatalf("len(LSP.Go.BuildFlags) = %d, want %d", len(cfg.LSP.Go.BuildFlags), len(defaults.LSP.Go.BuildFlags))
	}
	if cfg.LSP.Daemon.IdleTimeout != defaults.LSP.Daemon.IdleTimeout {
		t.Fatalf("LSP.Daemon.IdleTimeout = %q, want %q", cfg.LSP.Daemon.IdleTimeout, defaults.LSP.Daemon.IdleTimeout)
	}
	if cfg.Index != defaults.Index {
		t.Fatalf("Index = %#v, want %#v", cfg.Index, defaults.Index)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()

	envKeys := []string{
		envDBType,
		envDBAddress,
		envDBToken,
		envOllamaHost,
		envEmbeddingModel,
		envMaxInputChars,
		envStateDir,
		envGradleJavaHome,
		envLSPDaemonTimeout,
		envProjectRoot,
	}

	previousValues := map[string]*string{}
	for _, key := range envKeys {
		if value, ok := os.LookupEnv(key); ok {
			copyValue := value
			previousValues[key] = &copyValue
		} else {
			previousValues[key] = nil
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%q): %v", key, err)
		}
	}

	t.Cleanup(func() {
		for _, key := range envKeys {
			value := previousValues[key]
			if value == nil {
				_ = os.Unsetenv(key)
				continue
			}
			_ = os.Setenv(key, *value)
		}
	})
}

func setHomeDir(t *testing.T) string {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	return homeDir
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	previous := warningWriter
	warningWriter = buf
	t.Cleanup(func() {
		warningWriter = previous
	})
	return buf
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
