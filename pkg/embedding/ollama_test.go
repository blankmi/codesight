package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSplitTextAtLines(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		max      int
		wantN    int
		checkAll bool // verify all parts are within limit
		lossy    bool // truncation expected, skip content check
	}{
		{
			name:  "short text no split",
			text:  "line1\nline2\nline3",
			max:   100,
			wantN: 1,
		},
		{
			name:     "splits at line boundaries",
			text:     "aaaa\nbbbb\ncccc\ndddd",
			max:      10,
			wantN:    2,
			checkAll: true,
		},
		{
			name:  "reassembled content matches original",
			text:  "line1\nline2\nline3\nline4\nline5",
			max:   12,
			wantN: 3,
		},
		{
			name:     "single line exceeding max is truncated",
			text:     strings.Repeat("x", 20) + "\nshort",
			max:      10,
			wantN:    2,
			checkAll: true,
			lossy:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := splitTextAtLines(tt.text, tt.max)
			if len(parts) != tt.wantN {
				t.Errorf("got %d parts, want %d; parts: %q", len(parts), tt.wantN, parts)
			}
			if tt.checkAll {
				for i, p := range parts {
					if len(p) > tt.max {
						t.Errorf("part %d has %d chars, exceeds max %d", i, len(p), tt.max)
					}
				}
			}
			// All content should be preserved (unless truncation expected).
			if !tt.lossy {
				rejoined := strings.Join(parts, "\n")
				if rejoined != tt.text {
					t.Errorf("content lost after split:\ngot:  %q\nwant: %q", rejoined, tt.text)
				}
			}
		})
	}
}

func TestAverageVectors(t *testing.T) {
	vecs := [][]float32{
		{1, 2, 3},
		{3, 4, 5},
	}
	avg := averageVectors(vecs)
	want := []float32{2, 3, 4}
	for i := range want {
		if avg[i] != want[i] {
			t.Errorf("avg[%d] = %f, want %f", i, avg[i], want[i])
		}
	}
}

func TestAverageVectorsEmpty(t *testing.T) {
	if avg := averageVectors(nil); avg != nil {
		t.Errorf("expected nil for empty input, got %v", avg)
	}
}

func TestDetectContextLength(t *testing.T) {
	tests := []struct {
		name       string
		modelInfo  map[string]any
		wantTokens int
		wantChars  int
		wantErr    bool
	}{
		{
			name: "nomic architecture prefix",
			modelInfo: map[string]any{
				"nomic.context_length": float64(8192),
			},
			wantTokens: 8192,
			wantChars:  16384,
		},
		{
			name: "llama architecture prefix",
			modelInfo: map[string]any{
				"llama.context_length": float64(2048),
			},
			wantTokens: 2048,
			wantChars:  4096,
		},
		{
			name: "general.parameter_count ignored without context_length",
			modelInfo: map[string]any{
				"general.parameter_count": float64(137000000),
			},
			wantErr: true,
		},
		{
			name:      "empty model_info",
			modelInfo: map[string]any{},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/show" {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				resp := map[string]any{"model_info": tt.modelInfo}
				json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()

			client := NewOllamaClient(srv.URL, "test-model", "")
			tokens, err := client.DetectContextLength(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tokens != tt.wantTokens {
				t.Errorf("got %d tokens, want %d", tokens, tt.wantTokens)
			}
			if got := client.MaxInputChars(); got != tt.wantChars {
				t.Errorf("MaxInputChars() = %d, want %d", got, tt.wantChars)
			}
		})
	}
}

func TestDetectContextLengthHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewOllamaClient(srv.URL, "test-model", "")
	_, err := client.DetectContextLength(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestMaxInputCharsDefault(t *testing.T) {
	client := NewOllamaClient("http://localhost:11434", "test-model", "")
	if got := client.MaxInputChars(); got != defaultMaxInputChars {
		t.Errorf("MaxInputChars() = %d, want default %d", got, defaultMaxInputChars)
	}
}
