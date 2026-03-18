package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/blankbytes/codesight/pkg/lsp"
)

const (
	refsLSPHelperArg          = "refs-lsp-helper-process"
	refsLSPHelperWorkspaceEnv = "CODESIGHT_REFS_HELPER_WORKSPACE"
)

func TestRefsCommandRequiresSymbolArgument(t *testing.T) {
	_, _, err := executeRefsRootCommand(t, "refs")
	if err == nil {
		t.Fatal("expected missing symbol argument error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRefsCommandPassesThroughOutputAndNormalizedInputs(t *testing.T) {
	var got refsCommandOptions
	restoreRunRefsCommand(t, func(_ context.Context, opts refsCommandOptions) (string, error) {
		got = opts
		return "pkg/main.go:10  ->  Target()\n1 references found", nil
	})

	tempDir := t.TempDir()
	stdout, _, err := executeRefsRootCommand(
		t,
		"refs",
		"Target",
		"--path",
		tempDir,
		"--kind",
		"MeThOd",
	)
	if err != nil {
		t.Fatalf("refs command returned error: %v", err)
	}

	if stdout != "pkg/main.go:10  ->  Target()\n1 references found" {
		t.Fatalf("unexpected output: %q", stdout)
	}
	if got.Symbol != "Target" {
		t.Fatalf("symbol = %q, want %q", got.Symbol, "Target")
	}
	if got.Kind != "method" {
		t.Fatalf("kind = %q, want %q", got.Kind, "method")
	}
	wantRoot := tempDir
	if resolved, err := filepath.EvalSymlinks(tempDir); err == nil {
		wantRoot = resolved
	}
	if got.WorkspaceRoot != wantRoot {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, wantRoot)
	}
}

func TestRefsCommandDefaultsPathToWorkingDirectory(t *testing.T) {
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

	var got refsCommandOptions
	restoreRunRefsCommand(t, func(_ context.Context, opts refsCommandOptions) (string, error) {
		got = opts
		return "pkg/main.go:10  ->  Target()\n1 references found", nil
	})

	if _, _, err := executeRefsRootCommand(t, "refs", "Target"); err != nil {
		t.Fatalf("refs command returned error: %v", err)
	}
	if got.WorkspaceRoot != resolvedWorkingDirectory {
		t.Fatalf("workspace root = %q, want %q", got.WorkspaceRoot, resolvedWorkingDirectory)
	}
}

func TestRefsCommandInvalidKindExactError(t *testing.T) {
	called := false
	restoreRunRefsCommand(t, func(_ context.Context, _ refsCommandOptions) (string, error) {
		called = true
		return "", nil
	})

	_, _, err := executeRefsRootCommand(t, "refs", "Target", "--kind", "variable")
	if err == nil {
		t.Fatal("expected invalid kind error, got nil")
	}

	want := `invalid kind "variable" — allowed: function, method, class, interface, type, constant`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if called {
		t.Fatal("refs runner should not be called for invalid kind")
	}
}

func TestRefsCommandAmbiguousErrorPassThrough(t *testing.T) {
	want := strings.Join([]string{
		`ambiguous symbol "Target" — 2 definitions found. Use --path to narrow scope.`,
		"  - a/file.go:10 (function)",
		"  - z/file.go:4 (method)",
	}, "\n")

	restoreRunRefsCommand(t, func(_ context.Context, _ refsCommandOptions) (string, error) {
		return "", errors.New(want)
	})

	_, _, err := executeRefsRootCommand(t, "refs", "Target")
	if err == nil {
		t.Fatal("expected ambiguous symbol error, got nil")
	}
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestRefsCommandFallbackOutputIncludesGrepNote(t *testing.T) {
	restoreRunRefsCommand(t, func(_ context.Context, _ refsCommandOptions) (string, error) {
		return "(grep-based - install gopls for precise results)\nmain.go:2  ->  Target()\n1 references found", nil
	})

	stdout, _, err := executeRefsRootCommand(t, "refs", "Target")
	if err != nil {
		t.Fatalf("refs command returned error: %v", err)
	}
	if !strings.Contains(stdout, "(grep-based - install gopls for precise results)") {
		t.Fatalf("output missing required grep fallback note: %q", stdout)
	}
}

func TestRefsCommandMissingLSPBinaryFallsBackToGrep(t *testing.T) {
	restoreRunRefsCommand(t, executeRefsCommand)

	workspace := t.TempDir()
	source := "package sample\n\nfunc Target() {}\n"
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	t.Setenv("PATH", "")
	t.Setenv("CODESIGHT_STATE_DIR", t.TempDir())

	stdout, _, err := executeRefsRootCommand(t, "refs", "Target", "--path", workspace)
	if err != nil {
		t.Fatalf("expected grep fallback, got error: %v", err)
	}
	if !strings.Contains(stdout, "grep-based") {
		t.Fatalf("output %q does not include grep-based precision note", stdout)
	}
	if !strings.Contains(stdout, "Target") {
		t.Fatalf("output %q does not include the searched symbol", stdout)
	}
}

func TestExecuteRefsCommandUsesLSPOutputWhenServerAvailable(t *testing.T) {
	workspace := t.TempDir()
	source := strings.Join([]string{
		"package sample",
		"",
		"func Target() {",
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
	daemonConnector := &testRefsDaemonConnector{err: errors.New("daemon connector should not be called on windows routing")}
	restoreLSPRuntimeHooks(
		t,
		"windows",
		func(_ *lsp.Registry) lspDaemonConnector { return daemonConnector },
		startRefsLSPClient,
	)

	output, err := executeRefsCommand(context.Background(), refsCommandOptions{
		WorkspaceRoot: workspace,
		Symbol:        "Target",
	})
	if err != nil {
		t.Fatalf("executeRefsCommand returned error: %v", err)
	}

	if strings.Contains(output, "(grep-based - install") {
		t.Fatalf("output unexpectedly contains fallback note: %q", output)
	}
	if !strings.Contains(output, "main.go:4  ->  Target()") {
		t.Fatalf("output missing LSP-derived reference line: %q", output)
	}
	if !strings.Contains(output, "1 references found") {
		t.Fatalf("output missing summary line: %q", output)
	}
	if daemonConnector.calls != 0 {
		t.Fatalf("daemon connector calls = %d, want 0 on windows routing", daemonConnector.calls)
	}
}

func TestExecuteRefsCommandReusesDaemonBetweenInvocations(t *testing.T) {
	workspace := t.TempDir()
	source := strings.Join([]string{
		"package sample",
		"",
		"func Target() {",
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
		"#!/bin/sh\nexec %q -test.run=TestRefsCommandLSPHelperProcess -- %s\n",
		testBinary,
		refsLSPHelperArg,
	)
	if err := os.WriteFile(helperScriptPath, []byte(helperScript), 0o700); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	t.Setenv("PATH", helperBinDir)
	t.Setenv(refsLSPHelperWorkspaceEnv, workspace)

	daemonConnector := &testRefsBridgeDaemonConnector{registry: lsp.NewRegistry()}
	legacyErr := errors.New("legacy startup should not be used in daemon-routing test")
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return daemonConnector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, legacyErr
		},
	)

	opts := refsCommandOptions{
		WorkspaceRoot: workspace,
		Symbol:        "Target",
	}
	firstOutput, err := executeRefsCommand(context.Background(), opts)
	if err != nil {
		t.Fatalf("first executeRefsCommand returned error: %v", err)
	}

	secondOutput, err := executeRefsCommand(context.Background(), opts)
	if err != nil {
		t.Fatalf("second executeRefsCommand returned error: %v", err)
	}
	if daemonConnector.calls != 2 {
		t.Fatalf("daemon connector calls = %d, want 2", daemonConnector.calls)
	}
	if daemonConnector.reusedCalls != 1 {
		t.Fatalf("daemon connector reused calls = %d, want 1", daemonConnector.reusedCalls)
	}
	for _, seenWorkspace := range daemonConnector.workspaces {
		if seenWorkspace != workspace {
			t.Fatalf("daemon connector workspace = %q, want %q", seenWorkspace, workspace)
		}
	}
	for _, seenLanguage := range daemonConnector.languages {
		if seenLanguage != "go" {
			t.Fatalf("daemon connector language = %q, want %q", seenLanguage, "go")
		}
	}

	for _, output := range []string{firstOutput, secondOutput} {
		if strings.Contains(output, "(grep-based - install") {
			t.Fatalf("refs output unexpectedly used grep fallback: %q", output)
		}
		if !strings.Contains(output, "main.go:4  ->  Target()") {
			t.Fatalf("refs output missing LSP-derived reference line: %q", output)
		}
		if !strings.Contains(output, "1 references found") {
			t.Fatalf("refs output missing summary line: %q", output)
		}
	}
}

func TestExecuteRefsCommandEmitsColdStartHintForSlowJavaDaemonStart(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "Main.java"), []byte("class Main { void Target() {} }\n"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	helperBinDir := t.TempDir()
	helperScriptPath := filepath.Join(helperBinDir, "jdtls")
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

	daemonConnector := &testRefsHintDaemonConnector{
		registry: lsp.NewRegistry(),
		delay:    25 * time.Millisecond,
		reused:   false,
	}
	legacyErr := errors.New("legacy startup should not be used in daemon-routing test")
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return daemonConnector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, legacyErr
		},
	)

	previousThreshold := refsColdStartHintThreshold
	refsColdStartHintThreshold = 10 * time.Millisecond
	t.Cleanup(func() {
		refsColdStartHintThreshold = previousThreshold
	})

	output, err := executeRefsCommand(context.Background(), refsCommandOptions{
		WorkspaceRoot: workspace,
		Symbol:        "Target",
	})
	if err != nil {
		t.Fatalf("executeRefsCommand returned error: %v", err)
	}

	hint := "Tip: run 'cs warmup .' to pre-start the language server"
	if !strings.Contains(output, hint) {
		t.Fatalf("refs output missing cold-start hint: %q", output)
	}
}

func TestExecuteRefsCommandDoesNotEmitColdStartHintForWarmJavaDaemon(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "Main.java"), []byte("class Main { void Target() {} }\n"), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	helperBinDir := t.TempDir()
	helperScriptPath := filepath.Join(helperBinDir, "jdtls")
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

	daemonConnector := &testRefsHintDaemonConnector{
		registry: lsp.NewRegistry(),
		delay:    25 * time.Millisecond,
		reused:   true,
	}
	legacyErr := errors.New("legacy startup should not be used in daemon-routing test")
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return daemonConnector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, legacyErr
		},
	)

	previousThreshold := refsColdStartHintThreshold
	refsColdStartHintThreshold = 10 * time.Millisecond
	t.Cleanup(func() {
		refsColdStartHintThreshold = previousThreshold
	})

	output, err := executeRefsCommand(context.Background(), refsCommandOptions{
		WorkspaceRoot: workspace,
		Symbol:        "Target",
	})
	if err != nil {
		t.Fatalf("executeRefsCommand returned error: %v", err)
	}

	hint := "Tip: run 'cs warmup .' to pre-start the language server"
	if strings.Contains(output, hint) {
		t.Fatalf("refs output unexpectedly included cold-start hint on warm daemon: %q", output)
	}
}

