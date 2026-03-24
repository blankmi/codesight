//go:build !windows

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	daemonConnectProbeTimeout     = 200 * time.Millisecond
	daemonReadyTimeout            = 10 * time.Second
	daemonReadyPollInterval       = 25 * time.Millisecond
	daemonAcceptPollInterval      = 100 * time.Millisecond
	daemonInternalShutdownReqID   = int64(2_000_000_000)
	daemonInternalMessageDeadline = 2 * time.Second
)

type daemonClient struct {
	id     uint64
	conn   net.Conn
	writer *bufio.Writer
	mu     sync.Mutex // serializes writes to this client
}

// pendingRequest maps a daemon-assigned request ID back to the originating
// client and the client's original request ID so responses can be routed.
type pendingRequest struct {
	client   *daemonClient
	clientID json.RawMessage
}

type daemonRuntime struct {
	config daemonProcessConfig

	listener     *net.UnixListener
	serverCmd    *exec.Cmd
	serverStdin  io.WriteCloser
	serverStdout io.ReadCloser
	serverReader *bufio.Reader
	serverWriter *bufio.Writer
	serverExitCh chan error

	activityUnixNano atomic.Int64
	lastStatePersist atomic.Int64

	// Multi-client tracking.
	clientsMu    sync.Mutex
	clients      map[uint64]*daemonClient
	nextClientID uint64

	// Request ID remapping for multiplexing.
	pendingMu sync.Mutex
	pending   map[int64]pendingRequest // daemon-assigned ID → originating request
	nextReqID atomic.Int64             // monotonic counter for daemon-global IDs

	serverWriteMu sync.Mutex
	stateWriteMu  sync.Mutex

	shutdownResponseCh chan struct{}

	// Initialize caching: prevents duplicate initialize/initialized to the
	// language server when multiple clients connect over the daemon lifetime.
	// Phase: 0=uninitialized, 1=initialize response cached, 2=fully initialized.
	initPhase    atomic.Int32
	initMu       sync.Mutex
	initResponse []byte       // cached raw initialize response (set once)
	initReqID    atomic.Value // json.RawMessage — tracks in-flight initialize request ID
}

const daemonStatePersistInterval = 5 * time.Second

func runDaemonFromEnv() error {
	config, err := daemonConfigFromEnv()
	if err != nil {
		return fmt.Errorf("load daemon config: %w", err)
	}
	if err := runDaemon(config); err != nil {
		return fmt.Errorf("run daemon: %w", err)
	}
	return nil
}

