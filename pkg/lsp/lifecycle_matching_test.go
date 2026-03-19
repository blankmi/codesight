//go:build !windows

package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleStatusMatchesExactWorkspaceRootOnly(t *testing.T) {
	lifecycle := newTestLifecycle(t, DefaultIdleTimeout)

	parentWorkspace := t.TempDir()
	childWorkspace := filepath.Join(parentWorkspace, "child")
	if err := os.MkdirAll(childWorkspace, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	parentAbs, err := filepath.Abs(parentWorkspace)
	if err != nil {
		t.Fatalf("filepath.Abs parent returned error: %v", err)
	}
	childAbs, err := filepath.Abs(childWorkspace)
	if err != nil {
		t.Fatalf("filepath.Abs child returned error: %v", err)
	}

	deadPID := 999_999
	for processAlive(deadPID) {
		deadPID++
	}

	for index, workspaceRoot := range []string{parentAbs, childAbs} {
		stateKey := StateKey(workspaceRoot, "go")
		statePath, err := lifecycle.statePathForKey(stateKey)
		if err != nil {
			t.Fatalf("statePathForKey returned error: %v", err)
		}

		state := lifecycleState{
			WorkspaceRoot:    workspaceRoot,
			Language:         "go",
			StateKey:         stateKey,
			PID:              deadPID + index,
			Binary:           "gopls",
			StartedUnixNano:  time.Now().Add(-time.Hour).UnixNano(),
			LastUsedUnixNano: time.Now().Add(-time.Minute).UnixNano(),
		}
		if err := writeStateFile(statePath, state); err != nil {
			t.Fatalf("writeStateFile returned error: %v", err)
		}
	}

	statuses, err := lifecycle.Status(context.Background(), childWorkspace)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("Status returned %d entries, want 1", len(statuses))
	}
	if statuses[0].WorkspaceRoot != childAbs {
		t.Fatalf("Status WorkspaceRoot = %q, want %q", statuses[0].WorkspaceRoot, childAbs)
	}
}
