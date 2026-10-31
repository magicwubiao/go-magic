package agentplugin

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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// Runtime 管理单个 Agent Plugin 的 MCP 连接生命周期。
//
// 规范要求:单个 MCP server 启动失败仅禁用该条目,其余 server/skills/扩展继续可用。
// Start 对每个静态校验通过的条目尝试连接,失败记入 entry.Error,不传播异常。
type Runtime struct {
	plugin *Plugin
	mu     sync.RWMutex
	conns  map[string]*connection // key = server 条目名

	// registered 记录 RegisterTools 注册到注册表的工具名,
	// 供 UnregisterTools 在 reload/uninstall/disable 时清理,避免僵尸工具。
	registered []string

	// reg/pluginName 记录最近一次 RegisterTools 的注册表与插件名,
	// 供 ReconnectEntry 在重连后"先注销旧工具再重注册新工具"。
	reg        ToolRegistrar
	pluginName string

	// credentials 是用户为该插件填写的凭据(env key → value)。连接时合并进
	// stdio 子进程环境(覆盖 mcp.json 的同名气)。由 server 层注入,读写走 mu。
	credentials map[string]string

	// oauth 是共享的 OAuth 管理器（nil = 未配置，远程条目只走静态凭据）。
	// 与连接器共用同一个 token 存储，键加 agentplugin: 命名空间（见 oauth.go）。
	oauth *mcp.OAuthManager

	// reqID 为 CallTool 等请求生成自增 JSON-RPC id。
	// 必须显式带 id:无 id 的报文是通知,transport 不等响应;
	// 自增保证并发调用在同一 stdio 流上不撞 id。
	reqID atomic.Uint64

	// stopped 由 Stop 置位。连接在后台 goroutine 里跑，reload/uninstall
	// 可能在其完成前就调 Stop：此时握手成功的连接不能再塞回 conns，
	// 否则立刻变成没人认领的孤儿子进程（Windows 上还锁着 npx 的句柄）。
	stopped bool

	// suppressAutoOAuth 在批量启动期间置位：开机时把每个"装过但还没授权"
	// 的插件都弹一次浏览器是很糟的体验，交给用户在详情里点一次"登录"。
	// 手动重连 / 凭据变更 / 授权后重连不受影响。
	suppressAutoOAuth atomic.Bool
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
	// 确保 PLUGIN_DATA 可写目录存在(尽力而为:目录缺失只影响工具运行时写文件,
	// 那类失败会在具体工具调用处暴露,这里不吞成静默成功)。
	if r.plugin.DataDir != "" {
		_ = os.MkdirAll(r.plugin.DataDir, 0o755)
	}

	// 开机批量连接期间不自动弹授权页（见 suppressAutoOAuth 注释）。
	// 用 Store(true)/Store(false) 包住整段并行连接：startParallel 内部
	// 等所有 goroutine 结束才返回，不会把状态泄漏到之后的单条目重连。
	r.suppressAutoOAuth.Store(true)
	r.startParallel()
	r.suppressAutoOAuth.Store(false)
	return nil
}

// startParallel 并行连接所有静态校验通过的条目（并发上限 4）。
//
// 串行连接时多台 server 的握手时间会相加（每台上限 30s）：一个插件里
// 声明 3 台 npx server，冷启动就要等 3 次 npx 解析/下载。并行后总耗时
// 约等于最慢的一台。条目之间不共享状态（连接结果各自写回自己的
// MCPEntryStatus，连接表在 r.mu 内），并发安全。
// 上限 4 是防止一次拉起十几个子进程打爆内存/句柄。
func (r *Runtime) startParallel() {
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	pluginName := ""
	if r.plugin != nil && r.plugin.Manifest != nil {
		pluginName = r.plugin.Manifest.Name
	}

	for i := range r.plugin.MCPServers {
		entry := &r.plugin.MCPServers[i]
		if entry.Error != "" {
			continue // 静态校验已失败,跳过
		}
		wg.Add(1)
		go func(e *MCPEntryStatus) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			began := time.Now()
			toolCount, err := r.connectEntry(e)
			if err != nil {
				toolCount = 0
				r.setEntryStatus(e, false, 0, err.Error(), mcp.KindOf(err).String())
			}
			// 只给慢条目留日志：这是"服务起得慢"最直接的可观测信号。
			if took := time.Since(began); took > time.Second {
				log.Infof("[AgentPlugin] %s/%s mcp init %v (connected=%v tools=%d err=%v)",
					pluginName, e.Name, took.Round(time.Millisecond), err == nil, toolCount, err)
			}
		}(entry)
	}
	wg.Wait()
}

