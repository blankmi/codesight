package splitter

import (
	"strings"
	"testing"
)

func TestFallbackSplitter_BasicChunking(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line content"
	}
	code := strings.Join(lines, "\n")

	s := NewFallbackSplitter(20, 5)
	chunks, err := s.Split(code, "text", "test.txt")
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	for _, chunk := range chunks {
		if chunk.FilePath != "test.txt" {
			t.Errorf("chunk.FilePath = %q, want %q", chunk.FilePath, "test.txt")
		}
		if chunk.Language != "text" {
			t.Errorf("chunk.Language = %q, want %q", chunk.Language, "text")
		}
		if chunk.NodeType != "block" {
			t.Errorf("chunk.NodeType = %q, want %q", chunk.NodeType, "block")
		}
		if chunk.StartLine < 1 {
			t.Errorf("chunk.StartLine = %d, want >= 1", chunk.StartLine)
		}
	}
}

func TestFallbackSplitter_EmptyCode(t *testing.T) {
	s := NewFallbackSplitter(20, 5)
	chunks, err := s.Split("", "text", "empty.txt")
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for empty input, got %d", len(chunks))
	}
}

func TestFallbackSplitter_SmallFile(t *testing.T) {
	code := "func main() {\n\tprintln(\"hello\")\n}"

	s := NewFallbackSplitter(80, 10)
	chunks, err := s.Split(code, "go", "main.go")
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for small file, got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("chunk.StartLine = %d, want 1", chunks[0].StartLine)
	}
}

func TestFallbackSplitter_InvalidParams(t *testing.T) {
	s := NewFallbackSplitter(-1, -1)
	if s.chunkSize != 80 {
		t.Errorf("chunkSize = %d, want 80 (default)", s.chunkSize)
	}
	if s.overlap != 0 {
		t.Errorf("overlap = %d, want 0", s.overlap)
	}
}

func TestFallbackSplitter_OverlapEqualToChunkSize(t *testing.T) {
	s := NewFallbackSplitter(10, 10)
	if s.overlap >= s.chunkSize {
		t.Errorf("overlap %d should be < chunkSize %d", s.overlap, s.chunkSize)
	}
}
