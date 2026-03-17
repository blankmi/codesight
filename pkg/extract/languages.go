package extract

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/html"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
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
		return golang.GetLanguage()
	case "python":
		return python.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "javascript":
		return javascript.GetLanguage()
	case "typescript":
		return typescript.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "cpp":
		return cpp.GetLanguage()
	case "xml", "html":
		return html.GetLanguage()
	default:
		return nil
	}
}