func TestExecuteRefsCommandDoesNotEmitColdStartHintForNonJavaLanguage(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package sample\nfunc Target() {}\n"), 0o600); err != nil {
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

	daemonConnector := &testRefsHintDaemonConnector{
		registry: lsp.NewRegistry(),
		delay:    25 * time.Millisecond,
		reused:   false,
	}
	legacyErr := errors.New("legacy startup should not be used in daemon-routing test")
	restoreLSPRuntimeHooks(
		t,
		"linux",
		func(_ *lsp.Registry) lspDaemonConnector { return daemonConnector },
		func(_ context.Context, _ lsp.ServerSpec, _ string) (*lsp.Client, error) {
			return nil, legacyErr
		},
	)

	previousThreshold := refsColdStartHintThreshold
	refsColdStartHintThreshold = 10 * time.Millisecond
	t.Cleanup(func() {
		refsColdStartHintThreshold = previousThreshold
	})

	output, err := executeRefsCommand(context.Background(), refsCommandOptions{
		WorkspaceRoot: workspace,
		Symbol:        "Target",
	})
	if err != nil {
		t.Fatalf("executeRefsCommand returned error: %v", err)
	}

	hint := "Tip: run 'cs warmup .' to pre-start the language server"
	if strings.Contains(output, hint) {
		t.Fatalf("refs output unexpectedly included cold-start hint for non-java flow: %q", output)
	}
}

func executeRefsRootCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetRefsCommandFlagState(t)
	return executeRootCommand(t, args...)
}

func resetRefsCommandFlagState(t *testing.T) {
	t.Helper()

	for _, name := range []string{"path", "kind"} {
		flag := refsCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("refs flag %q not found", name)
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", name, err)
		}
		flag.Changed = false
	}

	if helpFlag := refsCmd.Flags().Lookup("help"); helpFlag != nil {
		if err := helpFlag.Value.Set(helpFlag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", "help", err)
		}
		helpFlag.Changed = false
	}
}

func restoreRunRefsCommand(t *testing.T, runner func(context.Context, refsCommandOptions) (string, error)) {
	t.Helper()

	previous := runRefsCommand
	runRefsCommand = runner
	t.Cleanup(func() {
		runRefsCommand = previous
	})
}

func TestRefsCommandLSPHelperProcess(t *testing.T) {
	if !hasRefsHelperArg(refsLSPHelperArg) {
		return
	}

	if err := runRefsLSPHelperProcess(os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

type refsRPCEnvelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
}

type refsRPCResponse struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      json.RawMessage        `json:"id"`
	Result  json.RawMessage        `json:"result,omitempty"`
	Error   *lsp.ResponseErrorBody `json:"error,omitempty"`
}

