package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewMatcherIncludesDefaults(t *testing.T) {
	dir := t.TempDir()
	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	for _, name := range []string{".git", "node_modules", "vendor", "__pycache__", ".DS_Store"} {
		if !m.MatchesRelative(name) {
			t.Errorf("expected default pattern to match %q", name)
		}
	}
}

func TestNewMatcherLoadsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".gitignore"), "*.log\nbuild/\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("app.log") {
		t.Error("expected .gitignore glob *.log to match app.log")
	}
	if !m.MatchesRelative("build/output.js") {
		t.Error("expected .gitignore pattern build/ to match build/output.js")
	}
	if m.MatchesRelative("src/main.go") {
		t.Error("src/main.go should not be ignored")
	}
}

func TestNewMatcherLoadsCsignore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "generated/\n*.pb.go\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("generated/code.go") {
		t.Error("expected .csignore pattern to match generated/code.go")
	}
	if !m.MatchesRelative("api.pb.go") {
		t.Error("expected .csignore glob *.pb.go to match api.pb.go")
	}
}

func TestNewMatcherMergesGitignoreAndCsignore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".gitignore"), "*.log\n")
	writeTestFile(t, filepath.Join(dir, ".csignore"), "*.generated.go\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("app.log") {
		t.Error("expected .gitignore pattern to match")
	}
	if !m.MatchesRelative("types.generated.go") {
		t.Error("expected .csignore pattern to match")
	}
}

func TestNewMatcherExtraPatterns(t *testing.T) {
	dir := t.TempDir()
	m, err := NewMatcher(dir, []string{"custom_dir"})
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("custom_dir") {
		t.Error("expected extra pattern to match custom_dir")
	}
	if !m.MatchesRelative("custom_dir/file.go") {
		t.Error("expected extra pattern to match files inside custom_dir")
	}
}

func TestMatchesRelativeComments(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "# this is a comment\n*.tmp\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("data.tmp") {
		t.Error("expected *.tmp to match")
	}
	if m.MatchesRelative("# this is a comment") {
		t.Error("comment lines should be skipped, not treated as patterns")
	}
}

func TestMatchesRelativeNegationDropped(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "*.log\n!important.log\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	// Negation patterns are silently dropped — important.log is still matched by *.log
	if !m.MatchesRelative("important.log") {
		t.Error("negation patterns are not supported; *.log should still match important.log")
	}
}

func TestMatchesRelativeBlankLines(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "\n  \n*.tmp\n\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("data.tmp") {
		t.Error("expected *.tmp to match despite blank lines in file")
	}
}

