package agentplugin

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

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// Runtime 管理单个 Agent Plugin 的 MCP 连接生命周期。
//
// 规范要求:单个 MCP server 启动失败仅禁用该条目,其余 server/skills/扩展继续可用。
// Start 对每个静态校验通过的条目尝试连接,失败记入 entry.Error,不传播异常。
type Runtime struct {
	plugin *Plugin
	mu     sync.RWMutex
	conns  map[string]*connection // key = server 条目名
}

type connection struct {
	spec      MCPServerSpec
	transport transport
	tools     []mcp.Tool
}

// transport 是 MCP 传输层抽象(与 mcp.Transport 等价,自包含实现以支持 cwd/headers)。
type transport interface {
	Send(ctx context.Context, req *mcp.JSONRPCRequest) (*mcp.JSONRPCResponse, error)
	Close() error
}

// NewRuntime 为已加载的插件创建 MCP 运行时(尚未连接)。
func NewRuntime(p *Plugin) *Runtime {
	return &Runtime{plugin: p, conns: make(map[string]*connection)}
}

// Start 连接所有静态校验通过的 MCP server。PLUGIN_DATA 目录不存在时自动创建。
// 任何条目失败仅记录到 plugin.MCPServers[i].Error,不影响其他条目。
func (r *Runtime) Start() error {
	if r.plugin == nil {
		return fmt.Errorf("nil plugin")
	}
	if r.plugin.MCPDisabled || len(r.plugin.MCPServers) == 0 {
		return nil
	}
	// 确保 PLUGIN_DATA 可写目录存在。
	if r.plugin.DataDir != "" {
		_ = os.MkdirAll(r.plugin.DataDir, 0o755)
	}

	for i := range r.plugin.MCPServers {
		entry := &r.plugin.MCPServers[i]
		if entry.Error != "" {
			continue // 静态校验已失败,跳过
		}
		if err := r.connectEntry(entry); err != nil {
			entry.Connected = false
			entry.Error = err.Error()
		}
	}
	return nil
}

// connectEntry 连接单个 MCP server 并发现工具。
func (r *Runtime) connectEntry(entry *MCPEntryStatus) error {
	tr, err := buildTransport(entry.Spec)
	if err != nil {
		return err
	}

	// MCP 握手:initialize → notifications/initialized → tools/list。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	initReq := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		Method:  "initialize",
		Params:  mustJSON(map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "go-magic-agentplugin", "version": "1.0"}}),
		ID:      1,
	}
	if _, err := tr.Send(ctx, initReq); err != nil {
		tr.Close()
		return fmt.Errorf("initialize: %w", err)
	}
	// initialized 通知(无响应)。
	_, _ = tr.Send(ctx, &mcp.JSONRPCRequest{JSONRPC: mcp.JSONRPCVersion, Method: "notifications/initialized"})

	toolsReq := &mcp.JSONRPCRequest{JSONRPC: mcp.JSONRPCVersion, Method: "tools/list", ID: 2}
	resp, err := tr.Send(ctx, toolsReq)
	if err != nil {
		tr.Close()
		return fmt.Errorf("tools/list: %w", err)
	}
	if resp.Error != nil {
		tr.Close()
		return fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}
	var res struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		tr.Close()
		return fmt.Errorf("parse tools: %w", err)
	}

	r.mu.Lock()
	r.conns[entry.Name] = &connection{spec: entry.Spec, transport: tr, tools: res.Tools}
	r.mu.Unlock()
	entry.Connected = true
	entry.ToolCount = len(res.Tools)
	return nil
}

// Stop 关闭所有 MCP 连接。
func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, c := range r.conns {
		_ = c.transport.Close()
		delete(r.conns, name)
	}
}

// ListTools 返回所有已连接 server 的工具列表。
func (r *Runtime) ListTools() []mcp.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []mcp.Tool
	for _, c := range r.conns {
		out = append(out, c.tools...)
	}
	return out
}

// CallTool 调用指定 server 的工具。
func (r *Runtime) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error) {
	r.mu.RLock()
	c, ok := r.conns[serverName]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("MCP server %q not connected", serverName)
	}
	req := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		Method:  "tools/call",
		Params:  mustJSON(map[string]any{"name": toolName, "arguments": args}),
	}
	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tool call error: %s", resp.Error.Message)
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("parse tool result: %w", err)
	}
	if len(res.Content) > 0 {
		return res.Content[0].Text, nil
	}
	return nil, nil
}

// MCPPluginTool 将插件 MCP 工具适配为 tool registry 可注册的形态。
type MCPPluginTool struct {
	pluginName string
	serverName string
	tool       mcp.Tool
	runtime    *Runtime
}

// Name 返回工具名(命名空间隔离,避免跨插件冲突)。
func (t *MCPPluginTool) Name() string {
	return fmt.Sprintf("ap_%s_%s_%s", t.pluginName, t.serverName, t.tool.Name)
}

