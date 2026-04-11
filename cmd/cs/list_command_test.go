package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListCommandRequiresFileFlag(t *testing.T) {
	_, _, err := executeListRootCommand(t, "list")
	if err == nil {
		t.Fatal("expected missing file flag error, got nil")
	}
	if !strings.Contains(err.Error(), `required flag(s) "file" not set`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestListCommandHelpShowsRequiredSignature(t *testing.T) {
	stdout, stderr, err := executeListRootCommand(t, "list", "--help")
	if err != nil {
		t.Fatalf("list help returned error: %v", err)
	}

	combined := stdout + "\n" + stderr
	if !strings.Contains(combined, "cs list -f <file>") {
		t.Fatalf("help text missing required signature, got:\n%s", combined)
	}
}

func TestListCommandDefaultFormatIsRawWithLOC(t *testing.T) {
	path := extractFixturePath(t, "languages", "sample.go")

	stdout, stderr, err := executeListRootCommand(t, "list", "-f", path, "-l", "go")
	if err != nil {
		t.Fatalf("list command returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	want := "function\tGoTarget\tL3-L5\tLOC=3"
	if stdout != want {
		t.Fatalf("raw output mismatch\n got: %q\nwant: %q", stdout, want)
	}
}

func TestListCommandJSONFormatIncludesLOC(t *testing.T) {
	path := extractFixturePath(t, "languages", "sample.go")

	stdout, _, err := executeListRootCommand(t, "list", "-f", path, "--format", "json")
	if err != nil {
		t.Fatalf("list command returned error: %v", err)
	}

	var payload []map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, stdout)
	}
	if len(payload) != 1 {
		t.Fatalf("json symbol count = %d, want 1", len(payload))
	}
	if got := payload[0]["loc"]; got != float64(3) {
		t.Fatalf("loc = %v, want 3", got)
	}
}

func TestListCommandDirectoryWarnsAndContinuesOnFileError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based read errors are not portable on Windows")
	}

	dir := t.TempDir()
	good := filepath.Join(dir, "good.go")
	bad := filepath.Join(dir, "bad.go")

	if err := os.WriteFile(good, []byte("package main\n\nfunc Good() {}\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(good) returned error: %v", err)
	}
	if err := os.WriteFile(bad, []byte("package main\n\nfunc Bad() {}\n"), 0o000); err != nil {
		t.Fatalf("os.WriteFile(bad) returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	stdout, stderr, err := executeListRootCommand(t, "list", "-f", dir, "-l", "go")
	if err != nil {
		t.Fatalf("list command returned error: %v", err)
	}

	if !strings.Contains(stdout, "good.go\tfunction\tGood\tL3-L3\tLOC=1") {
		t.Fatalf("stdout missing good symbol line: %q", stdout)
	}
	if !strings.Contains(stderr, "warning: failed to list symbols in") {
		t.Fatalf("stderr missing warning line: %q", stderr)
	}
}

func TestListCommandSummaryDirectory(t *testing.T) {
	dir := extractFixturePath(t, "directory")

	stdout, stderr, err := executeListRootCommand(t, "list", "-f", dir, "--summary")
	if err != nil {
		t.Fatalf("list command returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	if !strings.Contains(stdout, "(2 files, 9 lines)") {
		t.Fatalf("summary header mismatch: %q", stdout)
	}
	if !strings.Contains(stdout, "alpha.go") || !strings.Contains(stdout, "function(1)") {
		t.Fatalf("summary output missing alpha.go counts: %q", stdout)
	}
	if !strings.Contains(stdout, "nested/bravo.py") || !strings.Contains(stdout, "function(1)") {
		t.Fatalf("summary output missing nested/bravo.py counts: %q", stdout)
	}
}

func TestListCommandSummaryFileTargetErrors(t *testing.T) {
	path := extractFixturePath(t, "languages", "sample.go")

	_, _, err := executeListRootCommand(t, "list", "-f", path, "--summary")
	if err == nil {
		t.Fatal("expected summary file-target error, got nil")
	}
	if !strings.Contains(err.Error(), "--summary requires --file to point to a directory") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func executeListRootCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetListCommandFlagState(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)

	_, err := rootCmd.ExecuteC()
	rootCmd.SetArgs(nil)
	return stdout.String(), stderr.String(), err
}

func resetListCommandFlagState(t *testing.T) {
	t.Helper()

	for _, name := range []string{"file", "lang", "format", "type", "summary"} {
		flag := listCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("list flag %q not found", name)
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", name, err)
		}
		flag.Changed = false
	}

	if helpFlag := listCmd.Flags().Lookup("help"); helpFlag != nil {
		if err := helpFlag.Value.Set(helpFlag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", "help", err)
		}
		helpFlag.Changed = false
	}
}
