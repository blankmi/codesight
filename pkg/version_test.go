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

func TestIsStale(t *testing.T) {
	tests := []struct {
		name   string
		meta   *vectorstore.IndexMetadata
		commit string
		want   bool
	}{
		{"nil metadata", nil, "abc123", true},
		{"matching commit", &vectorstore.IndexMetadata{CommitSHA: "abc123"}, "abc123", false},
		{"different commit", &vectorstore.IndexMetadata{CommitSHA: "abc123"}, "def456", true},
		{"missing current commit", &vectorstore.IndexMetadata{CommitSHA: "abc123"}, "", true},
		{"missing indexed commit", &vectorstore.IndexMetadata{CommitSHA: ""}, "abc123", true},
		{"both commits missing", &vectorstore.IndexMetadata{CommitSHA: ""}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStale(tt.meta, tt.commit)
			if got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStalenessInfo(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		got := StalenessInfo(nil, "abc")
		if got != "not indexed" {
			t.Errorf("got %q, want %q", got, "not indexed")
		}
	})

	t.Run("up to date", func(t *testing.T) {
		meta := &vectorstore.IndexMetadata{
			CommitSHA:  "abc1234567890",
			IndexedAt:  time.Now().Add(-30 * time.Minute),
			FileCount:  42,
			ChunkCount: 200,
		}
		got := StalenessInfo(meta, "abc1234567890")
		if got == "" {
			t.Error("expected non-empty status")
		}
	})

	t.Run("stale", func(t *testing.T) {
		meta := &vectorstore.IndexMetadata{
			CommitSHA:  "abc1234567890",
			IndexedAt:  time.Now().Add(-2 * time.Hour),
			FileCount:  42,
			ChunkCount: 200,
		}
		got := StalenessInfo(meta, "def4567890123")
		if got == "" {
			t.Error("expected non-empty status")
		}
	})

	t.Run("missing commit metadata", func(t *testing.T) {
		meta := &vectorstore.IndexMetadata{
			CommitSHA:  "",
			IndexedAt:  time.Now().Add(-5 * time.Minute),
			FileCount:  10,
			ChunkCount: 20,
		}
		got := StalenessInfo(meta, "")
		if got == "" {
			t.Error("expected non-empty status")
		}
	})
}
