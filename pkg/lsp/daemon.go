package lsp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	daemonModeEnvVar   = "CODESIGHT_LSP_INTERNAL_DAEMON_MODE"
	daemonModeEnvValue = "1"
	daemonConfigEnvVar = "CODESIGHT_LSP_INTERNAL_DAEMON_CONFIG"

	daemonBusyMessage = "lsp daemon busy"

	defaultDaemonShutdownTimeout = 2 * time.Second
)

var ErrDaemonDisabled = errors.New("lsp daemon disabled on windows")

type daemonProcessConfig struct {
	WorkspaceRoot           string                   `json:"workspace_root"`
	Language                string                   `json:"language"`
	StateKey                string                   `json:"state_key"`
	StatePath               string                   `json:"state_path"`
	SocketPath              string                   `json:"socket_path"`
	Binary                  string                   `json:"binary"`
	Args                    []string                 `json:"args"`
	IdleTimeoutNS           int64                    `json:"idle_timeout_ns"`
	JavaGradleBuildBaseline *JavaGradleBuildBaseline `json:"java_gradle_build_baseline,omitempty"`
}

func (c daemonProcessConfig) idleTimeout() time.Duration {
	return time.Duration(c.IdleTimeoutNS)
}

func (c daemonProcessConfig) validate() error {
	if strings.TrimSpace(c.WorkspaceRoot) == "" {
		return errors.New("daemon workspace root is required")
	}
	if strings.TrimSpace(c.Language) == "" {
		return errors.New("daemon language is required")
	}
	if strings.TrimSpace(c.StateKey) == "" {
		return errors.New("daemon state key is required")
	}
	if strings.TrimSpace(c.StatePath) == "" {
		return errors.New("daemon state path is required")
	}
	if strings.TrimSpace(c.SocketPath) == "" {
		return errors.New("daemon socket path is required")
	}
	if strings.TrimSpace(c.Binary) == "" {
		return errors.New("daemon language-server binary is required")
	}
	if c.idleTimeout() <= 0 {
		return fmt.Errorf("daemon idle timeout must be > 0: %s", c.idleTimeout())
	}
	if c.JavaGradleBuildBaseline != nil && strings.TrimSpace(c.JavaGradleBuildBaseline.Fingerprint) == "" {
		return errors.New("daemon java gradle build baseline fingerprint is required")
	}
	return nil
}

func encodeDaemonProcessConfig(config daemonProcessConfig) (string, error) {
	payload, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal daemon config: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func decodeDaemonProcessConfig(raw string) (daemonProcessConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return daemonProcessConfig{}, errors.New("daemon config payload is required")
	}

	payload, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return daemonProcessConfig{}, fmt.Errorf("decode daemon config payload: %w", err)
	}

	var config daemonProcessConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return daemonProcessConfig{}, fmt.Errorf("unmarshal daemon config payload: %w", err)
	}

	if err := config.validate(); err != nil {
		return daemonProcessConfig{}, err
	}
	return config, nil
}

func daemonConfigFromEnv() (daemonProcessConfig, error) {
	encoded := strings.TrimSpace(os.Getenv(daemonConfigEnvVar))
	return decodeDaemonProcessConfig(encoded)
}

func init() {
	if strings.TrimSpace(os.Getenv(daemonModeEnvVar)) != daemonModeEnvValue {
		return
	}

	if err := runDaemonFromEnv(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
