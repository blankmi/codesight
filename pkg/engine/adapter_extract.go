package engine

import (
	"encoding/json"
	"path/filepath"
	"strings"

	extractpkg "codesight/pkg/extract"
	"codesight/pkg/splitter"
)

// TreeSitterExtractAdapter wraps extract.Extract to implement ExtractProvider.
type TreeSitterExtractAdapter struct{}

// Extract resolves a symbol definition from the workspace root.
func (a *TreeSitterExtractAdapter) Extract(workspaceRoot, symbol string) ([]*SymDefinition, error) {
	output, err := extractpkg.Extract(workspaceRoot, symbol, "json")
	if err != nil {
		return nil, err
	}

	// extract.Extract in JSON mode may return a single match or an array.
	// Try single match first.
	var match extractpkg.SymbolMatch
	if err := json.Unmarshal([]byte(output), &match); err == nil && match.Name != "" {
		return []*SymDefinition{matchToDefinition(workspaceRoot, match)}, nil
	}

	// Try array of matches.
	var matches []extractpkg.SymbolMatch
	if err := json.Unmarshal([]byte(output), &matches); err == nil && len(matches) > 0 {
		defs := make([]*SymDefinition, 0, len(matches))
		for _, m := range matches {
			defs = append(defs, matchToDefinition(workspaceRoot, m))
		}
		return defs, nil
	}

	return nil, nil
}

func matchToDefinition(workspaceRoot string, m extractpkg.SymbolMatch) *SymDefinition {
	lang := splitter.LanguageFromExtension(filepath.Ext(m.FilePath))
	lines := strings.Split(m.Code, "\n")
	sig := ""
	if len(lines) > 0 {
		sig = strings.TrimRight(lines[0], "\r\n")
	}

	return &SymDefinition{
		File:         relativeDefinitionPath(workspaceRoot, m.FilePath),
		Line:         m.StartLine,
		EndLine:      m.EndLine,
		Type:         m.SymbolType,
		Signature:    sig,
		Body:         m.Code,
		ViewStrategy: "full_body",
		Language:     lang,
	}
}

func relativeDefinitionPath(workspaceRoot, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}

	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(filepath.Clean(absPath))
	}
	return filepath.ToSlash(filepath.Clean(rel))
}
