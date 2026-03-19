package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	configpkg "github.com/blankbytes/codesight/pkg/config"
	"github.com/blankbytes/codesight/pkg/lsp"
)

type lspDaemonConnector interface {
	Connect(ctx context.Context, workspaceRoot, language string) (lsp.DaemonConnection, error)
}

type lspClientConnectionMetadata struct {
	UsedDaemon        bool
	DaemonLeaseReused bool
	ConnectDuration   time.Duration
}

var (
	warmupProbeInterval = 2 * time.Second
	warmupProbeTimeout  = 90 * time.Second

	refsColdStartHintThreshold = 5 * time.Second

	lspRuntimeGOOS               = runtime.GOOS
	lspRuntimeNewDaemonConnector func(registry *lsp.Registry) lspDaemonConnector
	lspRuntimeLegacyStarter = startRefsLSPClient
)

type lspCommandRuntime struct {
	registry               *lsp.Registry
	goos                   string
	daemonConnectorFactory func(*lsp.Registry) lspDaemonConnector
	legacyStarter          func(context.Context, lsp.ServerSpec, string) (*lsp.Client, error)

	daemonConnector lspDaemonConnector
}

func newLSPCommandRuntime(registry *lsp.Registry) *lspCommandRuntime {
	if registry == nil {
		registry = lsp.NewRegistry()
	}

	return &lspCommandRuntime{
		registry:               registry,
		goos:                   strings.ToLower(strings.TrimSpace(lspRuntimeGOOS)),
		daemonConnectorFactory: lspRuntimeNewDaemonConnector,
		legacyStarter:          lspRuntimeLegacyStarter,
	}
}

func (r *lspCommandRuntime) connectClient(
	ctx context.Context,
	workspaceRoot string,
	spec lsp.ServerSpec,
) (*lsp.Client, func(), error) {
	client, release, _, err := r.connectClientWithMetadata(ctx, workspaceRoot, spec)
	return client, release, err
}

func (r *lspCommandRuntime) connectClientWithMetadata(
	ctx context.Context,
	workspaceRoot string,
	spec lsp.ServerSpec,
) (*lsp.Client, func(), lspClientConnectionMetadata, error) {
	started := time.Now()
	if r == nil {
		return nil, nil, lspClientConnectionMetadata{}, errors.New("lsp command runtime is nil")
	}
	if strings.TrimSpace(r.goos) == "" {
		r.goos = runtime.GOOS
	}
	if r.legacyStarter == nil {
		return nil, nil, lspClientConnectionMetadata{}, errors.New("legacy lsp starter is nil")
	}

	if strings.EqualFold(r.goos, "windows") {
		client, err := r.legacyStarter(ctx, spec, workspaceRoot)
		if err != nil {
			return nil, nil, lspClientConnectionMetadata{}, err
		}
		release := func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), refsLSPShutdownTimeout)
			defer cancel()
			_ = client.Shutdown(shutdownCtx)
		}
		return client, release, lspClientConnectionMetadata{
			UsedDaemon:        false,
			DaemonLeaseReused: false,
			ConnectDuration:   time.Since(started),
		}, nil
	}

	connector := r.daemonConnector
	if connector == nil {
		if r.daemonConnectorFactory == nil {
			return nil, nil, lspClientConnectionMetadata{}, errors.New("daemon connector factory is nil")
		}
		connector = r.daemonConnectorFactory(r.registry)
		if connector == nil {
			return nil, nil, lspClientConnectionMetadata{}, errors.New("daemon connector is nil")
		}
		r.daemonConnector = connector
	}

	connection, err := connector.Connect(ctx, workspaceRoot, spec.Language)
	if err != nil {
		return nil, nil, lspClientConnectionMetadata{}, err
	}
	if connection.Client == nil {
		return nil, nil, lspClientConnectionMetadata{}, errors.New("daemon connector returned nil client")
	}

	release := func() {
		_ = connection.Client.Close()
	}
	return connection.Client, release, lspClientConnectionMetadata{
		UsedDaemon:        true,
		DaemonLeaseReused: connection.Lease.Reused,
		ConnectDuration:   time.Since(started),
	}, nil
}

func executeRefsCommand(ctx context.Context, opts refsCommandOptions) (string, error) {
	registry := lsp.NewRegistry()
	runtime := newLSPCommandRuntime(registry)

	fallbackBinary := ""
	var client *lsp.Client
	var release func()
	var emitColdStartHint bool

	// Always use the discovered project root for language detection and daemon connection.
	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		if !errors.Is(err, errNoSupportedRefsLanguage) {
			return "", err
		}
		// No supported language detected — will use grep fallback below.
	} else {
		spec, lookupErr := registry.Lookup(language)
		if lookupErr == nil {
			fallbackBinary = spec.Binary

			// Attempt daemon connection (linux/macOS) or legacy startup
			// (Windows). Refs must fall back to grep on any LSP startup error.
			connectedClient, closeClient, metadata, connectErr := runtime.connectClientWithMetadata(ctx, opts.WorkspaceRoot, spec)
			if connectErr == nil {
				client = connectedClient
				release = closeClient
				emitColdStartHint = shouldEmitRefsColdStartHint(language, metadata)
			}
		}
	}
	if release != nil {
		defer release()
	}

	engine := lsp.NewRefsEngine(nil, nil)
	if client != nil {
		engine = lsp.NewRefsEngine(client, nil)
	}
	output, err := engine.Find(ctx, lsp.RefsOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		FilterPath:    opts.FilterPath,
		Symbol:        opts.Symbol,
		Kind:          opts.Kind,
		FallbackLSP:   fallbackBinary,
	})
	if err != nil {
		return "", err
	}
	if emitColdStartHint {
		output = appendRefsColdStartHint(output)
	}
	return output, nil
}

