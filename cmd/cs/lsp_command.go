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

var lspCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up orphaned LSP daemon artifacts",
	Args:  cobra.NoArgs,
	RunE:  runLSPCleanup,
}

func init() {
	lspCmd.AddCommand(lspWarmupCmd, lspStatusCmd, lspRestartCmd, lspCleanupCmd)
}

var runLSPCommand = executeLSPCommand

type lspCommandOptions struct {
	WorkspaceRoot string
	Status        bool
	Restart       bool
}

type lspCommandResult struct {
	WorkspaceRoot string
	Language      string
	Supported     bool
	Statuses      []lsp.DaemonStatus
	Restarted     bool
}

func runLSPWarmup(cmd *cobra.Command, args []string) error {
	workspaceRoot := resolvedProjectRootForTarget(currentTargetDir())

	result, err := runLSPCommand(cmd.Context(), lspCommandOptions{
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
	workspaceRoot := resolvedProjectRootForTarget(currentTargetDir())

	result, err := runLSPCommand(cmd.Context(), lspCommandOptions{
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
	workspaceRoot := resolvedProjectRootForTarget(currentTargetDir())

	result, err := runLSPCommand(cmd.Context(), lspCommandOptions{
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

func runLSPCleanup(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	l := lsp.NewLifecycle(lsp.NewRegistry(), lsp.WithIdleTimeout(0)) // timeout not needed for cleanup

	cleaned, err := l.Cleanup(ctx)
	if err != nil {
		return err
	}

	if len(cleaned) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No orphaned LSP artifacts found")
		return err
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Cleaned up artifacts for %d orphaned daemons:\n", len(cleaned))
	if err != nil {
		return err
	}

	for _, c := range cleaned {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", c)
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

func executeLSPCommand(ctx context.Context, opts lspCommandOptions) (lspCommandResult, error) {
	registry := lsp.NewRegistry()

	if opts.Status {
		lifecycle := lsp.NewLifecycle(registry)
		statuses, err := lifecycle.Status(ctx, opts.WorkspaceRoot)
		if err != nil {
			return lspCommandResult{}, err
		}
		if statuses == nil {
			statuses = []lsp.DaemonStatus{}
		}
		return lspCommandResult{
			WorkspaceRoot: opts.WorkspaceRoot,
			Statuses:      statuses,
		}, nil
	}

	if opts.Restart {
		language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
		if err != nil {
			if errors.Is(err, errNoSupportedRefsLanguage) {
				return lspCommandResult{
					WorkspaceRoot: opts.WorkspaceRoot,
					Supported:     false,
				}, nil
			}
			return lspCommandResult{}, err
		}

		lifecycle := lsp.NewLifecycle(registry)
		statuses, err := lifecycle.Status(ctx, opts.WorkspaceRoot)
		if err != nil {
			return lspCommandResult{}, err
		}
		for _, s := range statuses {
			_ = lifecycle.StopByKey(s.StateKey)
		}

		// Start daemon through the connector so it sends initialize with
		// proper workspace folders, Gradle config, and JDK settings.
		spec, err := registry.Lookup(language)
		if err != nil {
			return lspCommandResult{}, err
		}
		runtime := newLSPCommandRuntime(registry)
		_, release, _, err := runtime.connectClientWithMetadata(ctx, opts.WorkspaceRoot, spec)
		if err != nil {
			return lspCommandResult{}, err
		}
		release()

		return lspCommandResult{
			WorkspaceRoot: opts.WorkspaceRoot,
			Language:      language,
			Supported:     true,
			Restarted:     true,
		}, nil
	}

	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		if errors.Is(err, errNoSupportedRefsLanguage) {
			return lspCommandResult{
				WorkspaceRoot: opts.WorkspaceRoot,
				Supported:     false,
			}, nil
		}
		return lspCommandResult{}, err
	}

	cfg, _ := configpkg.LoadConfig(opts.WorkspaceRoot)

	if err := executeLSPWarmupWithRegistry(ctx, registry, opts.WorkspaceRoot, language, cfg); err != nil {
		return lspCommandResult{}, err
	}

	return lspCommandResult{
		WorkspaceRoot: opts.WorkspaceRoot,
		Language:      language,
		Supported:     true,
	}, nil
}
