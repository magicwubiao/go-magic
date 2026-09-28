package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/tool"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// ghostCommand 是一个确定起不来的命令：cmd.Start() 会在 PATH 查找阶段就失败，
// 于是测试可以用它驱动"连接失败"分支，而不需要真的拉起一个 MCP 子进程。
const ghostCommand = "definitely-not-a-real-mcp-binary-xyz"

// newMCPTestServer 造一个只带 MCP 相关依赖的 Server，并把 magic home 指向
// 每个测试各自的临时目录 —— 本包的 TestMain 只给了一个共享目录，而
// persistMCPServers 会整份重写 config.json，写在共享目录里会污染别的用例。
func newMCPTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)
	return &Server{
		cfg:       &appconfig.Config{},
		magicHome: home,
		mcpMgr:    mcp.NewManager(),
		toolReg:   tool.NewRegistry(),
	}, home
}

func doMCPRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	if strings.HasPrefix(path, "/api/mcp/servers/") {
		s.handleMCPServerByID(rec, req)
	} else {
		s.handleMCPServers(rec, req)
	}
	return rec
}

// readPersistedConfig 读回落盘的 config.json —— 断言"配置真的写下去了"，
// 而不是只看内存里的 s.cfg。
func readPersistedConfig(t *testing.T, home string) appconfig.Config {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatalf("config.json not written: %v", err)
	}
	var cfg appconfig.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config.json is not valid JSON: %v", err)
	}
	return cfg
}

