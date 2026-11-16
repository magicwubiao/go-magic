package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/acp"
)

// ACP 管理接口的落盘/掩码语义。
//
// 这里守的是三条最容易静默出错的约定：
//  1. env 值只能以 *** 回显（列表接口任何已登录浏览器都能读）；
//  2. 纯 ${VAR} 引用不掩码（掩掉会让用户以为配置被改写，且它本来就是"值从哪来"）；
//  3. 把 *** 原样提交回来时必须还原成磁盘真值，否则"只改超时"会把凭据写成字面量。

func newACPServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "acp.json")
	return &Server{acpMgr: acp.NewManager(path)}, path
}

func acpRequest(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	if strings.HasSuffix(target, "/agents") {
		s.handleACPAgents(rec, req)
	} else {
		s.handleACPAgentByID(rec, req)
	}
	return rec
}

func decodeACPList(t *testing.T, rec *httptest.ResponseRecorder) acpAgentList {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list acpAgentList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v (%s)", err, rec.Body.String())
	}
	return list
}

func TestACPAgentsMaskEnvAndPreserveReferences(t *testing.T) {
	s, path := newACPServer(t)

	body := `{
		"name": "claude",
		"command": "npx",
		"args": ["-y", "@zed-industries/claude-code-acp"],
		"env": ["ANTHROPIC_API_KEY=sk-ant-secret", "OTHER_TOKEN=${OTHER_TOKEN}"],
		"description": "coding agent",
		"auto_approve": ["read", "search"],
		"enabled": true
	}`
	if rec := acpRequest(t, s, http.MethodPost, "/api/acp/agents", body); rec.Code != http.StatusOK {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	list := decodeACPList(t, acpRequest(t, s, http.MethodGet, "/api/acp/agents", ""))
	if list.ConfigPath != path {
		t.Errorf("config_path = %q, want %q", list.ConfigPath, path)
	}
	if len(list.Agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(list.Agents))
	}
	agent := list.Agents[0]
	if !list.ToolsAvailable {
		t.Error("tools_available should be true with one enabled agent")
	}

	env := strings.Join(agent.Env, ",")
	if !strings.Contains(env, "ANTHROPIC_API_KEY=***") {
		t.Errorf("plaintext env must be masked, got %q", env)
	}
	if !strings.Contains(env, "OTHER_TOKEN=${OTHER_TOKEN}") {
		t.Errorf("pure ${VAR} reference must not be masked, got %q", env)
	}
}

func TestACPAgentUpdateKeepsMaskedEnv(t *testing.T) {
	s, path := newACPServer(t)

	create := `{"name":"claude","command":"npx","env":["ANTHROPIC_API_KEY=sk-ant-secret"],"enabled":true}`
	if rec := acpRequest(t, s, http.MethodPost, "/api/acp/agents", create); rec.Code != http.StatusOK {
		t.Fatalf("create: %s", rec.Body.String())
	}

	// 前端把列表原样回传：env 是 ***，只改 timeout。
	update := `{"name":"claude","command":"npx","env":["ANTHROPIC_API_KEY=***"],"timeout_seconds":900,"enabled":true}`
	if rec := acpRequest(t, s, http.MethodPut, "/api/acp/agents/claude", update); rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	cfg, err := acp.LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got := cfg.Agents["claude"]
	if len(got.Env) != 1 || got.Env[0] != "ANTHROPIC_API_KEY=sk-ant-secret" {
		t.Errorf("env = %v, want the original secret restored", got.Env)
	}
	if got.TimeoutSeconds != 900 {
		t.Errorf("timeout = %d, want 900", got.TimeoutSeconds)
	}
}

func TestACPAgentRenameAndDelete(t *testing.T) {
	s, path := newACPServer(t)

	create := `{"name":"old","command":"npx","enabled":true}`
	if rec := acpRequest(t, s, http.MethodPost, "/api/acp/agents", create); rec.Code != http.StatusOK {
		t.Fatalf("create: %s", rec.Body.String())
	}

	// 重名必须 409：静默覆盖会把别人的 agent 改没。
	if rec := acpRequest(t, s, http.MethodPost, "/api/acp/agents", create); rec.Code != http.StatusConflict {
		t.Errorf("duplicate create: status = %d, want 409", rec.Code)
	}

	rename := `{"name":"new","command":"npx","enabled":true}`
	if rec := acpRequest(t, s, http.MethodPut, "/api/acp/agents/old", rename); rec.Code != http.StatusOK {
		t.Fatalf("rename: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	cfg, err := acp.LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := cfg.Agents["old"]; ok {
		t.Error("old key must be removed after rename")
	}
	if _, ok := cfg.Agents["new"]; !ok {
		t.Error("new key must exist after rename")
	}

	if rec := acpRequest(t, s, http.MethodDelete, "/api/acp/agents/new", ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	list := decodeACPList(t, acpRequest(t, s, http.MethodGet, "/api/acp/agents", ""))
	if len(list.Agents) != 0 {
		t.Errorf("agents = %d after delete, want 0", len(list.Agents))
	}
	if list.ToolsAvailable {
		t.Error("tools_available should be false with no agents")
	}
}

func TestACPAgentValidationRejectsBadInput(t *testing.T) {
	s, _ := newACPServer(t)

	cases := []struct {
		name string
		body string
	}{
		{"missing command", `{"name":"x","enabled":true}`},
		{"bad name", `{"name":"../evil","command":"npx"}`},
		{"bad kind", `{"name":"x","command":"npx","auto_approve":["rm-rf"]}`},
		{"env without equals", `{"name":"x","command":"npx","env":["NOVALUE"]}`},
	}
	for _, tc := range cases {
		if rec := acpRequest(t, s, http.MethodPost, "/api/acp/agents", tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body = %s)", tc.name, rec.Code, rec.Body.String())
		}
	}
}
