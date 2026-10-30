// Package mcpserver 把 go-magic 反向暴露成一个 MCP server（stdio JSON-RPC 2.0）。
//
// 设计取舍（与 codex mcp-server 同源）：**不把 read_file / execute_command 这些
// 内部工具逐个映射成 MCP 工具**，而是把"整个 agent"当成一个工具暴露出去。
//
//   - 工具面极窄（magic / magic_reply 两个），模型在调用方那边不会选错；
//   - 会话语义（多轮、工具循环、审批）全留在本进程内，调用方只需传一段自然语言；
//   - 代价是调用方拿不到细粒度控制，只能用 approval / working_dir 传边界。
//
// 这个包里没有 agent，只有协议与调度：真正的执行由 Backend 实现（见
// cmd/magic/mcp_server.go），因此协议层可以脱离模型与工具单独测试。
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ProtocolVersion 是本实现声明的 MCP 协议版本。
// 客户端请求别的版本时仍回这个值——MCP 的协商规则是服务端必须挑一个自己支持的版本。
const ProtocolVersion = "2024-11-05"

// ServerName / ServerVersion 出现在 initialize 的 serverInfo 里。
const (
	ServerName    = "go-magic"
	ServerVersion = "1.0.0"
)

// ThreadOptions 描述一个新会话的边界。
//
// 刻意没有 per-call 的工作目录：工具注册表（含 MCP/CLI 连接器）是按目录构造成
// 进程级单例的，给每个目录再造一份就等于把连接器连两遍。需要别的目录时，
// 由调用方用 `magic mcp-server --cwd <dir>` 起另一个实例——边界清楚，也不会
// 出现"同一个 server 里两个会话共用一份文件工具目录"的隐性串味。
type ThreadOptions struct {
	// AllowApproval 决定需要人工确认的工具是否放行。
	// stdio 通道上没有"人"可问，所以默认拒绝（fail-closed），
	// 由调用方显式传 approval=allow 才放开。
	AllowApproval bool
}

// Backend 是协议层依赖的执行后端。
type Backend interface {
	// NewThread 创建一个独立会话，返回其 id。
	NewThread(ctx context.Context, opts ThreadOptions) (string, error)
	// Reply 在同一会话里继续对话，返回最终回答。
	Reply(ctx context.Context, threadID, prompt string) (string, error)
	// Close 释放后端资源（MCP 连接等）。
	Close()
}

// Server 是 stdio 上的 MCP 服务端。
type Server struct {
	backend Backend
	in      io.Reader
	out     io.Writer

	// writeMu 串行化 stdout 写入：并发处理请求时，两条响应不能交错在一行里。
	writeMu sync.Mutex

	// callTimeout 是单次 tools/call 的上限（0 = 不额外加超时）。
	callTimeout time.Duration
}

// New 构造服务端。in/out 通常是 os.Stdin / os.Stdout。
func New(backend Backend, in io.Reader, out io.Writer) *Server {
	return &Server{backend: backend, in: in, out: out, callTimeout: 30 * time.Minute}
}

// SetCallTimeout 设置单次 tools/call 的超时。
func (s *Server) SetCallTimeout(d time.Duration) { s.callTimeout = d }

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// maxMessageBytes 是单条消息的上限。超过就整条丢弃并回 parse error：
// 没有上限时一个畸形客户端可以用一条永不换行的输入把内存吃光。
const maxMessageBytes = 8 << 20

// Serve 读取并处理所有请求，直到输入关闭。
//
// 读取用"一行一条 JSON"（stdio MCP 的帧格式），而不是流式 json.Decoder：
// Decoder 一旦撞上语法错误就无法可靠地重新对齐，循环里会不停地返回同一个错误
// （实测会把服务卡死）；按行读则天然可以"坏一行、跳一行"，与规范一致。
//
// 每个请求在独立 goroutine 里处理：一次 agent 回合可能跑几分钟，
// 期间必须还能响应 ping（客户端靠它判断进程还活着）。
func (s *Server) Serve(ctx context.Context) error {
	r := bufio.NewReaderSize(s.in, 64<<10)
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		line, tooLarge, err := readLine(r)
		if tooLarge {
			s.writeError(nil, -32700, fmt.Sprintf("message exceeds %d bytes", maxMessageBytes))
		} else if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			s.dispatch(ctx, &wg, trimmed)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// dispatch 解析一条消息并按需并发处理。
func (s *Server) dispatch(ctx context.Context, wg *sync.WaitGroup, line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(nil, -32700, fmt.Sprintf("parse error: %v", err))
		return
	}
	if req.ID == nil {
		// 通知：没有 id，按协议不回响应。
		return
	}
	wg.Add(1)
	go func(req rpcRequest) {
		defer wg.Done()
		s.handle(ctx, req)
	}(req)
}

