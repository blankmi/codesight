package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blankbytes/codesight/pkg/lsp"
)

func TestImplementsCommandRequiresSymbolArgument(t *testing.T) {
	_, _, err := executeImplementsRootCommand(t, "implements")
	if err == nil {
		t.Fatal("expected missing symbol argument error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestImplementsCommandPassesThroughOutputAndParsedInputs(t *testing.T) {
	var got implementsCommandOptions
	restoreRunImplementsCommand(t, func(_ context.Context, opts implementsCommandOptions) (string, error) {
		got = opts
		return strings.Join([]string{
			"ConcreteTarget (pkg/main.go)",
			"1 implementations",
		}, "\n"), nil
	})

	workspace := t.TempDir()
	stdout, _, err := executeImplementsRootCommand(
		t,
		"implements",
		"Target",
		"--path",
		workspace,
	)
	if err != nil {
		t.Fatalf("implements command returned error: %v", err)
	}

	wantOutput := strings.Join([]string{
		"ConcreteTarget (pkg/main.go)",
		"1 implementations",
	}, "\n")
	if stdout != wantOutput {
		t.Fatalf("unexpected output: %q", stdout)
	}
	if got.Symbol != "Target" {
		t.Fatalf("symbol = %q, want %q", got.Symbol, "Target")
	}
	wantRoot := workspace
	if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
		wantRoot = resolved
	}
	if got.WorkspaceRoot != wantRoot {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, wantRoot)
	}
}

func TestImplementsCommandDefaultsPath(t *testing.T) {
	workingDirectory := t.TempDir()
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	resolvedWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previousWD); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	})

	var got implementsCommandOptions
	restoreRunImplementsCommand(t, func(_ context.Context, opts implementsCommandOptions) (string, error) {
		got = opts
		return "0 implementations", nil
	})

	if _, _, err := executeImplementsRootCommand(t, "implements", "Target"); err != nil {
		t.Fatalf("implements command returned error: %v", err)
	}
	if got.WorkspaceRoot != resolvedWorkingDirectory {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedWorkingDirectory)
	}
}

func TestImplementsCommandAmbiguousErrorPassThrough(t *testing.T) {
	want := strings.Join([]string{
		`ambiguous symbol "Target" — 2 definitions found. Use --path to narrow scope.`,
		"  - a/file.go:10 (function)",
		"  - z/file.go:4 (method)",
	}, "\n")

	restoreRunImplementsCommand(t, func(_ context.Context, _ implementsCommandOptions) (string, error) {
		return "", errors.New(want)
	})

	_, _, err := executeImplementsRootCommand(t, "implements", "Target")
	if err == nil {
		t.Fatal("expected ambiguous symbol error, got nil")
	}
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestImplementsCommandMissingLSPBinaryExactError(t *testing.T) {
	restoreRunImplementsCommand(t, executeImplementsCommand)

	workspace := t.TempDir()
	source := "package sample\n\ntype Target interface { run() }\n"
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	t.Setenv("PATH", "")
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())

	_, _, err := executeImplementsRootCommand(t, "implements", "Target", "--path", workspace)
	if err == nil {
		t.Fatal("expected missing LSP binary error, got nil")
	}

	want := "cs implements: LSP required but gopls not found. Install: go install golang.org/x/tools/gopls@latest"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestImplementsCommandOutputPassThroughFormatContract(t *testing.T) {
	wantOutput := strings.Join([]string{
		"AlphaImpl (pkg/alpha.go)",
		"BetaImpl (pkg/beta.go)",
		"2 implementations",
	}, "\n")
	restoreRunImplementsCommand(t, func(_ context.Context, _ implementsCommandOptions) (string, error) {
		return wantOutput, nil
	})

	stdout, _, err := executeImplementsRootCommand(t, "implements", "Target")
	if err != nil {
		t.Fatalf("implements command returned error: %v", err)
	}
	if stdout != wantOutput {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

func TestExecuteImplementsCommandUsesDaemonConnectorOnUnix(t *testing.T) {
	workspace := t.TempDir()
	source := "package sample\n\ntype Target interface { run() }\n"
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	helperBinDir := t.TempDir()
	helperScriptPath := filepath.Join(helperBinDir, "gopls")
	if err := os.WriteFile(helperScriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	t.Setenv("PATH", helperBinDir)

	daemonErr := errors.New("daemon connector used")
	connector := &testImplementsDaemonConnector{err: daemonErr}
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return connector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, errors.New("legacy path should not be used on unix routing")
		},
	)

	_, err := executeImplementsCommand(context.Background(), implementsCommandOptions{
		WorkspaceRoot: workspace,
		Symbol:        "Target",
	})
	if !errors.Is(err, daemonErr) {
		t.Fatalf("executeImplementsCommand error = %v, want daemon error %v", err, daemonErr)
	}
	if connector.calls != 1 {
		t.Fatalf("daemon connector calls = %d, want 1", connector.calls)
	}
}

func executeImplementsRootCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetImplementsCommandFlagState(t)
	return executeRootCommand(t, args...)
}

func resetImplementsCommandFlagState(t *testing.T) {
	t.Helper()

	for _, name := range []string{"path"} {
		flag := implementsCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("implements flag %q not found", name)
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", name, err)
		}
		flag.Changed = false
	}

	if helpFlag := implementsCmd.Flags().Lookup("help"); helpFlag != nil {
		if err := helpFlag.Value.Set(helpFlag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", "help", err)
		}
		helpFlag.Changed = false
	}
}

func restoreRunImplementsCommand(t *testing.T, runner func(context.Context, implementsCommandOptions) (string, error)) {
	t.Helper()

	previous := runImplementsCommand
	runImplementsCommand = runner
	t.Cleanup(func() {
		runImplementsCommand = previous
	})
}

type testImplementsDaemonConnector struct {
	calls int
	err   error
}

func (c *testImplementsDaemonConnector) Connect(_ context.Context, _, _ string) (lsp.DaemonConnection, error) {
	c.calls++
	if c.err != nil {
		return lsp.DaemonConnection{}, c.err
	}
	return lsp.DaemonConnection{}, nil
}
