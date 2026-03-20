//go:build linux || darwin

package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleEnsureBackfillsLegacyProcessStartID(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	statePath, err := lifecycle.statePathForKey(lease.StateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	original, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	if original.DaemonProcessStartID == "" {
		t.Fatal("new daemon state should persist a daemon process start ID")
	}

	legacyState := original
	legacyState.DaemonProcessStartID = ""
	if err := writeStateFile(statePath, legacyState); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	reused, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure for legacy state returned error: %v", err)
	}
	if !reused.Reused {
		t.Fatal("legacy healthy daemon state should be reused")
	}
	if reused.PID != lease.PID {
		t.Fatalf("reused PID = %d, want %d", reused.PID, lease.PID)
	}

	updated, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile after backfill returned error: %v", err)
	}
	if updated.DaemonProcessStartID != original.DaemonProcessStartID {
		t.Fatalf("daemon process start ID after backfill = %q, want %q", updated.DaemonProcessStartID, original.DaemonProcessStartID)
	}
}

func TestLifecycleStopDoesNotKillLegacyLiveUnrelatedProcess(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}

	helper := startBackgroundLifecycleHelper(t)
	stateKey := StateKey(workspaceAbs, "go")
	statePath, err := lifecycle.statePathForKey(stateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	state := lifecycleState{
		WorkspaceRoot:    workspaceAbs,
		Language:         "go",
		StateKey:         stateKey,
		PID:              helper.Process.Pid,
		Binary:           "other-tool",
		StartedUnixNano:  time.Now().Add(-time.Hour).UnixNano(),
		LastUsedUnixNano: time.Now().Add(-time.Minute).UnixNano(),
	}
	if err := writeStateFile(statePath, state); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	if err := lifecycle.Stop(workspace, "go"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	assertProcessStillAlive(t, helper.Process.Pid)

	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state file to be removed, got err: %v", err)
	}
}

func TestLifecycleShutdownIdleDoesNotKillLegacyLiveUnrelatedProcess(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}

	helper := startBackgroundLifecycleHelper(t)
	stateKey := StateKey(workspaceAbs, "go")
	statePath, err := lifecycle.statePathForKey(stateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	state := lifecycleState{
		WorkspaceRoot:    workspaceAbs,
		Language:         "go",
		StateKey:         stateKey,
		PID:              helper.Process.Pid,
		Binary:           "other-tool",
		StartedUnixNano:  time.Now().Add(-time.Hour).UnixNano(),
		LastUsedUnixNano: time.Now().Add(-time.Hour).UnixNano(),
	}
	if err := writeStateFile(statePath, state); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	if err := lifecycle.ShutdownIdle(context.Background()); err != nil {
		t.Fatalf("ShutdownIdle returned error: %v", err)
	}
	assertProcessStillAlive(t, helper.Process.Pid)

	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state file to be removed, got err: %v", err)
	}
}

func TestLifecycleStopDoesNotKillMismatchedStrongIdentityProcess(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)
	workspace := t.TempDir()

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}

	helper := startBackgroundLifecycleHelper(t)
	stateKey := StateKey(workspaceAbs, "go")
	statePath, err := lifecycle.statePathForKey(stateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	state := lifecycleState{
		WorkspaceRoot:        workspaceAbs,
		Language:             "go",
		StateKey:             stateKey,
		PID:                  helper.Process.Pid,
		Binary:               "other-tool",
		DaemonProcessStartID: "mismatched-start-id",
		StartedUnixNano:      time.Now().Add(-time.Hour).UnixNano(),
		LastUsedUnixNano:     time.Now().Add(-time.Minute).UnixNano(),
	}
	if err := writeStateFile(statePath, state); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}

	if err := lifecycle.Stop(workspace, "go"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	assertProcessStillAlive(t, helper.Process.Pid)

	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state file to be removed, got err: %v", err)
	}
}

func startBackgroundLifecycleHelper(t *testing.T) *exec.Cmd {
	t.Helper()

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable returned error: %v", err)
	}

	cmd := exec.Command(testBinary, "-test.run=TestLifecycleHelperProcess", "--", lifecycleHelperArg)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	if !waitForProcessAlive(cmd.Process.Pid, time.Second) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper process %d did not become alive", cmd.Process.Pid)
	}

	t.Cleanup(func() {
		_ = killProcess(cmd.Process.Pid)
		_ = cmd.Wait()
	})

	return cmd
}

func assertProcessStillAlive(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			t.Fatalf("process %d exited unexpectedly", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !processAlive(pid) {
		t.Fatalf("process %d exited unexpectedly", pid)
	}
}
