// Package acp 实现 Agent Client Protocol（ACP）v1 的**客户端**侧，
// 让 go-magic 能把外部 ACP agent（Claude Code、Gemini CLI 等）当成一种外部能力来驱动。
//
// 与旧版（v0.5.x，已删除）的区别，改前必读：
//  1. 协议对齐 https://agentclientprotocol.com/protocol/v1 —— 方法名、字段名、
//     判别值全部按规范（initialize / session/new / session/prompt /
//     session/update / session/request_permission / fs/read_text_file ...
//     / session/cancel）。旧版自造的 connect / skill-call 与任何真实 ACP
//     agent 都不兼容，等于自己跟自己玩。
//  2. JSON-RPC 是**双向**的：agent 也会向 client 发请求（要权限、读写文件）。
//     所以 rpc.go 里有一个按 id 关联的 peer，既有 pending 表，也有入站
//     handler 表；旧版只有单向 Send，导致 stdio 通道根本拿不到响应。
//  3. 连接是**进程内长活对象**（manager 持有），不是每条 CLI 命令 new 一个
//     再随进程退出丢掉。旧版 `magic acp list/call` 因此永远查不到 agent。
//  4. 本包只做客户端。把 go-magic 自身暴露成 ACP agent（session/prompt 驱动
//     agent 主循环）是另一个量级的工作，未实现，也不留半成品桩代码。
package acp

import "encoding/json"

// ProtocolVersion 是本实现支持的唯一 ACP MAJOR 版本。
//
// 规范要求 client 在 initialize 里带上自己支持的最新版本；agent 若支持同一
// 版本必须原样返回，否则返回它自己的最新版本。收到不同版本时客户端必须断开
// 并告诉用户（见 Client.Initialize），不能"尽力而为"地继续跑——字段语义
// 变化是静默的，跑下去只会得到难解释的错。
const ProtocolVersion = 1

// jsonRPCVersion 是 JSON-RPC 2.0 封装字段的固定值。
const jsonRPCVersion = "2.0"

// ContentBlock 是提示词/模型输出里的一块内容。
//
// 按规范 ContentBlock 是 MCP 2025-06-18 ContentBlock 的超集，这里用单结构
// + Type 判别而不是 interface：ACP 的块类型只有五种，反序列化时不需要
// 自定义 UnmarshalJSON，也避免"未知类型把整条消息解崩"。
type ContentBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// image / audio：Base64 编码数据 + MIME。image 额外允许 uri。
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`

	// resource_link
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Size        *int64 `json:"size,omitempty"`

	// resource（内嵌资源）
	Resource *EmbeddedResource `json:"resource,omitempty"`
}

// 内容块类型取值。
const (
	ContentTypeText         = "text"
	ContentTypeImage        = "image"
	ContentTypeAudio        = "audio"
	ContentTypeResource     = "resource"
	ContentTypeResourceLink = "resource_link"
)

// EmbeddedResource 是 resource 块里的内容：text 或 blob 二选一。
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// TextBlock 构造一个文本内容块。
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentTypeText, Text: text}
}

// Implementation 描述 client/agent 的实现信息（name 与 version 必填）。
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// ClientCapabilities 是 client 在 initialize 里声明的能力。
// 规范：省略一律视为不支持，所以布尔字段只在 true 时出现。
type ClientCapabilities struct {
	FS       *FSCapabilities `json:"fs,omitempty"`
	Terminal bool            `json:"terminal,omitempty"`
}

// FSCapabilities 声明 fs/read_text_file 与 fs/write_text_file 是否可用。
type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

// InitializeParams 是 initialize 请求参数。
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         Implementation     `json:"clientInfo"`
}

// PromptCapabilities 声明 agent 能接受的提示词内容类型。
type PromptCapabilities struct {
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

// MCPCapabilities 声明 agent 支持哪种 MCP 传输。
type MCPCapabilities struct {
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

// AuthMethod 是 agent 公布的一种认证方式。
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AgentCapabilities 是 initialize 响应里 agent 声明的能力。
type AgentCapabilities struct {
	LoadSession        bool               `json:"loadSession,omitempty"`
	PromptCapabilities PromptCapabilities `json:"promptCapabilities,omitempty"`
	MCPCapabilities    MCPCapabilities    `json:"mcpCapabilities,omitempty"`
}

// InitializeResult 是 initialize 的响应。
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         *Implementation   `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod      `json:"authMethods,omitempty"`
}

// EnvVariable 是 MCP server / 子进程的环境变量项。
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// StdioMCPServer 是 session/new 里可以带给 agent 的 stdio MCP server 描述。
// 规范要求 command 是绝对路径、args 必填（可为空数组）、env 用 name/value。
type StdioMCPServer struct {
	Name    string        `json:"name"`
	Command string        `json:"command"`
	Args    []string      `json:"args"`
	Env     []EnvVariable `json:"env,omitempty"`
}

// NewSessionParams 是 session/new 请求参数。
type NewSessionParams struct {
	Cwd        string           `json:"cwd"`
	MCPServers []StdioMCPServer `json:"mcpServers"`
}

// NewSessionResult 是 session/new 的响应。
type NewSessionResult struct {
	SessionID string `json:"sessionId"`
}

