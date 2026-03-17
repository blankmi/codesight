package ignore

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var defaultPatterns = []string{
	".git",
	"node_modules",
	"vendor",
	"dist",
	"build",
	".next",
	"__pycache__",
	".pytest_cache",
	".mypy_cache",
	"target",
	".idea",
	".vscode",
	".DS_Store",
}

// Matcher evaluates ignore patterns for a specific target root.
type Matcher struct {
	root        string
	patterns    []string
	fingerprint string
}

// NewMatcher loads the effective ignore rules for root from defaults, extra
// patterns, .gitignore, and .csignore.
func NewMatcher(root string, extraPatterns []string) (*Matcher, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve ignore root: %w", err)
	}

	patterns := append([]string{}, defaultPatterns...)
	patterns = append(patterns, extraPatterns...)
	patterns = append(patterns, loadIgnoreFile(absRoot, ".gitignore")...)
	patterns = append(patterns, loadIgnoreFile(absRoot, ".csignore")...)
	patterns = normalizePatterns(patterns)

	return &Matcher{
		root:        filepath.Clean(absRoot),
		patterns:    patterns,
		fingerprint: fingerprintForPatterns(patterns),
	}, nil
}

// Root returns the absolute root associated with this matcher.
func (m *Matcher) Root() string {
	if m == nil {
		return ""
	}
	return m.root
}

// Fingerprint returns a deterministic hash of the effective ignore rules.
func (m *Matcher) Fingerprint() string {
	if m == nil {
		return ""
	}
	return m.fingerprint
}

// MatchesRelative reports whether relPath should be ignored relative to Root.
func (m *Matcher) MatchesRelative(relPath string) bool {
	if m == nil {
		return false
	}
	return matches(relPath, m.patterns)
}

// MatchesPath reports whether targetPath should be ignored under Root.
func (m *Matcher) MatchesPath(targetPath string) bool {
	if m == nil || strings.TrimSpace(targetPath) == "" {
		return false
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		absPath = filepath.Clean(targetPath)
	}

	relPath, err := filepath.Rel(m.root, absPath)
	if err != nil {
		return false
	}

	relPath = filepath.ToSlash(relPath)
	if relPath == "." || relPath == ".." || strings.HasPrefix(relPath, "../") {
		return false
	}

	return m.MatchesRelative(relPath)
}

// FindProjectRoot walks up from dir looking for a directory containing .git,
// .gitignore, or .csignore. Returns dir itself if nothing is found.
func FindProjectRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}

	current := filepath.Clean(abs)
	for {
		for _, marker := range []string{".git", ".gitignore", ".csignore"} {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return filepath.Clean(abs)
}

func loadIgnoreFile(root string, name string) []string {
	path := filepath.Join(root, name)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		patterns = append(patterns, scanner.Text())
	}
	return patterns
}

func normalizePatterns(patterns []string) []string {
	seen := make(map[string]struct{}, len(patterns))
	normalized := make([]string, 0, len(patterns))

	for _, rawPattern := range patterns {
		pattern, ok := normalizePattern(rawPattern)
		if !ok {
			continue
		}
		if _, exists := seen[pattern]; exists {
			continue
		}
		seen[pattern] = struct{}{}
		normalized = append(normalized, pattern)
	}

	return normalized
}

func normalizePattern(rawPattern string) (string, bool) {
	pattern := strings.TrimSpace(rawPattern)
	if pattern == "" || strings.HasPrefix(pattern, "#") || strings.HasPrefix(pattern, "!") {
		return "", false
	}

	pattern = filepath.ToSlash(pattern)
	pattern = strings.TrimSuffix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "./")
	if pattern == "" || pattern == "/" {
		return "", false
	}

	return pattern, true
}

func fingerprintForPatterns(patterns []string) string {
	normalized := append([]string(nil), patterns...)
	sort.Strings(normalized)
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return fmt.Sprintf("%x", sum[:])
}

func matches(relPath string, patterns []string) bool {
	relPath = filepath.ToSlash(relPath)
	relPath = strings.TrimPrefix(relPath, "./")
	if relPath == "" || relPath == "." {
		return false
	}

	name := path.Base(relPath)
	segments := strings.Split(relPath, "/")

	for _, rawPattern := range patterns {
		pattern := rawPattern
		anchored := strings.HasPrefix(pattern, "/")
		pattern = strings.TrimPrefix(pattern, "/")
		if pattern == "" {
			continue
		}

		if name == pattern {
			return true
		}

		if strings.ContainsAny(pattern, "*?[]") {
			if matched, _ := path.Match(pattern, name); matched {
				return true
			}
			if matched, _ := path.Match(pattern, relPath); matched {
				return true
			}
			if !anchored {
				for i := 0; i < len(segments); i++ {
					candidate := strings.Join(segments[i:], "/")
					if matched, _ := path.Match(pattern, candidate); matched {
						return true
					}
				}
			}
			continue
		}

		if strings.Contains(pattern, "/") {
			if relPath == pattern || strings.HasPrefix(relPath, pattern+"/") {
				return true
			}
			if !anchored {
				if strings.Contains(relPath, "/"+pattern+"/") || strings.HasSuffix(relPath, "/"+pattern) {
					return true
				}
			}
			continue
		}

		for _, segment := range segments {
			if segment == pattern {
				return true
			}
		}
	}

	return false
}
