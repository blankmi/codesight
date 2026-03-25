package engine

import (
	"fmt"
	"strings"
)

// RenderMarkdown converts a SymbolIntelligence result into LLM-facing Markdown.
func RenderMarkdown(result *SymbolIntelligence) string {
	if result == nil {
		return ""
	}

	switch result.Status {
	case "file_too_large":
		return renderFileTooLarge(result)
	case "not_found_exact", "not_found":
		return renderNotFound(result)
	default:
		return renderOK(result)
	}
}

func renderOK(r *SymbolIntelligence) string {
	var b strings.Builder

	title := r.Symbol
	if title == "" {
		title = r.Query
	}
	fmt.Fprintf(&b, "# Symbol: `%s`\n\n", title)

	// Summary.
	b.WriteString("## Summary\n")
	if r.Definition != nil {
		fmt.Fprintf(&b, "- kind: `%s`\n", r.Definition.Type)
		fmt.Fprintf(&b, "- file: `%s`\n", r.Definition.File)
		fmt.Fprintf(&b, "- view: `%s`\n", r.Definition.ViewStrategy)
	}
	fmt.Fprintf(&b, "- confidence: `%.2f`\n", r.Meta.Confidence)
	fmt.Fprintf(&b, "- budget: `%d lines total / %d used`\n",
		r.Meta.Budget.TotalLines,
		estimateUsedLines(r))
	b.WriteByte('\n')

	// Definition.
	if r.Definition != nil {
		renderDefinition(&b, r.Definition)
	}

	// References.
	if len(r.References) > 0 {
		fmt.Fprintf(&b, "## References (%d shown of %d, ranked)\n", r.Meta.RefsShown, r.Meta.RefsTotal)
		for _, ref := range r.References {
			fmt.Fprintf(&b, "- `%s:%d` score `%.0f` reason `%s`\n",
				ref.File, ref.Line, ref.Score, ref.Reason)
		}
		b.WriteByte('\n')
	}

	// Callers.
	if len(r.Callers) > 0 {
		fmt.Fprintf(&b, "## Callers (%d shown, ranked)\n", len(r.Callers))
		for _, caller := range r.Callers {
			fmt.Fprintf(&b, "- `%s` (`%s:%d`) reason `%s`\n",
				caller.Symbol, caller.File, caller.Line, caller.Reason)
		}
		b.WriteByte('\n')
	}

	// Implementations.
	if len(r.Implementations) > 0 {
		fmt.Fprintf(&b, "## Implementations (%d)\n", len(r.Implementations))
		for _, impl := range r.Implementations {
			fmt.Fprintf(&b, "- `%s` (`%s:%d`)\n", impl.Name, impl.File, impl.Line)
		}
		b.WriteByte('\n')
	}

	// Meta.
	renderMeta(&b, &r.Meta)

	return b.String()
}

func renderDefinition(b *strings.Builder, def *SymDefinition) {
	lang := def.Language
	if lang == "" {
		lang = ""
	}

	fmt.Fprintf(b, "## Definition (`%s`, lines %d-%d)\n", def.File, def.Line, def.EndLine)

	if def.Signature != "" {
		fmt.Fprintf(b, "```%s\n%s\n```\n\n", lang, def.Signature)
	}

	switch def.ViewStrategy {
	case "full_body":
		if def.Body != "" {
			fmt.Fprintf(b, "```%s\n%s\n```\n\n", lang, def.Body)
		}
	case "signature_plus_outline":
		renderOutline(b, def, lang)
	default: // signature_plus_slices
		for _, s := range def.Slices {
			fmt.Fprintf(b, "### %s (lines %d-%d)\n", s.Label, s.StartLine, s.EndLine)
			fmt.Fprintf(b, "```%s\n%s\n```\n", lang, s.Code)
			omitted := s.EndLine - s.StartLine
			if omitted > 0 {
				fmt.Fprintf(b, "... %d lines shown ...\n\n", omitted)
			}
		}
	}
}

