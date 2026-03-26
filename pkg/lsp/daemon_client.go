package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultDaemonConnectorRetryBudget    = 1
	defaultDaemonConnectorDialTimeout    = 200 * time.Millisecond
	defaultDaemonConnectorReqTimeout     = 10 * time.Second
	defaultDaemonConnectorJavaReqTimeout = 60 * time.Second
	defaultDaemonConnectorWorkspaceName  = "workspace"
	defaultDaemonConnectRetryBudget      = 1
	defaultDaemonConnectRetryDelay       = 25 * time.Millisecond
)

var ErrDaemonLegacyFallback = errors.New("lsp daemon requires legacy startup fallback")

// DaemonLegacyFallbackError marks non-daemon platforms where command wiring
// should route to legacy per-invocation startup.
type DaemonLegacyFallbackError struct {
	GOOS string
}

func (e *DaemonLegacyFallbackError) Error() string {
	if e == nil {
		return ErrDaemonLegacyFallback.Error()
	}
	if strings.TrimSpace(e.GOOS) == "" {
		return ErrDaemonLegacyFallback.Error()
	}
	return fmt.Sprintf("%s: %s", ErrDaemonLegacyFallback.Error(), e.GOOS)
}

func (e *DaemonLegacyFallbackError) Unwrap() error {
	return ErrDaemonLegacyFallback
}

// DaemonConnection contains an initialized LSP client connected through the
// daemon socket, plus lifecycle lease metadata.
type DaemonConnection struct {
	Client *Client
	Lease  Lease
}

// DaemonConnectorOption customizes daemon connector behavior.
type DaemonConnectorOption func(*DaemonConnector)

type daemonConnectorDialSocket func(ctx context.Context, socketPath string) (io.ReadWriteCloser, error)
type daemonConnectorInitParamsBuilder func(workspaceRoot string, spec ServerSpec) (InitializeParams, error)
type daemonConnectorRequestTimeoutResolver func(spec ServerSpec) time.Duration

// DaemonConnector connects to existing daemon sockets, launches daemons
// on-demand, and retries once after stale-state cleanup when connection/setup
// fails.
type DaemonConnector struct {
	registry                *Registry
	lifecycle               *Lifecycle
	dialSocket              daemonConnectorDialSocket
	initializeParamsBuilder daemonConnectorInitParamsBuilder
	requestTimeoutResolver  daemonConnectorRequestTimeoutResolver
	goos                    string
	retryBudget             int
	connectRetryBudget      int
	connectRetryDelay       time.Duration
}

// NewDaemonConnector builds a daemon client connector.
func NewDaemonConnector(registry *Registry, opts ...DaemonConnectorOption) *DaemonConnector {
	if registry == nil {
		registry = NewRegistry()
	}

	connector := &DaemonConnector{
		registry:                registry,
		lifecycle:               NewLifecycle(registry),
		dialSocket:              defaultDaemonConnectorDialSocket,
		initializeParamsBuilder: defaultDaemonConnectorInitializeParams,
		requestTimeoutResolver:  defaultDaemonConnectorRequestTimeout,
		goos:                    runtime.GOOS,
		retryBudget:             defaultDaemonConnectorRetryBudget,
		connectRetryBudget:      defaultDaemonConnectRetryBudget,
		connectRetryDelay:       defaultDaemonConnectRetryDelay,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(connector)
		}
	}

	if connector.registry == nil {
		connector.registry = NewRegistry()
	}
	if connector.lifecycle == nil {
		connector.lifecycle = NewLifecycle(connector.registry)
	}
	if connector.dialSocket == nil {
		connector.dialSocket = defaultDaemonConnectorDialSocket
	}
	if connector.initializeParamsBuilder == nil {
		connector.initializeParamsBuilder = defaultDaemonConnectorInitializeParams
	}
	if connector.requestTimeoutResolver == nil {
		connector.requestTimeoutResolver = defaultDaemonConnectorRequestTimeout
	}
	if strings.TrimSpace(connector.goos) == "" {
		connector.goos = runtime.GOOS
	}
	if connector.retryBudget < 0 {
		connector.retryBudget = 0
	}
	if connector.connectRetryBudget < 0 {
		connector.connectRetryBudget = 0
	}
	if connector.connectRetryDelay <= 0 {
		connector.connectRetryDelay = defaultDaemonConnectRetryDelay
	}

	return connector
}

