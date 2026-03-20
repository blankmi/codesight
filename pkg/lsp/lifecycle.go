package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	SocketPath    string
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
// The key is truncated to 16 hex characters to keep Unix domain socket paths
// under the 104-byte limit imposed by macOS (struct sockaddr_un.sun_path).
func StateKey(workspaceRoot, language string) string {
	normalizedRoot := filepath.Clean(strings.TrimSpace(workspaceRoot))
	normalizedLanguage := normalizeLanguage(language)

	sum := sha256.Sum256([]byte(normalizedRoot + "\x00" + normalizedLanguage))
	return hex.EncodeToString(sum[:8])
}

// DaemonStatus describes the observed state of a single LSP daemon.
type DaemonStatus struct {
	WorkspaceRoot string
	Language      string
	StateKey      string
	PID           int
	Binary        string
	Running       bool
	SocketHealthy bool
	StartedAt     time.Time
	LastUsedAt    time.Time
	LogPath       string
}

// Status returns daemon status for all languages in a workspace.
// It scans all state files and filters by canonical workspace root.
func (l *Lifecycle) Status(ctx context.Context, workspaceRoot string) ([]DaemonStatus, error) {
	if l == nil {
		return nil, errors.New("lifecycle is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	canonicalRoot, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}

	stateRoot, err := l.stateRoot()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var statuses []DaemonStatus
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != lifecycleStateFileExtension {
			continue
		}

		statePath := filepath.Join(stateRoot, entry.Name())
		state, err := readStateFile(statePath)
		if err != nil {
			continue
		}

		if state.WorkspaceRoot != canonicalRoot {
			continue
		}

		alive := processAlive(state.PID)
		running := alive
		if hasStrongProcessIdentity(state) {
			running = processMatchesState(state)
		}

		var socketHealthy bool
		if running {
			healthCtx, cancel := context.WithTimeout(ctx, defaultDaemonShutdownTimeout)
			socketHealthy = daemonSocketHealthy(healthCtx, state.SocketPath) == nil
			cancel()
		}

		logPath := strings.TrimSuffix(statePath, filepath.Ext(statePath)) + ".log"

		ds := DaemonStatus{
			WorkspaceRoot: state.WorkspaceRoot,
			Language:      state.Language,
			StateKey:      state.StateKey,
			PID:           state.PID,
			Binary:        state.Binary,
			Running:       running,
			SocketHealthy: socketHealthy,
			LogPath:       logPath,
		}
		if state.StartedUnixNano != 0 {
			ds.StartedAt = time.Unix(0, state.StartedUnixNano)
		}
		if state.LastUsedUnixNano != 0 {
			ds.LastUsedAt = time.Unix(0, state.LastUsedUnixNano)
		}

		statuses = append(statuses, ds)
	}

	return statuses, nil
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
	socketPath, err := l.socketPathForKey(stateKey)
	if err != nil {
		return Lease{}, err
	}

	now := time.Now()
	state, stateErr := readStateFile(statePath)
	if stateErr == nil {
		if l.isIdle(state, now) {
			_ = l.stopStateProcess(statePath, state)
		} else {
			if hasStrongProcessIdentity(state) {
				if processMatchesState(state) {
					socketHealthCtx, cancel := context.WithTimeout(ctx, defaultDaemonShutdownTimeout)
					socketErr := daemonSocketHealthy(socketHealthCtx, state.SocketPath)
					cancel()
					if socketErr == nil {
						state.LastUsedUnixNano = now.UnixNano()
						if err := writeStateFile(statePath, state); err != nil {
							return Lease{}, err
						}
						return leaseFromState(state, true), nil
					}
					_ = l.stopStateProcess(statePath, state)
				} else {
					_ = removeStateArtifacts(statePath, socketPathForState(statePath, state.StateKey))
				}
			} else if processAlive(state.PID) {
				socketHealthCtx, cancel := context.WithTimeout(ctx, defaultDaemonShutdownTimeout)
				socketErr := daemonSocketHealthy(socketHealthCtx, state.SocketPath)
				cancel()
				if socketErr == nil {
					state.LastUsedUnixNano = now.UnixNano()
					if err := refreshProcessStartID(&state); err != nil {
						return Lease{}, err
					}
					if err := writeStateFile(statePath, state); err != nil {
						return Lease{}, err
					}
					return leaseFromState(state, true), nil
				}
				_ = removeStateArtifacts(statePath, socketPathForState(statePath, state.StateKey))
			} else {
				_ = removeStateArtifacts(statePath, socketPathForState(statePath, state.StateKey))
			}
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		_ = removeStateArtifacts(statePath, socketPath)
	}

	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}

	if _, err := exec.LookPath(spec.Binary); err != nil {
		return Lease{}, fmt.Errorf("LSP required but %s not found. Install: %s", spec.Binary, spec.InstallHint)
	}

	args := append([]string(nil), spec.Args...)
	if strings.TrimSpace(strings.ToLower(spec.Binary)) == "jdtls" {
		dataDir := filepath.Join(filepath.Dir(statePath), "jdtls-data-"+stateKey)
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return Lease{}, fmt.Errorf("create jdtls data directory: %w", err)
		}
		args = append(args, "-data", dataDir)
	}

	daemonConfig := daemonProcessConfig{
		WorkspaceRoot: canonicalRoot,
		Language:      spec.Language,
		StateKey:      stateKey,
		StatePath:     statePath,
		SocketPath:    socketPath,
		Binary:        spec.Binary,
		Args:          args,
		IdleTimeoutNS: int64(l.idleTimeout),
	}
	pid, daemonProcessStartID, err := launchDaemonProcess(ctx, daemonConfig)
	if err != nil {
		return Lease{}, fmt.Errorf("start lsp daemon: %w", err)
	}

	state = lifecycleState{
		WorkspaceRoot:        canonicalRoot,
		Language:             spec.Language,
		StateKey:             stateKey,
		SocketPath:           socketPath,
		PID:                  pid,
		Binary:               spec.Binary,
		Args:                 append([]string(nil), spec.Args...),
		DaemonProcessStartID: strings.TrimSpace(daemonProcessStartID),
		StartedUnixNano:      now.UnixNano(),
		LastUsedUnixNano:     now.UnixNano(),
	}

	if err := writeStateFile(statePath, state); err != nil {
		_ = killProcessGroup(state.PID)
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
		_ = removeStateArtifacts(statePath, socketPathForState(statePath, stateKey))
		return nil
	}

	return l.stopStateProcess(statePath, state)
}

