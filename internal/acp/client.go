package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// 客户端侧的错误。
var (
	// ErrPromptInFlight 表示同一会话上一轮还没结束。
	// ACP 一个 session 同时只有一轮 prompt，并发发起只会让两轮输出互相污染，
	// 所以这里直接拒绝，而不是排队——排队会把工具调用的超时预算吃光。
	ErrPromptInFlight = errors.New("acp: a prompt turn is already running")

	// DefaultPromptTimeout 是单轮 prompt 的默认超时。
	// 60s（工具层默认值）对编码 agent 远远不够，一轮改十几个文件很常见。
	DefaultPromptTimeout = 10 * time.Minute

	// cancelGrace 是发出 session/cancel 后等待 agent 回 cancelled 的时间。
	// 规范要求 agent 必须用 cancelled 结束这一轮；给 5s 让它收尾，
	// 超时就当连接不可用（调用方拿到 ctx 错误）。
	cancelGrace = 5 * time.Second
)

// ToolCallInfo 是合并后的工具调用状态快照。
type ToolCallInfo struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status,omitempty"`
}

// Update 是一条 session/update 的结构化视图。
type Update struct {
	Kind     string          // sessionUpdate 判别值
	Text     string          // *_chunk 的文本（非文本块为空）
	ToolCall *ToolCallInfo   // tool_call / tool_call_update
	Plan     []PlanEntry     // plan
	Raw      json.RawMessage // 原始 update 对象，便于调用方取未建模字段
}

// ClientOptions 是建立一个 ACP 连接所需的全部参数。
type ClientOptions struct {
	// Name 是本地给这个 agent 起的名字（日志与工具参数里用）。
	Name string
	// Cwd 是会话工作目录，必须是绝对路径（规范强制）。
	Cwd string
	// Command/Args/Env 是 agent 子进程的启动方式。
	Command string
	Args    []string
	Env     []string
	// WorkDir 是子进程自身的 cwd（与会话 Cwd 可以不同）。
	WorkDir string
	// Timeout 是单轮 prompt 的超时，0 = DefaultPromptTimeout。
	Timeout time.Duration
	// AllowWrite 决定是否向 agent 声明 fs/write_text_file 能力。
	// 默认 false：能读已经是很大的信任，写要显式打开。
	AllowWrite bool
	// AdditionalDirs 是会话额外的可访问目录（必须是绝对路径）。
	AdditionalDirs []string
	// Permission 决定如何回答 session/request_permission，nil = 只读白名单。
	Permission PermissionFunc
	// OnUpdate 是全局更新回调（单轮 prompt 的回调会覆盖它）。
	OnUpdate func(Update)
	// ClientName/ClientVersion 用于 initialize 的 clientInfo。
	ClientName    string
	ClientVersion string
}

// Client 是一条已初始化、已建会话的 ACP 连接。
//
// 生命周期由 Manager 持有：连接是长活对象，跨多次工具调用复用同一个
// 会话（ACP 的上下文属于 session，每轮新建 session 等于每轮失忆）。
type Client struct {
	opts      ClientOptions
	transport Transport
	peer      *peer
	roots     *RootSet

	initRes InitializeResult

	// sessionID 会在 ResetSession 里被替换，而 Prompt / Cancel / 通知处理
	// 都会读它；用独立的小锁保护，避免跟 turn 状态的锁互相牵连。
	sessMu    sync.RWMutex
	sessionID string

	promptSlot chan struct{}

	mu        sync.Mutex
	onUpdate  func(Update)
	text      strings.Builder
	thought   strings.Builder
	tools     map[string]*ToolCallInfo
	toolOrder []string
	updates   int
	cancelled bool

	readMu  sync.Mutex
	readErr error
	done    chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// normalize 填充默认值并做最低限度校验。
func (o *ClientOptions) normalize() error {
	if o.Command == "" {
		return errors.New("acp: empty agent command")
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultPromptTimeout
	}
	if o.ClientName == "" {
		o.ClientName = "magic"
	}
	if o.Permission == nil {
		o.Permission = AllowKinds(DefaultAllowedKinds...)
	}
	return nil
}

// Connect 启动并初始化一个 ACP agent 子进程。
func Connect(ctx context.Context, opts ClientOptions) (*Client, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}

	transport, err := StartStdio(StdioOptions{
		Command: opts.Command,
		Args:    opts.Args,
		Env:     opts.Env,
		Dir:     opts.WorkDir,
	})
	if err != nil {
		return nil, err
	}

	client, err := newClientWithTransport(ctx, opts, transport)
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	return client, nil
}

