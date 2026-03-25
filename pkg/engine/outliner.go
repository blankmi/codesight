package engine

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/blankbytes/codesight/pkg/splitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

const (
	maxOutlineEntries     = 12
	outlineMethodThreshold = 160 // body lines above which we expand top methods
	maxTopMethods         = 4
)

// classLikeTypes are symbol types that should get outline rendering.
var classLikeTypes = map[string]bool{
	"class":     true,
	"struct":    true,
	"interface": true,
	"module":    true,
	"trait":     true,
	"enum":      true,
}

// IsClassLikeType returns true if the symbol type should get outline rendering.
func IsClassLikeType(symbolType string) bool {
	return classLikeTypes[strings.ToLower(symbolType)]
}

// failureCues are query terms that indicate debugging intent.
var failureCues = []string{
	"error", "err ", "exception", "failed", "failure", "bug",
	"fix", "panic", "stack", "trace", "crash", "fault",
	"debug", "broken",
}

// isFailureCueQuery returns true if the query suggests failure/debugging intent.
func isFailureCueQuery(query string) bool {
	lower := strings.ToLower(query)
	for _, cue := range failureCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// OutlineClassDefinition extracts a signature, member outline, and optionally
// top method bodies for a class-like symbol. Returns (signature, outline, topMethods, viewStrategy).
func OutlineClassDefinition(source []byte, language, symbolName string, startLine, endLine, budgetLines int) (string, []OutlineEntry, []CodeSlice, string) {
	bodyLines := endLine - startLine + 1
	if bodyLines <= budgetLines {
		return extractSignature(source, startLine), nil, nil, "full_body"
	}

	sig := signatureLine(source, startLine)

	// Parse AST.
	lang := splitter.GetLanguage(language)
	if lang == nil {
		// Fallback: can't outline without AST.
		return sig, nil, nil, ""
	}

	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(lang); err != nil {
		return sig, nil, nil, ""
	}

	tree := parser.ParseCtx(context.Background(), source, nil)
	if tree == nil {
		return sig, nil, nil, ""
	}
	defer tree.Close()

	root := tree.RootNode()
	outline := extractMembers(root, source, language, symbolName, startLine, endLine)

	var topMethods []CodeSlice
	if bodyLines > outlineMethodThreshold {
		topMethods = selectAndExpandTopMethods(outline, source, "", budgetLines)
	}

	return sig, outline, topMethods, "signature_plus_outline"
}

// extractMembers walks the AST to find members of a class-like symbol.
func extractMembers(root *sitter.Node, source []byte, language, symbolName string, startLine, endLine int) []OutlineEntry {
	// Find the class declaration node by name and start line.
	classNode := findClassNode(root, source, symbolName, startLine)
	if classNode == nil {
		return nil
	}

	var entries []OutlineEntry

	switch language {
	case "go":
		entries = extractGoMembers(root, classNode, source, symbolName, startLine, endLine)
	case "python":
		entries = extractPythonMembers(classNode, source)
	case "java":
		entries = extractJavaMembers(classNode, source)
	case "javascript", "typescript":
		entries = extractJSTSMembers(classNode, source)
	case "rust":
		entries = extractRustMembers(root, classNode, source, symbolName)
	case "cpp":
		entries = extractCPPMembers(classNode, source)
	}

	if len(entries) > maxOutlineEntries {
		entries = entries[:maxOutlineEntries]
	}
	return entries
}

// classNodeKinds are tree-sitter node kinds that represent class-like declarations.
var classNodeKinds = map[string]bool{
	// Go
	"type_declaration": true,
	"type_spec":        true,
	// Python
	"class_definition": true,
	// Java
	"class_declaration":     true,
	"interface_declaration": true,
	"enum_declaration":      true,
	// JS/TS
	"abstract_class_declaration": true,
	// Rust
	"struct_item": true,
	"trait_item":  true,
	"enum_item":   true,
	// C++
	"class_specifier":  true,
	"struct_specifier": true,
}

// findClassNode finds a class/struct/interface declaration node by matching
// the symbol name and approximate start line.
func findClassNode(node *sitter.Node, source []byte, symbolName string, startLine int) *sitter.Node {
	if node == nil {
		return nil
	}

	nodeStart := int(node.StartPosition().Row) + 1

	if classNodeKinds[node.Kind()] && nodeStart == startLine {
		// Check if this node's name matches.
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := string(source[nameNode.StartByte():nameNode.EndByte()])
			if name == symbolName {
				return node
			}
		}
	}

	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if result := findClassNode(child, source, symbolName, startLine); result != nil {
			return result
		}
	}

	return nil
}

