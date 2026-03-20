package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	userConfigSource              = "~/.codesight/config.toml"
	projectConfigSource           = ".codesight/config.toml"
	maxProjectConfigSearchParents = 12
)

const (
	envDBType                = "CODESIGHT_DB_TYPE"
	envDBAddress             = "CODESIGHT_DB_ADDRESS"
	envDBToken               = "CODESIGHT_DB_TOKEN"
	envOllamaHost            = "CODESIGHT_OLLAMA_HOST"
	envEmbeddingModel        = "CODESIGHT_EMBEDDING_MODEL"
	envMaxInputChars         = "CODESIGHT_OLLAMA_MAX_INPUT_CHARS"
	envStateDir              = "CODESIGHT_STATE_DIR"
	envGradleJavaHome        = "CODESIGHT_GRADLE_JAVA_HOME"
	envLSPDaemonTimeout      = "CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT"
	envLSPWarmupProbeTimeout = "CODESIGHT_LSP_DAEMON_WARMUP_PROBE_TIMEOUT"
	envProjectRoot           = "CODESIGHT_PROJECT_ROOT"
	defaultJavaTimeout       = "60s"
	defaultDaemonTimeout     = "10m"
	defaultDBType            = "milvus"
	defaultDBAddress         = "localhost:19530"
	defaultOllamaHost        = "http://127.0.0.1:11434"
	defaultEmbeddingModel    = "bge-m3"
)

const (
	keyDBType                = "db.type"
	keyDBAddress             = "db.address"
	keyDBToken               = "db.token"
	keyEmbeddingOllama       = "embedding.ollama_host"
	keyEmbeddingModel        = "embedding.model"
	keyEmbeddingMaxInput     = "embedding.max_input_chars"
	keyStateDir              = "state_dir"
	keyLSPJavaGradleHome     = "lsp.java.gradle_java_home"
	keyLSPJavaTimeout        = "lsp.java.timeout"
	keyLSPJavaArgs           = "lsp.java.args"
	keyLSPGoBuildFlags       = "lsp.go.build_flags"
	keyLSPDaemonTimeout      = "lsp.daemon.idle_timeout"
	keyLSPWarmupProbeTimeout = "lsp.daemon.warmup_probe_timeout"
	keyIndexWarmLSP          = "index.warm_lsp"
	keyProjectRoot           = "project_root"
)

var warningWriter io.Writer = os.Stderr

type Config struct {
	DB          DBConfig          `toml:"db"`
	Embedding   EmbeddingConfig   `toml:"embedding"`
	StateDir    string            `toml:"state_dir"`
	LSP         LSPConfig         `toml:"lsp"`
	Index       IndexConfig       `toml:"index"`
	ProjectRoot string            `toml:"project_root"`
	ConfigDir   string            `toml:"-"`
	Provenance  map[string]string `toml:"-"`
}

type DBConfig struct {
	Type    string `toml:"type"`
	Address string `toml:"address"`
	Token   string `toml:"token"`
}

type EmbeddingConfig struct {
	OllamaHost    string `toml:"ollama_host"`
	Model         string `toml:"model"`
	MaxInputChars int    `toml:"max_input_chars"`
}

type LSPConfig struct {
	Java   JavaLSPConfig   `toml:"java"`
	Go     GoLSPConfig     `toml:"go"`
	Daemon DaemonLSPConfig `toml:"daemon"`
}

type JavaLSPConfig struct {
	GradleJavaHome string   `toml:"gradle_java_home"`
	Timeout        string   `toml:"timeout"`
	Args           []string `toml:"args"`
}

type GoLSPConfig struct {
	BuildFlags []string `toml:"build_flags"`
}

type DaemonLSPConfig struct {
	IdleTimeout        string `toml:"idle_timeout"`
	WarmupProbeTimeout string `toml:"warmup_probe_timeout"`
}

type IndexConfig struct {
	WarmLSP bool `toml:"warm_lsp"`
}

