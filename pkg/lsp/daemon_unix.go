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
	daemonReadyTimeout            = 2 * time.Second
	daemonReadyPollInterval       = 25 * time.Millisecond
	daemonAcceptPollInterval      = 100 * time.Millisecond
	daemonInternalShutdownReqID   = int64(9_000_000_000_000_000)
	daemonInternalMessageDeadline = 2 * time.Second
)

type daemonClient struct {
	conn   net.Conn
	writer *bufio.Writer
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

	activityUnixNano  atomic.Int64
	lastStatePersist  atomic.Int64

	activeMu sync.Mutex
	active   *daemonClient

	serverWriteMu sync.Mutex
	stateWriteMu  sync.Mutex

	shutdownResponseCh chan struct{}
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

func launchDaemonProcess(ctx context.Context, config daemonProcessConfig) (int, error) {
	if err := config.validate(); err != nil {
		return 0, err
	}

	encoded, err := encodeDaemonProcessConfig(config)
	if err != nil {
		return 0, err
	}

	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve executable: %w", err)
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
		return 0, fmt.Errorf("start daemon process: %w", err)
	}

	go func() {
		_ = cmd.Wait()
	}()

	if err := waitForDaemonReady(ctx, config.SocketPath, cmd.Process.Pid); err != nil {
		_ = killProcess(cmd.Process.Pid)
		return 0, err
	}

	return cmd.Process.Pid, nil
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
	serverCmd.Stderr = io.Discard

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
		if !d.hasActiveClient() && d.idleExpired(time.Now()) {
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

		if !d.activateClient(conn) {
			_ = rejectBusyConnection(conn)
			continue
		}

		d.touchActivity(time.Now())
		go d.handleClient(conn)
	}
}

func (d *daemonRuntime) handleClient(conn net.Conn) {
	defer d.deactivateClient(conn)

	reader := bufio.NewReader(conn)
	for {
		payload, err := readLSPMessage(reader)
		if err != nil {
			return
		}

		d.touchActivity(time.Now())
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

func (d *daemonRuntime) forwardToClient(payload []byte) {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	if d.active == nil {
		return
	}
	if err := writeLSPMessage(d.active.writer, payload); err != nil {
		_ = d.active.conn.Close()
		d.active = nil
	}
}

func (d *daemonRuntime) activateClient(conn net.Conn) bool {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	if d.active != nil {
		return false
	}

	d.active = &daemonClient{
		conn:   conn,
		writer: bufio.NewWriter(conn),
	}
	return true
}

func (d *daemonRuntime) deactivateClient(conn net.Conn) {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	if d.active != nil && d.active.conn == conn {
		_ = d.active.conn.Close()
		d.active = nil
		return
	}
	_ = conn.Close()
}

func (d *daemonRuntime) hasActiveClient() bool {
	d.activeMu.Lock()
	active := d.active != nil
	d.activeMu.Unlock()
	return active
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
	d.activeMu.Lock()
	if d.active != nil {
		_ = d.active.conn.Close()
		d.active = nil
	}
	d.activeMu.Unlock()

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

func rejectBusyConnection(conn net.Conn) error {
	defer func() {
		_ = conn.Close()
	}()

	if err := conn.SetWriteDeadline(time.Now().Add(daemonInternalMessageDeadline)); err != nil {
		return err
	}
	if _, err := io.WriteString(conn, daemonBusyMessage+"\n"); err != nil {
		return err
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
