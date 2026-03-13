package splitter

// Chunk represents a meaningful segment of code extracted from a file.
type Chunk struct {
	Content   string
	FilePath  string
	StartLine int
	EndLine   int
	Language  string
	NodeType  string // "function", "class", "method", "interface", "type", "block"
}

// Splitter extracts semantic chunks from source code.
type Splitter interface {
	Split(code string, language string, filePath string) ([]Chunk, error)
	SupportedLanguages() []string
}
