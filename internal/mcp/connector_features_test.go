package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// 本文件锁定连接器产品层的四个关键行为（2026-10-27）：
//   1. 远程连接器的自定义请求头必须真的发出去（认证全靠它）；
//   2. 通知类请求收到 202/204 是成功，不是错误（否则标准 server 握手就失败）；
//   3. HTTP 状态码要映射成可行动的错误分类；
//   4. 工具级屏蔽与审批判定（destructiveHint / require_approval）。

func TestSSETransportSendsCustomHeaders(t *testing.T) {
	var mu sync.Mutex
	var gotAuth, gotAPIKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Api-Key")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	tr, err := NewSSETransportWithOptions(srv.URL, map[string]string{
		"Authorization": "Bearer secret-token",
		"X-Api-Key":     "k-123",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("NewSSETransportWithOptions: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tr.Send(ctx, &JSONRPCRequest{JSONRPC: JSONRPCVersion, Method: "ping", ID: 1}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("Authorization header not sent: %q", gotAuth)
	}
	if gotAPIKey != "k-123" {
		t.Fatalf("X-Api-Key header not sent: %q", gotAPIKey)
	}
}

func TestSSETransportAccepts202ForNotifications(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 标准 Streamable HTTP 服务端对 notifications/* 回 202 且无 body。
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	tr, _ := NewSSETransport(srv.URL)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tr.Send(ctx, &JSONRPCRequest{JSONRPC: JSONRPCVersion, Method: "notifications/initialized"}); err != nil {
		t.Fatalf("202 Accepted must be treated as success, got error: %v", err)
	}
}

func TestHTTPStatusErrorKinds(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorKind
	}{
		{http.StatusUnauthorized, ErrKindAuth},
		{http.StatusForbidden, ErrKindAuth},
		{http.StatusNotFound, ErrKindNotFound},
		{http.StatusNotAcceptable, ErrKindProtocol},
		{http.StatusInternalServerError, ErrKindServer},
		{http.StatusBadGateway, ErrKindServer},
	}
	for _, c := range cases {
		err := httpStatusError(c.status, "application/json", "")
		if got := KindOf(err); got != c.want {
			t.Errorf("status %d → kind %q, want %q (err=%v)", c.status, got, c.want, err)
		}
	}
}

func TestKindOfFallsBackToMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want ErrorKind
	}{
		{"HTTP 401 Unauthorized", ErrKindAuth},
		{"connection refused (is the server running?)", ErrKindNetwork},
		{"Unsupported protocol version: 2026-07-28", ErrKindProtocol},
		{"HTTP 503: upstream unavailable", ErrKindServer},
		{"something odd happened", ErrKindUnknown},
	}
	for _, c := range cases {
		if got := KindOf(errString2(c.msg)); got != c.want {
			t.Errorf("KindOf(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

func TestManagerDisabledToolFilter(t *testing.T) {
	m := NewManager()
	m.SetDisabledTools(map[string][]string{
		"big": {"dangerous_write", " "},
	})

	if !m.IsToolDisabled("big", "dangerous_write") {
		t.Error("dangerous_write should be disabled")
	}
	if m.IsToolDisabled("big", "read_file") {
		t.Error("read_file should stay enabled")
	}
	if m.IsToolDisabled("other", "dangerous_write") {
		t.Error("disabled list must be scoped per server")
	}

	// 空表 = 全部放行
	m.SetDisabledTools(nil)
	if m.IsToolDisabled("big", "dangerous_write") {
		t.Error("clearing the list must re-enable every tool")
	}
}

func TestMCPToolRequiresApproval(t *testing.T) {
	mgr := NewManager()

	tool := func(a *ToolAnnotations) *MCPTool {
		return NewMCPTool("srv", Tool{
			Name:        "t",
			Description: "d",
			InputSchema: map[string]interface{}{"type": "object"},
			Annotations: a,
		}, mgr)
	}

	if tool(nil).RequiresApproval() {
		t.Error("no annotations → no approval")
	}
	if !tool(&ToolAnnotations{DestructiveHint: true}).RequiresApproval() {
		t.Error("destructiveHint must require approval")
	}
	if tool(&ToolAnnotations{ReadOnlyHint: true}).RequiresApproval() {
		t.Error("readOnlyHint must not require approval")
	}
	if !tool(&ToolAnnotations{OpenWorldHint: true}).RequiresApproval() {
		t.Error("openWorldHint (not read-only) must require approval")
	}
	if tool(&ToolAnnotations{OpenWorldHint: true, ReadOnlyHint: true}).RequiresApproval() {
		t.Error("openWorld + readOnly is a query; no approval needed")
	}
}

func TestServerConfigProfileAndAutoConnect(t *testing.T) {
	off := false
	if (ServerConfig{}).AutoConnect() != true {
		t.Error("zero value must auto-connect (backwards compatible with old configs)")
	}
	if (ServerConfig{Enabled: &off}).AutoConnect() {
		t.Error("enabled:false must not auto-connect")
	}

	if !(ServerConfig{}).AppliesToProfile("work") {
		t.Error("empty Profiles means every profile")
	}
	if (ServerConfig{Profiles: []string{"home"}}).AppliesToProfile("work") {
		t.Error("connector scoped to home must not apply to work")
	}
	if !(ServerConfig{Profiles: []string{"Home"}}).AppliesToProfile("home") {
		t.Error("profile matching must be case-insensitive")
	}
}

func TestParseServersJSONHeaders(t *testing.T) {
	raw := `{"mcpServers": {"remote": {
		"type": "http",
		"url": "https://example.com/mcp",
		"headers": {"Authorization": "Bearer x", "X-Api-Key": "k"},
		"disabled_tools": ["a", "b"],
		"profiles": ["work"],
		"require_approval": true
	}}}`
	got, err := ParseServersJSON([]byte(raw))
	if err != nil {
		t.Fatalf("ParseServersJSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d servers", len(got))
	}
	cfg := got[0].Config
	if cfg.Headers["Authorization"] != "Bearer x" || cfg.Headers["X-Api-Key"] != "k" {
		t.Errorf("headers not parsed: %+v", cfg.Headers)
	}
	if len(cfg.DisabledTools) != 2 || cfg.DisabledTools[0] != "a" {
		t.Errorf("disabled_tools not parsed: %+v", cfg.DisabledTools)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0] != "work" {
		t.Errorf("profiles not parsed: %+v", cfg.Profiles)
	}
	if !cfg.RequireApproval {
		t.Error("require_approval not parsed")
	}
	if cfg.Transport != TransportHTTP {
		t.Errorf("transport = %q, want http", cfg.Transport)
	}
}

// TestSessionDeletedOnClose 验证有状态 server 的会话被主动回收：
// 不删的话服务端会一直挂着没人用的会话。
func TestSessionDeletedOnClose(t *testing.T) {
	var deleted string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deleted = r.Header.Get("Mcp-Session-Id")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-xyz")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	tr, _ := NewSSETransport(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tr.Send(ctx, &JSONRPCRequest{JSONRPC: JSONRPCVersion, Method: "initialize", ID: 1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	_ = tr.Close()

	mu.Lock()
	defer mu.Unlock()
	if deleted != "sess-xyz" {
		t.Fatalf("session was not deleted on Close (got %q)", deleted)
	}
}
