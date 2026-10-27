package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DeadNotifier 是 transport 的可选能力：底层链路终判死亡（stdio 子进程退出、
// 读流失败等）时调用回调。上层用它把连接状态从"活"改为"死"并触发自动重连，
// 而不必等下一次工具调用踩雷。
type DeadNotifier interface {
	OnDead(fn func(error))
}

// StdioTransport implements MCP transport over stdio.
//
// Responses are demultiplexed by JSON-RPC id: a single reader goroutine drains
// stdout and hands each response to the pending caller that owns that id. This
// keeps concurrent Send calls from stealing each other's responses (a plain
// per-call ReadBytes would let two in-flight requests race for whichever line
// arrives first).
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr io.ReadCloser

	// mu 保护 pending/closed/readErr/deadFn。注意：**绝不在持 mu 时写
	// stdin**——子进程不消费 stdin 时 Write 可能无限阻塞，那会把 deliver/
	// failAll 全部卡死，整个 transport 死锁。
	mu      sync.Mutex
	writeMu sync.Mutex // 单独序列化 stdin 写入
	closed  bool
	pending map[string]chan *JSONRPCResponse
	readErr error // terminal read error, surfaced to all waiters
	deadFn  func(error)
}

// requestIDString normalizes a JSON-RPC id (string or number) so it can be used
// as a map key. The wire form the server echoes back is the one we stored.
func requestIDString(id interface{}) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		// JSON numbers decode to float64; format without a trailing ".0".
		return strconv.FormatInt(int64(v), 10)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
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

	t := &StdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  stderr,
		pending: make(map[string]chan *JSONRPCResponse),
	}
	go t.readLoop(stdout)
	return t, nil
}

// readLoop is the single owner of the stdout stream. It reads one JSON-RPC
// message per line and routes it to the waiter registered for that id.
func (t *StdioTransport) readLoop(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var resp JSONRPCResponse
			if jsonErr := json.Unmarshal(bytes.TrimSpace(line), &resp); jsonErr != nil {
				// A malformed line should not kill the stream; keep reading.
				continue
			}
			t.deliver(&resp)
		}
		if err != nil {
			t.failAll(err)
			// 链路死亡通知放锁外调用，且只调一次（failAll 已把 readErr 固定）。
			t.mu.Lock()
			fn := t.deadFn
			t.mu.Unlock()
			if fn != nil {
				fn(err)
			}
			return
		}
	}
}

// deliver hands a response to the waiter registered for its id. The send
// happens while holding the lock so a concurrent failAll (which closes the
// channel after swapping the pending map) can never race with it.
func (t *StdioTransport) deliver(resp *JSONRPCResponse) {
	key := requestIDString(resp.ID)
	t.mu.Lock()
	ch, ok := t.pending[key]
	if ok {
		delete(t.pending, key)
		select {
		case ch <- resp: // buffered(1): never blocks
		default:
		}
	}
	t.mu.Unlock()
}

// failAll records the terminal error and wakes every pending waiter.
func (t *StdioTransport) failAll(err error) {
	t.mu.Lock()
	if t.readErr == nil {
		t.readErr = err
	}
	waiters := t.pending
	t.pending = make(map[string]chan *JSONRPCResponse)
	for _, ch := range waiters {
		close(ch)
	}
	t.mu.Unlock()
}