// WithDaemonConnectorLifecycle overrides lifecycle wiring. If the lifecycle
// contains a registry, that registry is also adopted so lookup and lifecycle
// launch specs stay aligned.
func WithDaemonConnectorLifecycle(lifecycle *Lifecycle) DaemonConnectorOption {
	return func(c *DaemonConnector) {
		if c == nil || lifecycle == nil {
			return
		}
		c.lifecycle = lifecycle
		if lifecycle.registry != nil {
			c.registry = lifecycle.registry
		}
	}
}

// WithDaemonConnectorInitializeParamsBuilder overrides initialize params
// construction for daemon-connected clients.
func WithDaemonConnectorInitializeParamsBuilder(builder func(workspaceRoot string, spec ServerSpec) (InitializeParams, error)) DaemonConnectorOption {
	return func(c *DaemonConnector) {
		if c == nil || builder == nil {
			return
		}
		c.initializeParamsBuilder = builder
	}
}

// WithDaemonConnectorRequestTimeoutResolver overrides request-timeout
// resolution per language-server spec.
func WithDaemonConnectorRequestTimeoutResolver(resolver func(spec ServerSpec) time.Duration) DaemonConnectorOption {
	return func(c *DaemonConnector) {
		if c == nil || resolver == nil {
			return
		}
		c.requestTimeoutResolver = resolver
	}
}

func withDaemonConnectorDialSocket(dialer daemonConnectorDialSocket) DaemonConnectorOption {
	return func(c *DaemonConnector) {
		if c == nil || dialer == nil {
			return
		}
		c.dialSocket = dialer
	}
}

func withDaemonConnectorGOOS(goos string) DaemonConnectorOption {
	return func(c *DaemonConnector) {
		if c == nil {
			return
		}
		c.goos = strings.TrimSpace(strings.ToLower(goos))
	}
}

func withDaemonConnectorRetryBudget(retryBudget int) DaemonConnectorOption {
	return func(c *DaemonConnector) {
		if c == nil {
			return
		}
		c.retryBudget = retryBudget
	}
}

// Connect returns an initialized daemon-backed client ready for immediate
// engine calls.
func (c *DaemonConnector) Connect(ctx context.Context, workspaceRoot, language string) (DaemonConnection, error) {
	if c == nil {
		return DaemonConnection{}, errors.New("daemon connector is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DaemonConnection{}, err
	}
	if c.goos == "windows" {
		fallbackErr := &DaemonLegacyFallbackError{GOOS: c.goos}
		return DaemonConnection{}, errors.Join(fallbackErr, ErrDaemonDisabled)
	}

	spec, err := c.registry.Lookup(language)
	if err != nil {
		return DaemonConnection{}, err
	}
	canonicalRoot, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return DaemonConnection{}, err
	}

	var lastErr error
	for attempt := 0; attempt <= c.retryBudget; attempt++ {
		lease, err := c.lifecycle.Ensure(ctx, canonicalRoot, spec.Language)
		if err != nil {
			lastErr = err
			if !c.shouldRetryEnsure(err, ctx) || attempt == c.retryBudget {
				return DaemonConnection{}, fmt.Errorf("ensure daemon lease: %w", err)
			}
			continue
		}

		client, err := c.connectLeaseWithRetry(ctx, lease, canonicalRoot, spec)
		if err == nil {
			return DaemonConnection{
				Client: client,
				Lease:  lease,
			}, nil
		}
		lastErr = err

		if !c.shouldRetry(err, ctx) || attempt == c.retryBudget {
			return DaemonConnection{}, fmt.Errorf("connect daemon client: %w", err)
		}

		if stopErr := c.lifecycle.Stop(canonicalRoot, spec.Language); stopErr != nil {
			return DaemonConnection{}, errors.Join(
				fmt.Errorf("connect daemon client: %w", err),
				fmt.Errorf("cleanup stale daemon state: %w", stopErr),
			)
		}
	}

	if lastErr == nil {
		lastErr = errors.New("unknown daemon connector error")
	}
	return DaemonConnection{}, fmt.Errorf("connect daemon client: %w", lastErr)
}

