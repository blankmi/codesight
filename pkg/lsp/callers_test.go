package lsp

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type stubCallersClient struct {
	workspaceSymbols   []SymbolInformation
	workspaceSymbolErr error

	prepareItems []CallHierarchyItem
	prepareErr   error

	incomingByItem map[string][]CallHierarchyIncomingCall
	incomingErr    error

	methods []string
}

func (s *stubCallersClient) Call(ctx context.Context, method string, params any, result any) error {
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

	case MethodTextDocumentPrepareCallHierarchy:
		if s.prepareErr != nil {
			return s.prepareErr
		}
		if _, ok := params.(CallHierarchyPrepareParams); !ok {
			return fmt.Errorf("prepare call hierarchy params type %T", params)
		}

		out, ok := result.(*[]CallHierarchyItem)
		if !ok {
			return fmt.Errorf("prepare call hierarchy result type %T", result)
		}
		*out = append([]CallHierarchyItem(nil), s.prepareItems...)
		return nil

	case MethodCallHierarchyIncomingCalls:
		if s.incomingErr != nil {
			return s.incomingErr
		}

		typedParams, ok := params.(CallHierarchyIncomingCallsParams)
		if !ok {
			return fmt.Errorf("incoming calls params type %T", params)
		}

		out, ok := result.(*[]CallHierarchyIncomingCall)
		if !ok {
			return fmt.Errorf("incoming calls result type %T", result)
		}

		incoming := s.incomingByItem[stubCallHierarchyKey(typedParams.Item)]
		*out = append([]CallHierarchyIncomingCall(nil), incoming...)
		return nil

	default:
		return fmt.Errorf("unexpected method %q", method)
	}
}

