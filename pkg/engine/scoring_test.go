package engine

import "testing"

func TestScoreReferences(t *testing.T) {
	refs := []SymReference{
		{File: "pkg/auth/auth.go", Line: 10, Snippet: "Authenticate(ctx)"},
		{File: "pkg/auth/auth.go", Line: 20, Snippet: "Authenticate(ctx)"},
		{File: "pkg/auth/auth.go", Line: 30, Snippet: "Authenticate(ctx)"},
		{File: "cmd/server/main.go", Line: 50, Snippet: "auth.Authenticate(ctx, token)"},
		{File: "tests/auth_test.go", Line: 5, Snippet: "Authenticate(ctx)"},
		{File: "pkg/unrelated/foo.go", Line: 1, Snippet: "// Authenticate is used here"},
	}

	scored := ScoreReferences(refs, "pkg/auth/auth.go", 4)

	// Should truncate to 4.
	if len(scored) != 4 {
		t.Fatalf("len = %d, want 4", len(scored))
	}

	// All should have non-zero scores.
	for i, ref := range scored {
		if ref.Score == 0 {
			t.Errorf("scored[%d].Score = 0, want non-zero", i)
		}
	}

	// Should be sorted descending.
	for i := 1; i < len(scored); i++ {
		if scored[i].Score > scored[i-1].Score {
			t.Errorf("not sorted descending at index %d: %.1f > %.1f", i, scored[i].Score, scored[i-1].Score)
		}
	}
}

func TestScoreCallers(t *testing.T) {
	callers := []SymCaller{
		{Symbol: "HandleLogin", File: "cmd/server/main.go", Line: 85, Depth: 1},
		{Symbol: "AuthMiddleware", File: "pkg/auth/middleware.go", Line: 10, Depth: 1},
		{Symbol: "DeepCaller", File: "pkg/other/deep.go", Line: 100, Depth: 3},
	}

	scored := ScoreCallers(callers, "pkg/auth/auth.go", 2)

	if len(scored) != 2 {
		t.Fatalf("len = %d, want 2", len(scored))
	}

	// Callers in same package should score higher.
	if scored[0].File != "pkg/auth/middleware.go" {
		t.Errorf("expected same-package caller first, got %s", scored[0].File)
	}
}

func TestNearDuplicatePenalty(t *testing.T) {
	// 5 refs from same file should trigger penalty.
	refs := make([]SymReference, 5)
	for i := range refs {
		refs[i] = SymReference{
			File:    "pkg/auth/auth.go",
			Line:    i + 1,
			Snippet: "call()",
		}
	}

	diverse := SymReference{
		File:    "cmd/server/main.go",
		Line:    1,
		Snippet: "call()",
	}
	refs = append(refs, diverse)

	scored := ScoreReferences(refs, "pkg/other/other.go", 10)

	// The diverse ref should rank higher due to no near-duplicate penalty.
	found := false
	for i, ref := range scored {
		if ref.File == "cmd/server/main.go" {
			if i > 3 {
				t.Errorf("diverse ref ranked at %d, expected in top 3", i)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("diverse ref not found in scored results")
	}
}
