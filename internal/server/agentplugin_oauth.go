package server

// 插件条目的 OAuth 授权 API。
//
//	POST /api/agent-plugins/{name}/mcp/{server}/oauth/start   — 发起授权，返回 authorize_url
//	GET  /api/agent-plugins/{name}/mcp/{server}/oauth/status  — 轮询状态
//	POST /api/agent-plugins/{name}/mcp/{server}/oauth/signout — 登出（删 token）
//
// 与连接器页同一套语义：token 存在共享的 oauth_tokens.json 里，键为
// agentplugin:<plugin>/<server>；授权完成后由 server.go 的 OnOAuthAuthorized
// 回调重连该条目，用户不需要回插件页再点一次"重试"。

import (
	"net/http"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// oauthStatusView 是给前端的授权状态（绝不含 token）。
type oauthStatusView struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

func toOAuthStatusView(st mcp.OAuthFlowStatus) oauthStatusView {
	return oauthStatusView{State: st.State, Error: st.Error}
}

// handlePluginEntryOAuth 处理单个插件 MCP 条目的 OAuth 动作。
func (s *Server) handlePluginEntryOAuth(w http.ResponseWriter, r *http.Request, name, server, action string) {
	s.acquireServerMu("")
	plugins := s.agentPlugins
	s.releaseServerMu()
	mp, ok := plugins[name]
	if !ok || mp == nil || mp.Plugin == nil {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}
	if mp.Runtime == nil {
		http.Error(w, "plugin has no MCP runtime (disabled or no mcp.json)", http.StatusConflict)
		return
	}
	if _, exists := mp.Runtime.EntryStatus(server); !exists {
		http.Error(w, "MCP server entry not found", http.StatusNotFound)
		return
	}

	switch action {
	case "start":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authorizeURL, err := mp.Runtime.StartOAuthManual(server)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonResponse(w, map[string]any{
			"authorize_url":      authorizeURL,
			"already_authorized": authorizeURL == "",
			"status":             toOAuthStatusView(mp.Runtime.OAuthStatus(server)),
		})

	case "status":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		jsonResponse(w, toOAuthStatusView(mp.Runtime.OAuthStatus(server)))

	case "signout":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := mp.Runtime.SignOutOAuth(server); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// 登出后重连：token 没了，条目应当回到"未授权"而不是继续显示已连接。
		go func() {
			if err := mp.Runtime.ReconnectEntry(server); err != nil {
				log.Infof("[AgentPlugin] %s/%s disconnected after OAuth sign-out", name, server)
				return
			}
		}()
		jsonResponse(w, map[string]any{
			"ok":     true,
			"status": toOAuthStatusView(mcp.OAuthFlowStatus{State: "none"}),
		})

	default:
		http.Error(w, "unknown oauth action", http.StatusNotFound)
	}
}

// reconnectPluginEntryAfterOAuth 在授权成功后重连插件的单个条目。
//
// 由 mcp.Manager 的 OnOAuthAuthorized 回调触发（token 已落盘）：重连成功会
// 把新工具注册进注册表，用户在浏览器点完"允许"回到会话就能直接调用。
func (s *Server) reconnectPluginEntryAfterOAuth(pluginName, serverName string) {
	s.acquireServerMu("")
	mp := s.agentPlugins[pluginName]
	s.releaseServerMu()
	if mp == nil || mp.Runtime == nil {
		return
	}
	if err := mp.Runtime.ReconnectEntry(serverName); err != nil {
		log.Warnf("[AgentPlugin] OAuth reconnect for %s/%s failed: %v", pluginName, serverName, err)
		return
	}
	log.Infof("[AgentPlugin] %s/%s connected after OAuth authorization", pluginName, serverName)
}

// oauthManagerForPlugins 返回共享的 OAuth 管理器（未配置时 nil）。
func (s *Server) oauthManagerForPlugins() *mcp.OAuthManager {
	if s.mcpMgr == nil {
		return nil
	}
	return s.mcpMgr.OAuth()
}
