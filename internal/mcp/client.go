package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// Protocol constants
const (
	JSONRPCVersion        = "2.0"
	ProtocolName          = "modelcontextprotocol"
	DefaultTimeout        = 30 * time.Second
	DefaultReconnectDelay = 5 * time.Second
	MaxReconnectAttempts  = 3
	ClientName            = "go-magic"
)

// ClientVersion 客户端上报给 MCP server 的版本号。做成 var：main 启动时用
// ldflags 注入的构建版本覆盖它（见 cmd/magic），避免和真实版本脱钩。
var ClientVersion = "0.5.8"

// handshakeProtocolVersions 是握手代协议版本，**从新到旧**排列。
//
// initialize 会从第一个开始尝试；服务端返回 UnsupportedProtocolVersion
// 时自动降级到下一个（见 isUnsupportedVersionError）。这样既不会
// 因为写死一个版本而在协议演进时失联，也不需要预先知道对端支持什么。
var handshakeProtocolVersions = []string{
	"2025-11-25", // 最新握手版：Tasks、枚举 schema、sampling 工具调用
	"2025-06-18", // 结构化工具输出、elicitation，移除 JSON-RPC 批处理
	"2025-03-26", // Streamable HTTP、OAuth 2.1、tool annotations
	"2024-11-05", // 初版稳定
}

// ProtocolVersion 是**首选**协议版本。
//
// 注意 MCP 有两代模型（见官方 SEP-2575）：
//   - modern era（2026-07-28 起）：**没有握手**，靠 server/discover 发现，
//     每个请求在 _meta 里自带版本号；
//   - handshake era（2025-11-25 及更早）：用 initialize 握手约定一个版本。
//
// 本客户端实现的是 **handshake** 路径，所以首选值必须落在握手代里。
// 曾经这里写死 2026-07-28（modern 代），DeepWiki 之类的服务端直接返回
// 400 "Unsupported protocol version"，整台 server 从此连不上。
var ProtocolVersion = handshakeProtocolVersions[0]

// unsupportedVersionHint 从服务端的 400 报文里挖出"它到底支持哪些版本"。
//
// 常见形态（DeepWiki 实测）：
//
//	Bad Request: Unsupported protocol version: 2026-07-28.
//	Supported versions: 2024-11-05, 2025-03-26, 2025-06-18, 2025-11-25
//
// 能拿到列表时按它的列表重试，拿不到就按 handshakeProtocolVersions 盲降。
func unsupportedVersionHint(msg string) []string {
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "unsupported protocol version") &&
		!strings.Contains(lower, "unsupportedprotocolversion") {
		return nil
	}
	idx := strings.Index(lower, "supported version")
	if idx < 0 {
		return nil
	}
	// 取 "supported versions:" 之后的部分，先剥掉 JSON 残留的引号与括号，
	// 再按分隔符切分——服务端报文常见的结尾形态是 `2025-11-25"}}`，
	// 不剥离的话最后一个版本号会连着 `"` 一起被切掉。
	tail := msg[idx:]
	tail = strings.NewReplacer(`"`, " ", `}`, " ", `]`, " ", `)`, " ", `(`, " ").Replace(tail)

	seen := map[string]bool{}
	var out []string
	for _, field := range strings.FieldsFunc(tail, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\r' || r == '.'
	}) {
		field = strings.TrimSpace(field)
		if len(field) == 10 && field[4] == '-' && field[7] == '-' && !seen[field] {
			seen[field] = true
			out = append(out, field)
		}
	}
	return out
}

// isUnsupportedVersionError 判断错误是否属于"协议版本不被接受"。
func isUnsupportedVersionError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "unsupported protocol version") ||
		strings.Contains(lower, "unsupportedprotocolversion") ||
		strings.Contains(lower, "-32600") && strings.Contains(lower, "version")
}

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// MCPMessage represents an MCP protocol message
type MCPMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Tool represents an MCP tool (2025-03-26 protocol compatible)
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Annotations *ToolAnnotations       `json:"annotations,omitempty"` // 2025-03-26
}

// ToolAnnotations holds metadata hints for clients (2025-03-26)
type ToolAnnotations struct {
	Title               string   `json:"title,omitempty"`
	ReadOnlyHint        bool     `json:"readOnlyHint,omitempty"`
	DestructiveHint     bool     `json:"destructiveHint,omitempty"`
	IdempotentHint      bool     `json:"idempotentHint,omitempty"`
	OpenWorldHint       bool     `json:"openWorldHint,omitempty"`
	ReasoningEffortHint string   `json:"reasoningEffortHint,omitempty"` // low | medium | high
	Tags                []string `json:"tags,omitempty"`
	Privacy             []string `json:"privacy,omitempty"`
}

// Supported transport names, used as ServerConfig.Transport values. External
// JSON configs spell these differently (type: "http", "streamable-http", …) —
// see NormalizeTransport in import.go for the mapping.
//
// TransportHTTP 与 TransportSSE 走同一份实现（SSETransport 本身就是
// Streamable HTTP 语义：一次 POST 一个请求，响应按 Content-Type 分流），
// 保留两个名字是为了让配置忠实反映用户写的端点类型，而不是在落盘时把
// "http" 改写成 "sse" 造成来回漂移。
const (
	TransportStdio = "stdio"
	TransportSSE   = "sse"
	TransportHTTP  = "http"
)