// readLine 读到换行为止；超过 maxMessageBytes 的剩余部分直接丢弃（tooLarge=true）。
func readLine(r *bufio.Reader) (line []byte, tooLarge bool, err error) {
	for {
		chunk, e := r.ReadSlice('\n')
		if !tooLarge {
			if len(line)+len(chunk) > maxMessageBytes {
				tooLarge = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		return line, tooLarge, e
	}
}

func (s *Server) handle(ctx context.Context, req rpcRequest) {
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": ServerName, "version": ServerVersion},
		})

	case "ping":
		s.writeResult(req.ID, map[string]any{})

	case "tools/list":
		s.writeResult(req.ID, map[string]any{"tools": toolDefs()})

	case "prompts/list":
		// 客户端（含一些 IDE）会主动探测；明确回空表比 -32601 更友好。
		s.writeResult(req.ID, map[string]any{"prompts": []any{}})

	case "resources/list":
		s.writeResult(req.ID, map[string]any{"resources": []any{}})

	case "tools/call":
		s.handleToolCall(ctx, req)

	default:
		s.writeError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (s *Server) handleToolCall(ctx context.Context, req rpcRequest) {
	var p callParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeError(req.ID, -32602, fmt.Sprintf("invalid params: %v", err))
			return
		}
	}

	prompt := strings.TrimSpace(argString(p.Arguments, "prompt"))
	if prompt == "" {
		s.writeToolError(req.ID, "", "prompt is required")
		return
	}

	callCtx := ctx
	if s.callTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, s.callTimeout)
		defer cancel()
	}

	switch p.Name {
	case "magic":
		threadID, err := s.backend.NewThread(callCtx, ThreadOptions{
			AllowApproval: strings.EqualFold(argString(p.Arguments, "approval"), "allow"),
		})
		if err != nil {
			s.writeToolError(req.ID, "", fmt.Sprintf("failed to start thread: %v", err))
			return
		}
		s.reply(callCtx, req.ID, threadID, prompt)

	case "magic_reply":
		threadID := strings.TrimSpace(argString(p.Arguments, "thread_id"))
		if threadID == "" {
			s.writeToolError(req.ID, "", "thread_id is required (use the id returned by the magic tool)")
			return
		}
		s.reply(callCtx, req.ID, threadID, prompt)

	default:
		s.writeError(req.ID, -32602, fmt.Sprintf("unknown tool: %s", p.Name))
	}
}

// reply 执行一轮对话并把结果包成 tools/call 的返回。
//
// 执行失败不写成 JSON-RPC error：协议里 error 表示"这个方法调用失败"，
// 而"agent 这一轮回答失败"属于工具结果的正常取值，写在 content + isError 里，
// 客户端才能把失败信息交回给它的模型（而不是显示成一次崩溃）。
func (s *Server) reply(ctx context.Context, id json.RawMessage, threadID, prompt string) {
	text, err := s.backend.Reply(ctx, threadID, prompt)
	if err != nil {
		s.writeToolError(id, threadID, err.Error())
		return
	}
	s.writeResult(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": false,
		"_meta":   map[string]any{"thread_id": threadID},
	})
}

func (s *Server) writeToolError(id json.RawMessage, threadID, msg string) {
	result := map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
	if threadID != "" {
		result["_meta"] = map[string]any{"thread_id": threadID}
	}
	s.writeResult(id, result)
}

func (s *Server) writeResult(id json.RawMessage, result any) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) writeError(id json.RawMessage, code int, msg string) {
	if id == nil {
		id = json.RawMessage("null")
	}
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

// write 一次性写出整条响应。
// json.Encoder.Encode 会在末尾补换行，stdio 协议就是"一行一条 JSON"。
func (s *Server) write(resp rpcResponse) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = json.NewEncoder(s.out).Encode(resp)
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// NewThreadID 生成会话 id（后端可复用，保证格式一致）。
func NewThreadID() string { return "th_" + uuid.NewString() }

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name": "magic",
			"description": "Delegate a task to the local Magic coding agent. " +
				"It can read and write files, run commands and use configured connectors " +
				"in the server's working directory. Returns the agent's final answer and a " +
				"thread_id you can pass to magic_reply to continue the same conversation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "The task or question for the agent, in natural language.",
					},
					"approval": map[string]any{
						"type":        "string",
						"enum":        []string{"deny", "allow"},
						"description": "Whether tools that normally need human confirmation may run. 'deny' (default) refuses them so the task fails safely; 'allow' lets them run unattended.",
					},
				},
				"required": []string{"prompt"},
			},
		},
		{
			"name": "magic_reply",
			"description": "Continue an existing Magic thread with another message. " +
				"Use the thread_id returned by a previous magic / magic_reply call.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"thread_id": map[string]any{
						"type":        "string",
						"description": "Thread id returned by a previous call.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Follow-up message for the same thread.",
					},
				},
				"required": []string{"thread_id", "prompt"},
			},
		},
	}
}
