package splitter

import "strings"

// FallbackSplitter performs simple line-based chunking with overlap.
type FallbackSplitter struct {
	chunkSize int // number of lines per chunk
	overlap   int // number of overlapping lines between chunks
}

// NewFallbackSplitter creates a line-based splitter.
func NewFallbackSplitter(chunkSize, overlap int) *FallbackSplitter {
	if chunkSize <= 0 {
		chunkSize = 80
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 4
	}
	return &FallbackSplitter{chunkSize: chunkSize, overlap: overlap}
}

func (f *FallbackSplitter) SupportedLanguages() []string {
	return nil // supports all languages as a fallback
}

func (f *FallbackSplitter) Split(code string, language string, filePath string) ([]Chunk, error) {
	lines := strings.Split(code, "\n")
	if len(lines) == 0 {
		return nil, nil
	}

	var chunks []Chunk
	step := f.chunkSize - f.overlap
	if step <= 0 {
		step = 1
	}

	for i := 0; i < len(lines); i += step {
		end := i + f.chunkSize
		if end > len(lines) {
			end = len(lines)
		}

		content := strings.Join(lines[i:end], "\n")
		if strings.TrimSpace(content) == "" {
			continue
		}

		chunks = append(chunks, Chunk{
			Content:   content,
			FilePath:  filePath,
			StartLine: i + 1,
			EndLine:   end,
			Language:  language,
			NodeType:  "block",
		})

		if end >= len(lines) {
			break
		}
	}

	return chunks, nil
}