// ServerConfig represents MCP server configuration
type ServerConfig struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Env       []string `json:"env,omitempty"`
	Transport string   `json:"transport"`     // "stdio" / "sse" / "http"
	URL       string   `json:"url,omitempty"` // for remote transports

	// Headers 是远程连接器的自定义请求头，用来放认证凭据
	// （Authorization: Bearer …、X-API-Key 等）。值在 API 层按 *** 掩码，
	// 与 Env 同一套约定。
	Headers map[string]string `json:"headers,omitempty"`

	// DisabledTools 屏蔽该 server 下的指定工具：不注册进工具注册表，模型
	// 完全看不到。用于给工具爆炸的 server（上百个工具）瘦身。
	DisabledTools []string `json:"disabled_tools,omitempty"`

	// Enabled 控制启动时是否自动连接。nil = true（老配置没有这个字段，
	// 语义必须是"照旧连接"，否则升级后所有连接器都会静默离线）。
	Enabled *bool `json:"enabled,omitempty"`

	// Profiles 限定该连接器只在指定分身里生效；空 = 所有分身。切换分身
	// 需要重启本来就是既有约定，因此这里只需在启动连接时过滤。
	Profiles []string `json:"profiles,omitempty"`

	// RequireApproval 强制该 server 的每次工具调用都走人工确认。
	// 给没写 tool annotations 的 server 兜底——annotations 是可选字段，
	// 不写就无从判断危险程度，只能由用户显式声明。
	RequireApproval bool `json:"require_approval,omitempty"`

	// OAuth 启用 OAuth 2.1 授权（发现/注册/PKCE/刷新，见 oauth.go）。
	// 配置了它，远程连接器不再依赖手工 headers，token 由流程自动管理。
	// nil = 不用 OAuth（继续走静态 headers）。
	OAuth *OAuthConfig `json:"oauth,omitempty"`
}

// AutoConnect 报告该连接器是否应在启动时自动连接。
func (c ServerConfig) AutoConnect() bool {
	return c.Enabled == nil || *c.Enabled
}

// AppliesToProfile 报告该连接器是否在给定分身里生效。
func (c ServerConfig) AppliesToProfile(profile string) bool {
	if len(c.Profiles) == 0 {
		return true
	}
	for _, p := range c.Profiles {
		if strings.EqualFold(strings.TrimSpace(p), strings.TrimSpace(profile)) {
			return true
		}
	}
	return false
}

// Transport interface for MCP transport layers
type Transport interface {
	Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error)
	Close() error
}

// Client represents an MCP client
type Client struct {
	name            string
	config          ServerConfig
	transport       Transport
	tools           map[string]Tool
	mu              sync.RWMutex
	connected       bool
	lastError       string
	lastErrorKind   ErrorKind
	lastHealthCheck time.Time
	timeout         time.Duration
	reconnect       bool
	reconnectDelay  time.Duration
	// manager 反向引用：底层链路死亡（stdio 子进程退出等）时用来触发
	// Manager 的自动重连。测试手工构造 Client 时为 nil，此时跳过重连。
	manager *Manager
	// negotiatedVersion 记录握手最终谈成的协议版本，供诊断与状态展示。
	negotiatedVersion string
	// serverName / serverVersion 来自 initialize 的 serverInfo，列表页直接
	// 展示，方便用户确认"连的到底是哪个 server"。
	serverName    string
	serverVersion string
}

// Manager manages multiple MCP server connections
type Manager struct {
	mu             sync.RWMutex
	clients        map[string]*Client
	failures       map[string]failureRecord // last connect failure per configured server
	reconnectDelay time.Duration
	// reconnecting 记录正在跑自动重连 goroutine 的 server，防止同一个
	// server 被多次失败事件重复拉起重连循环。
	reconnecting map[string]bool
	// disabled 是"按 server 屏蔽的工具"白名单表，来源是 config 的
	// disabled_tools。工具的可见性在注册表侧生效，这里只负责判定。
	disabled map[string]map[string]bool
	// stats 记录每个工具被调用的次数与失败次数（server + "/" + tool 为键），
	// 用于连接器页展示"到底有没有在用"。
	stats map[string]*ToolStat
	// oauth 管理 OAuth 2.1 授权（nil = 未配置 token store，OAuth 不可用）。
	oauth *OAuthManager
	// healthStop 关闭时停止后台健康巡检。
	healthStop chan struct{}
	healthOnce sync.Once
}

// failureRecord 是一条连接失败记录：错误文本 + 分类。
// 分类让 UI 能区分"去补凭据"（auth）和"去查网络"（network）。
type failureRecord struct {
	Message string    `json:"message"`
	Kind    ErrorKind `json:"kind"`
	At      time.Time `json:"at"`
}

// ToolStat 是单个 MCP 工具的调用统计。
type ToolStat struct {
	Calls      int64     `json:"calls"`
	Failures   int64     `json:"failures"`
	LastCallAt time.Time `json:"last_call_at"`
	LastError  string    `json:"last_error,omitempty"`
}

// NewManager creates a new MCP manager
func NewManager() *Manager {
	return &Manager{
		clients:        make(map[string]*Client),
		failures:       make(map[string]failureRecord),
		reconnecting:   make(map[string]bool),
		disabled:       make(map[string]map[string]bool),
		stats:          make(map[string]*ToolStat),
		reconnectDelay: DefaultReconnectDelay,
	}
}

// SetTokenStore 配置 OAuth token 持久化路径（通常 <magicHome>/mcp/oauth_tokens.json）。
//
// 为什么路径由外部注入而不是本包自己算：pkg/config 反向依赖本包（ServerConfig
// 定义在这里），本包再 import 它就是循环依赖。调用方（internal/server）有
// magicHome，传进来即可。
func (m *Manager) SetTokenStore(path string) error {
	if path == "" {
		return fmt.Errorf("token store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create OAuth token dir: %w", err)
	}
	m.mu.Lock()
	m.oauth = NewOAuthManager(NewOAuthTokenStore(path))
	m.mu.Unlock()
	log.Infof("[MCP] OAuth token store at %s", path)
	return nil
}

