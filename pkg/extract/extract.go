package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

var errSymbolNotFound = errors.New("symbol not found")

var directorySkipList = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	"__pycache__":  {},
	".idea":        {},
	".vscode":      {},
}

// Extract resolves one named symbol from a file or directory target.
func Extract(targetPath string, symbol string, format string) (string, error) {
	if strings.TrimSpace(targetPath) == "" {
		return "", fmt.Errorf("target path is required")
	}
	if strings.TrimSpace(symbol) == "" {
		return "", fmt.Errorf("symbol is required")
	}

	normalizedFormat, err := normalizeOutputFormat(format)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return "", fmt.Errorf("stat target: %w", err)
	}

	if info.IsDir() {
		return extractFromDirectory(targetPath, symbol, normalizedFormat)
	}

	return extractFromFile(targetPath, symbol, normalizedFormat)
}

func normalizeOutputFormat(format string) (OutputFormat, error) {
	if format == "" {
		return FormatRaw, nil
	}

	switch OutputFormat(strings.ToLower(format)) {
	case FormatRaw:
		return FormatRaw, nil
	case FormatJSON:
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported format %q: expected raw or json", format)
	}
}

func extractFromFile(path string, symbol string, format OutputFormat) (string, error) {
	match, err := findSymbolInFile(path, symbol)
	if err != nil {
		return "", err
	}

	if format == FormatRaw {
		return match.Code, nil
	}

	encoded, err := json.MarshalIndent(match, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}
	return string(encoded), nil
}

func extractFromDirectory(path string, symbol string, format OutputFormat) (string, error) {
	files, err := collectSupportedFiles(path)
	if err != nil {
		return "", err
	}

	matches := make([]SymbolMatch, 0)
	for _, file := range files {
		match, err := findSymbolInFile(file, symbol)
		if err != nil {
			if errors.Is(err, errSymbolNotFound) {
				continue
			}
			return "", err
		}
		matches = append(matches, match)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("%w: %s", errSymbolNotFound, symbol)
	}

	if format == FormatRaw {
		sections := make([]string, 0, len(matches))
		for _, match := range matches {
			sections = append(sections, fmt.Sprintf("=== file: %s ===\n%s", match.FilePath, match.Code))
		}
		return strings.Join(sections, "\n\n"), nil
	}

	encoded, err := json.MarshalIndent(matches, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}

	return string(encoded), nil
}

