package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	csignore "codesight/pkg/ignore"
	"codesight/pkg/lsp"
	"codesight/pkg/splitter"
	"github.com/spf13/cobra"
)

var (
	errCheckFoundSyntaxErrors = errors.New("syntax errors found")

	checkCmd = &cobra.Command{
		Use:          "check <path> [path...]",
		Short:        "Check one or more files for syntax errors using LSP",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE:         runCheck,
	}

	runCheckCommand = executeCheckCommand
)

type checkCommandOptions struct {
	WorkspaceRoot string
	TargetPaths   []string
}

func runCheck(cmd *cobra.Command, args []string) error {
	targetPaths := make([]string, 0, len(args))
	for _, arg := range args {
		resolved, err := resolveProjectPath(arg)
		if err != nil {
			return err
		}
		targetPaths = append(targetPaths, resolved)
	}

	workspaceRoot := resolvedProjectRootForTarget(currentTargetDir())
	result, err := runCheckCommand(cmd.Context(), checkCommandOptions{
		WorkspaceRoot: workspaceRoot,
		TargetPaths:   targetPaths,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(cmd.OutOrStdout(), result.Output()); err != nil {
		return err
	}
	if result.ErrorCount() > 0 {
		return errCheckFoundSyntaxErrors
	}
	return nil
}

func executeCheckCommand(ctx context.Context, opts checkCommandOptions) (lsp.CheckResult, error) {
	registry := lsp.NewRegistry()

	// Group resolved paths by detected language.
	pathsByLanguage := map[string][]string{}
	for _, targetPath := range opts.TargetPaths {
		resolved := strings.TrimSpace(targetPath)
		if resolved == "" {
			resolved = opts.WorkspaceRoot
		}
		resolvedTarget, err := resolveProjectPath(resolved)
		if err != nil {
			return lsp.CheckResult{}, err
		}
		language, err := detectCheckLanguage(opts.WorkspaceRoot, resolvedTarget, registry)
		if err != nil {
			return lsp.CheckResult{}, err
		}
		pathsByLanguage[language] = append(pathsByLanguage[language], resolvedTarget)
	}

	var allDiagnostics []lsp.CheckDiagnostic
	for language, paths := range pathsByLanguage {
		spec, err := registry.Lookup(language)
		if err != nil {
			return lsp.CheckResult{}, err
		}
		if _, err := exec.LookPath(spec.Binary); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return lsp.CheckResult{}, formatMissingCheckLSPError(spec.Binary, spec.InstallHint)
			}
			return lsp.CheckResult{}, err
		}

		runtime := newLSPCommandRuntime(registry)
		client, release, err := runtime.connectClient(ctx, opts.WorkspaceRoot, spec)
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return lsp.CheckResult{}, formatMissingCheckLSPError(spec.Binary, spec.InstallHint)
			}
			return lsp.CheckResult{}, err
		}

		engine := lsp.NewCheckEngine(client)
		for _, targetPath := range paths {
			result, err := engine.Run(ctx, lsp.CheckOptions{
				WorkspaceRoot: opts.WorkspaceRoot,
				TargetPath:    targetPath,
				Language:      language,
			})
			if err != nil {
				release()
				return lsp.CheckResult{}, err
			}
			allDiagnostics = append(allDiagnostics, result.Diagnostics...)
		}
		release()
	}

	return lsp.CheckResult{Diagnostics: allDiagnostics}, nil
}

func detectCheckLanguage(workspaceRoot, targetPath string, registry *lsp.Registry) (string, error) {
	if registry == nil {
		registry = lsp.NewRegistry()
	}

	resolvedTarget, err := resolveProjectPath(targetPath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(resolvedTarget)
	if err != nil {
		return "", err
	}

	if !info.IsDir() {
		language := normalizeCheckLanguage(splitter.LanguageFromExtension(filepath.Ext(resolvedTarget)))
		if language == "" {
			return "", unsupportedCheckLanguageError(resolvedTarget)
		}
		if _, err := registry.Lookup(language); err != nil {
			return "", unsupportedCheckLanguageError(resolvedTarget)
		}
		return language, nil
	}

	matcher, err := csignore.NewMatcher(workspaceRoot, nil)
	if err != nil {
		return "", fmt.Errorf("load ignore rules: %w", err)
	}

	countByLanguage := map[string]int{}
	supportedCache := map[string]bool{}

	err = filepath.WalkDir(resolvedTarget, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if path != resolvedTarget && matcher.MatchesPath(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if matcher.MatchesPath(path) {
			return nil
		}

		language := normalizeCheckLanguage(splitter.LanguageFromExtension(filepath.Ext(entry.Name())))
		if language == "" {
			return nil
		}

		supported, ok := supportedCache[language]
		if !ok {
			_, lookupErr := registry.Lookup(language)
			supported = lookupErr == nil
			supportedCache[language] = supported
		}
		if !supported {
			return nil
		}

		countByLanguage[language]++
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(countByLanguage) == 0 {
		return "", unsupportedCheckLanguageError(resolvedTarget)
	}

	bestLanguage := ""
	bestCount := -1
	for language, count := range countByLanguage {
		if count > bestCount || (count == bestCount && (bestLanguage == "" || language < bestLanguage)) {
			bestLanguage = language
			bestCount = count
		}
	}
	return bestLanguage, nil
}

func normalizeCheckLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func unsupportedCheckLanguageError(targetPath string) error {
	return fmt.Errorf("cs check: no supported LSP language detected for %s", filepath.Clean(strings.TrimSpace(targetPath)))
}

func formatMissingCheckLSPError(binary string, install string) error {
	trimmedBinary := strings.TrimSpace(binary)
	if trimmedBinary == "" {
		trimmedBinary = "lsp"
	}

	trimmedInstall := strings.TrimSpace(install)
	if trimmedInstall == "" {
		trimmedInstall = "install language server"
	}

	return fmt.Errorf(
		"cs check: LSP required but %s not found. Install: %s",
		trimmedBinary,
		trimmedInstall,
	)
}