// OnOAuthAuthorized 注册授权成功回调：OAuth 回调拿到 token 后触发，
// 上层用它重连该连接器并同步工具注册表。异步调用（新 goroutine）。
func (m *Manager) OnOAuthAuthorized(fn func(serverName string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.oauth == nil {
		m.oauth = NewOAuthManager(nil)
	}
	m.oauth.SetOnAuthorized(fn)
}

// OnOAuthFlowStarted 注册"新授权流程已创建"回调：上层用它自动打开浏览器，
// 让用户在授权页完成登录后凭据自动回流（授权成功另有 OnOAuthAuthorized
// 触发重连）。手动发起的流程不触发。异步调用（新 goroutine）。
func (m *Manager) OnOAuthFlowStarted(fn func(serverName, authorizeURL string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.oauth == nil {
		m.oauth = NewOAuthManager(nil)
	}
	m.oauth.SetOnFlowStarted(fn)
}

// OAuth 返回 OAuth 管理器；未配置时返回 nil（调用方需判空）。
func (m *Manager) OAuth() *OAuthManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.oauth
}

// LastFailure returns the most recent connection error recorded for a server
// (empty when the server is healthy or was never attempted).
//
// 三种来源按优先级：
//  1. 已连接的 client —— 健康，返回空；
//  2. 还在 map 里但已断开的 client（callTool/健康检查/链路死亡标记的）——
//     返回它记录的 lastError。此前这个盲区会让连接器页对一个已经死掉的
//     server 显示"无失败"；
//  3. 从未连上过的 —— 返回 connect 时记录的 failures 条目。
func (m *Manager) LastFailure(name string) string {
	m.mu.RLock()
	client, exists := m.clients[name]
	m.mu.RUnlock()
	if exists {
		client.mu.RLock()
		connected, lastErr := client.connected, client.lastError
		client.mu.RUnlock()
		if connected {
			return ""
		}
		return lastErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failures[name].Message
}

// LastFailureInfo 返回失败记录（文本 + 分类 + 时间）；没有记录时返回零值。
func (m *Manager) LastFailureInfo(name string) failureRecord {
	m.mu.RLock()
	client, exists := m.clients[name]
	m.mu.RUnlock()
	if exists {
		client.mu.RLock()
		connected, lastErr, kind := client.connected, client.lastError, client.lastErrorKind
		client.mu.RUnlock()
		if connected || lastErr == "" {
			return failureRecord{}
		}
		if kind == "" {
			kind = inferKindFromMessage(lastErr)
		}
		return failureRecord{Message: lastErr, Kind: kind}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failures[name]
}

// recordFailure remembers why a server failed to connect.
func (m *Manager) recordFailure(name string, err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	m.failures[name] = failureRecord{Message: err.Error(), Kind: KindOf(err), At: time.Now()}
	m.mu.Unlock()
}

// clearFailure drops the recorded failure for a server (on successful connect
// or explicit disconnect).
func (m *Manager) clearFailure(name string) {
	m.mu.Lock()
	delete(m.failures, name)
	m.mu.Unlock()
}

// recordFailureLocked is recordFailure for callers already holding m.mu.
func (m *Manager) recordFailureLocked(name string, err error) {
	if err != nil {
		m.failures[name] = failureRecord{Message: err.Error(), Kind: KindOf(err), At: time.Now()}
	}
}

// clearFailureLocked is clearFailure for callers already holding m.mu.
func (m *Manager) clearFailureLocked(name string) {
	delete(m.failures, name)
}

// connect 是三种连接入口(ConnectStdio / ConnectSSE / Reconnect)的公共实现，
// 采用两阶段模型：
//
//	阶段一(持写锁): 检查名字是否已被占用，随即释放锁。
//	阶段二(不持锁): 建 transport、initialize 握手、发现工具——这段时间可能
//	                长达 DefaultTimeout。
//	阶段三(持写锁): 回填 clients；若期间有别人抢先连上同名 server，则关闭
//	                自己刚建好的 transport 并报重复。
//
// 之所以必须这样拆: 旧实现整段握手都握着 Manager.mu 写锁，连接器页
// (ListServers)、工具列表(ListToolsByServer)、健康检查、断开操作全部被
// 挡在门外；而且那把锁会把并发的多 server 连接强行串行化。
func (m *Manager) connect(name string, config ServerConfig) error {
	if m.isConnected(name) {
		return fmt.Errorf("MCP server '%s' already connected", name)
	}

	client, err := m.buildClient(name, config)
	if err != nil {
		m.recordFailure(name, err)
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		// 并发窗口里另一个调用赢了这场竞争: 放弃自己刚建的连接，
		// 否则会泄漏一个 transport(stdio 情况下还留着子进程)。
		_ = client.transport.Close()
		return fmt.Errorf("MCP server '%s' already connected", name)
	}

	m.clearFailureLocked(name)
	m.clients[name] = client
	return nil
}

// buildClient 建好 transport 并完成 initialize 握手(含工具发现)。
// 全程不持有 Manager.mu —— 这是连接不再阻塞其它操作的关键。
func (m *Manager) buildClient(name string, config ServerConfig) (*Client, error) {
	// oauth 在构造后基本不再变化，快照一份避免无锁读竞争。
	m.mu.RLock()
	oauth := m.oauth
	m.mu.RUnlock()

	client := &Client{
		name:           name,
		config:         config,
		tools:          make(map[string]Tool),
		timeout:        DefaultTimeout,
		reconnect:      true,
		reconnectDelay: DefaultReconnectDelay,
		manager:        m,
	}

	var transport Transport
	var err error
	var kind string

	switch config.Transport {
	case TransportHTTP, TransportSSE:
		kind = "HTTP"
		// sse 与 http 共用同一实现：客户端的动作都是"POST 一个 JSON-RPC，
		// 按响应 Content-Type 分流"，两者的差异全在服务端。
		transport, err = NewSSETransportWithOptions(config.URL, config.Headers, defaultSSEConnectTimeout)
		if err == nil && config.OAuth != nil {
			// OAuth 在握手**前**校验凭据：token 没有就立刻失败并打上 auth
			// 分类，而不是让 initialize 吃一个 401 再绕一圈。UI 依赖这个
			// 分类亮"去授权"按钮。
			if oauth == nil {
				err = WrapKind(ErrKindAuth, fmt.Errorf(
					"OAuth is enabled for %q but no token store is configured (SetTokenStore was never called)", name))
			} else if _, terr := oauth.EnsureToken(name); terr != nil {
				err = terr
				// 自动获取路径：缺 token 就地发起授权流程（发现 + DCR +
				// 本机回调 listener），onFlowStarted 回调会弹出浏览器。
				// 异步执行——buildFlow 的网络往返最长 20s，不能拖住连接
				// 报错；失败也只记日志，原始 auth 错误照常向上返回。
				autoCfg := config
				go func(server string) {
					u, aerr := oauth.EnsureAuthorized(server, autoCfg)
					if aerr != nil {
						log.Warnf("[MCP] auto OAuth authorization for %q failed: %v", server, aerr)
						return
					}
					if u == "" {
						return // 冷却中，不打扰用户
					}
					log.Infof("[MCP] auto OAuth authorization started for %q — browser should open at the authorize page", server)
				}(name)
			}
		}
		if err == nil {
			if st, ok := transport.(*SSETransport); ok && config.OAuth != nil && oauth != nil {
				// 挂 provider 后每次请求自动带 Authorization，401 自动刷新重试。
				st.SetAuthProvider(oauth.Provider(name))
			}
		}
	default:
		kind = "stdio"
		transport, err = NewStdioTransport(config.Command, config.Args, config.Env)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create %s transport: %w", kind, err)
	}

	client.transport = transport
	client.connected = true

	// stdio 子进程退出是"链路终判死亡"：立刻把状态改成断开并调度自动
	// 重连，而不是等下一次工具调用踩雷才发现。
	if dn, ok := transport.(DeadNotifier); ok {
		dn.OnDead(func(err error) {
			if err == nil {
				err = fmt.Errorf("stdio process exited")
			}
			log.Warnf("[MCP] server %q link died: %v", name, err)
			client.markDisconnected(err)
		})
	}

	if err := client.initialize(); err != nil {
		transport.Close()
		// 无法归类时按协议问题处理：initialize 阶段失败，最可能的原因就是
		// 对端不说 MCP / 版本不兼容 / Accept 协商被拒。
		if KindOf(err) == ErrKindUnknown {
			err = WrapKind(ErrKindProtocol, err)
		}
		return nil, fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	return client, nil
}

// ConnectStdio connects to an MCP server using stdio transport
func (m *Manager) ConnectStdio(name string, config ServerConfig) error {
	config.Transport = "stdio"
	return m.connect(name, config)
}

// ConnectSSE connects to an MCP server using a remote transport (SSE or
// Streamable HTTP — both are served by SSETransport).
//
// 不要无条件把 Transport 改写成 "sse"：调用方可能配置的是 "http"，改写后
// 重连时读到的就是另一份语义，配置与运行时状态会不一致。
func (m *Manager) ConnectSSE(name string, config ServerConfig) error {
	if config.Transport == "" {
		config.Transport = TransportSSE
	}
	return m.connect(name, config)
}

// Disconnect disconnects an MCP server.
//
// 先把 client 从 map 里摘掉、再在**锁外**关闭 transport：stdio 的 Close
// 要等子进程退出（最多 5s + 强杀），绝不能攥着 Manager.mu 写锁做这件事，
// 否则所有 ListServers/CallTool 都被挡住——这和 connect 特意拆锁的初衷
// 相反。Close 失败不回滚：连接本来就是要拆掉的，条目必须保证被移除，
// 否则会留下一个"显示已连接但 transport 已半关"的僵尸。
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	delete(m.clients, name)
	m.clearFailureLocked(name)
	m.mu.Unlock()

	_ = client.transport.Close()
	return nil
}

// ListServers lists all connected MCP servers
func (m *Manager) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// ListTools lists all available tools from all connected servers
func (m *Manager) ListTools() []Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allTools []Tool
	for _, client := range m.clients {
		client.mu.RLock()
		for _, tool := range client.tools {
			allTools = append(allTools, tool)
		}
		client.mu.RUnlock()
	}
	return allTools
}

// ListToolsByServer lists tools available from a specific server
func (m *Manager) ListToolsByServer(name string) ([]Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[name]
	if !exists {
		return nil, fmt.Errorf("MCP server '%s' not found", name)
	}

	client.mu.RLock()
	tools := make([]Tool, 0, len(client.tools))
	for _, tool := range client.tools {
		tools = append(tools, tool)
	}
	client.mu.RUnlock()

	return tools, nil
}

// CallTool calls an MCP tool
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (interface{}, error) {
	m.mu.RLock()
	client, exists := m.clients[serverName]
	if !exists {
		return nil, fmt.Errorf("MCP server '%s' not found", serverName)
	}
	m.mu.RUnlock()

	return client.callTool(ctx, toolName, arguments)
}

// initializeTimeout 是 MCP 握手（initialize + tools/list）的总预算。
//
// 之所以不直接复用 DefaultTimeout(30s)：这个握手是**用户点保存后同步等待**
// 的路径，30s 与前端 request() 的 30s AbortController 完全重合，两端同时
// 踩线会让用户看到"转圈到失败且没有原因"。取一半后，最坏情况下后端仍能
// 在前端超时前返回一条可操作的错误。
const initializeTimeout = 15 * time.Second

// initialize initializes the MCP server and discovers tools (handshake era).
//
// 协议版本协商：从 handshakeProtocolVersions 里逐个尝试，直到服务端接受。
// 这一步是必要的——硬编码单个版本时，一旦服务端不支持该版本，整台 server
// 就永远连不上，而报错还常被超时重试掩盖成一句 "context deadline exceeded"。
func (c *Client) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()

	candidates := handshakeProtocolVersions
	var lastErr error

	for i, version := range candidates {
		resp, err := c.sendInitialize(ctx, version)
		if err != nil {
			// 版本不被接受 → 用服务端给出的支持列表继续协商
			if isUnsupportedVersionError(err) && i < len(candidates)-1 {
				if hint := unsupportedVersionHint(err.Error()); len(hint) > 0 {
					log.Infof("[MCP] server '%s' rejected protocol %s; it supports %s",
						c.name, version, strings.Join(hint, ", "))
				} else {
					log.Infof("[MCP] server '%s' rejected protocol %s; trying an older one",
						c.name, version)
				}
				lastErr = err
				continue
			}
			return err
		}
		if resp.Error != nil {
			if isUnsupportedVersionError(fmt.Errorf("%s", resp.Error.Message)) && i < len(candidates)-1 {
				lastErr = fmt.Errorf("initialize error: %s", resp.Error.Message)
				continue
			}
			return fmt.Errorf("initialize error: %s", resp.Error.Message)
		}

		// 握手成功：记录协商结果并继续完成能力发现
		c.negotiatedVersion = version
		c.validateInitResult(resp)
		return c.finishHandshake(ctx)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no supported protocol version")
	}
	return fmt.Errorf("protocol version negotiation failed: %w", lastErr)
}

// sendInitialize 用指定协议版本发一次 initialize 请求。
func (c *Client) sendInitialize(ctx context.Context, version string) (*JSONRPCResponse, error) {
	params, err := jsonMarshal(map[string]interface{}{
		"protocolVersion": version,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": false,
			},
			"logging": map[string]interface{}{},
			"prompts": map[string]interface{}{
				"listChanged": false,
			},
			"resources": map[string]interface{}{
				"listChanged": false,
				"subscribe":   false,
			},
		},
		"clientInfo": map[string]interface{}{
			"name":    ClientName,
			"version": ClientVersion,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal initialize params: %w", err)
	}
	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "initialize",
		Params:  params,
		// 用 uuid 而不是固定小整数：pending 表按 id 索引，两次并发的
		// 同 id 请求会互相覆盖 waiter，第一个永远收不到响应。
		ID: uuid.New().String(),
	}
	return c.transport.Send(ctx, req)
}

