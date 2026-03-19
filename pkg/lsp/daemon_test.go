//go:build !windows

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	daemonFakeLSPHelperArg      = "codesight-lsp-daemon-fake-lsp"
	daemonShutdownLogPathEnvVar = "CODESIGHT_LSP_DAEMON_TEST_SHUTDOWN_LOG"
	daemonInitLogPathEnvVar     = "CODESIGHT_LSP_DAEMON_TEST_INIT_LOG"
)

func TestDaemonProcessConfigValidateIdleTimeout(t *testing.T) {
	base := daemonProcessConfig{
		WorkspaceRoot: "/tmp/workspace",
		Language:      "go",
		StateKey:      "state-key",
		StatePath:     "/tmp/state.json",
		SocketPath:    "/tmp/state.sock",
		Binary:        "gopls",
		Args:          []string{"serve"},
	}

	testCases := []struct {
		name          string
		idleTimeoutNS int64
		wantErr       bool
	}{
		{name: "zero", idleTimeoutNS: 0, wantErr: true},
		{name: "negative", idleTimeoutNS: -1, wantErr: true},
		{name: "positive", idleTimeoutNS: int64(250 * time.Millisecond), wantErr: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			config := base
			config.IdleTimeoutNS = testCase.idleTimeoutNS

			err := config.validate()
			if testCase.wantErr {
				if err == nil {
					t.Fatal("validate error = nil, want timeout validation error")
				}
				if !strings.Contains(err.Error(), "daemon idle timeout must be > 0") {
					t.Fatalf("validate error = %v, want timeout validation error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate returned unexpected error: %v", err)
			}
		})
	}
}

func TestDaemonProcessConfigValidateJavaGradleBaseline(t *testing.T) {
	base := daemonProcessConfig{
		WorkspaceRoot: "/tmp/workspace",
		Language:      "java",
		StateKey:      "state-key",
		StatePath:     "/tmp/state.json",
		SocketPath:    "/tmp/state.sock",
		Binary:        "jdtls",
		Args:          []string{"-configuration", "/tmp/config"},
		IdleTimeoutNS: int64(10 * time.Second),
	}

	invalid := base
	invalid.JavaGradleBuildBaseline = &JavaGradleBuildBaseline{}
	if err := invalid.validate(); err == nil {
		t.Fatal("validate error = nil, want baseline fingerprint validation error")
	}

	valid := base
	valid.JavaGradleBuildBaseline = &JavaGradleBuildBaseline{Fingerprint: "java-baseline"}
	if err := valid.validate(); err != nil {
		t.Fatalf("validate returned error for valid baseline: %v", err)
	}
}

func TestDaemonProxyRoundTrip(t *testing.T) {
	lifecycle := newDaemonTestLifecycle(t, DefaultIdleTimeout, "")
	workspace := t.TempDir()

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	conn, err := dialDaemonSocket(context.Background(), lease.SocketPath)
	if err != nil {
		t.Fatalf("dialDaemonSocket returned error: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	request := RequestMessage{
		JSONRPC: JSONRPCVersion,
		ID:      42,
		Method:  "codesight/echo",
		Params: map[string]any{
			"message": "round-trip",
		},
	}
	if err := writeJSONRPCMessage(bufio.NewWriter(conn), request); err != nil {
		t.Fatalf("writeJSONRPCMessage returned error: %v", err)
	}

	payload, err := readLSPMessage(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("readLSPMessage returned error: %v", err)
	}

	var response ResponseMessage
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("json.Unmarshal response returned error: %v", err)
	}
	id, err := parseResponseID(response.ID)
	if err != nil {
		t.Fatalf("parseResponseID returned error: %v", err)
	}
	if id != request.ID {
		t.Fatalf("response id = %d, want %d", id, request.ID)
	}

	var echoed map[string]string
	if err := json.Unmarshal(response.Result, &echoed); err != nil {
		t.Fatalf("json.Unmarshal response result returned error: %v", err)
	}
	if echoed["message"] != "round-trip" {
		t.Fatalf("echoed message = %q, want %q", echoed["message"], "round-trip")
	}
}

func TestDaemonRejectsBusyConcurrentClient(t *testing.T) {
	lifecycle := newDaemonTestLifecycle(t, DefaultIdleTimeout, "")
	workspace := t.TempDir()

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	firstConn, err := dialDaemonSocket(context.Background(), lease.SocketPath)
	if err != nil {
		t.Fatalf("dial first daemon client returned error: %v", err)
	}
	defer func() {
		_ = firstConn.Close()
	}()

	time.Sleep(25 * time.Millisecond)

	secondConn, err := dialDaemonSocket(context.Background(), lease.SocketPath)
	if err != nil {
		t.Fatalf("dial second daemon client returned error: %v", err)
	}
	defer func() {
		_ = secondConn.Close()
	}()

	if err := secondConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline returned error: %v", err)
	}

	buffer := make([]byte, 128)
	n, readErr := secondConn.Read(buffer)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		t.Fatalf("read busy response returned error: %v", readErr)
	}
	if !strings.Contains(string(buffer[:n]), daemonBusyMessage) {
		t.Fatalf("busy response = %q, want substring %q", string(buffer[:n]), daemonBusyMessage)
	}
}

