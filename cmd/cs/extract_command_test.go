package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractCommandRequiresFileFlag(t *testing.T) {
	_, _, err := executeRootCommand(t, "extract", "-s", "GoTarget")
	if err == nil {
		t.Fatal("expected missing file flag error, got nil")
	}
	if !strings.Contains(err.Error(), `required flag(s) "file" not set`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestExtractCommandRequiresSymbolFlag(t *testing.T) {
	path := extractFixturePath(t, "languages", "sample.go")

	_, _, err := executeRootCommand(t, "extract", "-f", path)
	if err == nil {
		t.Fatal("expected missing symbol flag error, got nil")
	}
	if !strings.Contains(err.Error(), `required flag(s) "symbol" not set`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestExtractCommandHelpShowsRequiredSignature(t *testing.T) {
	stdout, stderr, err := executeRootCommand(t, "extract", "--help")
	if err != nil {
		t.Fatalf("extract help returned error: %v", err)
	}

	combined := stdout + "\n" + stderr
	if !strings.Contains(combined, "cs extract -f <file> -s <symbol>") {
		t.Fatalf("help text missing required signature, got:\n%s", combined)
	}
}

func TestExtractCommandDefaultFormatIsRaw(t *testing.T) {
	path := extractFixturePath(t, "languages", "sample.go")

	stdout, _, err := executeRootCommand(t, "extract", "-f", path, "-s", "GoTarget")
	if err != nil {
		t.Fatalf("extract command returned error: %v", err)
	}

	want := "func GoTarget() string {\n\treturn \"go\"\n}"
	if stdout != want {
		t.Fatalf("raw output mismatch\n got: %q\nwant: %q", stdout, want)
	}
}

func TestExtractCommandJSONFormatHappyPath(t *testing.T) {
	path := extractFixturePath(t, "languages", "sample.go")

	stdout, _, err := executeRootCommand(t, "extract", "-f", path, "-s", "GoTarget", "--format", "json")
	if err != nil {
		t.Fatalf("extract command returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput: %s", err, stdout)
	}
	if got := payload["name"]; got != "GoTarget" {
		t.Fatalf("name = %v, want GoTarget", got)
	}
	if got := payload["symbol_type"]; got != "function" {
		t.Fatalf("symbol_type = %v, want function", got)
	}
	if got := payload["file_path"]; got != filepath.ToSlash(path) {
		t.Fatalf("file_path = %v, want %q", got, filepath.ToSlash(path))
	}
}

func TestExtractCommandRejectsInvalidFormat(t *testing.T) {
	path := extractFixturePath(t, "languages", "sample.go")

	_, _, err := executeRootCommand(t, "extract", "-f", path, "-s", "GoTarget", "--format", "yaml")
	if err == nil {
		t.Fatal("expected invalid format error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error %q does not include required substring %q", err.Error(), "unsupported")
	}
}

func TestExtractCommandPropagatesRequiredErrorSubstrings(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		symbol      string
		mustContain string
	}{
		{
			name:        "symbol not found",
			path:        extractFixturePath(t, "languages", "sample.go"),
			symbol:      "MissingSymbol",
			mustContain: "symbol not found",
		},
		{
			name:        "unsupported language",
			path:        extractFixturePath(t, "unsupported", "sample.c"),
			symbol:      "cTarget",
			mustContain: "unsupported",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := executeRootCommand(t, "extract", "-f", tc.path, "-s", tc.symbol)
			if err == nil {
				t.Fatal("expected extract command error, got nil")
			}
			if !strings.Contains(err.Error(), tc.mustContain) {
				t.Fatalf("error %q does not include required substring %q", err.Error(), tc.mustContain)
			}
		})
	}
}

func executeRootCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetExtractCommandFlagState(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)

	_, err := rootCmd.ExecuteC()
	rootCmd.SetArgs(nil)
	return stdout.String(), stderr.String(), err
}

func resetExtractCommandFlagState(t *testing.T) {
	t.Helper()

	for _, name := range []string{"file", "symbol", "format"} {
		flag := extractCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("extract flag %q not found", name)
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", name, err)
		}
		flag.Changed = false
	}

	if helpFlag := extractCmd.Flags().Lookup("help"); helpFlag != nil {
		if err := helpFlag.Value.Set(helpFlag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", "help", err)
		}
		helpFlag.Changed = false
	}
}

func extractFixturePath(t *testing.T, parts ...string) string {
	t.Helper()

	base := append([]string{"..", "..", "pkg", "extract", "testdata"}, parts...)
	path, err := filepath.Abs(filepath.Join(base...))
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}
	return path
}