func TestCallersAmbiguousSymbolDeterministicOrdering(t *testing.T) {
	root := t.TempDir()
	client := &stubCallersClient{
		workspaceSymbols: []SymbolInformation{
			{
				Name: "Target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI: callersFileURI(filepath.Join(root, "zeta.go")),
					Range: Range{
						Start: Position{Line: 9},
					},
				},
			},
			{
				Name: "Target",
				Kind: SymbolKindFunction,
				Location: Location{
					URI: callersFileURI(filepath.Join(root, "alpha.go")),
					Range: Range{
						Start: Position{Line: 1},
					},
				},
			},
		},
	}

	engine := NewCallersEngine(client)
	_, err := engine.Find(context.Background(), CallersOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
		Depth:         1,
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

func TestCallersDepthOneFormatting(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target.go")
	alphaPath := filepath.Join(root, "alpha.go")
	bravoPath := filepath.Join(root, "bravo.go")

	rootItem := callersHierarchyItem(targetPath, "Target", 9)
	client := &stubCallersClient{
		workspaceSymbols: []SymbolInformation{
			callersSymbol(targetPath, "Target", 9),
		},
		prepareItems: []CallHierarchyItem{rootItem},
		incomingByItem: map[string][]CallHierarchyIncomingCall{
			stubCallHierarchyKey(rootItem): {
				callersIncomingCall(bravoPath, "bravoCaller", 20),
				callersIncomingCall(alphaPath, "alphaCaller", 12),
			},
		},
	}

	engine := NewCallersEngine(client)
	output, err := engine.Find(context.Background(), CallersOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
		Depth:         1,
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"Target (target.go:10)",
		"  <- alphaCaller (alpha.go:13)",
		"  <- bravoCaller (bravo.go:21)",
		"2 callers (depth 1)",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}

	wantMethods := []string{
		MethodWorkspaceSymbol,
		MethodTextDocumentPrepareCallHierarchy,
		MethodCallHierarchyIncomingCalls,
	}
	if !slices.Equal(client.methods, wantMethods) {
		t.Fatalf("method order = %v, want %v", client.methods, wantMethods)
	}
}

func TestCallersDepthTwoTraversal(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target.go")
	alphaPath := filepath.Join(root, "alpha.go")
	betaPath := filepath.Join(root, "beta.go")
	deltaPath := filepath.Join(root, "delta.go")
	gammaPath := filepath.Join(root, "gamma.go")

	rootItem := callersHierarchyItem(targetPath, "Target", 9)
	alphaItem := callersHierarchyItem(alphaPath, "alphaCaller", 12)
	betaItem := callersHierarchyItem(betaPath, "betaCaller", 25)
	gammaItem := callersHierarchyItem(gammaPath, "gammaCaller", 33)
	deltaItem := callersHierarchyItem(deltaPath, "deltaCaller", 40)

	client := &stubCallersClient{
		workspaceSymbols: []SymbolInformation{
			callersSymbol(targetPath, "Target", 9),
		},
		prepareItems: []CallHierarchyItem{rootItem},
		incomingByItem: map[string][]CallHierarchyIncomingCall{
			stubCallHierarchyKey(rootItem): {
				{From: betaItem},
				{From: alphaItem},
			},
			stubCallHierarchyKey(alphaItem): {
				{From: gammaItem},
			},
			stubCallHierarchyKey(betaItem): {
				{From: deltaItem},
			},
		},
	}

	engine := NewCallersEngine(client)
	output, err := engine.Find(context.Background(), CallersOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
		Depth:         2,
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"Target (target.go:10)",
		"  <- alphaCaller (alpha.go:13)",
		"    <- gammaCaller (gamma.go:34)",
		"  <- betaCaller (beta.go:26)",
		"    <- deltaCaller (delta.go:41)",
		"4 callers (depth 2)",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestCallersShallowestDepthWinsForDuplicateCaller(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target.go")
	alphaPath := filepath.Join(root, "alpha.go")
	betaPath := filepath.Join(root, "beta.go")

	rootItem := callersHierarchyItem(targetPath, "Target", 9)
	alphaItem := callersHierarchyItem(alphaPath, "alphaCaller", 12)
	betaItem := callersHierarchyItem(betaPath, "betaCaller", 25)

	client := &stubCallersClient{
		workspaceSymbols: []SymbolInformation{
			callersSymbol(targetPath, "Target", 9),
		},
		prepareItems: []CallHierarchyItem{rootItem},
		incomingByItem: map[string][]CallHierarchyIncomingCall{
			stubCallHierarchyKey(rootItem): {
				{From: betaItem},
				{From: alphaItem},
			},
			stubCallHierarchyKey(alphaItem): {
				{From: betaItem}, // also appears as a direct caller of Target
			},
		},
	}

	engine := NewCallersEngine(client)
	output, err := engine.Find(context.Background(), CallersOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
		Depth:         2,
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"Target (target.go:10)",
		"  <- alphaCaller (alpha.go:13)",
		"  <- betaCaller (beta.go:26)",
		"2 callers (depth 2)",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestCallersCycleAndDuplicateSuppression(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target.go")
	alphaPath := filepath.Join(root, "alpha.go")
	betaPath := filepath.Join(root, "beta.go")
	gammaPath := filepath.Join(root, "gamma.go")

	rootItem := callersHierarchyItem(targetPath, "Target", 9)
	alphaItem := callersHierarchyItem(alphaPath, "alphaCaller", 12)
	betaItem := callersHierarchyItem(betaPath, "betaCaller", 25)
	gammaItem := callersHierarchyItem(gammaPath, "gammaCaller", 33)

	client := &stubCallersClient{
		workspaceSymbols: []SymbolInformation{
			callersSymbol(targetPath, "Target", 9),
		},
		prepareItems: []CallHierarchyItem{rootItem},
		incomingByItem: map[string][]CallHierarchyIncomingCall{
			stubCallHierarchyKey(rootItem): {
				{From: betaItem},
				{From: alphaItem},
				{From: alphaItem}, // duplicate
			},
			stubCallHierarchyKey(alphaItem): {
				{From: rootItem},  // cycle back to root
				{From: gammaItem}, // unique
				{From: gammaItem}, // duplicate
			},
			stubCallHierarchyKey(betaItem): {
				{From: gammaItem}, // duplicate across branch
			},
			stubCallHierarchyKey(gammaItem): {
				{From: alphaItem}, // deeper cycle
			},
		},
	}

	engine := NewCallersEngine(client)
	output, err := engine.Find(context.Background(), CallersOptions{
		WorkspaceRoot: root,
		Symbol:        "Target",
		Depth:         3,
	})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	want := strings.Join([]string{
		"Target (target.go:10)",
		"  <- alphaCaller (alpha.go:13)",
		"    <- gammaCaller (gamma.go:34)",
		"  <- betaCaller (beta.go:26)",
		"3 callers (depth 3)",
	}, "\n")
	if output != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestCallersInvalidDepth(t *testing.T) {
	engine := NewCallersEngine(&stubCallersClient{})

	_, err := engine.Find(context.Background(), CallersOptions{
		WorkspaceRoot: t.TempDir(),
		Symbol:        "Target",
		Depth:         0,
	})
	if err == nil {
		t.Fatal("expected depth validation error, got nil")
	}

	want := "depth must be a positive integer"
	if err.Error() != want {
		t.Fatalf("depth error = %q, want %q", err.Error(), want)
	}
}

func TestCallersMissingLSPBinaryExactError(t *testing.T) {
	engine := NewCallersEngine(nil)

	_, err := engine.Find(context.Background(), CallersOptions{
		WorkspaceRoot: t.TempDir(),
		Symbol:        "Target",
		Depth:         1,
		LSPBinary:     "gopls",
		LSPInstall:    "go install golang.org/x/tools/gopls@latest",
	})
	if err == nil {
		t.Fatal("expected missing LSP error, got nil")
	}

	want := "cs callers: LSP required but gopls not found. Install: go install golang.org/x/tools/gopls@latest"
	if err.Error() != want {
		t.Fatalf("missing LSP error = %q, want %q", err.Error(), want)
	}
}

func TestCallersDeferredCommandIntegration(t *testing.T) {
	t.Skip("blocked by TK-008: callers command wiring pending")
}

func callersSymbol(path string, name string, line int) SymbolInformation {
	return SymbolInformation{
		Name: name,
		Kind: SymbolKindFunction,
		Location: Location{
			URI: callersFileURI(path),
			Range: Range{
				Start: Position{Line: line, Character: 0},
				End:   Position{Line: line, Character: 1},
			},
		},
	}
}

func callersHierarchyItem(path string, name string, line int) CallHierarchyItem {
	return CallHierarchyItem{
		Name: name,
		Kind: SymbolKindFunction,
		URI:  callersFileURI(path),
		Range: Range{
			Start: Position{Line: line, Character: 0},
			End:   Position{Line: line, Character: 1},
		},
		SelectionRange: Range{
			Start: Position{Line: line, Character: 0},
			End:   Position{Line: line, Character: 1},
		},
	}
}

func callersIncomingCall(path string, name string, line int) CallHierarchyIncomingCall {
	return CallHierarchyIncomingCall{
		From: callersHierarchyItem(path, name, line),
	}
}

func stubCallHierarchyKey(item CallHierarchyItem) string {
	line := item.SelectionRange.Start.Line
	character := item.SelectionRange.Start.Character
	return fmt.Sprintf("%s:%d:%d:%s", item.URI, line, character, item.Name)
}

func callersFileURI(path string) DocumentURI {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return DocumentURI("file://" + slashed)
}