func launchDaemonProcess(ctx context.Context, config daemonProcessConfig) (int, string, error) {
	if err := config.validate(); err != nil {
		return 0, "", err
	}

	encoded, err := encodeDaemonProcessConfig(config)
	if err != nil {
		return 0, "", err
	}

	exe, err := os.Executable()
	if err != nil {
		return 0, "", fmt.Errorf("resolve executable: %w", err)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(
		os.Environ(),
		daemonModeEnvVar+"="+daemonModeEnvValue,
		daemonConfigEnvVar+"="+encoded,
	)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = daemonLogWriter(config.StatePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, "", fmt.Errorf("start daemon process: %w", err)
	}

	startID, err := processStartID(cmd.Process.Pid)
	if err != nil && !errors.Is(err, errProcessIdentityUnsupported) {
		_ = killProcess(cmd.Process.Pid)
		return 0, "", fmt.Errorf("capture daemon process identity: %w", err)
	}

	go func() {
		_ = cmd.Wait()
	}()

	if err := waitForDaemonReady(ctx, config.SocketPath, cmd.Process.Pid); err != nil {
		_ = killProcess(cmd.Process.Pid)
		return 0, "", err
	}

	return cmd.Process.Pid, startID, nil
}

func daemonSocketHealthy(ctx context.Context, socketPath string) error {
	conn, err := dialDaemonSocket(ctx, socketPath)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func shutdownDaemonViaSocket(ctx context.Context, socketPath string) error {
	conn, err := dialDaemonSocket(ctx, socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(daemonInternalMessageDeadline))
	}

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	shutdownRequest := RequestMessage{
		JSONRPC: JSONRPCVersion,
		ID:      daemonInternalShutdownReqID,
		Method:  MethodShutdown,
	}
	if err := writeJSONRPCMessage(writer, shutdownRequest); err != nil {
		return fmt.Errorf("send daemon shutdown request: %w", err)
	}

	for {
		payload, err := readLSPMessage(reader)
		if err != nil {
			return fmt.Errorf("read daemon shutdown response: %w", err)
		}

		var response ResponseMessage
		if err := json.Unmarshal(payload, &response); err != nil {
			continue
		}
		if len(response.ID) == 0 {
			continue
		}
		id, err := parseResponseID(response.ID)
		if err != nil {
			continue
		}
		if id == daemonInternalShutdownReqID {
			break
		}
	}

	exitNotification := NotificationMessage{
		JSONRPC: JSONRPCVersion,
		Method:  MethodExit,
	}
	if err := writeJSONRPCMessage(writer, exitNotification); err != nil {
		return fmt.Errorf("send daemon exit notification: %w", err)
	}

	return nil
}

func runDaemon(config daemonProcessConfig) error {
	if err := config.validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(config.SocketPath), 0o700); err != nil {
		return fmt.Errorf("create daemon socket directory: %w", err)
	}

	if err := removeStaleSocket(config.SocketPath); err != nil {
		return err
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: config.SocketPath, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen daemon socket: %w", err)
	}
	if err := os.Chmod(config.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("set daemon socket permissions: %w", err)
	}

	serverCmd := exec.Command(config.Binary, config.Args...)
	serverCmd.Dir = config.WorkspaceRoot
	serverCmd.Env = scrubInternalDaemonEnv(os.Environ())
	serverStdin, err := serverCmd.StdinPipe()
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("create language-server stdin pipe: %w", err)
	}
	serverStdout, err := serverCmd.StdoutPipe()
	if err != nil {
		_ = listener.Close()
		_ = serverStdin.Close()
		return fmt.Errorf("create language-server stdout pipe: %w", err)
	}
	serverCmd.Stderr = daemonLogWriter(config.StatePath)

	if err := serverCmd.Start(); err != nil {
		_ = listener.Close()
		_ = serverStdin.Close()
		_ = serverStdout.Close()
		return fmt.Errorf("start language server %s: %w", config.Binary, err)
	}

	runtime := &daemonRuntime{
		config:             config,
		listener:           listener,
		serverCmd:          serverCmd,
		serverStdin:        serverStdin,
		serverStdout:       serverStdout,
		serverReader:       bufio.NewReader(serverStdout),
		serverWriter:       bufio.NewWriter(serverStdin),
		serverExitCh:       make(chan error, 1),
		shutdownResponseCh: make(chan struct{}, 1),
		clients:            make(map[uint64]*daemonClient),
		pending:            make(map[int64]pendingRequest),
	}
	runtime.activityUnixNano.Store(time.Now().UnixNano())
	runtime.touchActivity(time.Now())

	go func() {
		runtime.serverExitCh <- serverCmd.Wait()
	}()

	go runtime.serverReadLoop()

	defer runtime.cleanup()
	return runtime.acceptLoop()
}

func (d *daemonRuntime) acceptLoop() error {
	for {
		if d.clientCount() == 0 && d.idleExpired(time.Now()) {
			_ = d.shutdownLanguageServer()
			return nil
		}

		select {
		case err := <-d.serverExitCh:
			if err != nil && !errors.Is(err, os.ErrProcessDone) {
				return fmt.Errorf("language server exited: %w", err)
			}
			return nil
		default:
		}

		if err := d.listener.SetDeadline(time.Now().Add(daemonAcceptPollInterval)); err != nil {
			return fmt.Errorf("set daemon accept deadline: %w", err)
		}

		conn, err := d.listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept daemon client: %w", err)
		}

		client := d.addClient(conn)
		d.touchActivity(time.Now())
		go d.handleClient(client)
	}
}

func (d *daemonRuntime) handleClient(client *daemonClient) {
	defer d.removeClient(client)

	reader := bufio.NewReader(client.conn)
	for {
		payload, err := readLSPMessage(reader)
		if err != nil {
			return
		}

		d.touchActivity(time.Now())

		method := extractMethod(payload)
		reqID := extractRequestID(payload)
		phase := d.initPhase.Load()

		switch method {
		case MethodInitialize:
			if phase >= 1 {
				// Subsequent client: reply from cache, rewrite response ID.
				if err := d.replyInitializeFromCache(client, payload); err != nil {
					return
				}
				continue
			}
			// First client: remap ID and track for cache matching.
			if reqID != nil {
				remapped := d.remapRequest(client, reqID, payload)
				// Store as []byte (not json.RawMessage) to match cacheInitializeResponse's type assertion.
				if newID := extractRequestID(remapped); newID != nil {
					d.initReqID.Store(append([]byte(nil), newID...))
				}
				if err := d.writeToServer(remapped); err != nil {
					_ = killProcess(d.serverCmd.Process.Pid)
					return
				}
				continue
			}
		case MethodInitialized:
			if phase >= 2 {
				continue // swallow duplicate
			}
			// First client: forward notification (no ID), mark fully initialized.
			if err := d.writeToServer(payload); err != nil {
				_ = killProcess(d.serverCmd.Process.Pid)
				return
			}
			d.initPhase.Store(2)
			continue
		}

		// Requests (have both id and method): remap ID for multiplexing.
		if reqID != nil && method != "" {
			remapped := d.remapRequest(client, reqID, payload)
			if err := d.writeToServer(remapped); err != nil {
				_ = killProcess(d.serverCmd.Process.Pid)
				return
			}
			continue
		}

		// Notifications and other messages: forward as-is.
		if err := d.writeToServer(payload); err != nil {
			_ = killProcess(d.serverCmd.Process.Pid)
			return
		}
	}
}