func collectSupportedFiles(root string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path != root {
				if _, skip := directorySkipList[d.Name()]; skip {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		if _, ok := languageFromExtension(filepath.Ext(path)); !ok {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	sort.Slice(files, func(i int, j int) bool {
		return filepath.ToSlash(files[i]) < filepath.ToSlash(files[j])
	})

	return files, nil
}

func findSymbolInFile(path string, symbol string) (SymbolMatch, error) {
	language, err := languageForPath(path)
	if err != nil {
		return SymbolMatch{}, err
	}

	lang := parserForLanguage(language)
	if lang == nil {
		return SymbolMatch{}, fmt.Errorf("unsupported language %q", language)
	}

	source, err := os.ReadFile(path)
	if err != nil {
		return SymbolMatch{}, fmt.Errorf("read file: %w", err)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return SymbolMatch{}, fmt.Errorf("parse %s: %w", filepath.ToSlash(path), err)
	}
	defer tree.Close()

	node, symbolType, name, ok := findMatchingSymbolNode(tree.RootNode(), source, language, symbol)
	if !ok {
		return SymbolMatch{}, fmt.Errorf("%w: %s", errSymbolNotFound, symbol)
	}

	return SymbolMatch{
		Name:       name,
		Code:       node.Content(source),
		FilePath:   filepath.ToSlash(path),
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
		StartByte:  int(node.StartByte()),
		EndByte:    int(node.EndByte()),
		SymbolType: symbolType,
	}, nil
}

func languageForPath(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	language, ok := languageFromExtension(ext)
	if !ok {
		return "", fmt.Errorf("unsupported language for extension %q", ext)
	}
	return language, nil
}

func findMatchingSymbolNode(node *sitter.Node, source []byte, language string, symbol string) (*sitter.Node, string, string, bool) {
	if node == nil {
		return nil, "", "", false
	}

	// Go const/var specs can declare multiple identifiers in one node.
	if language == "go" {
		if name, symbolType, ok := matchGoNamedSpec(node, source, symbol); ok {
			return node, symbolType, name, true
		}
	}

	if name, symbolType, ok := symbolInfoForNode(node, source, language); ok && name == symbol {
		return node, symbolType, name, true
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		matchedNode, matchedType, matchedName, ok := findMatchingSymbolNode(child, source, language, symbol)
		if ok {
			return matchedNode, matchedType, matchedName, true
		}
	}

	return nil, "", "", false
}

func symbolInfoForNode(node *sitter.Node, source []byte, language string) (string, string, bool) {
	switch language {
	case "go":
		return symbolInfoForGo(node, source)
	case "python":
		return symbolInfoForPython(node, source)
	case "java":
		return symbolInfoForJava(node, source)
	case "javascript", "typescript":
		return symbolInfoForJSTS(node, source)
	case "rust":
		return symbolInfoForRust(node, source)
	case "cpp":
		return symbolInfoForCPP(node, source)
	case "html", "xml":
		return symbolInfoForHTML(node, source)
	default:
		return "", "", false
	}
}

func symbolInfoForGo(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Type() {
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
			switch typeNode.Type() {
			case "struct_type":
				symbolType = "struct"
			case "interface_type":
				symbolType = "interface"
			}
		}
		return name, symbolType, true
	case "const_spec":
		names := namesFromField(node, "name", source)
		if len(names) == 0 {
			return "", "", false
		}
		return names[0], "constant", true
	case "var_spec":
		names := namesFromField(node, "name", source)
		if len(names) == 0 {
			return "", "", false
		}
		return names[0], "variable", true
	default:
		return "", "", false
	}
}

func matchGoNamedSpec(node *sitter.Node, source []byte, symbol string) (string, string, bool) {
	switch node.Type() {
	case "const_spec":
		for _, name := range namesFromField(node, "name", source) {
			if name == symbol {
				return name, "constant", true
			}
		}
	case "var_spec":
		for _, name := range namesFromField(node, "name", source) {
			if name == symbol {
				return name, "variable", true
			}
		}
	}
	return "", "", false
}

func symbolInfoForPython(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Type() {
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

func symbolInfoForJava(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Type() {
	case "method_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "method", true
	case "constructor_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "constructor", true
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
	case "enum_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "enum", true
	default:
		return "", "", false
	}
}

func symbolInfoForJSTS(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Type() {
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
	case "class_declaration", "abstract_class_declaration":
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
	case "type_alias_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "type", true
	case "enum_declaration":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "enum", true
	case "variable_declarator":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		if parent := node.Parent(); parent != nil && parent.Type() == "lexical_declaration" {
			content := strings.TrimSpace(parent.Content(source))
			if strings.HasPrefix(content, "const ") {
				return name, "constant", true
			}
		}
		return name, "variable", true
	default:
		return "", "", false
	}
}

func symbolInfoForRust(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Type() {
	case "function_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "function", true
	case "function_signature_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "method", true
	case "struct_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "struct", true
	case "trait_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "trait", true
	case "enum_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "enum", true
	case "type_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "type", true
	case "const_item":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "constant", true
	default:
		return "", "", false
	}
}

func symbolInfoForCPP(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Type() {
	case "function_definition":
		nameNode := findFunctionNameNode(node.ChildByFieldName("declarator"))
		if nameNode == nil {
			return "", "", false
		}
		name := strings.TrimSpace(nameNode.Content(source))
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
	case "enum_specifier":
		name := nameFromField(node, "name", source)
		if name == "" {
			return "", "", false
		}
		return name, "enum", true
	default:
		return "", "", false
	}
}

func symbolInfoForHTML(node *sitter.Node, source []byte) (string, string, bool) {
	switch node.Type() {
	case "element":
		startTag := findNamedChildByType(node, "start_tag")
		if startTag == nil {
			startTag = findNamedChildByType(node, "self_closing_tag")
		}
		if startTag == nil {
			return "", "", false
		}
		tagName := findNamedChildByType(startTag, "tag_name")
		if tagName == nil {
			return "", "", false
		}
		name := strings.TrimSpace(tagName.Content(source))
		if name == "" {
			return "", "", false
		}
		return name, "element", true
	case "self_closing_tag":
		tagName := findNamedChildByType(node, "tag_name")
		if tagName == nil {
			return "", "", false
		}
		name := strings.TrimSpace(tagName.Content(source))
		if name == "" {
			return "", "", false
		}
		return name, "element", true
	default:
		return "", "", false
	}
}

func nameFromField(node *sitter.Node, field string, source []byte) string {
	if node == nil {
		return ""
	}
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return strings.TrimSpace(child.Content(source))
}

func namesFromField(node *sitter.Node, field string, source []byte) []string {
	if node == nil {
		return nil
	}

	names := make([]string, 0)
	seen := make(map[string]struct{})

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if node.FieldNameForChild(i) != field {
			continue
		}
		name := strings.TrimSpace(child.Content(source))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	return names
}

func findNamedChildByType(node *sitter.Node, nodeType string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Type() == nodeType {
			return child
		}
	}
	return nil
}

func findFunctionNameNode(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}

	switch node.Type() {
	case "identifier", "field_identifier", "operator_name", "destructor_name", "type_identifier":
		return node
	case "qualified_identifier", "scoped_identifier":
		for i := int(node.NamedChildCount()) - 1; i >= 0; i-- {
			child := node.NamedChild(i)
			if named := findFunctionNameNode(child); named != nil {
				return named
			}
		}
		return nil
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if named := findFunctionNameNode(child); named != nil {
			return named
		}
	}

	return nil
}