func TestDaemonIdleTimeoutRemovesArtifacts(t *testing.T) {
	idleTimeout := 150 * time.Millisecond
	shutdownLogPath := filepath.Join(t.TempDir(), "shutdown.log")
	lifecycle := newDaemonTestLifecycle(t, idleTimeout, shutdownLogPath)
	workspace := t.TempDir()

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}

	statePath, err := lifecycle.statePathForKey(lease.StateKey)
	if err != nil {
		t.Fatalf("statePathForKey returned error: %v", err)
	}

	if !waitForProcessExit(lease.PID, 5*time.Second) {
		t.Fatalf("daemon process %d did not exit after idle timeout", lease.PID)
	}

	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state file to be removed, got err: %v", err)
	}
	if _, err := os.Stat(lease.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected socket file to be removed, got err: %v", err)
	}

	logData, err := os.ReadFile(shutdownLogPath)
	if err != nil {
		t.Fatalf("os.ReadFile shutdown log returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(lines) != 2 || lines[0] != MethodShutdown || lines[1] != MethodExit {
		t.Fatalf("shutdown log lines = %q, want [%q %q]", lines, MethodShutdown, MethodExit)
	}
}

func TestDaemonRecoversStaleSocketAndState(t *testing.T) {
	lifecycle := newDaemonTestLifecycle(t, DefaultIdleTimeout, "")
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

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if lease.Reused {
		t.Fatal("stale daemon state should not be reused")
	}
	if lease.PID == stalePID {
		t.Fatalf("Ensure reused stale PID %d", stalePID)
	}

	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	conn, err := dialDaemonSocket(context.Background(), lease.SocketPath)
	if err != nil {
		t.Fatalf("dialDaemonSocket returned error: %v", err)
	}
	_ = conn.Close()
}

func TestDaemonFakeLanguageServerProcess(t *testing.T) {
	if !hasArg(daemonFakeLSPHelperArg) {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	shutdownLogPath := strings.TrimSpace(os.Getenv(daemonShutdownLogPathEnvVar))
	initLogPath := strings.TrimSpace(os.Getenv(daemonInitLogPathEnvVar))

	for {
		payload, err := readLSPMessage(reader)
		if err != nil {
			return
		}

		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			continue
		}

		switch envelope.Method {
		case MethodInitialize:
			appendDaemonShutdownLog(initLogPath, MethodInitialize)
			result, _ := json.Marshal(InitializeResult{
				Capabilities: map[string]any{"textDocumentSync": 1},
				ServerInfo:   &ServerInfo{Name: "fake-lsp", Version: "0.1"},
			})
			response := ResponseMessage{
				JSONRPC: JSONRPCVersion,
				ID:      append(json.RawMessage(nil), envelope.ID...),
				Result:  result,
			}
			if err := writeJSONRPCMessage(writer, response); err != nil {
				return
			}
		case MethodInitialized:
			appendDaemonShutdownLog(initLogPath, MethodInitialized)
		case MethodShutdown:
			appendDaemonShutdownLog(shutdownLogPath, MethodShutdown)
			response := ResponseMessage{
				JSONRPC: JSONRPCVersion,
				ID:      append(json.RawMessage(nil), envelope.ID...),
				Result:  json.RawMessage("null"),
			}
			if err := writeJSONRPCMessage(writer, response); err != nil {
				return
			}
		case MethodExit:
			appendDaemonShutdownLog(shutdownLogPath, MethodExit)
			return
		default:
			if len(envelope.ID) == 0 {
				continue
			}
			result := envelope.Params
			if len(result) == 0 {
				result = json.RawMessage(`{"ok":true}`)
			}
			response := ResponseMessage{
				JSONRPC: JSONRPCVersion,
				ID:      append(json.RawMessage(nil), envelope.ID...),
				Result:  append(json.RawMessage(nil), result...),
			}
			if err := writeJSONRPCMessage(writer, response); err != nil {
				return
			}
		}
	}
}

