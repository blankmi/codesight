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

// IsStale returns true if the indexed commit or ignore rules differ from the current state.
func IsStale(meta *vectorstore.IndexMetadata, currentCommit string, currentIgnoreFingerprint string) bool {
	if meta == nil {
		return true
	}

	indexedCommit := strings.TrimSpace(meta.CommitSHA)
	currentCommit = strings.TrimSpace(currentCommit)
	indexedIgnoreFingerprint := strings.TrimSpace(meta.IgnoreFingerprint)
	currentIgnoreFingerprint = strings.TrimSpace(currentIgnoreFingerprint)

	if indexedIgnoreFingerprint == "" || currentIgnoreFingerprint == "" {
		return true
	}
	if indexedIgnoreFingerprint != currentIgnoreFingerprint {
		return true
	}

	if indexedCommit == "" || currentCommit == "" {
		return true
	}
	return indexedCommit != currentCommit
}

// StalenessInfo returns a human-readable description of the index state.
func StalenessInfo(meta *vectorstore.IndexMetadata, currentCommit string, currentIgnoreFingerprint string) string {
	if meta == nil {
		return "not indexed"
	}

	age := time.Since(meta.IndexedAt).Truncate(time.Minute)
	ageStr := formatAge(age)

	indexedCommit := strings.TrimSpace(meta.CommitSHA)
	currentCommit = strings.TrimSpace(currentCommit)
	indexedIgnoreFingerprint := strings.TrimSpace(meta.IgnoreFingerprint)
	currentIgnoreFingerprint = strings.TrimSpace(currentIgnoreFingerprint)

	if indexedCommit != "" &&
		currentCommit != "" &&
		indexedCommit == currentCommit &&
		indexedIgnoreFingerprint != "" &&
		currentIgnoreFingerprint != "" &&
		indexedIgnoreFingerprint == currentIgnoreFingerprint {
		return fmt.Sprintf("up to date — indexed %s ago (%d files, %d chunks)",
			ageStr, meta.FileCount, meta.ChunkCount)
	}

	reasons := make([]string, 0, 2)
	if indexedCommit == "" {
		reasons = append(reasons, "missing indexed commit metadata")
	} else if currentCommit == "" {
		reasons = append(reasons, "current HEAD unavailable")
	} else if indexedCommit != currentCommit {
		reasons = append(reasons, fmt.Sprintf("HEAD is now %s", abbreviate(currentCommit)))
	}

	if indexedIgnoreFingerprint == "" {
		reasons = append(reasons, "missing indexed ignore metadata")
	} else if currentIgnoreFingerprint == "" {
		reasons = append(reasons, "current ignore rules unavailable")
	} else if indexedIgnoreFingerprint != currentIgnoreFingerprint {
		reasons = append(reasons, "ignore rules changed")
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "status unknown")
	}

	reasonText := strings.Join(reasons, " and ")
	if indexedCommit != "" && currentCommit != "" && indexedCommit != currentCommit {
		return fmt.Sprintf("stale — indexed %s ago at %s, %s (%d files, %d chunks)",
			ageStr, abbreviate(indexedCommit), reasonText, meta.FileCount, meta.ChunkCount)
	}

	return fmt.Sprintf("stale — indexed %s ago, %s (%d files, %d chunks)",
		ageStr, reasonText, meta.FileCount, meta.ChunkCount)
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
