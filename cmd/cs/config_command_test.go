package main

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var configOutputPattern = regexp.MustCompile(`^([a-z0-9_.]+) = (.*) \(([^)]+)\)$`)

func TestConfig_DefaultsOnly(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	projectDir := t.TempDir()

	stdout, _, err := executeRootCommand(t, "config", projectDir)
	if err != nil {
		t.Fatalf("config command returned error: %v", err)
	}

	entries := parseConfigOutput(t, stdout)
	want := map[string]configOutputEntry{
		"db.address":                {Value: "localhost:19530", Source: "default"},
		"db.token":                  {Value: "", Source: "default"},
		"db.type":                   {Value: "milvus", Source: "default"},
		"embedding.max_input_chars": {Value: "0", Source: "default"},
		"embedding.model":           {Value: "nomic-embed-text", Source: "default"},
		"embedding.ollama_host":     {Value: "http://127.0.0.1:11434", Source: "default"},
		"index.warm_lsp":            {Value: "false", Source: "default"},
		"lsp.daemon.idle_timeout":   {Value: "10m", Source: "default"},
		"lsp.go.build_flags":        {Value: "", Source: "default"},
		"lsp.java.args":             {Value: "", Source: "default"},
		"lsp.java.gradle_java_home": {Value: "", Source: "default"},
		"lsp.java.timeout":          {Value: "60s", Source: "default"},
		"state_dir":                 {Value: "", Source: "default"},
	}

	if len(entries) != len(want) {
		t.Fatalf("line count = %d, want %d\noutput:\n%s", len(entries), len(want), stdout)
	}

	for key, wantEntry := range want {
		gotEntry, ok := entries[key]
		if !ok {
			t.Fatalf("missing key %q in output:\n%s", key, stdout)
		}
		if gotEntry != wantEntry {
			t.Fatalf("%s = %#v, want %#v", key, gotEntry, wantEntry)
		}
	}
}

func TestConfig_WithProjectConfig(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	projectDir := t.TempDir()
	writeTestFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[embedding]
model = "project-model"

[lsp.daemon]
idle_timeout = "25s"
`)

	stdout, _, err := executeRootCommand(t, "config", projectDir)
	if err != nil {
		t.Fatalf("config command returned error: %v", err)
	}

	entries := parseConfigOutput(t, stdout)
	if got := entries["embedding.model"]; got.Value != "project-model" || got.Source != ".codesight/config.toml" {
		t.Fatalf("embedding.model = %#v, want value project-model with source .codesight/config.toml", got)
	}
	if got := entries["lsp.daemon.idle_timeout"]; got.Value != "25s" || got.Source != ".codesight/config.toml" {
		t.Fatalf("lsp.daemon.idle_timeout = %#v, want value 25s with source .codesight/config.toml", got)
	}
	if got := entries["db.type"]; got.Source != "default" {
		t.Fatalf("db.type source = %q, want default", got.Source)
	}
}

func TestConfig_WithEnvOverride(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)
	t.Setenv("CODESIGHT_EMBEDDING_MODEL", "env-model")
	t.Setenv("CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT", "40s")

	projectDir := t.TempDir()

	stdout, _, err := executeRootCommand(t, "config", projectDir)
	if err != nil {
		t.Fatalf("config command returned error: %v", err)
	}

	entries := parseConfigOutput(t, stdout)
	if got := entries["embedding.model"]; got.Value != "env-model" || got.Source != "CODESIGHT_EMBEDDING_MODEL" {
		t.Fatalf("embedding.model = %#v, want value env-model with source CODESIGHT_EMBEDDING_MODEL", got)
	}
	if got := entries["lsp.daemon.idle_timeout"]; got.Value != "40s" || got.Source != "CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT" {
		t.Fatalf("lsp.daemon.idle_timeout = %#v, want value 40s with source CODESIGHT_LSP_DAEMON_IDLE_TIMEOUT", got)
	}
}

func TestConfig_OutputSorted(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	projectDir := t.TempDir()

	stdout, _, err := executeRootCommand(t, "config", projectDir)
	if err != nil {
		t.Fatalf("config command returned error: %v", err)
	}

	lines := outputLines(stdout)
	keys := make([]string, 0, len(lines))
	for _, line := range lines {
		matches := configOutputPattern.FindStringSubmatch(line)
		if matches == nil {
			t.Fatalf("line %q did not match output format", line)
		}
		keys = append(keys, matches[1])
	}

	sortedKeys := append([]string(nil), keys...)
	sort.Strings(sortedKeys)
	if strings.Join(keys, "\n") != strings.Join(sortedKeys, "\n") {
		t.Fatalf("keys not sorted\n got: %v\nwant: %v", keys, sortedKeys)
	}
}

func TestConfig_OutputFormat(t *testing.T) {
	setTestHome(t)
	clearTestEnv(t)

	projectDir := t.TempDir()
	writeTestFile(t, filepath.Join(projectDir, ".codesight", "config.toml"), `
[lsp.go]
build_flags = ["-tags=integration", "-mod=mod"]
`)

	stdout, _, err := executeRootCommand(t, "config", projectDir)
	if err != nil {
		t.Fatalf("config command returned error: %v", err)
	}

	for _, line := range outputLines(stdout) {
		if !configOutputPattern.MatchString(line) {
			t.Fatalf("line %q did not match <key> = <value> (<source>)", line)
		}
	}

	entries := parseConfigOutput(t, stdout)
	if got := entries["db.token"]; got.Value != "" {
		t.Fatalf("db.token value = %q, want empty string", got.Value)
	}
	if got := entries["lsp.go.build_flags"]; got.Value != "-tags=integration,-mod=mod" {
		t.Fatalf("lsp.go.build_flags value = %q, want %q", got.Value, "-tags=integration,-mod=mod")
	}
}

type configOutputEntry struct {
	Value  string
	Source string
}

func parseConfigOutput(t *testing.T, stdout string) map[string]configOutputEntry {
	t.Helper()

	lines := outputLines(stdout)
	entries := make(map[string]configOutputEntry, len(lines))

	for _, line := range lines {
		matches := configOutputPattern.FindStringSubmatch(line)
		if matches == nil {
			t.Fatalf("line %q did not match output format", line)
		}

		key := matches[1]
		if _, exists := entries[key]; exists {
			t.Fatalf("duplicate key %q in output", key)
		}
		entries[key] = configOutputEntry{
			Value:  matches[2],
			Source: matches[3],
		}
	}

	return entries
}

func outputLines(stdout string) []string {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