func (d *daemonRuntime) serverReadLoop() {
	for {
		payload, err := readLSPMessage(d.serverReader)
		if err != nil {
			return
		}

		d.touchActivity(time.Now())
		d.observeShutdownResponse(payload)
		d.cacheInitializeResponse(payload)
		d.forwardToClient(payload)
	}
}

func (d *daemonRuntime) shutdownLanguageServer() error {
	shutdownErr := d.sendShutdownAndExit()

	select {
	case <-d.serverExitCh:
		return shutdownErr
	case <-time.After(defaultDaemonShutdownTimeout):
		killErr := killProcess(d.serverCmd.Process.Pid)
		return errors.Join(shutdownErr, killErr)
	}
}

func (d *daemonRuntime) sendShutdownAndExit() error {
	request := RequestMessage{
		JSONRPC: JSONRPCVersion,
		ID:      daemonInternalShutdownReqID,
		Method:  MethodShutdown,
	}
	if err := d.writeToServerJSON(request); err != nil {
		return err
	}

	select {
	case <-d.shutdownResponseCh:
	case <-time.After(defaultDaemonShutdownTimeout):
		return errors.New("timed out waiting for shutdown response")
	}

	notification := NotificationMessage{
		JSONRPC: JSONRPCVersion,
		Method:  MethodExit,
	}
	if err := d.writeToServerJSON(notification); err != nil {
		return err
	}
	return nil
}

func (d *daemonRuntime) writeToServerJSON(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal daemon message: %w", err)
	}
	return d.writeToServer(payload)
}

func (d *daemonRuntime) writeToServer(payload []byte) error {
	d.serverWriteMu.Lock()
	defer d.serverWriteMu.Unlock()

	if err := writeLSPMessage(d.serverWriter, payload); err != nil {
		return fmt.Errorf("proxy request to language server: %w", err)
	}
	return nil
}

func (d *daemonRuntime) observeShutdownResponse(payload []byte) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return
	}
	if envelope.Method != "" || len(envelope.ID) == 0 {
		return
	}

	id, err := parseResponseID(envelope.ID)
	if err != nil {
		return
	}
	if id != daemonInternalShutdownReqID {
		return
	}

	select {
	case d.shutdownResponseCh <- struct{}{}:
	default:
	}
}

func extractMethod(payload []byte) string {
	var envelope struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return ""
	}
	return envelope.Method
}

func extractRequestID(payload []byte) json.RawMessage {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil
	}
	if len(envelope.ID) == 0 {
		return nil
	}
	return envelope.ID
}

// rewriteJSONID replaces the "id" field in a JSON-RPC payload with newID.
// Uses unmarshal/marshal to ensure correct JSON encoding.
func rewriteJSONID(payload []byte, newID json.RawMessage) []byte {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return payload
	}
	msg["id"] = newID
	result, err := json.Marshal(msg)
	if err != nil {
		return payload
	}
	return result
}

func (d *daemonRuntime) cacheInitializeResponse(payload []byte) {
	if d.initPhase.Load() != 0 {
		return
	}

	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return
	}
	// Responses have an ID but no method.
	if envelope.Method != "" || len(envelope.ID) == 0 {
		return
	}

	// Check if this response ID matches the tracked initialize request ID.
	stored, ok := d.initReqID.Load().([]byte)
	if !ok || len(stored) == 0 {
		return
	}
	if string(envelope.ID) != string(stored) {
		return
	}

	d.initMu.Lock()
	defer d.initMu.Unlock()

	if d.initPhase.Load() != 0 {
		return
	}
	d.initResponse = append([]byte(nil), payload...)
	d.initPhase.Store(1)
}