// Send sends a JSON-RPC request and returns the response.
//
// Requests without an id (notifications) are fire-and-forget: they are written
// but never awaited, since the server sends no reply for them.
func (t *StdioTransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	key := requestIDString(req.ID)
	isNotification := req.ID == nil

	// Marshal the request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respCh := make(chan *JSONRPCResponse, 1)
	if !isNotification {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return nil, fmt.Errorf("transport is closed")
		}
		if t.readErr != nil {
			err := t.readErr
			t.mu.Unlock()
			return nil, fmt.Errorf("transport read failed: %w", err)
		}
		t.pending[key] = respCh
		t.mu.Unlock()
	}

	// Write to stdin under its own lock: a hung child that stops draining
	// stdin can block Write forever, and that must not take down the pending
	// map (deliver/failAll) with it — see the mutex comment on the struct.
	t.writeMu.Lock()
	_, werr := t.stdin.Write(append(data, '\n'))
	t.writeMu.Unlock()
	if werr != nil {
		t.unregister(key, respCh)
		return nil, fmt.Errorf("failed to write to stdin: %w", werr)
	}

	if isNotification {
		return &JSONRPCResponse{JSONRPC: JSONRPCVersion}, nil
	}

	select {
	case <-ctx.Done():
		// A late response may still arrive after the caller gave up; the
		// reader will drop it because the waiter is no longer registered.
		t.unregister(key, respCh)
		return nil, ctx.Err()
	case resp, ok := <-respCh:
		if !ok {
			t.mu.Lock()
			err := t.readErr
			t.mu.Unlock()
			if err == io.EOF {
				// Try to read from stderr for error info
				if errBytes, _ := io.ReadAll(t.stderr); len(errBytes) > 0 {
					return nil, fmt.Errorf("MCP server stderr: %s", string(errBytes))
				}
			}
			if err == nil {
				err = io.EOF
			}
			return nil, fmt.Errorf("failed to read from stdout: %w", err)
		}
		return resp, nil
	}
}

// unregister removes a pending waiter, guarding against a concurrent deliver
// that already took ownership of it.
func (t *StdioTransport) unregister(key string, ch chan *JSONRPCResponse) {
	t.mu.Lock()
	if cur, ok := t.pending[key]; ok && cur == ch {
		delete(t.pending, key)
	}
	t.mu.Unlock()
}

