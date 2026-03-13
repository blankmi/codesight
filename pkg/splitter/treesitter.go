package splitter

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// maxChunkLines is the maximum number of lines a single chunk can span before
// it is split further using the fallback splitter.
const maxChunkLines = 200

// defaultMaxChunkChars is the default maximum character length a single chunk may have.
const defaultMaxChunkChars = 16000

// TreeSitterSplitter uses tree-sitter AST parsing to extract semantic code chunks.
type TreeSitterSplitter struct {
	fallback      *FallbackSplitter
	maxChunkChars int
}

// Option configures a TreeSitterSplitter.
type Option func(*TreeSitterSplitter)

// WithMaxChunkChars sets the maximum character length for a single chunk.
func WithMaxChunkChars(n int) Option {
	return func(s *TreeSitterSplitter) {
		s.maxChunkChars = n
	}
}

// NewTreeSitterSplitter creates a new AST-aware splitter with a line-based fallback.
func NewTreeSitterSplitter(opts ...Option) *TreeSitterSplitter {
	s := &TreeSitterSplitter{
		fallback:      NewFallbackSplitter(80, 10),
		maxChunkChars: defaultMaxChunkChars,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (t *TreeSitterSplitter) SupportedLanguages() []string {
	return []string{"go", "typescript", "javascript", "python", "java", "rust", "c", "cpp"}
}

func (t *TreeSitterSplitter) Split(code string, language string, filePath string) ([]Chunk, error) {
	lang := getLanguage(language)
	if lang == nil {
		return t.fallback.Split(code, language, filePath)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, []byte(code))
	if err != nil {
		return t.fallback.Split(code, language, filePath)
	}
	defer tree.Close()

	lines := strings.Split(code, "\n")
	nodeTypes := nodeTypesForLanguage(language)

	var chunks []Chunk
	t.walkNode(tree.RootNode(), code, lines, filePath, language, nodeTypes, &chunks)

	if len(chunks) == 0 {
		return t.fallback.Split(code, language, filePath)
	}

	// Split oversized chunks (by line count or character length).
	var result []Chunk
	for _, chunk := range chunks {
		lineCount := chunk.EndLine - chunk.StartLine + 1
		if lineCount > maxChunkLines || len(chunk.Content) > t.maxChunkChars {
			sub, _ := t.fallback.Split(chunk.Content, language, filePath)
			for i := range sub {
				sub[i].StartLine += chunk.StartLine - 1
				sub[i].EndLine += chunk.StartLine - 1
				sub[i].NodeType = chunk.NodeType
			}
			result = append(result, sub...)
		} else {
			result = append(result, chunk)
		}
	}

	return result, nil
}

func (t *TreeSitterSplitter) walkNode(node *sitter.Node, code string, lines []string, filePath string, language string, nodeTypes map[string]string, chunks *[]Chunk) {
	typeName := node.Type()

	if label, ok := nodeTypes[typeName]; ok {
		startLine := int(node.StartPoint().Row) + 1
		endLine := int(node.EndPoint().Row) + 1
		content := extractLines(lines, startLine, endLine)

		*chunks = append(*chunks, Chunk{
			Content:   content,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			Language:  language,
			NodeType:  label,
		})
		return // don't recurse into matched nodes
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		t.walkNode(child, code, lines, filePath, language, nodeTypes, chunks)
	}
}

func extractLines(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}

func getLanguage(lang string) *sitter.Language {
	switch strings.ToLower(lang) {
	case "go":
		return golang.GetLanguage()
	case "typescript", "tsx":
		return typescript.GetLanguage()
	case "javascript", "jsx":
		return javascript.GetLanguage()
	case "python":
		return python.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "c", "cpp", "c++", "cc", "cxx":
		return cpp.GetLanguage()
	default:
		return nil
	}
}

func nodeTypesForLanguage(lang string) map[string]string {
	switch strings.ToLower(lang) {
	case "go":
		return map[string]string{
			"function_declaration": "function",
			"method_declaration":   "method",
			"type_declaration":     "type",
			"type_spec":            "type",
		}
	case "typescript", "tsx", "javascript", "jsx":
		return map[string]string{
			"function_declaration":       "function",
			"method_definition":          "method",
			"class_declaration":          "class",
			"interface_declaration":      "interface",
			"arrow_function":             "function",
			"export_statement":           "export",
			"lexical_declaration":        "declaration",
			"type_alias_declaration":     "type",
			"enum_declaration":           "enum",
			"abstract_class_declaration": "class",
		}
	case "python":
		return map[string]string{
			"function_definition":       "function",
			"class_definition":          "class",
			"decorated_definition":      "decorated",
			"async_function_definition": "function",
		}
	case "java":
		return map[string]string{
			"method_declaration":      "method",
			"class_declaration":       "class",
			"interface_declaration":   "interface",
			"enum_declaration":        "enum",
			"constructor_declaration": "constructor",
		}
	case "rust":
		return map[string]string{
			"function_item": "function",
			"impl_item":     "impl",
			"struct_item":   "struct",
			"enum_item":     "enum",
			"trait_item":    "trait",
			"mod_item":      "module",
			"type_item":     "type",
		}
	case "c", "cpp", "c++", "cc", "cxx":
		return map[string]string{
			"function_definition":  "function",
			"class_specifier":      "class",
			"struct_specifier":     "struct",
			"enum_specifier":       "enum",
			"namespace_definition": "namespace",
			"template_declaration": "template",
		}
	default:
		return map[string]string{}
	}
}

// LanguageFromExtension maps a file extension to a language name.
func LanguageFromExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js":
		return "javascript"
	case ".jsx":
		return "jsx"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hxx":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".cs":
		return "csharp"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".sh", ".bash":
		return "shell"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	case ".sql":
		return "sql"
	default:
		return ""
	}
}

// DescribeChunk returns a short one-line description of a chunk suitable for search results.
func DescribeChunk(chunk Chunk) string {
	firstLine := chunk.Content
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)
	if len(firstLine) > 120 {
		firstLine = firstLine[:120] + "..."
	}
	label := chunk.NodeType
	if label == "" {
		label = "block"
	}
	return fmt.Sprintf("%s — %s", label, firstLine)
}
