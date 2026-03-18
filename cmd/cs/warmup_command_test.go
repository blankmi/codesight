package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blankbytes/codesight/pkg/lsp"
)

func TestWarmupCommandSuccessOutputContract(t *testing.T) {
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

	stdout, _, err := executeRootCommand(t, "warmup", workspace)
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
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# sample\n"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	stdout, _, err := executeRootCommand(t, "warmup", workspace)
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