// finishHandshake 发 initialized 通知并发现工具。
func (c *Client) finishHandshake(ctx context.Context) error {
	// 通知本身不需要响应，但底层仍要走一次 HTTP 往返；沿用握手的 ctx
	// 以保证超时行为一致（用 Background 会让不响应的服务端把预算吃光）。
	notif := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	}
	_, _ = c.transport.Send(ctx, notif)

	return c.discoverTools(ctx)
}

// validateInitResult 解析服务端返回的 initialize 结果并记录版本差异。
func (c *Client) validateInitResult(resp *JSONRPCResponse) {
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		// 不致命：部分老服务端返回极简 payload
		log.Warnf("[MCP] Failed to parse initialize result: %v", err)
		return
	}
	// 记下对端自报的名字/版本，连接器列表直接展示，避免用户分不清
	// "我连的是 github 还是 github-enterprise"。这一项与协议版本无关，
	// 要在版本判断之前取，否则极简 payload 的 server 拿不到名字。
	if initResult.ServerInfo.Name != "" {
		c.serverName = initResult.ServerInfo.Name
		c.serverVersion = initResult.ServerInfo.Version
	}
	if initResult.ProtocolVersion == "" {
		return
	}
	// 服务端回的版本是**协商结果**（可能比客户端提出的更低），以它为准。
	if initResult.ProtocolVersion != c.negotiatedVersion {
		log.Infof("[MCP] server '%s' negotiated protocol %s (client proposed %s)",
			c.name, initResult.ProtocolVersion, c.negotiatedVersion)
		c.negotiatedVersion = initResult.ProtocolVersion
	} else {
		log.Infof("[MCP] server '%s' negotiated protocol %s", c.name, initResult.ProtocolVersion)
	}
}