type layerConfig struct {
	DB          layerDBConfig        `toml:"db"`
	Embedding   layerEmbeddingConfig `toml:"embedding"`
	StateDir    *string              `toml:"state_dir"`
	LSP         layerLSPConfig       `toml:"lsp"`
	Index       layerIndexConfig     `toml:"index"`
	ProjectRoot *string              `toml:"project_root"`
}

type layerDBConfig struct {
	Type    *string `toml:"type"`
	Address *string `toml:"address"`
	Token   *string `toml:"token"`
}

type layerEmbeddingConfig struct {
	OllamaHost    *string `toml:"ollama_host"`
	Model         *string `toml:"model"`
	MaxInputChars *int    `toml:"max_input_chars"`
}

type layerLSPConfig struct {
	Java   layerJavaLSPConfig   `toml:"java"`
	Go     layerGoLSPConfig     `toml:"go"`
	Daemon layerDaemonLSPConfig `toml:"daemon"`
}

type layerJavaLSPConfig struct {
	GradleJavaHome *string   `toml:"gradle_java_home"`
	Timeout        *string   `toml:"timeout"`
	Args           *[]string `toml:"args"`
}

type layerGoLSPConfig struct {
	BuildFlags *[]string `toml:"build_flags"`
}

type layerDaemonLSPConfig struct {
	IdleTimeout        *string `toml:"idle_timeout"`
	WarmupProbeTimeout *string `toml:"warmup_probe_timeout"`
}

type layerIndexConfig struct {
	WarmLSP *bool `toml:"warm_lsp"`
}

func Defaults() *Config {
	cfg := &Config{
		DB: DBConfig{
			Type:    defaultDBType,
			Address: defaultDBAddress,
			Token:   "",
		},
		Embedding: EmbeddingConfig{
			OllamaHost:    defaultOllamaHost,
			Model:         defaultEmbeddingModel,
			MaxInputChars: 0,
		},
		StateDir: "",
		LSP: LSPConfig{
			Java: JavaLSPConfig{
				GradleJavaHome: "",
				Timeout:        defaultJavaTimeout,
				Args:           []string{},
			},
			Go: GoLSPConfig{
				BuildFlags: []string{},
			},
			Daemon: DaemonLSPConfig{
				IdleTimeout: defaultDaemonTimeout,
			},
		},
		Index: IndexConfig{
			WarmLSP: false,
		},
		Provenance: map[string]string{},
	}

	for _, key := range allConfigKeys() {
		cfg.Provenance[key] = "default"
	}

	return cfg
}

