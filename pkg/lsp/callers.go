package lsp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type callersClient interface {
	Call(ctx context.Context, method string, params any, result any) error
}

// CallersOptions configures one callers lookup.
type CallersOptions struct {
	WorkspaceRoot string
	FilterPath    string
	Symbol        string
	Depth         int
	LSPBinary     string
	LSPInstall    string
}

// CallersEngine resolves and traverses incoming call hierarchy using LSP.
type CallersEngine struct {
	client callersClient
}

type callerOutputLine struct {
	depth int
	name  string
	path  string
	line  int
}

type hierarchyNode struct {
	item      CallHierarchyItem
	name      string
	path      string
	line      int
	character int
	identity  string
}

// NewCallersEngine creates a call hierarchy engine.
func NewCallersEngine(client callersClient) *CallersEngine {
	return &CallersEngine{client: client}
}

// Find returns formatted incoming callers for a symbol.
func (e *CallersEngine) Find(ctx context.Context, opts CallersOptions) (string, error) {
	symbol := strings.TrimSpace(opts.Symbol)
	if symbol == "" {
		return "", errors.New("symbol is required")
	}
	if opts.Depth <= 0 {
		return "", errors.New("depth must be a positive integer")
	}

	workspaceRoot, err := resolveWorkspaceRoot(opts.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	if e.client == nil {
		return "", formatMissingCallersLSPError(opts.LSPBinary, opts.LSPInstall)
	}
	matcher, err := newWorkspaceIgnoreMatcher(workspaceRoot)
	if err != nil {
		return "", err
	}

	symbols, err := e.lookupSymbols(ctx, symbol)
	if err != nil {
		return "", fmt.Errorf("workspace/symbol request failed: %w", err)
	}
	if len(symbols) == 0 {
		return "", fmt.Errorf("%w: %q", errLSPNoSymbols, symbol)
	}

	candidates, err := resolveCandidates(symbols, workspaceRoot, opts.FilterPath, matcher, symbol, "")
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf(`%w: %q`, errSymbolNotFound, symbol)
	}
	if len(candidates) > 1 {
		formatted := make([]symbolCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			formatted = append(formatted, symbolCandidate{
				Path: candidate.path,
				Line: candidate.line,
				Kind: candidate.kind,
			})
		}
		return "", formatAmbiguousSymbolError(symbol, formatted)
	}

	rootCandidate := candidates[0]
	rootName := rootCandidate.info.Name
	rootLine := fmt.Sprintf("%s (%s:%d)", rootName, rootCandidate.path, rootCandidate.line)

	rootItem, ok, err := e.prepareCallHierarchy(ctx, rootCandidate.info)
	if err != nil {
		return "", fmt.Errorf("textDocument/prepareCallHierarchy request failed: %w", err)
	}
	if !ok {
		return formatCallersOutput(rootLine, nil, 0, opts.Depth), nil
	}

	rootNode, err := hierarchyNodeForItem(workspaceRoot, matcher, rootItem)
	if err != nil {
		return "", err
	}

	seen := map[string]struct{}{
		rootNode.identity: {},
	}
	ancestors := map[string]struct{}{
		rootNode.identity: {},
	}
	outputLines := make([]callerOutputLine, 0)
	callersCount, err := e.walkIncoming(
		ctx,
		workspaceRoot,
		matcher,
		rootItem,
		opts.Depth,
		1,
		ancestors,
		seen,
		&outputLines,
	)
	if err != nil {
		return "", err
	}

	return formatCallersOutput(rootLine, outputLines, callersCount, opts.Depth), nil
}

func (e *CallersEngine) lookupSymbols(ctx context.Context, symbol string) ([]SymbolInformation, error) {
	var symbols []SymbolInformation
	if err := e.client.Call(
		ctx,
		MethodWorkspaceSymbol,
		WorkspaceSymbolParams{Query: symbol},
		&symbols,
	); err != nil {
		return nil, err
	}
	return symbols, nil
}

func (e *CallersEngine) prepareCallHierarchy(
	ctx context.Context,
	symbol SymbolInformation,
) (CallHierarchyItem, bool, error) {
	params := CallHierarchyPrepareParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: symbol.Location.URI},
			Position:     symbol.Location.Range.Start,
		},
	}

	var items []CallHierarchyItem
	if err := e.client.Call(ctx, MethodTextDocumentPrepareCallHierarchy, params, &items); err != nil {
		return CallHierarchyItem{}, false, err
	}
	if len(items) == 0 {
		return CallHierarchyItem{}, false, nil
	}

	sort.SliceStable(items, func(i, j int) bool {
		left := string(items[i].URI)
		right := string(items[j].URI)
		if left != right {
			return left < right
		}

		leftLine, leftChar := callHierarchyPosition(items[i])
		rightLine, rightChar := callHierarchyPosition(items[j])
		if leftLine != rightLine {
			return leftLine < rightLine
		}
		if leftChar != rightChar {
			return leftChar < rightChar
		}
		return items[i].Name < items[j].Name
	})

	return items[0], true, nil
}

func (e *CallersEngine) lookupIncomingCalls(
	ctx context.Context,
	item CallHierarchyItem,
) ([]CallHierarchyIncomingCall, error) {
	params := CallHierarchyIncomingCallsParams{Item: item}

	var incoming []CallHierarchyIncomingCall
	if err := e.client.Call(ctx, MethodCallHierarchyIncomingCalls, params, &incoming); err != nil {
		return nil, err
	}
	return incoming, nil
}