// discoverTools discovers available tools from the MCP server, following the
// cursor pagination: servers commonly cap a single page (e.g. at 100 tools),
// and ignoring nextCursor silently truncates the registry for large servers.
func (c *Client) discoverTools(ctx context.Context) error {
	// Refresh 是"重新发现"：先清空旧表，避免已下线的工具残留。
	c.mu.Lock()
	c.tools = make(map[string]Tool)
	c.mu.Unlock()

	cursor := ""
	for {
		params := map[string]interface{}{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		encoded, err := jsonMarshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal tools/list params: %w", err)
		}
		req := &JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			Method:  "tools/list",
			Params:  encoded,
			ID:      uuid.New().String(),
		}

		resp, err := c.transport.Send(ctx, req)
		if err != nil {
			return err
		}
		if resp.Error != nil {
			return fmt.Errorf("tools/list error: %s", resp.Error.Message)
		}

		var result struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return fmt.Errorf("failed to parse tools list: %w", err)
		}

		c.mu.Lock()
		for _, tool := range result.Tools {
			c.tools[tool.Name] = tool
		}
		c.mu.Unlock()

		if result.NextCursor == "" {
			return nil
		}
		cursor = result.NextCursor
	}
}

// callTool calls a tool on the MCP server (2025-03-26 compatible) and records
// usage statistics for the connectors page.
func (c *Client) callTool(ctx context.Context, toolName string, arguments map[string]interface{}) (interface{}, error) {
	res, err := c.doCallTool(ctx, toolName, arguments)
	c.recordCall(toolName, err)
	return res, err
}

// doCallTool 是 callTool 的实现体。
func (c *Client) doCallTool(ctx context.Context, toolName string, arguments map[string]interface{}) (interface{}, error) {
	encoded, err := jsonMarshal(map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool arguments: %w", err)
	}

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/call",
		Params:  encoded,
		ID:      uuid.New().String(),
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		c.markDisconnected(err)
		return nil, err
	}

	if resp.Error != nil {
		return nil, WrapKind(ErrKindTool, fmt.Errorf("tool call error: %s", resp.Error.Message))
	}

	var result struct {
		Content []struct {
			// Text content
			Type string `json:"type"`
			Text string `json:"text,omitempty"`

			// Image content (2025-03-26)
			Data     string `json:"data,omitempty"`     // base64
			MimeType string `json:"mimeType,omitempty"` // e.g. image/png

			// Embedded resource (2025-03-26)
			URI      string `json:"uri,omitempty"`
			BlobData string `json:"blobData,omitempty"`

			// Audio content (2025-03-26)
			// Transcript optional field for audio/image

		} `json:"content"`
		IsError    bool   `json:"isError,omitempty"`    // 2025-03-26
		NextCursor string `json:"nextCursor,omitempty"` // 2025-03-26 paginated response
	}

	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}

	// Server explicitly signaled an error via the 2025-03-26 isError field
	if result.IsError {
		if len(result.Content) > 0 && result.Content[0].Text != "" {
			return nil, WrapKind(ErrKindTool, fmt.Errorf("tool error: %s", result.Content[0].Text))
		}
		return nil, WrapKind(ErrKindTool, fmt.Errorf("tool call returned isError=true"))
	}

	// Concatenate all text-type contents; non-text (images, resources) are
	// preserved as annotations in the raw result but summarized as text for LLM.
	if len(result.Content) == 0 {
		return "", nil
	}

	var combined strings.Builder
	for i, item := range result.Content {
		if i > 0 {
			combined.WriteString("\n")
		}
		switch item.Type {
		case "", "text":
			combined.WriteString(item.Text)
		case "image":
			// NOTE: avoid square brackets — GLM mimics them in its output.
			desc := fmt.Sprintf("image %s (%d bytes)", item.MimeType, base64DecodedLen(item.Data))
			combined.WriteString(desc)
		case "embedded_resource":
			name := item.URI
			if name == "" {
				name = "embedded-resource"
			}
			combined.WriteString(fmt.Sprintf("resource: %s", name))
			if item.Text != "" {
				combined.WriteString("\n")
				combined.WriteString(item.Text)
			}
		case "audio":
			combined.WriteString("audio content")
		default:
			if item.Text != "" {
				combined.WriteString(item.Text)
			} else {
				combined.WriteString(fmt.Sprintf("%s content", item.Type))
			}
		}
	}

	return combined.String(), nil
}

