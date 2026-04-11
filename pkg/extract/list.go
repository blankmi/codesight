package extract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	csignore "github.com/blankbytes/codesight/pkg/ignore"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

var listLanguageAliases = map[string]string{
	"go":         "go",
	"golang":     "go",
	"python":     "python",
	"py":         "python",
	"java":       "java",
	"javascript": "javascript",
	"js":         "javascript",
	"typescript": "typescript",
	"ts":         "typescript",
	"rust":       "rust",
	"rs":         "rust",
	"cpp":        "cpp",
	"c++":        "cpp",
	"xml":        "xml",
	"svg":        "xml",
	"html":       "html",
	"htm":        "html",
}

var listSymbolTypeAliases = map[string]string{
	"":          "",
	"func":      "function",
	"function":  "function",
	"method":    "method",
	"class":     "class",
	"interface": "interface",
	"struct":    "struct",
	"type":      "type",
	"enum":      "enum",
	"trait":     "trait",
	"impl":      "impl",
	"element":   "element",
	"script":    "script",
	"style":     "style",
}

// ListSymbols lists symbols from a file or directory and formats output as raw
// or json. Warnings are returned for recoverable per-file directory failures.
func ListSymbols(targetPath, lang, format, symbolType string) (ListResult, error) {
	if strings.TrimSpace(targetPath) == "" {
		return ListResult{}, fmt.Errorf("target path is required")
	}

	normalizedLang, err := normalizeListLanguage(lang)
	if err != nil {
		return ListResult{}, err
	}

	normalizedType, err := normalizeListSymbolType(symbolType)
	if err != nil {
		return ListResult{}, err
	}

	normalizedFormat, err := normalizeOutputFormat(format)
	if err != nil {
		return ListResult{}, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return ListResult{}, fmt.Errorf("stat target: %w", err)
	}

	if !info.IsDir() {
		matcherRoot := csignore.FindProjectRoot(filepath.Dir(targetPath))
		matcher, err := csignore.NewMatcher(matcherRoot, nil)
		if err != nil {
			return ListResult{}, fmt.Errorf("load ignore rules: %w", err)
		}
		if matcher.MatchesPath(targetPath) {
			return ListResult{}, fmt.Errorf("target path is ignored by .gitignore/.csignore: %s", filepath.ToSlash(targetPath))
		}

		symbols, err := listSymbolsFromFile(targetPath, normalizedLang, normalizedType)
		if err != nil {
			return ListResult{}, err
		}

		output, err := renderListedSymbols(symbols, normalizedFormat, false)
		if err != nil {
			return ListResult{}, err
		}
		return ListResult{Output: output}, nil
	}

	matcher, err := csignore.NewMatcher(targetPath, nil)
	if err != nil {
		return ListResult{}, fmt.Errorf("load ignore rules: %w", err)
	}

	files, err := collectSupportedFilesForList(targetPath, matcher, normalizedLang)
	if err != nil {
		return ListResult{}, err
	}

	var (
		allSymbols     []ListSymbol
		warnings       []string
		failedFiles    int
		processedFiles int
	)

	for _, path := range files {
		symbols, listErr := listSymbolsFromFile(path, normalizedLang, normalizedType)
		if listErr != nil {
			failedFiles++
			warnings = append(warnings, fmt.Sprintf("warning: failed to list symbols in %s: %v", filepath.ToSlash(path), listErr))
			continue
		}

		processedFiles++
		allSymbols = append(allSymbols, symbols...)
	}

	if processedFiles == 0 && failedFiles > 0 {
		return ListResult{}, fmt.Errorf("failed to process any files under %s (%d errors)", targetPath, failedFiles)
	}

	output, err := renderListedSymbols(allSymbols, normalizedFormat, true)
	if err != nil {
		return ListResult{}, err
	}

	return ListResult{
		Output:   output,
		Warnings: warnings,
	}, nil
}

