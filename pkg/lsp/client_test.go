package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientCallRoundTrip(t *testing.T) {
	client, server := newClientPipe(t, WithRequestTimeout(2*time.Second))
	defer closePipeClient(t, client, server)

	serverDone := make(chan error, 1)
	go func() {
		payload, err := readLSPMessage(server.reader)
		if err != nil {
			serverDone <- fmt.Errorf("read request: %w", err)
			return
		}

		var request RequestMessage
		if err := json.Unmarshal(payload, &request); err != nil {
			serverDone <- fmt.Errorf("decode request: %w", err)
			return
		}
		if request.Method != "codesight/testRoundTrip" {
			serverDone <- fmt.Errorf("request method = %q, want %q", request.Method, "codesight/testRoundTrip")
			return
		}

		response := ResponseMessage{
			JSONRPC: JSONRPCVersion,
			ID:      rawMessageFromValue(t, request.ID),
			Result:  rawMessageFromValue(t, map[string]string{"status": "ok"}),
		}
		if err := writeServerMessage(server.writer, response); err != nil {
			serverDone <- fmt.Errorf("write response: %w", err)
			return
		}

		serverDone <- nil
	}()

	var result struct {
		Status string `json:"status"`
	}
	if err := client.Call(context.Background(), "codesight/testRoundTrip", map[string]string{"query": "foo"}, &result); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if result.Status != "ok" {
		t.Fatalf("Call result status = %q, want %q", result.Status, "ok")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server goroutine failed: %v", err)
	}
}

func TestClientInitializeHandshake(t *testing.T) {
	client, server := newClientPipe(t, WithRequestTimeout(2*time.Second))
	defer closePipeClient(t, client, server)

	serverDone := make(chan error, 1)
	go func() {
		initializePayload, err := readLSPMessage(server.reader)
		if err != nil {
			serverDone <- fmt.Errorf("read initialize request: %w", err)
			return
		}

		var initializeRequest RequestMessage
		if err := json.Unmarshal(initializePayload, &initializeRequest); err != nil {
			serverDone <- fmt.Errorf("decode initialize request: %w", err)
			return
		}
		if initializeRequest.Method != MethodInitialize {
			serverDone <- fmt.Errorf("first method = %q, want %q", initializeRequest.Method, MethodInitialize)
			return
		}

		initializeResponse := ResponseMessage{
			JSONRPC: JSONRPCVersion,
			ID:      rawMessageFromValue(t, initializeRequest.ID),
			Result: rawMessageFromValue(t, InitializeResult{
				Capabilities: map[string]any{
					"referencesProvider": true,
				},
				ServerInfo: &ServerInfo{Name: "test-lsp"},
			}),
		}
		if err := writeServerMessage(server.writer, initializeResponse); err != nil {
			serverDone <- fmt.Errorf("write initialize response: %w", err)
			return
		}

		initializedPayload, err := readLSPMessage(server.reader)
		if err != nil {
			serverDone <- fmt.Errorf("read initialized notification: %w", err)
			return
		}

		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(initializedPayload, &envelope); err != nil {
			serverDone <- fmt.Errorf("decode initialized envelope: %w", err)
			return
		}
		if envelope.Method != MethodInitialized {
			serverDone <- fmt.Errorf("second method = %q, want %q", envelope.Method, MethodInitialized)
			return
		}
		if len(envelope.ID) != 0 {
			serverDone <- fmt.Errorf("%s should be notification with no id", MethodInitialized)
			return
		}

		serverDone <- nil
	}()

	result, err := client.Initialize(context.Background(), InitializeParams{
		RootURI:      "file:///workspace",
		Capabilities: map[string]any{},
		ClientInfo: &ClientInfo{
			Name: "codesight-test",
		},
	})
	if err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if result.ServerInfo == nil || result.ServerInfo.Name != "test-lsp" {
		t.Fatalf("Initialize server info = %#v, want name %q", result.ServerInfo, "test-lsp")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server goroutine failed: %v", err)
	}
}

