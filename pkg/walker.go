package pkg

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// defaultIgnorePatterns are always excluded from indexing.
var defaultIgnorePatterns = []string{
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

// defaultExtensions are file extensions to include by default.
var defaultExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".java": true, ".rs": true, ".c": true, ".h": true,
	".cpp": true, ".cc": true, ".cxx": true, ".hpp": true, ".hxx": true,
	".rb": true, ".php": true, ".cs": true, ".swift": true, ".kt": true,
	".scala": true, ".sh": true, ".bash": true, ".sql": true,
}

// WalkOptions configures the file walker.
type WalkOptions struct {
	Extensions    map[string]bool // file extensions to include (nil = defaults)
	ExtraIgnore   []string        // additional patterns to ignore
	MaxFileSizeKB int             // skip files larger than this (0 = no limit)
}

// WalkFiles traverses a directory tree, respecting .gitignore and returning
// paths of source code files suitable for indexing.
func WalkFiles(root string, opts *WalkOptions) ([]string, error) {
	if opts == nil {
		opts = &WalkOptions{}
	}

	extensions := opts.Extensions
	if extensions == nil {
		extensions = defaultExtensions
	}

	ignorePatterns := append([]string{}, defaultIgnorePatterns...)
	ignorePatterns = append(ignorePatterns, opts.ExtraIgnore...)
	gitignore := loadGitignore(root)
	ignorePatterns = append(ignorePatterns, gitignore...)

	maxSize := int64(0)
	if opts.MaxFileSizeKB > 0 {
		maxSize = int64(opts.MaxFileSizeKB) * 1024
	}

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		if info.IsDir() {
			if shouldIgnore(rel, info.Name(), ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldIgnore(rel, info.Name(), ignorePatterns) {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !extensions[ext] {
			return nil
		}

		if maxSize > 0 && info.Size() > maxSize {
			return nil
		}

		files = append(files, path)
		return nil
	})

	return files, err
}

func shouldIgnore(relPath string, name string, patterns []string) bool {
	relPath = filepath.ToSlash(relPath)
	relPath = strings.TrimPrefix(relPath, "./")
	segments := strings.Split(relPath, "/")

	for _, rawPattern := range patterns {
		pattern := strings.TrimSpace(rawPattern)
		if pattern == "" || strings.HasPrefix(pattern, "#") || strings.HasPrefix(pattern, "!") {
			continue
		}

		pattern = filepath.ToSlash(pattern)
		pattern = strings.TrimSuffix(pattern, "/")
		pattern = strings.TrimPrefix(pattern, "./")

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

func loadGitignore(root string) []string {
	path := filepath.Join(root, ".gitignore")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip trailing slash for directory patterns
		line = strings.TrimSuffix(line, "/")
		if line != "" {
			patterns = append(patterns, line)
		}
	}
	return patterns
}
