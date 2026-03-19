package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	configpkg "github.com/blankbytes/codesight/pkg/config"
	"github.com/blankbytes/codesight/pkg/lsp"
	"github.com/spf13/cobra"
)

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Manage LSP daemons",
}

var lspWarmupCmd = &cobra.Command{
	Use:   "warmup [path]",
	Short: "Pre-start the language server for a workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runLSPWarmup,
}

var lspStatusCmd = &cobra.Command{
	Use:   "status [path]",
	Short: "Show LSP daemon status",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runLSPStatus,
}

var lspRestartCmd = &cobra.Command{
	Use:   "restart [path]",
	Short: "Restart the LSP daemon",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runLSPRestart,
}

func init() {
	lspCmd.AddCommand(lspWarmupCmd, lspStatusCmd, lspRestartCmd)
}

var runWarmupCommand = executeWarmupCommand

type warmupCommandOptions struct {
	WorkspaceRoot string
	Status        bool
	Restart       bool
}

type warmupCommandResult struct {
	WorkspaceRoot string
	Language      string
	Supported     bool
	Statuses      []lsp.DaemonStatus
	Restarted     bool
}

func resolveLSPSubcommandWorkspace(args []string) (string, error) {
	path := ""
	if len(args) > 0 {
		path = args[0]
	}
	return resolveRefsWorkspaceRoot(path)
}

func runLSPWarmup(cmd *cobra.Command, args []string) error {
	workspaceRoot, err := resolveLSPSubcommandWorkspace(args)
	if err != nil {
		return err
	}
	workspaceRoot = resolvedProjectRoot()

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

func runLSPStatus(cmd *cobra.Command, args []string) error {
	workspaceRoot, err := resolveLSPSubcommandWorkspace(args)
	if err != nil {
		return err
	}
	workspaceRoot = resolvedProjectRoot()

	result, err := runWarmupCommand(cmd.Context(), warmupCommandOptions{
		WorkspaceRoot: workspaceRoot,
		Status:        true,
	})
	if err != nil {
		return err
	}

	if result.Statuses != nil {
		if len(result.Statuses) == 0 {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "No LSP daemon found for %s\n", workspaceRoot)
			return err
		}
		for _, s := range result.Statuses {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "LSP daemon (%s): %s\n", s.Language, s.WorkspaceRoot)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  PID:       %d (%s)\n", s.PID, formatDaemonHealth(s))
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Binary:    %s\n", s.Binary)
			if !s.StartedAt.IsZero() {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Uptime:    %s\n", formatDuration(time.Since(s.StartedAt)))
			}
			if !s.LastUsedAt.IsZero() {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Last used: %s ago\n", formatDuration(time.Since(s.LastUsedAt)))
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Log:       %s\n", shortenHome(s.LogPath))
		}
	}
	return nil
}

func runLSPRestart(cmd *cobra.Command, args []string) error {
	workspaceRoot, err := resolveLSPSubcommandWorkspace(args)
	if err != nil {
		return err
	}
	workspaceRoot = resolvedProjectRoot()

	result, err := runWarmupCommand(cmd.Context(), warmupCommandOptions{
		WorkspaceRoot: workspaceRoot,
		Restart:       true,
	})
	if err != nil {
		return err
	}

	if result.Restarted {
		_, err := fmt.Fprintf(
			cmd.OutOrStdout(),
			"LSP daemon restarted (%s): %s\n",
			result.Language,
			result.WorkspaceRoot,
		)
		return err
	}

	if !result.Supported {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No supported LSP language detected")
		return err
	}

	return nil
}

func formatDaemonHealth(s lsp.DaemonStatus) string {
	if !s.Running {
		return "not running"
	}
	if s.SocketHealthy {
		return "running, socket healthy"
	}
	return "running, socket unhealthy"
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && len(rel) < len(path) {
		return "~/" + rel
	}
	return path
}

func executeWarmupCommand(ctx context.Context, opts warmupCommandOptions) (warmupCommandResult, error) {
	registry := lsp.NewRegistry()

	if opts.Status {
		lifecycle := lsp.NewLifecycle(registry)
		statuses, err := lifecycle.Status(ctx, opts.WorkspaceRoot)
		if err != nil {
			return warmupCommandResult{}, err
		}
		if statuses == nil {
			statuses = []lsp.DaemonStatus{}
		}
		return warmupCommandResult{
			WorkspaceRoot: opts.WorkspaceRoot,
			Statuses:      statuses,
		}, nil
	}

	if opts.Restart {
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

		lifecycle := lsp.NewLifecycle(registry)
		statuses, err := lifecycle.Status(ctx, opts.WorkspaceRoot)
		if err != nil {
			return warmupCommandResult{}, err
		}
		for _, s := range statuses {
			_ = lifecycle.StopByKey(s.StateKey)
		}

		// Start daemon through the connector so it sends initialize with
		// proper workspace folders, Gradle config, and JDK settings.
		spec, err := registry.Lookup(language)
		if err != nil {
			return warmupCommandResult{}, err
		}
		runtime := newLSPCommandRuntime(registry)
		_, release, _, err := runtime.connectClientWithMetadata(ctx, opts.WorkspaceRoot, spec)
		if err != nil {
			return warmupCommandResult{}, err
		}
		release()

		return warmupCommandResult{
			WorkspaceRoot: opts.WorkspaceRoot,
			Language:      language,
			Supported:     true,
			Restarted:     true,
		}, nil
	}

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

	cfg, _ := configpkg.LoadConfig(opts.WorkspaceRoot)

	if err := executeLSPWarmupWithRegistry(ctx, registry, opts.WorkspaceRoot, language, cfg); err != nil {
		return warmupCommandResult{}, err
	}

	return warmupCommandResult{
		WorkspaceRoot: opts.WorkspaceRoot,
		Language:      language,
		Supported:     true,
	}, nil
}