func (d *daemonRuntime) replyInitializeFromCache(client *daemonClient, requestPayload []byte) error {
	clientReqID := extractRequestID(requestPayload)
	if clientReqID == nil {
		return fmt.Errorf("initialize request missing id")
	}

	d.initMu.Lock()
	cached := d.initResponse
	d.initMu.Unlock()

	if len(cached) == 0 {
		return fmt.Errorf("no cached initialize response")
	}

	var response ResponseMessage
	if err := json.Unmarshal(cached, &response); err != nil {
		return fmt.Errorf("unmarshal cached initialize response: %w", err)
	}
	response.ID = append(json.RawMessage(nil), clientReqID...)

	rewritten, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal rewritten initialize response: %w", err)
	}

	if err := d.writeToClient(client, rewritten); err != nil {
		return fmt.Errorf("write cached initialize response: %w", err)
	}
	return nil
}

// remapRequest assigns a daemon-global request ID to a client request and
// registers it in the pending map so the response can be routed back.
// Returns a new payload with the remapped ID.
func (d *daemonRuntime) remapRequest(client *daemonClient, originalID json.RawMessage, payload []byte) []byte {
	daemonID := d.nextReqID.Add(1)

	d.pendingMu.Lock()
	d.pending[daemonID] = pendingRequest{
		client:   client,
		clientID: append(json.RawMessage(nil), originalID...),
	}
	d.pendingMu.Unlock()

	// Rewrite the "id" field in the JSON payload.
	newIDBytes, _ := json.Marshal(daemonID)
	return rewriteJSONID(payload, newIDBytes)
}

// forwardToClient routes a language-server message to the appropriate client.
// Responses (id, no method) are routed via the pending map.
// Server-initiated requests (id + method) are routed to the first available client.
// Notifications (method, no id) are broadcast to all clients.
func (d *daemonRuntime) forwardToClient(payload []byte) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return
	}

	hasID := len(envelope.ID) > 0
	hasMethod := envelope.Method != ""

	switch {
	case hasID && !hasMethod:
		// Response from language server — route to originating client.
		d.routeResponse(payload, envelope.ID)
	case hasID && hasMethod:
		// Server-initiated request — route to first available client and
		// reject if none are connected.
		d.routeServerRequest(payload)
	default:
		// Server notification — broadcast to all connected clients.
		d.broadcastToClients(payload)
	}
}

func (d *daemonRuntime) routeResponse(payload []byte, responseID json.RawMessage) {
	daemonID, err := parseResponseID(responseID)
	if err != nil {
		return
	}

	d.pendingMu.Lock()
	pending, found := d.pending[daemonID]
	if found {
		delete(d.pending, daemonID)
	}
	d.pendingMu.Unlock()

	if !found {
		// Orphaned response (client disconnected) — discard.
		return
	}

	// Rewrite the response ID back to the client's original ID.
	rewritten := rewriteJSONID(payload, pending.clientID)
	_ = d.writeToClient(pending.client, rewritten)
}

func (d *daemonRuntime) routeServerRequest(payload []byte) {
	d.clientsMu.Lock()
	var target *daemonClient
	for _, c := range d.clients {
		target = c
		break
	}
	d.clientsMu.Unlock()

	if target == nil {
		d.rejectServerRequest(payload)
		return
	}
	_ = d.writeToClient(target, payload)
}

func (d *daemonRuntime) broadcastToClients(payload []byte) {
	d.clientsMu.Lock()
	snapshot := make([]*daemonClient, 0, len(d.clients))
	for _, c := range d.clients {
		snapshot = append(snapshot, c)
	}
	d.clientsMu.Unlock()

	for _, c := range snapshot {
		_ = d.writeToClient(c, payload)
	}
}

func (d *daemonRuntime) writeToClient(client *daemonClient, payload []byte) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := writeLSPMessage(client.writer, payload); err != nil {
		_ = client.conn.Close()
		return err
	}
	return nil
}

func (d *daemonRuntime) rejectServerRequest(payload []byte) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return
	}
	if envelope.Method == "" || len(envelope.ID) == 0 {
		return
	}

	response := ResponseMessage{
		JSONRPC: JSONRPCVersion,
		ID:      envelope.ID,
		Error: &ResponseErrorBody{
			Code:    -32601,
			Message: "method not found (no active client)",
		},
	}
	_ = d.writeToServerJSON(response)
}

func (d *daemonRuntime) addClient(conn net.Conn) *daemonClient {
	d.clientsMu.Lock()
	defer d.clientsMu.Unlock()

	d.nextClientID++
	client := &daemonClient{
		id:     d.nextClientID,
		conn:   conn,
		writer: bufio.NewWriter(conn),
	}
	d.clients[client.id] = client
	return client
}

