package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// StateDirEnvVar controls where lifecycle state is persisted.
	StateDirEnvVar = "CODESIGHT_STATE_DIR"
	// DefaultStateDirName is used under the user home when StateDirEnvVar is unset.
	DefaultStateDirName = ".codesight"
	// DefaultIdleTimeout is the default daemon idle timeout.
	DefaultIdleTimeout = 10 * time.Minute

	lspStateSubdir = "lsp"
)

// Lease describes the active language-server process selected by Ensure.
type Lease struct {
	WorkspaceRoot string
	Language      string
	StateKey      string
	PID           int
	Binary        string
	Args          []string
	Reused        bool
}

// Lifecycle manages language-server process reuse and lifecycle state.
type Lifecycle struct {
	registry *Registry

	idleTimeout      time.Duration
	stateDirResolver func() (string, error)

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex

	// shutdownIdleBeforeLockHook is a test hook used to coordinate overlap tests.
	shutdownIdleBeforeLockHook func(statePath string)
}

// LifecycleOption customizes lifecycle behavior.
type LifecycleOption func(*Lifecycle)

// WithIdleTimeout overrides the lifecycle idle timeout.
func WithIdleTimeout(timeout time.Duration) LifecycleOption {
	return func(l *Lifecycle) {
		if timeout > 0 {
			l.idleTimeout = timeout
		}
	}
}

// WithStateDirResolver overrides state-dir resolution, mainly for tests.
func WithStateDirResolver(resolver func() (string, error)) LifecycleOption {
	return func(l *Lifecycle) {
		if resolver != nil {
			l.stateDirResolver = resolver
		}
	}
}

// NewLifecycle builds a process lifecycle manager.
func NewLifecycle(registry *Registry, opts ...LifecycleOption) *Lifecycle {
	if registry == nil {
		registry = NewRegistry()
	}

	lifecycle := &Lifecycle{
		registry:         registry,
		idleTimeout:      DefaultIdleTimeout,
		stateDirResolver: ResolveStateDir,
		locks:            make(map[string]*sync.Mutex),
	}

	for _, opt := range opts {
		opt(lifecycle)
	}

	return lifecycle
}

// ResolveStateDir returns the root state directory for lifecycle metadata.
func ResolveStateDir() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(StateDirEnvVar)); explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", StateDirEnvVar, err)
		}
		return filepath.Clean(abs), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, DefaultStateDirName), nil
}

// StateKey returns the deterministic lifecycle key for a workspace/language tuple.
func StateKey(workspaceRoot, language string) string {
	normalizedRoot := filepath.Clean(strings.TrimSpace(workspaceRoot))
	normalizedLanguage := normalizeLanguage(language)

	sum := sha256.Sum256([]byte(normalizedRoot + "\x00" + normalizedLanguage))
	return hex.EncodeToString(sum[:])
}

// Ensure reuses an active server for (workspace, language) or starts a new one.
func (l *Lifecycle) Ensure(ctx context.Context, workspaceRoot, language string) (Lease, error) {
	if l == nil {
		return Lease{}, errors.New("lifecycle is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}

	canonicalRoot, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return Lease{}, err
	}

	spec, err := l.registry.Lookup(language)
	if err != nil {
		return Lease{}, err
	}

	stateKey := StateKey(canonicalRoot, spec.Language)
	lock := l.lockForKey(stateKey)
	lock.Lock()
	defer lock.Unlock()

	statePath, err := l.statePathForKey(stateKey)
	if err != nil {
		return Lease{}, err
	}

	now := time.Now()
	state, stateErr := readStateFile(statePath)
	if stateErr == nil {
		if l.isIdle(state, now) {
			_ = killProcess(state.PID)
			_ = os.Remove(statePath)
		} else if processAlive(state.PID) {
			state.LastUsedUnixNano = now.UnixNano()
			if err := writeStateFile(statePath, state); err != nil {
				return Lease{}, err
			}
			return leaseFromState(state, true), nil
		} else {
			_ = os.Remove(statePath)
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		_ = os.Remove(statePath)
	}

	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}

	cmd := exec.Command(spec.Binary, spec.Args...)
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Lease{}, fmt.Errorf("LSP required but %s not found. Install: %s", spec.Binary, spec.InstallHint)
		}
		return Lease{}, fmt.Errorf("start language server %s: %w", spec.Binary, err)
	}

	go func() {
		_ = cmd.Wait()
	}()

	state = lifecycleState{
		WorkspaceRoot:    canonicalRoot,
		Language:         spec.Language,
		StateKey:         stateKey,
		PID:              cmd.Process.Pid,
		Binary:           spec.Binary,
		Args:             append([]string(nil), spec.Args...),
		StartedUnixNano:  now.UnixNano(),
		LastUsedUnixNano: now.UnixNano(),
	}

	if err := writeStateFile(statePath, state); err != nil {
		_ = killProcess(state.PID)
		return Lease{}, err
	}

	return leaseFromState(state, false), nil
}

