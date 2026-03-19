//go:build !windows

package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const lifecycleHelperArg = "codesight-lsp-helper-process"

func TestResolveStateDirUsesEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "state")
	t.Setenv(StateDirEnvVar, override)

	got, err := ResolveStateDir()
	if err != nil {
		t.Fatalf("ResolveStateDir returned error: %v", err)
	}

	want, err := filepath.Abs(override)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("ResolveStateDir() = %q, want %q", got, filepath.Clean(want))
	}
}

func TestResolveStateDirDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(StateDirEnvVar, "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := ResolveStateDir()
	if err != nil {
		t.Fatalf("ResolveStateDir returned error: %v", err)
	}

	want := filepath.Join(home, DefaultStateDirName)
	if got != want {
		t.Fatalf("ResolveStateDir() = %q, want %q", got, want)
	}
}

func TestStateKeyScopedByWorkspaceAndLanguage(t *testing.T) {
	a := StateKey("/repo/a", "go")
	aUpper := StateKey("/repo/a", "GO")
	b := StateKey("/repo/a", "python")
	c := StateKey("/repo/b", "go")

	if a != aUpper {
		t.Fatalf("state key must normalize language case, got %q and %q", a, aUpper)
	}
	if a == b {
		t.Fatalf("state keys should differ by language, both were %q", a)
	}
	if a == c {
		t.Fatalf("state keys should differ by workspace, both were %q", a)
	}
}