// --- Go ---

func extractGoMembers(root, classNode *sitter.Node, source []byte, symbolName string, startLine, endLine int) []OutlineEntry {
	var entries []OutlineEntry

	// Resolve the type node (struct_type or interface_type) from the class node.
	typeNode := classNode.ChildByFieldName("type")
	if typeNode == nil {
		typeNode = classNode // fallback if classNode is already the type node
	}

	switch typeNode.Kind() {
	case "struct_type":
		bodyNode := findChildByKind(typeNode, "field_declaration_list")
		if bodyNode != nil {
			for i := uint(0); i < bodyNode.NamedChildCount(); i++ {
				child := bodyNode.NamedChild(i)
				if child == nil {
					continue
				}
				entry := goMemberEntry(child, source)
				if entry.Name != "" {
					entries = append(entries, entry)
				}
			}
		}
	case "interface_type":
		// In the Go tree-sitter grammar, interface methods are direct children
		// of interface_type as "method_elem" nodes.
		for i := uint(0); i < typeNode.NamedChildCount(); i++ {
			child := typeNode.NamedChild(i)
			if child == nil {
				continue
			}
			entry := goMemberEntry(child, source)
			if entry.Name != "" {
				entries = append(entries, entry)
			}
		}
	}

	// Second pass: find methods with matching receiver type in the file.
	entries = append(entries, findGoMethodsByReceiver(root, source, symbolName)...)

	return entries
}

func goMemberEntry(node *sitter.Node, source []byte) OutlineEntry {
	kind := node.Kind()
	switch kind {
	case "field_declaration":
		// Field names may be in child nodes.
		name := ""
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child != nil && child.Kind() == "field_identifier" {
				name = string(source[child.StartByte():child.EndByte()])
				break
			}
		}
		if name == "" {
			// Embedded type.
			name = strings.TrimSpace(firstLine(string(source[node.StartByte():node.EndByte()])))
		}
		return OutlineEntry{
			Name:       name,
			Kind:       "field",
			Line:       int(node.StartPosition().Row) + 1,
			EndLine:    int(node.EndPosition().Row) + 1,
			Signature:  strings.TrimSpace(firstLine(string(source[node.StartByte():node.EndByte()]))),
			Visibility: goVisibility(name),
		}
	case "method_spec", "method_elem":
		name := ""
		// method_elem uses field_identifier as first named child for the name.
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = string(source[nameNode.StartByte():nameNode.EndByte()])
		}
		if name == "" {
			// Fallback: look for field_identifier child.
			for i := uint(0); i < node.NamedChildCount(); i++ {
				child := node.NamedChild(i)
				if child != nil && child.Kind() == "field_identifier" {
					name = string(source[child.StartByte():child.EndByte()])
					break
				}
			}
		}
		return OutlineEntry{
			Name:       name,
			Kind:       "method",
			Line:       int(node.StartPosition().Row) + 1,
			EndLine:    int(node.EndPosition().Row) + 1,
			Signature:  strings.TrimSpace(firstLine(string(source[node.StartByte():node.EndByte()]))),
			Visibility: goVisibility(name),
		}
	}
	return OutlineEntry{}
}

func findGoMethodsByReceiver(root *sitter.Node, source []byte, typeName string) []OutlineEntry {
	var entries []OutlineEntry
	for i := uint(0); i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		if child == nil || child.Kind() != "method_declaration" {
			continue
		}
		receiverNode := child.ChildByFieldName("receiver")
		if receiverNode == nil {
			continue
		}
		recContent := string(source[receiverNode.StartByte():receiverNode.EndByte()])
		// Match "*TypeName" or "TypeName" in receiver.
		if !strings.Contains(recContent, typeName) {
			continue
		}

		name := ""
		if nameNode := child.ChildByFieldName("name"); nameNode != nil {
			name = string(source[nameNode.StartByte():nameNode.EndByte()])
		}
		if name == "" {
			continue
		}

		entries = append(entries, OutlineEntry{
			Name:       name,
			Kind:       "method",
			Line:       int(child.StartPosition().Row) + 1,
			EndLine:    int(child.EndPosition().Row) + 1,
			Signature:  declarationLine(child, source),
			Visibility: goVisibility(name),
		})
	}
	return entries
}

func goVisibility(name string) string {
	if name == "" {
		return ""
	}
	r := []rune(name)
	if unicode.IsUpper(r[0]) {
		return "public"
	}
	return "private"
}

// --- Python ---

