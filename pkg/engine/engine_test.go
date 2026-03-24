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
