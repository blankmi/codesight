package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultClientRequestTimeout = 10 * time.Second

var ErrClientClosed = errors.New("lsp client closed")

// ClientOption configures a Client.
type ClientOption func(*Client) error

// WithRequestTimeout configures the default timeout applied when call context
// does not already define a deadline.
func WithRequestTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) error {
		if timeout <= 0 {
			return fmt.Errorf("request timeout must be > 0: %s", timeout)
		}
		c.requestTimeout = timeout
		return nil
	}
}

// ResponseError wraps a JSON-RPC error returned by the language server.
type ResponseError struct {
	Method string
	ID     int64
	Body   ResponseErrorBody
}

func (e *ResponseError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf(
		"lsp request %q failed with code %d: %s",
		e.Method,
		e.Body.Code,
		e.Body.Message,
	)
}

// Client implements a reusable JSON-RPC client over an LSP stdio transport.
type Client struct {
	transport io.ReadWriteCloser
	reader    *bufio.Reader
	writer    *bufio.Writer

	requestTimeout time.Duration
	nextID         atomic.Int64

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan ResponseMessage

	notificationMu     sync.RWMutex
	notificationNextID int64
	notifications      map[string]map[int64]func(json.RawMessage)

	initMu      sync.Mutex
	initialized bool

	readErrMu sync.RWMutex
	readErr   error
	done      chan struct{}

	closeOnce sync.Once
}

// NewClient starts a JSON-RPC client on top of an existing stdio transport.
func NewClient(transport io.ReadWriteCloser, opts ...ClientOption) (*Client, error) {
	if transport == nil {
		return nil, errors.New("transport is nil")
	}

	client := &Client{
		transport:      transport,
		reader:         bufio.NewReader(transport),
		writer:         bufio.NewWriter(transport),
		requestTimeout: defaultClientRequestTimeout,
		pending:        make(map[int64]chan ResponseMessage),
		notifications:  make(map[string]map[int64]func(json.RawMessage)),
		done:           make(chan struct{}),
	}
	client.nextID.Store(1)

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(client); err != nil {
			return nil, err
		}
	}

	go client.readLoop()
	return client, nil
}

// Initialize performs the explicit LSP initialize + initialized handshake.
func (c *Client) Initialize(ctx context.Context, params InitializeParams) (InitializeResult, error) {
	if c == nil {
		return InitializeResult{}, errors.New("client is nil")
	}

	c.initMu.Lock()
	defer c.initMu.Unlock()

	if c.initialized {
		return InitializeResult{}, errors.New("client is already initialized")
	}

	var result InitializeResult
	if err := c.Call(ctx, MethodInitialize, params, &result); err != nil {
		return InitializeResult{}, fmt.Errorf("initialize request failed: %w", err)
	}
	if err := c.Notify(ctx, MethodInitialized, InitializedParams{}); err != nil {
		return InitializeResult{}, fmt.Errorf("initialized notification failed: %w", err)
	}

	c.initialized = true
	return result, nil
}

// SubscribeNotification registers a handler for one server notification method.
// The returned function removes the handler when called.
func (c *Client) SubscribeNotification(method string, handler func(json.RawMessage)) (func(), error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	if handler == nil {
		return nil, errors.New("notification handler is nil")
	}

	method = strings.TrimSpace(method)
	if method == "" {
		return nil, errors.New("notification method is required")
	}
	if c.isDone() {
		return nil, c.transportErr()
	}

	c.notificationMu.Lock()
	defer c.notificationMu.Unlock()

	c.notificationNextID++
	handlerID := c.notificationNextID

	handlers := c.notifications[method]
	if handlers == nil {
		handlers = make(map[int64]func(json.RawMessage))
		c.notifications[method] = handlers
	}
	handlers[handlerID] = handler

	return func() {
		c.notificationMu.Lock()
		defer c.notificationMu.Unlock()

		handlers := c.notifications[method]
		if handlers == nil {
			return
		}
		delete(handlers, handlerID)
		if len(handlers) == 0 {
			delete(c.notifications, method)
		}
	}, nil
}

// Notify sends an LSP notification without waiting for a response.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	if c == nil {
		return errors.New("client is nil")
	}

	method = strings.TrimSpace(method)
	if method == "" {
		return errors.New("notification method is required")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("lsp notification %q aborted: %w", method, err)
	}
	if c.isDone() {
		return fmt.Errorf("lsp notification %q failed: %w", method, c.transportErr())
	}

	message := NotificationMessage{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  params,
	}
	if err := c.writeMessage(message); err != nil {
		return fmt.Errorf("lsp notification %q send failed: %w", method, err)
	}
	return nil
}

// Call sends an LSP request and waits for its response.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if c == nil {
		return errors.New("client is nil")
	}

	method = strings.TrimSpace(method)
	if method == "" {
		return errors.New("request method is required")
	}

	callCtx, cancel := c.withRequestTimeout(ctx)
	defer cancel()
	if err := callCtx.Err(); err != nil {
		return fmt.Errorf("lsp request %q aborted: %w", method, err)
	}

	id := c.nextID.Add(1) - 1
	responseCh := make(chan ResponseMessage, 1)
	if err := c.registerPending(id, responseCh); err != nil {
		return fmt.Errorf("lsp request %q failed before send: %w", method, err)
	}

	message := RequestMessage{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.writeMessage(message); err != nil {
		c.removePending(id)
		return fmt.Errorf("lsp request %q send failed: %w", method, err)
	}

	select {
	case response, ok := <-responseCh:
		if !ok {
			return fmt.Errorf("lsp request %q failed: %w", method, c.transportErr())
		}
		if response.Error != nil {
			return &ResponseError{
				Method: method,
				ID:     id,
				Body:   *response.Error,
			}
		}
		if result != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, result); err != nil {
				return fmt.Errorf("lsp request %q decode failed: %w", method, err)
			}
		}
		return nil
	case <-callCtx.Done():
		c.removePending(id)
		return fmt.Errorf("lsp request %q timed out: %w", method, callCtx.Err())
	case <-c.done:
		c.removePending(id)
		return fmt.Errorf("lsp request %q failed: %w", method, c.transportErr())
	}
}

