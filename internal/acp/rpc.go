package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// JSON-RPC 2.0 标准错误码（自定义码从 -32000 起，见 error.go 的使用点）。
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// RPCError 是 JSON-RPC 错误对象，实现 error 接口。
//
// 保留 Code/Data 而不是只留字符串：ACP 里 -32000 附近的错误含义由规范定义
// （认证失败、会话未知等），调用方要能按码分流；旧版把整个错误压成
// fmt.Errorf("...: %s", msg)，只能靠字符串匹配。
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// MethodNotFound 判断错误是否为"方法不存在"（-32601）。
// ACP 允许对端实现可选方法，调用前不该靠猜——问一次拿到 -32601 也算正常。
func (e *RPCError) MethodNotFound() bool {
	return e != nil && e.Code == codeMethodNotFound
}

// rpcEnvelope 是收发共用的封装：请求/通知/响应三种形态靠字段有无区分。
type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// rpcResponse 是写回对端的响应。Result 恒出现（空结果写 {}），
// 因为规范要求成功响应必须带 result 字段，null 会让严格的实现困惑。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// HandlerFunc 处理对端发来的**请求**并返回结果。
// 返回 *RPCError 表示协议级错误；普通 error 会被包成 -32603。
type HandlerFunc func(ctx context.Context, params json.RawMessage) (interface{}, error)

// NotifyHandler 处理对端发来的**通知**（无需响应）。
//
// 通知在读取协程里同步调用，保证 session/update 的内​​容块不会乱序；
// 因此实现必须快（只做解析与回调），不能阻塞等待用户输入。
type NotifyHandler func(params json.RawMessage)

// peer 是一条连接上的 JSON-RPC 端点。
//
// 设计要点（都是旧版踩过的坑）：
//   - 单一读取协程持有输入流，按 id 把响应投递给等待者 —— 并发 call 不会串包。
//   - 入站请求在独立协程里跑（可能阻塞在人工授权上），但并发数受限。
//   - 通知在读取协程里同步处理，保持顺序。
//   - 读流终止时 failAll，所有等待者立刻拿到明确错误，而不是等到超时。
type peer struct {
	writeFn func([]byte) error
	closeFn func() error

	writeMu sync.Mutex

	mu       sync.Mutex
	pending  map[string]chan *rpcEnvelope
	seq      int64
	closed   bool
	closeErr error

	handlers map[string]HandlerFunc
	notifies map[string]NotifyHandler

	inbound chan struct{} // 入站请求并发闸门
	wg      sync.WaitGroup
}

func newPeer(writeFn func([]byte) error, closeFn func() error) *peer {
	return &peer{
		writeFn:  writeFn,
		closeFn:  closeFn,
		pending:  make(map[string]chan *rpcEnvelope),
		handlers: make(map[string]HandlerFunc),
		notifies: make(map[string]NotifyHandler),
		inbound:  make(chan struct{}, 16),
	}
}

// Handle 注册入站请求处理器。
func (p *peer) Handle(method string, h HandlerFunc) {
	p.mu.Lock()
	p.handlers[method] = h
	p.mu.Unlock()
}

// OnNotify 注册入站通知处理器。
func (p *peer) OnNotify(method string, h NotifyHandler) {
	p.mu.Lock()
	p.notifies[method] = h
	p.mu.Unlock()
}

// call 发送请求并等待同 id 的响应。
func (p *peer) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	raw := marshalParams(params)

	p.mu.Lock()
	if p.closed {
		err := p.closeErr
		p.mu.Unlock()
		return nil, err
	}
	p.seq++
	key := strconv.FormatInt(p.seq, 10)
	ch := make(chan *rpcEnvelope, 1)
	p.pending[key] = ch
	p.mu.Unlock()

	env := rpcEnvelope{
		JSONRPC: jsonRPCVersion,
		ID:      json.RawMessage(key),
		Method:  method,
		Params:  raw,
	}
	data, err := json.Marshal(env)
	if err != nil {
		p.dropPending(key)
		return nil, fmt.Errorf("marshal %s request: %w", method, err)
	}

	if err := p.write(data); err != nil {
		p.dropPending(key)
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	select {
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf("%s: connection closed", method)
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		p.dropPending(key)
		return nil, ctx.Err()
	}
}

// notify 发送通知（不等响应）。
func (p *peer) notify(method string, params interface{}) error {
	env := rpcEnvelope{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  marshalParams(params),
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal %s notification: %w", method, err)
	}
	return p.write(data)
}