// ListSymbolsSummary summarizes symbols per file for a directory target.
func ListSymbolsSummary(targetPath, lang, format, symbolType string) (ListResult, error) {
	if strings.TrimSpace(targetPath) == "" {
		return ListResult{}, fmt.Errorf("target path is required")
	}

	normalizedLang, err := normalizeListLanguage(lang)
	if err != nil {
		return ListResult{}, err
	}

	normalizedType, err := normalizeListSymbolType(symbolType)
	if err != nil {
		return ListResult{}, err
	}

	normalizedFormat, err := normalizeOutputFormat(format)
	if err != nil {
		return ListResult{}, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return ListResult{}, fmt.Errorf("stat target: %w", err)
	}
	if !info.IsDir() {
		return ListResult{}, fmt.Errorf("summary mode requires a directory target: %s", filepath.ToSlash(targetPath))
	}

	matcher, err := csignore.NewMatcher(targetPath, nil)
	if err != nil {
		return ListResult{}, fmt.Errorf("load ignore rules: %w", err)
	}

	files, err := collectSupportedFilesForList(targetPath, matcher, normalizedLang)
	if err != nil {
		return ListResult{}, err
	}

	summary := ListSummaryResult{
		DirPath: filepath.ToSlash(targetPath),
		Files:   make([]FileSummary, 0, len(files)),
	}

	failedFiles := 0
	for _, path := range files {
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			failedFiles++
			warning := fmt.Sprintf("warning: failed to summarize symbols in %s: read file: %v", filepath.ToSlash(path), readErr)
			summary.Warnings = append(summary.Warnings, warning)
			continue
		}

		symbols, listErr := listSymbolsFromSource(path, source, normalizedLang, normalizedType)
		if listErr != nil {
			failedFiles++
			warning := fmt.Sprintf("warning: failed to summarize symbols in %s: %v", filepath.ToSlash(path), listErr)
			summary.Warnings = append(summary.Warnings, warning)
			continue
		}

		relativePath, relErr := filepath.Rel(targetPath, path)
		if relErr != nil {
			relativePath = path
		}
		relativePath = filepath.ToSlash(relativePath)

		lineCount := bytes.Count(source, []byte{'\n'}) + 1
		symbolCounts := make(map[string]int)
		for _, symbol := range symbols {
			symbolCounts[symbol.SymbolType]++
		}

		summary.Files = append(summary.Files, FileSummary{
			FilePath:     relativePath,
			TotalLines:   lineCount,
			SymbolCounts: symbolCounts,
		})
		summary.TotalLines += lineCount
	}

	if len(summary.Files) == 0 && failedFiles > 0 {
		return ListResult{}, fmt.Errorf("failed to process any files under %s (%d errors)", targetPath, failedFiles)
	}

	summary.FileCount = len(summary.Files)

	output, err := renderSummary(summary, normalizedFormat)
	if err != nil {
		return ListResult{}, err
	}

	return ListResult{
		Output:   output,
		Warnings: summary.Warnings,
	}, nil
}

func normalizeListLanguage(language string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(language))
	if trimmed == "" {
		return "", nil
	}
	normalized, ok := listLanguageAliases[trimmed]
	if !ok {
		return "", fmt.Errorf("unsupported language filter %q", language)
	}
	return normalized, nil
}

func normalizeListSymbolType(symbolType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(symbolType))
	value, ok := listSymbolTypeAliases[normalized]
	if !ok {
		return "", fmt.Errorf("unsupported symbol type %q", symbolType)
	}
	return value, nil
}

func collectSupportedFilesForList(root string, matcher *csignore.Matcher, languageFilter string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if path != root && matcher.MatchesPath(path) {
				return filepath.SkipDir
			}
			return nil
		}

		if !entry.Type().IsRegular() {
			return nil
		}
		if matcher.MatchesPath(path) {
			return nil
		}

		extLanguage, ok := languageFromExtension(filepath.Ext(path))
		if !ok {
			return nil
		}
		if languageFilter != "" && extLanguage != languageFilter {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		return filepath.ToSlash(files[i]) < filepath.ToSlash(files[j])
	})

	if len(files) == 0 {
		if languageFilter != "" {
			return nil, fmt.Errorf("no supported files found under %s for language %q", root, languageFilter)
		}
		return nil, fmt.Errorf("no supported files found under %s", root)
	}

	return files, nil
}

func listSymbolsFromFile(path string, languageFilter string, symbolType string) ([]ListSymbol, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return listSymbolsFromSource(path, source, languageFilter, symbolType)
}

