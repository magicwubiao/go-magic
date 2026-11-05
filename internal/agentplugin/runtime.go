package agentplugin

import (
	"context"
	"fmt"
	"os"
	"sort"
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
// 实现上**不再自带协议栈**：条目的 command/url/headers/cwd 被翻译成
// mcp.ServerConfig，交给 shared 的 mcp.Manager 去连。这带来的是同一套
// 连接器已有的能力：
//   - 协议版本协商（握手期逐个试，服务端不支持时自动降级）；
//   - tools/list 分页（nextCursor），大 server 不会被静默截断成第一页；
//   - image / audio / resource / structuredContent 全部内容类型；
//   - 链路死亡检测 + 自动重连（stdio 子进程退出后不再永久失效）；
//   - 懒连接 + 工具缓存（启动时只注册影子工具，首次调用才建连）；
//   - 准入白名单（mcp.Manager 的 SetSecurityCheck 是唯一收口）。
//
// 规范要求：单个 MCP server 启动失败仅禁用该条目，其余 server/skills/扩展
// 继续可用。Start 对每个静态校验通过的条目尝试连接，失败记入 entry.Error，
// 不传播异常。
type Runtime struct {
	plugin *Plugin

	// mgr 是共享的 MCP 管理器（由 server 注入，与连接器共用 token store 与
	// 工具缓存）。NewRuntime 会先建一个独立的兜底管理器，便于测试与 CLI
	// 单机使用；server 侧务必用 SetMCPManager 换成共享实例，否则 OAuth
	// 回调、工具缓存会各存一份。
	mgr *mcp.Manager

	mu sync.RWMutex

	// registered 记录每个条目注册进注册表的工具名（key = 条目名）。
	// 按条目分组而不是平铺：单条目重连只清它自己的工具，不能顺带把
	// 别的条目的工具从注册表里抹掉几十秒。
	registered map[string][]string

	// reg/pluginName 记录最近一次 RegisterTools 的注册表与插件名，
	// 供 ReconnectEntry 在重连后"先注销旧工具再重注册新工具"。
	reg        ToolRegistrar
	pluginName string

	// entryLocks 按条目名串行化重连：并发重连同一个条目（保存凭据 +
	// 点重试 + OAuth 回调可能同时发生）会互相关掉对方刚建的连接。
	entryLocks map[string]*sync.Mutex

	// credentials 是用户为该插件填写的凭据(env key → value)。连接时合并进
	// stdio 子进程环境(覆盖 mcp.json 的同名气)。由 server 层注入,读写走 mu。
	credentials map[string]string

	// oauth 是共享的 OAuth 管理器（nil = 未配置，远程条目只走静态凭据）。
	// 与连接器共用同一个 token 存储，键加 agentplugin: 命名空间（见 oauth.go）。
	oauth *mcp.OAuthManager

	// stopped 由 Stop 置位。连接在后台 goroutine 里跑，reload/uninstall
	// 可能在其完成前就调 Stop：此时握手成功的连接必须立刻拆掉，
	// 否则会留下没人认领的子进程。
	stopped bool

	// suppressAutoOAuth 在批量启动期间置位：开机时把每个"装过但还没授权"
	// 的插件都弹一次浏览器是很糟的体验，交给用户在详情里点一次"登录"。
	// 手动重连 / 凭据变更 / 授权后重连不受影响。
	suppressAutoOAuth atomic.Bool
}

// NewRuntime 为已加载的插件创建 MCP 运行时(尚未连接)。
func NewRuntime(p *Plugin) *Runtime {
	return &Runtime{
		plugin:      p,
		mgr:         mcp.NewManager(),
		registered:  make(map[string][]string),
		entryLocks:  make(map[string]*sync.Mutex),
		credentials: nil,
	}
}

// SetMCPManager 注入共享的 MCP 管理器（server 层在加载后立即调用）。
//
// 必须是共享实例：OAuth token、授权流程状态、工具缓存都挂在管理器上，
// 每个插件各建一个就等于把"一次授权、处处可用"拆成了 N 份。
func (r *Runtime) SetMCPManager(mgr *mcp.Manager) {
	if mgr == nil {
		return
	}
	r.mu.Lock()
	r.mgr = mgr
	r.mu.Unlock()
}

// manager 返回当前 MCP 管理器（锁内读，nil 时回退到兜底实例）。
func (r *Runtime) manager() *mcp.Manager {
	r.mu.RLock()
	mgr := r.mgr
	r.mu.RUnlock()
	return mgr
}

// key 返回条目在 MCP 管理器里的名字，同时也是 OAuth token 的键。
//
// 两个用途共用一个字符串是有意的：token 存储按 key 索引，管理器的
// OnOAuthAuthorized 回调拿到的就是这个 key，server 直接 ParseOAuthKey
// 就能找回插件名与条目名，不需要额外的映射表。
func (r *Runtime) key(serverName string) string {
	return OAuthKey(r.pluginNameOrEmpty(), serverName)
}

// Start 连接所有静态校验通过的 MCP server。PLUGIN_DATA 目录不存在时自动创建。
// 任何条目失败仅记录到 plugin.MCPServers[i].Error,不影响其他条目。
func (r *Runtime) Start() error {
	if r.plugin == nil {
		return fmt.Errorf("nil plugin")
	}
	r.mu.Lock()
	r.stopped = false
	r.mu.Unlock()

	// 媒体后端先挂上，再连 MCP 条目。
	//
	// 顺序反过来的话，插件没有 mcp.json（或 mcp.json 无效）时 Start 会提前返回，
	// 声明写错的 warning 根本不会产生 —— 用户看到的是"provider 名字填了但没用"。
	// 注册本身不依赖连接：可用性由 ServerConnected 动态判定，条目连上就自动可用。
	r.RegisterMediaProviders()

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
// 串行连接时多台 server 的握手时间会相加（每台上限 15s）：一个插件里
// 声明 3 台 npx server，冷启动就要等 3 次 npx 解析/下载。并行后总耗时
// 约等于最慢的一台。条目之间不共享状态（连接结果各自写回自己的
// MCPEntryStatus，连接表在 mcp.Manager 内），并发安全。
// 上限 4 是防止一次拉起十几个子进程打爆内存/句柄。
func (r *Runtime) startParallel() {
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	pluginName := r.pluginNameOrEmpty()

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
			err := r.connectEntry(e)
			// 只给慢条目留日志：这是"服务起得慢"最直接的可观测信号。
			if took := time.Since(began); took > time.Second {
				log.Infof("[AgentPlugin] %s/%s mcp init %v (connected=%v err=%v)",
					pluginName, e.Name, took.Round(time.Millisecond), e.Connected, err)
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

// lockEntry 串行化同一个条目的连接/重连。
func (r *Runtime) lockEntry(name string) func() {
	r.mu.Lock()
	lk, ok := r.entryLocks[name]
	if !ok {
		lk = &sync.Mutex{}
		r.entryLocks[name] = lk
	}
	r.mu.Unlock()
	lk.Lock()
	return lk.Unlock
}

// connectEntry 连接单个 MCP server 并发现工具。
func (r *Runtime) connectEntry(entry *MCPEntryStatus) error {
	unlock := r.lockEntry(entry.Name)
	defer unlock()

	// 用户凭据最后合并(覆盖 mcp.json 同名 env / 替换 ${CRED:NAME}),凭据改了无需重新 Load。
	spec, missing := applyCredentials(entry.Spec, r.credentialsSnapshot())
	// env 里留空的 key 也是"待填凭据"：合并后仍为空就按缺凭据处理，
	// 不把空字符串塞进子进程再让它用 401 告诉用户。
	missing = uniqueSorted(append(missing, missingEnvCreds(entry.Spec, spec.Env)...))
	if len(missing) > 0 {
		// 缺凭据属于 auth 类失败:UI 据此亮"去授权",而不是让用户去查网络。
		err := missingCredsError(missing)
		r.setEntryStatus(entry, false, 0, err.Error(), mcp.ErrKindAuth.String())
		return err
	}

	// 地址可能带着凭据占位（局域网设备的 base URL）：加载期看不到真实主机，
	// 只能在这里复核一次，否则 ${CRED:X} 能被填成任意明文公网地址绕过 https 规则。
	// 地址来自用户填写的凭据，写错了属于"要改配置"，按 auth 类亮授权入口。
	if isRemoteSpec(spec) {
		if _, err := validateHTTPSpec(spec); err != nil {
			werr := mcp.WrapKind(mcp.ErrKindAuth, fmt.Errorf("invalid server url: %w", err))
			r.setEntryStatus(entry, false, 0, werr.Error(), mcp.ErrKindAuth.String())
			return werr
		}
	}

	cfg, err := r.serverConfigFor(entry.Name, spec)
	if err != nil {
		r.setEntryStatus(entry, false, 0, err.Error(), mcp.KindOf(err).String())
		return err
	}

	name := r.key(entry.Name)
	mgr := r.manager()
	if mgr == nil {
		err := fmt.Errorf("MCP manager is not configured")
		r.setEntryStatus(entry, false, 0, err.Error(), mcp.ErrKindUnknown.String())
		return err
	}
	// 重连前先摘掉旧连接（ConnectXxx 对已连接的条目会直接报错）。
	if mgr.IsConnected(name) {
		_ = mgr.Disconnect(name)
	}

	// 懒连接：有上一次的工具缓存时先不建连，只登记配置。首次调用由
	// mcp.Manager 的 EnsureConnected 补连，工具面来自缓存里的影子工具。
	// 缓存为空时不延后——否则模型看不到任何工具，也就永远不会触发首次调用。
	if cfg.Lazy && mgr.DeferIfCached(name, cfg) {
		toolCount := 0
		if tools, terr := mgr.ListToolsByServer(name); terr == nil {
			toolCount = len(tools)
		}
		r.setEntryState(entry, false, toolCount, "", "", true)
		return nil
	}

	var cerr error
	if isRemoteSpec(spec) {
		cerr = mgr.ConnectSSE(name, cfg)
	} else {
		cerr = mgr.ConnectStdio(name, cfg)
	}
	if cerr != nil {
		msg, kind := mgr.FailureReason(name)
		if msg == "" {
			msg = cerr.Error()
		}
		if kind == "" {
			kind = mcp.KindOf(cerr)
		}
		// 零配置远程条目（没有声明 oauth、也没有待填凭据）被 401 拒绝时，
		// 就地探测授权服务器并发起授权：用户看到"需要授权"的同时浏览器
		// 已经打开了授权页。声明了 oauth 的条目由 mcp.Manager 自己发起。
		if kind == mcp.ErrKindAuth && spec.OAuth == nil && oauthCapable(spec) {
			r.maybeStartOAuth(entry.Name, spec)
		}
		r.setEntryStatus(entry, false, 0, msg, kind.String())
		return cerr
	}

	toolCount := 0
	if tools, terr := mgr.ListToolsByServer(name); terr == nil {
		toolCount = len(tools)
	}
	// Stop 可能在这个后台连接完成之前就跑完了（卸载/禁用与启动赛跑）：
	// 那时新连接没人认领，必须立刻拆掉，否则就是一个指向已删除插件目录的
	// 孤儿子进程（Windows 上还会锁住目录，导致卸载失败）。
	r.mu.RLock()
	stopped := r.stopped
	r.mu.RUnlock()
	if stopped {
		_ = mgr.Disconnect(name)
		err := fmt.Errorf("runtime stopped")
		r.setEntryStatus(entry, false, 0, err.Error(), mcp.ErrKindUnknown.String())
		return err
	}
	r.setEntryStatus(entry, true, toolCount, "", "")
	return nil
}

// serverConfigFor 把插件条目翻译成 mcp.ServerConfig。
//
// OAuth 的取舍（与旧实现等价，但判断依据换成「token 是否已就绪」）：
//   - 显式声明 oauth：token 就绪才把 OAuth 交给 mcp.Manager（它会挂 provider
//     并在 401 时刷新）；没有 token 时，开机阶段直接报 auth 让用户点"登录"
//     （避免每装一个插件就自动弹一次浏览器），其余场景交给 Manager 自动发起；
//   - 零配置远程条目且已有（或曾有）token：挂一个空 OAuthConfig 让 provider
//     生效，401 时还能刷新；
//   - 其余：不挂，等握手 401 再探测。
func (r *Runtime) serverConfigFor(serverName string, spec MCPServerSpec) (mcp.ServerConfig, error) {
	cfg := mcp.ServerConfig{
		// 插件条目一律懒连接：有工具缓存时启动不拉起子进程，首次调用再连。
		Lazy: true,
	}
	if spec.Type == TransportSSE {
		cfg.Transport = mcp.TransportSSE
	} else {
		cfg.Transport = mcp.TransportHTTP
	}

	if !isRemoteSpec(spec) {
		cfg.Transport = mcp.TransportStdio
		cfg.Command = spec.Command
		cfg.Args = append([]string(nil), spec.Args...)
		cfg.Env = envMapToSlice(spec.Env)
		cfg.Cwd = spec.Cwd
		return cfg, nil
	}

	cfg.URL = spec.URL
	if len(spec.Headers) > 0 {
		cfg.Headers = make(map[string]string, len(spec.Headers))
		for k, v := range spec.Headers {
			cfg.Headers[k] = v
		}
	}

	oauthMgr := r.oauthSnapshot()
	if oauthMgr == nil {
		return cfg, nil
	}
	key := r.key(serverName)

	if spec.OAuth != nil {
		cp := *spec.OAuth
		if len(spec.OAuth.Scopes) > 0 {
			cp.Scopes = append([]string(nil), spec.OAuth.Scopes...)
		}
		if _, err := oauthMgr.EnsureToken(key); err != nil && r.suppressAutoOAuth.Load() {
			// 开机阶段不自动弹浏览器：把"还没授权"如实报出来，UI 会亮
			// "登录"按钮，用户点一下就走完整流程。
			return cfg, mcp.WrapKind(mcp.ErrKindAuth, fmt.Errorf(
				"OAuth authorization required for %q — click Sign in to complete it", serverName))
		}
		cfg.OAuth = &cp
		return cfg, nil
	}

	if !oauthCapable(spec) {
		return cfg, nil
	}
	switch oauthMgr.Status(key).State {
	case mcp.OAuthStateAuthorized, "expired":
		cfg.OAuth = &mcp.OAuthConfig{}
	}
	return cfg, nil
}

// setEntryStatus 在锁内写条目状态（后台连接与 HTTP 读取并发）。
func (r *Runtime) setEntryStatus(entry *MCPEntryStatus, connected bool, toolCount int, errMsg, errKind string) {
	r.setEntryState(entry, connected, toolCount, errMsg, errKind, false)
}

// setEntryState 同 setEntryStatus，额外记录"是否处于懒连接等待态"。
func (r *Runtime) setEntryState(entry *MCPEntryStatus, connected bool, toolCount int, errMsg, errKind string, deferred bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.Connected = connected
	entry.ToolCount = toolCount
	entry.Error = errMsg
	entry.ErrorKind = errKind
	entry.Deferred = deferred
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
//
// 注册表操作严格限定在本条目：只注销本条目此前注册的工具名，其它条目的
// 工具全程留在注册表里（早先的实现是"清空整个 runtime 再全量重注册"，
// 并发重连时两个条目会互相把对方的工具抹掉）。
func (r *Runtime) ReconnectEntry(serverName string) error {
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
	r.mu.RLock()
	staticErr, staticKind, connected := entry.Error, entry.ErrorKind, entry.Connected
	r.mu.RUnlock()
	if staticErr != "" && staticKind == "" && !connected {
		return fmt.Errorf("entry %q failed static validation: %s", serverName, staticErr)
	}

	reg, pluginName := r.connRegistry()
	// 重连前先注销本条目的旧工具:重连后 tools/list 可能返回不同集合,
	// 不注销会留下指向已关 transport 的僵尸工具。
	r.UnregisterEntryTools(reg, serverName)

	if err := r.connectEntry(entry); err != nil {
		return err
	}
	// 用新工具列表重注册(ReconnectEntry 只在 server 层的注册表上调用)。
	r.RegisterEntryTools(reg, pluginName, serverName)
	return nil
}

// connRegistry 取最近一次注册使用的注册表与插件名。
func (r *Runtime) connRegistry() (ToolRegistrar, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.reg, r.pluginName
}

// Stop 关闭所有 MCP 连接。
func (r *Runtime) Stop() {
	// 先摘媒体后端：连接一断，这些 provider 就没有可调用的目标了，
	// 留着会让 config 里指向它的 image_gen 变成"可用但必然失败"。
	r.UnregisterMediaProviders()

	mgr := r.manager()
	r.mu.Lock()
	r.stopped = true
	var names []string
	if r.plugin != nil {
		names = make([]string, 0, len(r.plugin.MCPServers))
		for _, ms := range r.plugin.MCPServers {
			names = append(names, ms.Name)
		}
	}
	pluginName := r.pluginNameOrEmpty()
	r.mu.Unlock()

	if mgr == nil {
		return
	}
	for _, name := range names {
		_ = mgr.Disconnect(OAuthKey(pluginName, name))
	}
}

// CallTool 调用指定 server 的工具。
func (r *Runtime) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error) {
	mgr := r.manager()
	if mgr == nil {
		return nil, fmt.Errorf("MCP manager is not configured")
	}
	return mgr.CallTool(ctx, r.key(serverName), toolName, args)
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
	return PluginToolName(t.pluginName, t.serverName, t.tool.Name)
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

// Timeout 实现 tool.TimeoutProvider：插件 MCP 工具与内置 MCP 工具同一口径
// （见 mcp.MCPTool.Timeout）。远程工具的耗时由上游服务决定，60s 的默认值
// 会把正常的慢查询（读 wiki、代码扫描）误判成失败。
func (t *MCPPluginTool) Timeout() time.Duration { return tool.MaxTimeout }

// RequiresApproval 恒为 true：插件 MCP 工具的 annotations 是插件作者自声明的，
// 不是信任根——把 destructive 工具标成 readOnlyHint 就能免审批直接执行。
// 与插件 CLI 工具同一口径（见 cliPluginTool.RequiresApproval）。
func (t *MCPPluginTool) RequiresApproval() bool { return true }

// ApprovalSummary 给审批卡片一句人类可读的动作描述。
func (t *MCPPluginTool) ApprovalSummary() string {
	return fmt.Sprintf("MCP %s/%s (plugin %s) → %s", t.pluginName, t.serverName, t.pluginName, t.tool.Name)
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

// RegisterTools 将所有已连接（或懒连接、有缓存）条目的工具注册到工具注册表,
// 并按条目记录工具名供注销使用。
func (r *Runtime) RegisterTools(reg ToolRegistrar, pluginName string) {
	if reg == nil {
		return
	}
	r.mu.Lock()
	r.reg = reg
	r.pluginName = pluginName
	r.mu.Unlock()

	for _, name := range r.entryNames() {
		r.RegisterEntryTools(reg, pluginName, name)
	}
}

// RegisterEntryTools 只注册单个条目的工具。
func (r *Runtime) RegisterEntryTools(reg ToolRegistrar, pluginName, serverName string) {
	if reg == nil {
		return
	}
	mgr := r.manager()
	if mgr == nil {
		return
	}
	tools, err := mgr.ListToolsByServer(OAuthKey(pluginName, serverName))
	if err != nil {
		// 没连上且没有缓存的条目没有工具可注册：这不是错误，状态由
		// EntryStatus 表达（UI 显示"未连接"并给出重试入口）。
		return
	}
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		t := &MCPPluginTool{
			pluginName: pluginName,
			serverName: serverName,
			tool:       tl,
			runtime:    r,
		}
		reg.Register(t)
		names = append(names, t.Name())
	}
	r.mu.Lock()
	if r.registered == nil {
		r.registered = make(map[string][]string)
	}
	r.registered[serverName] = names
	r.mu.Unlock()
}

// UnregisterEntryTools 注销单个条目此前注册的工具。
func (r *Runtime) UnregisterEntryTools(reg ToolRegistrar, serverName string) {
	u, ok := reg.(ToolUnregistrar)
	if !ok {
		return
	}
	r.mu.Lock()
	names := r.registered[serverName]
	delete(r.registered, serverName)
	r.mu.Unlock()
	for _, n := range names {
		u.Unregister(n)
	}
}

// UnregisterTools 将此前 RegisterTools 注册的工具从注册表注销。
// reg 不支持注销(仅实现 ToolRegistrar)时静默跳过。
func (r *Runtime) UnregisterTools(reg ToolRegistrar) {
	u, ok := reg.(ToolUnregistrar)
	if !ok {
		return
	}
	r.mu.Lock()
	all := r.registered
	r.registered = make(map[string][]string)
	r.mu.Unlock()

	for _, names := range all {
		for _, n := range names {
			u.Unregister(n)
		}
	}
}

// entryNames 返回所有 MCP 条目名（升序，保证注册顺序稳定）。
func (r *Runtime) entryNames() []string {
	if r.plugin == nil {
		return nil
	}
	names := make([]string, 0, len(r.plugin.MCPServers))
	for _, ms := range r.plugin.MCPServers {
		names = append(names, ms.Name)
	}
	sort.Strings(names)
	return names
}

// envMapToSlice 把 env map 转成 "k=v" 切片（排序保证子进程环境稳定）。
func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// entrySummaryLine 供日志使用：一行说明条目的传输目标（不含凭据）。
func entrySummaryLine(spec MCPServerSpec) string {
	if isRemoteSpec(spec) {
		return spec.Type + " " + strings.TrimSpace(spec.URL)
	}
	return "stdio " + strings.TrimSpace(spec.Command)
}