// newClientWithTransport 在已建立的传输上完成握手与会话创建。
// 单独抽出来是为了让测试能用管道注入一个假 agent，而不必真起子进程。
func newClientWithTransport(ctx context.Context, opts ClientOptions, transport Transport) (*Client, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}

	roots, err := NewRootSet(opts.Cwd, opts.AdditionalDirs)
	if err != nil {
		return nil, err
	}

	c := &Client{
		opts:       opts,
		transport:  transport,
		roots:      roots,
		promptSlot: make(chan struct{}, 1),
		tools:      make(map[string]*ToolCallInfo),
		done:       make(chan struct{}),
	}
	c.peer = newPeer(transport.WriteMessage, transport.Close)
	c.registerHandlers()
	go c.readLoop()

	initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := c.initialize(initCtx); err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := c.newSession(initCtx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// registerHandlers 注册 agent 可能回调的客户端方法。
//
// 只注册真实实现的方法。没实现的能力不会在 initialize 里声明，
// 声明了却不处理等于骗对端——agent 会一直等一个永远不来的响应。
func (c *Client) registerHandlers() {
	c.peer.OnNotify("session/update", c.handleSessionUpdate)

	c.peer.Handle("session/request_permission", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var p RequestPermissionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: codeInvalidParams, Message: "invalid request_permission params: " + err.Error()}
		}

		c.mu.Lock()
		cancelled := c.cancelled
		c.mu.Unlock()
		if cancelled {
			// 规范：本轮已取消时，所有待处理授权必须以 cancelled 回答。
			return RequestPermissionResult{Outcome: PermissionOutcome{Outcome: OutcomeCancelled}}, nil
		}

		outcome := c.opts.Permission(ctx, PermissionRequest{
			SessionID: p.SessionID,
			ToolCall:  p.ToolCall,
			Options:   p.Options,
		})
		if outcome.Outcome == OutcomeSelected && outcome.OptionID == "" {
			outcome = PermissionOutcome{Outcome: OutcomeCancelled}
		}
		return RequestPermissionResult{Outcome: outcome}, nil
	})

	c.peer.Handle("fs/read_text_file", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var p ReadTextFileParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: codeInvalidParams, Message: "invalid read_text_file params: " + err.Error()}
		}
		res, err := c.roots.ReadText(p)
		if err != nil {
			return nil, err
		}
		return res, nil
	})

	c.peer.Handle("fs/write_text_file", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		if !c.opts.AllowWrite {
			// 理论上 agent 不该调用未声明的方法，但外部实现参差不齐，
			// 这里明确拒绝而不是默默写盘。
			return nil, &RPCError{Code: codeMethodNotFound, Message: "fs/write_text_file is not enabled"}
		}
		var p WriteTextFileParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: codeInvalidParams, Message: "invalid write_text_file params: " + err.Error()}
		}
		if err := c.roots.WriteText(p); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	})
}

func (c *Client) readLoop() {
	defer close(c.done)

	for {
		data, err := c.transport.ReadMessage()
		if err != nil {
			c.setReadErr(err)
			cause := fmt.Errorf("acp: agent %q stream closed: %w", c.opts.Name, err)
			if diag := strings.TrimSpace(c.transport.Diagnostics()); diag != "" {
				cause = fmt.Errorf("%w (stderr: %s)", cause, tailSnippet(diag))
			}
			c.peer.failAll(cause)
			return
		}
		if err := c.peer.dispatch(data); err != nil {
			// 解析不了的消息没有 id，回不了错；记录后继续读。
			log.Warnf("[ACP] %s: drop bad message: %v", c.opts.Name, err)
		}
	}
}

func (c *Client) setReadErr(err error) {
	c.readMu.Lock()
	c.readErr = err
	c.readMu.Unlock()
}

// readCause 返回连接已死的原因，用于给 RPC 错误补充上下文。
func (c *Client) readCause() error {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	return c.readErr
}