func (d *daemonRuntime) removeClient(client *daemonClient) {
	d.clientsMu.Lock()
	delete(d.clients, client.id)
	d.clientsMu.Unlock()

	_ = client.conn.Close()

	// Purge pending requests for this client so orphaned responses are discarded.
	d.pendingMu.Lock()
	for id, p := range d.pending {
		if p.client == client {
			delete(d.pending, id)
		}
	}
	d.pendingMu.Unlock()
}

func (d *daemonRuntime) clientCount() int {
	d.clientsMu.Lock()
	n := len(d.clients)
	d.clientsMu.Unlock()
	return n
}

func (d *daemonRuntime) idleExpired(now time.Time) bool {
	last := d.activityUnixNano.Load()
	if last == 0 {
		return false
	}
	return now.Sub(time.Unix(0, last)) >= d.config.idleTimeout()
}

func (d *daemonRuntime) touchActivity(now time.Time) {
	d.activityUnixNano.Store(now.UnixNano())

	lastPersist := d.lastStatePersist.Load()
	if lastPersist != 0 && now.Sub(time.Unix(0, lastPersist)) < daemonStatePersistInterval {
		return
	}

	d.stateWriteMu.Lock()
	defer d.stateWriteMu.Unlock()

	state, err := readStateFile(d.config.StatePath)
	if err != nil {
		return
	}
	state.LastUsedUnixNano = now.UnixNano()
	if err := writeStateFile(d.config.StatePath, state); err != nil {
		return
	}
	d.lastStatePersist.Store(now.UnixNano())
}

func (d *daemonRuntime) cleanup() {
	d.clientsMu.Lock()
	for id, c := range d.clients {
		_ = c.conn.Close()
		delete(d.clients, id)
	}
	d.clientsMu.Unlock()

	if d.listener != nil {
		_ = d.listener.Close()
	}
	if d.serverStdin != nil {
		_ = d.serverStdin.Close()
	}
	if d.serverStdout != nil {
		_ = d.serverStdout.Close()
	}

	if d.serverCmd != nil && d.serverCmd.Process != nil && processAlive(d.serverCmd.Process.Pid) {
		_ = killProcess(d.serverCmd.Process.Pid)
		select {
		case <-d.serverExitCh:
		case <-time.After(defaultDaemonShutdownTimeout):
		}
	}

	_ = removeStateArtifacts(d.config.StatePath, d.config.SocketPath)
}

func removeStaleSocket(socketPath string) error {
	trimmed := strings.TrimSpace(socketPath)
	if trimmed == "" {
		return errors.New("daemon socket path is required")
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat daemon socket: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("daemon socket path is a directory: %s", trimmed)
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), daemonConnectProbeTimeout)
	probeErr := daemonSocketHealthy(probeCtx, trimmed)
	cancel()
	if probeErr == nil {
		return fmt.Errorf("daemon socket already active: %s", trimmed)
	}

	if err := os.Remove(trimmed); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale daemon socket: %w", err)
	}
	return nil
}

func waitForDaemonReady(ctx context.Context, socketPath string, pid int) error {
	if ctx == nil {
		ctx = context.Background()
	}

	readyCtx, cancel := context.WithTimeout(ctx, daemonReadyTimeout)
	defer cancel()

	for {
		if err := readyCtx.Err(); err != nil {
			return fmt.Errorf("wait for daemon socket %q: %w", socketPath, err)
		}
		if !processAlive(pid) {
			return fmt.Errorf("lsp daemon process %d exited before ready", pid)
		}

		info, err := os.Stat(socketPath)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat daemon socket %q: %w", socketPath, err)
		}

		time.Sleep(daemonReadyPollInterval)
	}
}

func dialDaemonSocket(ctx context.Context, socketPath string) (net.Conn, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, errors.New("daemon socket path is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	dialer := &net.Dialer{Timeout: daemonConnectProbeTimeout}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial daemon socket %q: %w", socketPath, err)
	}
	return conn, nil
}

func writeJSONRPCMessage(writer *bufio.Writer, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal json-rpc message: %w", err)
	}
	if err := writeLSPMessage(writer, payload); err != nil {
		return fmt.Errorf("write json-rpc message: %w", err)
	}
	return nil
}

const daemonLogFileExtension = ".log"

func daemonLogWriter(statePath string) io.Writer {
	logPath := strings.TrimSuffix(statePath, filepath.Ext(statePath)) + daemonLogFileExtension
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return io.Discard
	}
	return f
}

func scrubInternalDaemonEnv(env []string) []string {
	scrubbed := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, daemonModeEnvVar+"=") {
			continue
		}
		if strings.HasPrefix(entry, daemonConfigEnvVar+"=") {
			continue
		}
		scrubbed = append(scrubbed, entry)
	}
	return scrubbed
}
