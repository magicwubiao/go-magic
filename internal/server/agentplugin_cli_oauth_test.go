package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 插件内 CLI 连接器的 OAuth 契约：
//   - 路由可达且 token 键带插件命名空间（不与全局连接器互相覆盖）；
//   - 未登录时摘要与 blocker 都明确说"要登录"，而不是报连接失败；
//   - 已登录（token 在 store 里）后同一路径变成 authorized / 无 blocker。
func TestPluginCLIOAuthRoutesAndSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)
	tokenPath := filepath.Join(home, "oauth_tokens.json")

	conn := cliconn.Connector{
		Command: "git",
		OAuth: &cliconn.OAuthSpec{
			DeviceAuthorizationEndpoint: "http://127.0.0.1:1/device",
			TokenEndpoint:               "http://127.0.0.1:1/token",
			ClientID:                    "cid",
			TokenEnv:                    "GH_TOKEN",
		},
		Tools: []cliconn.ToolSpec{{Name: "status", Description: "d", Args: []string{"status"}}},
	}
	plugin := &agentplugin.Plugin{
		Manifest:      &agentplugin.Manifest{Name: "demo"},
		CLIConnectors: []agentplugin.CLIConnectorStatus{{Name: "gh", Connector: conn}},
	}
	plugins := map[string]*agentplugin.ManagedPlugin{"demo": {Plugin: plugin}}

	mcpMgr := mcp.NewManager()
	if err := mcpMgr.SetTokenStore(tokenPath); err != nil {
		t.Fatalf("SetTokenStore: %v", err)
	}
	s := &Server{
		cfg:          &appconfig.Config{},
		magicHome:    home,
		mcpMgr:       mcpMgr,
		agentPlugins: plugins,
	}

	call := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleAgentPlugins(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	// 未登录：status 可达、configured=true、state=none。
	rec := call(http.MethodGet, "/api/agent-plugins/demo/cli/gh/oauth/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", rec.Code, rec.Body.String())
	}
	var view struct {
		Configured bool `json:"configured"`
		Status     struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if !view.Configured || view.Status.State != "none" {
		t.Fatalf("status view = %+v", view)
	}

	// 未知连接器必须 404（证明路径进到了处理器而不是被前缀分支挡掉）。
	if rec := call(http.MethodGet, "/api/agent-plugins/demo/cli/nope/oauth/status"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown connector code = %d, want 404", rec.Code)
	}

	// start：设备授权端点不可达 → 4xx/5xx 的明确错误，不能 500 或挂住。
	if rec := call(http.MethodPost, "/api/agent-plugins/demo/cli/gh/oauth/start"); rec.Code < http.StatusBadRequest {
		t.Fatalf("start with unreachable endpoint = %d body=%s", rec.Code, rec.Body.String())
	}

	// 摘要：CLI 条目要带 oauth 视图与"去登录"blocker。
	list := agentplugin.Summary(plugins)
	s.decoratePluginSummaries(list, plugins)
	item := list[0]
	entries, _ := item["cli_connectors"].([]map[string]any)
	if len(entries) != 1 {
		t.Fatalf("cli_connectors = %v", item["cli_connectors"])
	}
	if entries[0]["oauth_configured"] != true {
		t.Fatalf("oauth_configured = %v", entries[0]["oauth_configured"])
	}
	if entries[0]["oauth_token_env"] != "GH_TOKEN" {
		t.Fatalf("oauth_token_env = %v", entries[0]["oauth_token_env"])
	}
	if entries[0]["oauth_state"] != "none" {
		t.Fatalf("oauth_state = %v", entries[0]["oauth_state"])
	}

	// 命令可用时，未登录必须是一个 oauth blocker（scope=cli），供前端亮"去登录"。
	if agentplugin.CommandAvailable("git") {
		var sawLogin bool
		blockers, _ := item["blockers"].([]pluginBlocker)
		for _, b := range blockers {
			if b.Kind == "oauth" && b.Scope == "cli" && b.Name == "gh" {
				sawLogin = true
			}
		}
		if !sawLogin {
			t.Fatalf("blockers = %+v, want an oauth/cli blocker for gh", blockers)
		}
	}

	// 登录成功（token 落在插件命名空间）后：status=authorized，blocker 消失。
	store := mcp.NewOAuthTokenStore(tokenPath)
	if err := store.Save(agentplugin.CLIOAuthKey("demo", "gh"), mcp.OAuthToken{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save token: %v", err)
	}
	// 全局连接器的键不能读到这枚 token：命名空间隔离的最后一道验证。
	if _, ok := store.Get("cliconn:gh"); ok {
		t.Fatal("plugin token leaked into the global connector namespace")
	}

	// 工厂返回的源必须按插件键取 token —— 这是"执行前注入"链路的起点。
	// 取错键的话，工具会带着空 token 去跑，而 UI 却显示"已登录"。
	src := s.pluginCLITokenSource("demo", "gh")
	tok, err := src.EnsureAccessToken()
	if err != nil || tok != "tok" {
		t.Fatalf("token source = (%q, %v), want tok", tok, err)
	}

	rec = call(http.MethodGet, "/api/agent-plugins/demo/cli/gh/oauth/status")
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Status.State != "authorized" {
		t.Fatalf("state after login = %q, want authorized", view.Status.State)
	}

	list = agentplugin.Summary(plugins)
	s.decoratePluginSummaries(list, plugins)
	for _, b := range list[0]["blockers"].([]pluginBlocker) {
		if b.Kind == "oauth" {
			t.Fatalf("oauth blocker must disappear after login: %+v", b)
		}
	}

	// 登出：token 删除，状态回到 none。
	if rec := call(http.MethodPost, "/api/agent-plugins/demo/cli/gh/oauth/signout"); rec.Code != http.StatusOK {
		t.Fatalf("signout = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = call(http.MethodGet, "/api/agent-plugins/demo/cli/gh/oauth/status")
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Status.State == "authorized" {
		t.Fatal("token survived sign-out")
	}
}
