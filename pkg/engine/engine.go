package engine

import (
	"context"
	"os"
	"strings"
	"sync"
)

// RefsProvider fetches structured references for a symbol.
type RefsProvider interface {
	FindRefs(ctx context.Context, workspaceRoot, filterPath, symbol, kind string) ([]SymReference, string, error)
}

// CallersProvider fetches structured callers for a symbol.
type CallersProvider interface {
	FindCallers(ctx context.Context, workspaceRoot, filterPath, symbol string, depth int) ([]SymCaller, error)
}

// ImplProvider fetches structured implementations for a symbol.
type ImplProvider interface {
	FindImplementations(ctx context.Context, workspaceRoot, filterPath, symbol string) ([]SymImpl, error)
}

// ExtractProvider resolves a symbol definition.
type ExtractProvider interface {
	Extract(workspaceRoot, symbol string) (*SymDefinition, error)
}

// Engine orchestrates a unified query across extract, refs, callers, and implements.
type Engine struct {
	WorkspaceRoot string
	Refs          RefsProvider
	Callers       CallersProvider
	Implements    ImplProvider
	Extractor     ExtractProvider
}

// QueryOptions configures a single unified query.
type QueryOptions struct {
	Query  string
	Path   string // --path scope
	Depth  int    // --depth
	Budget string // auto|small|medium|large
	Mode   string // auto|symbol|text|ast|path
}

// Query performs a unified retrieval, routing internally based on query classification.
func (e *Engine) Query(ctx context.Context, opts QueryOptions) (*SymbolIntelligence, error) {
	intent := Classify(opts.Query, opts.Mode)
	budget := ComputeBudget(opts.Budget, opts.Depth)
	depth := opts.Depth
	if depth <= 0 {
		depth = 1
	}

	filterPath := opts.Path

	result := &SymbolIntelligence{
		Query: opts.Query,
		Mode:  string(intent),
		Meta: SymMeta{
			Mode:        string(intent),
			SearchChain: []string{string(intent)},
			Budget:      budget,
		},
	}

	switch intent {
	case IntentSymbol:
		e.querySymbol(ctx, result, filterPath, depth, budget)
	case IntentPath:
		e.queryPath(result)
	case IntentText:
		e.queryText(ctx, result, filterPath, budget)
	case IntentAST:
		result.Status = "not_found"
		result.Meta.NextHint = "AST search is not yet implemented; try --mode symbol or --mode text"
	}

	return result, nil
}

