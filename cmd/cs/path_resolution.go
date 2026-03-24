package main

import (
	"os"
	"path/filepath"
	"strings"

	configpkg "github.com/blankbytes/codesight/pkg/config"
	"github.com/spf13/cobra"
)

var runtimeTargetDir string

func commandTargetPath(cmd *cobra.Command, args []string) (string, error) {
	if cmd == nil {
		return "", nil
	}

	switch cmd {
	case queryCmd, searchCmd, refsCmd, callersCmd, implementsCmd:
		return cmd.Flags().GetString("path")
	case indexCmd, statusCmd, clearCmd, configCmd:
		if len(args) > 0 {
			return args[0], nil
		}
		return "", nil
	case lspWarmupCmd, lspStatusCmd, lspRestartCmd:
		if len(args) > 0 {
			return args[0], nil
		}
		return "", nil
	default:
		// Root command with persistent --path flag.
		if p, err := cmd.Flags().GetString("path"); err == nil && p != "" {
			return p, nil
		}
		return "", nil
	}
}

func resolveCommandTargetDir(path string) (string, error) {
	target := strings.TrimSpace(path)
	if target == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return "", err
		}
		target = workingDirectory
	}

	absPath, err := resolveProjectPath(target)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		absPath = filepath.Dir(absPath)
	}

	return filepath.Clean(absPath), nil
}

func currentTargetDir() string {
	if strings.TrimSpace(runtimeTargetDir) != "" {
		return runtimeTargetDir
	}

	targetDir, err := resolveCommandTargetDir("")
	if err != nil {
		return "."
	}
	return targetDir
}

func resolvedProjectRootForTarget(targetDir string) string {
	resolvedTarget := strings.TrimSpace(targetDir)
	if resolvedTarget == "" {
		resolvedTarget = currentTargetDir()
	}

	cfg := currentConfig()
	if cfg.ConfigDir != "" {
		if root, err := cfg.ResolvedProjectRoot(cfg.ConfigDir); err == nil {
			return root
		}
	}

	return resolvedTarget
}

func runtimeConfigForTarget(targetDir string) (*configpkg.Config, error) {
	cfg, err := configpkg.LoadConfig(targetDir)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
