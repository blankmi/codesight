package extract

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSupportedLanguagesExactSet(t *testing.T) {
	want := []string{"go", "python", "java", "javascript", "typescript", "rust", "cpp", "xml", "html"}
	got := SupportedLanguages()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedLanguages() = %v, want %v", got, want)
	}
}

func TestExtractFileModeRawAndJSONContracts(t *testing.T) {
	path := fixturePath("languages", "sample.go")

	rawOut, err := Extract(path, "GoTarget", "raw")
	if err != nil {
		t.Fatalf("Extract raw returned error: %v", err)
	}

	wantRaw := "func GoTarget() string {\n\treturn \"go\"\n}"
	if rawOut != wantRaw {
		t.Fatalf("raw output mismatch\n got: %q\nwant: %q", rawOut, wantRaw)
	}

	jsonOut, err := Extract(path, "GoTarget", "json")
	if err != nil {
		t.Fatalf("Extract json returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, jsonOut)
	}
	assertJSONContractKeys(t, payload)

	if got := payload["name"]; got != "GoTarget" {
		t.Fatalf("name = %v, want GoTarget", got)
	}
	if got := payload["code"]; got != wantRaw {
		t.Fatalf("code = %v, want %q", got, wantRaw)
	}
	if got := payload["file_path"]; got != filepath.ToSlash(path) {
		t.Fatalf("file_path = %v, want %q", got, filepath.ToSlash(path))
	}
	if got := payload["symbol_type"]; got != "function" {
		t.Fatalf("symbol_type = %v, want function", got)
	}
}

func TestExtractLanguageRoutingForRequiredSet(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		symbol      string
		mustContain string
	}{
		{name: "go", path: fixturePath("languages", "sample.go"), symbol: "GoTarget", mustContain: "func GoTarget()"},
		{name: "python", path: fixturePath("languages", "sample.py"), symbol: "py_target", mustContain: "def py_target()"},
		{name: "java", path: fixturePath("languages", "sample.java"), symbol: "JavaTarget", mustContain: "class JavaTarget"},
		{name: "javascript", path: fixturePath("languages", "sample.js"), symbol: "jsTarget", mustContain: "function jsTarget()"},
		{name: "typescript", path: fixturePath("languages", "sample.ts"), symbol: "tsTarget", mustContain: "function tsTarget()"},
		{name: "rust", path: fixturePath("languages", "sample.rs"), symbol: "rust_target", mustContain: "fn rust_target()"},
		{name: "cpp", path: fixturePath("languages", "sample.cpp"), symbol: "cppTarget", mustContain: "cppTarget()"},
		{name: "html", path: fixturePath("languages", "sample.html"), symbol: "widget", mustContain: "<widget>ok</widget>"},
		{name: "xml", path: fixturePath("languages", "sample.xml"), symbol: "record", mustContain: "<record id=\"1\">ok</record>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Extract(tc.path, tc.symbol, "raw")
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			if !strings.Contains(out, tc.mustContain) {
				t.Fatalf("output %q does not contain %q", out, tc.mustContain)
			}
		})
	}
}