// Description 返回工具描述。
func (t *MCPPluginTool) Description() string {
	return fmt.Sprintf("AgentPlugin %s/%s: %s", t.pluginName, t.serverName, t.tool.Description)
}

// Schema 返回工具输入 schema(与 tool.Tool 接口对接)。
func (t *MCPPluginTool) Schema() map[string]any {
	if t.tool.InputSchema == nil {
		return map[string]any{}
	}
	return t.tool.InputSchema
}

// Execute 执行工具调用。
func (t *MCPPluginTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.runtime.CallTool(ctx, t.serverName, t.tool.Name, args)
}

// ToolRegistrar 是工具注册表的最小接口(由 *tool.Registry 实现)。
// 使用 tool.Tool 命名接口类型,确保 *tool.Registry.Register(tool.Tool) 签名精确匹配。
type ToolRegistrar interface {
	Register(t tool.Tool)
}

// RegisterTools 将所有已连接 server 的工具注册到工具注册表。
func (r *Runtime) RegisterTools(reg ToolRegistrar, pluginName string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for serverName, c := range r.conns {
		for _, tool := range c.tools {
			reg.Register(&MCPPluginTool{
				pluginName: pluginName,
				serverName: serverName,
				tool:       tool,
				runtime:    r,
			})
		}
	}
}

// buildTransport 按传输类型构造 transport。
func buildTransport(spec MCPServerSpec) (transport, error) {
	switch spec.Type {
	case TransportStdio:
		return newStdioPluginTransport(spec.Command, spec.Args, envMapToSlice(spec.Env), spec.Cwd)
	case TransportStreamableHTTP, TransportSSE:
		return newHTTPTransport(spec.URL, spec.Headers, spec.Type == TransportSSE)
	default:
		return nil, fmt.Errorf("unsupported transport %q", spec.Type)
	}
}

func envMapToSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// ---- stdio transport ----

type stdioPluginTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser
	mu     sync.Mutex
	closed bool
}

func newStdioPluginTransport(command string, args, env []string, dir string) (*stdioPluginTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)
	if dir != "" {
		cmd.Dir = dir
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start MCP server: %w", err)
	}
	return &stdioPluginTransport{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr}, nil
}

func (t *stdioPluginTransport) Send(ctx context.Context, req *mcp.JSONRPCRequest) (*mcp.JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.mu.Unlock()
		return nil, err
	}
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("write stdin: %w", err)
	}
	t.mu.Unlock()

	type result struct {
		resp *mcp.JSONRPCResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				if eb, _ := io.ReadAll(t.stderr); len(eb) > 0 {
					ch <- result{err: fmt.Errorf("MCP server stderr: %s", string(eb))}
					return
				}
			}
			ch <- result{err: fmt.Errorf("read stdout: %w", err)}
			return
		}
		var resp mcp.JSONRPCResponse
		if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
			ch <- result{err: fmt.Errorf("unmarshal response: %w", err)}
			return
		}
		ch <- result{resp: &resp}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.resp, r.err
	}
}

func (t *stdioPluginTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	_ = t.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- t.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		<-done
	}
	return nil
}

// ---- http transport (streamable-http / sse) ----

type httpPluginTransport struct {
	url     string
	headers map[string]string
	isSSE   bool
	client  *http.Client
}

func newHTTPTransport(u string, headers map[string]string, isSSE bool) (*httpPluginTransport, error) {
	return &httpPluginTransport{
		url:     u,
		headers: headers,
		isSSE:   isSSE,
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (t *httpPluginTransport) Send(ctx context.Context, req *mcp.JSONRPCRequest) (*mcp.JSONRPCResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	if t.isSSE {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json, text/event-stream")
	}
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// 按 Content-Type 决定解析方式。
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return parseSSEBody(resp.Body)
	}
	// application/json:直接 unmarshal。
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var jr mcp.JSONRPCResponse
	if err := json.Unmarshal(body, &jr); err != nil {
		// 兜底:可能仍是 SSE 格式。
		return parseSSEBody(bytes.NewReader(body))
	}
	return &jr, nil
}

func parseSSEBody(body io.Reader) (*mcp.JSONRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			d := strings.TrimPrefix(line, "data: ")
			if d == "" || d == ":" {
				continue
			}
			if strings.HasPrefix(d, "{") {
				dataLine = d
				break
			}
		}
	}
	if dataLine == "" {
		return nil, fmt.Errorf("no SSE data received")
	}
	var jr mcp.JSONRPCResponse
	if err := json.Unmarshal([]byte(dataLine), &jr); err != nil {
		return nil, fmt.Errorf("unmarshal SSE data: %w", err)
	}
	return &jr, nil
}

func (t *httpPluginTransport) Close() error { return nil }
