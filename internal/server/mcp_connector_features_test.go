package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 本文件覆盖 2026-10-27 连接器完善后的接口契约：
//   1. 自定义请求头与 env 同一套掩码/还原语义；
//   2. 单个工具的启用/屏蔽会落盘，并且立刻反映到 Manager 的可见性判定；
//   3. 连接器目录可搜索、按分类过滤；
//   4. 试连不存在的连接器返回 404，而不是 500。

func TestMCPHeadersMaskedAndRestored(t *testing.T) {
	s, home := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"remote": {
			Transport: mcp.TransportHTTP,
			URL:       "https://example.com/mcp",
			Headers:   map[string]string{"Authorization": "Bearer real-token", "X-Api-Key": "k-1"},
		},
	}}

	rec := doMCPRequest(t, s, http.MethodGet, "/api/mcp/servers/remote", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var view mcpServerView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if view.Headers["Authorization"] != maskedEnvValue || view.Headers["X-Api-Key"] != maskedEnvValue {
		t.Fatalf("headers leaked through the API: %v", view.Headers)
	}
	if view.State != mcp.StateConfigured {
		t.Errorf("state = %q, want configured", view.State)
	}

	// 表单原样回传 *** + 新增一个头：掩码要还原，新增的要保留。
	rec = doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/remote",
		`{"transport": "http", "url": "https://example.com/mcp",
		  "headers": {"Authorization": "***", "X-Trace": "on"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	persisted := readPersistedConfig(t, home)
	got := persisted.MCP.Servers["remote"].Headers
	want := map[string]string{"Authorization": "Bearer real-token", "X-Trace": "on"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("headers = %v, want %v", got, want)
	}
}

func TestMCPToolTogglePersistsAndUpdatesVisibility(t *testing.T) {
	s, home := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"big": {Command: "npx", Transport: mcp.TransportStdio},
	}}

	rec := doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/big/tools/dangerous_write",
		`{"enabled": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	persisted := readPersistedConfig(t, home)
	if got := persisted.MCP.Servers["big"].DisabledTools; !reflect.DeepEqual(got, []string{"dangerous_write"}) {
		t.Fatalf("disabled_tools = %v, want [dangerous_write]", got)
	}
	if !s.mcpMgr.IsToolDisabled("big", "dangerous_write") {
		t.Error("manager visibility not refreshed after the toggle")
	}
	if s.mcpMgr.IsToolDisabled("big", "read_file") {
		t.Error("toggling one tool must not affect the others")
	}

	// 再打开：从屏蔽列表里移除。
	rec = doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/big/tools/dangerous_write",
		`{"enabled": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := readPersistedConfig(t, home).MCP.Servers["big"].DisabledTools; len(got) != 0 {
		t.Fatalf("disabled_tools = %v, want empty", got)
	}
	if s.mcpMgr.IsToolDisabled("big", "dangerous_write") {
		t.Error("re-enabled tool still reported as disabled")
	}
}

func TestMCPToolToggleValidatesBody(t *testing.T) {
	s, _ := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"big": {Command: "npx", Transport: mcp.TransportStdio},
	}}

	rec := doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/big/tools/x", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing enabled → status %d, want 400", rec.Code)
	}

	rec = doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/nope/tools/x", `{"enabled": false}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown server → status %d, want 404", rec.Code)
	}
}

func TestMCPTestEndpointUnknownServer(t *testing.T) {
	s, _ := newMCPTestServer(t)
	rec := doMCPRequest(t, s, http.MethodPost, "/api/mcp/servers/ghost/test", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMCPCatalogEndpoint(t *testing.T) {
	s, _ := newMCPTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/catalog", nil)
	rec := httptest.NewRecorder()
	s.handleMCPCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Connectors []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Category string `json:"category"`
			Requires []struct {
				Key   string `json:"key"`
				Where string `json:"where"`
			} `json:"requires"`
		} `json:"connectors"`
		Categories []struct {
			ID string `json:"id"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(body.Connectors) == 0 {
		t.Fatal("catalog is empty")
	}
	if len(body.Categories) == 0 {
		t.Error("categories are empty")
	}

	// 搜索命中的是 git 相关条目，且必须带凭据说明（否则装完也连不上）。
	req = httptest.NewRequest(http.MethodGet, "/api/mcp/catalog?q=github", nil)
	rec = httptest.NewRecorder()
	s.handleMCPCatalog(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	// 搜索同时匹配描述，所以 github 可能和提到 GitHub 的条目一起返回；
	// 关键是 github 本身必须命中，且带着"要填什么凭据"的说明。
	var github *struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Category string `json:"category"`
		Requires []struct {
			Key   string `json:"key"`
			Where string `json:"where"`
		} `json:"requires"`
	}
	for i := range body.Connectors {
		if body.Connectors[i].ID == "github" {
			github = &body.Connectors[i]
			break
		}
	}
	if github == nil {
		t.Fatalf("search result = %+v, want the github connector", body.Connectors)
	}
	if len(github.Requires) == 0 || github.Requires[0].Where != "env" {
		t.Errorf("github requirements = %+v", github.Requires)
	}
}
