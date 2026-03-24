package engine

import (
	"path/filepath"
	"sort"
	"strings"
)

const (
	weightDirectCall       = 3.0
	weightSameFileOrPkg    = 2.0
	weightPathProximity    = 2.0
	weightDiversityBonus   = 1.0
	penaltyNearDuplicate   = 2.0
	duplicateThresholdFile = 2 // refs from the same file after this count get penalized
)

// ScoreReferences scores and sorts references descending by score,
// then truncates to maxItems.
func ScoreReferences(refs []SymReference, defFile string, maxItems int) []SymReference {
	if len(refs) == 0 {
		return refs
	}

	defDir := filepath.Dir(defFile)
	fileCounts := countByFile(refs)

	for i := range refs {
		refs[i].Score = scoreRef(refs[i], defFile, defDir, fileCounts)
	}

	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].Score > refs[j].Score
	})

	if maxItems > 0 && len(refs) > maxItems {
		refs = refs[:maxItems]
	}
	return refs
}

// ScoreCallers scores and sorts callers descending by score,
// then truncates to maxItems.
func ScoreCallers(callers []SymCaller, defFile string, maxItems int) []SymCaller {
	if len(callers) == 0 {
		return callers
	}

	defDir := filepath.Dir(defFile)

	for i := range callers {
		callers[i].Score = scoreCaller(callers[i], defFile, defDir)
	}

	sort.SliceStable(callers, func(i, j int) bool {
		return callers[i].Score > callers[j].Score
	})

	if maxItems > 0 && len(callers) > maxItems {
		callers = callers[:maxItems]
	}
	return callers
}

func scoreRef(ref SymReference, defFile, defDir string, fileCounts map[string]int) float64 {
	score := 0.0

	// Direct call bonus: snippet contains function-call-like pattern.
	if strings.Contains(ref.Snippet, "(") {
		score += weightDirectCall
		ref.Reason = appendReason(ref.Reason, "direct call")
	}

	// Same file or same package.
	if ref.File == defFile {
		score += weightSameFileOrPkg
		ref.Reason = appendReason(ref.Reason, "same file")
	} else if filepath.Dir(ref.File) == defDir {
		score += weightSameFileOrPkg
		ref.Reason = appendReason(ref.Reason, "same package")
	}

	// Path proximity: shared path prefix depth.
	score += pathProximityScore(ref.File, defFile)

	// Test file bonus for diversity.
	if isTestFile(ref.File) {
		score += weightDiversityBonus
		ref.Reason = appendReason(ref.Reason, "test anchor")
	}

	// Near-duplicate penalty.
	if fileCounts[ref.File] > duplicateThresholdFile {
		score -= penaltyNearDuplicate
	}

	return score
}

func scoreCaller(caller SymCaller, defFile, defDir string) float64 {
	score := weightDirectCall // callers are always direct

	if caller.File == defFile {
		score += weightSameFileOrPkg
	} else if filepath.Dir(caller.File) == defDir {
		score += weightSameFileOrPkg
	}

	score += pathProximityScore(caller.File, defFile)

	// Shallow callers rank higher.
	if caller.Depth <= 1 {
		score += weightDiversityBonus
	}

	return score
}

func pathProximityScore(file, defFile string) float64 {
	fileParts := strings.Split(filepath.ToSlash(file), "/")
	defParts := strings.Split(filepath.ToSlash(defFile), "/")

	shared := 0
	limit := len(fileParts)
	if len(defParts) < limit {
		limit = len(defParts)
	}
	for i := 0; i < limit; i++ {
		if fileParts[i] != defParts[i] {
			break
		}
		shared++
	}

	if shared == 0 {
		return 0
	}
	// Normalize: more shared segments = closer to weightPathProximity.
	return weightPathProximity * float64(shared) / float64(len(defParts))
}

func countByFile(refs []SymReference) map[string]int {
	counts := make(map[string]int, len(refs))
	for _, ref := range refs {
		counts[ref.File]++
	}
	return counts
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasPrefix(base, "test_") ||
		strings.Contains(path, "/test/") ||
		strings.Contains(path, "/tests/")
}

func appendReason(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + " + " + addition
}
