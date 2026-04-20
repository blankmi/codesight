package pkg

import (
	"os"
	"path/filepath"
	"strings"

	csignore "codesight/pkg/ignore"
)

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

// WalkFiles traverses a directory tree, respecting .gitignore/.csignore and
// returning paths of source code files suitable for indexing.
func WalkFiles(root string, opts *WalkOptions) ([]string, error) {
	matcher, err := matcherForWalk(root, opts)
	if err != nil {
		return nil, err
	}

	return walkFiles(root, opts, matcher)
}

func matcherForWalk(root string, opts *WalkOptions) (*csignore.Matcher, error) {
	return csignore.NewMatcher(root, extraIgnorePatterns(opts))
}

func walkFiles(root string, opts *WalkOptions, matcher *csignore.Matcher) ([]string, error) {
	extensions := extensionsForWalk(opts)
	maxSize := maxFileSizeBytes(opts)

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		if info.IsDir() {
			if matcher.MatchesRelative(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if matcher.MatchesRelative(rel) {
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

func extensionsForWalk(opts *WalkOptions) map[string]bool {
	if opts == nil || opts.Extensions == nil {
		return defaultExtensions
	}
	return opts.Extensions
}

func extraIgnorePatterns(opts *WalkOptions) []string {
	if opts == nil || len(opts.ExtraIgnore) == 0 {
		return nil
	}
	return append([]string(nil), opts.ExtraIgnore...)
}

func maxFileSizeBytes(opts *WalkOptions) int64 {
	if opts == nil || opts.MaxFileSizeKB <= 0 {
		return 0
	}
	return int64(opts.MaxFileSizeKB) * 1024
}
