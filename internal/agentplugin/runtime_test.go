package agentplugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// fakePluginServer 模拟一台 Streamable HTTP MCP server：
// initialize 回一个可用协议版本，tools/list 返回指定工具，
// tools/call 把 arguments.text 原样回显。
//
// 用它而不是子进程：测试二进制在部分环境（Windows AV/文件锁）下会被拒绝
// fork/exec，而 Runtime 现在走的是 mcp.Manager 的远程传输，HTTP 分支
// 与真实 server 走的是同一条代码路径。
func fakePluginServer(t *testing.T, toolNames ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mcp.JSONRPCRequest
		_ = json.Unmarshal(body, &req)

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]any{"name": "fake", "version": "1.0"},
			}
		case "tools/list":
			tools := make([]map[string]any, 0, len(toolNames))
			for _, n := range toolNames {
				tools = append(tools, map[string]any{
					"name":        n,
					"description": "fake " + n,
					"inputSchema": map[string]any{"type": "object"},
				})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			var p struct {
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			text, _ := p.Arguments["text"].(string)
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
			}
		default:
			result = map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
}

// newTestRuntime 构造一个只含远程条目的插件运行时。
func newTestRuntime(t *testing.T, url string, serverNames ...string) *Runtime {
	t.Helper()
	p := &Plugin{
		Root:     t.TempDir(),
		DataDir:  t.TempDir(),
		Manifest: &Manifest{Name: "demo"},
	}
	for _, name := range serverNames {
		p.MCPServers = append(p.MCPServers, MCPEntryStatus{
			Name: name,
			Spec: MCPServerSpec{Type: TransportStreamableHTTP, URL: url},
		})
	}
	return NewRuntime(p)
}

// fakeRegistrar 记录注册/注销的工具名与实际对象。
type fakeRegistrar struct {
	mu         sync.Mutex
	registered map[string]bool
	tools      map[string]tool.Tool
}

func newFakeRegistrar() *fakeRegistrar {
	return &fakeRegistrar{registered: map[string]bool{}, tools: map[string]tool.Tool{}}
}

func (f *fakeRegistrar) Register(t tool.Tool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered[t.Name()] = true
	f.tools[t.Name()] = t
}

func (f *fakeRegistrar) Unregister(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.registered, name)
	delete(f.tools, name)
}

func (f *fakeRegistrar) has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registered[name]
}

// TestRuntime_ConnectsThroughMCPManager 覆盖整条链路：
// 连上（走 mcp.Manager 的版本协商/工具发现）→ 注册工具 → 调用工具 → Stop 断开。
func TestRuntime_ConnectsThroughMCPManager(t *testing.T) {
	srv := fakePluginServer(t, "echo", "whoami")
	defer srv.Close()

	r := newTestRuntime(t, srv.URL, "srv")
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()

	st, ok := r.EntryStatus("srv")
	if !ok {
		t.Fatal("entry status missing")
	}
	if !st.Connected || st.ToolCount != 2 {
		t.Fatalf("entry status = %+v, want connected with 2 tools", st)
	}

	reg := newFakeRegistrar()
	r.RegisterTools(reg, "demo")
	name := PluginToolName("demo", "srv", "echo")
	if !reg.has(name) {
		t.Fatalf("tool %q not registered: %v", name, reg.registered)
	}
	// 第三方插件的 MCP 工具必须走审批：annotations 是作者自声明的，不是信任根。
	if aware, ok := reg.tools[name].(interface{ RequiresApproval() bool }); !ok || !aware.RequiresApproval() {
		t.Fatalf("plugin MCP tool %q must require approval", name)
	}
	if sum, ok := reg.tools[name].(interface{ ApprovalSummary() string }); !ok || sum.ApprovalSummary() == "" {
		t.Fatalf("plugin MCP tool %q must provide an approval summary", name)
	}

	out, err := r.CallTool(context.Background(), "srv", "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "hi" {
		t.Fatalf("CallTool = %v, want hi", out)
	}

	// Stop 之后条目不可再调用（连接已拆）。
	r.Stop()
	if _, err := r.CallTool(context.Background(), "srv", "echo", nil); err == nil {
		t.Fatal("CallTool should fail after Stop")
	}
}

// TestRuntime_ReconnectEntryOnlyTouchesItsOwnTools 回归：单条目重连不得影响
// 其它条目已注册的工具。
//
// 旧实现 ReconnectEntry 先 UnregisterTools（清空整个 runtime 的注册记录）
// 再全量重注册，任意一个条目重连都会让别的条目的工具在注册表里消失。
func TestRuntime_ReconnectEntryOnlyTouchesItsOwnTools(t *testing.T) {
	srv := fakePluginServer(t, "echo")
	defer srv.Close()

	r := newTestRuntime(t, srv.URL, "a", "b")
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()

	reg := newFakeRegistrar()
	r.RegisterTools(reg, "demo")
	aName := PluginToolName("demo", "a", "echo")
	bName := PluginToolName("demo", "b", "echo")
	if !reg.has(aName) || !reg.has(bName) {
		t.Fatalf("both entries should be registered: %v", reg.registered)
	}

	if err := r.ReconnectEntry("a"); err != nil {
		t.Fatalf("ReconnectEntry: %v", err)
	}
	if !reg.has(aName) {
		t.Fatalf("reconnected entry lost its tool: %v", reg.registered)
	}
	if !reg.has(bName) {
		t.Fatalf("other entry's tool was removed by reconnecting %q: %v", "a", reg.registered)
	}
}