func listSymbolsFromSource(path string, source []byte, languageFilter string, symbolType string) ([]ListSymbol, error) {
	language := languageFilter
	if language == "" {
		resolvedLanguage, err := languageForPath(path)
		if err != nil {
			return nil, err
		}
		language = resolvedLanguage
	}

	parserLanguage := parserForLanguage(language)
	if parserLanguage == nil {
		return nil, fmt.Errorf("unsupported language %q", language)
	}

	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(parserLanguage); err != nil {
		return nil, fmt.Errorf("set language: %w", err)
	}

	tree := parseSource(parser, source)
	if tree == nil {
		return nil, fmt.Errorf("parse %s: parser returned nil tree", filepath.ToSlash(path))
	}
	defer tree.Close()

	seen := map[uint64]struct{}{}
	results := make([]ListSymbol, 0)
	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		name, matchedType, ok := listSymbolInfoForNode(node, source, language)
		if ok && listSymbolTypeMatches(symbolType, matchedType) {
			key := (uint64(node.StartByte()) << 32) | uint64(node.EndByte())
			if _, exists := seen[key]; !exists {
				startLine := int(node.StartPosition().Row) + 1
				endLine := int(node.EndPosition().Row) + 1
				loc := endLine - startLine + 1
				if loc < 0 {
					loc = 0
				}
				results = append(results, ListSymbol{
					Name:       name,
					Code:       nodeContent(node, source),
					FilePath:   filepath.ToSlash(path),
					StartLine:  startLine,
					EndLine:    endLine,
					StartByte:  int(node.StartByte()),
					EndByte:    int(node.EndByte()),
					SymbolType: matchedType,
					LOC:        loc,
				})
				seen[key] = struct{}{}
			}
		}

		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child == nil {
				continue
			}
			walk(child)
		}
	}

	walk(tree.RootNode())
	return results, nil
}

func listSymbolTypeMatches(filter, matchedType string) bool {
	if filter == "" {
		return true
	}
	if filter == matchedType {
		return true
	}
	return filter == "function" && matchedType == "method"
}

func listSymbolInfoForNode(node *sitter.Node, source []byte, language string) (string, string, bool) {
	switch language {
	case "go":
		return listSymbolInfoForGo(node, source)
	case "python":
		return listSymbolInfoForPython(node, source)
	case "java":
		return listSymbolInfoForJava(node, source)
	case "javascript":
		return listSymbolInfoForJSTS(node, source, false)
	case "typescript":
		return listSymbolInfoForJSTS(node, source, true)
	case "rust":
		return listSymbolInfoForRust(node, source)
	case "cpp":
		return listSymbolInfoForCPP(node, source)
	case "html", "xml":
		return listSymbolInfoForHTML(node, source)
	default:
		return "", "", false
	}
}

func listSymbolInfoForGo(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Kind() {
	case "function_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "function", true
	case "method_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "method", true
	case "type_spec":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		symbolType := "type"
		if typeNode := node.ChildByFieldName("type"); typeNode != nil {
			switch typeNode.Kind() {
			case "struct_type":
				symbolType = "struct"
			case "interface_type":
				symbolType = "interface"
			}
		}
		return name, symbolType, true
	default:
		return "", "", false
	}
}

func listSymbolInfoForPython(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Kind() {
	case "function_definition", "async_function_definition":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "function", true
	case "class_definition":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "class", true
	default:
		return "", "", false
	}
}

func listSymbolInfoForJava(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Kind() {
	case "method_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "method", true
	case "class_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "class", true
	case "interface_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "interface", true
	default:
		return "", "", false
	}
}

func listSymbolInfoForJSTS(node *sitter.Node, source []byte, includeInterfaces bool) (string, string, bool) {
	switch node.Kind() {
	case "function_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "function", true
	case "method_definition":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "method", true
	case "class_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "class", true
	case "interface_declaration":
		if !includeInterfaces {
			return "", "", false
		}
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "interface", true
	default:
		return "", "", false
	}
}

func listSymbolInfoForRust(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Kind() {
	case "function_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "function", true
	case "struct_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "struct", true
	case "enum_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "enum", true
	case "trait_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "trait", true
	case "impl_item":
		typeNode := node.ChildByFieldName("type")
		if typeNode == nil {
			return "", "", false
		}
		name := strings.TrimSpace(nodeContent(typeNode, source))
		if name == "" {
			return "", "", false
		}
		return name, "impl", true
	default:
		return "", "", false
	}
}

func listSymbolInfoForCPP(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Kind() {
	case "function_definition":
		nameNode := findFunctionNameNode(node.ChildByFieldName("declarator"))
		if nameNode == nil {
			return "", "", false
		}
		name := strings.TrimSpace(nodeContent(nameNode, source))
		if name == "" {
			return "", "", false
		}
		return name, "function", true
	case "class_specifier":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "class", true
	case "struct_specifier":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "struct", true
	default:
		return "", "", false
	}
}