func TestClientTimeoutAndErrorPropagation(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		client, server := newClientPipe(t, WithRequestTimeout(40*time.Millisecond))
		defer closePipeClient(t, client, server)

		readDone := make(chan error, 1)
		go func() {
			_, err := readLSPMessage(server.reader)
			if err != nil {
				readDone <- fmt.Errorf("read timeout request: %w", err)
				return
			}
			readDone <- nil
		}()

		err := client.Call(context.Background(), "codesight/hang", map[string]any{"value": 1}, nil)
		if err == nil {
			t.Fatal("Call returned nil error, want timeout")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Call error = %v, want context.DeadlineExceeded", err)
		}
		if !strings.Contains(err.Error(), "codesight/hang") {
			t.Fatalf("Call error = %q, want method name in message", err.Error())
		}

		if readErr := <-readDone; readErr != nil {
			t.Fatalf("server goroutine failed: %v", readErr)
		}
	})

	t.Run("server error", func(t *testing.T) {
		client, server := newClientPipe(t, WithRequestTimeout(2*time.Second))
		defer closePipeClient(t, client, server)

		serverDone := make(chan error, 1)
		go func() {
			payload, err := readLSPMessage(server.reader)
			if err != nil {
				serverDone <- fmt.Errorf("read request: %w", err)
				return
			}

			var request RequestMessage
			if err := json.Unmarshal(payload, &request); err != nil {
				serverDone <- fmt.Errorf("decode request: %w", err)
				return
			}

			response := ResponseMessage{
				JSONRPC: JSONRPCVersion,
				ID:      rawMessageFromValue(t, request.ID),
				Error: &ResponseErrorBody{
					Code:    -32001,
					Message: "server exploded",
				},
			}
			if err := writeServerMessage(server.writer, response); err != nil {
				serverDone <- fmt.Errorf("write response: %w", err)
				return
			}

			serverDone <- nil
		}()

		err := client.Call(context.Background(), "codesight/fail", nil, nil)
		if err == nil {
			t.Fatal("Call returned nil error, want response error")
		}

		var responseErr *ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("Call error = %T, want *ResponseError", err)
		}
		if responseErr.Body.Code != -32001 {
			t.Fatalf("ResponseError code = %d, want %d", responseErr.Body.Code, -32001)
		}
		if responseErr.Body.Message != "server exploded" {
			t.Fatalf("ResponseError message = %q, want %q", responseErr.Body.Message, "server exploded")
		}

		if serverErr := <-serverDone; serverErr != nil {
			t.Fatalf("server goroutine failed: %v", serverErr)
		}
	})
}

func TestClientConcurrentRequestHandling(t *testing.T) {
	client, server := newClientPipe(t, WithRequestTimeout(2*time.Second))
	defer closePipeClient(t, client, server)

	const requestCount = 16
	serverDone := make(chan error, 1)
	go func() {
		type requestRecord struct {
			ID    int64
			Index int
		}

		records := make([]requestRecord, 0, requestCount)
		for i := 0; i < requestCount; i++ {
			payload, err := readLSPMessage(server.reader)
			if err != nil {
				serverDone <- fmt.Errorf("read request %d: %w", i, err)
				return
			}

			var request struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
				Params struct {
					Index int `json:"index"`
				} `json:"params"`
			}
			if err := json.Unmarshal(payload, &request); err != nil {
				serverDone <- fmt.Errorf("decode request %d: %w", i, err)
				return
			}
			if request.Method != "codesight/concurrent" {
				serverDone <- fmt.Errorf("request method = %q, want %q", request.Method, "codesight/concurrent")
				return
			}

			records = append(records, requestRecord{
				ID:    request.ID,
				Index: request.Params.Index,
			})
		}

		for i := len(records) - 1; i >= 0; i-- {
			response := ResponseMessage{
				JSONRPC: JSONRPCVersion,
				ID:      rawMessageFromValue(t, records[i].ID),
				Result:  rawMessageFromValue(t, map[string]int{"index": records[i].Index}),
			}
			if err := writeServerMessage(server.writer, response); err != nil {
				serverDone <- fmt.Errorf("write response %d: %w", i, err)
				return
			}
		}

		serverDone <- nil
	}()

	errs := make(chan error, requestCount)
	var waitGroup sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		index := i
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			var result struct {
				Index int `json:"index"`
			}
			if err := client.Call(context.Background(), "codesight/concurrent", map[string]int{"index": index}, &result); err != nil {
				errs <- fmt.Errorf("request %d call failed: %w", index, err)
				return
			}
			if result.Index != index {
				errs <- fmt.Errorf("request %d result index = %d", index, result.Index)
			}
		}()
	}

	waitGroup.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("%v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server goroutine failed: %v", err)
	}
}