// OnDead registers the link-death callback (see DeadNotifier).
func (t *StdioTransport) OnDead(fn func(error)) {
	t.mu.Lock()
	t.deadFn = fn
	t.mu.Unlock()
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

// SSETransport 是远程 MCP 的 HTTP 传输。
//
// 它实现的是 Streamable HTTP 的语义（每个请求一次 POST，响应可能是普通
// JSON，也可能是一条 SSE 流），同时兼容早期的 SSE 端点——两者的差别只在
// 服务端怎么回，客户端行为一致。类型名沿用 SSE 是历史原因（见
// NormalizeTransport 的说明）。
type SSETransport struct {
	url        string
	client     *http.Client
	mu         sync.Mutex
	closed     bool
	reconnect  bool
	maxRetries int
	retryDelay time.Duration
	// sessionID 是服务端在首个响应里通过 Mcp-Session-Id 派发的会话标识。
	// Streamable HTTP 的有状态 server 要求后续请求原样回传，丢掉它会导致
	// 第二个请求起 404/400（"Missing session ID"）。
	sessionID string
	// headers 是用户为该连接器配置的额外请求头（Authorization / X-API-Key
	// 等）。按值拷贝，构造后不再改写，因此无锁读取是安全的。
	headers map[string]string
}

// defaultSSEConnectTimeout 是 SSE/Streamable-HTTP 传输单次 HTTP 请求的超时。
//
// 这个值必须**明显小于**前端 request() 的 30s AbortController 超时，
// 否则两端同时踩线：后端还在等握手、前端已经 abort，用户看到"保存转圈
// 到失败"，且拿不到任何原因。留出 2 倍余量后，后端总能在前端超时前
// 返回一条带原因的错误。
const defaultSSEConnectTimeout = 15 * time.Second

// NewSSETransport creates a new SSE transport
func NewSSETransport(url string) (*SSETransport, error) {
	return NewSSETransportWithTimeout(url, defaultSSEConnectTimeout)
}

// NewSSETransportWithTimeout 用指定超时构造 SSE 传输（<=0 时取默认值）。
// 供需要调整握手预算的调用方使用。
func NewSSETransportWithTimeout(url string, timeout time.Duration) (*SSETransport, error) {
	return NewSSETransportWithOptions(url, nil, timeout)
}

// NewSSETransportWithOptions 构造可带自定义请求头的远程传输。
//
// headers 目前承载的是认证：绝大多数云端 MCP server 只认 Authorization
// 或 X-API-Key。以前没有这个入口，用户只能把 token 拼进 URL query，既
// 会泄漏到日志，也没法通过 CORS/网关校验。这里做一次拷贝，防止调用方
// 之后修改 map 造成并发读写。
func NewSSETransportWithOptions(url string, headers map[string]string, timeout time.Duration) (*SSETransport, error) {
	if timeout <= 0 {
		timeout = defaultSSEConnectTimeout
	}
	var copied map[string]string
	if len(headers) > 0 {
		copied = make(map[string]string, len(headers))
		for k, v := range headers {
			copied[strings.TrimSpace(k)] = v
		}
	}
	return &SSETransport{
		url:        url,
		client:     &http.Client{Timeout: timeout},
		closed:     false,
		reconnect:  true,
		maxRetries: 3,
		retryDelay: time.Second,
		headers:    copied,
	}, nil
}

// retryableMethod reports whether a JSON-RPC method may safely be retried.
// Only read-only / idempotent methods qualify: tools/call may have side effects
// and must never be replayed automatically, even if the response was lost.
func retryableMethod(method string) bool {
	switch method {
	case "initialize", "ping", "tools/list", "resources/list",
		"prompts/list", "notifications/initialized":
		return true
	default:
		return false
	}
}

// Send sends a JSON-RPC request and returns the response
func (t *SSETransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport is closed")
	}
	t.mu.Unlock()

	// Non-idempotent calls get a single attempt: on a lost response we must not
	// risk running the tool twice.
	maxAttempts := 0
	if retryableMethod(req.Method) {
		maxAttempts = t.maxRetries
	}

	// 重试共享调用方的超时预算（ctx），而每次 doRequest 自己还有 15s 的
	// client.Timeout。若不设防，3 次重试最坏会花掉 3×15s + 退避，把
	// initialize 的整体预算吃光，最终只吐出一句 "context deadline exceeded"
	// ——用户看不出到底是 DNS、超时还是服务端拒绝。
	//
	// 这里要求每次尝试前 ctx 至少还剩 attemptBudget：不够就停止重试并
	// 原样返回上一次的真实错误，让"连接超时"这类可操作信息活到最后。
	const attemptBudget = 3 * time.Second

	var lastErr error
	for attempt := 0; attempt <= maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, lastErr2(ctx.Err(), lastErr)
			case <-time.After(t.retryDelay * time.Duration(attempt)):
			}
			// 预算不够再做一次完整尝试就放弃，保留上一次的具体错误。
			if dl, ok := ctx.Deadline(); ok && time.Until(dl) < attemptBudget {
				return nil, fmt.Errorf("SSE request gave up after %d attempt(s): %w", attempt, lastErr)
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

	if maxAttempts == 0 {
		return nil, lastErr
	}
	return nil, fmt.Errorf("SSE request failed after %d attempts: %w", maxAttempts, lastErr)
}

// lastErr2 在超时/取消时优先保留上一次的具体错误：ctx 错误本身
// （"context deadline exceeded"）对用户没有任何诊断价值，而 lastErr
// 里带着 DNS 失败 / 连接被拒 / 超时这类可操作原因。
func lastErr2(ctxErr, lastErr error) error {
	if lastErr != nil {
		return lastErr
	}
	return ctxErr
}

// acceptHeader is what a Streamable-HTTP MCP client must advertise. The spec
// requires the client to accept BOTH a JSON-RPC response and an SSE stream: a
// server that gets only "text/event-stream" is entitled to reject the request
// with 406 Not Acceptable (this is what broke ms-learn before).
const acceptHeader = "application/json, text/event-stream"

// maxMCPResponseBytes bounds how much of a response body we buffer, so a
// misbehaving server cannot make us allocate unbounded memory.
const maxMCPResponseBytes = 8 << 20 // 8MB

