package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/blankbytes/codesight/pkg/lsp"
)

func TestCheckCommandRequiresAtLeastOneArg(t *testing.T) {
	restoreRunCheckCommand(t, func(_ context.Context, opts checkCommandOptions) (lsp.CheckResult, error) {
		t.Fatal("runCheckCommand should not be called with zero args")
		return lsp.CheckResult{}, nil
	})

	_, _, err := executeCheckRootCommand(t, "check")
	if err == nil {
		t.Fatal("expected error for zero args, got nil")
	}
}

func TestCheckCommandDirectoryPathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	_, nestedDir, resolvedRepoRoot, resolvedNestedDir := createNestedProject(t)
	setWorkingDirectory(t, t.TempDir())

	var got checkCommandOptions
	restoreRunCheckCommand(t, func(_ context.Context, opts checkCommandOptions) (lsp.CheckResult, error) {
		got = opts
		return lsp.CheckResult{}, nil
	})

	if _, _, err := executeCheckRootCommand(t, "check", nestedDir); err != nil {
		t.Fatalf("check command returned error: %v", err)
	}

	if got.WorkspaceRoot != resolvedRepoRoot {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedRepoRoot)
	}
	if len(got.TargetPaths) != 1 || got.TargetPaths[0] != resolvedNestedDir {
		t.Fatalf("target paths = %q, want [%q]", got.TargetPaths, resolvedNestedDir)
	}
}

func TestCheckCommandFilePathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	repoRoot, nestedDir, resolvedRepoRoot, _ := createNestedProject(t)
	filePath := filepath.Join(nestedDir, "main.go")
	writeTestFile(t, filePath, "package sample\n")
	setWorkingDirectory(t, t.TempDir())

	var got checkCommandOptions
	restoreRunCheckCommand(t, func(_ context.Context, opts checkCommandOptions) (lsp.CheckResult, error) {
		got = opts
		return lsp.CheckResult{}, nil
	})

	if _, _, err := executeCheckRootCommand(t, "check", filePath); err != nil {
		t.Fatalf("check command returned error: %v", err)
	}

	if got.WorkspaceRoot != resolvedRepoRoot {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedRepoRoot)
	}
	wantTarget := mustResolvePath(t, filePath)
	if len(got.TargetPaths) != 1 || got.TargetPaths[0] != wantTarget {
		t.Fatalf("target paths = %q, want [%q]", got.TargetPaths, wantTarget)
	}
	_ = repoRoot
}

func TestCheckCommandOutputPassThroughAndExitError(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "main.go")
	writeTestFile(t, filePath, "package sample\n")

	restoreRunCheckCommand(t, func(_ context.Context, _ checkCommandOptions) (lsp.CheckResult, error) {
		return lsp.CheckResult{
			Diagnostics: []lsp.CheckDiagnostic{
				{
					Path:    "main.go",
					Line:    4,
					Column:  2,
					Message: "unexpected token",
				},
			},
		}, nil
	})

	stdout, _, err := executeCheckRootCommand(t, "check", filePath)
	if !errors.Is(err, errCheckFoundSyntaxErrors) {
		t.Fatalf("error = %v, want errCheckFoundSyntaxErrors", err)
	}

	want := "main.go\n  4:2  unexpected token\n1 syntax error found"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestExecuteCheckCommandMissingLSPBinaryExactError(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "main.go"), "package sample\n")

	t.Setenv("PATH", "")
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())

	_, err := executeCheckCommand(context.Background(), checkCommandOptions{
		WorkspaceRoot: workspace,
		TargetPaths:   []string{workspace},
	})
	if err == nil {
		t.Fatal("expected missing LSP binary error, got nil")
	}

	want := "cs check: LSP required but gopls not found. Install: go install golang.org/x/tools/gopls@latest"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestExecuteCheckCommandUnsupportedLanguageExactError(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "README.txt")
	writeTestFile(t, target, "unsupported\n")

	_, err := executeCheckCommand(context.Background(), checkCommandOptions{
		WorkspaceRoot: workspace,
		TargetPaths:   []string{target},
	})
	if err == nil {
		t.Fatal("expected unsupported language error, got nil")
	}

	want := fmt.Sprintf("cs check: no supported LSP language detected for %s", mustResolvePath(t, target))
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestExecuteCheckCommandUsesDaemonConnectorOnUnix(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "main.go"), "package sample\n")

	helperBinDir := t.TempDir()
	helperScriptPath := filepath.Join(helperBinDir, "gopls")
	if err := os.WriteFile(helperScriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	t.Setenv("PATH", helperBinDir)

	daemonErr := errors.New("daemon connector used")
	connector := &testCheckDaemonConnector{err: daemonErr}
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return connector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, errors.New("legacy path should not be used on unix routing")
		},
	)

	_, err := executeCheckCommand(context.Background(), checkCommandOptions{
		WorkspaceRoot: workspace,
		TargetPaths:   []string{workspace},
	})
	if !errors.Is(err, daemonErr) {
		t.Fatalf("executeCheckCommand error = %v, want daemon error %v", err, daemonErr)
	}
	if connector.calls != 1 {
		t.Fatalf("daemon connector calls = %d, want 1", connector.calls)
	}
}

