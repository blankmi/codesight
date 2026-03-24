package engine

import (
	"encoding/json"
	"path/filepath"
	"strings"

	extractpkg "github.com/blankbytes/codesight/pkg/extract"
	"github.com/blankbytes/codesight/pkg/splitter"
)

// TreeSitterExtractAdapter wraps extract.Extract to implement ExtractProvider.
type TreeSitterExtractAdapter struct{}

// Extract resolves a symbol definition from the workspace root.
func (a *TreeSitterExtractAdapter) Extract(workspaceRoot, symbol string) (*SymDefinition, error) {
	output, err := extractpkg.Extract(workspaceRoot, symbol, "json")
	if err != nil {
		return nil, err
	}

	// extract.Extract in JSON mode may return a single match or an array.
	// Try single match first.
	var match extractpkg.SymbolMatch
	if err := json.Unmarshal([]byte(output), &match); err == nil && match.Name != "" {
		return matchToDefinition(match), nil
	}

	// Try array of matches — pick the first.
	var matches []extractpkg.SymbolMatch
	if err := json.Unmarshal([]byte(output), &matches); err == nil && len(matches) > 0 {
		return matchToDefinition(matches[0]), nil
	}

	return nil, nil
}

func matchToDefinition(m extractpkg.SymbolMatch) *SymDefinition {
	lang := splitter.LanguageFromExtension(filepath.Ext(m.FilePath))
	lines := strings.Split(m.Code, "\n")
	sig := ""
	if len(lines) > 0 {
		sig = strings.TrimRight(lines[0], "\r\n")
	}

	return &SymDefinition{
		File:         m.FilePath,
		Line:         m.StartLine,
		EndLine:      m.EndLine,
		Type:         m.SymbolType,
		Signature:    sig,
		Body:         m.Code,
		ViewStrategy: "full_body",
		Language:     lang,
	}
}