func (c *Client) initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientCapabilities: ClientCapabilities{
			FS: &FSCapabilities{ReadTextFile: true, WriteTextFile: c.opts.AllowWrite},
		},
		ClientInfo: Implementation{
			Name:    c.opts.ClientName,
			Title:   "go-magic",
			Version: c.opts.ClientVersion,
		},
	}

	raw, err := c.peer.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	var res InitializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("initialize: bad response: %w", err)
	}
	if res.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf(
			"acp: agent %q speaks protocol version %d, this client supports %d",
			c.opts.Name, res.ProtocolVersion, ProtocolVersion)
	}
	c.initRes = res
	return nil
}

func (c *Client) newSession(ctx context.Context) error {
	raw, err := c.peer.call(ctx, "session/new", NewSessionParams{
		Cwd:        c.opts.Cwd,
		MCPServers: []StdioMCPServer{},
	})
	if err != nil {
		return fmt.Errorf("session/new: %w", err)
	}

	var res NewSessionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("session/new: bad response: %w", err)
	}
	if res.SessionID == "" {
		return errors.New("session/new: agent returned empty sessionId")
	}
	c.sessMu.Lock()
	c.sessionID = res.SessionID
	c.sessMu.Unlock()
	return nil
}

// Info 返回 initialize 协商结果。
func (c *Client) Info() InitializeResult { return c.initRes }

// SessionID 返回当前会话 id。
func (c *Client) SessionID() string { return c.currentSession() }

func (c *Client) currentSession() string {
	c.sessMu.RLock()
	defer c.sessMu.RUnlock()
	return c.sessionID
}

// Roots 返回会话可访问目录。
func (c *Client) Roots() []string { return c.roots.Roots() }

// PromptOutcome 是一轮 prompt 的结果。
type PromptOutcome struct {
	StopReason string
	// Text 是 agent_message_chunk 拼起来的正文（工具输出不在此列）。
	Text string
	// Thought 是 agent_thought_chunk 拼起来的思考内容。
	Thought string
	// ToolCalls 是本轮出现过的工具调用（按首次出现顺序）。
	ToolCalls []ToolCallInfo
	// Updates 是收到的 session/update 条数。
	Updates int
	// Cancelled 表示这一轮是被客户端取消的。
	Cancelled bool
}

// Prompt 发送一轮用户消息并等它结束。
//
// 取消语义按规范实现：ctx 到期/被取消时先发 session/cancel **通知**，
// 再等 agent 用 cancelled 结束这一轮；超时才放弃并返回 ctx 错误。
// 直接 return 会让 agent 继续在后台跑，下一次 prompt 就撞在"上一轮还没完"上。
func (c *Client) Prompt(ctx context.Context, blocks []ContentBlock, onUpdate func(Update)) (*PromptOutcome, error) {
	select {
	case c.promptSlot <- struct{}{}:
		defer func() { <-c.promptSlot }()
	default:
		return nil, ErrPromptInFlight
	}

	c.mu.Lock()
	c.resetTurnLocked(onUpdate)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.onUpdate = nil
		c.mu.Unlock()
	}()

	callCtx, cancelCall := context.WithTimeout(context.Background(), c.opts.Timeout)
	defer cancelCall()

	type callResult struct {
		raw json.RawMessage
		err error
	}
	ch := make(chan callResult, 1)
	go func() {
		raw, err := c.peer.call(callCtx, "session/prompt", PromptParams{
			SessionID: c.currentSession(),
			Prompt:    blocks,
		})
		ch <- callResult{raw: raw, err: err}
	}()

	select {
	case r := <-ch:
		return c.finishTurn(r.raw, r.err)
	case <-ctx.Done():
		c.markCancelled()
		if err := c.peer.notify("session/cancel", CancelParams{SessionID: c.currentSession()}); err != nil {
			return nil, fmt.Errorf("cancel prompt: %w", err)
		}
		select {
		case r := <-ch:
			out, err := c.finishTurn(r.raw, r.err)
			if out != nil {
				out.Cancelled = true
				if out.StopReason == "" {
					out.StopReason = StopReasonCancelled
				}
			}
			return out, err
		case <-time.After(cancelGrace):
			return nil, ctx.Err()
		}
	}
}

func (c *Client) finishTurn(raw json.RawMessage, err error) (*PromptOutcome, error) {
	if err != nil {
		if cause := c.readCause(); cause != nil {
			return nil, fmt.Errorf("%w (agent exited: %v)", err, cause)
		}
		return nil, err
	}

	var res PromptResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("session/prompt: bad response: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	out := &PromptOutcome{
		StopReason: res.StopReason,
		Text:       c.text.String(),
		Thought:    c.thought.String(),
		Updates:    c.updates,
	}
	for _, id := range c.toolOrder {
		if tc, ok := c.tools[id]; ok {
			out.ToolCalls = append(out.ToolCalls, *tc)
		}
	}
	return out, nil
}

