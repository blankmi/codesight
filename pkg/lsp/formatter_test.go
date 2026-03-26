package lsp

import (
	"strings"
	"testing"
)

func TestFormatterReferencesOutputContract(t *testing.T) {
	references := []referenceLine{
		{Path: "zeta.go", Line: 7, Snippet: " target()"},
		{Path: "alpha.go", Line: 3, Snippet: "\ttarget()"},
	}

	output := formatReferencesOutput(references, "")
	want := strings.Join([]string{
		"alpha.go (1 ref)",
		"  :3  target()",
		"zeta.go (1 ref)",
		"  :7  target()",
		"2 references found",
	}, "\n")

	if output != want {
		t.Fatalf("formatted output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestFormatterReferencesOutputIncludesFallbackNote(t *testing.T) {
	references := []referenceLine{
		{Path: "alpha.go", Line: 2, Snippet: "target()"},
	}

	output := formatReferencesOutput(references, "(grep-based - install gopls for precise results)")
	want := strings.Join([]string{
		"(grep-based - install gopls for precise results)",
		"alpha.go (1 ref)",
		"  :2  target()",
		"1 references found",
	}, "\n")

	if output != want {
		t.Fatalf("formatted fallback output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestFormatterReferencesOutputDropsImportLinesAndGroupsByFile(t *testing.T) {
	references := []referenceLine{
		{Path: "alpha.go", Line: 2, Snippet: "import target/pkg"},
		{Path: "alpha.go", Line: 8, Snippet: " target()"},
		{Path: "alpha.go", Line: 12, Snippet: "\ttarget(value)"},
		{Path: "bravo.go", Line: 4, Snippet: "target()"},
	}

	output := formatReferencesOutput(references, "")
	want := strings.Join([]string{
		"alpha.go (2 refs)",
		"  :8  target()",
		"  :12  target(value)",
		"bravo.go (1 ref)",
		"  :4  target()",
		"3 references found",
	}, "\n")

	if output != want {
		t.Fatalf("formatted grouped output mismatch\n got: %q\nwant: %q", output, want)
	}
}

func TestFormatterAmbiguousSymbolErrorOrdering(t *testing.T) {
	err := formatAmbiguousSymbolError("Target", []symbolCandidate{
		{Path: "zeta.go", Line: 9, Kind: "function"},
		{Path: "alpha.go", Line: 2, Kind: "function"},
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
	if lines[2] != "  - zeta.go:9 (function)" {
		t.Fatalf("second candidate = %q, want %q", lines[2], "  - zeta.go:9 (function)")
	}
}
