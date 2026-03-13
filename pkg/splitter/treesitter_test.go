package splitter

import "testing"

func TestTreeSitterSplitter_GoCode(t *testing.T) {
	code := `package main

import "fmt"

func Hello() string {
	return "hello"
}

func Goodbye() string {
	return "goodbye"
}

type Greeter struct {
	Name string
}

func (g *Greeter) Greet() string {
	return fmt.Sprintf("Hello, %s", g.Name)
}
`
	s := NewTreeSitterSplitter()
	chunks, err := s.Split(code, "go", "main.go")
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks from Go code, got none")
	}

	// Should find functions and type declarations
	nodeTypes := map[string]bool{}
	for _, chunk := range chunks {
		nodeTypes[chunk.NodeType] = true
		if chunk.FilePath != "main.go" {
			t.Errorf("chunk.FilePath = %q, want %q", chunk.FilePath, "main.go")
		}
		if chunk.Language != "go" {
			t.Errorf("chunk.Language = %q, want %q", chunk.Language, "go")
		}
	}

	if !nodeTypes["function"] {
		t.Error("expected at least one function chunk")
	}
}

func TestTreeSitterSplitter_PythonCode(t *testing.T) {
	code := `class Calculator:
    def __init__(self):
        self.result = 0

    def add(self, x):
        self.result += x
        return self

def standalone():
    pass
`
	s := NewTreeSitterSplitter()
	chunks, err := s.Split(code, "python", "calc.py")
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks from Python code, got none")
	}
}

func TestTreeSitterSplitter_UnsupportedLanguageFallback(t *testing.T) {
	code := "some random content\nline two\nline three"
	s := NewTreeSitterSplitter()
	chunks, err := s.Split(code, "brainfuck", "test.bf")
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected fallback chunks for unsupported language, got none")
	}
}

func TestTreeSitterSplitter_SupportedLanguages(t *testing.T) {
	s := NewTreeSitterSplitter()
	langs := s.SupportedLanguages()
	if len(langs) == 0 {
		t.Fatal("expected supported languages, got none")
	}

	want := map[string]bool{"go": true, "typescript": true, "python": true, "java": true, "rust": true}
	for lang := range want {
		found := false
		for _, l := range langs {
			if l == lang {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in supported languages", lang)
		}
	}
}

func TestLanguageFromExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".ts", "typescript"},
		{".tsx", "tsx"},
		{".py", "python"},
		{".java", "java"},
		{".rs", "rust"},
		{".c", "c"},
		{".cpp", "cpp"},
		{".unknown", ""},
	}

	for _, tt := range tests {
		got := LanguageFromExtension(tt.ext)
		if got != tt.want {
			t.Errorf("LanguageFromExtension(%q) = %q, want %q", tt.ext, got, tt.want)
		}
	}
}

func TestDescribeChunk(t *testing.T) {
	chunk := Chunk{
		Content:  "func Hello() string {\n\treturn \"hello\"\n}",
		NodeType: "function",
	}
	desc := DescribeChunk(chunk)
	if desc == "" {
		t.Error("expected non-empty description")
	}
}
