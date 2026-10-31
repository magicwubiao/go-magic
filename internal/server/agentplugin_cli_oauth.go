package server

// 插件内 CLI 连接器的 OAuth（设备码）路由与视图。
//
//	POST /api/agent-plugins/{name}/cli/{connector}/oauth/start
//	GET  /api/agent-plugins/{name}/cli/{connector}/oauth/status
//	POST /api/agent-plugins/{name}/cli/{connector}/oauth/signout
//
// 与全局连接器是同一套流程（serveCLIOAuth），区别只在 token 键带插件
// 命名空间：同一个 SQL 客户端在 A 插件里用账号甲、B 插件里用账号乙时，
// 两枚 token 不能互相覆盖。

import (
	"fmt"
	"net/http"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// pluginCLIConnector 取插件内某个 CLI 连接器（锁外使用副本，配置只读）。
func (s *Server) pluginCLIConnector(pluginName, connectorName string) (*agentplugin.ManagedPlugin, cliconn.Connector, bool) {
	s.acquireServerMu("")
	plugins := s.agentPlugins
	s.releaseServerMu()

	mp, ok := plugins[pluginName]
	if !ok || mp == nil || mp.Plugin == nil {
		return nil, cliconn.Connector{}, false
	}
	for _, st := range mp.Plugin.CLIConnectors {
		if st.Name != connectorName {
			continue
		}
		return mp, st.Connector, true
	}
	return mp, cliconn.Connector{}, false
}

// handlePluginCLIOAuth 处理插件内 CLI 连接器的 OAuth 动作。
func (s *Server) handlePluginCLIOAuth(w http.ResponseWriter, r *http.Request, pluginName, connectorName, action string) {
	_, conn, ok := s.pluginCLIConnector(pluginName, connectorName)
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("CLI connector %q not found in plugin %q", connectorName, pluginName))
		return
	}
	s.serveCLIOAuth(w, r, agentplugin.CLIOAuthKey(pluginName, connectorName), conn.OAuth, action, func() {
		// 工具在构造时就把连接器（含 token 源）拷贝进去了，但 token 是执行期
		// 才取的，所以登出无需重注册工具；摘要里的状态每次请求现算。
		log.Infof("[AgentPlugin] CLI connector %s/%s signed out OAuth token", pluginName, connectorName)
	})
}

// pluginCLIConnectorOAuthStatus 返回插件内连接器的授权状态（未配置时 none）。
func (s *Server) pluginCLIConnectorOAuthStatus(pluginName, connectorName string) mcp.OAuthFlowStatus {
	mgr := s.cliOAuthManager()
	if mgr == nil {
		return mcp.OAuthFlowStatus{State: "none"}
	}
	return mgr.Status(agentplugin.CLIOAuthKey(pluginName, connectorName))
}

// decorateCLIConnectorOAuth 给 Summary 的 CLI 条目补 OAuth 视图。
//
// 只能在这里算：Manager 不认识 OAuth 管理器（它属于 server），而 UI 需要
// "已登录 / 未登录 / 等待授权"来决定按钮与状态标签。
func (s *Server) decorateCLIConnectorOAuth(item map[string]any, mp *agentplugin.ManagedPlugin) {
	entries, ok := item["cli_connectors"].([]map[string]any)
	if !ok || len(entries) == 0 || mp == nil || mp.Plugin == nil {
		return
	}
	pluginName, _ := item["name"].(string)
	conns := make(map[string]cliconn.Connector, len(mp.Plugin.CLIConnectors))
	for _, st := range mp.Plugin.CLIConnectors {
		conns[st.Name] = st.Connector
	}

	mgr := s.cliOAuthManager()
	for _, e := range entries {
		connName, _ := e["name"].(string)
		conn, ok := conns[connName]
		if !ok || conn.OAuth == nil {
			continue
		}
		e["oauth_configured"] = conn.OAuth.Configured()
		e["oauth_token_env"] = conn.OAuth.TokenEnv
		if mgr == nil {
			continue
		}
		st := mgr.Status(agentplugin.CLIOAuthKey(pluginName, connName))
		e["oauth_state"] = st.State
		if st.Error != "" {
			e["oauth_error"] = st.Error
		}
		if st.UserCode != "" {
			e["oauth_user_code"] = st.UserCode
		}
		if st.VerificationURI != "" {
			e["oauth_verification_uri"] = st.VerificationURI
		}
	}
}
