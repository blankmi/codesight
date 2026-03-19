package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	configpkg "github.com/blankbytes/codesight/pkg/config"
	"github.com/spf13/cobra"
)

const defaultConfigSource = "default"

var configCmd = &cobra.Command{
	Use:   "config [path]",
	Short: "Show effective configuration values and their sources",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runConfig,
}

type configDisplayEntry struct {
	Key    string
	Value  string
	Source string
}

func runConfig(cmd *cobra.Command, args []string) error {
	for _, entry := range buildConfigDisplayEntries(currentConfig()) {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s = %s (%s)\n", entry.Key, entry.Value, entry.Source); err != nil {
			return err
		}
	}
	return nil
}

func buildConfigDisplayEntries(cfg *configpkg.Config) []configDisplayEntry {
	if cfg == nil {
		cfg = configpkg.Defaults()
	}

	projectRootValue := cfg.ProjectRoot
	if cfg.ConfigDir != "" {
		if resolved, err := cfg.ResolvedProjectRoot(cfg.ConfigDir); err == nil {
			projectRootValue = resolved
		}
	}

	entries := []configDisplayEntry{
		{Key: "db.address", Value: cfg.DB.Address, Source: configValueSource(cfg, "db.address")},
		{Key: "db.token", Value: cfg.DB.Token, Source: configValueSource(cfg, "db.token")},
		{Key: "db.type", Value: cfg.DB.Type, Source: configValueSource(cfg, "db.type")},
		{Key: "embedding.max_input_chars", Value: strconv.Itoa(cfg.Embedding.MaxInputChars), Source: configValueSource(cfg, "embedding.max_input_chars")},
		{Key: "embedding.model", Value: cfg.Embedding.Model, Source: configValueSource(cfg, "embedding.model")},
		{Key: "embedding.ollama_host", Value: cfg.Embedding.OllamaHost, Source: configValueSource(cfg, "embedding.ollama_host")},
		{Key: "index.warm_lsp", Value: strconv.FormatBool(cfg.Index.WarmLSP), Source: configValueSource(cfg, "index.warm_lsp")},
		{Key: "lsp.daemon.idle_timeout", Value: cfg.LSP.Daemon.IdleTimeout, Source: configValueSource(cfg, "lsp.daemon.idle_timeout")},
		{Key: "lsp.go.build_flags", Value: strings.Join(cfg.LSP.Go.BuildFlags, ","), Source: configValueSource(cfg, "lsp.go.build_flags")},
		{Key: "lsp.java.args", Value: strings.Join(cfg.LSP.Java.Args, ","), Source: configValueSource(cfg, "lsp.java.args")},
		{Key: "lsp.java.gradle_java_home", Value: cfg.LSP.Java.GradleJavaHome, Source: configValueSource(cfg, "lsp.java.gradle_java_home")},
		{Key: "lsp.java.timeout", Value: cfg.LSP.Java.Timeout, Source: configValueSource(cfg, "lsp.java.timeout")},
		{Key: "project_root", Value: projectRootValue, Source: configValueSource(cfg, "project_root")},
		{Key: "state_dir", Value: cfg.StateDir, Source: configValueSource(cfg, "state_dir")},
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	return entries
}

func configValueSource(cfg *configpkg.Config, key string) string {
	if cfg == nil || cfg.Provenance == nil {
		return defaultConfigSource
	}
	source := cfg.Provenance[key]
	if source == "" {
		return defaultConfigSource
	}
	return source
}
