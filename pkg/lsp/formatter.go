package lsp

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const allowedRefKindsCSV = "function, method, class, interface, type, constant"

type referenceLine struct {
	Path    string
	Line    int
	Snippet string
}

type symbolCandidate struct {
	Path string
	Line int
	Kind string
}

func formatReferencesOutput(references []referenceLine, fallbackNote string) string {
	groupedRefs, refCount := compactReferences(references)

	lines := make([]string, 0, refCount+2)
	if fallbackNote != "" {
		lines = append(lines, fallbackNote)
	}

	for _, group := range groupedRefs {
		label := "refs"
		if len(group.Refs) == 1 {
			label = "ref"
		}
		lines = append(lines, fmt.Sprintf("%s (%d %s)", group.Path, len(group.Refs), label))
		for _, ref := range group.Refs {
			lines = append(lines, fmt.Sprintf("  :%d  %s", ref.Line, normalizeSnippet(ref.Snippet)))
		}
	}

	lines = append(lines, fmt.Sprintf("%d references found", refCount))
	return strings.Join(lines, "\n")
}

func formatAmbiguousSymbolError(symbol string, candidates []symbolCandidate) error {
	sorted := append([]symbolCandidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		return sorted[i].Kind < sorted[j].Kind
	})

	header := fmt.Sprintf(
		`ambiguous symbol %q — %d definitions found. Use --path to narrow scope.`,
		symbol,
		len(sorted),
	)
	if len(sorted) == 0 {
		return errors.New(header)
	}

	var builder strings.Builder
	builder.WriteString(header)
	for _, candidate := range sorted {
		builder.WriteString("\n")
		builder.WriteString(fmt.Sprintf("  - %s:%d (%s)", candidate.Path, candidate.Line, candidate.Kind))
	}

	return errors.New(builder.String())
}

func dedupeAndSortReferences(references []referenceLine) []referenceLine {
	if len(references) == 0 {
		return nil
	}

	sorted := append([]referenceLine(nil), references...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		return sorted[i].Snippet < sorted[j].Snippet
	})

	out := make([]referenceLine, 0, len(sorted))
	seen := make(map[string]struct{}, len(sorted))
	for _, ref := range sorted {
		key := fmt.Sprintf("%s:%d:%s", ref.Path, ref.Line, ref.Snippet)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}

	return out
}

func normalizeSnippet(snippet string) string {
	return strings.TrimSpace(snippet)
}

type referenceGroup struct {
	Path string
	Refs []referenceLine
}

func compactReferences(references []referenceLine) ([]referenceGroup, int) {
	sorted := dedupeAndSortReferences(references)
	groups := make([]referenceGroup, 0)
	total := 0

	for _, ref := range sorted {
		if isImportReference(ref.Snippet) {
			continue
		}
		total++

		if len(groups) == 0 || groups[len(groups)-1].Path != ref.Path {
			groups = append(groups, referenceGroup{
				Path: ref.Path,
				Refs: []referenceLine{ref},
			})
			continue
		}
		groups[len(groups)-1].Refs = append(groups[len(groups)-1].Refs, ref)
	}

	return groups, total
}

func isImportReference(snippet string) bool {
	return strings.HasPrefix(normalizeSnippet(snippet), "import ")
}
