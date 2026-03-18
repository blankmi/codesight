//go:build windows

package lsp

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestLifecycleEnsureReturnsErrDaemonDisabledOnWindows(t *testing.T) {
	lifecycle := newWindowsFallbackLifecycle(t)

	_, err := lifecycle.Ensure(context.Background(), t.TempDir(), "go")
	if !errors.Is(err, ErrDaemonDisabled) {
		t.Fatalf("Ensure error = %v, want ErrDaemonDisabled", err)
	}
}

func newWindowsFallbackLifecycle(t *testing.T) *Lifecycle {
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
			InstallHint: "test helper process",
		},
	})

	return NewLifecycle(
		registry,
		WithIdleTimeout(DefaultIdleTimeout),
		WithStateDirResolver(func() (string, error) {
			return stateDir, nil
		}),
	)
}
