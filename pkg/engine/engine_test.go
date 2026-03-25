package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Mock providers for testing.

type mockExtractor struct {
	def *SymDefinition
	err error
}

func (m *mockExtractor) Extract(workspaceRoot, symbol string) (*SymDefinition, error) {
	return m.def, m.err
}

type mockRefsProvider struct {
	refs   []SymReference
	source string
	err    error
}

func (m *mockRefsProvider) FindRefs(ctx context.Context, workspaceRoot, filterPath, symbol, kind string) ([]SymReference, string, error) {
	return m.refs, m.source, m.err
}

type mockCallersProvider struct {
	callers []SymCaller
	err     error
}

func (m *mockCallersProvider) FindCallers(ctx context.Context, workspaceRoot, filterPath, symbol string, depth int) ([]SymCaller, error) {
	return m.callers, m.err
}

type mockImplProvider struct {
	impls []SymImpl
	err   error
}

func (m *mockImplProvider) FindImplementations(ctx context.Context, workspaceRoot, filterPath, symbol string) ([]SymImpl, error) {
	return m.impls, m.err
}

type mockSearchProvider struct {
	refs   []SymReference
	err    error
	called bool
}

func (m *mockSearchProvider) Search(ctx context.Context, workspaceRoot, query string, limit int) ([]SymReference, error) {
	m.called = true
	return m.refs, m.err
}

func TestQueryTextFallsBackToSemanticSearch(t *testing.T) {
	search := &mockSearchProvider{
		refs: []SymReference{
			{File: "pkg/auth.go", Line: 10, Snippet: "func Authenticate()", Score: 0.87, Reason: "semantic (score 0.87)"},
		},
	}
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Refs: &mockRefsProvider{
			refs: nil, source: "grep",
		},
		Search: search,
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "How does authentication work?",
		Mode:  "text",
		Depth: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if result.Meta.RefsSource != "semantic" {
		t.Errorf("RefsSource = %q, want semantic", result.Meta.RefsSource)
	}
	if !search.called {
		t.Error("semantic search should have been called")
	}
	chain := strings.Join(result.Meta.SearchChain, " ")
	if !strings.Contains(chain, "semantic") {
		t.Errorf("SearchChain = %v, expected semantic", result.Meta.SearchChain)
	}
	if result.Meta.Confidence != 0.40 {
		t.Errorf("Confidence = %v, want 0.40 for semantic fallback", result.Meta.Confidence)
	}
}

func TestQueryTextSkipsSemanticWhenGrepSucceeds(t *testing.T) {
	search := &mockSearchProvider{
		refs: []SymReference{
			{File: "should/not/appear.go", Line: 1},
		},
	}
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Refs: &mockRefsProvider{
			refs:   []SymReference{{File: "pkg/foo.go", Line: 5, Snippet: "match"}},
			source: "grep",
		},
		Search: search,
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "match",
		Mode:  "text",
		Depth: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if search.called {
		t.Error("semantic search should NOT be called when grep has results")
	}
	if result.Meta.RefsSource != "grep" {
		t.Errorf("RefsSource = %q, want grep", result.Meta.RefsSource)
	}
	if result.Meta.Confidence != 0.50 {
		t.Errorf("Confidence = %v, want 0.50 for grep text search", result.Meta.Confidence)
	}
}

func TestQueryTextGrepResultsAreScored(t *testing.T) {
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Refs: &mockRefsProvider{
			refs: []SymReference{
				{File: "pkg/foo.go", Line: 5, Snippet: "deleteSupplier(ctx)"},
				{File: "pkg/bar.go", Line: 20, Snippet: "deleteSupplier(cfg)"},
				{File: "pkg/baz.go", Line: 10, Snippet: "deleteSupplier(req)"},
			},
			source: "grep",
		},
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "deleteSupplier",
		Mode:  "text",
		Depth: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.Confidence != 0.50 {
		t.Errorf("Confidence = %v, want 0.50", result.Meta.Confidence)
	}
	// All refs have "(" in snippet so ScoreReferences should assign direct-call scores and reasons.
	for i, ref := range result.References {
		if ref.Score == 0 {
			t.Errorf("References[%d].Score = 0, want non-zero", i)
		}
		if ref.Reason == "" {
			t.Errorf("References[%d].Reason is empty, want scoring explanation", i)
		}
	}
	// Verify refs are sorted descending by score.
	for i := 1; i < len(result.References); i++ {
		if result.References[i].Score > result.References[i-1].Score {
			t.Errorf("References not sorted by score: [%d].Score=%v > [%d].Score=%v",
				i, result.References[i].Score, i-1, result.References[i-1].Score)
		}
	}
}