// base64DecodedLen returns the approximate byte size of a base64-encoded
// payload without decoding the full string (cheap for logging).
func base64DecodedLen(s string) int {
	if s == "" {
		return 0
	}
	n := len(s)
	pad := 0
	if n >= 1 && s[n-1] == '=' {
		pad++
	}
	if n >= 2 && s[n-2] == '=' {
		pad++
	}
	return (n*3)/4 - pad
}

// NamespacePrefix is the registry-name prefix shared by every tool exposed by
// a standalone MCP server (mcp_<server>_<tool>).
const NamespacePrefix = "mcp_"

// ToolPrefix returns the registry-name prefix under which the tools of a given
// standalone MCP server are registered (e.g. "mcp_filesystem_"). Callers use
// it to find/remove all tools that belong to one server.
func ToolPrefix(serverName string) string {
	return NamespacePrefix + serverName + "_"
}

// ToolName returns the canonical registry name for a tool exposed by a
// standalone MCP server: mcp_<server>_<tool>.
func ToolName(serverName, toolName string) string {
	return ToolPrefix(serverName) + toolName
}

// MCPTool adapts a tool discovered on a standalone MCP server so it can be
// registered into the tool registry and invoked from the agent loop. It
// satisfies the tool.Tool interface (Name/Description/Schema/Execute) by
// construction, even though this package intentionally does not import the
// tool package (avoiding the tool -> kanban -> config -> mcp import cycle).
type MCPTool struct {
	serverName string
	tool       Tool
	manager    *Manager
}

// NewMCPTool creates a registry adapter for a single MCP server tool.
func NewMCPTool(serverName string, tool Tool, manager *Manager) *MCPTool {
	return &MCPTool{serverName: serverName, tool: tool, manager: manager}
}

// Name returns the tool name (mcp_<server>_<tool>).
func (t *MCPTool) Name() string {
	return ToolName(t.serverName, t.tool.Name)
}

// Description returns the tool description.
func (t *MCPTool) Description() string {
	return fmt.Sprintf("MCP %s: %s", t.serverName, t.tool.Description)
}

// Schema returns the input schema in OpenAI function-calling JSON Schema form.
func (t *MCPTool) Schema() map[string]interface{} {
	return t.tool.InputSchema
}

// Parameters returns the tool parameters (deprecated alias of Schema kept for
// callers that still use the old adapter shape).
func (t *MCPTool) Parameters() map[string]interface{} {
	return t.Schema()
}

// Execute executes the MCP tool.
func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return t.manager.CallTool(ctx, t.serverName, t.tool.Name, args)
}

// RequiresApproval 实现 tool.ApprovalAware：报告这次调用是否需要人工确认。
//
// 判定依据是 MCP 的 tool annotations（2025-03-26 引入，可选字段）：
//   - 显式 destructiveHint → 必须确认（能删数据、能发消息）；
//   - openWorldHint 且不是只读 → 会触达外部世界，确认；
//   - readOnlyHint → 不确认（纯查询）；
//   - 没写 annotations → 无从判断，默认不拦，由 server 级
//     require_approval 兜底。
//
// 这条链路以前完全缺失：annotations 解析出来了，但没有任何消费方，于是
// 一个暴露 delete_file 的 MCP server 可以被模型直接调用，而同样的操作走
// bash 却要弹窗。
func (t *MCPTool) RequiresApproval() bool {
	if t.manager != nil && t.manager.ServerRequiresApproval(t.serverName) {
		return true
	}
	a := t.tool.Annotations
	if a == nil {
		return false
	}
	if a.DestructiveHint {
		return true
	}
	if a.ReadOnlyHint {
		return false
	}
	return a.OpenWorldHint
}

// ApprovalSummary 给审批卡片一句人类可读的动作描述。
func (t *MCPTool) ApprovalSummary() string {
	return fmt.Sprintf("MCP %s → %s", t.serverName, t.tool.Name)
}

// IsReadOnly 报告工具是否被标注为只读（供 UI 展示"安全/可写"标记）。
func (t *MCPTool) IsReadOnly() bool {
	return t.tool.Annotations != nil && t.tool.Annotations.ReadOnlyHint
}

// ServerRequiresApproval 报告某个 server 是否被配置为"每次调用都确认"。
func (m *Manager) ServerRequiresApproval(name string) bool {
	m.mu.RLock()
	client, ok := m.clients[name]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	return client.config.RequireApproval
}

// isConnected reports whether a server is currently connected.
func (m *Manager) isConnected(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.clients[name]
	return ok
}

// markDisconnected records that the underlying transport has failed, so health
// and status queries stop reporting a dead connection as alive. The reason is
// kept so the UI can show *why* a server went down, and an automatic reconnect
// is scheduled (bounded attempts with linear backoff).
func (c *Client) markDisconnected(err error) {
	c.mu.Lock()
	c.connected = false
	if err != nil {
		c.lastError = err.Error()
		c.lastErrorKind = KindOf(err)
	}
	c.mu.Unlock()

	if c.manager != nil {
		c.manager.scheduleReconnect(c.name)
	}
}

