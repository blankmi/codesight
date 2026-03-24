package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type stubRefsClient struct {
	workspaceSymbols    []SymbolInformation
	workspaceSymbolErr  error
	references          []Location
	referencesErr       error
	methods             []string
	lastReferenceParams ReferenceParams
}

func (s *stubRefsClient) Call(ctx context.Context, method string, params any, result any) error {
	s.methods = append(s.methods, method)

	switch method {
	case MethodWorkspaceSymbol:
		if s.workspaceSymbolErr != nil {
			return s.workspaceSymbolErr
		}

		out, ok := result.(*[]SymbolInformation)
		if !ok {
			return fmt.Errorf("workspace/symbol result type %T", result)
		}
		*out = append([]SymbolInformation(nil), s.workspaceSymbols...)
		return nil

	case MethodTextDocumentReferences:
		if s.referencesErr != nil {
			return s.referencesErr
		}

		typedParams, ok := params.(ReferenceParams)
		if !ok {
			return fmt.Errorf("references params type %T", params)
		}
		s.lastReferenceParams = typedParams

		out, ok := result.(*[]Location)
		if !ok {
			return fmt.Errorf("references result type %T", result)
		}
		*out = append([]Location(nil), s.references...)
		return nil

	default:
		return fmt.Errorf("unexpected method %q", method)
	}
}

type stubFallback struct {
	called bool
}

func (s *stubFallback) Find(ctx context.Context, workspaceRoot string, symbol string) ([]referenceLine, error) {
	s.called = true
	return nil, nil
}

func TestRefsLSPHappyPath(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha.go")
	bravo := filepath.Join(root, "bravo.go")

	writeTestFile(t, alpha, strings.Join([]string{
		"package demo",
		"",
		"func target() {}",
		"",
		"func useAlpha() {",
		"\t_ = target()",
		"}",
	}, "\n"))
	writeTestFile(t, bravo, strings.Join([]string{
		"package demo",
		"",
		"func useBravo() {",
		"\t_ = target()",
		"}",
	}, "\n"))

	client := &stubRefsClient{
		workspaceSymbols: []SymbolInformation{
			{
				Name: "target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI: fileURI(alpha),
					Range: Range{
						Start: Position{Line: 2, Character: 5},
						End:   Position{Line: 2, Character: 11},
					},
				},
			},
		},
		references: []Location{
			{
				URI: fileURI(bravo),
				Range: Range{
					Start: Position{Line: 3, Character: 6},
					End:   Position{Line: 3, Character: 12},
				},
			},
			{
				URI: fileURI(alpha),
				Range: Range{
					Start: Position{Line: 5, Character: 6},
					End:   Position{Line: 5, Character: 12},
				},
			},
		},
	}

	engine := NewRefsEngine(client, nil)
	output, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "target",
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"alpha.go:6  ->  _ = target()",
		"bravo.go:4  ->  _ = target()",
		"2 references found",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}

	wantMethods := []string{MethodWorkspaceSymbol, MethodTextDocumentReferences}
	if !slices.Equal(client.methods, wantMethods) {
		t.Fatalf("method order = %v, want %v", client.methods, wantMethods)
	}
	if client.lastReferenceParams.Context.IncludeDeclaration {
		t.Fatal("references lookup should set IncludeDeclaration=false")
	}
}

func TestRefsLSPRespectsCsignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".csignore"), []byte("ignored.go\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.csignore) returned error: %v", err)
	}

	target := filepath.Join(root, "target.go")
	ignored := filepath.Join(root, "ignored.go")
	use := filepath.Join(root, "use.go")

	writeTestFile(t, target, strings.Join([]string{
		"package demo",
		"",
		"func Target() {}",
	}, "\n"))
	writeTestFile(t, ignored, strings.Join([]string{
		"package demo",
		"",
		"func Target() {}",
		"",
		"func ignoredUse() {",
		"\tTarget()",
		"}",
	}, "\n"))
	writeTestFile(t, use, strings.Join([]string{
		"package demo",
		"",
		"func useTarget() {",
		"\tTarget()",
		"}",
	}, "\n"))

	client := &stubRefsClient{
		workspaceSymbols: []SymbolInformation{
			{
				Name: "Target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI:   fileURI(ignored),
					Range: Range{Start: Position{Line: 2, Character: 5}},
				},
			},
			{
				Name: "Target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI:   fileURI(target),
					Range: Range{Start: Position{Line: 2, Character: 5}},
				},
			},
		},
		references: []Location{
			{
				URI:   fileURI(ignored),
				Range: Range{Start: Position{Line: 5, Character: 1}},
			},
			{
				URI:   fileURI(use),
				Range: Range{Start: Position{Line: 3, Character: 1}},
			},
		},
	}

	engine := NewRefsEngine(client, nil)
	output, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"use.go:4  ->  Target()",
		"1 references found",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestRefsAmbiguousSymbolDeterministicOrdering(t *testing.T) {
	root := t.TempDir()

	client := &stubRefsClient{
		workspaceSymbols: []SymbolInformation{
			{
				Name: "Target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI: fileURI(filepath.Join(root, "zeta.go")),
					Range: Range{
						Start: Position{Line: 9},
					},
				},
			},
			{
				Name: "Target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI: fileURI(filepath.Join(root, "alpha.go")),
					Range: Range{
						Start: Position{Line: 1},
					},
				},
			},
		},
	}

	engine := NewRefsEngine(client, nil)
	_, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
	})
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}

	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 3 {
		t.Fatalf("ambiguity output lines = %d, want 3: %q", len(lines), err.Error())
	}

	wantHeader := `ambiguous symbol "Target" — 2 definitions found. Use --path to narrow scope.`
	if lines[0] != wantHeader {
		t.Fatalf("ambiguity header = %q, want %q", lines[0], wantHeader)
	}
	if lines[1] != "  - alpha.go:2 (function)" {
		t.Fatalf("first candidate = %q, want %q", lines[1], "  - alpha.go:2 (function)")
	}
	if lines[2] != "  - zeta.go:10 (function)" {
		t.Fatalf("second candidate = %q, want %q", lines[2], "  - zeta.go:10 (function)")
	}
}

func TestRefsKindFilterIncludeExclude(t *testing.T) {
	root := t.TempDir()
	targetFile := filepath.Join(root, "target.go")
	useFile := filepath.Join(root, "use.go")
	writeTestFile(t, targetFile, "type Target struct{}\n")
	writeTestFile(t, useFile, "func use() {\n\t_ = Target{}\n}\n")

	client := &stubRefsClient{
		workspaceSymbols: []SymbolInformation{
			{
				Name: "Target",
				Kind: SymbolKindClass,
				Location: Location{
					URI:   fileURI(targetFile),
					Range: Range{Start: Position{Line: 0}},
				},
			},
			{
				Name: "Target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI:   fileURI(filepath.Join(root, "target_func.go")),
					Range: Range{Start: Position{Line: 0}},
				},
			},
		},
		references: []Location{
			{
				URI:   fileURI(useFile),
				Range: Range{Start: Position{Line: 1}},
			},
		},
	}

	engine := NewRefsEngine(client, nil)
	includedOutput, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
		Kind:          "FUNCTION",
	})
	if err != nil {
		t.Fatalf("Find with function kind returned error: %v", err)
	}
	wantIncluded := strings.Join([]string{
		"use.go:2  ->  _ = Target{}",
		"1 references found",
	}, "\n")
	if includedOutput != wantIncluded {
		t.Fatalf("included output mismatch\n got: %q\nwant: %q", includedOutput, wantIncluded)
	}

	// When kind filter excludes all LSP candidates, the engine degrades to
	// grep fallback. The temp dir contains "Target" in its files, so grep
	// should find matches and return successfully with a precision note.
	excludedOutput, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
		Kind:          "method",
	})
	if err != nil {
		t.Fatalf("Find with excluded kind returned error (expected grep fallback): %v", err)
	}
	if !strings.Contains(excludedOutput, "Target") {
		t.Fatalf("grep fallback output should contain Target, got: %q", excludedOutput)
	}
	if !strings.Contains(excludedOutput, "grep-based") {
		t.Fatalf("grep fallback output should contain precision note, got: %q", excludedOutput)
	}
}

func TestRefsInvalidKindExactError(t *testing.T) {
	engine := NewRefsEngine(nil, nil)

	_, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: t.TempDir(),
		Symbol:        "Target",
		Kind:          "BAD_KIND",
	})
	if err == nil {
		t.Fatal("expected invalid kind error, got nil")
	}

	want := `invalid kind "BAD_KIND" — allowed: function, method, class, interface, type, constant`
	if err.Error() != want {
		t.Fatalf("invalid kind error = %q, want %q", err.Error(), want)
	}
}