var errRefsHelperIdleTimeout = errors.New("refs helper idle timeout")

func runRefsLSPHelperProcess(stdin io.Reader, stdout io.Writer) error {
	workspaceRoot := strings.TrimSpace(os.Getenv(refsLSPHelperWorkspaceEnv))
	if workspaceRoot == "" {
		return fmt.Errorf("%s is required", refsLSPHelperWorkspaceEnv)
	}

	targetURI, err := refsTestFileURI(filepath.Join(workspaceRoot, "main.go"))
	if err != nil {
		return err
	}

	reader := bufio.NewReader(stdin)
	writer := bufio.NewWriter(stdout)

	for {
		payload, err := readRefsLSPMessageWithTimeout(reader, 500*time.Millisecond)
		if err != nil {
			if errors.Is(err, errRefsHelperIdleTimeout) || errors.Is(err, io.EOF) {
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
				"capabilities": map[string]any{},
				"serverInfo": map[string]any{
					"name": "refs-helper",
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
		case lsp.MethodTextDocumentReferences:
			result, err := json.Marshal([]lsp.Location{
				{
					URI: targetURI,
					Range: lsp.Range{
						Start: lsp.Position{Line: 3, Character: 4},
						End:   lsp.Position{Line: 3, Character: 10},
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

func readRefsLSPMessageWithTimeout(reader *bufio.Reader, timeout time.Duration) ([]byte, error) {
	type readResult struct {
		payload []byte
		err     error
	}

	resultCh := make(chan readResult, 1)
	go func() {
		payload, err := readRefsLSPMessage(reader)
		resultCh <- readResult{payload: payload, err: err}
	}()

	select {
	case result := <-resultCh:
		return result.payload, result.err
	case <-time.After(timeout):
		return nil, errRefsHelperIdleTimeout
	}
}

func readRefsLSPMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			continue
		}

		parsed, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, err
		}
		contentLength = parsed
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeRefsLSPMessage(writer *bufio.Writer, response refsRPCResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}

func hasRefsHelperArg(target string) bool {
	for _, arg := range os.Args {
		if arg == target {
			return true
		}
	}
	return false
}

func refsTestFileURI(path string) (lsp.DocumentURI, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	normalized := filepath.ToSlash(absPath)
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}

	return lsp.DocumentURI((&url.URL{
		Scheme: "file",
		Path:   normalized,
	}).String()), nil
}

type testRefsDaemonConnector struct {
	calls int
	err   error
}

func (c *testRefsDaemonConnector) Connect(_ context.Context, _, _ string) (lsp.DaemonConnection, error) {
	c.calls++
	if c.err != nil {
		return lsp.DaemonConnection{}, c.err
	}
	return lsp.DaemonConnection{}, nil
}

type testRefsBridgeDaemonConnector struct {
	registry *lsp.Registry

	calls       int
	reusedCalls int
	workspaces  []string
	languages   []string
}

func (c *testRefsBridgeDaemonConnector) Connect(ctx context.Context, workspaceRoot, language string) (lsp.DaemonConnection, error) {
	c.calls++
	if c.calls > 1 {
		c.reusedCalls++
	}
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
			PID:           9_002,
			Reused:        c.calls > 1,
		},
	}, nil
}

type testRefsHintDaemonConnector struct {
	registry *lsp.Registry
	delay    time.Duration
	reused   bool
}

func (c *testRefsHintDaemonConnector) Connect(
	ctx context.Context,
	workspaceRoot string,
	language string,
) (lsp.DaemonConnection, error) {
	if c.delay > 0 {
		time.Sleep(c.delay)
	}

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
			PID:           9_005,
			Reused:        c.reused,
		},
	}, nil
}