// recordCall 累计一次工具调用的统计（调用数 / 失败数 / 最近一次错误）。
func (c *Client) recordCall(toolName string, err error) {
	if c.manager == nil {
		return
	}
	c.manager.recordToolStat(c.name, toolName, err)
}

// scheduleReconnect 在后台尝试自动重连一个断开的 server：
// 最多 MaxReconnectAttempts 次，线性退避（reconnectDelay × 次数）。
// 同一个 server 同时只允许一个重连循环（reconnecting 去重）；用户手动
// Disconnect 删掉条目后循环自然退出；并发窗口里被抢先连上（"already
// connected"）也视为成功。
func (m *Manager) scheduleReconnect(name string) {
	m.mu.Lock()
	if m.reconnecting[name] {
		m.mu.Unlock()
		return
	}
	if _, ok := m.clients[name]; !ok {
		// 已被用户主动断开/删除，不必复活。
		m.mu.Unlock()
		return
	}
	m.reconnecting[name] = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.reconnecting, name)
			m.mu.Unlock()
		}()

		for attempt := 1; attempt <= MaxReconnectAttempts; attempt++ {
			time.Sleep(m.reconnectDelay * time.Duration(attempt))

			m.mu.RLock()
			client, ok := m.clients[name]
			stillDown := ok && !client.connected
			m.mu.RUnlock()
			if !stillDown {
				return // 已恢复（手动重连/健康检查成功）或已被删除
			}

			err := m.Reconnect(name)
			if err == nil || strings.Contains(err.Error(), "already connected") {
				log.Infof("[MCP] auto-reconnected server %q after %d attempt(s)", name, attempt)
				return
			}
			log.Warnf("[MCP] auto-reconnect %q attempt %d/%d failed: %v",
				name, attempt, MaxReconnectAttempts, err)
		}
		log.Warnf("[MCP] auto-reconnect %q gave up after %d attempts; manual reconnect required",
			name, MaxReconnectAttempts)
	}()
}

// markConnected clears any previous failure and marks the client healthy.
func (c *Client) markConnected() {
	c.mu.Lock()
	c.connected = true
	c.lastError = ""
	c.lastErrorKind = ""
	c.mu.Unlock()
}

// ConnectConfigured connects every configured standalone MCP server. Per-server
// failures are collected into a single error so one broken server never blocks
// the others (matching the isolation policy used for Agent Plugin MCP servers).
// Callers then sync the discovered tools into their tool registry (see
// mcpbridge.SyncToRegistry).
// ConnectConfigured connects every server in the map and reports the failures.
//
// 并发连接: 每个 server 一个 goroutine。旧实现是串行 for 循环，总耗时等于
// 各 server 握手耗时之和(每台上限 DefaultTimeout=30s)，多台 server 时会
// 让启动期后台连接拖到几分钟；并发后总耗时等于最慢的一台。
// Manager 自身的锁已在 connect 中拆开(握手不持锁)，因此并发是安全的。
// 失败信息按 server 名排序后再拼接，保证同样的失败集合产出同样的错误串。
func (m *Manager) ConnectConfigured(servers map[string]ServerConfig) error {
	var (
		wg       sync.WaitGroup
		failMu   sync.Mutex
		failures []string
	)

	for name, cfg := range servers {
		if m.isConnected(name) {
			continue
		}

		wg.Add(1)
		go func(name string, cfg ServerConfig) {
			defer wg.Done()

			var err error
			switch cfg.Transport {
			case TransportHTTP, TransportSSE:
				err = m.ConnectSSE(name, cfg)
			default:
				err = m.ConnectStdio(name, cfg)
			}

			if err != nil {
				failMu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", name, err))
				failMu.Unlock()
				log.Warnf("[MCP] failed to connect server %q: %v", name, err)
				return
			}
			log.Infof("[MCP] connected server %q", name)
		}(name, cfg)
	}

	wg.Wait()

	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("failed to connect MCP server(s): %s", strings.Join(failures, "; "))
	}
	return nil
}

// DisconnectAll disconnects every connected MCP server. Safe to call multiple
// times (e.g. from deferred shutdown paths).
func (m *Manager) DisconnectAll() {
	for _, name := range m.ListServers() {
		_ = m.Disconnect(name)
	}
}

// --- 工具可见性（per-server disabled_tools）---

// SetDisabledTools 用配置刷新"按 server 屏蔽的工具"表。整表替换，因此
// 传入空 map 等价于"全部放行"。
//
// 屏蔽发生在注册表侧（mcpbridge 不注册被屏蔽的工具），这里只保留判定
// 依据；管理器本身不改变 tools/list 的结果，因为"发现"和"暴露给模型"
// 是两件事——后者才决定 prompt 的体积。
func (m *Manager) SetDisabledTools(byServer map[string][]string) {
	next := make(map[string]map[string]bool, len(byServer))
	for server, tools := range byServer {
		if len(tools) == 0 {
			continue
		}
		set := make(map[string]bool, len(tools))
		for _, t := range tools {
			if t = strings.TrimSpace(t); t != "" {
				set[t] = true
			}
		}
		if len(set) > 0 {
			next[server] = set
		}
	}
	m.mu.Lock()
	m.disabled = next
	m.mu.Unlock()
}

// IsToolDisabled 报告某 server 下的某个工具是否被用户屏蔽。
func (m *Manager) IsToolDisabled(server, tool string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	set, ok := m.disabled[server]
	if !ok {
		return false
	}
	return set[tool]
}

// --- 调用统计 ---

// recordToolStat 累计一次工具调用。
func (m *Manager) recordToolStat(server, tool string, err error) {
	key := server + "/" + tool
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stats == nil {
		m.stats = make(map[string]*ToolStat)
	}
	st, ok := m.stats[key]
	if !ok {
		st = &ToolStat{}
		m.stats[key] = st
	}
	st.Calls++
	st.LastCallAt = time.Now()
	if err != nil {
		st.Failures++
		st.LastError = err.Error()
	} else {
		st.LastError = ""
	}
}

// ToolStats 返回某个 server 下全部工具的调用统计（工具名 → 统计）。
func (m *Manager) ToolStats(server string) map[string]ToolStat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix := server + "/"
	out := make(map[string]ToolStat)
	for key, st := range m.stats {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		out[strings.TrimPrefix(key, prefix)] = *st
	}
	return out
}

