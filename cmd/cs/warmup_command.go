package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/blankbytes/codesight/pkg/lsp"
	"github.com/spf13/cobra"
)

var warmupCmd = &cobra.Command{
	Use:   "warmup [path]",
	Short: "Pre-start the language server for a workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWarmup,
}

var runWarmupCommand = executeWarmupCommand

type warmupCommandOptions struct {
	WorkspaceRoot string
}

type warmupCommandResult struct {
	WorkspaceRoot string
	Language      string
	Supported     bool
}

func runWarmup(cmd *cobra.Command, args []string) error {
	path := ""
	if len(args) > 0 {
		path = args[0]
	}

	workspaceRoot, err := resolveRefsWorkspaceRoot(path)
	if err != nil {
		return err
	}

	result, err := runWarmupCommand(cmd.Context(), warmupCommandOptions{
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		return err
	}

	if !result.Supported {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No supported LSP language detected for warmup")
		return err
	}

	_, err = fmt.Fprintf(
		cmd.OutOrStdout(),
		"LSP warmup ready (%s): %s\n",
		result.Language,
		result.WorkspaceRoot,
	)
	return err
}

func executeWarmupCommand(ctx context.Context, opts warmupCommandOptions) (warmupCommandResult, error) {
	registry := lsp.NewRegistry()

	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		if errors.Is(err, errNoSupportedRefsLanguage) {
			return warmupCommandResult{
				WorkspaceRoot: opts.WorkspaceRoot,
				Supported:     false,
			}, nil
		}
		return warmupCommandResult{}, err
	}

	if err := executeLSPWarmupWithRegistry(ctx, registry, opts.WorkspaceRoot, language); err != nil {
		return warmupCommandResult{}, err
	}

	return warmupCommandResult{
		WorkspaceRoot: opts.WorkspaceRoot,
		Language:      language,
		Supported:     true,
	}, nil
}
