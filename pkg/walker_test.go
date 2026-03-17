package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkFiles_BasicWalk(t *testing.T) {
	dir := t.TempDir()

	// Create some test files
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "util.py"), "def foo(): pass")
	writeFile(t, filepath.Join(dir, "readme.md"), "# Hello")        // excluded by default extensions
	writeFile(t, filepath.Join(dir, "data.json"), `{"key": "val"}`) // excluded by default extensions

	files, err := WalkFiles(dir, nil)
	if err != nil {
		t.Fatalf("WalkFiles error: %v", err)
	}

	got := map[string]bool{}
	for _, f := range files {
		got[filepath.Base(f)] = true
	}

	if !got["main.go"] {
		t.Error("expected main.go in results")
	}
	if !got["util.py"] {
		t.Error("expected util.py in results")
	}
	if got["readme.md"] {
		t.Error("readme.md should be excluded (not a code extension)")
	}
}

func TestWalkFiles_IgnoresNodeModules(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, "node_modules", "pkg", "index.js"), "module.exports = {}")
	writeFile(t, filepath.Join(dir, "app.js"), "console.log('hi')")

	files, err := WalkFiles(dir, nil)
	if err != nil {
		t.Fatalf("WalkFiles error: %v", err)
	}

	for _, f := range files {
		if filepath.Base(f) == "index.js" {
			t.Error("node_modules files should be ignored")
		}
	}
}

func TestWalkFiles_RespectsGitignore(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, ".gitignore"), "*.generated.go\nbuild/")
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "types.generated.go"), "package main")
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, "build", "output.go"), "package build")

	files, err := WalkFiles(dir, nil)
	if err != nil {
		t.Fatalf("WalkFiles error: %v", err)
	}

	got := map[string]bool{}
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		got[rel] = true
	}

	if !got["main.go"] {
		t.Error("expected main.go")
	}
	if got["types.generated.go"] {
		t.Error("types.generated.go should be ignored via .gitignore")
	}
}

func TestWalkFiles_RespectsCsignoreAlongsideGitignore(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, ".gitignore"), "*.generated.go\n")
	writeFile(t, filepath.Join(dir, ".csignore"), "build/\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "types.generated.go"), "package main")
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, "build", "output.go"), "package build")

	files, err := WalkFiles(dir, nil)
	if err != nil {
		t.Fatalf("WalkFiles error: %v", err)
	}

	got := map[string]bool{}
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		got[rel] = true
	}

	if !got["main.go"] {
		t.Error("expected main.go")
	}
	if got["types.generated.go"] {
		t.Error("types.generated.go should be ignored via .gitignore")
	}
	if got["build/output.go"] {
		t.Error("build/output.go should be ignored via .csignore")
	}
}

func TestWalkFiles_GitignoreDirectoryPatternDoesNotSubstringMatch(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, ".gitignore"), "build/\n")
	writeFile(t, filepath.Join(dir, "rebuild.go"), "package main")
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, "build", "output.go"), "package build")

	files, err := WalkFiles(dir, nil)
	if err != nil {
		t.Fatalf("WalkFiles error: %v", err)
	}

	got := map[string]bool{}
	for _, f := range files {
		got[filepath.Base(f)] = true
	}

	if !got["rebuild.go"] {
		t.Error("rebuild.go should not be ignored by build/ pattern")
	}
	if got["output.go"] {
		t.Error("build/output.go should be ignored via build/ pattern")
	}
}

func TestWalkFiles_CustomExtensions(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "app.ts"), "export default {}")
	writeFile(t, filepath.Join(dir, "style.css"), "body {}")

	files, err := WalkFiles(dir, &WalkOptions{
		Extensions: map[string]bool{".css": true},
	})
	if err != nil {
		t.Fatalf("WalkFiles error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if filepath.Base(files[0]) != "style.css" {
		t.Errorf("expected style.css, got %s", filepath.Base(files[0]))
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
