package engine

import (
	"path/filepath"
	"strings"
	"unicode"
)

// QueryIntent represents the classified intent of a query.
type QueryIntent string

const (
	IntentSymbol QueryIntent = "symbol"
	IntentPath   QueryIntent = "path"
	IntentText   QueryIntent = "text"
	IntentAST    QueryIntent = "ast"
)

// knownExtensions are file extensions that indicate a path intent.
var knownExtensions = map[string]struct{}{
	".go": {}, ".ts": {}, ".tsx": {}, ".js": {}, ".jsx": {},
	".py": {}, ".java": {}, ".rs": {}, ".c": {}, ".h": {},
	".cpp": {}, ".cc": {}, ".hpp": {}, ".rb": {}, ".php": {},
	".cs": {}, ".swift": {}, ".kt": {}, ".scala": {}, ".sh": {},
	".sql": {}, ".yaml": {}, ".yml": {}, ".json": {}, ".toml": {},
	".xml": {}, ".html": {}, ".css": {}, ".md": {},
}

// Classify determines the query intent using deterministic heuristics.
// If modeOverride is non-empty and not "auto", it is returned directly.
func Classify(query string, modeOverride string) QueryIntent {
	if modeOverride != "" && modeOverride != "auto" {
		switch QueryIntent(modeOverride) {
		case IntentSymbol, IntentPath, IntentText, IntentAST:
			return QueryIntent(modeOverride)
		}
	}

	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return IntentText
	}

	// Path: contains separator or has a known file extension.
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, string(filepath.Separator)) {
		return IntentPath
	}
	ext := filepath.Ext(trimmed)
	if ext != "" {
		if _, ok := knownExtensions[strings.ToLower(ext)]; ok {
			return IntentPath
		}
	}

	// Text: quoted string.
	if strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "'") {
		return IntentText
	}

	// AST: structural pattern markers (future expansion).
	if strings.ContainsAny(trimmed, "{}") {
		return IntentAST
	}

	// Text: multi-word phrase (checked after AST to let structural patterns win).
	if strings.Contains(trimmed, " ") {
		if !looksLikeQualifiedName(trimmed) {
			return IntentText
		}
	}

	// Default: symbol.
	return IntentSymbol
}

// looksLikeQualifiedName returns true for patterns like "auth.Login" or "pkg.Foo".
func looksLikeQualifiedName(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		for _, r := range part {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}
