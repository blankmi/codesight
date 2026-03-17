package lsp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type stubImplementsClient struct {
	workspaceSymbols   []SymbolInformation
	workspaceSymbolErr error

	prepareItems []typeHierarchyItem
	prepareErr   error

	subtypesByItem map[string][]typeHierarchyItem
	subtypesErr    error

	methods []string
}

func (s *stubImplementsClient) Call(ctx context.Context, method string, params any, result any) error {
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

	case methodTextDocumentPrepareTypeHierarchy:
		if s.prepareErr != nil {
			return s.prepareErr
		}
		if _, ok := params.(typeHierarchyPrepareParams); !ok {
			return fmt.Errorf("prepare type hierarchy params type %T", params)
		}

		out, ok := result.(*[]typeHierarchyItem)
		if !ok {
			return fmt.Errorf("prepare type hierarchy result type %T", result)
		}
		*out = append([]typeHierarchyItem(nil), s.prepareItems...)
		return nil

	case methodTypeHierarchySubtypes:
		if s.subtypesErr != nil {
			return s.subtypesErr
		}

		typedParams, ok := params.(typeHierarchySubtypesParams)
		if !ok {
			return fmt.Errorf("subtypes params type %T", params)
		}

		out, ok := result.(*[]typeHierarchyItem)
		if !ok {
			return fmt.Errorf("subtypes result type %T", result)
		}

		subtypes := s.subtypesByItem[stubTypeHierarchyKey(typedParams.Item)]
		*out = append([]typeHierarchyItem(nil), subtypes...)
		return nil

	default:
		return fmt.Errorf("unexpected method %q", method)
	}
}

func TestImplementsLSPHappyPath(t *testing.T) {
	root := t.TempDir()
	target := implementsTypeHierarchyItem("Target", filepath.Join(root, "target.go"), 2)

	client := &stubImplementsClient{
		workspaceSymbols: []SymbolInformation{
			implementsSymbol("Target", filepath.Join(root, "target.go"), 2),
		},
		prepareItems: []typeHierarchyItem{target},
		subtypesByItem: map[string][]typeHierarchyItem{
			stubTypeHierarchyKey(target): {
				implementsTypeHierarchyItem("ZuluImpl", filepath.Join(root, "zeta.go"), 11),
				implementsTypeHierarchyItem("AlphaImpl", filepath.Join(root, "alpha.go"), 5),
			},
		},
	}

	engine := NewImplementsEngine(client)
	output, err := engine.Find(context.Background(), ImplementsOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"AlphaImpl (alpha.go)",
		"ZuluImpl (zeta.go)",
		"2 implementations",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}

	wantMethods := []string{
		MethodWorkspaceSymbol,
		methodTextDocumentPrepareTypeHierarchy,
		methodTypeHierarchySubtypes,
	}
	if !slices.Equal(client.methods, wantMethods) {
		t.Fatalf("method order = %v, want %v", client.methods, wantMethods)
	}
}

func TestImplementsAmbiguousSymbolDeterministicOrdering(t *testing.T) {
	root := t.TempDir()
	client := &stubImplementsClient{
		workspaceSymbols: []SymbolInformation{
			implementsSymbol("Target", filepath.Join(root, "zeta.go"), 9),
			implementsSymbol("Target", filepath.Join(root, "alpha.go"), 1),
		},
	}

	engine := NewImplementsEngine(client)
	_, err := engine.Find(context.Background(), ImplementsOptions{
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

	wantMethods := []string{MethodWorkspaceSymbol}
	if !slices.Equal(client.methods, wantMethods) {
		t.Fatalf("method order = %v, want %v", client.methods, wantMethods)
	}
}

func TestImplementsMissingLSPBinaryExactError(t *testing.T) {
	engine := NewImplementsEngine(nil)

	_, err := engine.Find(context.Background(), ImplementsOptions{
		WorkspaceRoot: t.TempDir(),
		Symbol:        "Target",
		LSPBinary:     "gopls",
		LSPInstall:    "go install golang.org/x/tools/gopls@latest",
	})
	if err == nil {
		t.Fatal("expected missing LSP error, got nil")
	}

	want := "cs implements: LSP required but gopls not found. Install: go install golang.org/x/tools/gopls@latest"
	if err.Error() != want {
		t.Fatalf("missing LSP error = %q, want %q", err.Error(), want)
	}
}

func TestImplementsDuplicateSuppressionAndOrdering(t *testing.T) {
	root := t.TempDir()
	target := implementsTypeHierarchyItem("Target", filepath.Join(root, "target.go"), 1)

	client := &stubImplementsClient{
		workspaceSymbols: []SymbolInformation{
			implementsSymbol("Target", filepath.Join(root, "target.go"), 1),
		},
		prepareItems: []typeHierarchyItem{target},
		subtypesByItem: map[string][]typeHierarchyItem{
			stubTypeHierarchyKey(target): {
				implementsTypeHierarchyItem("BetaImpl", filepath.Join(root, "beta.go"), 20),
				implementsTypeHierarchyItem("AlphaImpl", filepath.Join(root, "alpha.go"), 30),
				implementsTypeHierarchyItem("AlphaImpl", filepath.Join(root, "alpha.go"), 10),
				implementsTypeHierarchyItem("GammaImpl", filepath.Join(root, "alpha.go"), 2),
				implementsTypeHierarchyItem("BetaImpl", filepath.Join(root, "beta.go"), 20),
			},
		},
	}

	engine := NewImplementsEngine(client)
	output, err := engine.Find(context.Background(), ImplementsOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"AlphaImpl (alpha.go)",
		"GammaImpl (alpha.go)",
		"BetaImpl (beta.go)",
		"3 implementations",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestImplementsMissingSymbol(t *testing.T) {
	root := t.TempDir()
	client := &stubImplementsClient{
		workspaceSymbols: []SymbolInformation{
			implementsSymbol("Other", filepath.Join(root, "other.go"), 0),
		},
	}

	engine := NewImplementsEngine(client)
	_, err := engine.Find(context.Background(), ImplementsOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
	})
	if err == nil {
		t.Fatal("expected missing symbol error, got nil")
	}
	if !errors.Is(err, errSymbolNotFound) {
		t.Fatalf("error = %v, want errSymbolNotFound", err)
	}

	wantMethods := []string{MethodWorkspaceSymbol}
	if !slices.Equal(client.methods, wantMethods) {
		t.Fatalf("method order = %v, want %v", client.methods, wantMethods)
	}
}

func implementsSymbol(name string, path string, line int) SymbolInformation {
	return SymbolInformation{
		Name: name,
		Kind: SymbolKindFunction,
		Location: Location{
			URI: implementsFileURI(path),
			Range: Range{
				Start: Position{Line: line},
			},
		},
	}
}

func implementsTypeHierarchyItem(name string, path string, line int) typeHierarchyItem {
	return typeHierarchyItem{
		Name: name,
		Kind: SymbolKindClass,
		URI:  implementsFileURI(path),
		Range: Range{
			Start: Position{Line: line},
			End:   Position{Line: line, Character: 1},
		},
		SelectionRange: Range{
			Start: Position{Line: line},
			End:   Position{Line: line, Character: 1},
		},
	}
}

func stubTypeHierarchyKey(item typeHierarchyItem) string {
	line, character := typeHierarchyPosition(item)
	return fmt.Sprintf("%s:%d:%d:%s", item.URI, line, character, item.Name)
}

func implementsFileURI(path string) DocumentURI {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return DocumentURI("file://" + slashed)
}
