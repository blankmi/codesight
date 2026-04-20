package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	configpkg "codesight/pkg/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize .codesight project configuration",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runInit,
}

type projectTypeDetection struct {
	java       bool
	goLang     bool
	rust       bool
	typescript bool
}

func runInit(cmd *cobra.Command, args []string) error {
	targetPath := "."
	if len(args) == 1 {
		targetPath = args[0]
	}

	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}

	info, err := os.Stat(absTargetPath)
	if err != nil {
		return fmt.Errorf("stat target path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("target path must be a directory: %s", targetPath)
	}

	codesightDir := filepath.Join(absTargetPath, ".codesight")
	if err := os.MkdirAll(codesightDir, 0o700); err != nil {
		return fmt.Errorf("create .codesight directory: %w", err)
	}

	configPath := filepath.Join(codesightDir, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), ".codesight/config.toml already exists, skipping")
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat .codesight/config.toml: %w", err)
	}

	detected, err := detectProjectTypes(absTargetPath)
	if err != nil {
		return err
	}

	configContent := renderInitConfigTemplate(detected)
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		return fmt.Errorf("write .codesight/config.toml: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Created .codesight/config.toml")

	gitignorePath := filepath.Join(codesightDir, ".gitignore")
	_, statErr := os.Stat(gitignorePath)
	gitignoreExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat .codesight/.gitignore: %w", statErr)
	}

	if err := ensureCodesightGitignore(codesightDir); err != nil {
		return fmt.Errorf("ensure .codesight/.gitignore: %w", err)
	}
	if !gitignoreExists {
		fmt.Fprintln(cmd.OutOrStdout(), "Created .codesight/.gitignore")
	}

	return nil
}

func detectProjectTypes(targetPath string) (projectTypeDetection, error) {
	detected := projectTypeDetection{}

	var err error
	if detected.java, err = anyFilesExist(targetPath, "build.gradle.kts", "build.gradle", "pom.xml"); err != nil {
		return projectTypeDetection{}, err
	}
	if detected.goLang, err = anyFilesExist(targetPath, "go.mod"); err != nil {
		return projectTypeDetection{}, err
	}
	if detected.rust, err = anyFilesExist(targetPath, "Cargo.toml"); err != nil {
		return projectTypeDetection{}, err
	}
	if detected.typescript, err = anyFilesExist(targetPath, "package.json"); err != nil {
		return projectTypeDetection{}, err
	}

	return detected, nil
}

func anyFilesExist(targetPath string, fileNames ...string) (bool, error) {
	for _, fileName := range fileNames {
		exists, err := fileExists(filepath.Join(targetPath, fileName))
		if err != nil {
			return false, fmt.Errorf("check %s: %w", fileName, err)
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func renderInitConfigTemplate(detected projectTypeDetection) string {
	defaultModel := configpkg.Defaults().Embedding.Model

	var b strings.Builder
	b.WriteString("# Codesight project configuration.\n")
	b.WriteString("# Environment variables (CODESIGHT_*) override these values.\n\n")

	b.WriteString("# Project root relative to this directory (default: parent of .codesight/).\n")
	b.WriteString("# project_root = \"..\"\n\n")

	b.WriteString("[embedding]\n")
	b.WriteString("# Embedding model used for semantic indexing and search.\n")
	b.WriteString(fmt.Sprintf("model = %q\n", defaultModel))

	if detected.java {
		b.WriteString("\n[lsp.java]\n")
		b.WriteString("# Optional JDK home used for Gradle import/build tooling.\n")
		b.WriteString("gradle_java_home = \"\"\n")
	}
	if detected.goLang {
		b.WriteString("\n[lsp.go]\n")
		b.WriteString("# Additional build flags passed to gopls.\n")
		b.WriteString("build_flags = []\n")
	}
	if detected.rust {
		b.WriteString("\n[lsp.rust]\n")
		b.WriteString("# Placeholder for future Rust LSP settings.\n")
	}
	if detected.typescript {
		b.WriteString("\n[lsp.typescript]\n")
		b.WriteString("# Placeholder for future TypeScript/JavaScript LSP settings.\n")
	}

	return b.String()
}
