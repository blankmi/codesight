package extract

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_html "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

var supportedLanguagesInOrder = []string{
	"go",
	"python",
	"java",
	"javascript",
	"typescript",
	"rust",
	"cpp",
	"xml",
	"html",
}

var extensionToLanguage = map[string]string{
	".go":   "go",
	".py":   "python",
	".java": "java",
	".js":   "javascript",
	".jsx":  "javascript",
	".ts":   "typescript",
	".tsx":  "typescript",
	".rs":   "rust",
	".cpp":  "cpp",
	".cc":   "cpp",
	".cxx":  "cpp",
	".h":    "cpp",
	".hpp":  "cpp",
	".hxx":  "cpp",
	".xml":  "xml",
	".html": "html",
	".htm":  "html",
}

func SupportedLanguages() []string {
	langs := make([]string, len(supportedLanguagesInOrder))
	copy(langs, supportedLanguagesInOrder)
	return langs
}

func languageFromExtension(ext string) (string, bool) {
	lang, ok := extensionToLanguage[strings.ToLower(ext)]
	return lang, ok
}

func parserForLanguage(language string) *sitter.Language {
	switch strings.ToLower(language) {
	case "go":
		return sitter.NewLanguage(tree_sitter_go.Language())
	case "python":
		return sitter.NewLanguage(tree_sitter_python.Language())
	case "java":
		return sitter.NewLanguage(tree_sitter_java.Language())
	case "javascript":
		return sitter.NewLanguage(tree_sitter_javascript.Language())
	case "typescript":
		return sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	case "rust":
		return sitter.NewLanguage(tree_sitter_rust.Language())
	case "cpp":
		return sitter.NewLanguage(tree_sitter_cpp.Language())
	case "xml", "html":
		return sitter.NewLanguage(tree_sitter_html.Language())
	default:
		return nil
	}
}