// Shutdown sends shutdown and exit, then closes the underlying transport.
func (c *Client) Shutdown(ctx context.Context) error {
	if c == nil {
		return errors.New("client is nil")
	}

	shutdownErr := c.Call(ctx, MethodShutdown, nil, nil)
	exitErr := c.Notify(ctx, MethodExit, nil)
	closeErr := c.Close()

	return errors.Join(
		wrapErr("shutdown request failed", shutdownErr),
		wrapErr("exit notification failed", exitErr),
		wrapErr("close client failed", closeErr),
	)
}

// Close closes the underlying transport and waits for the reader loop to stop.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	var closeErr error
	c.closeOnce.Do(func() {
		closeErr = c.transport.Close()
		<-c.done
		if errors.Is(closeErr, io.EOF) {
			closeErr = nil
		}
	})
	return closeErr
}

func (c *Client) withRequestTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.requestTimeout <= 0 {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.requestTimeout)
}

func (c *Client) registerPending(id int64, ch chan ResponseMessage) error {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	if c.isDone() {
		return c.transportErr()
	}
	if _, exists := c.pending[id]; exists {
		return fmt.Errorf("duplicate request id %d", id)
	}
	c.pending[id] = ch
	return nil
}

func (c *Client) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) routeResponse(response ResponseMessage) {
	id, err := parseResponseID(response.ID)
	if err != nil {
		return
	}

	c.pendingMu.Lock()
	responseCh, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()

	if ok {
		responseCh <- response
		close(responseCh)
	}
}

func (c *Client) failPending(err error) {
	c.setReadErr(err)

	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan ResponseMessage)
	c.pendingMu.Unlock()

	for _, responseCh := range pending {
		close(responseCh)
	}
}

func (c *Client) setReadErr(err error) {
	if err == nil {
		return
	}

	c.readErrMu.Lock()
	if c.readErr == nil {
		c.readErr = err
	}
	c.readErrMu.Unlock()
}

func (c *Client) transportErr() error {
	c.readErrMu.RLock()
	err := c.readErr
	c.readErrMu.RUnlock()

	if err != nil {
		return err
	}
	return ErrClientClosed
}

func (c *Client) dispatchNotification(method string, params json.RawMessage) {
	c.notificationMu.RLock()
	registered := c.notifications[method]
	if len(registered) == 0 {
		c.notificationMu.RUnlock()
		return
	}

	handlers := make([]func(json.RawMessage), 0, len(registered))
	for _, handler := range registered {
		handlers = append(handlers, handler)
	}
	c.notificationMu.RUnlock()

	payload := append(json.RawMessage(nil), params...)
	for _, handler := range handlers {
		handler(payload)
	}
}

func (c *Client) isDone() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *Client) readLoop() {
	defer close(c.done)

	for {
		payload, err := readLSPMessage(c.reader)
		if err != nil {
			c.failPending(fmt.Errorf("read lsp message: %w", err))
			return
		}

		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			c.failPending(fmt.Errorf("decode lsp message envelope: %w", err))
			return
		}

		if envelope.Method != "" {
			if len(envelope.ID) == 0 {
				c.dispatchNotification(envelope.Method, envelope.Params)
				continue
			}
			if err := c.replyMethodNotFound(envelope.ID); err != nil {
				c.failPending(fmt.Errorf("reply to server request failed: %w", err))
				return
			}
			continue
		}
		if len(envelope.ID) == 0 {
			continue
		}

		var response ResponseMessage
		if err := json.Unmarshal(payload, &response); err != nil {
			c.failPending(fmt.Errorf("decode lsp response: %w", err))
			return
		}
		c.routeResponse(response)
	}
}

func (c *Client) replyMethodNotFound(id json.RawMessage) error {
	response := ResponseMessage{
		JSONRPC: JSONRPCVersion,
		ID:      append(json.RawMessage(nil), id...),
		Error: &ResponseErrorBody{
			Code:    -32601,
			Message: "method not found",
		},
	}
	return c.writeMessage(response)
}

func (c *Client) writeMessage(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal lsp message: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := writeLSPMessage(c.writer, payload); err != nil {
		return fmt.Errorf("write lsp message: %w", err)
	}
	return nil
}

func writeLSPMessage(writer io.Writer, payload []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(writer, header); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	if flusher, ok := writer.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}

func readLSPMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed LSP header %q", trimmed)
		}

		name := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])
		if name != "content-length" {
			continue
		}

		length, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid Content-Length %q: %w", value, err)
		}
		if length < 0 {
			return nil, fmt.Errorf("invalid Content-Length %q: must be >= 0", value)
		}
		contentLength = length
	}

	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseResponseID(rawID json.RawMessage) (int64, error) {
	if len(rawID) == 0 {
		return 0, errors.New("response id is missing")
	}

	var numeric int64
	if err := json.Unmarshal(rawID, &numeric); err == nil {
		return numeric, nil
	}

	var stringID string
	if err := json.Unmarshal(rawID, &stringID); err == nil {
		id, parseErr := strconv.ParseInt(stringID, 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("parse response id %q: %w", stringID, parseErr)
		}
		return id, nil
	}

	return 0, fmt.Errorf("unsupported response id %q", string(rawID))
}

func wrapErr(message string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
