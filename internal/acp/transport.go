package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Transport interface for ACP transport layers
type Transport interface {
	Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error)
	Receive(ctx context.Context) (*JSONRPCRequest, error)
	Respond(ctx context.Context, resp *JSONRPCResponse) error
	Close() error
}

// StdioTransport implements ACP transport over stdio
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser
	mu     sync.Mutex
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(command string, args, env []string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ACP server: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: stderr,
	}, nil
}

// NewStdioTransportFromProcess creates a stdio transport from an existing process
func NewStdioTransportFromProcess(cmd *exec.Cmd) (*StdioTransport, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ACP server: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: stderr,
	}, nil
}

// Send sends a JSON-RPC response
func (t *StdioTransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Marshal the response
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	// Write to stdin with newline delimiter
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil, nil
}

// Receive receives a JSON-RPC request (blocking)
func (t *StdioTransport) Receive(ctx context.Context) (*JSONRPCRequest, error) {
	type result struct {
		req *JSONRPCRequest
		err error
	}

	done := make(chan result, 1)
	go func() {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			done <- result{err: fmt.Errorf("failed to read from stdout: %w", err)}
			return
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(bytes.TrimSpace(line), &req); err != nil {
			done <- result{err: fmt.Errorf("failed to unmarshal request: %w", err)}
			return
		}

		done <- result{req: &req}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		return r.req, r.err
	}
}

// Respond sends a JSON-RPC response (for server-side use)
func (t *StdioTransport) Respond(ctx context.Context, resp *JSONRPCResponse) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Marshal the response
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	// Write to stdin with newline delimiter
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// Close closes the stdio transport
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var errs []error

	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if t.cmd.Process != nil {
		if err := t.cmd.Process.Kill(); err != nil {
			errs = append(errs, err)
		}
	}

	if err := t.cmd.Wait(); err != nil {
		errs = append(errs, err)
	}

	// Drain stderr
	go io.Copy(io.Discard, t.stderr)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing transport: %v", errs)
	}
	return nil
}

// HTTPTransport implements ACP transport over HTTP
type HTTPTransport struct {
	client   *http.Client
	baseURL  string
	headers  map[string]string
	mu       sync.Mutex
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(baseURL string, headers map[string]string) *HTTPTransport {
	return &HTTPTransport{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: strings.TrimSuffix(baseURL, "/"),
		headers: headers,
	}
}

// Send sends a JSON-RPC request and returns the response
func (t *HTTPTransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/rpc", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &jsonResp, nil
}

// Receive receives a JSON-RPC request (not applicable for HTTP)
func (t *HTTPTransport) Receive(ctx context.Context) (*JSONRPCRequest, error) {
	return nil, fmt.Errorf("Receive not supported for HTTP transport")
}

// Respond sends a JSON-RPC response (no-op for HTTP transport)
func (t *HTTPTransport) Respond(ctx context.Context, resp *JSONRPCResponse) error {
	return nil
}

// Close closes the HTTP transport (no-op)
func (t *HTTPTransport) Close() error {
	return nil
}

// SSETransport implements ACP transport over Server-Sent Events
type SSETransport struct {
	url        string
	client     *http.Client
	eventChan  chan string
	headers    map[string]string
	mu         sync.Mutex
	connected  bool
	cancelFunc context.CancelFunc
}

// NewSSETransport creates a new SSE transport
func NewSSETransport(url string, headers map[string]string) (*SSETransport, error) {
	return &SSETransport{
		url:       url,
		client:    &http.Client{Timeout: 0}, // No timeout for SSE
		eventChan: make(chan string, 100),
		headers:   headers,
		connected: true,
	}, nil
}

// Send sends a JSON-RPC request and returns the response
func (t *SSETransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if !t.connected {
		t.mu.Unlock()
		return nil, fmt.Errorf("SSE transport not connected")
	}
	t.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Read SSE events
	scanner := bufio.NewScanner(resp.Body)
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	var responseData string

	for scanner.Scan() {
		line := scanner.Text()

		// Parse SSE format: data: {...}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "" || data == ":" {
				continue
			}
			responseData = data
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE read error: %w", err)
	}

	if responseData == "" {
		return nil, fmt.Errorf("no response data received")
	}

	var jsonResp JSONRPCResponse
	if err := json.Unmarshal([]byte(responseData), &jsonResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &jsonResp, nil
}

// Receive receives a JSON-RPC request via SSE (for inbound requests)
func (t *SSETransport) Receive(ctx context.Context) (*JSONRPCRequest, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case data, ok := <-t.eventChan:
		if !ok {
			return nil, fmt.Errorf("event channel closed")
		}
		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(data), &req); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
		return &req, nil
	}
}