func TestDaemonInitializeCaching(t *testing.T) {
	initLogPath := filepath.Join(t.TempDir(), "init.log")
	lifecycle := newDaemonTestLifecycleWithInitLog(t, DefaultIdleTimeout, "", initLogPath)
	workspace := t.TempDir()

	lease, err := lifecycle.Ensure(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = lifecycle.Stop(workspace, "go")
	})

	// Helper: perform initialize handshake + echo request on a connection.
	doSession := func(reqID int64) {
		conn, err := dialDaemonSocket(context.Background(), lease.SocketPath)
		if err != nil {
			t.Fatalf("dialDaemonSocket returned error: %v", err)
		}
		defer func() {
			_ = conn.Close()
		}()

		writer := bufio.NewWriter(conn)
		reader := bufio.NewReader(conn)

		// Send initialize request.
		initReq := RequestMessage{
			JSONRPC: JSONRPCVersion,
			ID:      reqID,
			Method:  MethodInitialize,
			Params:  InitializeParams{Capabilities: map[string]any{}},
		}
		if err := writeJSONRPCMessage(writer, initReq); err != nil {
			t.Fatalf("write initialize returned error: %v", err)
		}

		// Read initialize response.
		payload, err := readLSPMessage(reader)
		if err != nil {
			t.Fatalf("read initialize response returned error: %v", err)
		}
		var resp ResponseMessage
		if err := json.Unmarshal(payload, &resp); err != nil {
			t.Fatalf("unmarshal initialize response returned error: %v", err)
		}
		gotID, err := parseResponseID(resp.ID)
		if err != nil {
			t.Fatalf("parseResponseID returned error: %v", err)
		}
		if gotID != reqID {
			t.Fatalf("initialize response id = %d, want %d", gotID, reqID)
		}
		if resp.Error != nil {
			t.Fatalf("initialize response error: %v", resp.Error)
		}

		// Send initialized notification.
		initedNotif := NotificationMessage{
			JSONRPC: JSONRPCVersion,
			Method:  MethodInitialized,
		}
		if err := writeJSONRPCMessage(writer, initedNotif); err != nil {
			t.Fatalf("write initialized returned error: %v", err)
		}

		// Send an echo request to confirm the connection works.
		echoReq := RequestMessage{
			JSONRPC: JSONRPCVersion,
			ID:      reqID + 1000,
			Method:  "codesight/echo",
			Params:  map[string]any{"session": reqID},
		}
		if err := writeJSONRPCMessage(writer, echoReq); err != nil {
			t.Fatalf("write echo returned error: %v", err)
		}

		echoPayload, err := readLSPMessage(reader)
		if err != nil {
			t.Fatalf("read echo response returned error: %v", err)
		}
		var echoResp ResponseMessage
		if err := json.Unmarshal(echoPayload, &echoResp); err != nil {
			t.Fatalf("unmarshal echo response returned error: %v", err)
		}
		echoRespID, _ := parseResponseID(echoResp.ID)
		if echoRespID != reqID+1000 {
			t.Fatalf("echo response id = %d, want %d", echoRespID, reqID+1000)
		}
	}

	// First client session.
	doSession(1)

	// Small gap to let the daemon deactivate the first client.
	time.Sleep(50 * time.Millisecond)

	// Second client session.
	doSession(100)

	// Small gap to let async log writes complete.
	time.Sleep(50 * time.Millisecond)

	// Verify the fake server received exactly 1 initialize and 1 initialized.
	logData, err := os.ReadFile(initLogPath)
	if err != nil {
		t.Fatalf("os.ReadFile init log returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")

	initCount := 0
	initializedCount := 0
	for _, line := range lines {
		switch line {
		case MethodInitialize:
			initCount++
		case MethodInitialized:
			initializedCount++
		}
	}

	if initCount != 1 {
		t.Fatalf("fake server received %d initialize requests, want 1", initCount)
	}
	if initializedCount != 1 {
		t.Fatalf("fake server received %d initialized notifications, want 1", initializedCount)
	}
}

func newDaemonTestLifecycleWithInitLog(t *testing.T, idleTimeout time.Duration, shutdownLogPath, initLogPath string) *Lifecycle {
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
	t.Setenv(daemonShutdownLogPathEnvVar, shutdownLogPath)
	t.Setenv(daemonInitLogPathEnvVar, initLogPath)

	registry := NewRegistryFromEntries(map[string]ServerSpec{
		"go": {
			Language:    "go",
			Binary:      testBinary,
			Args:        []string{"-test.run=TestDaemonFakeLanguageServerProcess", "--", daemonFakeLSPHelperArg},
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

func newDaemonTestLifecycle(t *testing.T, idleTimeout time.Duration, shutdownLogPath string) *Lifecycle {
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
	t.Setenv(daemonShutdownLogPathEnvVar, shutdownLogPath)

	registry := NewRegistryFromEntries(map[string]ServerSpec{
		"go": {
			Language:    "go",
			Binary:      testBinary,
			Args:        []string{"-test.run=TestDaemonFakeLanguageServerProcess", "--", daemonFakeLSPHelperArg},
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

func appendDaemonShutdownLog(path, event string) {
	if strings.TrimSpace(path) == "" {
		return
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() {
		_ = file.Close()
	}()

	_, _ = file.WriteString(event + "\n")
}