// Stop terminates and removes lifecycle state for one workspace/language tuple.
func (l *Lifecycle) Stop(workspaceRoot, language string) error {
	canonicalRoot, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return err
	}
	stateKey := StateKey(canonicalRoot, language)

	lock := l.lockForKey(stateKey)
	lock.Lock()
	defer lock.Unlock()

	statePath, err := l.statePathForKey(stateKey)
	if err != nil {
		return err
	}

	state, err := readStateFile(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		_ = os.Remove(statePath)
		return nil
	}

	if err := killProcess(state.PID); err != nil {
		return err
	}
	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ShutdownIdle stops servers whose state indicates they exceeded idle timeout.
func (l *Lifecycle) ShutdownIdle(ctx context.Context) error {
	if l == nil {
		return errors.New("lifecycle is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	stateRoot, err := l.stateRoot()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	now := time.Now()
	var shutdownErrs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		statePath := filepath.Join(stateRoot, entry.Name())
		if l.shutdownIdleBeforeLockHook != nil {
			l.shutdownIdleBeforeLockHook(statePath)
		}

		stateKey := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		lock := l.lockForKey(stateKey)
		lock.Lock()
		state, err := readStateFile(statePath)
		if err != nil {
			_ = os.Remove(statePath)
			lock.Unlock()
			continue
		}
		if !l.isIdle(state, now) {
			lock.Unlock()
			continue
		}
		if err := killProcess(state.PID); err != nil {
			shutdownErrs = append(shutdownErrs, err)
		}
		if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			shutdownErrs = append(shutdownErrs, err)
		}
		lock.Unlock()
	}

	return errors.Join(shutdownErrs...)
}

func (l *Lifecycle) isIdle(state lifecycleState, now time.Time) bool {
	if l.idleTimeout <= 0 {
		return false
	}

	var referenceUnixNano int64
	if state.LastUsedUnixNano != 0 {
		referenceUnixNano = state.LastUsedUnixNano
	} else if state.StartedUnixNano != 0 {
		referenceUnixNano = state.StartedUnixNano
	} else {
		return false
	}

	lastUsed := time.Unix(0, referenceUnixNano)
	return now.Sub(lastUsed) >= l.idleTimeout
}

func (l *Lifecycle) stateRoot() (string, error) {
	if l.stateDirResolver == nil {
		return "", errors.New("state dir resolver is nil")
	}

	stateDir, err := l.stateDirResolver()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(stateDir) == "" {
		return "", errors.New("resolved state directory is empty")
	}

	root := filepath.Join(stateDir, lspStateSubdir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create state directory: %w", err)
	}
	return root, nil
}

func (l *Lifecycle) statePathForKey(stateKey string) (string, error) {
	root, err := l.stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, stateKey+".json"), nil
}

func (l *Lifecycle) lockForKey(stateKey string) *sync.Mutex {
	l.locksMu.Lock()
	defer l.locksMu.Unlock()

	lock, ok := l.locks[stateKey]
	if ok {
		return lock
	}
	lock = &sync.Mutex{}
	l.locks[stateKey] = lock
	return lock
}

type lifecycleState struct {
	WorkspaceRoot    string   `json:"workspace_root"`
	Language         string   `json:"language"`
	StateKey         string   `json:"state_key"`
	PID              int      `json:"pid"`
	Binary           string   `json:"binary"`
	Args             []string `json:"args"`
	StartedUnixNano  int64    `json:"started_unix_nano"`
	LastUsedUnixNano int64    `json:"last_used_unix_nano"`
}

func readStateFile(path string) (lifecycleState, error) {
	var state lifecycleState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return lifecycleState{}, err
	}
	if state.StateKey == "" {
		state.StateKey = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return state, nil
}

func writeStateFile(path string, state lifecycleState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal lifecycle state: %w", err)
	}

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return fmt.Errorf("write lifecycle state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename lifecycle state: %w", err)
	}
	return nil
}

func leaseFromState(state lifecycleState, reused bool) Lease {
	return Lease{
		WorkspaceRoot: state.WorkspaceRoot,
		Language:      state.Language,
		StateKey:      state.StateKey,
		PID:           state.PID,
		Binary:        state.Binary,
		Args:          append([]string(nil), state.Args...),
		Reused:        reused,
	}
}

func canonicalWorkspaceRoot(workspaceRoot string) (string, error) {
	trimmed := strings.TrimSpace(workspaceRoot)
	if trimmed == "" {
		return "", errors.New("workspace root is required")
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return filepath.Clean(abs), nil
}

func killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := process.Kill(); err != nil && !isNoSuchProcess(err) && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) || isNoSuchProcess(err) {
		return false
	}

	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.EPERM {
		return true
	}

	return false
}

func isNoSuchProcess(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.ESRCH
}
