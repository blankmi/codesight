package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	configpkg "codesight/pkg/config"
)

const lombokConfigOff = "off"

// javaLombokBuildFileGlobs lists the build files inspected for Lombok usage,
// relative to the workspace root. One directory level of submodules is
// covered so multi-module Maven/Gradle layouts are detected.
var javaLombokBuildFileGlobs = []string{
	"pom.xml",
	"build.gradle",
	"build.gradle.kts",
	"settings.gradle",
	"settings.gradle.kts",
	"gradle/libs.versions.toml",
	"buildSrc/build.gradle",
	"buildSrc/build.gradle.kts",
	"*/pom.xml",
	"*/build.gradle",
	"*/build.gradle.kts",
}

// javaLombokAgentArgs returns the jdtls launch argument enabling the Lombok
// javaagent, or nil when injection is disabled, unnecessary, or impossible.
// Without the agent, jdtls treats Lombok-generated members as compile errors
// and silently drops cross-file method references.
func javaLombokAgentArgs(workspaceRoot string, cfg *configpkg.Config) []string {
	mode := ""
	if cfg != nil {
		mode = strings.TrimSpace(cfg.LSP.Java.Lombok)
		if strings.EqualFold(mode, lombokConfigOff) {
			return nil
		}
		if javaArgsContainLombokAgent(cfg.LSP.Java.Args) {
			return nil
		}
	}

	jar := mode
	if jar == "" {
		if !javaProjectUsesLombok(workspaceRoot) {
			return nil
		}
		jar = findLombokJar()
	}
	if jar == "" {
		return nil
	}
	if _, err := os.Stat(jar); err != nil {
		return nil
	}

	return []string{"--jvm-arg=-javaagent:" + jar}
}

func javaArgsContainLombokAgent(args []string) bool {
	for _, arg := range args {
		lowered := strings.ToLower(arg)
		if strings.Contains(lowered, "-javaagent") && strings.Contains(lowered, "lombok") {
			return true
		}
	}
	return false
}

func javaProjectUsesLombok(workspaceRoot string) bool {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return false
	}

	for _, pattern := range javaLombokBuildFileGlobs {
		paths, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			continue
		}
		for _, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(string(content)), "lombok") {
				return true
			}
		}
	}
	return false
}

// findLombokJar returns the newest lombok jar available in the local Gradle
// and Maven caches, or "" when none is present.
func findLombokJar() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	bestJar := ""
	var bestVersion []int
	consider := func(version, jar string) {
		if jar == "" {
			return
		}
		parsed := parseLombokVersion(version)
		if parsed == nil {
			return
		}
		if bestJar == "" || compareLombokVersions(parsed, bestVersion) > 0 {
			bestJar = jar
			bestVersion = parsed
		}
	}

	gradleBase := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", "org.projectlombok", "lombok")
	for _, versionDir := range listSubdirectories(gradleBase) {
		version := filepath.Base(versionDir)
		matches, err := filepath.Glob(filepath.Join(versionDir, "*", "lombok-"+version+".jar"))
		if err != nil || len(matches) == 0 {
			continue
		}
		consider(version, matches[0])
	}

	mavenBase := filepath.Join(home, ".m2", "repository", "org", "projectlombok", "lombok")
	for _, versionDir := range listSubdirectories(mavenBase) {
		version := filepath.Base(versionDir)
		jar := filepath.Join(versionDir, "lombok-"+version+".jar")
		if _, err := os.Stat(jar); err != nil {
			continue
		}
		consider(version, jar)
	}

	return bestJar
}

func listSubdirectories(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(base, entry.Name()))
		}
	}
	return dirs
}

func parseLombokVersion(version string) []int {
	// Numeric prefix segments only: "1.18.46" -> [1 18 46]; snapshots and
	// edge releases ("edge-SNAPSHOT") are skipped.
	parts := strings.Split(version, ".")
	parsed := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		parsed = append(parsed, number)
	}
	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

func compareLombokVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}
