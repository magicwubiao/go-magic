package agentplugin

// 插件级 OAuth 2.1 授权。
//
// 为什么插件需要它：目录里越来越多的是**远程** MCP server（SaaS 官方端点），
// 它们支持 OAuth，却要用户先去后台建一个 API key、找到填哪个 env/header。
// MCP 连接器页早就支持"点一下登录、浏览器授权完自动连上"，插件这边却只有
// 手填凭据一条路——同一个服务，从连接器页装和从插件页装，成本差一个数量级。
//
// 做法是把插件的远程条目**接进同一套 mcp.OAuthManager**：
//   - token 落在同一个 oauth_tokens.json（0600），key 加命名空间前缀
//     "agentplugin:<plugin>/<server>"，与连接器互不干扰；
//   - 发现 / 动态注册 / PKCE / 回调 listener / 刷新全部复用 mcp 包，
//     插件侧只负责"把 spec 翻译成 ServerConfig"和"把 token 塞进请求头"；
//   - 授权完成后由 server 层的 OnOAuthAuthorized 回调重连对应条目。
//
// 两条路径：
//   1. 显式：mcp.json 条目写 "oauth": {...}，连接前就用 token；没有则报
//      auth 错误并自动发起授权（onFlowStarted 会弹浏览器）。
//   2. 零配置：远程条目没有待填凭据，握手回 401 时自动探测授权服务器并
//      发起授权。声明了 API key 占位符的条目不自动走 OAuth——那类 401 是
//      "key 填错了"，把它导到授权页只会让人更困惑。

import (
	"fmt"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// oauthKeyPrefix 是插件 token 在共享 token 存储里的命名空间。
// 用 ':' 而不是 '/' 开头，避免与连接器名（用户可自由起名）撞键。
const oauthKeyPrefix = "agentplugin:"

// OAuthKey 返回插件条目在 token 存储里的键。
func OAuthKey(pluginName, serverName string) string {
	return oauthKeyPrefix + pluginName + "/" + serverName
}

// ParseOAuthKey 把 OAuthKey 拆回 (plugin, server)。
// 非插件键（连接器）返回 ok=false，调用方据此跳过。
func ParseOAuthKey(key string) (pluginName, serverName string, ok bool) {
	if !strings.HasPrefix(key, oauthKeyPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, oauthKeyPrefix)
	idx := strings.LastIndex(rest, "/")
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// SetOAuth 注入 OAuth 管理器（nil = 禁用 OAuth，全部走凭据/静态 headers）。
// 与 SetCredentials 一样由 server 层在加载后注入。
func (r *Runtime) SetOAuth(mgr *mcp.OAuthManager) {
	r.mu.Lock()
	r.oauth = mgr
	r.mu.Unlock()
}

// oauthSnapshot 取管理器快照（连接在后台 goroutine 里跑）。
func (r *Runtime) oauthSnapshot() *mcp.OAuthManager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.oauth
}

// pluginName 返回插件名（清单缺失时为空）。
func (r *Runtime) pluginNameOrEmpty() string {
	if r == nil || r.plugin == nil || r.plugin.Manifest == nil {
		return ""
	}
	return r.plugin.Manifest.Name
}

// oauthKeyFor 返回条目的 token 键。
func (r *Runtime) oauthKeyFor(serverName string) string {
	return OAuthKey(r.pluginNameOrEmpty(), serverName)
}

// isRemoteSpec 报告条目是否走 HTTP 传输（OAuth 只对远程有意义）。
func isRemoteSpec(spec MCPServerSpec) bool {
	return spec.Type == TransportStreamableHTTP || spec.Type == TransportSSE
}

// oauthCapable 报告条目是否**可以**走 OAuth：
//   - 显式声明了 oauth；或
//   - 远程条目且没有任何待填凭据（零配置探测路径）。
//
// 已有 Authorization 头的条目不算：那是用户自己配的静态凭据，不该被 OAuth 顶掉。
func oauthCapable(spec MCPServerSpec) bool {
	if !isRemoteSpec(spec) {
		return false
	}
	if spec.OAuth != nil {
		return true
	}
	if hasAuthorizationHeader(spec.Headers) {
		return false
	}
	return len(SpecCredentialKeys(spec)) == 0
}

// hasAuthorizationHeader 判断 headers 里是否已配置授权头（大小写不敏感）。
func hasAuthorizationHeader(headers map[string]string) bool {
	for k := range headers {
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "authorization", "x-api-key", "api-key", "x-auth-token":
			return true
		}
	}
	return false
}

// oauthScopes 返回条目声明的权限范围（仅展示用）。
func oauthScopes(spec MCPServerSpec) []string {
	if spec.OAuth == nil || len(spec.OAuth.Scopes) == 0 {
		return nil
	}
	return append([]string(nil), spec.OAuth.Scopes...)
}

// oauthServerConfig 把插件条目翻译成 OAuth 流程需要的 ServerConfig。
//
// 只需要 URL / 传输类型 / OAuth 配置三项：mcp.OAuthManager 用不到 headers，
// 也绝不能把插件凭据带进去（token 走独立的 Authorization 注入）。
func oauthServerConfig(spec MCPServerSpec) mcp.ServerConfig {
	transport := mcp.TransportHTTP
	if spec.Type == TransportSSE {
		transport = mcp.TransportSSE
	}
	var ocfg *mcp.OAuthConfig
	if spec.OAuth != nil {
		cp := *spec.OAuth
		if len(spec.OAuth.Scopes) > 0 {
			cp.Scopes = append([]string(nil), spec.OAuth.Scopes...)
		}
		ocfg = &cp
	}
	return mcp.ServerConfig{
		Transport: transport,
		URL:       spec.URL,
		OAuth:     ocfg,
	}
}