func TestRefsMissingSymbolFallsBackToGrep(t *testing.T) {
	root := t.TempDir()

	client := &stubRefsClient{
		workspaceSymbols: []SymbolInformation{
			{
				Name: "Other",
				Kind: SymbolKindFunction,
				Location: Location{
					URI:   fileURI(filepath.Join(root, "other.go")),
					Range: Range{Start: Position{Line: 0}},
				},
			},
		},
	}
	fallback := &stubFallback{}
	engine := NewRefsEngine(client, fallback)

	_, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "MissingSymbol",
	})
	if err == nil {
		t.Fatal("expected not-found error from grep path, got nil")
	}
	if !fallback.called {
		t.Fatal("fallback should run when LSP responds with no matching symbol")
	}
	if errors.Is(err, errSymbolNotFound) {
		t.Fatal("error should come from grep path, not LSP errSymbolNotFound")
	}
	if !strings.Contains(err.Error(), `"MissingSymbol"`) {
		t.Fatalf("missing symbol value in error: %q", err.Error())
	}
}

func TestRefsSymbolNotFoundFallsBackToGrepWithMatches(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "code.go"), strings.Join([]string{
		"package demo",
		"",
		"// grep-target appears here",
		"func useIt() {}",
	}, "\n"))

	client := &stubRefsClient{
		workspaceSymbols: []SymbolInformation{
			{
				Name: "Unrelated",
				Kind: SymbolKindFunction,
				Location: Location{
					URI:   fileURI(filepath.Join(root, "code.go")),
					Range: Range{Start: Position{Line: 3}},
				},
			},
		},
	}

	// Find (formatted output)
	engine := NewRefsEngine(client, nil)
	output, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "grep-target",
		FallbackLSP:   "gopls",
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if !strings.Contains(output, "grep-target") {
		t.Fatalf("expected grep results containing search term, got: %q", output)
	}
	if !strings.Contains(output, "grep-based") {
		t.Fatalf("expected precision note in output, got: %q", output)
	}

	// FindReferencesRaw (structured output)
	raw, source, err := engine.FindReferencesRaw(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "grep-target",
	})
	if err != nil {
		t.Fatalf("FindReferencesRaw returned error: %v", err)
	}
	if source != "grep" {
		t.Fatalf("source = %q, want grep", source)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty raw results from grep fallback")
	}
}

func TestRefsFallbackToGrepIncludesPrecisionNote(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "zeta.go"), strings.Join([]string{
		"package demo",
		"",
		"func useZeta() {",
		"\t_ = target()",
		"}",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "alpha.go"), strings.Join([]string{
		"package demo",
		"",
		"func useAlpha() {",
		"\t_ = target()",
		"\t_ = target()",
		"}",
	}, "\n"))

	client := &stubRefsClient{
		workspaceSymbolErr: errors.New("lsp unavailable"),
	}
	engine := NewRefsEngine(client, nil)

	output, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "target",
		FallbackLSP:   "gopls",
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"(grep-based - install gopls for precise results)",
		"alpha.go:4  ->  _ = target()",
		"alpha.go:5  ->  _ = target()",
		"zeta.go:4  ->  _ = target()",
		"3 references found",
	}, "\n")
	if output != want {
		t.Fatalf("fallback output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestRefsFallbackRespectsCsignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".csignore"), []byte("ignored.go\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.csignore) returned error: %v", err)
	}

	writeTestFile(t, filepath.Join(root, "ignored.go"), strings.Join([]string{
		"package demo",
		"",
		"func ignoredUse() {",
		"\t_ = target()",
		"}",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "visible.go"), strings.Join([]string{
		"package demo",
		"",
		"func visibleUse() {",
		"\t_ = target()",
		"}",
	}, "\n"))

	client := &stubRefsClient{
		workspaceSymbolErr: errors.New("lsp unavailable"),
	}
	engine := NewRefsEngine(client, nil)

	output, err := engine.Find(context.Background(), RefsOptions{
		WorkspaceRoot: root,
		Symbol:        "target",
		FallbackLSP:   "gopls",
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"(grep-based - install gopls for precise results)",
		"visible.go:4  ->  _ = target()",
		"1 references found",
	}, "\n")
	if output != want {
		t.Fatalf("fallback output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestRefsDeferredCommandIntegration(t *testing.T) {
	t.Skip("blocked by TK-006: refs command wiring pending")
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}

func fileURI(path string) DocumentURI {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return DocumentURI("file://" + slashed)
}
