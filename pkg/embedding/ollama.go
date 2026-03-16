package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// OllamaClient calls the Ollama /api/embed endpoint for embeddings.
type OllamaClient struct {
	host      string
	model     string
	keepAlive string
	client    *http.Client

	mu            sync.Mutex
	dimension     int
	maxInputChars int
}

// NewOllamaClient creates a new Ollama embedding client.
func NewOllamaClient(host, model, keepAlive string) *OllamaClient {
	if keepAlive == "" {
		keepAlive = "5m"
	}
	return &OllamaClient{
		host:      host,
		model:     model,
		keepAlive: keepAlive,
		client:    &http.Client{},
	}
}

type ollamaEmbedRequest struct {
	Model     string   `json:"model"`
	Input     []string `json:"input"`
	KeepAlive string   `json:"keep_alive,omitempty"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (o *OllamaClient) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := o.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("ollama returned empty embeddings")
	}
	return results[0], nil
}

const (
	// defaultMaxInputChars is the fallback character limit when context length
	// detection is not available.
	defaultMaxInputChars = 16000
	// minAdaptiveMaxInputChars is the smallest limit adaptive retries will use.
	minAdaptiveMaxInputChars = 512
	// maxAdaptiveEmbedAttempts bounds retries after context overflow responses.
	maxAdaptiveEmbedAttempts = 5
)

// MaxInputChars returns the detected character limit or the default (16000).
func (o *OllamaClient) MaxInputChars() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.maxInputChars > 0 {
		return o.maxInputChars
	}
	return defaultMaxInputChars
}

// SetMaxInputChars updates the maximum character limit used for embedding
// requests. Non-positive values are ignored.
func (o *OllamaClient) SetMaxInputChars(n int) {
	if n <= 0 {
		return
	}
	o.mu.Lock()
	o.maxInputChars = n
	o.mu.Unlock()
}

// DetectContextLength queries Ollama's /api/show endpoint to discover the
// model's actual context length, then derives a safe character limit from it.
// The character limit uses a conservative 1 char/token ratio.
func (o *OllamaClient) DetectContextLength(ctx context.Context) (int, error) {
	reqBody, err := json.Marshal(map[string]string{"name": o.model})
	if err != nil {
		return 0, fmt.Errorf("marshal show request: %w", err)
	}

	url := fmt.Sprintf("%s/api/show", o.host)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return 0, fmt.Errorf("create show request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("show request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("show returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode show response: %w", err)
	}

	// Look for any key ending in ".context_length" in model_info.
	for key, val := range result.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			switch v := val.(type) {
			case float64:
				contextTokens := int(v)
				charLimit := contextTokens
				o.SetMaxInputChars(charLimit)
				return contextTokens, nil
			}
		}
	}

	return 0, fmt.Errorf("no context_length found in model_info")
}

func (o *OllamaClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	maxChars := o.MaxInputChars()
	var lastErr error

	for attempt := 1; attempt <= maxAdaptiveEmbedAttempts; attempt++ {
		// Split oversized texts into sub-chunks at line boundaries and track
		// which sub-chunks map back to which original text index.
		var flatTexts []string
		type span struct{ start, count int }
		mapping := make([]span, len(texts))

		for i, t := range texts {
			if len(t) <= maxChars {
				mapping[i] = span{start: len(flatTexts), count: 1}
				flatTexts = append(flatTexts, t)
			} else {
				parts := splitTextAtLines(t, maxChars)
				mapping[i] = span{start: len(flatTexts), count: len(parts)}
				flatTexts = append(flatTexts, parts...)
			}
		}

		// Embed all flat texts in sub-batches to avoid huge single requests.
		allVecs, err := o.embedFlat(ctx, flatTexts, maxChars)
		if err != nil {
			if !isContextOverflowError(err) {
				return nil, err
			}
			lastErr = err

			nextMaxChars := maxChars / 2
			if nextMaxChars < minAdaptiveMaxInputChars {
				nextMaxChars = minAdaptiveMaxInputChars
			}
			if nextMaxChars >= maxChars {
				return nil, fmt.Errorf("ollama context overflow after %d attempts; final max input chars=%d: %w", attempt, maxChars, lastErr)
			}

			maxChars = nextMaxChars
			o.SetMaxInputChars(maxChars)
			continue
		}

		// Reassemble: average vectors for texts that were split.
		results := make([][]float32, len(texts))
		for i, m := range mapping {
			if m.count == 1 {
				results[i] = allVecs[m.start]
			} else {
				results[i] = averageVectors(allVecs[m.start : m.start+m.count])
			}
		}
		return results, nil
	}

	return nil, fmt.Errorf("ollama context overflow after %d attempts; final max input chars=%d: %w", maxAdaptiveEmbedAttempts, maxChars, lastErr)
}

// embedFlat sends texts to Ollama in sub-batches and returns one vector per text.
// Sub-batches are sized by total character count to stay within model context limits.
func (o *OllamaClient) embedFlat(ctx context.Context, texts []string, maxBatchChars int) ([][]float32, error) {
	if maxBatchChars <= 0 {
		maxBatchChars = o.MaxInputChars()
	}

	var all [][]float32
	batchStart := 0
	batchChars := 0

	for i, t := range texts {
		if batchChars+len(t) > maxBatchChars && i > batchStart {
			vecs, err := o.doEmbed(ctx, texts[batchStart:i])
			if err != nil {
				return nil, err
			}
			all = append(all, vecs...)
			batchStart = i
			batchChars = 0
		}
		batchChars += len(t)
	}

	// Flush remaining.
	if batchStart < len(texts) {
		vecs, err := o.doEmbed(ctx, texts[batchStart:])
		if err != nil {
			return nil, err
		}
		all = append(all, vecs...)
	}

	return all, nil
}

// doEmbed makes a single HTTP call to Ollama's /api/embed endpoint.
func (o *OllamaClient) doEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:     o.model,
		Input:     texts,
		KeepAlive: o.keepAlive,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/embed", o.host)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		responseText := string(respBody)
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(responseText), "input length exceeds the context length") {
			return nil, &contextOverflowError{statusCode: resp.StatusCode, body: responseText}
		}
		return nil, fmt.Errorf("ollama embed returned %d: %s", resp.StatusCode, responseText)
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama decode response: %w", err)
	}

	if len(result.Embeddings) > 0 && len(result.Embeddings[0]) > 0 {
		o.mu.Lock()
		if o.dimension == 0 {
			o.dimension = len(result.Embeddings[0])
		}
		o.mu.Unlock()
	}

	return result.Embeddings, nil
}

type contextOverflowError struct {
	statusCode int
	body       string
}

func (e *contextOverflowError) Error() string {
	return fmt.Sprintf("ollama embed returned %d: %s", e.statusCode, e.body)
}

func isContextOverflowError(err error) bool {
	var contextErr *contextOverflowError
	return errors.As(err, &contextErr)
}

// splitTextAtLines splits text into pieces of at most maxChars, breaking at
// newline boundaries when possible. If no newline exists in the current window,
// it hard-wraps at maxChars. Content is preserved without truncation.
func splitTextAtLines(text string, maxChars int) []string {
	if text == "" {
		return []string{""}
	}
	if maxChars <= 0 || len(text) <= maxChars {
		return []string{text}
	}

	var parts []string
	for start := 0; start < len(text); {
		if len(text)-start <= maxChars {
			parts = append(parts, text[start:])
			break
		}

		windowEnd := start + maxChars
		if nl := strings.LastIndexByte(text[start:windowEnd], '\n'); nl >= 0 {
			splitAt := start + nl + 1 // include newline to preserve exact content.
			parts = append(parts, text[start:splitAt])
			start = splitAt
			continue
		}

		parts = append(parts, text[start:windowEnd])
		start = windowEnd
	}

	return parts
}

// averageVectors computes the element-wise mean of multiple vectors.
func averageVectors(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	avg := make([]float32, dim)
	for _, v := range vecs {
		for j, val := range v {
			avg[j] += val
		}
	}
	scale := 1.0 / float32(len(vecs))
	for j := range avg {
		avg[j] *= scale
	}
	return avg
}

func (o *OllamaClient) Dimension() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dimension
}

func (o *OllamaClient) Name() string {
	return fmt.Sprintf("ollama/%s", o.model)
}

// DetectDimension makes a probe embedding call to determine the vector dimension.
func (o *OllamaClient) DetectDimension(ctx context.Context) (int, error) {
	vec, err := o.Embed(ctx, "dimension probe")
	if err != nil {
		return 0, fmt.Errorf("detecting embedding dimension: %w", err)
	}
	return len(vec), nil
}
