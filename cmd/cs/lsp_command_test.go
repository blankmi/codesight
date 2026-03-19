package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/blankbytes/codesight/pkg/lsp"
)

func TestWarmupCommandSuccessOutputContract(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, ".codesight", "config.toml"), "")
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
		"#!/bin/sh\nexec %q -test.run=TestRefsCommandLSPHelperProcess -- %s\n",
		testBinary,
		refsLSPHelperArg,
	)
	if err := os.WriteFile(helperScriptPath, []byte(helperScript), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	t.Setenv("PATH", helperBinDir)
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())
	t.Setenv(refsLSPHelperWorkspaceEnv, workspace)

	daemonConnector := &testWarmupBridgeDaemonConnector{registry: lsp.NewRegistry()}
	legacyErr := errors.New("legacy startup should not be used in warmup daemon-routing test")
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return daemonConnector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, legacyErr
		},
	)

	stdout, _, err := executeRootCommand(t, "lsp", "warmup", workspace)
	if err != nil {
		t.Fatalf("warmup command returned error: %v", err)
	}

	wantRoot, err := resolveRefsWorkspaceRoot(workspace)
	if err != nil {
		t.Fatalf("resolveRefsWorkspaceRoot returned error: %v", err)
	}

	want := fmt.Sprintf("LSP warmup ready (go): %s", wantRoot)
	if strings.TrimSpace(stdout) != want {
		t.Fatalf("warmup output = %q, want %q", strings.TrimSpace(stdout), want)
	}
	if daemonConnector.calls != 1 {
		t.Fatalf("daemon connector calls = %d, want 1", daemonConnector.calls)
	}
	if len(daemonConnector.languages) != 1 || daemonConnector.languages[0] != "go" {
		t.Fatalf("daemon connector languages = %#v, want [go]", daemonConnector.languages)
	}
	if len(daemonConnector.workspaces) != 1 || daemonConnector.workspaces[0] != wantRoot {
		t.Fatalf("daemon connector workspaces = %#v, want [%q]", daemonConnector.workspaces, wantRoot)
	}
}

func TestWarmupCommandUnsupportedLanguageOutputContract(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, ".codesight", "config.toml"), "")
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# sample\n"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	stdout, _, err := executeRootCommand(t, "lsp", "warmup", workspace)
	if err != nil {
		t.Fatalf("warmup command returned error: %v", err)
	}

	want := "No supported LSP language detected for warmup"
	if strings.TrimSpace(stdout) != want {
		t.Fatalf("warmup output = %q, want %q", strings.TrimSpace(stdout), want)
	}
}

type testWarmupBridgeDaemonConnector struct {
	registry *lsp.Registry

	calls      int
	workspaces []string
	languages  []string
}

func TestWarmupCommandProbeEmptySymbolsTimesOut(t *testing.T) {
	// Shorten probe timers so the test doesn't block for 90s.
	prevInterval, prevTimeout := warmupProbeInterval, warmupProbeTimeout
	warmupProbeInterval = 50 * time.Millisecond
	warmupProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() {
		warmupProbeInterval = prevInterval
		warmupProbeTimeout = prevTimeout
	})

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, ".codesight", "config.toml"), "")
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package sample\n"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	helperBinDir := t.TempDir()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	helperScriptPath := filepath.Join(helperBinDir, "gopls")
	helperScript := fmt.Sprintf(
		"#!/bin/sh\nexec %q -test.run=TestRefsCommandLSPHelperProcess -- %s\n",
		testBinary,
		refsLSPHelperArg,
	)
	if err := os.WriteFile(helperScriptPath, []byte(helperScript), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	t.Setenv("PATH", helperBinDir)
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())
	t.Setenv(refsLSPHelperWorkspaceEnv, workspace)

	daemonConnector := &testWarmupEmptySymbolsDaemonConnector{}
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return daemonConnector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, errors.New("legacy startup should not be used")
		},
	)

	_, _, err = executeRootCommand(t, "lsp", "warmup", workspace)
	if err == nil {
		t.Fatal("expected error from warmup probe timeout, got nil")
	}
	if !strings.Contains(err.Error(), "returned no symbols") {
		t.Fatalf("expected 'returned no symbols' in error, got: %v", err)
	}
}

// testWarmupEmptySymbolsDaemonConnector returns a client backed by a fake LSP
// server that responds to workspace/symbol with an empty array.
type testWarmupEmptySymbolsDaemonConnector struct{}