func extractPythonMembers(classNode *sitter.Node, source []byte) []OutlineEntry {
	var entries []OutlineEntry
	bodyNode := classNode.ChildByFieldName("body")
	if bodyNode == nil {
		return nil
	}

	for i := uint(0); i < bodyNode.NamedChildCount(); i++ {
		child := bodyNode.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "function_definition", "async_function_definition":
			name := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			kind := "method"
			if name == "__init__" {
				kind = "constructor"
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       kind,
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: pythonVisibility(name),
			})
		case "expression_statement":
			// Class-level assignment (class variable).
			content := declarationLine(child, source)
			if strings.Contains(content, "=") {
				name := strings.TrimSpace(strings.SplitN(content, "=", 2)[0])
				// Remove type annotation.
				if colonIdx := strings.Index(name, ":"); colonIdx >= 0 {
					name = strings.TrimSpace(name[:colonIdx])
				}
				entries = append(entries, OutlineEntry{
					Name:       name,
					Kind:       "field",
					Line:       int(child.StartPosition().Row) + 1,
					EndLine:    int(child.EndPosition().Row) + 1,
					Signature:  content,
					Visibility: pythonVisibility(name),
				})
			}
		case "decorated_definition":
			// Unwrap the decorated definition.
			for j := uint(0); j < child.NamedChildCount(); j++ {
				inner := child.NamedChild(j)
				if inner == nil {
					continue
				}
				if inner.Kind() == "function_definition" || inner.Kind() == "async_function_definition" {
					name := ""
					if nameNode := inner.ChildByFieldName("name"); nameNode != nil {
						name = string(source[nameNode.StartByte():nameNode.EndByte()])
					}
					entries = append(entries, OutlineEntry{
						Name:       name,
						Kind:       "method",
						Line:       int(child.StartPosition().Row) + 1,
						EndLine:    int(child.EndPosition().Row) + 1,
						Signature:  declarationLine(inner, source),
						Visibility: pythonVisibility(name),
					})
				}
			}
		}
	}
	return entries
}

func pythonVisibility(name string) string {
	if strings.HasPrefix(name, "__") && !strings.HasSuffix(name, "__") {
		return "private"
	}
	if strings.HasPrefix(name, "_") {
		return "private"
	}
	return "public"
}

// --- Java ---

func extractJavaMembers(classNode *sitter.Node, source []byte) []OutlineEntry {
	var entries []OutlineEntry
	bodyNode := classNode.ChildByFieldName("body")
	if bodyNode == nil {
		return nil
	}

	for i := uint(0); i < bodyNode.NamedChildCount(); i++ {
		child := bodyNode.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "method_declaration":
			name := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "method",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: javaVisibility(child, source),
			})
		case "constructor_declaration":
			name := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "constructor",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: javaVisibility(child, source),
			})
		case "field_declaration":
			content := declarationLine(child, source)
			name := extractJavaFieldName(child, source)
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "field",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  content,
				Visibility: javaVisibility(child, source),
			})
		case "class_declaration", "interface_declaration", "enum_declaration":
			name := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "nested_type",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: javaVisibility(child, source),
			})
		}
	}
	return entries
}

func extractJavaFieldName(node *sitter.Node, source []byte) string {
	// Walk children looking for variable_declarator → name.
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == "variable_declarator" {
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				return string(source[nameNode.StartByte():nameNode.EndByte()])
			}
		}
	}
	return ""
}

func javaVisibility(node *sitter.Node, source []byte) string {
	// Check for modifier nodes among children.
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Kind() == "modifiers" {
			content := string(source[child.StartByte():child.EndByte()])
			if strings.Contains(content, "public") {
				return "public"
			}
			if strings.Contains(content, "private") {
				return "private"
			}
			if strings.Contains(content, "protected") {
				return "protected"
			}
		}
	}
	return ""
}

// --- JavaScript / TypeScript ---

func extractJSTSMembers(classNode *sitter.Node, source []byte) []OutlineEntry {
	var entries []OutlineEntry
	bodyNode := classNode.ChildByFieldName("body")
	if bodyNode == nil {
		return nil
	}

	for i := uint(0); i < bodyNode.NamedChildCount(); i++ {
		child := bodyNode.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "method_definition":
			name := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			kind := "method"
			if name == "constructor" {
				kind = "constructor"
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       kind,
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: jstsVisibility(child, source),
			})
		case "field_definition", "public_field_definition":
			name := ""
			if nameNode := child.ChildByFieldName("property"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			if name == "" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					name = string(source[nameNode.StartByte():nameNode.EndByte()])
				}
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "field",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: jstsVisibility(child, source),
			})
		}
	}
	return entries
}