// --- 后台健康巡检 ---

// DefaultHealthInterval 是后台巡检的默认间隔。
//
// 取 3 分钟的理由：MCP server 多为本地子进程或常驻服务，掉线后用户在几分钟
// 内就会用到它；间隔再长，连接器页会长期显示"已连接"而实际早死了。ping 本身
// 很便宜（一个 JSON-RPC 往返，5s 超时）。
const DefaultHealthInterval = 3 * time.Minute

// StartHealthMonitor 启动后台健康巡检（幂等：重复调用只有第一次生效）。
//
// 之前只有手动点"健康检查"才会 ping，空闲的 server 掉线后状态会一直陈旧，
// 用户在列表里看到绿灯、模型调用时才踩雷。
func (m *Manager) StartHealthMonitor(interval time.Duration) {
	if interval <= 0 {
		interval = DefaultHealthInterval
	}
	m.healthOnce.Do(func() {
		stop := make(chan struct{})
		m.mu.Lock()
		m.healthStop = stop
		m.mu.Unlock()
		go m.healthLoop(interval, stop)
	})
}

// StopHealthMonitor 停止后台巡检（未启动时安全）。
func (m *Manager) StopHealthMonitor() {
	m.mu.Lock()
	stop := m.healthStop
	m.healthStop = nil
	m.mu.Unlock()
	if stop != nil {
		select {
		case <-stop:
			// already closed
		default:
			close(stop)
		}
	}
}

func (m *Manager) healthLoop(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			m.checkAllHealth()
		}
	}
}

// checkAllHealth 并发（最多 4 路）ping 所有已连接的 server。
// 失败的由 HealthCheck 自己 markDisconnected，从而进入自动重连流程。
func (m *Manager) checkAllHealth() {
	names := m.ListServers()
	if len(names) == 0 {
		return
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := m.HealthCheck(name); err != nil {
				log.Warnf("[MCP] health check failed for %q: %v", name, err)
			}
		}(name)
	}
	wg.Wait()
}

// HealthCheck checks the health of a connected MCP server
func (m *Manager) HealthCheck(name string) error {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	client.mu.RLock()
	connected := client.connected
	client.mu.RUnlock()
	m.mu.RUnlock()

	if !connected {
		return fmt.Errorf("MCP server '%s' is disconnected", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ping 的 id 用 uuid：pending 表按 id 索引，固定 ID 的两次并发
	// HealthCheck 会互相覆盖 waiter，第一个永远等不到响应。
	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "ping",
		ID:      uuid.New().String(),
	}

	resp, err := client.transport.Send(ctx, req)
	if err != nil {
		client.markDisconnected(err)
		return err
	}

	if resp.Error != nil {
		pingErr := fmt.Errorf("ping error: %s", resp.Error.Message)
		client.markDisconnected(pingErr)
		return pingErr
	}

	// Update last health check time and restore connected state.
	client.mu.Lock()
	client.connected = true
	client.lastError = ""
	client.lastHealthCheck = time.Now()
	client.mu.Unlock()

	return nil
}

// Reconnect attempts to reconnect to an MCP server
func (m *Manager) Reconnect(name string) error {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	// config 是构造后不再改写的只读字段，在 Manager 读锁内取值即可。
	// 旧实现在这里额外调了一次 client.mu.RUnlock() 却没有先 RLock，
	// 对已连接的 server 重连必然触发 "sync: RUnlock of unlocked RWMutex".
	config := client.config
	m.mu.RUnlock()

	// 先断开旧连接(失败可忽略：目标就是重建)，再走统一的连接路径。
	if err := m.Disconnect(name); err != nil {
		log.Warnf("[MCP] reconnect: disconnect %q before reconnect failed: %v", name, err)
	}

	return m.connect(name, config)
}

// RefreshTools rediscovers tools from a connected MCP server
func (m *Manager) RefreshTools(name string) error {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	m.mu.RUnlock()

	// 沿用握手预算：Background 会让一个不响应的服务端把调用方无限挂住。
	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()

	if err := client.discoverTools(ctx); err != nil {
		return fmt.Errorf("failed to refresh tools: %w", err)
	}

	return nil
}

// GetServerInfo returns information about a connected MCP server
func (m *Manager) GetServerInfo(name string) (map[string]interface{}, error) {
	m.mu.RLock()
	client, exists := m.clients[name]
	m.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("MCP server '%s' not found", name)
	}
	// Snapshot every client field under the client lock: reading `connected`
	// after unlocking would race with Reconnect/disconnect paths.
	client.mu.RLock()
	toolCount := len(client.tools)
	lastCheck := client.lastHealthCheck
	connected := client.connected
	lastError := client.lastError
	lastErrorKind := client.lastErrorKind
	transport := client.config.Transport
	serverName := client.serverName
	serverVersion := client.serverVersion
	protocol := client.negotiatedVersion
	client.mu.RUnlock()

	state := StateConnected
	switch {
	case connected:
		state = StateConnected
	case lastErrorKind == ErrKindAuth:
		state = StateUnauthorized
	case lastError != "":
		state = StateError
	default:
		state = StateDisconnected
	}

	return map[string]interface{}{
		"name":              name,
		"connected":         connected,
		"state":             state,
		"tool_count":        toolCount,
		"transport":         transport,
		"last_health_check": lastCheck,
		"last_error":        lastError,
		"error_kind":        string(lastErrorKind),
		"error_hint":        ErrorKindHint(lastErrorKind),
		"server_name":       serverName,
		"server_version":    serverVersion,
		"protocol":          protocol,
	}, nil
}

func jsonMarshal(v interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ConfigLoader loads MCP configuration
type ConfigLoader struct{}

// LoadFromConfig loads MCP servers from configuration. It is a thin alias of
// ConnectConfigured (并发连接 + 故障隔离)，两者曾经行为不一致：这个入口是
// 串行的，多台 server 的启动耗时是各台之和。收敛成一个实现，避免漂移。
func (cl *ConfigLoader) LoadFromConfig(mgr *Manager, servers map[string]ServerConfig) error {
	return mgr.ConnectConfigured(servers)
}
