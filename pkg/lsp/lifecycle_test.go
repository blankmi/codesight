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

	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("expected lifecycle state file: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("state file permissions = %#o, expected user-only permissions", info.Mode().Perm())
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

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})
}

func TestLifecycleDeferredRefsCommandIntegration(t *testing.T) {
	t.Skip("blocked by TK-006: refs command integration not wired")
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
	stateDir := t.TempDir()

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

func TestLifecycleStopMissingStateIsNoop(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	err := lifecycle.Stop(workspace, "go")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stop returned unexpected error: %v", err)
	}
}
