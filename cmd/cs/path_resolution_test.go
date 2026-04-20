package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codesight/pkg/lsp"
)

func TestResolveCommandTargetDirDefaultsToWorkingDirectory(t *testing.T) {
	workingDirectory := t.TempDir()
	setWorkingDirectory(t, workingDirectory)

	got, err := resolveCommandTargetDir("")
	if err != nil {
		t.Fatalf("resolveCommandTargetDir returned error: %v", err)
	}

	want := mustResolvePath(t, workingDirectory)
	if got != want {
		t.Fatalf("resolveCommandTargetDir(\"\") = %q, want %q", got, want)
	}
}

func TestResolveCommandTargetDirFileInputUsesParentDirectory(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "pkg", "main.go")
	writeTestFile(t, filePath, "package sample\n")

	got, err := resolveCommandTargetDir(filePath)
	if err != nil {
		t.Fatalf("resolveCommandTargetDir returned error: %v", err)
	}

	want := mustResolvePath(t, filepath.Dir(filePath))
	if got != want {
		t.Fatalf("resolveCommandTargetDir(%q) = %q, want %q", filePath, got, want)
	}
}

func TestResolveCommandTargetDirResolvesSymlinkDirectories(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	linkDir := filepath.Join(base, "link")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	got, err := resolveCommandTargetDir(linkDir)
	if err != nil {
		t.Fatalf("resolveCommandTargetDir returned error: %v", err)
	}

	want := mustResolvePath(t, realDir)
	if got != want {
		t.Fatalf("resolveCommandTargetDir(%q) = %q, want %q", linkDir, got, want)
	}
}

func TestRefsCommandNestedPathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	repoRoot, nestedDir, resolvedRepoRoot, resolvedNestedDir := createNestedProject(t)
	setWorkingDirectory(t, t.TempDir())

	var got refsCommandOptions
	restoreRunRefsCommand(t, func(_ context.Context, opts refsCommandOptions) (string, error) {
		got = opts
		return "1 references found", nil
	})

	_, _, err := executeRefsRootCommand(t, "refs", "Target", "--path", nestedDir)
	if err != nil {
		t.Fatalf("refs command returned error: %v", err)
	}

	if got.WorkspaceRoot != resolvedRepoRoot {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedRepoRoot)
	}
	if got.FilterPath != resolvedNestedDir {
		t.Fatalf("filter path = %q, want %q", got.FilterPath, resolvedNestedDir)
	}
	if got.Symbol != "Target" {
		t.Fatalf("symbol = %q, want %q", got.Symbol, "Target")
	}
	_ = repoRoot
}

func TestCallersCommandNestedPathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	_, nestedDir, resolvedRepoRoot, resolvedNestedDir := createNestedProject(t)
	setWorkingDirectory(t, t.TempDir())

	var got callersCommandOptions
	restoreRunCallersCommand(t, func(_ context.Context, opts callersCommandOptions) (string, error) {
		got = opts
		return "0 callers (depth 1)", nil
	})

	_, _, err := executeCallersRootCommand(t, "callers", "Target", "--path", nestedDir)
	if err != nil {
		t.Fatalf("callers command returned error: %v", err)
	}

	if got.WorkspaceRoot != resolvedRepoRoot {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedRepoRoot)
	}
	if got.FilterPath != resolvedNestedDir {
		t.Fatalf("filter path = %q, want %q", got.FilterPath, resolvedNestedDir)
	}
}

func TestImplementsCommandNestedPathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	_, nestedDir, resolvedRepoRoot, resolvedNestedDir := createNestedProject(t)
	setWorkingDirectory(t, t.TempDir())

	var got implementsCommandOptions
	restoreRunImplementsCommand(t, func(_ context.Context, opts implementsCommandOptions) (string, error) {
		got = opts
		return "0 implementations", nil
	})

	_, _, err := executeImplementsRootCommand(t, "implements", "Target", "--path", nestedDir)
	if err != nil {
		t.Fatalf("implements command returned error: %v", err)
	}

	if got.WorkspaceRoot != resolvedRepoRoot {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedRepoRoot)
	}
	if got.FilterPath != resolvedNestedDir {
		t.Fatalf("filter path = %q, want %q", got.FilterPath, resolvedNestedDir)
	}
}

