package pkg

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/blankbytes/codesight/pkg/vectorstore"
)

// CollectionName returns a deterministic collection name for a project path.
func CollectionName(projectPath string) string {
	hash := sha256.Sum256([]byte(projectPath))
	return fmt.Sprintf("cs_%x", hash[:8])
}

// IsStale returns true if the indexed commit differs from the current HEAD.
func IsStale(meta *vectorstore.IndexMetadata, currentCommit string) bool {
	if meta == nil {
		return true
	}
	indexedCommit := strings.TrimSpace(meta.CommitSHA)
	currentCommit = strings.TrimSpace(currentCommit)
	if indexedCommit == "" || currentCommit == "" {
		return true
	}
	return indexedCommit != currentCommit
}

// StalenessInfo returns a human-readable description of the index state.
func StalenessInfo(meta *vectorstore.IndexMetadata, currentCommit string) string {
	if meta == nil {
		return "not indexed"
	}

	age := time.Since(meta.IndexedAt).Truncate(time.Minute)
	ageStr := formatAge(age)

	indexedCommit := strings.TrimSpace(meta.CommitSHA)
	currentCommit = strings.TrimSpace(currentCommit)
	if indexedCommit == "" || currentCommit == "" {
		return fmt.Sprintf("unknown — indexed %s ago (missing commit metadata; %d files, %d chunks)",
			ageStr, meta.FileCount, meta.ChunkCount)
	}

	if indexedCommit == currentCommit {
		return fmt.Sprintf("up to date — indexed %s ago (%d files, %d chunks)",
			ageStr, meta.FileCount, meta.ChunkCount)
	}

	return fmt.Sprintf("stale — indexed %s ago at %s, HEAD is now %s (%d files, %d chunks)",
		ageStr, abbreviate(indexedCommit), abbreviate(currentCommit), meta.FileCount, meta.ChunkCount)
}

func abbreviate(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