func TestMatchesRelativeGlobPatterns(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "*.generated.go\ntest_*.py\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"types.generated.go", true},
		{"pkg/types.generated.go", true},
		{"main.go", false},
		{"test_auth.py", true},
		{"tests/test_auth.py", true},
		{"auth_test.py", false},
	}
	for _, tt := range tests {
		if got := m.MatchesRelative(tt.path); got != tt.want {
			t.Errorf("MatchesRelative(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestMatchesRelativeAnchoredPattern(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "/root_only\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("root_only") {
		t.Error("anchored pattern should match at root")
	}
	if !m.MatchesRelative("root_only/file.go") {
		t.Error("anchored pattern should match contents of root_only/")
	}
}

func TestMatchesRelativeSegmentMatching(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "cache\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"cache", true},
		{"cache/data.bin", true},
		{"pkg/cache/data.bin", true},
		{"cacheutils.go", false},
		{"my_cache_tool", false},
	}
	for _, tt := range tests {
		if got := m.MatchesRelative(tt.path); got != tt.want {
			t.Errorf("MatchesRelative(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestMatchesRelativeSlashContainingPattern(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "docs/internal\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesRelative("docs/internal") {
		t.Error("expected docs/internal to match")
	}
	if !m.MatchesRelative("docs/internal/secret.md") {
		t.Error("expected docs/internal/secret.md to match")
	}
	if m.MatchesRelative("docs/public/file.md") {
		t.Error("docs/public/file.md should not match")
	}
}

func TestMatchesPathAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "*.tmp\n")

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if !m.MatchesPath(filepath.Join(dir, "data.tmp")) {
		t.Error("MatchesPath should match absolute path inside root")
	}
	if m.MatchesPath(filepath.Join(dir, "main.go")) {
		t.Error("MatchesPath should not match non-ignored absolute path")
	}
}

func TestMatchesPathOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if m.MatchesPath(filepath.Join(other, "node_modules")) {
		t.Error("paths outside root should not be matched")
	}
}

func TestMatchesRelativeRootAndDotPaths(t *testing.T) {
	dir := t.TempDir()
	m, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	for _, p := range []string{".", "..", "../outside"} {
		if m.MatchesRelative(p) {
			t.Errorf("MatchesRelative(%q) should return false", p)
		}
	}
}

func TestNilMatcherSafety(t *testing.T) {
	var m *Matcher

	if m.Root() != "" {
		t.Error("nil matcher Root() should return empty string")
	}
	if m.Fingerprint() != "" {
		t.Error("nil matcher Fingerprint() should return empty string")
	}
	if m.MatchesRelative("anything") {
		t.Error("nil matcher MatchesRelative should return false")
	}
	if m.MatchesPath("/any/path") {
		t.Error("nil matcher MatchesPath should return false")
	}
}

func TestFingerprintDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".csignore"), "*.tmp\nbuild/\n")

	m1, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}
	m2, err := NewMatcher(dir, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if m1.Fingerprint() != m2.Fingerprint() {
		t.Error("fingerprints should be identical for same rules")
	}
	if m1.Fingerprint() == "" {
		t.Error("fingerprint should not be empty")
	}
}

func TestFingerprintChangesWithRules(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeTestFile(t, filepath.Join(dir1, ".csignore"), "*.tmp\n")
	writeTestFile(t, filepath.Join(dir2, ".csignore"), "*.log\n")

	m1, err := NewMatcher(dir1, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}
	m2, err := NewMatcher(dir2, nil)
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	if m1.Fingerprint() == m2.Fingerprint() {
		t.Error("fingerprints should differ for different rules")
	}
}

func TestFindProjectRootWithCsignore(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	writeTestFile(t, filepath.Join(root, ".csignore"), "*.tmp\n")

	got := FindProjectRoot(sub)
	if got != filepath.Clean(root) {
		t.Fatalf("FindProjectRoot = %q, want %q", got, root)
	}
}

func TestFindProjectRootWithGitDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) returned error: %v", err)
	}

	got := FindProjectRoot(sub)
	if got != filepath.Clean(root) {
		t.Fatalf("FindProjectRoot = %q, want %q", got, root)
	}
}

func TestFindProjectRootFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	// No markers anywhere — should return the input directory itself
	got := FindProjectRoot(sub)
	if got != filepath.Clean(sub) {
		t.Fatalf("FindProjectRoot = %q, want %q", got, sub)
	}
}

func TestNormalizePatternEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"# comment", "", false},
		{"!negation", "", false},
		{"build/", "build", true},
		{"./relative", "relative", true},
		{"  spaces  ", "spaces", true},
		{"/", "", false},
		{"./", ".", true},
	}
	for _, tt := range tests {
		got, ok := normalizePattern(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("normalizePattern(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDeduplication(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".gitignore"), "*.tmp\nbuild\n")
	writeTestFile(t, filepath.Join(dir, ".csignore"), "*.tmp\nbuild\n")

	m, err := NewMatcher(dir, []string{"*.tmp"})
	if err != nil {
		t.Fatalf("NewMatcher returned error: %v", err)
	}

	// Count how many times *.tmp appears in the patterns
	count := 0
	for _, p := range m.patterns {
		if p == "*.tmp" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected *.tmp to appear once after dedup, got %d", count)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) returned error: %v", path, err)
	}
}