func TestConfigCommandNestedPathUsesNearestProjectConfig(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	repoRoot, nestedDir, resolvedRepoRoot, _ := createNestedProject(t)
	writeTestFile(t, filepath.Join(repoRoot, ".codesight", "config.toml"), `
[embedding]
model = "project-model"
`)
	setWorkingDirectory(t, t.TempDir())

	stdout, _, err := executeRootCommand(t, "config", nestedDir)
	if err != nil {
		t.Fatalf("config command returned error: %v", err)
	}

	entries := parseConfigOutput(t, stdout)
	if got := entries["embedding.model"]; got.Value != "project-model" || got.Source != ".codesight/config.toml" {
		t.Fatalf("embedding.model = %#v, want value project-model with source .codesight/config.toml", got)
	}
	if got := entries["project_root"]; got.Value != resolvedRepoRoot || got.Source != "default" {
		t.Fatalf("project_root = %#v, want value %q with source default", got, resolvedRepoRoot)
	}
}

func TestLSPWarmupNestedPathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	repoRoot, nestedDir, resolvedRepoRoot, _ := createNestedProject(t)
	writeTestFile(t, filepath.Join(repoRoot, "main.go"), "package sample\n")
	setWorkingDirectory(t, t.TempDir())

	daemonConnector := setupLSPWarmupTestRuntime(t, resolvedRepoRoot)

	stdout, _, err := executeRootCommand(t, "lsp", "warmup", nestedDir)
	if err != nil {
		t.Fatalf("warmup command returned error: %v", err)
	}

	want := fmt.Sprintf("LSP warmup ready (go): %s", resolvedRepoRoot)
	if got := trimCommandOutput(stdout); got != want {
		t.Fatalf("warmup output = %q, want %q", got, want)
	}
	if len(daemonConnector.workspaces) != 1 || daemonConnector.workspaces[0] != resolvedRepoRoot {
		t.Fatalf("daemon connector workspaces = %#v, want [%q]", daemonConnector.workspaces, resolvedRepoRoot)
	}
}

func TestLSPStatusNestedPathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	_, nestedDir, resolvedRepoRoot, _ := createNestedProject(t)
	setWorkingDirectory(t, t.TempDir())
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())

	stdout, _, err := executeRootCommand(t, "lsp", "status", nestedDir)
	if err != nil {
		t.Fatalf("status command returned error: %v", err)
	}

	want := fmt.Sprintf("No LSP daemon found for %s", resolvedRepoRoot)
	if got := trimCommandOutput(stdout); got != want {
		t.Fatalf("status output = %q, want %q", got, want)
	}
}

func TestLSPRestartNestedPathUsesNearestProjectRoot(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	repoRoot, nestedDir, resolvedRepoRoot, _ := createNestedProject(t)
	writeTestFile(t, filepath.Join(repoRoot, "main.go"), "package sample\n")
	setWorkingDirectory(t, t.TempDir())

	daemonConnector := setupLSPWarmupTestRuntime(t, resolvedRepoRoot)

	stdout, _, err := executeRootCommand(t, "lsp", "restart", nestedDir)
	if err != nil {
		t.Fatalf("restart command returned error: %v", err)
	}

	want := fmt.Sprintf("LSP daemon restarted (go): %s", resolvedRepoRoot)
	if got := trimCommandOutput(stdout); got != want {
		t.Fatalf("restart output = %q, want %q", got, want)
	}
	if len(daemonConnector.workspaces) != 1 || daemonConnector.workspaces[0] != resolvedRepoRoot {
		t.Fatalf("daemon connector workspaces = %#v, want [%q]", daemonConnector.workspaces, resolvedRepoRoot)
	}
}

func createNestedProject(t *testing.T) (string, string, string, string) {
	t.Helper()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	nestedDir := filepath.Join(repoRoot, "services", "api")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", nestedDir, err)
	}
	writeTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeTestFile(t, filepath.Join(repoRoot, ".codesight", "config.toml"), "")

	return repoRoot, nestedDir, mustResolvePath(t, repoRoot), mustResolvePath(t, nestedDir)
}

func setupLSPWarmupTestRuntime(t *testing.T, workspaceRoot string) *testWarmupBridgeDaemonConnector {
	t.Helper()

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
	t.Setenv(refsLSPHelperWorkspaceEnv, workspaceRoot)

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

	return daemonConnector
}

func setWorkingDirectory(t *testing.T, dir string) {
	t.Helper()

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previousWD); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	})
}

func mustResolvePath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", path, err)
	}
	if evalResolved, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
		resolved = evalResolved
	}
	return filepath.Clean(resolved)
}

func trimCommandOutput(output string) string {
	return strings.TrimSpace(output)
}