func LoadConfig(projectPath string) (*Config, error) {
	cfg := Defaults()

	userConfigPath, err := userConfigPath()
	if err != nil {
		return nil, err
	}
	if err := applyConfigFile(cfg, userConfigPath, userConfigSource); err != nil {
		return nil, err
	}

	projConfigPath, err := projectConfigPath(projectPath)
	if err != nil {
		return nil, err
	}
	if projConfigPath != "" {
		if err := applyConfigFile(cfg, projConfigPath, projectConfigSource); err != nil {
			return nil, err
		}
		cfg.ConfigDir = filepath.Dir(projConfigPath)
	}

	if err := applyEnv(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func userConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(homeDir, ".codesight", "config.toml"), nil
}

func projectConfigPath(projectPath string) (string, error) {
	startDir, err := resolveProjectConfigStartDir(projectPath)
	if err != nil {
		return "", err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	} else if absHome, absErr := filepath.Abs(homeDir); absErr == nil {
		homeDir = filepath.Clean(absHome)
		if resolvedHome, resolveErr := filepath.EvalSymlinks(homeDir); resolveErr == nil {
			homeDir = filepath.Clean(resolvedHome)
		}
	} else {
		homeDir = ""
	}

	userCfgPath, err := userConfigPath()
	if err != nil {
		return "", err
	}
	if absUserCfgPath, absErr := filepath.Abs(userCfgPath); absErr == nil {
		userCfgPath = filepath.Clean(absUserCfgPath)
		if resolvedUserCfgPath, resolveErr := filepath.EvalSymlinks(userCfgPath); resolveErr == nil {
			userCfgPath = filepath.Clean(resolvedUserCfgPath)
		}
	}

	startWithinHome := homeDir != "" && pathWithinRoot(startDir, homeDir)

	current := startDir
	for parents := 0; ; parents++ {
		candidate := filepath.Join(current, ".codesight", "config.toml")
		if !sameCleanPath(candidate, userCfgPath) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}

		gitPath := filepath.Join(current, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return "", nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		if startWithinHome && sameCleanPath(current, homeDir) {
			return "", nil
		}
		if !startWithinHome && parents >= maxProjectConfigSearchParents {
			return "", nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}

func resolveProjectConfigStartDir(projectPath string) (string, error) {
	target := strings.TrimSpace(projectPath)
	if target == "" {
		target = "."
	}

	absProjectPath, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	resolvedProjectPath, err := filepath.EvalSymlinks(absProjectPath)
	if err == nil {
		absProjectPath = resolvedProjectPath
	}

	info, err := os.Stat(absProjectPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		absProjectPath = filepath.Dir(absProjectPath)
	}

	return filepath.Clean(absProjectPath), nil
}

func sameCleanPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func pathWithinRoot(path, root string) bool {
	if sameCleanPath(path, root) {
		return true
	}
	return strings.HasPrefix(filepath.Clean(path), filepath.Clean(root)+string(filepath.Separator))
}

func applyConfigFile(cfg *Config, path string, source string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", source, err)
	}

	var layer layerConfig
	metadata, err := toml.DecodeFile(path, &layer)
	if err != nil {
		return fmt.Errorf("parse %s: %w", source, err)
	}

	warnUnknownKeys(metadata, source)

	if err := mergeLayer(cfg, layer, source); err != nil {
		return fmt.Errorf("apply %s: %w", source, err)
	}

	return nil
}

func warnUnknownKeys(metadata toml.MetaData, source string) {
	knownTopLevel := map[string]struct{}{
		"db":           {},
		"embedding":    {},
		"state_dir":    {},
		"lsp":          {},
		"index":        {},
		"project_root": {},
	}

	seenTopLevelWarnings := map[string]struct{}{}
	seenKeyWarnings := map[string]struct{}{}

	for _, key := range metadata.Undecoded() {
		parts := append([]string(nil), key...)
		if len(parts) == 0 {
			continue
		}

		topLevel := parts[0]
		if _, ok := knownTopLevel[topLevel]; !ok {
			if _, seen := seenTopLevelWarnings[topLevel]; !seen {
				warnf("warning: unknown top-level section %q in %s\n", topLevel, source)
				seenTopLevelWarnings[topLevel] = struct{}{}
			}
			continue
		}

		dotted := strings.Join(parts, ".")
		if _, seen := seenKeyWarnings[dotted]; seen {
			continue
		}
		warnf("warning: unknown config key %q in %s\n", dotted, source)
		seenKeyWarnings[dotted] = struct{}{}
	}
}

func mergeLayer(cfg *Config, layer layerConfig, source string) error {
	if layer.DB.Type != nil {
		cfg.DB.Type = *layer.DB.Type
		cfg.Provenance[keyDBType] = source
	}
	if layer.DB.Address != nil {
		cfg.DB.Address = *layer.DB.Address
		cfg.Provenance[keyDBAddress] = source
	}
	if layer.DB.Token != nil {
		cfg.DB.Token = *layer.DB.Token
		cfg.Provenance[keyDBToken] = source
	}

	if layer.Embedding.OllamaHost != nil {
		cfg.Embedding.OllamaHost = *layer.Embedding.OllamaHost
		cfg.Provenance[keyEmbeddingOllama] = source
	}
	if layer.Embedding.Model != nil {
		cfg.Embedding.Model = *layer.Embedding.Model
		cfg.Provenance[keyEmbeddingModel] = source
	}
	if layer.Embedding.MaxInputChars != nil {
		cfg.Embedding.MaxInputChars = *layer.Embedding.MaxInputChars
		cfg.Provenance[keyEmbeddingMaxInput] = source
	}

	if layer.StateDir != nil {
		cfg.StateDir = *layer.StateDir
		cfg.Provenance[keyStateDir] = source
	}

	if layer.LSP.Java.GradleJavaHome != nil {
		cfg.LSP.Java.GradleJavaHome = *layer.LSP.Java.GradleJavaHome
		cfg.Provenance[keyLSPJavaGradleHome] = source
	}
	if layer.LSP.Java.Timeout != nil {
		if _, err := time.ParseDuration(*layer.LSP.Java.Timeout); err != nil {
			return fmt.Errorf("invalid lsp.java.timeout %q: %w", *layer.LSP.Java.Timeout, err)
		}
		cfg.LSP.Java.Timeout = *layer.LSP.Java.Timeout
		cfg.Provenance[keyLSPJavaTimeout] = source
	}
	if layer.LSP.Java.Args != nil {
		cfg.LSP.Java.Args = cloneStrings(*layer.LSP.Java.Args)
		cfg.Provenance[keyLSPJavaArgs] = source
	}
	if layer.LSP.Go.BuildFlags != nil {
		cfg.LSP.Go.BuildFlags = cloneStrings(*layer.LSP.Go.BuildFlags)
		cfg.Provenance[keyLSPGoBuildFlags] = source
	}
	if layer.LSP.Daemon.IdleTimeout != nil {
		if _, err := parsePositiveDuration(*layer.LSP.Daemon.IdleTimeout, keyLSPDaemonTimeout); err != nil {
			return err
		}
		cfg.LSP.Daemon.IdleTimeout = strings.TrimSpace(*layer.LSP.Daemon.IdleTimeout)
		cfg.Provenance[keyLSPDaemonTimeout] = source
	}

	if layer.LSP.Daemon.WarmupProbeTimeout != nil {
		if _, err := parsePositiveDuration(*layer.LSP.Daemon.WarmupProbeTimeout, keyLSPWarmupProbeTimeout); err != nil {
			return err
		}
		cfg.LSP.Daemon.WarmupProbeTimeout = strings.TrimSpace(*layer.LSP.Daemon.WarmupProbeTimeout)
		cfg.Provenance[keyLSPWarmupProbeTimeout] = source
	}

	if layer.Index.WarmLSP != nil {
		cfg.Index.WarmLSP = *layer.Index.WarmLSP
		cfg.Provenance[keyIndexWarmLSP] = source
	}

	if layer.ProjectRoot != nil {
		cfg.ProjectRoot = *layer.ProjectRoot
		cfg.Provenance[keyProjectRoot] = source
	}

	return nil
}

func applyEnv(cfg *Config) error {
	if value, ok := os.LookupEnv(envDBType); ok && value != "" {
		cfg.DB.Type = value
		cfg.Provenance[keyDBType] = envDBType
	}
	if value, ok := os.LookupEnv(envDBAddress); ok && value != "" {
		cfg.DB.Address = value
		cfg.Provenance[keyDBAddress] = envDBAddress
	}
	if value, ok := os.LookupEnv(envDBToken); ok {
		cfg.DB.Token = value
		cfg.Provenance[keyDBToken] = envDBToken
	}

	if value, ok := os.LookupEnv(envOllamaHost); ok && value != "" {
		cfg.Embedding.OllamaHost = value
		cfg.Provenance[keyEmbeddingOllama] = envOllamaHost
	}
	if value, ok := os.LookupEnv(envEmbeddingModel); ok && value != "" {
		cfg.Embedding.Model = value
		cfg.Provenance[keyEmbeddingModel] = envEmbeddingModel
	}
	if raw, ok := os.LookupEnv(envMaxInputChars); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			parsed, err := strconv.Atoi(trimmed)
			if err != nil || parsed < 0 {
				warnf("warning: invalid %s value %q; keeping previous value\n", envMaxInputChars, raw)
			} else {
				cfg.Embedding.MaxInputChars = parsed
				cfg.Provenance[keyEmbeddingMaxInput] = envMaxInputChars
			}
		}
	}

	if value, ok := os.LookupEnv(envStateDir); ok {
		cfg.StateDir = value
		cfg.Provenance[keyStateDir] = envStateDir
	}
	if value, ok := os.LookupEnv(envGradleJavaHome); ok {
		cfg.LSP.Java.GradleJavaHome = value
		cfg.Provenance[keyLSPJavaGradleHome] = envGradleJavaHome
	}
	if raw, ok := os.LookupEnv(envLSPDaemonTimeout); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			if _, err := parsePositiveDuration(trimmed, keyLSPDaemonTimeout); err != nil {
				return err
			}
			cfg.LSP.Daemon.IdleTimeout = trimmed
			cfg.Provenance[keyLSPDaemonTimeout] = envLSPDaemonTimeout
		}
	}

	if raw, ok := os.LookupEnv(envLSPWarmupProbeTimeout); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			if _, err := parsePositiveDuration(trimmed, keyLSPWarmupProbeTimeout); err != nil {
				return err
			}
			cfg.LSP.Daemon.WarmupProbeTimeout = trimmed
			cfg.Provenance[keyLSPWarmupProbeTimeout] = envLSPWarmupProbeTimeout
		}
	}

	if value, ok := os.LookupEnv(envProjectRoot); ok && value != "" {
		cfg.ProjectRoot = value
		cfg.Provenance[keyProjectRoot] = envProjectRoot
	}

	return nil
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func allConfigKeys() []string {
	return []string{
		keyDBType,
		keyDBAddress,
		keyDBToken,
		keyEmbeddingOllama,
		keyEmbeddingModel,
		keyEmbeddingMaxInput,
		keyStateDir,
		keyLSPJavaGradleHome,
		keyLSPJavaTimeout,
		keyLSPJavaArgs,
		keyLSPGoBuildFlags,
		keyLSPDaemonTimeout,
		keyLSPWarmupProbeTimeout,
		keyIndexWarmLSP,
		keyProjectRoot,
	}
}