// PromptParams 是 session/prompt 请求参数。
type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// StopReason 取值（session/prompt 的结束原因）。
const (
	StopReasonEndTurn         = "end_turn"
	StopReasonMaxTokens       = "max_tokens"
	StopReasonMaxTurnRequests = "max_turn_requests"
	StopReasonRefusal         = "refusal"
	StopReasonCancelled       = "cancelled"
)

// PromptResult 是 session/prompt 的响应。
type PromptResult struct {
	StopReason string `json:"stopReason"`
}

// CancelParams 是 session/cancel 通知参数。
type CancelParams struct {
	SessionID string `json:"sessionId"`
}

// SetModeParams 是 session/set_mode 请求参数。
type SetModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SessionNotification 是 session/update 通知的信封：sessionId + 判别式 update。
type SessionNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

// SessionUpdate 判别值（update.sessionUpdate）。
const (
	UpdateUserMessageChunk        = "user_message_chunk"
	UpdateAgentMessageChunk       = "agent_message_chunk"
	UpdateAgentThoughtChunk       = "agent_thought_chunk"
	UpdateToolCall                = "tool_call"
	UpdateToolCallUpdate          = "tool_call_update"
	UpdatePlan                    = "plan"
	UpdateAvailableCommandsUpdate = "available_commands_update"
	UpdateCurrentModeUpdate       = "current_mode_update"
)

// SessionUpdateHeader 只解析判别字段，用来决定后续反序列化成哪个具体类型。
type SessionUpdateHeader struct {
	SessionUpdate string `json:"sessionUpdate"`
}

// MessageChunkUpdate 对应 user/agent/thought 三种 *_chunk 更新。
type MessageChunkUpdate struct {
	SessionUpdate string       `json:"sessionUpdate"`
	MessageID     string       `json:"messageId,omitempty"`
	Content       ContentBlock `json:"content"`
}

// 工具种类取值（用于权限策略的白名单匹配）。
const (
	ToolKindRead       = "read"
	ToolKindEdit       = "edit"
	ToolKindDelete     = "delete"
	ToolKindMove       = "move"
	ToolKindSearch     = "search"
	ToolKindExecute    = "execute"
	ToolKindThink      = "think"
	ToolKindFetch      = "fetch"
	ToolKindSwitchMode = "switch_mode"
	ToolKindOther      = "other"
)

// 工具调用状态取值。
const (
	ToolStatusPending    = "pending"
	ToolStatusInProgress = "in_progress"
	ToolStatusCompleted  = "completed"
	ToolStatusFailed     = "failed"
)

// ToolCallContent 是工具调用产出的内容：普通内容块 / diff / terminal 三选一。
type ToolCallContent struct {
	Type       string        `json:"type"`
	Content    *ContentBlock `json:"content,omitempty"`
	Path       string        `json:"path,omitempty"`
	OldText    *string       `json:"oldText,omitempty"`
	NewText    string        `json:"newText,omitempty"`
	TerminalID string        `json:"terminalId,omitempty"`
}

// ToolCallLocation 是工具调用涉及的文件位置（1-based 行号）。
type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// ToolCall 是 tool_call / tool_call_update 的载荷。
//
// tool_call_update 里除 toolCallId 外全部可选，所以这里用指针区分
// "没给" 与 "给了零值"；合并逻辑见 Client.applyToolCall。
type ToolCall struct {
	SessionUpdate string             `json:"sessionUpdate"`
	ToolCallID    string             `json:"toolCallId"`
	Title         *string            `json:"title,omitempty"`
	Name          *string            `json:"name,omitempty"`
	Kind          *string            `json:"kind,omitempty"`
	Status        *string            `json:"status,omitempty"`
	Content       []ToolCallContent  `json:"content,omitempty"`
	Locations     []ToolCallLocation `json:"locations,omitempty"`
	RawInput      json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage    `json:"rawOutput,omitempty"`
}

// PlanEntry 是 plan 更新里的一条待办。
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// PlanUpdate 是 plan 类型更新。
type PlanUpdate struct {
	SessionUpdate string      `json:"sessionUpdate"`
	Entries       []PlanEntry `json:"entries"`
}

// RequestPermissionParams 是 agent 向 client 请求授权的参数。
type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCall           `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// 授权选项种类取值。
const (
	PermissionAllowOnce    = "allow_once"
	PermissionAllowAlways  = "allow_always"
	PermissionRejectOnce   = "reject_once"
	PermissionRejectAlways = "reject_always"
)

// PermissionOption 是 agent 给出的一个可选项。
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// 授权结果判别值。
const (
	OutcomeSelected  = "selected"
	OutcomeCancelled = "cancelled"
)

// PermissionOutcome 是授权结果：selected + optionId，或 cancelled。
type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// RequestPermissionResult 是 session/request_permission 的响应体。
type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// ReadTextFileParams 是 fs/read_text_file 参数（line 1-based，limit 行数）。
type ReadTextFileParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

// ReadTextFileResult 是 fs/read_text_file 响应。
type ReadTextFileResult struct {
	Content string `json:"content"`
}

// WriteTextFileParams 是 fs/write_text_file 参数。
type WriteTextFileParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}
