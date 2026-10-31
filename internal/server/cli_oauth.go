package server

// CLI 连接器的 OAuth（设备码流程）。
//
// 适用场景：CLI 只认环境变量里的 token（GH_TOKEN / VERCEL_TOKEN / …），
// 本身没有登录命令，或者登录命令必须开浏览器、跑不通"委托登录"。
// 这时由本程序按 RFC 8628 拿 token，执行子进程前注入 TokenEnv 指向的变量。
//
// 与 MCP 共用 mcp.OAuthManager 与 oauth_tokens.json，键加 cliconn: 前缀：
//   - 一份凭据文件，登出/过期/刷新只有一套逻辑；
//   - token 不落 config.json（那里只放端点与 client_id）；
//   - 刷新在 EnsureAccessToken 里静默完成，CLI 侧无感。
//
// 请求：
//   POST /api/cli/connectors/{name}/oauth/start    起设备码流程（幂等）
//   GET  /api/cli/connectors/{name}/oauth/status   轮询状态（含短码/授权 URL）
//   POST /api/cli/connectors/{name}/oauth/signout  登出（删 token）

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// cliOAuthKeyPrefix 是 CLI 连接器在共享 token 存储里的命名空间。
const cliOAuthKeyPrefix = "cliconn:"

// cliOAuthKey 拼出存储键。
func cliOAuthKey(name string) string { return cliOAuthKeyPrefix + name }

// cliOAuthSource 把共享的 OAuth 管理器绑定到某个连接器，
// 实现 cliconn.TokenSource（执行期按需取 token）。
//
// manager 每次调用时再取：连接器的注册发生在启动早期，那时 mcpMgr 可能
// 还没构造好。缓存下来会得到一个永远为 nil 的源，所有带 oauth 的调用都报
// "no token store is wired"。
type cliOAuthSource struct {
	srv *Server
	// key 是 token 存储键：全局连接器 cliconn:<name>，
	// 插件内连接器 cliconn:agentplugin/<plugin>/<connector>。
	key string
}

func (s cliOAuthSource) EnsureAccessToken() (string, error) {
	mgr := s.srv.cliOAuthManager()
	if mgr == nil {
		return "", fmt.Errorf("OAuth is not configured on this server")
	}
	return mgr.EnsureAccessToken(s.key)
}

// pluginCLITokenSource 是 agentplugin.CLITokenSourceFactory 的 server 侧实现。
//
// 惰性绑定 server 而不是直接拿 *mcp.OAuthManager：插件加载早于 MCP 管理器
// 就绪的场景真实存在（启动早期），提前取会拿到 nil 并永久失效。
func (s *Server) pluginCLITokenSource(pluginName, connectorName string) cliconn.TokenSource {
	return cliOAuthSource{srv: s, key: agentplugin.CLIOAuthKey(pluginName, connectorName)}
}

// cliOAuthManager 返回共享的 OAuth 管理器（未配置时 nil）。
func (s *Server) cliOAuthManager() *mcp.OAuthManager {
	if s.mcpMgr == nil {
		return nil
	}
	return s.mcpMgr.OAuth()
}

// withCLIOAuthTokenSource 给声明了 OAuth 的连接器挂上 token 源。
//
// 挂在**配置副本**上：Connector.OAuth.Tokens 是 json:"-" 的运行时字段，
// 不会写回 config.json，也不会被复制到别处。
func (s *Server) withCLIOAuthTokenSource(conns map[string]cliconn.Connector) {
	for name, c := range conns {
		if c.OAuth == nil {
			continue
		}
		// 必须复制一份：Config 里的 OAuth 是共享指针，直接写 Tokens 会改到
		// 内存中的用户配置，并且和已注册工具里的读取并发（-race 会报）。
		spec := *c.OAuth
		spec.Tokens = cliOAuthSource{srv: s, key: cliOAuthKey(name)}
		c.OAuth = &spec
		conns[name] = c
	}
}

// handleCLIConnectorOAuth 处理全局连接器级 OAuth 的三个动作。
func (s *Server) handleCLIConnectorOAuth(w http.ResponseWriter, r *http.Request, name, action string) {
	conn, ok := s.cliConnectorConfigs()[name]
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("CLI connector %q not found", name))
		return
	}
	s.serveCLIOAuth(w, r, cliOAuthKey(name), conn.OAuth, action, func() {
		// 登出后立刻重同步：下次调用会拿到 auth 类错误，而不是带着旧 token 重试。
		s.applyCLIConnectors()
		log.Infof("[CLI] signed out OAuth token for %q", name)
	})
}

// serveCLIOAuth 是设备码流程的唯一实现，全局连接器与插件内连接器共用。
//
// key 决定 token 落在哪（命名空间由调用方拼好）；onSignout 用于登出后的
// 宿主侧收尾（重同步连接器 / 刷新插件工具），nil 表示无需额外动作。
func (s *Server) serveCLIOAuth(w http.ResponseWriter, r *http.Request, key string, spec *cliconn.OAuthSpec, action string, onSignout func()) {
	mgr := s.cliOAuthManager()
	if mgr == nil {
		jsonError(w, http.StatusBadRequest, "OAuth is not configured on this server")
		return
	}

	switch action {
	case "start":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !spec.Configured() {
			jsonError(w, http.StatusBadRequest,
				"this connector has no oauth block (device_authorization_endpoint / token_endpoint / client_id / token_env)")
			return
		}
		dc, err := mgr.StartDeviceAuthorization(key, mcp.DeviceFlowConfig{
			DeviceAuthorizationEndpoint: spec.DeviceAuthorizationEndpoint,
			TokenEndpoint:               spec.TokenEndpoint,
			ClientID:                    spec.ClientID,
			ClientSecret:                spec.ClientSecret,
			Scopes:                      spec.Scopes,
			Audience:                    spec.Audience,
		})
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		status := mgr.Status(key)
		resp := map[string]any{
			"ok":     true,
			"status": status,
			// dc == nil 表示已有可用 token（幂等），此时 status.state=authorized。
			"already_authorized": dc == nil,
		}
		if dc != nil {
			resp["device"] = map[string]any{
				"user_code":                 dc.UserCode,
				"verification_uri":          dc.VerificationURI,
				"verification_uri_complete": dc.VerificationURIComplete,
				"expires_in":                dc.ExpiresIn,
				"interval":                  dc.Interval,
			}
		}
		jsonResponse(w, resp)

	case "status":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		jsonResponse(w, map[string]any{
			"ok":         true,
			"status":     mgr.Status(key),
			"configured": spec.Configured(),
		})

	case "signout":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := mgr.SignOut(key); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if onSignout != nil {
			onSignout()
		}
		jsonResponse(w, map[string]any{"ok": true})

	default:
		http.NotFound(w, r)
	}
}

// cliOAuthStatusText 把 mcp 的状态翻译成前端直接可用的一句话。
//
// 单独放在服务端而不是前端拼：状态机（none/expired/awaiting/failed）
// 是后端概念，前端只管展示。
func cliOAuthStatusText(st mcp.OAuthFlowStatus) string {
	switch st.State {
	case mcp.OAuthStateAuthorized:
		return "authorized"
	case mcp.OAuthStateAwaitingAuthorization:
		if st.UserCode != "" {
			return "awaiting_device_code"
		}
		return "awaiting_authorization"
	case mcp.OAuthStateFailed:
		return "failed"
	default:
		return strings.ToLower(st.State)
	}
}