func TestLifecycleFirstStartCreatesProcess(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if lease.Reused {
		t.Fatal("first Ensure call should not mark lease as reused")
	}
	if lease.PID <= 0 {
		t.Fatalf("lease PID = %d, expected positive PID", lease.PID)
	}
	if !waitForProcessAlive(lease.PID, time.Second) {
		t.Fatalf("process %d did not become alive", lease.PID)
	}

	statePath, err := lifecycle.statePathForKey(lease.StateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}
	socketPath, err := lifecycle.socketPathForKey(lease.StateKey)
	if err != nil {
		t.Fatalf("socketPathForKey returned error: %v", err)
	}
	if lease.SocketPath != socketPath {
		t.Fatalf("lease socket path = %q, want %q", lease.SocketPath, socketPath)
	}

	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("expected lifecycle state file: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("state file permissions = %#o, expected user-only permissions", info.Mode().Perm())
	}
	onDisk, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	if onDisk.SocketPath != socketPath {
		t.Fatalf("state socket path = %q, want %q", onDisk.SocketPath, socketPath)
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleReusesLiveProcess(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	first, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("first Ensure returned error: %v", err)
	}
	second, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("second Ensure returned error: %v", err)
	}

	if second.Reused != true {
		t.Fatal("second Ensure should mark lease as reused")
	}
	if second.PID != first.PID {
		t.Fatalf("second PID = %d, want reused PID %d", second.PID, first.PID)
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleConcurrentEnsureIsSafe(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	var wg sync.WaitGroup
	leases := make([]Lease, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			leases[index], errs[index] = lifecycle.Ensure(context.Background(), workspace, "go")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Ensure call %d returned error: %v", i, err)
		}
	}
	if leases[0].PID != leases[1].PID {
		t.Fatalf("concurrent Ensure calls started different PIDs: %d and %d", leases[0].PID, leases[1].PID)
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleIdleTimeoutShutdownPath(t *testing.T) {
	idleTimeout := 75 * time.Millisecond
	lifecycle := newTestLifecycle(t, idleTimeout)
	workspace := t.TempDir()

	first, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !waitForProcessAlive(first.PID, time.Second) {
		t.Fatalf("process %d did not become alive", first.PID)
	}

	time.Sleep(idleTimeout + 40*time.Millisecond)
	if err := lifecycle.ShutdownIdle(context.Background()); err != nil {
		t.Fatalf("ShutdownIdle returned error: %v", err)
	}
	if !waitForProcessExit(first.PID, time.Second) {
		t.Fatalf("idle process %d did not exit", first.PID)
	}

	second, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure after ShutdownIdle returned error: %v", err)
	}
	if second.Reused {
		t.Fatal("Ensure after idle shutdown should start a new process")
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleEnsureShutdownIdleOverlapDoesNotKillReusedProcess(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	first, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("initial Ensure returned error: %v", err)
	}
	if !waitForProcessAlive(first.PID, time.Second) {
		t.Fatalf("process %d did not become alive", first.PID)
	}

	statePath, err := lifecycle.statePathForKey(first.StateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	paused := make(chan struct{})
	resume := make(chan struct{})
	var pauseOnce sync.Once
	lifecycle.shutdownIdleBeforeLockHook = func(path string) {
		if path != statePath {
			return
		}
		pauseOnce.Do(func() {
			close(paused)
		})
		<-resume
	}
	t.Cleanup(func() {
		lifecycle.shutdownIdleBeforeLockHook = nil
		_ = lifecycle.Stop(workspace, "go")
	})

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- lifecycle.ShutdownIdle(context.Background())
	}()

	select {
	case <-paused:
	case <-time.After(time.Second):
		close(resume)
		t.Fatal("ShutdownIdle did not reach overlap hook")
	}

	reused, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		close(resume)
		t.Fatalf("Ensure during overlap returned error: %v", err)
	}
	if !reused.Reused {
		close(resume)
		t.Fatal("Ensure during overlap should reuse existing process")
	}
	if reused.PID != first.PID {
		close(resume)
		t.Fatalf("Ensure reused PID = %d, want %d", reused.PID, first.PID)
	}

	close(resume)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("ShutdownIdle returned error: %v", err)
	}

	if !waitForProcessAlive(reused.PID, time.Second) {
		t.Fatalf("reused process %d was killed during ShutdownIdle overlap", reused.PID)
	}

	onDisk, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	if onDisk.PID != reused.PID {
		t.Fatalf("state PID = %d, want %d", onDisk.PID, reused.PID)
	}
}

func TestLifecycleIsIdleFallsBackToStartedWhenLastUsedMissing(t *testing.T) {
	idleTimeout := 200 * time.Millisecond
	lifecycle := newTestLifecycle(t, idleTimeout)
	now := time.Now()

	idleByStarted := lifecycleState{
		StartedUnixNano:  now.Add(-time.Second).UnixNano(),
		LastUsedUnixNano: 0,
	}
	if !lifecycle.isIdle(idleByStarted, now) {
		t.Fatal("state with missing last-used timestamp should fall back to started timestamp")
	}

	freshByStarted := lifecycleState{
		StartedUnixNano:  now.Add(-50 * time.Millisecond).UnixNano(),
		LastUsedUnixNano: 0,
	}
	if lifecycle.isIdle(freshByStarted, now) {
		t.Fatal("state should not be idle when started timestamp is within idle timeout")
	}

	missingTimestamps := lifecycleState{}
	if lifecycle.isIdle(missingTimestamps, now) {
		t.Fatal("state with no timestamps should not be considered idle")
	}
}

func TestLifecycleRecoversStalePIDState(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}

	stalePID := 999_999
	for processAlive(stalePID) {
		stalePID++
	}

	stateKey := StateKey(workspaceAbs, "go")
	statePath, err := lifecycle.statePathForKey(stateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	staleState := lifecycleState{
		WorkspaceRoot:    workspaceAbs,
		Language:         "go",
		StateKey:         stateKey,
		PID:              stalePID,
		Binary:           "stale-binary",
		Args:             []string{"--stale"},
		StartedUnixNano:  time.Now().Add(-time.Minute).UnixNano(),
		LastUsedUnixNano: time.Now().UnixNano(),
	}
	if err := writeStateFile(statePath, staleState); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if lease.Reused {
		t.Fatal("stale PID state should not be reused")
	}
	if lease.PID == stalePID {
		t.Fatalf("Ensure reused stale PID %d", stalePID)
	}

	onDisk, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	if onDisk.PID != lease.PID {
		t.Fatalf("state PID = %d, want %d", onDisk.PID, lease.PID)
	}
	if onDisk.SocketPath == "" {
		t.Fatal("state socket path should be populated")
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleRecoversCorruptStateFile(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	stateKey := StateKey(workspaceAbs, "go")

	statePath, err := lifecycle.statePathForKey(stateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}
	socketPath, err := lifecycle.socketPathForKey(stateKey)
	if err != nil {
		t.Fatalf("socketPathForKey returned error: %v", err)
	}

	if err := os.WriteFile(statePath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile statePath returned error: %v", err)
	}
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("os.WriteFile socketPath returned error: %v", err)
	}

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if lease.Reused {
		t.Fatal("corrupt state should not be reused")
	}
	if lease.SocketPath != socketPath {
		t.Fatalf("lease socket path = %q, want %q", lease.SocketPath, socketPath)
	}
	if !waitForProcessAlive(lease.PID, time.Second) {
		t.Fatalf("process %d did not become alive", lease.PID)
	}

	socketInfo, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("expected daemon socket to exist after recovery, got err: %v", err)
	}
	if socketInfo.Mode()&os.ModeSocket == 0 {
		t.Fatalf("expected daemon socket path to be a socket, mode = %v", socketInfo.Mode())
	}

	onDisk, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	if onDisk.PID != lease.PID {
		t.Fatalf("state PID = %d, want %d", onDisk.PID, lease.PID)
	}
	if onDisk.SocketPath != socketPath {
		t.Fatalf("state socket path = %q, want %q", onDisk.SocketPath, socketPath)
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleEnsureReuseRefreshesLastUsedTimestamp(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	first, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("first Ensure returned error: %v", err)
	}
	statePath, err := lifecycle.statePathForKey(first.StateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}
	firstState, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile after first Ensure returned error: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	second, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("second Ensure returned error: %v", err)
	}
	if !second.Reused {
		t.Fatal("second Ensure should mark lease as reused")
	}
	if second.PID != first.PID {
		t.Fatalf("second PID = %d, want reused PID %d", second.PID, first.PID)
	}

	secondState, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile after second Ensure returned error: %v", err)
	}
	if secondState.LastUsedUnixNano <= firstState.LastUsedUnixNano {
		t.Fatalf(
			"last used timestamp did not increase on reuse: first=%d second=%d",
			firstState.LastUsedUnixNano,
			secondState.LastUsedUnixNano,
		)
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleHelperProcess(t *testing.T) {
	if !hasArg(lifecycleHelperArg) {
		return
	}

	for {
		time.Sleep(100 * time.Millisecond)
	}
}

func newTestLifecycle(t *testing.T, idleTimeout time.Duration) *Lifecycle {
	t.Helper()

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable returned error: %v", err)
	}
	stateDir, err := os.MkdirTemp("", "cslsp-state-")
	if err != nil {
		t.Fatalf("os.MkdirTemp returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(stateDir)
	})

	registry := NewRegistryFromEntries(map[string]ServerSpec{
		"go": {
			Language:    "go",
			Binary:      testBinary,
			Args:        []string{"-test.run=TestLifecycleHelperProcess", "--", lifecycleHelperArg},
			InstallHint: "test helper process",
		},
	})

	return NewLifecycle(
		registry,
		WithIdleTimeout(idleTimeout),
		WithStateDirResolver(func() (string, error) {
			return stateDir, nil
		}),
	)
}

func waitForProcessAlive(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if processAlive(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return processAlive(pid)
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return !processAlive(pid)
}

func hasArg(target string) bool {
	for _, arg := range os.Args {
		if arg == target {
			return true
		}
	}
	return false
}

func TestLifecycleStatusFindsRunningDaemon(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !waitForProcessAlive(lease.PID, time.Second) {
		t.Fatalf("process %d did not become alive", lease.PID)
	}
	t.Cleanup(func() { _ = lifecycle.Stop(workspace, "go") })

	statuses, err := lifecycle.Status(context.Background(), workspace)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("Status returned %d entries, want 1", len(statuses))
	}

	s := statuses[0]
	if s.PID != lease.PID {
		t.Fatalf("Status PID = %d, want %d", s.PID, lease.PID)
	}
	if s.Language != "go" {
		t.Fatalf("Status Language = %q, want %q", s.Language, "go")
	}
	if !s.Running {
		t.Fatal("Status Running = false, want true")
	}
	if s.StartedAt.IsZero() {
		t.Fatal("Status StartedAt should not be zero")
	}
	if s.LogPath == "" {
		t.Fatal("Status LogPath should not be empty")
	}
}

func TestLifecycleStatusNoMatchWorkspace(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	_, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	t.Cleanup(func() { _ = lifecycle.Stop(workspace, "go") })

	otherWorkspace := t.TempDir()
	statuses, err := lifecycle.Status(context.Background(), otherWorkspace)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("Status returned %d entries for non-matching workspace, want 0", len(statuses))
	}
}