func (c *DaemonConnector) connectLeaseWithRetry(
	ctx context.Context,
	lease Lease,
	workspaceRoot string,
	spec ServerSpec,
) (*Client, error) {
	for attempt := 0; ; attempt++ {
		client, err := c.connectLease(ctx, lease, workspaceRoot, spec)
		if err == nil {
			return client, nil
		}

		if attempt >= c.connectRetryBudget {
			return nil, err
		}
		if ctx != nil && ctx.Err() != nil {
			return nil, err
		}
		if !isTransientConnectError(err) {
			return nil, err
		}
		time.Sleep(c.connectRetryDelay)
	}
}

func (c *DaemonConnector) connectLease(
	ctx context.Context,
	lease Lease,
	workspaceRoot string,
	spec ServerSpec,
) (*Client, error) {
	transport, err := c.dialSocket(ctx, lease.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("dial daemon socket: %w", err)
	}

	requestTimeout := c.requestTimeoutResolver(spec)
	if requestTimeout <= 0 {
		_ = transport.Close()
		return nil, daemonConnectorNoRetryError{
			err: fmt.Errorf("request timeout must be > 0: %s", requestTimeout),
		}
	}

	client, err := NewClient(transport, WithRequestTimeout(requestTimeout))
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("create daemon client: %w", err)
	}

	initializeParams, err := c.initializeParamsBuilder(workspaceRoot, spec)
	if err != nil {
		_ = client.Close()
		return nil, daemonConnectorNoRetryError{
			err: fmt.Errorf("build initialize params: %w", err),
		}
	}

	if _, err := client.Initialize(ctx, initializeParams); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("initialize daemon client: %w", err)
	}

	return client, nil
}

func (c *DaemonConnector) shouldRetry(err error, ctx context.Context) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrUnsupportedLanguage) {
		return false
	}
	var noRetry daemonConnectorNoRetryError
	if errors.As(err, &noRetry) {
		return false
	}
	if strings.Contains(err.Error(), "LSP required but ") {
		return false
	}
	return true
}

func (c *DaemonConnector) shouldRetryEnsure(err error, ctx context.Context) bool {
	if !c.shouldRetry(err, ctx) {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if strings.Contains(err.Error(), "rename lifecycle state") {
		return true
	}
	return false
}

func isTransientConnectError(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()
	return strings.Contains(message, daemonBusyMessage) ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "missing Content-Length header")
}

func defaultDaemonConnectorDialSocket(ctx context.Context, socketPath string) (io.ReadWriteCloser, error) {
	trimmed := strings.TrimSpace(socketPath)
	if trimmed == "" {
		return nil, errors.New("daemon socket path is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	dialer := &net.Dialer{Timeout: defaultDaemonConnectorDialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", trimmed)
	if err != nil {
		return nil, fmt.Errorf("dial daemon socket %q: %w", trimmed, err)
	}
	return conn, nil
}

func defaultDaemonConnectorInitializeParams(workspaceRoot string, spec ServerSpec) (InitializeParams, error) {
	_ = spec

	rootURI, err := daemonWorkspaceURI(workspaceRoot)
	if err != nil {
		return InitializeParams{}, err
	}

	workspaceName := filepath.Base(workspaceRoot)
	if strings.TrimSpace(workspaceName) == "" || workspaceName == string(filepath.Separator) {
		workspaceName = defaultDaemonConnectorWorkspaceName
	}

	return InitializeParams{
		ProcessID:  nil, // null tells LSP server not to monitor a parent process; the daemon manages lifecycle
		RootURI:    rootURI,
		ClientInfo: &ClientInfo{Name: "cs"},
		Capabilities: map[string]any{
			"textDocument": map[string]any{},
		},
		WorkspaceFolders: []WorkspaceFolder{
			{
				URI:  rootURI,
				Name: workspaceName,
			},
		},
	}, nil
}

func defaultDaemonConnectorRequestTimeout(spec ServerSpec) time.Duration {
	if strings.TrimSpace(strings.ToLower(spec.Binary)) == "jdtls" {
		return defaultDaemonConnectorJavaReqTimeout
	}
	return defaultDaemonConnectorReqTimeout
}

func daemonWorkspaceURI(workspaceRoot string) (DocumentURI, error) {
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	normalized := filepath.ToSlash(absPath)
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}

	return DocumentURI((&url.URL{
		Scheme: "file",
		Path:   normalized,
	}).String()), nil
}

type daemonConnectorNoRetryError struct {
	err error
}

func (e daemonConnectorNoRetryError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e daemonConnectorNoRetryError) Unwrap() error {
	return e.err
}