func (p *peer) write(data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	p.mu.Lock()
	closed, cerr := p.closed, p.closeErr
	p.mu.Unlock()
	if closed {
		return cerr
	}
	return p.writeFn(data)
}

func (p *peer) dropPending(key string) {
	p.mu.Lock()
	delete(p.pending, key)
	p.mu.Unlock()
}

// dispatch 处理一条来自对端的消息。由读取协程调用。
func (p *peer) dispatch(data []byte) error {
	var env rpcEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("invalid JSON-RPC message: %w", err)
	}

	switch {
	case env.Method == "" && len(env.ID) > 0:
		p.deliver(&env)
		return nil
	case env.Method != "" && len(env.ID) > 0:
		p.serve(env)
		return nil
	case env.Method != "":
		p.handleNotify(env)
		return nil
	default:
		return fmt.Errorf("message has neither method nor id")
	}
}

func (p *peer) deliver(env *rpcEnvelope) {
	key := idKey(env.ID)

	p.mu.Lock()
	ch, ok := p.pending[key]
	if ok {
		delete(p.pending, key)
	}
	p.mu.Unlock()

	if !ok {
		// 超时/取消后迟到的响应，丢弃即可：等待者已经拿到 ctx 错误。
		return
	}
	select {
	case ch <- env:
	default:
	}
}

// serve 处理入站请求：限制并发，独立协程执行后写回响应。
func (p *peer) serve(env rpcEnvelope) {
	p.mu.Lock()
	h := p.handlers[env.Method]
	p.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		// 闸门放在协程里拿：读取协程绝不能因为授权请求排队而停摆，
		// 否则同一连接上的 session/update 会被一起卡住。
		select {
		case p.inbound <- struct{}{}:
			defer func() { <-p.inbound }()
		case <-time.After(30 * time.Second):
			p.respond(env.ID, nil, &RPCError{
				Code:    codeInternalError,
				Message: "too many concurrent requests",
			})
			return
		}

		if h == nil {
			p.respond(env.ID, nil, &RPCError{
				Code:    codeMethodNotFound,
				Message: "method not found: " + env.Method,
			})
			return
		}

		result, err := h(context.Background(), env.Params)
		if err != nil {
			if rpcErr, ok := err.(*RPCError); ok {
				p.respond(env.ID, nil, rpcErr)
				return
			}
			p.respond(env.ID, nil, &RPCError{Code: codeInternalError, Message: err.Error()})
			return
		}
		p.respond(env.ID, result, nil)
	}()
}

func (p *peer) handleNotify(env rpcEnvelope) {
	p.mu.Lock()
	h := p.notifies[env.Method]
	p.mu.Unlock()
	if h == nil {
		return // 未知通知按规范直接忽略，不能回错
	}
	h(env.Params)
}

func (p *peer) respond(id json.RawMessage, result interface{}, rpcErr *RPCError) {
	resp := rpcResponse{JSONRPC: jsonRPCVersion, ID: id, Error: rpcErr}
	if rpcErr == nil {
		if result == nil {
			resp.Result = json.RawMessage("{}")
		} else {
			data, err := json.Marshal(result)
			if err != nil {
				resp.Error = &RPCError{Code: codeInternalError, Message: err.Error()}
			} else {
				resp.Result = data
			}
		}
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_ = p.write(data)
}

// Close 关闭连接：标记关闭、唤醒所有等待者、关闭底层传输。
func (p *peer) Close() error {
	p.mu.Lock()
	if p.closed {
		err := p.closeErr
		p.mu.Unlock()
		return err
	}
	p.closed = true
	p.closeErr = errPeerClosed
	pending := p.pending
	p.pending = make(map[string]chan *rpcEnvelope)
	p.mu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- nil:
		default:
		}
	}

	if p.closeFn != nil {
		return p.closeFn()
	}
	return nil
}

// failAll 在读流终止时调用：把所有等待者唤醒并带上终止原因。
func (p *peer) failAll(cause error) {
	p.mu.Lock()
	p.closed = true
	p.closeErr = cause
	pending := p.pending
	p.pending = make(map[string]chan *rpcEnvelope)
	p.mu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- nil:
		default:
		}
	}
}

// wait 等待所有入站请求协程退出（Close 后调用，便于测试无泄漏）。
func (p *peer) wait() { p.wg.Wait() }

// idKey 把 JSON-RPC id 归一化成 map key。
// 对端可能回 "1" 也可能回 1，两者必须命中同一个等待者。
func idKey(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if len(s) >= 2 && s[0] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

func marshalParams(params interface{}) json.RawMessage {
	if params == nil {
		return nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	return data
}
