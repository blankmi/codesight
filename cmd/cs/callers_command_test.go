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
	"strings"
	"testing"
	"time"

	"github.com/blankbytes/codesight/pkg/lsp"
)

const (
	callersLSPHelperArg          = "callers-lsp-helper-process"
	callersLSPHelperWorkspaceEnv = "CODESIGHT_CALLERS_HELPER_WORKSPACE"
)

func TestCallersCommandRequiresSymbolArgument(t *testing.T) {
	_, _, err := executeCallersRootCommand(t, "callers")
	if err == nil {
		t.Fatal("expected missing symbol argument error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCallersCommandPassesThroughOutputAndParsedInputs(t *testing.T) {
	var got callersCommandOptions
	restoreRunCallersCommand(t, func(_ context.Context, opts callersCommandOptions) (string, error) {
		got = opts
		return strings.Join([]string{
			"Target (pkg/main.go:10)",
			"  <- callerOne (pkg/main.go:40)",
			"1 callers (depth 2)",
		}, "\n"), nil
	})

	workspace := t.TempDir()
	stdout, _, err := executeCallersRootCommand(
		t,
		"callers",
		"Target",
		"--path",
		workspace,
		"--depth",
		"2",
	)
	if err != nil {
		t.Fatalf("callers command returned error: %v", err)
	}

	wantOutput := strings.Join([]string{
		"Target (pkg/main.go:10)",
		"  <- callerOne (pkg/main.go:40)",
		"1 callers (depth 2)",
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
	if got.Depth != 2 {
		t.Fatalf("depth = %d, want 2", got.Depth)
	}
}

func TestCallersCommandDefaultsPathAndDepth(t *testing.T) {
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

	var got callersCommandOptions
	restoreRunCallersCommand(t, func(_ context.Context, opts callersCommandOptions) (string, error) {
		got = opts
		return "Target (pkg/main.go:10)\n0 callers (depth 1)", nil
	})

	if _, _, err := executeCallersRootCommand(t, "callers", "Target"); err != nil {
		t.Fatalf("callers command returned error: %v", err)
	}
	if got.WorkspaceRoot != resolvedWorkingDirectory {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedWorkingDirectory)
	}
	if got.Depth != 1 {
		t.Fatalf("depth = %d, want 1", got.Depth)
	}
}

func TestCallersCommandCustomDepthPropagation(t *testing.T) {
	var got callersCommandOptions
	restoreRunCallersCommand(t, func(_ context.Context, opts callersCommandOptions) (string, error) {
		got = opts
		return "Target (pkg/main.go:10)\n0 callers (depth 3)", nil
	})

	if _, _, err := executeCallersRootCommand(t, "callers", "Target", "--depth", "3"); err != nil {
		t.Fatalf("callers command returned error: %v", err)
	}
	if got.Depth != 3 {
		t.Fatalf("depth = %d, want 3", got.Depth)
	}
}

func TestCallersCommandInvalidDepthFailsFast(t *testing.T) {
	called := false
	restoreRunCallersCommand(t, func(_ context.Context, _ callersCommandOptions) (string, error) {
		called = true
		return "", nil
	})

	_, _, err := executeCallersRootCommand(t, "callers", "Target", "--depth", "0")
	if err == nil {
		t.Fatal("expected invalid depth error, got nil")
	}
	if err.Error() != "depth must be a positive integer" {
		t.Fatalf("error = %q, want %q", err.Error(), "depth must be a positive integer")
	}
	if called {
		t.Fatal("callers runner should not be called for non-positive depth")
	}
}

func TestCallersCommandAmbiguousErrorPassThrough(t *testing.T) {
	want := strings.Join([]string{
		`ambiguous symbol "Target" — 2 definitions found. Use --path to narrow scope.`,
		"  - a/file.go:10 (function)",
		"  - z/file.go:4 (method)",
	}, "\n")

	restoreRunCallersCommand(t, func(_ context.Context, _ callersCommandOptions) (string, error) {
		return "", errors.New(want)
	})

	_, _, err := executeCallersRootCommand(t, "callers", "Target")
	if err == nil {
		t.Fatal("expected ambiguous symbol error, got nil")
	}
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestCallersCommandMissingLSPBinaryExactError(t *testing.T) {
	restoreRunCallersCommand(t, executeCallersCommand)

	workspace := t.TempDir()
	source := "package sample\n\nfunc Target() {}\n"
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	t.Setenv("PATH", "")
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())

	_, _, err := executeCallersRootCommand(t, "callers", "Target", "--path", workspace)
	if err == nil {
		t.Fatal("expected missing LSP binary error, got nil")
	}

	want := "cs callers: LSP required but gopls not found. Install: go install golang.org/x/tools/gopls@latest"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestCallersCommandOutputPassThroughFormatContract(t *testing.T) {
	want := strings.Join([]string{
		"Target (pkg/main.go:3)",
		"  <- callerOne (pkg/main.go:9)",
		"    <- callerTwo (pkg/main.go:20)",
		"2 callers (depth 2)",
	}, "\n")
	restoreRunCallersCommand(t, func(_ context.Context, _ callersCommandOptions) (string, error) {
		return want, nil
	})

	stdout, _, err := executeCallersRootCommand(t, "callers", "Target", "--depth", "2")
	if err != nil {
		t.Fatalf("callers command returned error: %v", err)
	}
	if stdout != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout, want)
	}
}

func TestExecuteCallersCommandReusesLifecycleBetweenInvocations(t *testing.T) {
	workspace := t.TempDir()
	source := strings.Join([]string{
		"package sample",
		"",
		"func Target() {}",
		"",
		"func Caller() {",
		"    Target()",
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	helperBinDir := t.TempDir()
	helperScriptPath := filepath.Join(helperBinDir, "gopls")
	helperScript := fmt.Sprintf(
		"#!/bin/sh\nexec %q -test.run=TestCallersCommandLSPHelperProcess -- %s\n",
		testBinary,
		callersLSPHelperArg,
	)
	if err := os.WriteFile(helperScriptPath, []byte(helperScript), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	stateDir := t.TempDir()
	t.Setenv("PATH", helperBinDir)
	t.Setenv("CODESIGHT_STATE_DIR", stateDir)
	t.Setenv(callersLSPHelperWorkspaceEnv, workspace)
	t.Cleanup(func() {
		_ = lsp.NewLifecycle(lsp.NewRegistry()).Stop(workspace, "go")
	})

	opts := callersCommandOptions{
		WorkspaceRoot: workspace,
		Symbol:        "Target",
		Depth:         1,
	}

	firstOutput, err := executeCallersCommand(context.Background(), opts)
	if err != nil {
		t.Fatalf("first executeCallersCommand returned error: %v", err)
	}

	secondOutput, err := executeCallersCommand(context.Background(), opts)
	if err != nil {
		t.Fatalf("second executeCallersCommand returned error: %v", err)
	}

	for _, output := range []string{firstOutput, secondOutput} {
		if !strings.Contains(output, "Target (main.go:3)") {
			t.Fatalf("output missing root line: %q", output)
		}
		if !strings.Contains(output, "  <- Caller (main.go:5)") {
			t.Fatalf("output missing caller line: %q", output)
		}
		if !strings.Contains(output, "1 callers (depth 1)") {
			t.Fatalf("output missing summary line: %q", output)
		}
	}
}

func executeCallersRootCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetCallersCommandFlagState(t)
	return executeRootCommand(t, args...)
}

func resetCallersCommandFlagState(t *testing.T) {
	t.Helper()

	for _, name := range []string{"path", "depth"} {
		flag := callersCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("callers flag %q not found", name)
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", name, err)
		}
		flag.Changed = false
	}

	if helpFlag := callersCmd.Flags().Lookup("help"); helpFlag != nil {
		if err := helpFlag.Value.Set(helpFlag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", "help", err)
		}
		helpFlag.Changed = false
	}
}

func restoreRunCallersCommand(t *testing.T, runner func(context.Context, callersCommandOptions) (string, error)) {
	t.Helper()

	previous := runCallersCommand
	runCallersCommand = runner
	t.Cleanup(func() {
		runCallersCommand = previous
	})
}

func TestCallersCommandLSPHelperProcess(t *testing.T) {
	if !hasCallersHelperArg(callersLSPHelperArg) {
		return
	}
	if err := runCallersLSPHelperProcess(os.Stdin, os.Stdout); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func runCallersLSPHelperProcess(stdin io.Reader, stdout io.Writer) error {
	workspaceRoot := strings.TrimSpace(os.Getenv(callersLSPHelperWorkspaceEnv))
	if workspaceRoot == "" {
		return fmt.Errorf("%s is required", callersLSPHelperWorkspaceEnv)
	}

	targetURI, err := refsTestFileURI(filepath.Join(workspaceRoot, "main.go"))
	if err != nil {
		return err
	}

	reader := bufio.NewReader(stdin)
	writer := bufio.NewWriter(stdout)

	for {
		payload, err := readRefsLSPMessageWithTimeout(reader, 3*time.Second)
		if err != nil {
			if errors.Is(err, errRefsHelperIdleTimeout) {
				return nil
			}
			if errors.Is(err, io.EOF) {
				// Lifecycle probes launch the helper without an LSP transport. Keep the
				// process alive briefly so a second invocation can observe lease reuse.
				time.Sleep(2 * time.Second)
				return nil
			}
			return err
		}

		var envelope refsRPCEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return fmt.Errorf("decode request: %w", err)
		}

		switch envelope.Method {
		case lsp.MethodInitialize:
			result, err := json.Marshal(map[string]any{
				"capabilities": map[string]any{
					"callHierarchyProvider": true,
				},
				"serverInfo": map[string]any{
					"name": "callers-helper",
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
		case lsp.MethodWorkspaceSymbol:
			result, err := json.Marshal([]lsp.SymbolInformation{
				{
					Name: "Target",
					Kind: lsp.SymbolKindFunction,
					Location: lsp.Location{
						URI: targetURI,
						Range: lsp.Range{
							Start: lsp.Position{Line: 2, Character: 0},
							End:   lsp.Position{Line: 2, Character: 6},
						},
					},
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
		case lsp.MethodTextDocumentPrepareCallHierarchy:
			result, err := json.Marshal([]lsp.CallHierarchyItem{
				{
					Name: "Target",
					Kind: lsp.SymbolKindFunction,
					URI:  targetURI,
					Range: lsp.Range{
						Start: lsp.Position{Line: 2, Character: 0},
						End:   lsp.Position{Line: 2, Character: 14},
					},
					SelectionRange: lsp.Range{
						Start: lsp.Position{Line: 2, Character: 5},
						End:   lsp.Position{Line: 2, Character: 11},
					},
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
		case lsp.MethodCallHierarchyIncomingCalls:
			result, err := json.Marshal([]lsp.CallHierarchyIncomingCall{
				{
					From: lsp.CallHierarchyItem{
						Name: "Caller",
						Kind: lsp.SymbolKindFunction,
						URI:  targetURI,
						Range: lsp.Range{
							Start: lsp.Position{Line: 4, Character: 0},
							End:   lsp.Position{Line: 6, Character: 1},
						},
						SelectionRange: lsp.Range{
							Start: lsp.Position{Line: 4, Character: 5},
							End:   lsp.Position{Line: 4, Character: 11},
						},
					},
					FromRanges: []lsp.Range{
						{
							Start: lsp.Position{Line: 5, Character: 4},
							End:   lsp.Position{Line: 5, Character: 10},
						},
					},
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
		case lsp.MethodShutdown:
			result, err := json.Marshal(nil)
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
		case lsp.MethodExit:
			return nil
		default:
			return fmt.Errorf("unsupported method: %s", envelope.Method)
		}
	}
}

func readCallersLifecyclePID(t *testing.T, stateDir, workspaceRoot, language string) int {
	t.Helper()

	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		t.Fatalf("resolve workspace root: %v", err)
	}

	statePath := filepath.Join(stateDir, "lsp", lsp.StateKey(absWorkspace, language)+".json")
	deadline := time.Now().Add(2 * time.Second)

	for {
		payload, err := os.ReadFile(statePath)
		if err == nil {
			var state struct {
				PID int `json:"pid"`
			}
			if err := json.Unmarshal(payload, &state); err != nil {
				t.Fatalf("decode lifecycle state %q: %v", statePath, err)
			}
			if state.PID <= 0 {
				t.Fatalf("lifecycle state %q has invalid pid: %d", statePath, state.PID)
			}
			return state.PID
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read lifecycle state %q: %v", statePath, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("lifecycle state file not found after timeout: %s", statePath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasCallersHelperArg(arg string) bool {
	for _, candidate := range os.Args[1:] {
		if candidate == arg {
			return true
		}
	}
	return false
}
