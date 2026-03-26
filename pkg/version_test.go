package pkg

import (
	"testing"
	"time"

	"github.com/blankbytes/codesight/pkg/vectorstore"
)

func TestCollectionName_Deterministic(t *testing.T) {
	a := CollectionName("/home/user/project")
	b := CollectionName("/home/user/project")
	if a != b {
		t.Errorf("CollectionName not deterministic: %q != %q", a, b)
	}

	c := CollectionName("/home/user/other")
	if a == c {
		t.Error("expected different collection names for different paths")
	}
}

func TestCollectionName_Prefix(t *testing.T) {
	name := CollectionName("/some/path")
	if len(name) < 4 || name[:3] != "cs_" {
		t.Errorf("expected cs_ prefix, got %q", name)
	}
}

func TestResolveCollectionName_OverrideWins(t *testing.T) {
	got := ResolveCollectionName("/some/path", "shared_worktree_index")
	if got != "shared_worktree_index" {
		t.Fatalf("ResolveCollectionName() = %q, want %q", got, "shared_worktree_index")
	}
}

func TestResolveCollectionName_EmptyOverrideFallsBack(t *testing.T) {
	want := CollectionName("/some/path")

	for _, override := range []string{"", "   "} {
		if got := ResolveCollectionName("/some/path", override); got != want {
			t.Fatalf("ResolveCollectionName(%q) = %q, want %q", override, got, want)
		}
	}
}

func TestIsStale(t *testing.T) {
	tests := []struct {
		name              string
		meta              *vectorstore.IndexMetadata
		commit            string
		ignoreFingerprint string
		want              bool
	}{
		{"nil metadata", nil, "abc123", "ignore-a", true},
		{"matching commit and ignore rules", &vectorstore.IndexMetadata{CommitSHA: "abc123", IgnoreFingerprint: "ignore-a"}, "abc123", "ignore-a", false},
		{"different commit", &vectorstore.IndexMetadata{CommitSHA: "abc123", IgnoreFingerprint: "ignore-a"}, "def456", "ignore-a", true},
		{"different ignore rules", &vectorstore.IndexMetadata{CommitSHA: "abc123", IgnoreFingerprint: "ignore-a"}, "abc123", "ignore-b", true},
		{"missing current commit", &vectorstore.IndexMetadata{CommitSHA: "abc123", IgnoreFingerprint: "ignore-a"}, "", "ignore-a", true},
		{"missing indexed commit", &vectorstore.IndexMetadata{CommitSHA: "", IgnoreFingerprint: "ignore-a"}, "abc123", "ignore-a", true},
		{"missing indexed ignore metadata", &vectorstore.IndexMetadata{CommitSHA: "abc123", IgnoreFingerprint: ""}, "abc123", "ignore-a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStale(tt.meta, tt.commit, tt.ignoreFingerprint)
			if got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStalenessInfo(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		got := StalenessInfo(nil, "abc", "ignore-a")
		if got != "not indexed" {
			t.Errorf("got %q, want %q", got, "not indexed")
		}
	})

	t.Run("up to date", func(t *testing.T) {
		meta := &vectorstore.IndexMetadata{
			CommitSHA:         "abc1234567890",
			IgnoreFingerprint: "ignore-a",
			IndexedAt:         time.Now().Add(-30 * time.Minute),
			FileCount:         42,
			ChunkCount:        200,
		}
		got := StalenessInfo(meta, "abc1234567890", "ignore-a")
		if got != "up to date — indexed 30m ago (42 files, 200 chunks)" {
			t.Fatalf("unexpected status: %q", got)
		}
	})

	t.Run("stale because ignore rules changed", func(t *testing.T) {
		meta := &vectorstore.IndexMetadata{
			CommitSHA:         "abc1234567890",
			IgnoreFingerprint: "ignore-a",
			IndexedAt:         time.Now().Add(-2 * time.Hour),
			FileCount:         42,
			ChunkCount:        200,
		}
		got := StalenessInfo(meta, "abc1234567890", "ignore-b")
		want := "stale — indexed 2h ago, ignore rules changed (42 files, 200 chunks)"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("stale because commit and ignore rules changed", func(t *testing.T) {
		meta := &vectorstore.IndexMetadata{
			CommitSHA:         "abc1234567890",
			IgnoreFingerprint: "ignore-a",
			IndexedAt:         time.Now().Add(-2 * time.Hour),
			FileCount:         42,
			ChunkCount:        200,
		}
		got := StalenessInfo(meta, "def4567890123", "ignore-b")
		want := "stale — indexed 2h ago at abc1234, HEAD is now def4567 and ignore rules changed (42 files, 200 chunks)"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("missing indexed ignore metadata", func(t *testing.T) {
		meta := &vectorstore.IndexMetadata{
			CommitSHA:         "abc1234567890",
			IgnoreFingerprint: "",
			IndexedAt:         time.Now().Add(-5 * time.Minute),
			FileCount:         10,
			ChunkCount:        20,
		}
		got := StalenessInfo(meta, "abc1234567890", "ignore-a")
		want := "stale — indexed 5m ago, missing indexed ignore metadata (10 files, 20 chunks)"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