func (cfg *Config) LSPDaemonIdleTimeoutDuration() (time.Duration, error) {
	if cfg == nil {
		cfg = Defaults()
	}
	return parsePositiveDuration(cfg.LSP.Daemon.IdleTimeout, keyLSPDaemonTimeout)
}

func (cfg *Config) LSPWarmupProbeTimeoutDuration() (time.Duration, error) {
	if cfg == nil || cfg.LSP.Daemon.WarmupProbeTimeout == "" {
		return 0, nil
	}
	return parsePositiveDuration(cfg.LSP.Daemon.WarmupProbeTimeout, keyLSPWarmupProbeTimeout)
}

func parsePositiveDuration(raw string, key string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, raw, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be > 0", key, raw)
	}
	return parsed, nil
}

func (cfg *Config) ResolvedProjectRoot(configDir string) (string, error) {
	raw := cfg.ProjectRoot
	if raw == "" {
		if configDir == "" {
			return "", fmt.Errorf("no project root configured and no .codesight directory found")
		}
		resolved, err := filepath.Abs(filepath.Dir(configDir))
		if err != nil {
			return "", fmt.Errorf("resolve default project root: %w", err)
		}
		return resolved, nil
	}

	// Env var values are already absolute.
	if cfg.Provenance[keyProjectRoot] == envProjectRoot {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return "", fmt.Errorf("resolve project root: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("project root %q does not exist: %w", abs, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("project root %q is not a directory", abs)
		}
		return abs, nil
	}

	if configDir == "" {
		return "", fmt.Errorf("project_root is set but no .codesight directory found to resolve it against")
	}
	abs, err := filepath.Abs(filepath.Join(configDir, raw))
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project root %q does not exist: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root %q is not a directory", abs)
	}
	return abs, nil
}

func warnf(format string, args ...any) {
	_, _ = fmt.Fprintf(warningWriter, format, args...)
}