// Respond sends a JSON-RPC response (no-op for SSE transport)
func (t *SSETransport) Respond(ctx context.Context, resp *JSONRPCResponse) error {
	return nil
}

// Close closes the SSE transport
func (t *SSETransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.connected = false
	close(t.eventChan)
	return nil
}

// TCPTransport implements ACP transport over TCP
type TCPTransport struct {
	conn        net.Conn
	address     string
	listener    net.Listener
	isServer    bool
	mu          sync.Mutex
	readTimeout time.Duration
	writeTimeout time.Duration
}

// NewTCPTransport creates a new TCP transport client
func NewTCPTransport(address string) (*TCPTransport, error) {
	return &TCPTransport{
		address:      address,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
	}, nil
}

// NewTCPTransportAsServer creates a new TCP transport server
func NewTCPTransportAsServer(address string) (*TCPTransport, error) {
	return &TCPTransport{
		address:      address,
		isServer:     true,
		readTimeout:  0, // No timeout for server
		writeTimeout: 30 * time.Second,
	}, nil
}

// Connect establishes the TCP connection
func (t *TCPTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	conn, err := dialer.DialContext(ctx, "tcp", t.address)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", t.address, err)
	}

	t.conn = conn
	return nil
}

// Listen starts the TCP server
func (t *TCPTransport) Listen(ctx context.Context) error {
	if !t.isServer {
		return fmt.Errorf("Listen only available for server transport")
	}

	ln, err := net.Listen("tcp", t.address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", t.address, err)
	}

	t.listener = ln
	go t.acceptLoop(ctx)
	return nil
}

// acceptLoop accepts incoming connections
func (t *TCPTransport) acceptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		t.mu.Lock()
		ln := t.listener
		t.mu.Unlock()

		if ln == nil {
			return
		}

		ln.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
		conn, err := ln.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		// Handle connection
		go t.handleConnection(conn)
	}
}

// handleConnection handles a single TCP connection
func (t *TCPTransport) handleConnection(conn net.Conn) {
	t.mu.Lock()
	if t.conn == nil {
		t.conn = conn
	}
	t.mu.Unlock()

	// For now, just keep the connection alive
	// In a full implementation, this would handle multiple sessions
}

// Send sends a JSON-RPC request
func (t *TCPTransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("TCP transport not connected")
	}

	// Marshal the request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Write with newline delimiter
	if t.writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(t.writeTimeout))
	}

	_, err = conn.Write(append(data, '\n'))
	if err != nil {
		return nil, fmt.Errorf("failed to write to TCP: %w", err)
	}

	// Read response
	if t.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(t.readTimeout))
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read from TCP: %w", err)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// Receive receives a JSON-RPC request
func (t *TCPTransport) Receive(ctx context.Context) (*JSONRPCRequest, error) {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("TCP transport not connected")
	}

	// Read request
	if t.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(t.readTimeout))
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("connection closed")
		}
		return nil, fmt.Errorf("failed to read from TCP: %w", err)
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(bytes.TrimSpace(line), &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	return &req, nil
}

// Close closes the TCP connection
func (t *TCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var errs []error

	if t.conn != nil {
		if err := t.conn.Close(); err != nil {
			errs = append(errs, err)
		}
		t.conn = nil
	}

	if t.listener != nil {
		if err := t.listener.Close(); err != nil {
			errs = append(errs, err)
		}
		t.listener = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing TCP transport: %v", errs)
	}
	return nil
}

// IsConnected checks if the TCP connection is alive
func (t *TCPTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn == nil {
		return false
	}

	// Check if connection is still valid by setting a zero deadline
	t.conn.SetDeadline(time.Now())
	return true
}

// mustMarshal marshals a value to JSON or panics
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