func listSymbolInfoForHTML(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Kind() {
	case "script_element":
		startTag := findNamedChildByType(node, "start_tag")
		tagName := findNamedChildByType(startTag, "tag_name")
		if tagName == nil {
			return "", "", false
		}
		name := strings.TrimSpace(nodeContent(tagName, source))
		if name == "" {
			return "", "", false
		}
		return name, "script", true
	case "style_element":
		startTag := findNamedChildByType(node, "start_tag")
		tagName := findNamedChildByType(startTag, "tag_name")
		if tagName == nil {
			return "", "", false
		}
		name := strings.TrimSpace(nodeContent(tagName, source))
		if name == "" {
			return "", "", false
		}
		return name, "style", true
	case "element":
		startTag := findNamedChildByType(node, "start_tag")
		if startTag == nil {
			return "", "", false
		}
		tagName := findNamedChildByType(startTag, "tag_name")
		if tagName == nil {
			return "", "", false
		}
		name := strings.TrimSpace(nodeContent(tagName, source))
		if name == "" {
			return "", "", false
		}
		return name, "element", true
	case "self_closing_tag":
		tagName := findNamedChildByType(node, "tag_name")
		if tagName == nil {
			return "", "", false
		}
		name := strings.TrimSpace(nodeContent(tagName, source))
		if name == "" {
			return "", "", false
		}
		return name, "element", true
	default:
		return "", "", false
	}
}

func renderSummary(summary ListSummaryResult, format OutputFormat) (string, error) {
	switch format {
	case FormatJSON:
		payload, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal output: %w", err)
		}
		return string(payload), nil
	case FormatRaw:
		header := fmt.Sprintf("%s  (%d files, %d lines)", summary.DirPath, summary.FileCount, summary.TotalLines)
		if len(summary.Files) == 0 {
			return header, nil
		}

		maxPathWidth := 0
		maxLineWidth := 0
		for _, file := range summary.Files {
			if len(file.FilePath) > maxPathWidth {
				maxPathWidth = len(file.FilePath)
			}
			lineWidth := len(strconv.Itoa(file.TotalLines))
			if lineWidth > maxLineWidth {
				maxLineWidth = lineWidth
			}
		}

		lines := make([]string, 0, len(summary.Files))
		for _, file := range summary.Files {
			typeNames := make([]string, 0, len(file.SymbolCounts))
			for symbolType := range file.SymbolCounts {
				typeNames = append(typeNames, symbolType)
			}
			sort.Strings(typeNames)

			countParts := make([]string, 0, len(typeNames))
			for _, symbolType := range typeNames {
				countParts = append(countParts, fmt.Sprintf("%s(%d)", symbolType, file.SymbolCounts[symbolType]))
			}

			line := fmt.Sprintf("  %-*s %*d lines", maxPathWidth, file.FilePath, maxLineWidth, file.TotalLines)
			if len(countParts) > 0 {
				line = fmt.Sprintf("%s    %s", line, strings.Join(countParts, " "))
			}
			lines = append(lines, line)
		}
		return header + "\n\n" + strings.Join(lines, "\n"), nil
	default:
		return "", fmt.Errorf("unsupported format %q: expected raw or json", format)
	}
}

func renderListedSymbols(symbols []ListSymbol, format OutputFormat, includePath bool) (string, error) {
	switch format {
	case FormatJSON:
		payload, err := json.MarshalIndent(symbols, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal output: %w", err)
		}
		return string(payload), nil
	case FormatRaw:
		lines := make([]string, 0, len(symbols))
		for _, symbol := range symbols {
			if includePath {
				lines = append(lines, fmt.Sprintf("%s\t%s\t%s\tL%d-L%d\tLOC=%d",
					symbol.FilePath,
					symbol.SymbolType,
					symbol.Name,
					symbol.StartLine,
					symbol.EndLine,
					symbol.LOC,
				))
				continue
			}
			lines = append(lines, fmt.Sprintf("%s\t%s\tL%d-L%d\tLOC=%d",
				symbol.SymbolType,
				symbol.Name,
				symbol.StartLine,
				symbol.EndLine,
				symbol.LOC,
			))
		}
		return strings.Join(lines, "\n"), nil
	default:
		return "", fmt.Errorf("unsupported format %q: expected raw or json", format)
	}
}