// SetCredentials 注入/更新用户凭据(env key → value)。幂等;nil 表示清空。
func (r *Runtime) SetCredentials(creds map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credentials = creds
}

// credentialsSnapshot 返回当前凭据的拷贝,供连接时合并。
func (r *Runtime) credentialsSnapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.credentials) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.credentials))
	for k, v := range r.credentials {
		out[k] = v
	}
	return out
}

// connectEntry 连接单个 MCP server 并发现工具。
func (r *Runtime) connectEntry(entry *MCPEntryStatus) (int, error) {
	// 用户凭据最后合并(覆盖 mcp.json 同名 env / 替换 ${CRED:NAME}),凭据改了无需重新 Load。
	spec, missing := applyCredentials(entry.Spec, r.credentialsSnapshot())
	if len(missing) > 0 {
		// 缺凭据属于 auth 类失败:UI 据此亮"去授权",而不是让用户去查网络。
		return 0, missingCredsError(missing)
	}

	// OAuth：显式声明的条目在握手**前**就要有 token（没有就发流程并直接
	// 报 auth），零配置条目则先看有没有历史 token，等 401 再触发探测。
	authProvider, oauthErr := r.oauthForEntry(entry.Name, spec)
	if oauthErr != nil {
		return 0, oauthErr
	}

	tr, err := buildTransport(spec, authProvider)
	if err != nil {
		return 0, err
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
		return 0, r.handshakeError(entry.Name, spec, "initialize", err)
	}
	// initialized 通知(无响应,Send 不等待)。
	_, _ = tr.Send(ctx, &mcp.JSONRPCRequest{JSONRPC: mcp.JSONRPCVersion, Method: "notifications/initialized"})

	toolsReq := &mcp.JSONRPCRequest{JSONRPC: mcp.JSONRPCVersion, Method: "tools/list", ID: 2}
	resp, err := tr.Send(ctx, toolsReq)
	if err != nil {
		tr.Close()
		return 0, r.handshakeError(entry.Name, spec, "tools/list", err)
	}
	if resp.Error != nil {
		tr.Close()
		return 0, fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}
	var res struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		tr.Close()
		return 0, fmt.Errorf("parse tools: %w", err)
	}

	// 连接表与条目状态在同一把锁内更新：状态会被 HTTP handler（插件列表、
	// 就绪检查）并发读取，而连接现在跑在后台 goroutine 里。
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		tr.Close()
		return 0, fmt.Errorf("runtime stopped")
	}
	r.conns[entry.Name] = &connection{spec: entry.Spec, transport: tr, tools: res.Tools}
	entry.Connected = true
	entry.ToolCount = len(res.Tools)
	entry.Error = ""
	entry.ErrorKind = ""
	r.mu.Unlock()
	return len(res.Tools), nil
}

// setEntryStatus 在锁内写条目状态（后台连接与 HTTP 读取并发）。
func (r *Runtime) setEntryStatus(entry *MCPEntryStatus, connected bool, toolCount int, errMsg, errKind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.Connected = connected
	entry.ToolCount = toolCount
	entry.Error = errMsg
	entry.ErrorKind = errKind
}

// EntryStatuses 返回 MCP 条目状态的快照（含 Spec 的浅拷贝）。
// 供 API / 摘要层读取：条目状态在后台连接期间持续变化，直接遍历
// plugin.MCPServers 会跟写侧并发（-race 报 data race，UI 也可能读到半更新）。
func (r *Runtime) EntryStatuses() []MCPEntryStatus {
	if r == nil || r.plugin == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]MCPEntryStatus, len(r.plugin.MCPServers))
	copy(out, r.plugin.MCPServers)
	r.mu.RUnlock()
	// OAuth 视图在锁外补：读 token 文件是 IO，不能攥着 Runtime.mu。
	for i := range out {
		r.enrichOAuthStatus(&out[i])
	}
	return out
}