func executeCallersCommand(ctx context.Context, opts callersCommandOptions) (string, error) {
	registry := lsp.NewRegistry()
	runtime := newLSPCommandRuntime(registry)

	// Always use the discovered project root for language detection and daemon connection.
	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		return "", err
	}

	spec, err := registry.Lookup(language)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(spec.Binary); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", callersMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}

	client, release, err := runtime.connectClient(ctx, opts.WorkspaceRoot, spec)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", callersMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}
	defer release()

	return lsp.NewCallersEngine(client).Find(ctx, lsp.CallersOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		FilterPath:    opts.FilterPath,
		Symbol:        opts.Symbol,
		Depth:         opts.Depth,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
}

func executeImplementsCommand(ctx context.Context, opts implementsCommandOptions) (string, error) {
	registry := lsp.NewRegistry()
	runtime := newLSPCommandRuntime(registry)

	// Always use the discovered project root for language detection and daemon connection.
	language, err := detectRefsLanguage(opts.WorkspaceRoot, registry)
	if err != nil {
		return "", err
	}

	spec, err := registry.Lookup(language)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(spec.Binary); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", implementsMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}

	client, release, err := runtime.connectClient(ctx, opts.WorkspaceRoot, spec)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", implementsMissingLSPError(ctx, opts, spec)
		}
		return "", err
	}
	defer release()

	return lsp.NewImplementsEngine(client).Find(ctx, lsp.ImplementsOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		FilterPath:    opts.FilterPath,
		Symbol:        opts.Symbol,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
}

func callersMissingLSPError(ctx context.Context, opts callersCommandOptions, spec lsp.ServerSpec) error {
	_, err := lsp.NewCallersEngine(nil).Find(ctx, lsp.CallersOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Symbol:        opts.Symbol,
		Depth:         opts.Depth,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
	return err
}

func implementsMissingLSPError(ctx context.Context, opts implementsCommandOptions, spec lsp.ServerSpec) error {
	_, err := lsp.NewImplementsEngine(nil).Find(ctx, lsp.ImplementsOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Symbol:        opts.Symbol,
		LSPBinary:     spec.Binary,
		LSPInstall:    spec.InstallHint,
	})
	return err
}

func shouldEmitRefsColdStartHint(language string, metadata lspClientConnectionMetadata) bool {
	if !metadata.UsedDaemon || metadata.DaemonLeaseReused {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(language), "java") {
		return false
	}
	return metadata.ConnectDuration > refsColdStartHintThreshold
}

func appendRefsColdStartHint(output string) string {
	const tip = "Tip: run 'cs lsp warmup .' to pre-start the language server"
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return tip
	}
	return trimmed + "\n" + tip
}

func executeLSPWarmup(ctx context.Context, workspaceRoot, language string, cfg *configpkg.Config) error {
	registry := lsp.NewRegistry()
	return executeLSPWarmupWithRegistry(ctx, registry, workspaceRoot, language, cfg)
}

func executeLSPWarmupWithRegistry(
	ctx context.Context,
	registry *lsp.Registry,
	workspaceRoot string,
	language string,
	cfg *configpkg.Config,
) error {
	if registry == nil {
		registry = lsp.NewRegistry()
	}

	spec, err := registry.Lookup(language)
	if err != nil {
		return err
	}

	runtime := newLSPCommandRuntime(registry)
	client, release, _, err := runtime.connectClientWithMetadata(ctx, workspaceRoot, spec)
	if err != nil {
		return err
	}
	defer release()

	// Readiness probe: poll workspace/symbol until the LSP returns symbols,
	// indicating it has finished indexing.
	probeTimeout := resolvedWarmupProbeTimeout(cfg)
	ticker := time.NewTicker(warmupProbeInterval)
	defer ticker.Stop()
	deadline := time.After(probeTimeout)

	for attempt := 0; ; attempt++ {
		var symbols []lsp.SymbolInformation
		if probeErr := client.Call(ctx, lsp.MethodWorkspaceSymbol,
			lsp.WorkspaceSymbolParams{Query: "*"}, &symbols); probeErr != nil {
			return fmt.Errorf("LSP readiness probe failed: %w", probeErr)
		}
		if len(symbols) > 0 {
			return nil
		}

		// First attempt returned nothing — wait and retry.
		select {
		case <-ctx.Done():
			return fmt.Errorf("warmup cancelled while waiting for LSP to index: %w", ctx.Err())
		case <-deadline:
			return fmt.Errorf("LSP connected but returned no symbols after %s — the language server may not have finished indexing", probeTimeout)
		case <-ticker.C:
			// retry
		}
	}
}

func resolvedWarmupProbeTimeout(cfg *configpkg.Config) time.Duration {
	if cfg != nil {
		if d, err := cfg.LSPWarmupProbeTimeoutDuration(); err == nil && d > 0 {
			return d
		}
	}
	return warmupProbeTimeout
}

func resolvedLSPDaemonIdleTimeout(cfg *configpkg.Config) time.Duration {
	if cfg == nil {
		return lsp.DefaultIdleTimeout
	}

	timeout, err := cfg.LSPDaemonIdleTimeoutDuration()
	if err != nil {
		return lsp.DefaultIdleTimeout
	}
	return timeout
}