func jstsVisibility(node *sitter.Node, source []byte) string {
	decl := declarationLine(node, source)
	if strings.HasPrefix(decl, "private ") || strings.HasPrefix(decl, "#") {
		return "private"
	}
	if strings.HasPrefix(decl, "protected ") {
		return "protected"
	}
	if strings.HasPrefix(decl, "public ") {
		return "public"
	}
	return "public" // JS/TS default is public
}

// --- Rust ---

func extractRustMembers(root, classNode *sitter.Node, source []byte, symbolName string) []OutlineEntry {
	var entries []OutlineEntry

	// For structs: extract fields.
	bodyNode := findChildByKind(classNode, "field_declaration_list")
	if bodyNode != nil {
		for i := uint(0); i < bodyNode.NamedChildCount(); i++ {
			child := bodyNode.NamedChild(i)
			if child == nil || child.Kind() != "field_declaration" {
				continue
			}
			name := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			content := string(source[child.StartByte():child.EndByte()])
			vis := "private"
			if strings.HasPrefix(strings.TrimSpace(content), "pub") {
				vis = "public"
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "field",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: vis,
			})
		}
	}

	// Find impl blocks for this type and extract methods.
	for i := uint(0); i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		if child == nil || child.Kind() != "impl_item" {
			continue
		}
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		implTypeName := string(source[typeNode.StartByte():typeNode.EndByte()])
		if implTypeName != symbolName {
			continue
		}
		implBody := findChildByKind(child, "declaration_list")
		if implBody == nil {
			continue
		}
		for j := uint(0); j < implBody.NamedChildCount(); j++ {
			fn := implBody.NamedChild(j)
			if fn == nil || fn.Kind() != "function_item" {
				continue
			}
			name := ""
			if nameNode := fn.ChildByFieldName("name"); nameNode != nil {
				name = string(source[nameNode.StartByte():nameNode.EndByte()])
			}
			kind := "method"
			if name == "new" {
				kind = "constructor"
			}
			content := string(source[fn.StartByte():fn.EndByte()])
			vis := "private"
			if strings.HasPrefix(strings.TrimSpace(content), "pub") {
				vis = "public"
			}
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       kind,
				Line:       int(fn.StartPosition().Row) + 1,
				EndLine:    int(fn.EndPosition().Row) + 1,
				Signature:  declarationLine(fn, source),
				Visibility: vis,
			})
		}
	}

	return entries
}

// --- C++ ---

func extractCPPMembers(classNode *sitter.Node, source []byte) []OutlineEntry {
	var entries []OutlineEntry
	bodyNode := findChildByKind(classNode, "field_declaration_list")
	if bodyNode == nil {
		return nil
	}

	currentVisibility := "private" // C++ default
	for i := uint(0); i < bodyNode.NamedChildCount(); i++ {
		child := bodyNode.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "access_specifier":
			content := strings.TrimSpace(string(source[child.StartByte():child.EndByte()]))
			content = strings.TrimSuffix(content, ":")
			currentVisibility = strings.TrimSpace(content)
		case "function_definition":
			name := extractCPPFunctionName(child, source)
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "method",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  declarationLine(child, source),
				Visibility: currentVisibility,
			})
		case "declaration", "field_declaration":
			content := declarationLine(child, source)
			name := extractCPPDeclName(child, source)
			entries = append(entries, OutlineEntry{
				Name:       name,
				Kind:       "field",
				Line:       int(child.StartPosition().Row) + 1,
				EndLine:    int(child.EndPosition().Row) + 1,
				Signature:  content,
				Visibility: currentVisibility,
			})
		}
	}
	return entries
}

func extractCPPFunctionName(node *sitter.Node, source []byte) string {
	declNode := node.ChildByFieldName("declarator")
	if declNode == nil {
		return ""
	}
	// function_declarator → declarator → identifier
	if inner := declNode.ChildByFieldName("declarator"); inner != nil {
		return string(source[inner.StartByte():inner.EndByte()])
	}
	return string(source[declNode.StartByte():declNode.EndByte()])
}

func extractCPPDeclName(node *sitter.Node, source []byte) string {
	if declNode := node.ChildByFieldName("declarator"); declNode != nil {
		return string(source[declNode.StartByte():declNode.EndByte()])
	}
	return ""
}

// --- Method ranking and expansion ---

