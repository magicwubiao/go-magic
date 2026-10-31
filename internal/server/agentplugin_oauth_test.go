package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/internal/mcp"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 插件条目的 OAuth 路由契约：
//   - {name}/mcp/{server}/oauth/status 能到达处理器（前缀路由曾把整类路径挡在门外）；
//   - 未授权时返回 none，不会 405；
//   - 没有授权服务器元数据时 start 返回 4xx（而不是 500 或挂住）。
func TestPluginOAuthRoutesAreReachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)

	// MCP 端点：对一切请求回 401，且没有 well-known 元数据。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	mcpMgr := mcp.NewManager()
	if err := mcpMgr.SetTokenStore(filepath.Join(home, "oauth_tokens.json")); err != nil {
		t.Fatalf("SetTokenStore: %v", err)
	}

	plugin := &agentplugin.Plugin{
		Manifest: &agentplugin.Manifest{Name: "demo"},
		MCPServers: []agentplugin.MCPEntryStatus{{
			Name: "svc",
			Spec: agentplugin.MCPServerSpec{
				Type:  agentplugin.TransportStreamableHTTP,
				URL:   srv.URL,
				OAuth: &mcp.OAuthConfig{},
			},
		}},
	}
	rt := agentplugin.NewRuntime(plugin)
	rt.SetOAuth(mcpMgr.OAuth())

	s := &Server{
		cfg:          &appconfig.Config{},
		magicHome:    home,
		mcpMgr:       mcpMgr,
		agentPlugins: map[string]*agentplugin.ManagedPlugin{"demo": {Plugin: plugin, Runtime: rt}},
	}

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleAgentPlugins(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	res := get("/api/agent-plugins/demo/mcp/svc/oauth/status")
	if res.Code != http.StatusOK {
		t.Fatalf("status route code = %d body=%s", res.Code, res.Body.String())
	}
	var view struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v (%s)", err, res.Body.String())
	}
	if view.State != "none" {
		t.Fatalf("state = %q, want none", view.State)
	}

	// 未知条目：404 而不是 405（证明进到了处理器）。
	if res := get("/api/agent-plugins/demo/mcp/nope/oauth/status"); res.Code != http.StatusNotFound {
		t.Fatalf("unknown entry code = %d, want 404", res.Code)
	}

	// connect 路由同样必须可达（此前被 case "mcp" 挡死，前端重试一直 405）。
	rec := httptest.NewRecorder()
	s.handleAgentPlugins(rec, httptest.NewRequest(http.MethodPost,
		"/api/agent-plugins/demo/mcp/svc/connect", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("connect route code = %d body=%s", rec.Code, rec.Body.String())
	}

	// start：本机没有授权服务器元数据 → 400 + 可读错误，不能是 500。
	rec = httptest.NewRecorder()
	s.handleAgentPlugins(rec, httptest.NewRequest(http.MethodPost,
		"/api/agent-plugins/demo/mcp/svc/oauth/start", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("start without AS code = %d body=%s", rec.Code, rec.Body.String())
	}
}
