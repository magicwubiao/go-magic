package server

// OAuth 2.1 的服务端契约测试：
//   1. client_secret 在 API 视图里必须是 ***，绝不能明文外泄；
//   2. 表单回传 *** 时要还原磁盘真值（与 env/headers 同一约定）；
//   3. oauth/status / oauth/start / oauth/signout 三个端点在未配置
//      token store / 未授权时的兜底行为。

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

func TestMCPOAuthViewMasksSecret(t *testing.T) {
	s, _ := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"cloud": {
			Transport: mcp.TransportHTTP,
			URL:       "https://example.com/mcp",
			OAuth:     &mcp.OAuthConfig{ClientID: "cid-1", ClientSecret: "s3cret", Scopes: []string{"mcp:tools"}},
		},
	}}

	rec := doMCPRequest(t, s, http.MethodGet, "/api/mcp/servers/cloud", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var view mcpServerView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if view.OAuth == nil {
		t.Fatal("oauth view is missing")
	}
	if !view.OAuth.Enabled || view.OAuth.ClientID != "cid-1" {
		t.Errorf("oauth view = %+v", view.OAuth)
	}
	// 密钥必须掩码；状态在没有任何 token store 时退化为 none。
	if view.OAuth.ClientSecret != maskedEnvValue {
		t.Errorf("client_secret leaked: %q", view.OAuth.ClientSecret)
	}
	if view.OAuth.HasToken {
		t.Error("HasToken should be false before authorization")
	}
	if view.OAuth.State != "none" {
		t.Errorf("state = %q, want none", view.OAuth.State)
	}
}

func TestMCPOAuthSecretRestoredOnSave(t *testing.T) {
	s, home := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"cloud": {
			Transport: mcp.TransportHTTP,
			URL:       "https://example.com/mcp",
			OAuth:     &mcp.OAuthConfig{ClientID: "cid-1", ClientSecret: "old-s3cret"},
		},
	}}

	// 回传 *** → 保留磁盘真值；scopes 更新要生效。
	rec := doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/cloud",
		`{"transport":"http","url":"https://example.com/mcp",
		  "oauth":{"client_id":"cid-1","client_secret":"***","scopes":["mcp:tools"]}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	persisted := readPersistedConfig(t, home)
	got := persisted.MCP.Servers["cloud"].OAuth
	want := &mcp.OAuthConfig{ClientID: "cid-1", ClientSecret: "old-s3cret", Scopes: []string{"mcp:tools"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("oauth after save = %+v, want %+v", got, want)
	}

	// 用户真的换了密钥 → 覆盖。
	rec = doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/cloud",
		`{"transport":"http","url":"https://example.com/mcp",
		  "oauth":{"client_id":"cid-1","client_secret":"new-s3cret"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	persisted = readPersistedConfig(t, home)
	if got := persisted.MCP.Servers["cloud"].OAuth.ClientSecret; got != "new-s3cret" {
		t.Fatalf("client secret not replaced: %q", got)
	}

	// oauth 字段整个去掉 → 配置被清除（用户关闭 OAuth）。
	rec = doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/cloud",
		`{"transport":"http","url":"https://example.com/mcp"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	persisted = readPersistedConfig(t, home)
	if persisted.MCP.Servers["cloud"].OAuth != nil {
		t.Fatalf("oauth should be removed, got %+v", persisted.MCP.Servers["cloud"].OAuth)
	}
}

func TestMCPOAuthEndpointsWithoutStore(t *testing.T) {
	s, _ := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"cloud": {
			Transport: mcp.TransportHTTP,
			URL:       "https://example.com/mcp",
			OAuth:     &mcp.OAuthConfig{},
		},
	}}

	// status：没有 token store 也要能回答（none），不能 500。
	rec := doMCPRequest(t, s, http.MethodGet, "/api/mcp/servers/cloud/oauth/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status endpoint = %d, body = %s", rec.Code, rec.Body.String())
	}
	var st mcp.OAuthFlowStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if st.State != "none" {
		t.Errorf("state = %q, want none", st.State)
	}

	// start：没有 token store 是服务端装配问题，500 + 明确信息。
	rec = doMCPRequest(t, s, http.MethodPost, "/api/mcp/servers/cloud/oauth/start", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("start without store = %d, want 500", rec.Code)
	}

	// 未配置的 server → 404。
	rec = doMCPRequest(t, s, http.MethodPost, "/api/mcp/servers/ghost/oauth/start", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("start for unknown server = %d, want 404", rec.Code)
	}
}