// TestRuntime_MissingCredentialsIsAuthError 缺凭据要归到 auth 类，
// UI 才能亮"去授权"而不是让用户去查网络。
func TestRuntime_MissingCredentialsIsAuthError(t *testing.T) {
	p := &Plugin{
		Root:     t.TempDir(),
		Manifest: &Manifest{Name: "demo"},
		MCPServers: []MCPEntryStatus{{
			Name: "srv",
			Spec: MCPServerSpec{
				Type:    TransportStdio,
				Command: "definitely-not-a-real-binary",
				Cwd:     t.TempDir(),
				Env:     map[string]string{"API_KEY": ""},
			},
		}},
	}
	r := NewRuntime(p)
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()

	st, _ := r.EntryStatus("srv")
	if st.ErrorKind != string(mcp.ErrKindAuth) {
		t.Fatalf("error kind = %q, want auth (err=%q)", st.ErrorKind, st.Error)
	}
	if !strings.Contains(st.Error, "API_KEY") {
		t.Fatalf("error should name the missing credential: %q", st.Error)
	}
}

// TestApplyCredentials_IgnoresUndeclaredKeys 回归：凭据只能填进条目声明过的
// key，不能变成"往子进程环境里注入任意变量"（PATH / LD_PRELOAD / NODE_OPTIONS）。
func TestApplyCredentials_IgnoresUndeclaredKeys(t *testing.T) {
	spec := MCPServerSpec{
		Type:    TransportStdio,
		Command: "srv",
		Env: map[string]string{
			"API_KEY": "",
			"MODE":    "readonly",
		},
	}
	creds := map[string]string{
		"API_KEY":      "secret",
		"LD_PRELOAD":   "/tmp/evil.so",
		"NODE_OPTIONS": "--require /tmp/evil.js",
	}
	out, missing := applyCredentials(spec, creds)
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	if out.Env["API_KEY"] != "secret" {
		t.Fatalf("declared key not filled: %v", out.Env)
	}
	if _, ok := out.Env["LD_PRELOAD"]; ok {
		t.Fatalf("undeclared key must not be injected: %v", out.Env)
	}
	if _, ok := out.Env["NODE_OPTIONS"]; ok {
		t.Fatalf("undeclared key must not be injected: %v", out.Env)
	}
	if out.Env["MODE"] != "readonly" {
		t.Fatalf("declared literal must survive: %v", out.Env)
	}
}

// TestPluginToolName_NoSegmentCollision 回归：不同切分不得拼出同一个工具名，
// 且名字必须落在 provider 接受的字符集与长度内。
func TestPluginToolName_NoSegmentCollision(t *testing.T) {
	cases := [][3]string{
		{"demo", "a_b", "c"},
		{"demo", "a", "b_c"},
		{"demo", "srv-1", "read file"},
		{"demo", "srv 1", "read_file"},
		{"具名", "服务器", "工具"},
		{"demo", strings.Repeat("s", 40), strings.Repeat("t", 40)},
	}
	seen := map[string][3]string{}
	for _, c := range cases {
		name := PluginToolName(c[0], c[1], c[2])
		if len(name) > maxToolNameLen {
			t.Errorf("name %q exceeds %d chars", name, maxToolNameLen)
		}
		for _, r := range name {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '-' || r == '_'
			if !ok {
				t.Errorf("name %q contains illegal character %q", name, r)
				break
			}
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("collision: %v and %v both map to %q", prev, c, name)
		}
		seen[name] = c
	}
}

func TestValidateEntryName(t *testing.T) {
	for _, name := range []string{"main", "srv-1", "weather.api", "a_1"} {
		if err := validateEntryName(name); err != nil {
			t.Errorf("entry name %q should be valid: %v", name, err)
		}
	}
	for _, name := range []string{"", strings.Repeat("a", 65), "a b", "srv/1", "具名"} {
		if err := validateEntryName(name); err == nil {
			t.Errorf("entry name %q should be invalid", name)
		}
	}
}

// TestUninstall_RejectsUnsafeNames 回归:卸载只接受单级目录名,防路径穿越,
// 并且不能删掉安装/升级自己用的隐藏目录。
func TestUninstall_RejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"", "..", ".", "a/b", "a\\b", "../../etc", ".staging-abc", ".backup-abc"} {
		if _, err := Uninstall(name, false); err == nil {
			t.Errorf("Uninstall(%q) should be rejected", name)
		}
	}
}