func (c *testWarmupEmptySymbolsDaemonConnector) Connect(
	_ context.Context,
	workspaceRoot string,
	language string,
) (lsp.DaemonConnection, error) {
	serverConn, clientConn := net.Pipe()

	// Fake LSP server: reads requests, responds with empty result.
	go func() {
		defer serverConn.Close()
		reader := bufio.NewReader(serverConn)
		for {
			payload, err := readTestLSPMessage(reader)
			if err != nil {
				return
			}
			var req struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &req); err != nil {
				return
			}
			if req.ID == 0 {
				continue // notification
			}
			result, _ := json.Marshal([]lsp.SymbolInformation{})
			resp, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  json.RawMessage(result),
			})
			header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(resp))
			if _, err := serverConn.Write([]byte(header)); err != nil {
				return
			}
			if _, err := serverConn.Write(resp); err != nil {
				return
			}
		}
	}()

	client, err := lsp.NewClient(clientConn)
	if err != nil {
		clientConn.Close()
		serverConn.Close()
		return lsp.DaemonConnection{}, err
	}

	return lsp.DaemonConnection{
		Client: client,
		Lease: lsp.Lease{
			WorkspaceRoot: workspaceRoot,
			Language:      language,
			PID:           9_005,
			Reused:        false,
		},
	}, nil
}

func readTestLSPMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "content-length:") {
			val := strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1])
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("parse content-length %q: %w", val, err)
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing content-length")
	}
	buf := make([]byte, contentLength)
	_, err := io.ReadFull(reader, buf)
	return buf, err
}

func TestWarmupStatusNoDaemon(t *testing.T) {
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())

	workspace := t.TempDir()
	result, err := executeWarmupCommand(context.Background(), warmupCommandOptions{
		WorkspaceRoot: workspace,
		Status:        true,
	})
	if err != nil {
		t.Fatalf("executeWarmupCommand returned error: %v", err)
	}

	if result.Statuses == nil {
		t.Fatal("expected non-nil Statuses slice for --status")
	}
	if len(result.Statuses) != 0 {
		t.Fatalf("expected 0 statuses, got %d", len(result.Statuses))
	}
}

func TestWarmupStatusWithDaemon(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)

	workspace := t.TempDir()
	workspaceAbs, err := resolveRefsWorkspaceRoot(workspace)
	if err != nil {
		t.Fatalf("resolveRefsWorkspaceRoot returned error: %v", err)
	}

	stateKey := lsp.StateKey(workspaceAbs, "go")
	lspDir := filepath.Join(stateDir, "lsp")
	if err := os.MkdirAll(lspDir, 0o700); err != nil {
		t.Fatalf("mkdir returned error: %v", err)
	}

	state := struct {
		WorkspaceRoot    string   `json:"workspace_root"`
		Language         string   `json:"language"`
		StateKey         string   `json:"state_key"`
		PID              int      `json:"pid"`
		Binary           string   `json:"binary"`
		Args             []string `json:"args"`
		StartedUnixNano  int64    `json:"started_unix_nano"`
		LastUsedUnixNano int64    `json:"last_used_unix_nano"`
	}{
		WorkspaceRoot:    workspaceAbs,
		Language:         "go",
		StateKey:         stateKey,
		PID:              os.Getpid(),
		Binary:           "gopls",
		Args:             []string{"serve"},
		StartedUnixNano:  time.Now().Add(-time.Hour).UnixNano(),
		LastUsedUnixNano: time.Now().Add(-time.Minute).UnixNano(),
	}

	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	statePath := filepath.Join(lspDir, stateKey+".json")
	if err := os.WriteFile(statePath, payload, 0o600); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	result, err := executeWarmupCommand(context.Background(), warmupCommandOptions{
		WorkspaceRoot: workspaceAbs,
		Status:        true,
	})
	if err != nil {
		t.Fatalf("executeWarmupCommand returned error: %v", err)
	}

	if len(result.Statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(result.Statuses))
	}

	s := result.Statuses[0]
	if s.Language != "go" {
		t.Fatalf("status Language = %q, want %q", s.Language, "go")
	}
	if s.PID != os.Getpid() {
		t.Fatalf("status PID = %d, want %d", s.PID, os.Getpid())
	}
	if s.Binary != "gopls" {
		t.Fatalf("status Binary = %q, want %q", s.Binary, "gopls")
	}
}

func (c *testWarmupBridgeDaemonConnector) Connect(
	ctx context.Context,
	workspaceRoot string,
	language string,
) (lsp.DaemonConnection, error) {
	c.calls++
	c.workspaces = append(c.workspaces, workspaceRoot)
	c.languages = append(c.languages, language)

	spec, err := c.registry.Lookup(language)
	if err != nil {
		return lsp.DaemonConnection{}, err
	}

	client, err := startRefsLSPClient(ctx, spec, workspaceRoot)
	if err != nil {
		return lsp.DaemonConnection{}, err
	}

	return lsp.DaemonConnection{
		Client: client,
		Lease: lsp.Lease{
			WorkspaceRoot: workspaceRoot,
			Language:      language,
			PID:           9_004,
			Reused:        false,
		},
	}, nil
}
