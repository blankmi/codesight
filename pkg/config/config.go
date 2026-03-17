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
	userConfigSource    = "~/.codesight/config.toml"
	projectConfigSource = ".codesight/config.toml"
)

const (
	envDBType             = "CODESIGHT_DB_TYPE"
	envDBAddress          = "CODESIGHT_DB_ADDRESS"
	envDBToken            = "CODESIGHT_DB_TOKEN"
	envOllamaHost         = "CODESIGHT_OLLAMA_HOST"
	envEmbeddingModel     = "CODESIGHT_EMBEDDING_MODEL"
	envMaxInputChars      = "CODESIGHT_OLLAMA_MAX_INPUT_CHARS"
	envStateDir           = "CODESIGHT_STATE_DIR"
	envGradleJavaHome     = "CODESIGHT_GRADLE_JAVA_HOME"
	defaultJavaTimeout    = "60s"
	defaultDBType         = "milvus"
	defaultDBAddress      = "localhost:19530"
	defaultOllamaHost     = "http://127.0.0.1:11434"
	defaultEmbeddingModel = "nomic-embed-text"
)

const (
	keyDBType            = "db.type"
	keyDBAddress         = "db.address"
	keyDBToken           = "db.token"
	keyEmbeddingOllama   = "embedding.ollama_host"
	keyEmbeddingModel    = "embedding.model"
	keyEmbeddingMaxInput = "embedding.max_input_chars"
	keyStateDir          = "state_dir"
	keyLSPJavaGradleHome = "lsp.java.gradle_java_home"
	keyLSPJavaTimeout    = "lsp.java.timeout"
	keyLSPJavaArgs       = "lsp.java.args"
	keyLSPGoBuildFlags   = "lsp.go.build_flags"
	keyIndexWarmLSP      = "index.warm_lsp"
)

var warningWriter io.Writer = os.Stderr

type Config struct {
	DB         DBConfig          `toml:"db"`
	Embedding  EmbeddingConfig   `toml:"embedding"`
	StateDir   string            `toml:"state_dir"`
	LSP        LSPConfig         `toml:"lsp"`
	Index      IndexConfig       `toml:"index"`
	Provenance map[string]string `toml:"-"`
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
	Java JavaLSPConfig `toml:"java"`
	Go   GoLSPConfig   `toml:"go"`
}

type JavaLSPConfig struct {
	GradleJavaHome string   `toml:"gradle_java_home"`
	Timeout        string   `toml:"timeout"`
	Args           []string `toml:"args"`
}

type GoLSPConfig struct {
	BuildFlags []string `toml:"build_flags"`
}

type IndexConfig struct {
	WarmLSP bool `toml:"warm_lsp"`
}

type layerConfig struct {
	DB        layerDBConfig        `toml:"db"`
	Embedding layerEmbeddingConfig `toml:"embedding"`
	StateDir  *string              `toml:"state_dir"`
	LSP       layerLSPConfig       `toml:"lsp"`
	Index     layerIndexConfig     `toml:"index"`
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
	Java layerJavaLSPConfig `toml:"java"`
	Go   layerGoLSPConfig   `toml:"go"`
}

type layerJavaLSPConfig struct {
	GradleJavaHome *string   `toml:"gradle_java_home"`
	Timeout        *string   `toml:"timeout"`
	Args           *[]string `toml:"args"`
}

type layerGoLSPConfig struct {
	BuildFlags *[]string `toml:"build_flags"`
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

	projectConfigPath, err := projectConfigPath(projectPath)
	if err != nil {
		return nil, err
	}
	if err := applyConfigFile(cfg, projectConfigPath, projectConfigSource); err != nil {
		return nil, err
	}

	applyEnv(cfg)

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
	if projectPath == "" {
		projectPath = "."
	}
	absProjectPath, err := filepath.Abs(projectPath)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	return filepath.Join(absProjectPath, ".codesight", "config.toml"), nil
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
		"db":        {},
		"embedding": {},
		"state_dir": {},
		"lsp":       {},
		"index":     {},
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

	if layer.Index.WarmLSP != nil {
		cfg.Index.WarmLSP = *layer.Index.WarmLSP
		cfg.Provenance[keyIndexWarmLSP] = source
	}

	return nil
}

func applyEnv(cfg *Config) {
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
		keyIndexWarmLSP,
	}
}

func warnf(format string, args ...any) {
	_, _ = fmt.Fprintf(warningWriter, format, args...)
}
