package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	pkg "github.com/blankbytes/codesight/pkg"
	"github.com/blankbytes/codesight/pkg/engine"
	"github.com/blankbytes/codesight/pkg/lsp"
	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Unified code intelligence retrieval",
	Long:  "Single-call retrieval engine that routes, ranks, and budgets code intelligence from symbol extraction, references, callers, and implementations.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runQuery,
}

var runQueryCommand = executeQueryCommand

func runQuery(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	pathFlag, _ := cmd.Flags().GetString("path")
	depthFlag, _ := cmd.Flags().GetInt("depth")
	budgetFlag, _ := cmd.Flags().GetString("budget")
	modeFlag, _ := cmd.Flags().GetString("mode")

	output, err := runQueryCommand(cmd.Context(), queryCommandOptions{
		Query:  query,
		Path:   pathFlag,
		Depth:  depthFlag,
		Budget: budgetFlag,
		Mode:   modeFlag,
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(cmd.OutOrStdout(), output)
	return err
}

type queryCommandOptions struct {
	Query  string
	Path   string
	Depth  int
	Budget string
	Mode   string
}

func executeQueryCommand(ctx context.Context, opts queryCommandOptions) (string, error) {
	targetDir := currentTargetDir()
	workspaceRoot := resolvedProjectRootForTarget(targetDir)

	registry := lsp.NewRegistry()
	runtime := newLSPCommandRuntime(registry)

	// Set up LSP client (best-effort — engine degrades gracefully without it).
	var client *lsp.Client
	var release func()

	language, err := detectRefsLanguage(workspaceRoot, registry)
	if err == nil {
		spec, lookupErr := registry.Lookup(language)
		if lookupErr == nil {
			if _, pathErr := exec.LookPath(spec.Binary); pathErr == nil {
				c, r, connectErr := runtime.connectClient(ctx, workspaceRoot, spec)
				if connectErr == nil {
					client = c
					release = r
				}
			}
		}
	}
	if release != nil {
		defer release()
	}

	// Build providers.
	var refsProvider engine.RefsProvider
	var callersProvider engine.CallersProvider
	var implProvider engine.ImplProvider

	refsEngine := lsp.NewRefsEngine(client, nil)
	refsProvider = &engine.LSPRefsAdapter{Engine: refsEngine}

	if client != nil {
		callersEngine := lsp.NewCallersEngine(client)
		callersProvider = &engine.LSPCallersAdapter{Engine: callersEngine}

		implEngine := lsp.NewImplementsEngine(client)
		implProvider = &engine.LSPImplAdapter{Engine: implEngine}
	}

	// Best-effort semantic search — skip if infrastructure unavailable.
	var searchProvider engine.SearchProvider
	cfg := currentConfig()
	store, storeErr := newStore(cfg)
	if storeErr == nil {
		connectCtx, connectCancel := context.WithTimeout(ctx, 2*time.Second)
		if connectErr := store.Connect(connectCtx); connectErr == nil {
			searchProvider = &engine.SemanticSearchAdapter{
				Searcher: &pkg.Searcher{
					Store:    store,
					Embedder: newEmbedder(cfg),
				},
				CollectionName: cfg.DB.CollectionName,
			}
			defer store.Close()
		}
		connectCancel()
	}

	depth := opts.Depth
	if depth <= 0 {
		depth = 1
	}

	eng := &engine.Engine{
		WorkspaceRoot: workspaceRoot,
		Refs:          refsProvider,
		Callers:       callersProvider,
		Implements:    implProvider,
		Extractor:     &engine.TreeSitterExtractAdapter{},
		Search:        searchProvider,
	}

	result, err := eng.Query(ctx, engine.QueryOptions{
		Query:  opts.Query,
		Path:   opts.Path,
		Depth:  depth,
		Budget: opts.Budget,
		Mode:   opts.Mode,
	})
	if err != nil {
		return "", err
	}

	return engine.RenderMarkdown(result, flagVerbose), nil
}
