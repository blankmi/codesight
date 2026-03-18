//go:build !windows

package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonConnectorLaunchesDaemonWhenNoStateExists(t *testing.T) {
	lifecycle := newDaemonTestLifecycle(t, DefaultIdleTimeout, "")
	connector := NewDaemonConnector(nil, WithDaemonConnectorLifecycle(lifecycle))
	workspace := t.TempDir()

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	connection, err := connector.Connect(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer func() {
		_ = connection.Client.Close()
	}()

	if connection.Lease.Reused {
		t.Fatal("first daemon connect should not be reused")
	}
	if connection.Lease.PID <= 0 {
		t.Fatalf("daemon PID = %d, expected positive PID", connection.Lease.PID)
	}
	if !waitForProcessAlive(connection.Lease.PID, time.Second) {
		t.Fatalf("daemon process %d did not become alive", connection.Lease.PID)
	}

	assertDaemonConnectorReady(t, connection.Client)
}

func TestDaemonConnectorConnectsExistingDaemonWithoutRelaunch(t *testing.T) {
	lifecycle := newDaemonTestLifecycle(t, DefaultIdleTimeout, "")
	connector := NewDaemonConnector(
		nil,
		WithDaemonConnectorLifecycle(lifecycle),
		withDaemonConnectorRetryBudget(0),
	)
	workspace := t.TempDir()

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	existing, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if existing.Reused {
		t.Fatal("first Ensure should not report reused daemon")
	}

	second, err := connectWithTransientRetry(connector, workspace, "go")
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer func() {
		_ = second.Client.Close()
	}()

	if second.Lease.PID != existing.PID {
		t.Fatalf("connector daemon PID = %d, want existing PID %d", second.Lease.PID, existing.PID)
	}
	if !second.Lease.Reused {
		t.Fatal("connector should report daemon lease as reused when daemon already exists")
	}

	assertDaemonConnectorReady(t, second.Client)
}

func TestDaemonConnectorCleansStaleStateThenReconnects(t *testing.T) {
	lifecycle := newDaemonTestLifecycle(t, DefaultIdleTimeout, "")
	connector := NewDaemonConnector(nil, WithDaemonConnectorLifecycle(lifecycle))
	workspace := t.TempDir()

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

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

	stalePID := 999_999
	for processAlive(stalePID) {
		stalePID++
	}

	staleState := lifecycleState{
		WorkspaceRoot:    workspaceAbs,
		Language:         "go",
		StateKey:         stateKey,
		SocketPath:       socketPath,
		PID:              stalePID,
		Binary:           "stale-binary",
		Args:             []string{"--stale"},
		StartedUnixNano:  time.Now().Add(-time.Minute).UnixNano(),
		LastUsedUnixNano: time.Now().UnixNano(),
	}
	if err := writeStateFile(statePath, staleState); err != nil {
		t.Fatalf("writeStateFile returned error: %v", err)
	}
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("os.WriteFile socketPath returned error: %v", err)
	}

	connection, err := connector.Connect(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer func() {
		_ = connection.Client.Close()
	}()

	if connection.Lease.Reused {
		t.Fatal("stale daemon state should not be reused")
	}
	if connection.Lease.PID == stalePID {
		t.Fatalf("Connect reused stale PID %d", stalePID)
	}
	if connection.Lease.SocketPath != socketPath {
		t.Fatalf("socket path = %q, want %q", connection.Lease.SocketPath, socketPath)
	}

	onDisk, err := readStateFile(statePath)
	if err != nil {
		t.Fatalf("readStateFile returned error: %v", err)
	}
	if onDisk.PID != connection.Lease.PID {
		t.Fatalf("state PID = %d, want %d", onDisk.PID, connection.Lease.PID)
	}

	assertDaemonConnectorReady(t, connection.Client)
}

func TestDaemonConnectorRetryBudgetExhaustionReturnsWrappedError(t *testing.T) {
	lifecycle := newDaemonTestLifecycle(t, DefaultIdleTimeout, "")
	workspace := t.TempDir()
	dialErr := errors.New("simulated dial failure")
	dialCalls := 0

	connector := NewDaemonConnector(
		nil,
		WithDaemonConnectorLifecycle(lifecycle),
		withDaemonConnectorRetryBudget(1),
		withDaemonConnectorDialSocket(func(ctx context.Context, socketPath string) (io.ReadWriteCloser, error) {
			_ = ctx
			_ = socketPath
			dialCalls++
			return nil, dialErr
		}),
	)

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	_, err := connector.Connect(context.Background(), workspace, "go")
	if err == nil {
		t.Fatal("Connect returned nil error, want wrapped retry exhaustion error")
	}
	if dialCalls != 2 {
		t.Fatalf("dial attempts = %d, want 2", dialCalls)
	}
	if !errors.Is(err, dialErr) {
		t.Fatalf("Connect error = %v, want wrapped dial error", err)
	}
	if !strings.Contains(err.Error(), "connect daemon client") {
		t.Fatalf("Connect error = %q, want wrapped connector context", err)
	}
}

func TestDaemonConnectorUnsupportedPlatformReturnsFallbackSignal(t *testing.T) {
	connector := NewDaemonConnector(NewRegistry(), withDaemonConnectorGOOS("windows"))

	_, err := connector.Connect(context.Background(), t.TempDir(), "go")
	if err == nil {
		t.Fatal("Connect returned nil error, want legacy fallback signal")
	}
	if !errors.Is(err, ErrDaemonLegacyFallback) {
		t.Fatalf("Connect error = %v, want ErrDaemonLegacyFallback", err)
	}
	if !errors.Is(err, ErrDaemonDisabled) {
		t.Fatalf("Connect error = %v, want ErrDaemonDisabled", err)
	}

	var fallbackErr *DaemonLegacyFallbackError
	if !errors.As(err, &fallbackErr) {
		t.Fatalf("Connect error = %v, want DaemonLegacyFallbackError", err)
	}
	if fallbackErr.GOOS != "windows" {
		t.Fatalf("fallback GOOS = %q, want %q", fallbackErr.GOOS, "windows")
	}
}

func assertDaemonConnectorReady(t *testing.T, client *Client) {
	t.Helper()

	if client == nil {
		t.Fatal("daemon client is nil")
	}

	params := map[string]string{"message": "ready"}
	var response map[string]string
	if err := client.Call(context.Background(), "codesight/echo", params, &response); err != nil {
		t.Fatalf("client.Call returned error: %v", err)
	}
	if response["message"] != "ready" {
		t.Fatalf("echo response = %q, want %q", response["message"], "ready")
	}
}

func connectWithTransientRetry(connector *DaemonConnector, workspace, language string) (DaemonConnection, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, err := connector.Connect(context.Background(), workspace, language)
		if err == nil {
			return connection, nil
		}
		if !isTransientConnectorError(err) {
			return DaemonConnection{}, err
		}
		if time.Now().After(deadline) {
			return DaemonConnection{}, err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func isTransientConnectorError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, daemonBusyMessage) ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "missing Content-Length header")
}