// Cancel 取消当前轮（没有进行中的轮次时是空操作）。
func (c *Client) Cancel() error {
	c.markCancelled()
	return c.peer.notify("session/cancel", CancelParams{SessionID: c.currentSession()})
}

// ResetSession 丢弃当前会话，新建一个（清空上下文）。
func (c *Client) ResetSession(ctx context.Context) error {
	return c.newSession(ctx)
}

func (c *Client) markCancelled() {
	c.mu.Lock()
	c.cancelled = true
	c.mu.Unlock()
}

func (c *Client) resetTurnLocked(onUpdate func(Update)) {
	c.onUpdate = onUpdate
	c.text.Reset()
	c.thought.Reset()
	c.tools = make(map[string]*ToolCallInfo)
	c.toolOrder = nil
	c.updates = 0
	c.cancelled = false
}

func (c *Client) handleSessionUpdate(params json.RawMessage) {
	var n SessionNotification
	if err := json.Unmarshal(params, &n); err != nil {
		log.Warnf("[ACP] %s: bad session/update: %v", c.opts.Name, err)
		return
	}
	if sid := c.currentSession(); n.SessionID != "" && sid != "" && n.SessionID != sid {
		return // 不是我们的会话（agent 串了，或者我们已重置）
	}

	var header SessionUpdateHeader
	if err := json.Unmarshal(n.Update, &header); err != nil {
		return
	}

	u := Update{Kind: header.SessionUpdate, Raw: n.Update}

	switch header.SessionUpdate {
	case UpdateAgentMessageChunk, UpdateAgentThoughtChunk, UpdateUserMessageChunk:
		var chunk MessageChunkUpdate
		if err := json.Unmarshal(n.Update, &chunk); err == nil && chunk.Content.Type == ContentTypeText {
			u.Text = chunk.Content.Text
			c.mu.Lock()
			if header.SessionUpdate == UpdateAgentMessageChunk {
				c.text.WriteString(chunk.Content.Text)
			} else if header.SessionUpdate == UpdateAgentThoughtChunk {
				c.thought.WriteString(chunk.Content.Text)
			}
			c.mu.Unlock()
		}
	case UpdateToolCall, UpdateToolCallUpdate:
		var tc ToolCall
		if err := json.Unmarshal(n.Update, &tc); err == nil {
			info := c.applyToolCall(&tc)
			u.ToolCall = &info
		}
	case UpdatePlan:
		var plan PlanUpdate
		if err := json.Unmarshal(n.Update, &plan); err == nil {
			u.Plan = plan.Entries
		}
	}

	c.mu.Lock()
	c.updates++
	cb := c.onUpdate
	c.mu.Unlock()

	if cb != nil {
		cb(u)
	}
}

// applyToolCall 合并增量更新：tool_call_update 只带变化字段，
// 覆盖式赋值会把之前收到的 title/kind 清空。
func (c *Client) applyToolCall(tc *ToolCall) ToolCallInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.tools[tc.ToolCallID]
	if !ok {
		info = &ToolCallInfo{ID: tc.ToolCallID}
		c.tools[tc.ToolCallID] = info
		c.toolOrder = append(c.toolOrder, tc.ToolCallID)
	}
	if tc.Title != nil {
		info.Title = *tc.Title
	}
	if tc.Name != nil {
		info.Name = *tc.Name
	}
	if tc.Kind != nil {
		info.Kind = *tc.Kind
	}
	if tc.Status != nil {
		info.Status = *tc.Status
	}
	return *info
}

// alive 判断连接是否还能用：读流没断、也没被本地关闭。
//
// 只看 readErr 不够——Close 之后 done 已关，此时 readErr 仍可能是 nil。
func (c *Client) alive() bool {
	select {
	case <-c.done:
		return false
	default:
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	return c.readErr == nil
}

// Close 关闭连接。可重复调用。
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.peer.Close()
		// 等读取协程退出，避免测试里出现"连接关了但协程还在读"的假泄漏。
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
		}
		c.peer.wait()
	})
	return c.closeErr
}

// tailSnippet 把 stderr 尾巴压成一行，便于塞进错误信息。
func tailSnippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 400
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
}
