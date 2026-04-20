package main

import (
	"fmt"
	"os"

	extractpkg "codesight/pkg/extract"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list -f <file>",
	Short: "List symbols in a file or directory",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func init() {
	listCmd.Flags().StringP("file", "f", "", "file or directory path")
	listCmd.Flags().StringP("lang", "l", "", "language filter (optional)")
	listCmd.Flags().String("format", "raw", "output format (raw|json)")
	listCmd.Flags().StringP("type", "t", "", "symbol type filter (optional)")
	listCmd.Flags().Bool("summary", false, "summarize symbols per file (directory targets only)")

	if err := listCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
}

func runList(cmd *cobra.Command, args []string) error {
	targetPath, err := cmd.Flags().GetString("file")
	if err != nil {
		return err
	}
	language, err := cmd.Flags().GetString("lang")
	if err != nil {
		return err
	}
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	symbolType, err := cmd.Flags().GetString("type")
	if err != nil {
		return err
	}
	summary, err := cmd.Flags().GetBool("summary")
	if err != nil {
		return err
	}

	if resolved, err := resolveProjectPath(targetPath); err == nil {
		targetPath = resolved
	}

	var result extractpkg.ListResult
	if summary {
		info, err := os.Stat(targetPath)
		if err != nil {
			return fmt.Errorf("stat target: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("--summary requires --file to point to a directory")
		}

		result, err = extractpkg.ListSymbolsSummary(targetPath, language, format, symbolType)
		if err != nil {
			return err
		}
	} else {
		result, err = extractpkg.ListSymbols(targetPath, language, format, symbolType)
		if err != nil {
			return err
		}
	}

	if result.Output != "" {
		if _, err := fmt.Fprint(cmd.OutOrStdout(), result.Output); err != nil {
			return err
		}
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintln(cmd.ErrOrStderr(), warning); err != nil {
			return err
		}
	}

	return nil
}