func TestLifecycleStatusDeadPID(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}

	deadPID := 999_999
	for processAlive(deadPID) {
		deadPID++
	}

	stateKey := StateKey(workspaceAbs, "go")
	statePath, err := lifecycle.statePathForKey(stateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	state := lifecycleState{
		WorkspaceRoot:    workspaceAbs,
		Language:         "go",
		StateKey:         stateKey,
		PID:              deadPID,
		Binary:           "test-binary",
		StartedUnixNano:  time.Now().Add(-time.Hour).UnixNano(),
		LastUsedUnixNano: time.Now().Add(-time.Minute).UnixNano(),
	}
	if err := writeStateFile(statePath, state); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	statuses, err := lifecycle.Status(context.Background(), workspace)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("Status returned %d entries, want 1", len(statuses))
	}

	s := statuses[0]
	if s.Running {
		t.Fatal("Status Running = true for dead PID, want false")
	}
	if s.SocketHealthy {
		t.Fatal("Status SocketHealthy = true for dead PID, want false")
	}
	if s.PID != deadPID {
		t.Fatalf("Status PID = %d, want %d", s.PID, deadPID)
	}
}

func TestLifecycleStopByKeyRemovesLegacyStateFile(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}

	// Simulate a legacy state file with a full-length hash key.
	legacyKey := "f4c21a9120853516393401f7e28ae43b116c699e19c83b03e56400d5053f8094"
	statePath, err := lifecycle.statePathForKey(legacyKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	deadPID := 999_999
	for processAlive(deadPID) {
		deadPID++
	}

	state := lifecycleState{
		WorkspaceRoot:    workspaceAbs,
		Language:         "go",
		StateKey:         legacyKey,
		PID:              deadPID,
		Binary:           "gopls",
		StartedUnixNano:  time.Now().Add(-time.Hour).UnixNano(),
		LastUsedUnixNano: time.Now().Add(-time.Minute).UnixNano(),
	}
	if err := writeStateFile(statePath, state); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	// Status should find it.
	statuses, err := lifecycle.Status(context.Background(), workspace)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("Status returned %d entries, want 1", len(statuses))
	}
	if statuses[0].StateKey != legacyKey {
		t.Fatalf("Status StateKey = %q, want %q", statuses[0].StateKey, legacyKey)
	}

	// StopByKey should remove the legacy state file.
	if err := lifecycle.StopByKey(legacyKey); err != nil {
		t.Fatalf("StopByKey returned error: %v", err)
	}

	// State file should be gone.
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state file to be removed, got err: %v", err)
	}

	// Status should now return empty.
	statuses, err = lifecycle.Status(context.Background(), workspace)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("Status returned %d entries after StopByKey, want 0", len(statuses))
	}
}

func TestLifecycleStopMissingStateIsNoop(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	err := lifecycle.Stop(workspace, "go")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stop returned unexpected error: %v", err)
	}
}