func renderOutline(b *strings.Builder, def *SymDefinition, lang string) {
	if len(def.Outline) > 0 {
		fmt.Fprintf(b, "### Member Outline (%d members)\n", len(def.Outline))
		b.WriteString("| Line | Kind | Visibility | Name |\n")
		b.WriteString("|------|------|------------|------|\n")
		for _, e := range def.Outline {
			vis := e.Visibility
			if vis == "" {
				vis = "-"
			}
			sig := e.Signature
			if len(sig) > 80 {
				sig = sig[:80] + "..."
			}
			fmt.Fprintf(b, "| %d | %s | %s | `%s` |\n", e.Line, e.Kind, vis, sig)
		}
		b.WriteByte('\n')
	}

	// Expanded top method bodies.
	if len(def.Slices) > 0 {
		for _, s := range def.Slices {
			fmt.Fprintf(b, "### %s (lines %d-%d)\n", s.Label, s.StartLine, s.EndLine)
			fmt.Fprintf(b, "```%s\n%s\n```\n\n", lang, s.Code)
		}

		// Continuation hint: list remaining methods not expanded.
		var remaining []string
		expandedNames := make(map[string]bool)
		for _, s := range def.Slices {
			name := strings.TrimPrefix(s.Label, "Method: ")
			expandedNames[name] = true
		}
		for _, e := range def.Outline {
			if (e.Kind == "method" || e.Kind == "constructor") && !expandedNames[e.Name] {
				remaining = append(remaining, e.Name)
			}
		}
		if len(remaining) > 0 {
			if len(remaining) > 6 {
				remaining = remaining[:6]
			}
			fmt.Fprintf(b, "> Next most relevant methods: `%s`\n\n", strings.Join(remaining, "`, `"))
		}
	}
}

func renderNotFound(r *SymbolIntelligence) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# No Exact Symbol: `%s`\n", r.Query)
	fmt.Fprintf(&b, "No exact symbol named `%s` was found.\n\n", r.Query)

	if len(r.Ambiguous) > 0 {
		b.WriteString("## Closest candidates\n")
		for _, c := range r.Ambiguous {
			fmt.Fprintf(&b, "- `%s` (`%s`, `%s`) reason `%s`\n",
				c.Name, c.Type, c.File, c.Reason)
		}
		b.WriteByte('\n')
	}

	if len(r.References) > 0 {
		fmt.Fprintf(&b, "## Text matches (%d)\n", len(r.References))
		for _, ref := range r.References {
			fmt.Fprintf(&b, "- `%s:%d` `%s`\n", ref.File, ref.Line, strings.TrimSpace(ref.Snippet))
		}
		b.WriteByte('\n')
	}

	renderMeta(&b, &r.Meta)

	return b.String()
}

func renderFileTooLarge(r *SymbolIntelligence) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Error: File Too Large (`%s`)\n", r.Query)
	fmt.Fprintf(&b, "Reading this file will overflow your context window.\n\n")
	fmt.Fprintf(&b, "▶ To search for a specific error, you may now use grep:\n")
	fmt.Fprintf(&b, "`grep -n \"PATTERN\" %s`\n", r.Query)
	fmt.Fprintf(&b, "▶ To read the end of the file, you may use tail:\n")
	fmt.Fprintf(&b, "`tail -n 100 %s`\n", r.Query)

	return b.String()
}

func renderMeta(b *strings.Builder, meta *SymMeta) {
	b.WriteString("## Meta\n")
	fmt.Fprintf(b, "- `search_chain`: `%s`\n", strings.Join(meta.SearchChain, " -> "))
	if meta.NextHint != "" {
		fmt.Fprintf(b, "- `next_hint`: `%s`\n", meta.NextHint)
	}
	if len(meta.Errors) > 0 {
		b.WriteString("- `errors`:\n")
		for _, e := range meta.Errors {
			fmt.Fprintf(b, "  - `%s`\n", e)
		}
	}
}

func estimateUsedLines(r *SymbolIntelligence) int {
	used := 0

	// Summary block ~5 lines.
	used += 5

	if r.Definition != nil {
		if r.Definition.Body != "" {
			used += strings.Count(r.Definition.Body, "\n") + 1
		}
		for _, s := range r.Definition.Slices {
			used += strings.Count(s.Code, "\n") + 3 // slice header + code + spacing
		}
		if len(r.Definition.Outline) > 0 {
			used += len(r.Definition.Outline) + 3 // table header + entries
		}
	}

	// ~2 lines per reference/caller entry.
	used += len(r.References) * 2
	used += len(r.Callers) * 2
	used += len(r.Implementations)

	// Meta ~4 lines.
	used += 4

	return used
}
