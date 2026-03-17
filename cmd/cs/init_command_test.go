package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_CreatesConfigAndGitignore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := t.TempDir()

	_, _, err := executeRootCommand(t, "init", target)
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	configPath := filepath.Join(target, ".codesight", "config.toml")
	configContent := readTestFile(t, configPath)
	if !strings.Contains(configContent, "[embedding]") {
		t.Fatalf("config missing [embedding] section:\n%s", configContent)
	}
	if !strings.Contains(configContent, `model = "nomic-embed-text"`) {
		t.Fatalf("config missing default embedding model:\n%s", configContent)
	}

	gitignorePath := filepath.Join(target, ".codesight", ".gitignore")
	gitignoreContent := readTestFile(t, gitignorePath)
	if gitignoreContent != "lsp/\n" {
		t.Fatalf(".gitignore content = %q, want %q", gitignoreContent, "lsp/\n")
	}
}

func TestInit_JavaProjectDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := t.TempDir()
	writeTestFile(t, filepath.Join(target, "build.gradle.kts"), "plugins {}\n")

	_, _, err := executeRootCommand(t, "init", target)
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	configPath := filepath.Join(target, ".codesight", "config.toml")
	configContent := readTestFile(t, configPath)
	if !strings.Contains(configContent, "[lsp.java]") {
		t.Fatalf("config missing [lsp.java] section:\n%s", configContent)
	}
	if !strings.Contains(configContent, `gradle_java_home = ""`) {
		t.Fatalf("config missing gradle_java_home placeholder:\n%s", configContent)
	}
}

func TestInit_GoProjectDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := t.TempDir()
	writeTestFile(t, filepath.Join(target, "go.mod"), "module example.com/test\n")

	_, _, err := executeRootCommand(t, "init", target)
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	configPath := filepath.Join(target, ".codesight", "config.toml")
	configContent := readTestFile(t, configPath)
	if !strings.Contains(configContent, "[lsp.go]") {
		t.Fatalf("config missing [lsp.go] section:\n%s", configContent)
	}
	if !strings.Contains(configContent, "build_flags = []") {
		t.Fatalf("config missing build_flags placeholder:\n%s", configContent)
	}
}

func TestInit_RustProjectDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := t.TempDir()
	writeTestFile(t, filepath.Join(target, "Cargo.toml"), "[package]\nname = \"example\"\nversion = \"0.1.0\"\n")

	_, _, err := executeRootCommand(t, "init", target)
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	configPath := filepath.Join(target, ".codesight", "config.toml")
	configContent := readTestFile(t, configPath)
	if !strings.Contains(configContent, "[lsp.rust]") {
		t.Fatalf("config missing [lsp.rust] section:\n%s", configContent)
	}
}

func TestInit_MultipleProjectTypes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := t.TempDir()
	writeTestFile(t, filepath.Join(target, "go.mod"), "module example.com/test\n")
	writeTestFile(t, filepath.Join(target, "package.json"), "{\n  \"name\": \"example\"\n}\n")

	_, _, err := executeRootCommand(t, "init", target)
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	configPath := filepath.Join(target, ".codesight", "config.toml")
	configContent := readTestFile(t, configPath)
	if !strings.Contains(configContent, "[lsp.go]") {
		t.Fatalf("config missing [lsp.go] section:\n%s", configContent)
	}
	if !strings.Contains(configContent, "[lsp.typescript]") {
		t.Fatalf("config missing [lsp.typescript] section:\n%s", configContent)
	}
}

func TestInit_NoClobber(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := t.TempDir()

	existing := "existing = true\n"
	configPath := filepath.Join(target, ".codesight", "config.toml")
	writeTestFile(t, configPath, existing)

	stdout, _, err := executeRootCommand(t, "init", target)
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}
	if stdout != ".codesight/config.toml already exists, skipping\n" {
		t.Fatalf("stdout = %q, want %q", stdout, ".codesight/config.toml already exists, skipping\n")
	}

	got := readTestFile(t, configPath)
	if got != existing {
		t.Fatalf("config was overwritten\n got: %q\nwant: %q", got, existing)
	}

	gitignorePath := filepath.Join(target, ".codesight", ".gitignore")
	if _, err := os.Stat(gitignorePath); !os.IsNotExist(err) {
		t.Fatalf("expected .gitignore not to be created on no-clobber path, err=%v", err)
	}
}

func TestInit_DefaultPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(target); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	_, _, err = executeRootCommand(t, "init")
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	configPath := filepath.Join(target, ".codesight", "config.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config at default path: %v", err)
	}
}

func TestInit_TargetPathIgnoresMalformedWorkingDirConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	workingDir := t.TempDir()
	writeTestFile(t, filepath.Join(workingDir, ".codesight", "config.toml"), "[embedding\nmodel = \"broken\"\n")

	target := t.TempDir()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	_, _, err = executeRootCommand(t, "init", target)
	if err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	configPath := filepath.Join(target, ".codesight", "config.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file at target path: %v", err)
	}
}

func TestInit_OutputMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Run("creates config and gitignore messages", func(t *testing.T) {
		target := t.TempDir()

		stdout, _, err := executeRootCommand(t, "init", target)
		if err != nil {
			t.Fatalf("init command returned error: %v", err)
		}

		want := "Created .codesight/config.toml\nCreated .codesight/.gitignore\n"
		if stdout != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	})

	t.Run("omits gitignore message when file already exists", func(t *testing.T) {
		target := t.TempDir()
		writeTestFile(t, filepath.Join(target, ".codesight", ".gitignore"), "cache/\n")

		stdout, _, err := executeRootCommand(t, "init", target)
		if err != nil {
			t.Fatalf("init command returned error: %v", err)
		}

		want := "Created .codesight/config.toml\n"
		if stdout != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	})
}

