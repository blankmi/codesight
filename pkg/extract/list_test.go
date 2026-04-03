package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListSymbolsFileModeRawIncludesLOC(t *testing.T) {
	path := fixturePath("languages", "sample.go")

	result, err := ListSymbols(path, "go", "raw", "")
	if err != nil {
		t.Fatalf("ListSymbols returned error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", result.Warnings)
	}

	if got := result.Output; got != "function\tGoTarget\tL3-L5\tLOC=3" {
		t.Fatalf("raw output = %q, want %q", got, "function\tGoTarget\tL3-L5\tLOC=3")
	}
}

func TestListSymbolsFileModeJSONIncludesLOC(t *testing.T) {
	path := fixturePath("languages", "sample.go")

	result, err := ListSymbols(path, "go", "json", "")
	if err != nil {
		t.Fatalf("ListSymbols returned error: %v", err)
	}

	var payload []map[string]any
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, result.Output)
	}
	if len(payload) != 1 {
		t.Fatalf("json symbol count = %d, want 1", len(payload))
	}

	symbol := payload[0]
	if got := symbol["name"]; got != "GoTarget" {
		t.Fatalf("name = %v, want GoTarget", got)
	}
	if got := symbol["symbol_type"]; got != "function" {
		t.Fatalf("symbol_type = %v, want function", got)
	}
	if got := symbol["loc"]; got != float64(3) {
		t.Fatalf("loc = %v, want 3", got)
	}
}

func TestListSymbolsFunctionFilterIncludesMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	content := `package sample

type Target struct{}

func Top() {}

func (Target) Method() {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	result, err := ListSymbols(path, "go", "json", "function")
	if err != nil {
		t.Fatalf("ListSymbols returned error: %v", err)
	}

	var payload []map[string]any
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, result.Output)
	}
	if len(payload) != 2 {
		t.Fatalf("json symbol count = %d, want 2", len(payload))
	}

	types := []string{
		payload[0]["symbol_type"].(string),
		payload[1]["symbol_type"].(string),
	}
	if !(types[0] == "function" && types[1] == "method") {
		t.Fatalf("symbol types = %v, want [function method]", types)
	}
}

func TestListSymbolsDirectoryDeterministicOrderingAndRawPath(t *testing.T) {
	dir := fixturePath("directory")

	result, err := ListSymbols(dir, "", "raw", "")
	if err != nil {
		t.Fatalf("ListSymbols returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != 2 {
		t.Fatalf("raw line count = %d, want 2 (%q)", len(lines), result.Output)
	}

	wantFirst := filepath.ToSlash(filepath.Join(dir, "alpha.go")) + "\tfunction\tDirTarget\tL3-L5\tLOC=3"
	if lines[0] != wantFirst {
		t.Fatalf("first line = %q, want %q", lines[0], wantFirst)
	}
	wantSecond := filepath.ToSlash(filepath.Join(dir, "nested", "bravo.py")) + "\tfunction\tDirTarget\tL1-L2\tLOC=2"
	if lines[1] != wantSecond {
		t.Fatalf("second line = %q, want %q", lines[1], wantSecond)
	}
}

func TestListSymbolsDirectoryLanguageAliasFilter(t *testing.T) {
	dir := fixturePath("directory")

	result, err := ListSymbols(dir, "golang", "json", "")
	if err != nil {
		t.Fatalf("ListSymbols returned error: %v", err)
	}

	var payload []map[string]any
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, result.Output)
	}
	if len(payload) != 1 {
		t.Fatalf("json symbol count = %d, want 1", len(payload))
	}
	if got := payload[0]["file_path"]; got != filepath.ToSlash(filepath.Join(dir, "alpha.go")) {
		t.Fatalf("file_path = %v, want alpha.go", got)
	}
}

func TestListSymbolsDirectoryNoSupportedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	_, err := ListSymbols(dir, "", "raw", "")
	if err == nil {
		t.Fatal("expected no-supported-files error, got nil")
	}
	if !strings.Contains(err.Error(), "no supported files found under") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListSymbolsDirectoryNoSupportedFilesWithLanguageFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.py"), []byte("def f():\n    pass\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	_, err := ListSymbols(dir, "go", "raw", "")
	if err == nil {
		t.Fatal("expected no-supported-files error, got nil")
	}
	if !strings.Contains(err.Error(), `no supported files found under `) || !strings.Contains(err.Error(), `for language "go"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListSymbolsDirectoryWarnsAndContinuesOnFileError(t *testing.T) {
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

	result, err := ListSymbols(dir, "go", "raw", "")
	if err != nil {
		t.Fatalf("ListSymbols returned error: %v", err)
	}

	if !strings.Contains(result.Output, "good.go\tfunction\tGood\tL3-L3\tLOC=1") {
		t.Fatalf("expected good symbol output, got: %q", result.Output)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warning count = %d, want 1 (%v)", len(result.Warnings), result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "warning: failed to list symbols in") {
		t.Fatalf("warning = %q, want prefix", result.Warnings[0])
	}
}

func TestListSymbolsDirectoryGuardedFailureWhenAllFilesFail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based read errors are not portable on Windows")
	}

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package main\n\nfunc Bad() {}\n"), 0o000); err != nil {
		t.Fatalf("os.WriteFile(bad) returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	_, err := ListSymbols(dir, "go", "raw", "")
	if err == nil {
		t.Fatal("expected guarded failure error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to process any files under") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListSymbolsRejectsUnsupportedType(t *testing.T) {
	path := fixturePath("languages", "sample.go")

	_, err := ListSymbols(path, "go", "raw", "constant")
	if err == nil {
		t.Fatal("expected unsupported symbol type error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported symbol type") {
		t.Fatalf("unexpected error: %v", err)
	}
}