// StartOAuthManual 由 UI 按钮触发授权：返回浏览器要打开的 authorize URL。
// 已有有效 token 时返回 ("", nil)。不触发 onFlowStarted——前端自己会
// window.open，后端再弹一次就是两个重复标签页。
func (r *Runtime) StartOAuthManual(serverName string) (string, error) {
	mgr := r.oauthSnapshot()
	if mgr == nil {
		return "", fmt.Errorf("OAuth is not configured on this server")
	}
	spec, ok := r.entrySpec(serverName)
	if !ok {
		return "", fmt.Errorf("MCP server %q not found in plugin", serverName)
	}
	if !isRemoteSpec(spec) {
		return "", fmt.Errorf("OAuth only applies to remote (streamable-http/sse) entries")
	}
	return mgr.StartOAuthManual(r.oauthKeyFor(serverName), oauthServerConfig(spec))
}

// OAuthStatus 返回条目的授权状态快照（供 UI 轮询）。
// 未启用/不可用的条目返回 none。
func (r *Runtime) OAuthStatus(serverName string) mcp.OAuthFlowStatus {
	mgr := r.oauthSnapshot()
	if mgr == nil {
		return mcp.OAuthFlowStatus{State: "none"}
	}
	spec, ok := r.entrySpec(serverName)
	if !ok || !oauthCapable(spec) {
		return mcp.OAuthFlowStatus{State: "none"}
	}
	return mgr.Status(r.oauthKeyFor(serverName))
}

// SignOutOAuth 删除条目的 token 并终止进行中的流程（登出）。
//
// 顺带压住 10 分钟内的自动授权：登出后条目会被重连，重连必然 401，
// 没有这道闸门就会立刻弹出一个新的授权标签页。
func (r *Runtime) SignOutOAuth(serverName string) error {
	mgr := r.oauthSnapshot()
	if mgr == nil {
		return fmt.Errorf("OAuth is not configured on this server")
	}
	key := r.oauthKeyFor(serverName)
	if err := mgr.SignOut(key); err != nil {
		return err
	}
	mgr.SuppressAutoStart(key, 10*time.Minute)
	return nil
}

// entrySpec 返回条目标注的 spec（锁内拷贝）。
func (r *Runtime) entrySpec(serverName string) (MCPServerSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.plugin == nil {
		return MCPServerSpec{}, false
	}
	for i := range r.plugin.MCPServers {
		if r.plugin.MCPServers[i].Name == serverName {
			return r.plugin.MCPServers[i].Spec, true
		}
	}
	return MCPServerSpec{}, false
}

// maybeStartOAuth 在"缺 token / 被 401 拒绝"时就地发起授权流程。
//
// 异步：发现 + DCR + 起 listener 最长 20s，不能拖住连接报错；失败只记日志，
// 调用方照常把 auth 类错误向上抛（UI 据此亮"登录"按钮）。EnsureAuthorized
// 自带冷却与"进行中流程复用"，重连风暴不会连开一堆浏览器标签页。
func (r *Runtime) maybeStartOAuth(serverName string, spec MCPServerSpec) {
	mgr := r.oauthSnapshot()
	if mgr == nil || !oauthCapable(spec) {
		return
	}
	if r.suppressAutoOAuth.Load() {
		return
	}
	key := r.oauthKeyFor(serverName)
	cfg := oauthServerConfig(spec)
	go func() {
		authorizeURL, err := mgr.EnsureAuthorized(key, cfg)
		if err != nil {
			log.Debugf("[AgentPlugin] OAuth discovery for %s skipped: %v", key, err)
			return
		}
		if authorizeURL == "" {
			return // 已有 token 或冷却中，不打扰用户
		}
		log.Infof("[AgentPlugin] OAuth flow started for %s — browser should open at the authorize page", key)
	}()
}

// enrichOAuthStatus 给条目状态补上 OAuth 视图字段。
//
// 只读 token 存储，不发起任何网络请求；在锁外的状态快照上逐条填充，
// 避免把 token 文件的 IO 塞进 Runtime.mu。
//
// "可用"的判定比 oauthCapable 更保守：零配置条目只有**已经授权过**或
// **正因 401 连不上**时才把登录入口摆出来。否则 context7 / deepwiki 这类
// 公共远程 server（不需要任何凭据、已经连上）也会长出一个"OAuth 登录"
// 按钮，点下去只会得到"找不到授权服务器"。
func (r *Runtime) enrichOAuthStatus(st *MCPEntryStatus) {
	if st == nil || r.oauthSnapshot() == nil || !oauthCapable(st.Spec) {
		return
	}
	flow := r.OAuthStatus(st.Name)
	declared := st.Spec.OAuth != nil
	if !declared && flow.State == "none" && st.ErrorKind != string(mcp.ErrKindAuth) {
		return
	}
	st.OAuthAvailable = true
	st.OAuthState = flow.State
	st.OAuthError = flow.Error
}