// selectAndExpandTopMethods ranks methods from the outline and returns their
// full bodies as CodeSlice items.
func selectAndExpandTopMethods(outline []OutlineEntry, source []byte, query string, budgetLines int) []CodeSlice {
	methods := make([]OutlineEntry, 0)
	for _, e := range outline {
		if e.Kind == "method" || e.Kind == "constructor" {
			methods = append(methods, e)
		}
	}
	if len(methods) == 0 {
		return nil
	}

	ranked := rankMethods(methods, query)

	lines := strings.Split(string(source), "\n")
	var slices []CodeSlice
	usedLines := 0

	for i, entry := range ranked {
		if i >= maxTopMethods {
			break
		}
		methodLines := entry.EndLine - entry.Line + 1
		if usedLines+methodLines > budgetLines {
			break
		}

		code := joinLines(lines, entry.Line-1, entry.EndLine-1)
		reason := rankReason(entry)
		slices = append(slices, CodeSlice{
			Label:     "Method: " + entry.Name,
			StartLine: entry.Line,
			EndLine:   entry.EndLine,
			Reason:    reason,
			Code:      code,
		})
		usedLines += methodLines
	}
	return slices
}

// rankMethods scores and sorts method entries for expansion priority.
func rankMethods(entries []OutlineEntry, query string) []OutlineEntry {
	type scored struct {
		entry OutlineEntry
		score int
	}

	queryWords := strings.Fields(strings.ToLower(query))
	firstQuartile := 0
	if len(entries) > 0 {
		firstQuartile = entries[0].Line + (entries[len(entries)-1].Line-entries[0].Line)/4
	}

	items := make([]scored, len(entries))
	for i, e := range entries {
		s := 0

		// Constructor bonus.
		if e.Kind == "constructor" || isConstructorName(e.Name) {
			s += 5
		}

		// Public visibility bonus.
		if e.Visibility == "public" {
			s += 3
		}

		// Medium body size bonus.
		bodySize := e.EndLine - e.Line + 1
		if bodySize >= 10 && bodySize <= 40 {
			s += 2
		}

		// Query term match.
		nameLower := strings.ToLower(e.Name)
		for _, w := range queryWords {
			if strings.Contains(nameLower, w) {
				s += 2
				break
			}
		}

		// Early position bonus.
		if e.Line <= firstQuartile {
			s += 1
		}

		// Single-liner penalty.
		if e.EndLine == e.Line {
			s -= 1
		}

		items[i] = scored{entry: e, score: s}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	result := make([]OutlineEntry, len(items))
	for i, item := range items {
		result[i] = item.entry
	}
	return result
}

func isConstructorName(name string) bool {
	lower := strings.ToLower(name)
	return lower == "__init__" || lower == "constructor" || lower == "new" ||
		lower == "init" || strings.HasPrefix(name, "New")
}

func rankReason(entry OutlineEntry) string {
	parts := make([]string, 0, 3)
	if entry.Kind == "constructor" || isConstructorName(entry.Name) {
		parts = append(parts, "constructor")
	}
	if entry.Visibility == "public" {
		parts = append(parts, "public")
	}
	if len(parts) == 0 {
		parts = append(parts, "relevant")
	}
	return strings.Join(parts, ", ")
}

// --- Helpers ---

func findChildByKind(node *sitter.Node, kind string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

// isAnnotationLine returns true if the line is an annotation, decorator, or attribute
// that should be skipped when extracting signatures.
func isAnnotationLine(line string) bool {
	return strings.HasPrefix(line, "@") || // Java/Python/TS annotations/decorators
		strings.HasPrefix(line, "#[") // Rust attributes
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// declarationLine returns the first line of a node's content that is not an
// annotation or decorator. For Java (@Override, @Inject) and Python (@property)
// the actual declaration is on a subsequent line.
func declarationLine(node *sitter.Node, source []byte) string {
	content := string(source[node.StartByte():node.EndByte()])
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip annotations, decorators, and Rust attributes.
		if isAnnotationLine(trimmed) {
			continue
		}
		return trimmed
	}
	// Fallback: return the first non-empty line.
	return strings.TrimSpace(firstLine(content))
}

// signatureLine extracts the declaration line from source starting at startLine,
// skipping annotation/decorator lines that precede the actual declaration.
func signatureLine(source []byte, startLine int) string {
	lines := strings.Split(string(source), "\n")
	for i := startLine - 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if isAnnotationLine(trimmed) {
			continue
		}
		return trimmed
	}
	if startLine-1 < len(lines) {
		return strings.TrimSpace(lines[startLine-1])
	}
	return ""
}