func TestClientGracefulShutdown(t *testing.T) {
	client, server := newClientPipe(t, WithRequestTimeout(2*time.Second))
	defer closePipeClient(t, client, server)

	serverDone := make(chan error, 1)
	go func() {
		shutdownPayload, err := readLSPMessage(server.reader)
		if err != nil {
			serverDone <- fmt.Errorf("read shutdown request: %w", err)
			return
		}

		var shutdownRequest RequestMessage
		if err := json.Unmarshal(shutdownPayload, &shutdownRequest); err != nil {
			serverDone <- fmt.Errorf("decode shutdown request: %w", err)
			return
		}
		if shutdownRequest.Method != MethodShutdown {
			serverDone <- fmt.Errorf("first method = %q, want %q", shutdownRequest.Method, MethodShutdown)
			return
		}

		shutdownResponse := ResponseMessage{
			JSONRPC: JSONRPCVersion,
			ID:      rawMessageFromValue(t, shutdownRequest.ID),
			Result:  rawMessageFromValue(t, nil),
		}
		if err := writeServerMessage(server.writer, shutdownResponse); err != nil {
			serverDone <- fmt.Errorf("write shutdown response: %w", err)
			return
		}

		exitPayload, err := readLSPMessage(server.reader)
		if err != nil {
			serverDone <- fmt.Errorf("read exit notification: %w", err)
			return
		}

		var exitEnvelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(exitPayload, &exitEnvelope); err != nil {
			serverDone <- fmt.Errorf("decode exit envelope: %w", err)
			return
		}
		if exitEnvelope.Method != MethodExit {
			serverDone <- fmt.Errorf("second method = %q, want %q", exitEnvelope.Method, MethodExit)
			return
		}
		if len(exitEnvelope.ID) != 0 {
			serverDone <- fmt.Errorf("%s should be notification with no id", MethodExit)
			return
		}

		if _, err := readLSPMessage(server.reader); err == nil {
			serverDone <- errors.New("expected transport close after exit notification")
			return
		}

		serverDone <- nil
	}()

	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("server goroutine failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server goroutine")
	}
}

func TestClientDeferredRefsCommandIntegration(t *testing.T) {
	t.Skip("blocked by TK-006: refs command integration not wired")
}

type pipeServer struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
}

func newClientPipe(t *testing.T, opts ...ClientOption) (*Client, *pipeServer) {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	client, err := NewClient(clientConn, opts...)
	if err != nil {
		_ = clientConn.Close()
		_ = serverConn.Close()
		t.Fatalf("NewClient returned error: %v", err)
	}

	return client, &pipeServer{
		conn:   serverConn,
		reader: bufio.NewReader(serverConn),
		writer: bufio.NewWriter(serverConn),
	}
}

func closePipeClient(t *testing.T, client *Client, server *pipeServer) {
	t.Helper()

	if client != nil {
		if err := client.Close(); err != nil {
			t.Fatalf("client.Close returned error: %v", err)
		}
	}
	if server != nil && server.conn != nil {
		if err := server.conn.Close(); err != nil {
			t.Fatalf("server conn close returned error: %v", err)
		}
	}
}

func writeServerMessage(writer *bufio.Writer, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return writeLSPMessage(writer, payload)
}

func rawMessageFromValue(t *testing.T, value any) json.RawMessage {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%v) returned error: %v", value, err)
	}
	return raw
}
