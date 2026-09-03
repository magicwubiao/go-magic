package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StdioTransport implements MCP transport over stdio
type StdioTransport struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stderr     io.ReadCloser
	mu         sync.Mutex
	closed     bool
	processErr error
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(command string, args, env []string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)

	// Set platform-specific process attributes
	setProcessAttrs(cmd)

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
		return nil, fmt.Errorf("failed to start MCP server: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: stderr,
	}, nil
}

// Send sends a JSON-RPC request and returns the response
func (t *StdioTransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport is closed")
	}
	t.mu.Unlock()

	// Marshal the request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Write to stdin with newline delimiter
	t.mu.Lock()
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to write to stdin: %w", err)
	}
	t.mu.Unlock()

	// Read response with context timeout
	respCh := make(chan *JSONRPCResponse, 1)
	errCh := make(chan error, 1)

	go func() {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				// Try to read from stderr for error info
				errBytes, _ := io.ReadAll(t.stderr)
				if len(errBytes) > 0 {
					errCh <- fmt.Errorf("MCP server stderr: %s", string(errBytes))
					return
				}
			}
			errCh <- fmt.Errorf("failed to read from stdout: %w", err)
			return
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
			errCh <- fmt.Errorf("failed to unmarshal response: %w", err)
			return
		}
		respCh <- &resp
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	case err := <-errCh:
		return nil, err
	}
}

// Close closes the stdio transport gracefully
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	var errs []error

	// Close stdin first to signal EOF to the process
	if err := t.stdin.Close(); err != nil {
		errs = append(errs, fmt.Errorf("stdin close error: %w", err))
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			// Check if process already exited
			if _, ok := err.(*exec.ExitError); ok {
				// Normal exit, no error
			} else {
				errs = append(errs, fmt.Errorf("process wait error: %w", err))
			}
		}
	case <-time.After(5 * time.Second):
		// Force kill if process doesn't exit gracefully
		if t.cmd.Process != nil {
			// Kill process group using platform-specific method
			killProcessGroup(t.cmd.Process)
		}
		// Wait for the process to be killed
		<-done
		errs = append(errs, fmt.Errorf("process force killed after timeout"))
	}

	// Drain stderr
	go io.Copy(io.Discard, t.stderr)

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// SSETransport implements MCP transport over Server-Sent Events
type SSETransport struct {
	url        string
	client     *http.Client
	eventChan  chan string
	mu         sync.Mutex
	closed     bool
	reconnect  bool
	maxRetries int
	retryDelay time.Duration
}

// NewSSETransport creates a new SSE transport
func NewSSETransport(url string) (*SSETransport, error) {
	return &SSETransport{
		url:        url,
		client:     &http.Client{Timeout: 30 * time.Second},
		eventChan:  make(chan string, 100),
		closed:     false,
		reconnect:  true,
		maxRetries: 3,
		retryDelay: time.Second,
	}, nil
}

// Send sends a JSON-RPC request and returns the response
func (t *SSETransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport is closed")
	}
	t.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(t.retryDelay * time.Duration(attempt)):
			}
		}

		resp, err := t.doRequest(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		t.mu.Lock()
		if t.closed || !t.reconnect {
			t.mu.Unlock()
			break
		}
		t.mu.Unlock()
	}

	return nil, fmt.Errorf("SSE request failed after %d attempts: %w", t.maxRetries, lastErr)
}

func (t *SSETransport) doRequest(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
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

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	return t.parseSSEResponse(resp.Body)
}

func (t *SSETransport) parseSSEResponse(body io.Reader) (*JSONRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	var responseData string
	var eventType string

	for scanner.Scan() {
		line := scanner.Text()

		// Parse SSE format
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			// Skip heartbeat/comments
			if data == "" || data == ":" {
				continue
			}
			// If this is a JSON-RPC response, parse it
			if strings.HasPrefix(data, "{") {
				responseData = data
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read SSE response: %w", err)
	}

	if responseData == "" {
		return nil, fmt.Errorf("no data received from SSE")
	}

	var jsonResp JSONRPCResponse
	if err := json.Unmarshal([]byte(responseData), &jsonResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON-RPC response: %w", err)
	}

	_ = eventType // eventType can be used for routing different event types

	return &jsonResp, nil
}

// Close closes the SSE transport
func (t *SSETransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	t.reconnect = false
	close(t.eventChan)
	return nil
}

// SetReconnect enables or disables automatic reconnection
func (t *SSETransport) SetReconnect(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reconnect = enabled
}