// JSON 导入：多个服务器一次写入，且连接失败的条目也必须留在配置里
// （用户才能在列表里看到自己刚加的东西并改正）。
func TestMCPAddFromJSONPersistsEvenWhenConnectFails(t *testing.T) {
	s, home := newMCPTestServer(t)

	body := `{"mcpServers": {
		"ghost": {"command": "` + ghostCommand + `", "args": ["-y", "pkg"], "env": {"TOKEN": "s3cret"}},
		"ghost2": {"type": "stdio", "command": "` + ghostCommand + `2"}
	}}`
	rec := doMCPRequest(t, s, http.MethodPost, "/api/mcp/servers", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool     `json:"success"`
		Added   []string `json:"added"`
		Failed  []struct {
			Name  string `json:"name"`
			Error string `json:"error"`
		} `json:"failed"`
		Servers []mcpServerView `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if resp.Success {
		t.Error("success should be false when every server failed to connect")
	}
	if len(resp.Added) != 0 || len(resp.Failed) != 2 {
		t.Fatalf("added = %v, failed = %v; want 0 added and 2 failed", resp.Added, resp.Failed)
	}
	for _, f := range resp.Failed {
		if !strings.Contains(f.Error, "MCP server") {
			t.Errorf("failure %q should explain the connect error, got %q", f.Name, f.Error)
		}
	}

	persisted := readPersistedConfig(t, home)
	if persisted.MCP == nil || len(persisted.MCP.Servers) != 2 {
		t.Fatalf("servers were not persisted: %+v", persisted.MCP)
	}
	ghost := persisted.MCP.Servers["ghost"]
	if ghost.Command != ghostCommand || ghost.Transport != mcp.TransportStdio {
		t.Errorf("ghost = %+v, want the stdio config that was posted", ghost)
	}
	if !reflect.DeepEqual(ghost.Args, []string{"-y", "pkg"}) {
		t.Errorf("ghost.Args = %v", ghost.Args)
	}
	if !reflect.DeepEqual(ghost.Env, []string{"TOKEN=s3cret"}) {
		t.Errorf("ghost.Env = %v, want the env to survive as K=V", ghost.Env)
	}
}

// 单个服务器连接失败 → 非 2xx（前端据此弹错误），且不能用 5xx：request() 对
// 5xx 会自动重试三次，而每次重试都会再 spawn 一个 MCP 子进程。
func TestMCPAddSingleFailureReturnsBadRequest(t *testing.T) {
	s, _ := newMCPTestServer(t)

	rec := doMCPRequest(t, s, http.MethodPost, "/api/mcp/servers",
		`{"mcpServers": {"ghost": {"command": "`+ghostCommand+`"}}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (5xx would be retried by the dashboard), body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "MCP server") {
		t.Errorf("body should carry the connect error, got %s", rec.Body.String())
	}
}

func TestMCPAddRejectsBadJSON(t *testing.T) {
	s, _ := newMCPTestServer(t)

	cases := []struct{ name, body string }{
		{"非法 JSON", `{"mcpServers": `},
		{"缺 name", `{"command": "npx", "type": "stdio"}`},
		{"不支持的传输方式", `{"mcpServers": {"x": {"command": "npx", "type": "websocket"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doMCPRequest(t, s, http.MethodPost, "/api/mcp/servers", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "error") {
				t.Errorf("body = %s, want a JSON error payload", rec.Body.String())
			}
		})
	}
}

// PUT /api/mcp/servers/{name} 以前完全没实现（405），而 Web 页面的
// "添加/保存"按钮走的正是它 —— 这条用例锁住的是"表单能提交"。
func TestMCPUpsertAcceptsFormBodyWithoutName(t *testing.T) {
	s, home := newMCPTestServer(t)

	rec := doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/filesystem",
		`{"command": "`+ghostCommand+`", "args": ["-y", "pkg"], "transport": "stdio"}`)

	// 连接必然失败（命令不存在），但配置要落盘、错误要带上原因。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	persisted := readPersistedConfig(t, home)
	if got := persisted.MCP.Servers["filesystem"]; got.Command != ghostCommand {
		t.Fatalf("filesystem config = %+v, want the posted command", got)
	}
}

func TestMCPUpsertRejectsNameMismatch(t *testing.T) {
	s, _ := newMCPTestServer(t)

	rec := doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/filesystem",
		`{"name": "other", "command": "npx"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "does not match the URL") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// env 的值永远只以 *** 出现在接口里；表单原样回传时不能把真值冲掉。
func TestMCPEnvMaskedInAPIAndRestoredOnSave(t *testing.T) {
	s, home := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"fs": {Command: "npx", Transport: mcp.TransportStdio, Env: []string{"TOKEN=real-secret", "REGION=cn"}},
	}}

	rec := doMCPRequest(t, s, http.MethodGet, "/api/mcp/servers/fs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var view mcpServerView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if view.Env["TOKEN"] != maskedEnvValue || view.Env["REGION"] != maskedEnvValue {
		t.Fatalf("env leaked through the API: %v", view.Env)
	}
	if !view.Configured || view.Connected {
		t.Errorf("view = %+v, want configured-but-not-connected", view)
	}
	if view.Transport != mcp.TransportStdio {
		t.Errorf("transport = %q, want stdio", view.Transport)
	}

	// 表单回传掩码值 + 一个新变量：掩码要还原成磁盘上的真值。
	rec = doMCPRequest(t, s, http.MethodPut, "/api/mcp/servers/fs",
		`{"command": "`+ghostCommand+`", "env": {"TOKEN": "***", "NEW": "1"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	persisted := readPersistedConfig(t, home)
	// 对象形态的 env 在解析阶段就按 key 排序，所以顺序是 NEW 在前；
	// 关键断言是 TOKEN 拿回了磁盘上的真值，而不是被 *** 覆盖。
	if got := persisted.MCP.Servers["fs"].Env; !reflect.DeepEqual(got, []string{"NEW=1", "TOKEN=real-secret"}) {
		t.Fatalf("env = %v, want the masked value restored and the new one added", got)
	}
}

func TestMCPDeleteRemovesFromConfig(t *testing.T) {
	s, home := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"fs": {Command: "npx", Transport: mcp.TransportStdio},
	}}

	rec := doMCPRequest(t, s, http.MethodDelete, "/api/mcp/servers/fs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := readPersistedConfig(t, home).MCP.Servers; len(got) != 0 {
		t.Errorf("server still persisted after delete: %v", got)
	}

	// 再删一次：既没连过也没配置过 → 404，而不是静默成功。
	rec = doMCPRequest(t, s, http.MethodDelete, "/api/mcp/servers/fs", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}
}

// 列表要把"配置过但没连上"的服务器也列出来，否则用户看不到自己刚加的东西。
func TestListMCPServersMergesConfiguredAndLive(t *testing.T) {
	s, _ := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"zeta":  {URL: "http://localhost:9/sse", Transport: mcp.TransportSSE},
		"alpha": {Command: "npx", Transport: mcp.TransportStdio, Env: []string{"KEY=v"}},
	}}

	rec := doMCPRequest(t, s, http.MethodGet, "/api/mcp/servers", "")
	var list []mcpServerView
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d servers, want 2: %+v", len(list), list)
	}
	if list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Errorf("list is not sorted by name: %s, %s", list[0].Name, list[1].Name)
	}
	for _, view := range list {
		if !view.Configured || view.Connected {
			t.Errorf("%s: configured = %v, connected = %v; want true/false", view.Name, view.Configured, view.Connected)
		}
	}
	if list[0].Env["KEY"] != maskedEnvValue {
		t.Errorf("env not masked in the list: %v", list[0].Env)
	}
	// 没有 transport 字段时按 command/url 推断，前端要拿它渲染标签。
	if list[1].Transport != mcp.TransportSSE {
		t.Errorf("zeta transport = %q, want sse", list[1].Transport)
	}
}

func TestMergeMaskedServerEnv(t *testing.T) {
	stored := []string{"TOKEN=secret", "REGION=cn"}

	got := mergeMaskedServerEnv(stored, []string{"TOKEN=***", "REGION=us", "NEW=1"})
	want := []string{"TOKEN=secret", "REGION=us", "NEW=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeMaskedServerEnv = %v, want %v", got, want)
	}

	// 空输入 = 清空 env（表单里删光所有行）
	if got := mergeMaskedServerEnv(stored, nil); got != nil {
		t.Errorf("empty incoming = %v, want nil", got)
	}
	// 未知 key 的掩码无法还原，保持原样而不是编一个值
	if got := mergeMaskedServerEnv(nil, []string{"X=***"}); !reflect.DeepEqual(got, []string{"X=***"}) {
		t.Errorf("unresolvable mask = %v", got)
	}
	// 垃圾行（没有 =）被跳过，不写进配置
	if got := mergeMaskedServerEnv(stored, []string{"JUNK", "A=1"}); !reflect.DeepEqual(got, []string{"A=1"}) {
		t.Errorf("junk handling = %v", got)
	}
}

func TestMCPListOmitsZeroHealthCheckTime(t *testing.T) {
	s, _ := newMCPTestServer(t)
	s.cfg.MCP = &appconfig.MCPConfig{Servers: map[string]mcp.ServerConfig{
		"fs": {Command: "npx", Transport: mcp.TransportStdio},
	}}

	// 未做过健康检查时不能吐 0001-01-01T00:00:00Z（前端会把它当成真实时间渲染）
	if body := doMCPRequest(t, s, http.MethodGet, "/api/mcp/servers", "").Body.String(); strings.Contains(body, "0001-01-01") {
		t.Errorf("zero health-check time leaked into the API: %s", body)
	}
}