// EntryStatus 返回单个条目的状态快照（锁内读）。
func (r *Runtime) EntryStatus(name string) (MCPEntryStatus, bool) {
	if r == nil || r.plugin == nil {
		return MCPEntryStatus{}, false
	}
	r.mu.RLock()
	var found MCPEntryStatus
	ok := false
	for i := range r.plugin.MCPServers {
		if r.plugin.MCPServers[i].Name == name {
			found = r.plugin.MCPServers[i]
			ok = true
			break
		}
	}
	r.mu.RUnlock()
	if !ok {
		return MCPEntryStatus{}, false
	}
	r.enrichOAuthStatus(&found)
	return found, true
}

// ReconnectEntry 断开并重连单个 MCP server 条目,成功后用新工具列表重注册。
//
// 用于"补完凭据立即生效"的场景:server 层更新凭据后调用本方法,不必整包
// reload(那会杀掉其它 server 的子进程、重注入全部 skills)。失败时保留旧连接
// 已不可能(先关再连),但条目状态会带上分类错误供 UI 展示。
func (r *Runtime) ReconnectEntry(serverName string) error {
	// 先摘旧连接并停掉子进程:无论后面成败,旧 transport 都不能复用。
	r.mu.Lock()
	c, ok := r.conns[serverName]
	if ok {
		delete(r.conns, serverName)
	}
	reg, pluginName := r.reg, r.pluginName
	r.mu.Unlock()
	if ok && c != nil {
		_ = c.transport.Close()
	}

	// 找到条目(静态校验失败/不存在的条目直接报错)。
	var entry *MCPEntryStatus
	for i := range r.plugin.MCPServers {
		if r.plugin.MCPServers[i].Name == serverName {
			entry = &r.plugin.MCPServers[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("MCP server %q not found in plugin", serverName)
	}
	// 静态校验失败的条目(mcp.json 配置本身不合法):没有可运行的 spec,
	// 重连没有意义。区分依据:连接失败会带 ErrorKind 分类,静态错误不会。
	// 状态字段在后台连接 goroutine 里也会写，读要走锁。
	r.mu.RLock()
	staticErr, staticKind, connected := entry.Error, entry.ErrorKind, entry.Connected
	r.mu.RUnlock()
	if staticErr != "" && staticKind == "" && !connected {
		return fmt.Errorf("entry %q failed static validation: %s", serverName, staticErr)
	}

	// 重连前先注销旧工具:重连后 tools/list 可能返回不同集合,
	// 不注销会留下指向已关 transport 的僵尸工具。
	if reg != nil {
		r.UnregisterTools(reg)
	}

	if _, err := r.connectEntry(entry); err != nil {
		r.setEntryStatus(entry, false, 0, err.Error(), mcp.KindOf(err).String())
		return err
	}

	// 用新工具列表重注册(ReconnectEntry 只在 server 层的注册表上调用)。
	if reg != nil {
		r.RegisterTools(reg, pluginName)
	}
	return nil
}

// Stop 关闭所有 MCP 连接。
func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
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
		ID:      r.reqID.Add(1), // 无 id 即通知,transport 不会等响应
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
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("parse tool result: %w", err)
	}
	var texts []string
	for _, c := range res.Content {
		if c.Type == "text" && c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	// MCP 规范:协议层成功但工具执行失败时 isError=true,内容即错误详情。
	if res.IsError {
		return nil, fmt.Errorf("tool error: %s", strings.Join(texts, "\n"))
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n"), nil
	}
	// 非 text 内容(image/resource 等)当前不支持,返回空。
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

// ToolUnregistrar 是支持注销的工具注册表(*tool.Registry 同时实现两者)。
type ToolUnregistrar interface {
	ToolRegistrar
	Unregister(name string)
}

// RegisterTools 将所有已连接 server 的工具注册到工具注册表,
// 并记录工具名供 UnregisterTools 清理。
func (r *Runtime) RegisterTools(reg ToolRegistrar, pluginName string) {
	var names []string
	r.mu.RLock()
	for serverName, c := range r.conns {
		for _, tool := range c.tools {
			name := fmt.Sprintf("ap_%s_%s_%s", pluginName, serverName, tool.Name)
			reg.Register(&MCPPluginTool{
				pluginName: pluginName,
				serverName: serverName,
				tool:       tool,
				runtime:    r,
			})
			names = append(names, name)
		}
	}
	r.mu.RUnlock()

	r.mu.Lock()
	r.registered = append(r.registered, names...)
	// 记住注册表与插件名,ReconnectEntry 重连后要用同一注册表重注册。
	r.reg = reg
	r.pluginName = pluginName
	r.mu.Unlock()
}

// UnregisterTools 将此前 RegisterTools 注册的工具从注册表注销。
// reg 不支持注销(仅实现 ToolRegistrar)时静默跳过。
func (r *Runtime) UnregisterTools(reg ToolRegistrar) {
	u, ok := reg.(ToolUnregistrar)
	if !ok {
		return
	}
	r.mu.Lock()
	names := r.registered
	r.registered = nil
	r.mu.Unlock()
	for _, n := range names {
		u.Unregister(n)
	}
}

// oauthForEntry 决定连接某个条目时挂哪个 token 提供者（nil = 不挂）。
//
// 三种情况：
//   - 显式声明 oauth：必须有可用 token，拿不到就报 auth 并就地发起授权；
//   - 零配置远程条目且本地已有（或曾有过）token：挂上，401 时还能刷新；
//   - 其余：不挂，等握手 401 再触发探测（不主动打扰只填了 API key 的条目）。
func (r *Runtime) oauthForEntry(serverName string, spec MCPServerSpec) (mcp.AuthTokenProvider, error) {
	mgr := r.oauthSnapshot()
	if mgr == nil || !isRemoteSpec(spec) || !oauthCapable(spec) {
		return nil, nil
	}
	key := r.oauthKeyFor(serverName)
	if spec.OAuth != nil {
		if _, err := mgr.EnsureToken(key); err != nil {
			r.maybeStartOAuth(serverName, spec)
			return nil, fmt.Errorf("OAuth credentials for %q are not ready: %w", serverName, err)
		}
		return mgr.Provider(key), nil
	}
	switch mgr.Status(key).State {
	case mcp.OAuthStateAuthorized, "expired":
		return mgr.Provider(key), nil
	}
	return nil, nil
}

// handshakeError 给握手失败补上阶段信息；401 且条目可走 OAuth 时顺手发起
// 授权流程——用户看到的错误是"需要授权"，浏览器里同时已经打开了授权页。
func (r *Runtime) handshakeError(serverName string, spec MCPServerSpec, stage string, err error) error {
	var se *httpStatusError
	if errors.As(err, &se) && se.status == http.StatusUnauthorized && oauthCapable(spec) {
		r.maybeStartOAuth(serverName, spec)
		return fmt.Errorf("%s: %w", stage, mcp.WrapKind(mcp.ErrKindAuth, err))
	}
	return fmt.Errorf("%s: %w", stage, err)
}

// buildTransport 按传输类型构造 transport。auth 非 nil 时远程传输会自动
// 带 Authorization 头，并在 401 时刷新重试一次。
func buildTransport(spec MCPServerSpec, auth mcp.AuthTokenProvider) (transport, error) {
	switch spec.Type {
	case TransportStdio:
		return newStdioPluginTransport(spec.Command, spec.Args, envMapToSlice(spec.Env), spec.Cwd)
	case TransportStreamableHTTP, TransportSSE:
		tr := newHTTPTransport(spec.URL, spec.Headers, spec.Type == TransportSSE)
		tr.SetAuthProvider(auth)
		return tr, nil
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

// pendingResult 是一次等待响应的最终结果。
type pendingResult struct {
	resp *mcp.JSONRPCResponse
	err  error
}

// stdioPluginTransport 用单一 reader goroutine 从 stdout 读行并按 JSON-RPC id
// 分发到 pending map,保证:
//   - 通知(method 无 id)只写不等响应,MCP 规范下 server 不会回包;
//   - 并发请求的响应按 id 匹配,不会错配到其他请求;
//   - 超时/关闭后等待者被显式唤醒,reader goroutine 无泄漏。
type stdioPluginTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	writeMu sync.Mutex // 串行化 stdin 写入

	pendingMu sync.Mutex
	pending   map[string]chan pendingResult // key = 规范化响应 id
	closed    bool
	closeOnce sync.Once
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
	t := &stdioPluginTransport{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		stderr:  stderr,
		pending: make(map[string]chan pendingResult),
	}
	go t.readLoop()
	return t, nil
}

// newPipeTransport 用内存管道构造 transport(不启动子进程),供测试驱动
// 协议分帧/分发逻辑。stdout 为 server 写出方向,stdin 为 client 写入方向。
func newPipeTransport(stdout io.Reader, stdin io.WriteCloser) *stdioPluginTransport {
	t := &stdioPluginTransport{
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: make(map[string]chan pendingResult),
	}
	go t.readLoop()
	return t
}

// responseKey 把请求/响应 id 规范化为 map 键。经 json.Marshal 序列化:
// float64(1) 与 int(1) 都得到 "1",字符串 "1" 得到 "\"1\"",二者不混淆。
func responseKey(id any) string {
	data, err := json.Marshal(id)
	if err != nil {
		return fmt.Sprintf("%v", id)
	}
	return string(data)
}

func (t *stdioPluginTransport) readLoop() {
	for {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			// stdout 关闭:尽力带上 server 的 stderr 信息(管道模式下无 stderr)。
			msg := fmt.Errorf("read stdout: %w", err)
			if t.stderr != nil {
				if eb, _ := io.ReadAll(t.stderr); len(eb) > 0 {
					msg = fmt.Errorf("MCP server stderr: %s (%w)", strings.TrimSpace(string(eb)), err)
				}
			}
			t.failAllPending(msg)
			return
		}
		var resp mcp.JSONRPCResponse
		if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
			continue // 非响应行(server 日志等噪声):丢弃
		}
		if resp.ID == nil {
			continue // server 主动通知:忽略
		}
		if ch := t.takePending(responseKey(resp.ID)); ch != nil {
			ch <- pendingResult{resp: &resp}
		}
		// 无匹配等待者的响应(如超时后迟到的响应):丢弃,保持流同步。
	}
}

// failAllPending 在流终止时唤醒所有等待者。
func (t *stdioPluginTransport) failAllPending(err error) {
	t.pendingMu.Lock()
	pending := t.pending
	t.pending = make(map[string]chan pendingResult)
	t.pendingMu.Unlock()
	for _, ch := range pending {
		ch <- pendingResult{err: err}
	}
}

// takePending 取出并删除指定 key 的等待者;不存在返回 nil。
func (t *stdioPluginTransport) takePending(key string) chan pendingResult {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	ch, ok := t.pending[key]
	if ok {
		delete(t.pending, key)
	}
	return ch
}

func (t *stdioPluginTransport) Send(ctx context.Context, req *mcp.JSONRPCRequest) (*mcp.JSONRPCResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// 通知(JSON-RPC 规范无 id 字段):只写不等响应。
	// 此前通知也走等待路径,会阻塞抢占后续请求的响应行,造成握手挂死。
	if req.ID == nil {
		t.writeMu.Lock()
		defer t.writeMu.Unlock()
		t.pendingMu.Lock()
		closed := t.closed
		t.pendingMu.Unlock()
		if closed {
			return nil, fmt.Errorf("transport closed")
		}
		if _, err := t.stdin.Write(append(data, '\n')); err != nil {
			return nil, fmt.Errorf("write stdin: %w", err)
		}
		return nil, nil
	}

	key := responseKey(req.ID)
	ch := make(chan pendingResult, 1)
	t.pendingMu.Lock()
	if t.closed {
		t.pendingMu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}
	t.pending[key] = ch
	t.pendingMu.Unlock()

	t.writeMu.Lock()
	_, werr := t.stdin.Write(append(data, '\n'))
	t.writeMu.Unlock()
	if werr != nil {
		t.takePending(key)
		return nil, fmt.Errorf("write stdin: %w", werr)
	}

	select {
	case r := <-ch:
		return r.resp, r.err
	case <-ctx.Done():
		t.takePending(key) // 迟到的响应由 readLoop 丢弃,流保持同步
		return nil, ctx.Err()
	}
}

func (t *stdioPluginTransport) Close() error {
	t.closeOnce.Do(func() {
		t.pendingMu.Lock()
		t.closed = true
		pending := t.pending
		t.pending = make(map[string]chan pendingResult)
		t.pendingMu.Unlock()

		_ = t.stdin.Close()
		// 管道模式(cmd 为 nil)没有子进程可等待。
		if t.cmd != nil {
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
		}

		for _, ch := range pending {
			ch <- pendingResult{err: fmt.Errorf("transport closed")}
		}
	})
	return nil
}

// ---- http transport (streamable-http / sse) ----

type httpPluginTransport struct {
	url     string
	headers map[string]string
	isSSE   bool
	client  *http.Client
	// auth 是 OAuth token 提供者（nil = 静态凭据）。只在构造后、首次 Send
	// 之前由 buildTransport 设置，之后只读。
	auth mcp.AuthTokenProvider
}

// httpStatusError 保留 HTTP 状态码，供上层区分"要去授权"(401) 与普通失败。
type httpStatusError struct {
	status int
	msg    string
}

func (e *httpStatusError) Error() string { return e.msg }

func newHTTPTransport(u string, headers map[string]string, isSSE bool) *httpPluginTransport {
	return &httpPluginTransport{
		url:     u,
		headers: headers,
		isSSE:   isSSE,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// SetAuthProvider 挂上 OAuth token 提供者。
func (t *httpPluginTransport) SetAuthProvider(p mcp.AuthTokenProvider) {
	t.auth = p
}

func (t *httpPluginTransport) Send(ctx context.Context, req *mcp.JSONRPCRequest) (*mcp.JSONRPCResponse, error) {
	resp, err := t.sendOnce(ctx, req)
	if err == nil {
		return resp, nil
	}
	// 与连接器同一策略：401 只刷新重试一次。反复刷新通常意味着 token 被
	// 吊销，重试解决不了，只会给授权服务器刷请求量。
	var se *httpStatusError
	if t.auth != nil && errors.As(err, &se) && se.status == http.StatusUnauthorized {
		if header, rerr := t.auth.RefreshToken(ctx); rerr == nil && header != "" {
			log.Warnf("[AgentPlugin] access token rejected (HTTP 401), retrying once with refreshed credentials")
			return t.sendOnceWithToken(ctx, req, header)
		}
	}
	return nil, err
}

// sendOnce 取当前 token 后发送。
func (t *httpPluginTransport) sendOnce(ctx context.Context, req *mcp.JSONRPCRequest) (*mcp.JSONRPCResponse, error) {
	var tokenHeader string
	if t.auth != nil {
		header, err := t.auth.CurrentToken(ctx)
		if err != nil {
			// EnsureToken 返回的已经是 auth 类错误，原样透传。
			return nil, err
		}
		tokenHeader = header
	}
	return t.sendOnceWithToken(ctx, req, tokenHeader)
}

func (t *httpPluginTransport) sendOnceWithToken(ctx context.Context, req *mcp.JSONRPCRequest, tokenHeader string) (*mcp.JSONRPCResponse, error) {
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
	if tokenHeader != "" {
		httpReq.Header.Set("Authorization", tokenHeader)
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
		// 401/403 归到 auth 类：UI 才能亮"登录/重新授权"，而不是让人去查网络。
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if resp.StatusCode == http.StatusUnauthorized {
			msg = "HTTP 401 Unauthorized: the server requires credentials"
		}
		err := &httpStatusError{status: resp.StatusCode, msg: msg}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, mcp.WrapKind(mcp.ErrKindAuth, err)
		}
		return nil, err
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