func (e *Engine) querySymbol(ctx context.Context, result *SymbolIntelligence, filterPath string, depth int, budget SymBudget) {
	symbol := result.Query
	result.Symbol = symbol

	// Step 1: Extract definition.
	var def *SymDefinition
	if e.Extractor != nil {
		d, err := e.Extractor.Extract(e.WorkspaceRoot, symbol)
		if err == nil && d != nil {
			def = d
		}
	}

	if def == nil {
		// Degrade: symbol not found, try text fallback.
		result.Meta.SearchChain = append(result.Meta.SearchChain, "text")
		e.queryText(ctx, result, filterPath, budget)
		if result.Status == "" {
			result.Status = "not_found_exact"
			result.Meta.NextHint = "retry with --path, or provide a fuller query"
		}
		return
	}

	result.Status = "ok"
	result.Meta.Confidence = 0.85

	// Apply slicing if definition exceeds budget.
	if def.Body != "" {
		bodyLines := strings.Count(def.Body, "\n") + 1
		if bodyLines > budget.DefinitionLines {
			source, err := os.ReadFile(def.File)
			if err == nil {
				sig, slices, strategy := SliceDefinition(source, def.Language, def.Line, def.EndLine, budget.DefinitionLines)
				def.Signature = sig
				def.Slices = slices
				def.ViewStrategy = strategy
				if strategy == "signature_plus_slices" {
					def.Body = "" // body is represented via slices
				}
			}
		} else {
			def.ViewStrategy = "full_body"
		}
	}
	result.Definition = def

	// Step 2: Parallel fetch of refs, callers, implements.
	type refsResult struct {
		refs   []SymReference
		source string
		err    error
	}
	type callersResult struct {
		callers []SymCaller
		err     error
	}
	type implResult struct {
		impls []SymImpl
		err   error
	}

	var wg sync.WaitGroup
	var rr refsResult
	var cr callersResult
	var ir implResult

	if e.Refs != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr.refs, rr.source, rr.err = e.Refs.FindRefs(ctx, e.WorkspaceRoot, filterPath, symbol, "")
		}()
	}

	if e.Callers != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cr.callers, cr.err = e.Callers.FindCallers(ctx, e.WorkspaceRoot, filterPath, symbol, depth)
		}()
	}

	if e.Implements != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ir.impls, ir.err = e.Implements.FindImplementations(ctx, e.WorkspaceRoot, filterPath, symbol)
		}()
	}

	wg.Wait()

	defFile := ""
	if def != nil {
		defFile = def.File
	}

	// Process refs.
	if rr.err != nil {
		result.Meta.Errors = append(result.Meta.Errors, "refs: "+rr.err.Error())
	} else {
		maxRefItems := budget.ReferenceLines / 2 // ~2 lines per ref entry
		if maxRefItems < 1 {
			maxRefItems = 1
		}
		result.Meta.RefsTotal = len(rr.refs)
		result.References = ScoreReferences(rr.refs, defFile, maxRefItems)
		result.Meta.RefsShown = len(result.References)
		result.Meta.RefsSource = rr.source
	}

	// Process callers.
	if cr.err != nil {
		result.Meta.Errors = append(result.Meta.Errors, "callers: "+cr.err.Error())
	} else {
		maxCallerItems := budget.CallerLines / 2
		if maxCallerItems < 1 {
			maxCallerItems = 1
		}
		result.Callers = ScoreCallers(cr.callers, defFile, maxCallerItems)
	}

	// Process implements.
	if ir.err != nil {
		result.Meta.Errors = append(result.Meta.Errors, "implements: "+ir.err.Error())
	} else {
		result.Implementations = ir.impls
	}
}

func (e *Engine) queryPath(result *SymbolIntelligence) {
	query := result.Query

	info, err := os.Stat(query)
	if err != nil {
		result.Status = "not_found"
		result.Meta.NextHint = "file or directory not found: " + query
		return
	}

	if info.IsDir() {
		result.Status = "ok"
		result.Meta.NextHint = "directory listing; use cs query with a symbol name for deeper inspection"
		return
	}

	// Check file size to guard against context flooding.
	if info.Size() > 500*1024 { // 500KB
		result.Status = "file_too_large"
		result.Meta.NextHint = "file is too large; use grep or tail to inspect specific sections"
		return
	}

	lineCount := countFileLines(query)
	if lineCount > 500 {
		result.Status = "file_too_large"
		result.Meta.NextHint = "file is " + strings.TrimSpace(string(rune(lineCount))) + " lines; use grep or tail"
		return
	}

	result.Status = "ok"
}

func (e *Engine) queryText(ctx context.Context, result *SymbolIntelligence, filterPath string, budget SymBudget) {
	if e.Refs == nil {
		return
	}

	// Use refs provider as a text search (grep fallback will handle it).
	refs, source, err := e.Refs.FindRefs(ctx, e.WorkspaceRoot, filterPath, result.Query, "")
	if err != nil {
		result.Meta.Errors = append(result.Meta.Errors, "text search: "+err.Error())
		return
	}

	if len(refs) > 0 {
		result.Status = "ok"
		maxItems := budget.ReferenceLines / 2
		if maxItems < 1 {
			maxItems = 1
		}
		result.Meta.RefsTotal = len(refs)
		result.References = refs
		if len(result.References) > maxItems {
			result.References = result.References[:maxItems]
		}
		result.Meta.RefsShown = len(result.References)
		result.Meta.RefsSource = source

		// Build candidates from top references.
		for _, ref := range result.References {
			result.Ambiguous = append(result.Ambiguous, SymCandidate{
				Name:   result.Query,
				Type:   "text match",
				File:   ref.File,
				Reason: "text search",
			})
		}
	}
}

func countFileLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n") + 1
}
