package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	configpkg "codesight/pkg/config"
	"codesight/pkg/lsp"
)

const (
	lspRuntimeHelperArg                = "lsp-runtime-helper-process"
	lspRuntimeShutdownLogPathEnvVar    = "CODESIGHT_LSP_RUNTIME_TEST_SHUTDOWN_LOG"
	lspRuntimeConfiguredIdleTimeout    = 150 * time.Millisecond
	lspRuntimeIdleShutdownWaitTimeout  = 5 * time.Second
	lspRuntimeStateRemovalPollInterval = 25 * time.Millisecond
)

func TestLSPRuntimeNewDaemonConnectorHonorsConfiguredIdleTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon connector lifecycle assertions are unix-only")
	}

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package sample\n"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	helperBinDir := t.TempDir()
	helperScriptPath := filepath.Join(helperBinDir, "gopls")
	helperScript := fmt.Sprintf(
		"#!/bin/sh\nexec %q -test.run=TestLSPRuntimeDaemonHelperProcess -- %s\n",
		testBinary,
		lspRuntimeHelperArg,
	)
	if err := os.WriteFile(helperScriptPath, []byte(helperScript), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	stateDir, err := os.MkdirTemp("", "csrt-")
	if err != nil {
		t.Fatalf("os.MkdirTemp state dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(stateDir)
	})
	shutdownLogPath := filepath.Join(t.TempDir(), "shutdown.log")
	t.Setenv("PATH", helperBinDir)
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)
	t.Setenv(lspRuntimeShutdownLogPathEnvVar, shutdownLogPath)

	previousRuntimeConfig := runtimeConfig
	cfg := configpkg.Defaults()
	cfg.LSP.Daemon.IdleTimeout = lspRuntimeConfiguredIdleTimeout.String()
	runtimeConfig = cfg
	t.Cleanup(func() {
		runtimeConfig = previousRuntimeConfig
	})

	connector := lspRuntimeNewDaemonConnector(lsp.NewRegistry())
	connection, err := connector.Connect(context.Background(), workspace, "go")
	if err != nil {
		t.Fatalf("connector.Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = lsp.NewLifecycle(lsp.NewRegistry()).Stop(workspace, "go")
	})

	statePath := strings.TrimSuffix(connection.Lease.SocketPath, ".sock") + ".json"
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file %q to exist: %v", statePath, err)
	}

	if err := connection.Client.Close(); err != nil {
		t.Fatalf("connection.Client.Close returned error: %v", err)
	}

	if err := waitForPathRemoval(statePath, lspRuntimeIdleShutdownWaitTimeout); err != nil {
		t.Fatalf("daemon state file removal: %v", err)
	}
	if err := waitForPathRemoval(connection.Lease.SocketPath, lspRuntimeIdleShutdownWaitTimeout); err != nil {
		t.Fatalf("daemon socket file removal: %v", err)
	}

	logData, err := os.ReadFile(shutdownLogPath)
	if err != nil {
		t.Fatalf("os.ReadFile shutdown log returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(lines) != 2 || lines[0] != lsp.MethodShutdown || lines[1] != lsp.MethodExit {
		t.Fatalf("shutdown log lines = %q, want [%q %q]", lines, lsp.MethodShutdown, lsp.MethodExit)
	}
}

func waitForPathRemoval(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for removal of %s", path)
		}
		time.Sleep(lspRuntimeStateRemovalPollInterval)
	}
}

func TestLSPRuntimeDaemonHelperProcess(t *testing.T) {
	if !hasLSPRuntimeHelperArg(lspRuntimeHelperArg) {
		return
	}

	shutdownLogPath := strings.TrimSpace(os.Getenv(lspRuntimeShutdownLogPathEnvVar))
	if err := runLSPRuntimeDaemonHelperProcess(os.Stdin, os.Stdout, shutdownLogPath); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func runLSPRuntimeDaemonHelperProcess(stdin io.Reader, stdout io.Writer, shutdownLogPath string) error {
	reader := bufio.NewReader(stdin)
	writer := bufio.NewWriter(stdout)

	for {
		payload, err := readRefsLSPMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		var envelope refsRPCEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			continue
		}

		switch envelope.Method {
		case lsp.MethodInitialize:
			result, err := json.Marshal(map[string]any{
				"capabilities": map[string]any{},
				"serverInfo": map[string]any{
					"name": "lsp-runtime-helper",
				},
			})
			if err != nil {
				return err
			}
			if err := writeRefsLSPMessage(writer, refsRPCResponse{
				JSONRPC: lsp.JSONRPCVersion,
				ID:      envelope.ID,
				Result:  result,
			}); err != nil {
				return err
			}
		case lsp.MethodInitialized:
			continue
		case lsp.MethodShutdown:
			appendLSPRuntimeShutdownLog(shutdownLogPath, lsp.MethodShutdown)
			if err := writeRefsLSPMessage(writer, refsRPCResponse{
				JSONRPC: lsp.JSONRPCVersion,
				ID:      envelope.ID,
				Result:  json.RawMessage("null"),
			}); err != nil {
				return err
			}
		case lsp.MethodExit:
			appendLSPRuntimeShutdownLog(shutdownLogPath, lsp.MethodExit)
			return nil
		default:
			if len(envelope.ID) == 0 {
				continue
			}
			if err := writeRefsLSPMessage(writer, refsRPCResponse{
				JSONRPC: lsp.JSONRPCVersion,
				ID:      envelope.ID,
				Result:  json.RawMessage("null"),
			}); err != nil {
				return err
			}
		}
	}
}

func appendLSPRuntimeShutdownLog(path, method string) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return
	}

	logFile, err := os.OpenFile(trimmed, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() {
		_ = logFile.Close()
	}()

	_, _ = fmt.Fprintln(logFile, method)
}

func hasLSPRuntimeHelperArg(target string) bool {
	for _, arg := range os.Args {
		if arg == target {
			return true
		}
	}
	return false
}