func (e *CallersEngine) walkIncoming(
	ctx context.Context,
	workspaceRoot string,
	matcher interface{ MatchesPath(string) bool },
	item CallHierarchyItem,
	maxDepth int,
	depth int,
	ancestors map[string]struct{},
	seen map[string]struct{},
	outputLines *[]callerOutputLine,
) (int, error) {
	if depth > maxDepth {
		return 0, nil
	}

	incoming, err := e.lookupIncomingCalls(ctx, item)
	if err != nil {
		return 0, fmt.Errorf("callHierarchy/incomingCalls request failed: %w", err)
	}

	nodes, err := incomingNodes(workspaceRoot, matcher, incoming)
	if err != nil {
		return 0, err
	}

	levelNodes := make([]hierarchyNode, 0, len(nodes))
	for _, node := range nodes {
		if _, cyclic := ancestors[node.identity]; cyclic {
			continue
		}
		if _, duplicate := seen[node.identity]; duplicate {
			continue
		}

		// Reserve all nodes at this depth before descending so the shallowest
		// occurrence wins when the same caller is discoverable transitively.
		seen[node.identity] = struct{}{}
		levelNodes = append(levelNodes, node)
	}

	count := 0
	for _, node := range levelNodes {
		*outputLines = append(*outputLines, callerOutputLine{
			depth: depth,
			name:  node.name,
			path:  node.path,
			line:  node.line,
		})
		count++

		if depth >= maxDepth {
			continue
		}

		ancestors[node.identity] = struct{}{}
		childCount, err := e.walkIncoming(
			ctx,
			workspaceRoot,
			matcher,
			node.item,
			maxDepth,
			depth+1,
			ancestors,
			seen,
			outputLines,
		)
		delete(ancestors, node.identity)
		if err != nil {
			return 0, err
		}

		count += childCount
	}

	return count, nil
}

func incomingNodes(
	workspaceRoot string,
	matcher interface{ MatchesPath(string) bool },
	incoming []CallHierarchyIncomingCall,
) ([]hierarchyNode, error) {
	nodes := make([]hierarchyNode, 0, len(incoming))
	seen := make(map[string]struct{}, len(incoming))

	for _, call := range incoming {
		node, err := hierarchyNodeForItem(workspaceRoot, matcher, call.From)
		if err != nil {
			return nil, err
		}
		if node.identity == "" {
			continue
		}
		if _, ok := seen[node.identity]; ok {
			continue
		}
		seen[node.identity] = struct{}{}
		nodes = append(nodes, node)
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].path != nodes[j].path {
			return nodes[i].path < nodes[j].path
		}
		if nodes[i].line != nodes[j].line {
			return nodes[i].line < nodes[j].line
		}
		if nodes[i].character != nodes[j].character {
			return nodes[i].character < nodes[j].character
		}
		return nodes[i].name < nodes[j].name
	})

	return nodes, nil
}

func hierarchyNodeForItem(
	workspaceRoot string,
	matcher interface{ MatchesPath(string) bool },
	item CallHierarchyItem,
) (hierarchyNode, error) {
	path, err := documentURIToPath(item.URI)
	if err != nil {
		return hierarchyNode{}, err
	}
	if matcher != nil && matcher.MatchesPath(path) {
		return hierarchyNode{}, nil
	}

	line, character := callHierarchyPosition(item)
	relative := relativePath(workspaceRoot, path)
	return hierarchyNode{
		item:      item,
		name:      item.Name,
		path:      relative,
		line:      line + 1,
		character: character,
		identity:  callHierarchyIdentity(relative, line, character, item.Name),
	}, nil
}

func callHierarchyPosition(item CallHierarchyItem) (int, int) {
	line := item.SelectionRange.Start.Line
	character := item.SelectionRange.Start.Character
	if line < 0 {
		line = item.Range.Start.Line
		character = item.Range.Start.Character
	}
	if line < 0 {
		line = 0
	}
	if character < 0 {
		character = 0
	}
	return line, character
}

func callHierarchyIdentity(path string, line int, character int, name string) string {
	return fmt.Sprintf("%s:%d:%d:%s", path, line, character, name)
}

func formatCallersOutput(
	rootLine string,
	callers []callerOutputLine,
	callersCount int,
	depth int,
) string {
	lines := make([]string, 0, len(callers)+2)
	lines = append(lines, rootLine)

	for _, caller := range callers {
		indent := strings.Repeat("  ", caller.depth)
		lines = append(lines, fmt.Sprintf("%s<- %s (%s:%d)", indent, caller.name, caller.path, caller.line))
	}

	lines = append(lines, fmt.Sprintf("%d callers (depth %d)", callersCount, depth))
	return strings.Join(lines, "\n")
}

func formatMissingCallersLSPError(binary string, install string) error {
	trimmedBinary := strings.TrimSpace(binary)
	if trimmedBinary == "" {
		trimmedBinary = "lsp"
	}

	trimmedInstall := strings.TrimSpace(install)
	if trimmedInstall == "" {
		trimmedInstall = "install language server"
	}

	return fmt.Errorf(
		"cs callers: LSP required but %s not found. Install: %s",
		trimmedBinary,
		trimmedInstall,
	)
}