func TestQuerySymbolConfidence(t *testing.T) {
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Extractor: &mockExtractor{
			def: &SymDefinition{
				File: "pkg/auth.go", Line: 10, EndLine: 20,
				Type: "function", Language: "go", Body: "func F() {}",
			},
		},
		Refs:    &mockRefsProvider{source: "lsp"},
		Callers: &mockCallersProvider{},
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "F",
		Depth: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85 for symbol search", result.Meta.Confidence)
	}
}

func TestQueryTextSemanticSearchErrorIsGraceful(t *testing.T) {
	search := &mockSearchProvider{
		err: errors.New("no index found"),
	}
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Refs:          &mockRefsProvider{refs: nil, source: "grep"},
		Search:        search,
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "missing query",
		Mode:  "text",
		Depth: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, e := range result.Meta.Errors {
		if strings.Contains(e, "semantic search") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected semantic search error in Meta.Errors, got %v", result.Meta.Errors)
	}
}

func TestQueryTextNilSearchProvider(t *testing.T) {
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Refs:          &mockRefsProvider{refs: nil, source: "grep"},
		Search:        nil,
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "something",
		Mode:  "text",
		Depth: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not panic and should not have semantic in chain.
	chain := strings.Join(result.Meta.SearchChain, " ")
	if strings.Contains(chain, "semantic") {
		t.Errorf("SearchChain should not contain semantic when provider is nil: %v", result.Meta.SearchChain)
	}
}

func TestQuerySymbolSuccess(t *testing.T) {
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Extractor: &mockExtractor{
			def: &SymDefinition{
				File:     "pkg/auth.go",
				Line:     10,
				EndLine:  20,
				Type:     "function",
				Language: "go",
				Body:     "func Authenticate() {}",
			},
		},
		Refs: &mockRefsProvider{
			refs: []SymReference{
				{File: "cmd/main.go", Line: 5, Snippet: "Authenticate()"},
			},
			source: "lsp",
		},
		Callers: &mockCallersProvider{
			callers: []SymCaller{
				{Symbol: "HandleLogin", File: "cmd/main.go", Line: 5, Depth: 1},
			},
		},
		Implements: &mockImplProvider{},
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "Authenticate",
		Depth: 1,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if result.Definition == nil {
		t.Fatal("Definition is nil")
	}
	if len(result.References) == 0 {
		t.Error("expected references")
	}
	if len(result.Callers) == 0 {
		t.Error("expected callers")
	}
}

func TestQuerySymbolDegradation(t *testing.T) {
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Extractor: &mockExtractor{
			err: errors.New("symbol not found"),
		},
		Refs: &mockRefsProvider{
			refs: []SymReference{
				{File: "pkg/foo.go", Line: 10, Snippet: "FooBar text match"},
			},
			source: "grep",
		},
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "FooBar",
		Depth: 1,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should degrade to text search.
	if !strings.Contains(strings.Join(result.Meta.SearchChain, " "), "text") {
		t.Errorf("SearchChain = %v, expected text fallback", result.Meta.SearchChain)
	}
}

