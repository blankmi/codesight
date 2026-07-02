package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"

	configpkg "codesight/pkg/config"
	"codesight/pkg/lsp"
)

var javaGradleTrackedBuildFiles = []string{
	"build.gradle.kts",
	"build.gradle",
	"settings.gradle.kts",
	"settings.gradle",
}

func init() {
	lspRuntimeNewDaemonConnector = func(registry *lsp.Registry) lspDaemonConnector {
		if registry == nil {
			registry = newLSPRegistry("")
		}

		return lsp.NewDaemonConnector(
			registry,
			lsp.WithDaemonConnectorLifecycle(
				lsp.NewLifecycle(
					registry,
					lsp.WithIdleTimeout(resolvedLSPDaemonIdleTimeout(currentConfig())),
					lsp.WithServerEnvResolver(refsLSPServerEnv),
				),
			),
			lsp.WithDaemonConnectorInitializeParamsBuilder(refsInitializeParams),
		)
	}
}

func jdtlsInitOptionsForWorkspace(workspaceRoot string, cfg *configpkg.Config) (map[string]any, error) {
	suppressGradleImport, err := shouldSuppressJDTLSGradleImport(workspaceRoot)
	if err != nil {
		return nil, err
	}
	baseOptions := jdtlsInitOptions(cfg)
	if !suppressGradleImport {
		return baseOptions, nil
	}

	gradleJavaHome := ""
	if cfg != nil {
		gradleJavaHome = strings.TrimSpace(cfg.LSP.Java.GradleJavaHome)
	}

	return buildJDTLSInitOptions(gradleJavaHome, true), nil
}

func refsLSPServerEnv(spec lsp.ServerSpec) []string {
	return javaLSPServerEnv(spec, currentConfig())
}

func javaLSPServerEnv(spec lsp.ServerSpec, cfg *configpkg.Config) []string {
	if !strings.EqualFold(strings.TrimSpace(spec.Binary), "jdtls") {
		return nil
	}
	if cfg == nil {
		return nil
	}
	runtimeJavaHome := strings.TrimSpace(cfg.LSP.Java.RuntimeJavaHome)
	if runtimeJavaHome == "" {
		return nil
	}
	return []string{"JAVA_HOME=" + runtimeJavaHome}
}

func mergeProcessEnv(base []string, overrides []string) []string {
	normalizedOverrides := normalizeProcessEnvOverrides(overrides)
	if len(normalizedOverrides) == 0 {
		return append([]string(nil), base...)
	}

	overrideNames := map[string]struct{}{}
	for _, entry := range normalizedOverrides {
		name, _, ok := strings.Cut(entry, "=")
		if ok {
			overrideNames[name] = struct{}{}
		}
	}

	merged := make([]string, 0, len(base)+len(normalizedOverrides))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, overridden := overrideNames[name]; overridden {
				continue
			}
		}
		merged = append(merged, entry)
	}
	merged = append(merged, normalizedOverrides...)
	return merged
}

func normalizeProcessEnvOverrides(env []string) []string {
	if len(env) == 0 {
		return nil
	}

	byName := map[string]string{}
	for _, entry := range env {
		trimmed := strings.TrimSpace(entry)
		name, value, ok := strings.Cut(trimmed, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		byName[name] = name + "=" + value
	}
	if len(byName) == 0 {
		return nil
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	normalized := make([]string, 0, len(names))
	for _, name := range names {
		normalized = append(normalized, byName[name])
	}
	return normalized
}

func shouldSuppressJDTLSGradleImport(workspaceRoot string) (bool, error) {
	currentBaseline, err := detectJavaGradleBuildBaseline(workspaceRoot)
	if err != nil {
		return false, err
	}

	previousBaseline, err := lsp.ReadJavaGradleBuildBaseline(workspaceRoot)
	baselineExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read java gradle build baseline: %w", err)
	}

	if err := lsp.WriteJavaGradleBuildBaseline(workspaceRoot, currentBaseline); err != nil {
		return false, fmt.Errorf("write java gradle build baseline: %w", err)
	}

	if !baselineExists {
		return false, nil
	}

	if previousBaseline.Fingerprint == "" {
		return false, nil
	}

	return previousBaseline.Fingerprint == currentBaseline.Fingerprint, nil
}

func detectJavaGradleBuildBaseline(workspaceRoot string) (lsp.JavaGradleBuildBaseline, error) {
	canonicalRoot, err := resolveProjectPath(workspaceRoot)
	if err != nil {
		return lsp.JavaGradleBuildBaseline{}, fmt.Errorf("resolve workspace root for java build baseline: %w", err)
	}

	baseline := lsp.JavaGradleBuildBaseline{
		Files: make([]lsp.JavaGradleBuildFile, 0, len(javaGradleTrackedBuildFiles)),
	}

	digest := sha256.New()
	for _, relativePath := range javaGradleTrackedBuildFiles {
		fileState, err := detectJavaGradleBuildFile(canonicalRoot, relativePath)
		if err != nil {
			return lsp.JavaGradleBuildBaseline{}, err
		}
		baseline.Files = append(baseline.Files, fileState)
		writeJavaGradleFingerprintPart(digest, fileState)
	}

	baseline.Fingerprint = hex.EncodeToString(digest.Sum(nil))
	return baseline, nil
}

func detectJavaGradleBuildFile(workspaceRoot, relativePath string) (lsp.JavaGradleBuildFile, error) {
	state := lsp.JavaGradleBuildFile{Path: relativePath}

	fullPath := filepath.Join(workspaceRoot, relativePath)
	info, err := os.Stat(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return lsp.JavaGradleBuildFile{}, fmt.Errorf("stat java build file %q: %w", fullPath, err)
	}
	if info.IsDir() {
		return lsp.JavaGradleBuildFile{}, fmt.Errorf("java build file path is a directory: %s", fullPath)
	}

	contents, err := os.ReadFile(fullPath)
	if err != nil {
		return lsp.JavaGradleBuildFile{}, fmt.Errorf("read java build file %q: %w", fullPath, err)
	}

	contentSum := sha256.Sum256(contents)
	state.Exists = true
	state.ModTimeUnixNano = info.ModTime().UnixNano()
	state.SizeBytes = info.Size()
	state.ContentSHA256 = hex.EncodeToString(contentSum[:])
	return state, nil
}

func writeJavaGradleFingerprintPart(digest hash.Hash, state lsp.JavaGradleBuildFile) {
	if digest == nil {
		return
	}
	_, _ = fmt.Fprintf(
		digest,
		"%s\x00%t\x00%s\x00",
		state.Path,
		state.Exists,
		state.ContentSHA256,
	)
}

func buildJDTLSInitOptions(gradleJavaHome string, suppressGradleImport bool) map[string]any {
	gradleOptions := map[string]any{}
	if strings.TrimSpace(gradleJavaHome) != "" {
		gradleOptions["java"] = map[string]any{
			"home": strings.TrimSpace(gradleJavaHome),
		}
	}
	if suppressGradleImport {
		gradleOptions["enabled"] = false
	}
	if len(gradleOptions) == 0 {
		return nil
	}

	return map[string]any{
		"settings": map[string]any{
			"java": map[string]any{
				"import": map[string]any{
					"gradle": gradleOptions,
				},
				"symbols": map[string]any{
					"includeSourceMethodDeclarations": true,
				},
			},
		},
	}
}
