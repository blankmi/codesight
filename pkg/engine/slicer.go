package engine

import (
	"context"
	"strings"

	"github.com/blankbytes/codesight/pkg/splitter"
	sitter "github.com/smacker/go-tree-sitter"
)

const (
	headerSliceLines = 12
	salientSliceMax  = 3
	salientSliceSize = 10
)

// SliceDefinition extracts a signature and salient slices from a definition body
// when it exceeds the budget. Returns (signature, slices, viewStrategy).
// If the body fits within budgetLines, viewStrategy is "full_body" and slices is nil.
func SliceDefinition(source []byte, language string, startLine, endLine, budgetLines int) (string, []CodeSlice, string) {
	bodyLines := endLine - startLine + 1
	if bodyLines <= budgetLines {
		return extractSignature(source, startLine), nil, "full_body"
	}

	sig := extractSignature(source, startLine)
	slices := make([]CodeSlice, 0, salientSliceMax+1)

	lines := strings.Split(string(source), "\n")

	// Header slice: first N lines of the body.
	headerEnd := startLine - 1 + headerSliceLines
	if headerEnd > endLine-1 {
		headerEnd = endLine - 1
	}
	slices = append(slices, CodeSlice{
		Label:     "Header slice",
		StartLine: startLine,
		EndLine:   headerEnd + 1,
		Reason:    "function entry",
		Code:      joinLines(lines, startLine-1, headerEnd),
	})

	// Try to find salient slices via Tree-sitter.
	treeSlices := findSalientSlices(source, language, startLine, endLine)
	for _, s := range treeSlices {
		if len(slices) >= salientSliceMax+1 {
			break
		}
		// Avoid overlap with header slice.
		if s.StartLine <= headerEnd+1 {
			continue
		}
		slices = append(slices, s)
	}

	// Return path slice: last few lines.
	if endLine-salientSliceSize > headerEnd+1 && len(slices) < salientSliceMax+1 {
		returnStart := endLine - salientSliceSize
		if returnStart < startLine {
			returnStart = startLine
		}
		slices = append(slices, CodeSlice{
			Label:     "Return path slice",
			StartLine: returnStart,
			EndLine:   endLine,
			Reason:    "return path",
			Code:      joinLines(lines, returnStart-1, endLine-1),
		})
	}

	return sig, slices, "signature_plus_slices"
}

func extractSignature(source []byte, startLine int) string {
	lines := strings.Split(string(source), "\n")
	if startLine-1 >= len(lines) {
		return ""
	}
	// Return the first line as the signature.
	return strings.TrimRight(lines[startLine-1], "\r\n")
}

func joinLines(lines []string, from, to int) string {
	if from < 0 {
		from = 0
	}
	if to >= len(lines) {
		to = len(lines) - 1
	}
	if from > to {
		return ""
	}
	return strings.Join(lines[from:to+1], "\n")
}

func findSalientSlices(source []byte, language string, startLine, endLine int) []CodeSlice {
	lang := splitter.GetLanguage(language)
	if lang == nil {
		return findSalientSlicesByText(source, startLine, endLine)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return findSalientSlicesByText(source, startLine, endLine)
	}
	defer tree.Close()

	return findErrorAndIOSlices(tree.RootNode(), source, startLine, endLine)
}

func findErrorAndIOSlices(root *sitter.Node, source []byte, startLine, endLine int) []CodeSlice {
	var slices []CodeSlice
	lines := strings.Split(string(source), "\n")

	// Walk the AST looking for error handling and I/O patterns.
	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil || len(slices) >= salientSliceMax {
			return
		}

		nodeLine := int(node.StartPoint().Row) + 1
		if nodeLine < startLine || nodeLine > endLine {
			for i := 0; i < int(node.ChildCount()); i++ {
				walk(node.Child(i))
			}
			return
		}

		nodeType := node.Type()
		content := node.Content(source)

		isErrorPath := (nodeType == "if_statement" &&
			(strings.Contains(content, "err != nil") || strings.Contains(content, "err :="))) ||
			nodeType == "try_statement" || nodeType == "catch_clause"

		isIO := nodeType == "call_expression" &&
			(strings.Contains(content, "Read") || strings.Contains(content, "Write") ||
				strings.Contains(content, "Open") || strings.Contains(content, "Close") ||
				strings.Contains(content, "Get") || strings.Contains(content, "Post") ||
				strings.Contains(content, "Query") || strings.Contains(content, "Exec") ||
				strings.Contains(content, "Send") || strings.Contains(content, "Recv"))

		if isErrorPath || isIO {
			label := "Error path slice"
			reason := "error handling"
			if isIO {
				label = "I/O site slice"
				reason = "external I/O"
			}

			sliceEnd := int(node.EndPoint().Row) + 1
			if sliceEnd-nodeLine > salientSliceSize {
				sliceEnd = nodeLine + salientSliceSize - 1
			}
			if sliceEnd > endLine {
				sliceEnd = endLine
			}
			slices = append(slices, CodeSlice{
				Label:     label,
				StartLine: nodeLine,
				EndLine:   sliceEnd,
				Reason:    reason,
				Code:      joinLines(lines, nodeLine-1, sliceEnd-1),
			})
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i))
		}
	}

	walk(root)
	return slices
}

// findSalientSlicesByText is a fallback when Tree-sitter parsing isn't available.
func findSalientSlicesByText(source []byte, startLine, endLine int) []CodeSlice {
	lines := strings.Split(string(source), "\n")
	var slices []CodeSlice

	errorPatterns := []string{"if err", "catch", "except ", "rescue "}
	ioPatterns := []string{"Read", "Write", "Open", "Close", "Get", "Post", "Query", "Exec", "Send", "Recv"}

	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		if len(slices) >= salientSliceMax {
			break
		}
		line := lines[i]

		isError := false
		for _, pattern := range errorPatterns {
			if strings.Contains(line, pattern) {
				isError = true
				break
			}
		}

		isIO := false
		if !isError {
			for _, pattern := range ioPatterns {
				if strings.Contains(line, pattern) {
					isIO = true
					break
				}
			}
		}

		if isError || isIO {
			label := "Error path slice"
			reason := "error handling"
			if isIO {
				label = "I/O site slice"
				reason = "external I/O"
			}

			sliceEnd := i + salientSliceSize
			if sliceEnd >= len(lines) {
				sliceEnd = len(lines) - 1
			}
			if sliceEnd >= endLine {
				sliceEnd = endLine - 1
			}
			slices = append(slices, CodeSlice{
				Label:     label,
				StartLine: i + 1,
				EndLine:   sliceEnd + 1,
				Reason:    reason,
				Code:      joinLines(lines, i, sliceEnd),
			})

			// Skip ahead.
			i = sliceEnd
		}
	}

	return slices
}