func TestExtractGoVarSpecRawAndJSONContracts(t *testing.T) {
	path := fixturePath("languages", "go_decls.go")

	rawOut, err := Extract(path, "GoVar", "raw")
	if err != nil {
		t.Fatalf("Extract raw returned error: %v", err)
	}
	wantRaw := "GoVar = 1"
	if rawOut != wantRaw {
		t.Fatalf("raw output mismatch\n got: %q\nwant: %q", rawOut, wantRaw)
	}

	jsonOut, err := Extract(path, "GoVar", "json")
	if err != nil {
		t.Fatalf("Extract json returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, jsonOut)
	}
	assertJSONContractKeys(t, payload)

	if got := payload["name"]; got != "GoVar" {
		t.Fatalf("name = %v, want GoVar", got)
	}
	if got := payload["code"]; got != wantRaw {
		t.Fatalf("code = %v, want %q", got, wantRaw)
	}
	if got := payload["symbol_type"]; got != "variable" {
		t.Fatalf("symbol_type = %v, want variable", got)
	}
}

func TestExtractGoConstSpecMatchesSecondaryIdentifier(t *testing.T) {
	path := fixturePath("languages", "go_decls.go")

	rawOut, err := Extract(path, "B", "raw")
	if err != nil {
		t.Fatalf("Extract raw returned error: %v", err)
	}
	wantRaw := "A, B = 1, 2"
	if rawOut != wantRaw {
		t.Fatalf("raw output mismatch\n got: %q\nwant: %q", rawOut, wantRaw)
	}

	jsonOut, err := Extract(path, "B", "json")
	if err != nil {
		t.Fatalf("Extract json returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, jsonOut)
	}
	if got := payload["name"]; got != "B" {
		t.Fatalf("name = %v, want B", got)
	}
	if got := payload["symbol_type"]; got != "constant" {
		t.Fatalf("symbol_type = %v, want constant", got)
	}
}

func TestExtractDirectoryModeDeterministicOrderingAndContracts(t *testing.T) {
	dir := fixturePath("directory")

	rawOut, err := Extract(dir, "DirTarget", "raw")
	if err != nil {
		t.Fatalf("Extract raw directory returned error: %v", err)
	}

	headers := extractHeaders(rawOut)
	wantHeaders := []string{
		fmt.Sprintf("=== file: %s ===", filepath.ToSlash(filepath.Join(dir, "alpha.go"))),
		fmt.Sprintf("=== file: %s ===", filepath.ToSlash(filepath.Join(dir, "nested", "bravo.py"))),
	}
	if !reflect.DeepEqual(headers, wantHeaders) {
		t.Fatalf("raw headers = %v, want %v\noutput:\n%s", headers, wantHeaders, rawOut)
	}
	if strings.Contains(rawOut, "vendor/ignored.go") {
		t.Fatalf("raw output should skip vendor directory, got: %s", rawOut)
	}

	jsonOut, err := Extract(dir, "DirTarget", "json")
	if err != nil {
		t.Fatalf("Extract json directory returned error: %v", err)
	}

	var payload []map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, jsonOut)
	}
	if len(payload) != 2 {
		t.Fatalf("json result length = %d, want 2 (%v)", len(payload), payload)
	}

	file0, _ := payload[0]["file_path"].(string)
	file1, _ := payload[1]["file_path"].(string)
	if file0 != filepath.ToSlash(filepath.Join(dir, "alpha.go")) || file1 != filepath.ToSlash(filepath.Join(dir, "nested", "bravo.py")) {
		t.Fatalf("json file ordering mismatch: got [%s, %s]", file0, file1)
	}
}

func TestExtractSymbolNotFound(t *testing.T) {
	path := fixturePath("languages", "sample.go")
	_, err := Extract(path, "MissingSymbol", "raw")
	if err == nil {
		t.Fatal("expected error for missing symbol, got nil")
	}
	if !strings.Contains(err.Error(), "symbol not found") {
		t.Fatalf("error %q does not include required substring %q", err.Error(), "symbol not found")
	}
}

func TestExtractUnsupportedLanguage(t *testing.T) {
	path := fixturePath("unsupported", "sample.c")
	_, err := Extract(path, "cTarget", "raw")
	if err == nil {
		t.Fatal("expected unsupported language error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error %q does not include required substring %q", err.Error(), "unsupported")
	}
}

func extractHeaders(output string) []string {
	lines := strings.Split(output, "\n")
	headers := make([]string, 0)
	for _, line := range lines {
		if strings.HasPrefix(line, "=== file: ") {
			headers = append(headers, line)
		}
	}
	return headers
}

func assertJSONContractKeys(t *testing.T, payload map[string]any) {
	t.Helper()

	wantKeys := []string{"name", "code", "file_path", "start_line", "end_line", "start_byte", "end_byte", "symbol_type"}
	if len(payload) != len(wantKeys) {
		t.Fatalf("json key count = %d, want %d (payload=%v)", len(payload), len(wantKeys), payload)
	}
	for _, key := range wantKeys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing json key %q in payload %v", key, payload)
		}
	}
}

func fixturePath(parts ...string) string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller failed")
	}
	allParts := append([]string{filepath.Dir(thisFile), "testdata"}, parts...)
	return filepath.Join(allParts...)
}