func TestExecuteCheckCommandWindowsRoutingUsesLegacyStartup(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "main.go"), "package sample\n")

	helperBinDir := t.TempDir()
	helperScriptPath := filepath.Join(helperBinDir, "gopls")
	if err := os.WriteFile(helperScriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	t.Setenv("PATH", helperBinDir)
	legacyErr := errors.New("legacy startup used")
	daemonConnector := &testCheckDaemonConnector{err: errors.New("daemon connector should not be called on windows routing")}
	restoreLSPRuntimeHooks(
		t,
		"windows",
		func(_ *lsp.Registry) lspDaemonConnector {
			return daemonConnector
		},
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, legacyErr
		},
	)

	_, err := executeCheckCommand(context.Background(), checkCommandOptions{
		WorkspaceRoot: workspace,
		TargetPaths:   []string{workspace},
	})
	if !errors.Is(err, legacyErr) {
		t.Fatalf("executeCheckCommand error = %v, want legacy error %v", err, legacyErr)
	}
	if daemonConnector.calls != 0 {
		t.Fatalf("daemon connector calls = %d, want 0 on windows routing", daemonConnector.calls)
	}
}

func TestCheckCommandAcceptsMultipleFiles(t *testing.T) {
	workspace := t.TempDir()
	file1 := filepath.Join(workspace, "a.go")
	file2 := filepath.Join(workspace, "b.go")
	writeTestFile(t, file1, "package sample\n")
	writeTestFile(t, file2, "package sample\n")

	var got checkCommandOptions
	restoreRunCheckCommand(t, func(_ context.Context, opts checkCommandOptions) (lsp.CheckResult, error) {
		got = opts
		return lsp.CheckResult{}, nil
	})

	if _, _, err := executeCheckRootCommand(t, "check", file1, file2); err != nil {
		t.Fatalf("check command returned error: %v", err)
	}

	if len(got.TargetPaths) != 2 {
		t.Fatalf("target paths length = %d, want 2", len(got.TargetPaths))
	}
	wantFirst := mustResolvePath(t, file1)
	wantSecond := mustResolvePath(t, file2)
	if got.TargetPaths[0] != wantFirst || got.TargetPaths[1] != wantSecond {
		t.Fatalf("target paths = %q, want [%q, %q]", got.TargetPaths, wantFirst, wantSecond)
	}
}

func executeCheckRootCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetCheckCommandFlagState(t)
	return executeRootCommand(t, args...)
}

func resetCheckCommandFlagState(t *testing.T) {
	t.Helper()

	if helpFlag := checkCmd.Flags().Lookup("help"); helpFlag != nil {
		if err := helpFlag.Value.Set(helpFlag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", "help", err)
		}
		helpFlag.Changed = false
	}
}

func restoreRunCheckCommand(t *testing.T, runner func(context.Context, checkCommandOptions) (lsp.CheckResult, error)) {
	t.Helper()

	previous := runCheckCommand
	runCheckCommand = runner
	t.Cleanup(func() {
		runCheckCommand = previous
	})
}

type testCheckDaemonConnector struct {
	calls int
	err   error
}

func (c *testCheckDaemonConnector) Connect(_ context.Context, _, _ string) (lsp.DaemonConnection, error) {
	c.calls++
	if c.err != nil {
		return lsp.DaemonConnection{}, c.err
	}
	return lsp.DaemonConnection{}, nil
}
