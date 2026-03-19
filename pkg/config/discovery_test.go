package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestProjectConfigPath_FindsNearestAncestorProjectConfig(t *testing.T) {
	home := setNestedHome(t)
	clearConfigEnv(t)

	repo := filepath.Join(home, "repo")
	nested := filepath.Join(repo, "pkg", "service")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", nested, err)
	}
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(repo, ".codesight", "config.toml"), "project_root = \"..\"\n")

	got, err := projectConfigPath(nested)
	if err != nil {
		t.Fatalf("projectConfigPath returned error: %v", err)
	}

	want := mustResolveConfigPath(t, filepath.Join(repo, ".codesight", "config.toml"))
	if got != want {
		t.Fatalf("projectConfigPath() = %q, want %q", got, want)
	}
}

func TestProjectConfigPath_StopsAtGitBoundary(t *testing.T) {
	home := setNestedHome(t)
	clearConfigEnv(t)

	workspace := filepath.Join(home, "workspace")
	repo := filepath.Join(workspace, "repo")
	nested := filepath.Join(repo, "pkg", "service")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", nested, err)
	}
	writeFile(t, filepath.Join(workspace, ".codesight", "config.toml"), "project_root = \"..\"\n")
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")

	got, err := projectConfigPath(nested)
	if err != nil {
		t.Fatalf("projectConfigPath returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("projectConfigPath() = %q, want empty when config only exists above git boundary", got)
	}
}

func TestProjectConfigPath_StopsAtHomeBoundary(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "user")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", home, err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	clearConfigEnv(t)

	nested := filepath.Join(home, "workspace", "repo", "pkg", "service")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", nested, err)
	}
	writeFile(t, filepath.Join(base, ".codesight", "config.toml"), "project_root = \"..\"\n")

	got, err := projectConfigPath(nested)
	if err != nil {
		t.Fatalf("projectConfigPath returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("projectConfigPath() = %q, want empty when config only exists above home boundary", got)
	}
}

func TestProjectConfigPath_StopsAfterMaxParentsOutsideHome(t *testing.T) {
	_ = setHomeDir(t)
	clearConfigEnv(t)

	base := t.TempDir()
	current := filepath.Join(base, "start")
	for i := 0; i < maxProjectConfigSearchParents+2; i++ {
		current = filepath.Join(current, "d"+strconv.Itoa(i))
	}
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", current, err)
	}

	writeFile(t, filepath.Join(base, ".codesight", "config.toml"), "project_root = \"..\"\n")

	got, err := projectConfigPath(current)
	if err != nil {
		t.Fatalf("projectConfigPath returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("projectConfigPath() = %q, want empty when config exists beyond parent-depth cap", got)
	}
}

func TestLoadConfig_FindsNearestAncestorProjectConfig(t *testing.T) {
	home := setNestedHome(t)
	clearConfigEnv(t)

	repo := filepath.Join(home, "repo")
	nested := filepath.Join(repo, "pkg", "service")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", nested, err)
	}
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(repo, ".codesight", "config.toml"), `
[embedding]
model = "project-model"
`)

	cfg, err := LoadConfig(nested)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	wantConfigDir := mustResolveConfigPath(t, filepath.Join(repo, ".codesight"))
	if cfg.ConfigDir != wantConfigDir {
		t.Fatalf("ConfigDir = %q, want %q", cfg.ConfigDir, wantConfigDir)
	}
	if cfg.Embedding.Model != "project-model" {
		t.Fatalf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "project-model")
	}
	if cfg.Provenance[keyEmbeddingModel] != projectConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyEmbeddingModel, cfg.Provenance[keyEmbeddingModel], projectConfigSource)
	}
}

func TestLoadConfig_DoesNotTreatUserConfigAsProjectConfig(t *testing.T) {
	home := setNestedHome(t)
	clearConfigEnv(t)

	writeFile(t, filepath.Join(home, ".codesight", "config.toml"), `
[embedding]
model = "user-model"
`)

	projectDir := filepath.Join(home, "workspace", "repo", "pkg", "service")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", projectDir, err)
	}
	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.ConfigDir != "" {
		t.Fatalf("ConfigDir = %q, want empty when only user config exists", cfg.ConfigDir)
	}
	if cfg.Embedding.Model != "user-model" {
		t.Fatalf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "user-model")
	}
	if cfg.Provenance[keyEmbeddingModel] != userConfigSource {
		t.Fatalf("Provenance[%q] = %q, want %q", keyEmbeddingModel, cfg.Provenance[keyEmbeddingModel], userConfigSource)
	}
}

func setNestedHome(t *testing.T) string {
	t.Helper()

	home := filepath.Join(t.TempDir(), "home", "user")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", home, err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func mustResolveConfigPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", path, err)
	}
	if evalResolved, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
		resolved = evalResolved
	}
	return filepath.Clean(resolved)
}
