package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	lifecycleStateFileExtension  = ".json"
	lifecycleSocketFileExtension = ".sock"
	javaGradleBaselineExtension  = ".java-gradle.json"
)

type JavaGradleBuildBaseline struct {
	Fingerprint string                `json:"fingerprint"`
	Files       []JavaGradleBuildFile `json:"files,omitempty"`
}

type JavaGradleBuildFile struct {
	Path            string `json:"path"`
	Exists          bool   `json:"exists"`
	ModTimeUnixNano int64  `json:"mod_time_unix_nano,omitempty"`
	SizeBytes       int64  `json:"size_bytes,omitempty"`
	ContentSHA256   string `json:"content_sha256,omitempty"`
}

type lifecycleState struct {
	WorkspaceRoot           string                   `json:"workspace_root"`
	Language                string                   `json:"language"`
	StateKey                string                   `json:"state_key"`
	SocketPath              string                   `json:"socket_path,omitempty"`
	PID                     int                      `json:"pid"`
	Binary                  string                   `json:"binary"`
	Args                    []string                 `json:"args"`
	DaemonProcessStartID    string                   `json:"daemon_process_start_id,omitempty"`
	StartedUnixNano         int64                    `json:"started_unix_nano"`
	LastUsedUnixNano        int64                    `json:"last_used_unix_nano"`
	JavaGradleBuildBaseline *JavaGradleBuildBaseline `json:"java_gradle_build_baseline,omitempty"`
}

func readStateFile(path string) (lifecycleState, error) {
	var state lifecycleState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return lifecycleState{}, fmt.Errorf("unmarshal lifecycle state: %w", err)
	}
	state.StateKey = normalizeStateKey(path, state.StateKey)
	state.SocketPath = normalizeSocketPath(path, state.StateKey)
	state.DaemonProcessStartID = strings.TrimSpace(state.DaemonProcessStartID)
	if state.JavaGradleBuildBaseline != nil {
		normalized := normalizeJavaGradleBuildBaseline(*state.JavaGradleBuildBaseline)
		state.JavaGradleBuildBaseline = &normalized
	}
	return state, nil
}

func writeStateFile(path string, state lifecycleState) error {
	state.StateKey = normalizeStateKey(path, state.StateKey)
	state.SocketPath = normalizeSocketPath(path, state.StateKey)
	state.DaemonProcessStartID = strings.TrimSpace(state.DaemonProcessStartID)
	if state.JavaGradleBuildBaseline != nil {
		normalized := normalizeJavaGradleBuildBaseline(*state.JavaGradleBuildBaseline)
		state.JavaGradleBuildBaseline = &normalized
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal lifecycle state: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write lifecycle state: %w", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write lifecycle state: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write lifecycle state: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("write lifecycle state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename lifecycle state: %w", err)
	}
	return nil
}

func socketPathForState(statePath, stateKey string) string {
	key := strings.TrimSpace(stateKey)
	if key == "" {
		key = stateKeyFromStatePath(statePath)
	}
	return filepath.Join(filepath.Dir(statePath), key+lifecycleSocketFileExtension)
}

func normalizeSocketPath(statePath, stateKey string) string {
	return socketPathForState(statePath, stateKey)
}

func normalizeStateKey(statePath, persistedStateKey string) string {
	stateKey := strings.TrimSpace(stateKeyFromStatePath(statePath))
	if stateKey != "" {
		return stateKey
	}
	return strings.TrimSpace(persistedStateKey)
}

func stateKeyFromStatePath(path string) string {
	filename := filepath.Base(path)
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func ReadJavaGradleBuildBaseline(workspaceRoot string) (JavaGradleBuildBaseline, error) {
	statePath, baselinePath, err := javaGradleBaselinePaths(workspaceRoot)
	if err != nil {
		return JavaGradleBuildBaseline{}, err
	}

	state, err := readStateFile(statePath)
	if err == nil && state.JavaGradleBuildBaseline != nil {
		return normalizeJavaGradleBuildBaseline(*state.JavaGradleBuildBaseline), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return JavaGradleBuildBaseline{}, fmt.Errorf("read lifecycle state: %w", err)
	}

	data, err := os.ReadFile(baselinePath)
	if err != nil {
		return JavaGradleBuildBaseline{}, err
	}

	var baseline JavaGradleBuildBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return JavaGradleBuildBaseline{}, fmt.Errorf("unmarshal java gradle build baseline: %w", err)
	}
	return normalizeJavaGradleBuildBaseline(baseline), nil
}

func WriteJavaGradleBuildBaseline(workspaceRoot string, baseline JavaGradleBuildBaseline) error {
	statePath, baselinePath, err := javaGradleBaselinePaths(workspaceRoot)
	if err != nil {
		return err
	}

	normalized := normalizeJavaGradleBuildBaseline(baseline)
	if normalized.Fingerprint == "" {
		return errors.New("java gradle build baseline fingerprint is required")
	}

	payload, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal java gradle build baseline: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(baselinePath), filepath.Base(baselinePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write java gradle build baseline: %w", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write java gradle build baseline: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write java gradle build baseline: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("write java gradle build baseline: %w", err)
	}
	if err := os.Rename(tempPath, baselinePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename java gradle build baseline: %w", err)
	}

	state, err := readStateFile(statePath)
	if err == nil {
		state.JavaGradleBuildBaseline = &normalized
		if err := writeStateFile(statePath, state); err != nil {
			return fmt.Errorf("update lifecycle state baseline: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read lifecycle state: %w", err)
	}

	return nil
}

func javaGradleBaselinePaths(workspaceRoot string) (string, string, error) {
	canonicalRoot, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return "", "", err
	}

	stateDir, err := ResolveStateDir()
	if err != nil {
		return "", "", err
	}
	stateRoot := filepath.Join(stateDir, lspStateSubdir)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return "", "", fmt.Errorf("create state directory: %w", err)
	}

	stateKey := StateKey(canonicalRoot, "java")
	statePath := filepath.Join(stateRoot, stateKey+lifecycleStateFileExtension)
	baselinePath := filepath.Join(stateRoot, stateKey+javaGradleBaselineExtension)
	return statePath, baselinePath, nil
}

func normalizeJavaGradleBuildBaseline(baseline JavaGradleBuildBaseline) JavaGradleBuildBaseline {
	normalized := JavaGradleBuildBaseline{
		Fingerprint: strings.TrimSpace(baseline.Fingerprint),
	}
	if len(baseline.Files) == 0 {
		return normalized
	}

	files := make([]JavaGradleBuildFile, 0, len(baseline.Files))
	for _, file := range baseline.Files {
		files = append(files, JavaGradleBuildFile{
			Path:            strings.TrimSpace(file.Path),
			Exists:          file.Exists,
			ModTimeUnixNano: file.ModTimeUnixNano,
			SizeBytes:       file.SizeBytes,
			ContentSHA256:   strings.TrimSpace(file.ContentSHA256),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	normalized.Files = files
	return normalized
}