// StopByKey terminates and removes lifecycle state for an exact state key.
// Use this when the state key may differ from what StateKey() computes
// (e.g. legacy state files with full-length hashes).
func (l *Lifecycle) StopByKey(stateKey string) error {
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
		_ = removeStateArtifacts(statePath, socketPathForState(statePath, stateKey))
		return nil
	}

	return l.stopStateProcess(statePath, state)
}

// Cleanup scans the state directory and removes artifacts for daemons that are
// no longer running. This includes state files, logs, and socket files that
// were not cleaned up due to abrupt crashes or manual process termination.
func (l *Lifecycle) Cleanup(ctx context.Context) ([]string, error) {
	if l == nil {
		return nil, errors.New("lifecycle is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = ctx // Avoid ineffassign if ctx is otherwise unused in future iterations

	stateRoot, err := l.stateRoot()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cleaned []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != lifecycleStateFileExtension {
			continue
		}

		statePath := filepath.Join(stateRoot, entry.Name())
		state, err := readStateFile(statePath)
		if err != nil {
			// If we can't read the state file, it's corrupt; clean it up.
			_ = removeStateArtifacts(statePath, "")
			cleaned = append(cleaned, entry.Name())
			continue
		}

		alive := processAlive(state.PID)
		running := alive
		if hasStrongProcessIdentity(state) {
			running = processMatchesState(state)
		}

		if !running {
			// Daemon is dead; clean up its artifacts.
			socketPath := socketPathForState(statePath, state.StateKey)
			if err := removeStateArtifacts(statePath, socketPath); err == nil {
				cleaned = append(cleaned, entry.Name())
			}
		}
	}

	return cleaned, nil
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
		if entry.IsDir() || filepath.Ext(entry.Name()) != lifecycleStateFileExtension {
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
			_ = removeStateArtifacts(statePath, socketPathForState(statePath, stateKey))
			lock.Unlock()
			continue
		}
		if !l.isIdle(state, now) {
			lock.Unlock()
			continue
		}
		if err := l.stopStateProcess(statePath, state); err != nil {
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
	return filepath.Join(root, stateKey+lifecycleStateFileExtension), nil
}

func (l *Lifecycle) socketPathForKey(stateKey string) (string, error) {
	statePath, err := l.statePathForKey(stateKey)
	if err != nil {
		return "", err
	}
	return socketPathForState(statePath, stateKey), nil
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

func leaseFromState(state lifecycleState, reused bool) Lease {
	return Lease{
		WorkspaceRoot: state.WorkspaceRoot,
		Language:      state.Language,
		StateKey:      state.StateKey,
		SocketPath:    state.SocketPath,
		PID:           state.PID,
		Binary:        state.Binary,
		Args:          append([]string(nil), state.Args...),
		Reused:        reused,
	}
}

func (l *Lifecycle) stopStateProcess(statePath string, state lifecycleState) error {
	socketPath := socketPathForState(statePath, state.StateKey)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultDaemonShutdownTimeout)
	_ = shutdownDaemonViaSocket(shutdownCtx, socketPath)
	cancel()

	var stopErrs []error
	if hasStrongProcessIdentity(state) && processAlive(state.PID) && processMatchesState(state) {
		if err := killProcessGroup(state.PID); err != nil {
			stopErrs = append(stopErrs, err)
		}
	}
	if err := removeStateArtifacts(statePath, socketPath); err != nil {
		stopErrs = append(stopErrs, err)
	}
	return errors.Join(stopErrs...)
}

func removeStateArtifacts(statePath, socketPath string) error {
	var removeErrs []error
	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		removeErrs = append(removeErrs, err)
	}
	if strings.TrimSpace(socketPath) != "" {
		if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			removeErrs = append(removeErrs, err)
		}
	}
	artifactBase := strings.TrimSuffix(statePath, lifecycleStateFileExtension)
	logPath := artifactBase + daemonLogFileExtension
	if err := os.Remove(logPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		removeErrs = append(removeErrs, err)
	}
	// Remove the Gradle baseline file so that a restart triggers a fresh
	// Gradle import instead of suppressing it based on stale state.
	baselinePath := artifactBase + javaGradleBaselineExtension
	if err := os.Remove(baselinePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		removeErrs = append(removeErrs, err)
	}
	return errors.Join(removeErrs...)
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
	if pid <= 1 || pid == os.Getpid() {
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

func killProcessGroup(pid int) error {
	if pid <= 1 || pid == os.Getpid() {
		return nil
	}

	// Never kill our own process group.
	if pgrp := syscall.Getpgrp(); pgrp == pid {
		return nil
	}

	// Kill the entire process group (-pid) so child processes (e.g. jdtls)
	// are also terminated. The daemon is started with Setsid, making it the
	// process group leader. Falling back to single-process kill if the
	// group kill fails (e.g. process already exited).
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !isNoSuchProcess(err) {
		return killProcess(pid)
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