// classifyHTTPError turns a transport-level net/http error into a message that
// says what actually went wrong, rather than a generic "HTTP request failed".
// DNS failures, refused connections and timeouts are the three the UI needs to
// distinguish (each has a different fix).
func classifyHTTPError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	// 所有分支都是"够不着对端"，统一归 network。分类在 wrap 时打标签，
	// 单条消息保持原样（既有测试与 UI 文案都依赖这些字符串）。
	wrap := func(msg string) error { return WrapKind(ErrKindNetwork, fmt.Errorf("%s: %w", msg, err)) }

	switch {
	case errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || strings.Contains(lower, "timeout"):
		return wrap("connection timed out")
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "getaddrinfo") ||
		strings.Contains(lower, "lookup ") || strings.Contains(lower, "dns"):
		return wrap("DNS lookup failed (check the URL host or your network)")
	case strings.Contains(lower, "connection refused"):
		return wrap("connection refused (is the server running and reachable?)")
	case strings.Contains(lower, "no route to host") || strings.Contains(lower, "network is unreachable"):
		return wrap("host unreachable (check network/VPN)")
	case strings.Contains(lower, "certificate") || strings.Contains(lower, "tls"):
		return wrap("TLS/certificate error")
	default:
		return wrap("HTTP request failed")
	}
}

// httpStatusError builds a descriptive error for a non-200 response, folding in
// the server's own explanation when it sent one. 返回值带错误分类（401/403 →
// auth、5xx → server、406/400 → protocol、404/405 → not_found），供 UI 决定
// 给出"去补凭据"还是"去改 URL"。
func httpStatusError(status int, contentType, body string) error {
	return WrapKind(ClassifyStatus(status), httpStatusErrorPlain(status, contentType, body))
}

func httpStatusErrorPlain(status int, contentType, body string) error {
	body = strings.TrimSpace(body)
	if body != "" {
		if isJSONContentType(contentType) || strings.HasPrefix(body, "{") {
			// Prefer the error/message field when the server sent structured JSON.
			var payload struct {
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if json.Unmarshal([]byte(body), &payload) == nil {
				if payload.Error != "" {
					return fmt.Errorf("HTTP %d: %s", status, payload.Error)
				}
				if payload.Message != "" {
					return fmt.Errorf("HTTP %d: %s", status, payload.Message)
				}
			}
		}
		if len(body) > 300 {
			body = body[:300] + "…"
		}
		return fmt.Errorf("HTTP %d: %s", status, body)
	}

	// No body: at least name the common statuses so the operator knows where to look.
	switch status {
	case http.StatusNotAcceptable:
		return fmt.Errorf("HTTP 406 Not Acceptable: server rejected the Accept/Content-Type negotiation")
	case http.StatusUnauthorized:
		return fmt.Errorf("HTTP 401 Unauthorized: the server requires credentials")
	case http.StatusForbidden:
		return fmt.Errorf("HTTP 403 Forbidden: access denied")
	case http.StatusNotFound:
		return fmt.Errorf("HTTP 404 Not Found: check the endpoint path")
	case http.StatusMethodNotAllowed:
		return fmt.Errorf("HTTP 405 Method Not Allowed: the endpoint may not accept POST")
	default:
		return fmt.Errorf("HTTP %d", status)
	}
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
	httpReq.Header.Set("Accept", acceptHeader)
	// Protocol negotiation header used by Streamable HTTP servers; harmless for
	// older SSE-only endpoints.
	httpReq.Header.Set("MCP-Protocol-Version", ProtocolVersion)

	// 用户配置的请求头放最后：认证头（Authorization / X-API-Key）是这里
	// 唯一的用途，必须能覆盖上面的默认值。
	for k, v := range t.headers {
		if k != "" {
			httpReq.Header.Set(k, v)
		}
	}

	// 有状态 server：回传它派发的会话 id。
	t.mu.Lock()
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, classifyHTTPError(err)
	}
	defer resp.Body.Close()

	// 首个响应可能携带 Mcp-Session-Id；记住它，后续请求带上。
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		if t.sessionID == "" {
			t.sessionID = sid
		}
		t.mu.Unlock()
	}

	// 202 Accepted / 204 No Content 是通知类请求（notifications/initialized
	// 等）的正常回执：服务端接受了消息，但没有响应体。以前这里按"非 200"
	// 处理，直接把一次成功的握手通知变成错误——对采用标准语义的服务端，
	// 连接会因此在握手阶段就失败。
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return &JSONRPCResponse{JSONRPC: JSONRPCVersion}, nil
	}

	if resp.StatusCode != http.StatusOK {
		// Read a bounded slice of the body: servers usually explain the failure
		// (e.g. "Missing session ID") in the payload, which is far more useful
		// than a bare status code in the UI.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, httpStatusError(resp.StatusCode, resp.Header.Get("Content-Type"), string(snippet))
	}

	// Streamable HTTP servers may answer with either a plain JSON body or an SSE
	// stream, depending on the request and their own policy. Route on the
	// Content-Type instead of assuming SSE.
	if isJSONContentType(resp.Header.Get("Content-Type")) {
		return parseJSONRPCBody(resp.Body)
	}
	return t.parseSSEResponse(resp.Body)
}