func TestQueryPartialResults(t *testing.T) {
	eng := &Engine{
		WorkspaceRoot: "/workspace",
		Extractor: &mockExtractor{
			def: &SymDefinition{
				File:     "pkg/auth.go",
				Line:     1,
				EndLine:  5,
				Type:     "function",
				Language: "go",
				Body:     "func F() {}",
			},
		},
		Refs: &mockRefsProvider{
			refs: []SymReference{
				{File: "cmd/main.go", Line: 1, Snippet: "F()"},
			},
			source: "lsp",
		},
		Callers: &mockCallersProvider{
			err: errors.New("LSP timeout"),
		},
	}

	result, err := eng.Query(context.Background(), QueryOptions{
		Query: "F",
		Depth: 1,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	// Should have refs but note callers error.
	if len(result.References) == 0 {
		t.Error("expected references despite callers failure")
	}
	if len(result.Meta.Errors) == 0 {
		t.Error("expected error recorded for callers")
	}
}

func TestRenderMarkdownSymbol(t *testing.T) {
	result := &SymbolIntelligence{
		Query:  "Authenticate",
		Symbol: "Authenticate",
		Status: "ok",
		Mode:   "symbol",
		Definition: &SymDefinition{
			File:         "pkg/auth.go",
			Line:         42,
			EndLine:      103,
			Type:         "function",
			Signature:    "func Authenticate(ctx context.Context, token string) (*User, error)",
			ViewStrategy: "full_body",
			Body:         "func Authenticate(ctx context.Context, token string) (*User, error) {\n\treturn nil, nil\n}",
			Language:     "go",
		},
		References: []SymReference{
			{File: "cmd/server/main.go", Line: 88, Score: 12, Reason: "direct call"},
		},
		Callers: []SymCaller{
			{Symbol: "HandleLogin", File: "cmd/server/main.go", Line: 85, Score: 8, Reason: "top-level entrypoint"},
		},
		Meta: SymMeta{
			Mode:        "symbol",
			SearchChain: []string{"symbol"},
			Confidence:  0.87,
			Budget:      ComputeBudget("auto", 1),
			RefsSource:  "lsp",
			RefsShown:   1,
			RefsTotal:   15,
		},
	}

	md := RenderMarkdown(result)

	checks := []string{
		"# Symbol: `Authenticate`",
		"## Summary",
		"## Definition",
		"## References",
		"## Callers",
		"## Meta",
		"`search_chain`: `symbol`",
	}
	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Errorf("missing %q in rendered markdown", check)
		}
	}
}

func TestRenderMarkdownNotFound(t *testing.T) {
	result := &SymbolIntelligence{
		Query:  "Foo",
		Status: "not_found_exact",
		Ambiguous: []SymCandidate{
			{Name: "FooAuth", Type: "function", File: "pkg/auth/foo.go", Reason: "name similarity"},
		},
		Meta: SymMeta{
			SearchChain: []string{"symbol", "text"},
			NextHint:    "retry with --path",
		},
	}

	md := RenderMarkdown(result)

	if !strings.Contains(md, "# No Exact Symbol: `Foo`") {
		t.Error("missing not-found header")
	}
	if !strings.Contains(md, "FooAuth") {
		t.Error("missing candidate")
	}
}

func TestRenderMarkdownSlices(t *testing.T) {
	result := &SymbolIntelligence{
		Query:  "LongFunction",
		Symbol: "LongFunction",
		Status: "ok",
		Mode:   "symbol",
		Definition: &SymDefinition{
			File:         "pkg/main.go",
			Line:         1,
			EndLine:      100,
			Type:         "function",
			Signature:    "func LongFunction() {",
			ViewStrategy: "signature_plus_slices",
			Language:     "go",
			Slices: []CodeSlice{
				{Label: "Header slice", StartLine: 1, EndLine: 5, Code: "func LongFunction() {\n\t// ..."},
				{Label: "I/O site slice", StartLine: 50, EndLine: 55, Code: "\tdata, err := os.ReadFile(path)"},
			},
		},
		Meta: SymMeta{
			Mode:        "symbol",
			SearchChain: []string{"symbol"},
			Budget:      ComputeBudget("small", 1),
		},
	}

	md := RenderMarkdown(result)

	if !strings.Contains(md, "### Header slice") {
		t.Error("missing Header slice")
	}
	if !strings.Contains(md, "### I/O site slice") {
		t.Error("missing I/O site slice")
	}
}