// isJSONContentType reports whether a response Content-Type denotes a plain
// JSON body (as opposed to an SSE stream or anything else).
func isJSONContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct == "application/json" || strings.HasSuffix(ct, "+json")
}

// parseJSONRPCBody decodes a plain JSON JSON-RPC response (the non-streaming
// shape of a Streamable HTTP reply).
func parseJSONRPCBody(body io.Reader) (*JSONRPCResponse, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxMCPResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response body")
	}

	// A JSON body may still be a batch (array); we only ever send single
	// requests, so accept either a single object or pick the first element.
	if trimmed[0] == '[' {
		var batch []JSONRPCResponse
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON-RPC batch response: %w", err)
		}
		if len(batch) == 0 {
			return nil, fmt.Errorf("empty JSON-RPC batch response")
		}
		return &batch[0], nil
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(trimmed, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON-RPC response: %w", err)
	}
	return &resp, nil
}

func (t *SSETransport) parseSSEResponse(body io.Reader) (*JSONRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxMCPResponseBytes)

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
			if strings.HasPrefix(data, "{") || strings.HasPrefix(data, "[") {
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

	// A transport-level error event ("event: error") is a failure, not a
	// response — its payload carries {code, message} rather than a JSON-RPC
	// envelope. Surface it instead of silently unmarshalling an empty result.
	if eventType == "error" {
		var sseErr struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(responseData), &sseErr); err == nil && sseErr.Message != "" {
			if sseErr.Code != "" {
				return nil, fmt.Errorf("SSE server error [%s]: %s", sseErr.Code, sseErr.Message)
			}
			return nil, fmt.Errorf("SSE server error: %s", sseErr.Message)
		}
		return nil, fmt.Errorf("SSE server error: %s", responseData)
	}

	// Reuse the plain-JSON decoder: an SSE data line is a JSON-RPC envelope,
	// and some servers deliver it as a batch array even over SSE.
	return parseJSONRPCBody(strings.NewReader(responseData))
}

// Close closes the SSE transport.
//
// 对有状态的 Streamable HTTP server，规范约定客户端用 DELETE + Mcp-Session-Id
// 主动结束会话；不这么做，服务端会一直挂着一个没人用的会话（进程内资源、
// 上游连接），反复重连会累积。失败不影响关闭——那只是"尽力清理"。
func (t *SSETransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.reconnect = false
	session := t.sessionID
	t.mu.Unlock()

	if session != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil)
		if err == nil {
			req.Header.Set("Mcp-Session-Id", session)
			for k, v := range t.headers {
				if k != "" {
					req.Header.Set(k, v)
				}
			}
			if resp, doErr := t.client.Do(req); doErr == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
			}
		}
	}
	return nil
}

// SetReconnect enables or disables automatic reconnection
func (t *SSETransport) SetReconnect(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reconnect = enabled
}
