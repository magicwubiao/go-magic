package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/tool"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// CLI 连接器接口契约：
//  1. 保存落盘（config.json 的 cli.connectors），重启后还在；
//  2. env 只以 *** 返回，回传的 *** 在保存时还原成磁盘真值；
//  3. 校验类错误同步 4xx，不落盘；
//  4. 增删改后注册表里的 cli_* 工具立即跟着变。

// TestCLIHelperProcess 是子进程入口（试跑接口用），不是测试。
func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("CLI_HELPER") != "1" {
		return
	}
	// CLI_ECHO_ENV 指定要回显的环境变量名，用来验证"表单回传的 *** 被还原成真值"。
	if key := os.Getenv("CLI_ECHO_ENV"); key != "" {
		fmt.Print(os.Getenv(key))
		os.Exit(0)
	}
	fmt.Print("hello from helper")
	os.Exit(0)
}

func newCLITestServer(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)
	return &Server{
		cfg:       &appconfig.Config{},
		magicHome: home,
		toolReg:   tool.NewRegistry(),
		cliMgr:    nil,
	}, home
}

func doCLIRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader = strings.NewReader(body)
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()

	switch {
	case path == "/api/cli/connectors":
		s.handleCLIConnectors(rec, req)
	case path == "/api/cli/test":
		s.handleCLITest(rec, req)
	case path == "/api/cli/parse":
		s.handleCLIParse(rec, req)
	case strings.HasPrefix(path, "/api/cli/connectors/"):
		s.handleCLIConnectorByID(rec, req)
	default:
		t.Fatalf("unexpected path %s", path)
	}
	return rec
}

func ghConnectorBody() string {
	return `{
		"command": "gh",
		"description": "GitHub CLI",
		"env": ["GH_TOKEN=real-token"],
		"tools": [{
			"name": "pr_list",
			"description": "List pull requests",
			"args": ["pr", "list", "--limit", "{{limit}}"],
			"params": [{"name": "limit", "type": "integer"}],
			"read_only": true
		}]
	}`
}

func TestCLIConnectorUpsertPersistsAndRegisters(t *testing.T) {
	s, home := newCLITestServer(t)

	rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/gh", ghConnectorBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !s.toolReg.HasTool("cli_gh_pr_list") {
		t.Fatal("tool was not registered")
	}

	persisted := readPersistedConfig(t, home)
	if persisted.Cli == nil || persisted.Cli.Connectors["gh"].Command != "gh" {
		t.Fatalf("config not persisted: %#v", persisted.Cli)
	}
}

func TestCLIConnectorEnvMaskedAndRestored(t *testing.T) {
	s, _ := newCLITestServer(t)
	if rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/gh", ghConnectorBody()); rec.Code != http.StatusOK {
		t.Fatalf("seed failed: %s", rec.Body.String())
	}

	rec := doCLIRequest(t, s, http.MethodGet, "/api/cli/connectors/gh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var view cliConnectorView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(view.Env) != 1 || view.Env[0] != "GH_TOKEN="+maskedEnvValue {
		t.Fatalf("env leaked: %v", view.Env)
	}
	if len(view.ToolNames) != 1 || view.ToolNames[0] != "cli_gh_pr_list" {
		t.Fatalf("tool_names = %v", view.ToolNames)
	}

	// 表单原样回传（*** 未改）→ 磁盘上的真值必须还在
	body := strings.Replace(ghConnectorBody(), "real-token", maskedEnvValue, 1)
	if rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/gh", body); rec.Code != http.StatusOK {
		t.Fatalf("update failed: %s", rec.Body.String())
	}
	stored := s.cliConnectorConfigs()["gh"]
	if len(stored.Env) != 1 || stored.Env[0] != "GH_TOKEN=real-token" {
		t.Fatalf("masked env was written back verbatim: %v", stored.Env)
	}
}

func TestCLIConnectorValidationRejected(t *testing.T) {
	s, home := newCLITestServer(t)

	cases := []string{
		`{"command": "", "tools": []}`,                                                          // 缺 command
		`{"command": "gh", "tools": []}`,                                                        // 没有工具
		`{"command": "gh", "tools": [{"name": "x", "description": "d"}]}`,                       // 没有 args
		`{"command": "gh", "tools": [{"name": "x", "description": "d", "args": ["{{nope}}"]}]}`, // 未声明参数
	}
	for _, body := range cases {
		rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/gh", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rec.Code)
		}
	}

	// 校验失败不该落盘：config.json 此时根本不应被创建
	if _, err := os.Stat(filepath.Join(home, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid config must not be persisted (stat err = %v)", err)
	}

	rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/-bad", ghConnectorBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid connector name: status = %d", rec.Code)
	}
}

func TestCLIConnectorDeleteRemovesTools(t *testing.T) {
	s, home := newCLITestServer(t)
	if rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/gh", ghConnectorBody()); rec.Code != http.StatusOK {
		t.Fatalf("seed failed: %s", rec.Body.String())
	}

	rec := doCLIRequest(t, s, http.MethodDelete, "/api/cli/connectors/gh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if s.toolReg.HasTool("cli_gh_pr_list") {
		t.Fatal("tools of a deleted connector must be unregistered")
	}
	if cfg := readPersistedConfig(t, home); cfg.Cli != nil && len(cfg.Cli.Connectors) > 0 {
		t.Fatalf("connector still persisted: %#v", cfg.Cli.Connectors)
	}

	if rec := doCLIRequest(t, s, http.MethodDelete, "/api/cli/connectors/gh", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: status = %d, want 404", rec.Code)
	}
}

func TestCLIConnectorListSortedAndActive(t *testing.T) {
	s, _ := newCLITestServer(t)
	for _, name := range []string{"zeta", "alpha"} {
		body := strings.Replace(ghConnectorBody(), `"command": "gh"`, `"command": "gh"`, 1)
		if rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/"+name, body); rec.Code != http.StatusOK {
			t.Fatalf("seed %s failed: %s", name, rec.Body.String())
		}
	}
	rec := doCLIRequest(t, s, http.MethodGet, "/api/cli/connectors", "")
	var views []cliConnectorView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(views) != 2 || views[0].Name != "alpha" || views[1].Name != "zeta" {
		t.Fatalf("views = %+v", views)
	}
	if !views[0].Active {
		t.Fatal("connector without profiles/enabled=false should be active")
	}
}

// 快速导入：命令行 → 工具草稿。无状态，不落盘。
func TestCLIParseEndpoint(t *testing.T) {
	s, _ := newCLITestServer(t)

	rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/parse",
		`{"input": "gh pr list --repo owner/repo --limit 20 --json"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Command string `json:"command"`
		Tool    struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
		} `json:"tool"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if resp.Command != "gh" || resp.Tool.Name != "pr_list" {
		t.Fatalf("got command=%q tool=%q", resp.Command, resp.Tool.Name)
	}
	if len(resp.Tool.Args) == 0 || resp.Tool.Args[0] != "pr" {
		t.Fatalf("args = %v", resp.Tool.Args)
	}

	// 非法输入要 4xx，且不能落任何配置。
	if rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/parse", `{"input": "   "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty input: status = %d", rec.Code)
	}
	if s.cfg.Cli != nil {
		t.Fatal("parse must not persist anything")
	}
}

func TestCLITestEndpointRunsCommand(t *testing.T) {
	s, _ := newCLITestServer(t)

	body := fmt.Sprintf(`{
		"connector": {"command": %q, "args": ["-test.run=TestCLIHelperProcess"], "env": ["CLI_HELPER=1"],
			"tools": [{"name": "hi", "description": "d", "args": ["x"]}]},
		"tool": "hi"
	}`, os.Args[0])

	rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/test", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK     bool                   `json:"ok"`
		Result map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if !resp.OK {
		t.Fatalf("test run failed: %s", rec.Body.String())
	}
	if out, _ := resp.Result["stdout"].(string); !strings.Contains(out, "hello from helper") {
		t.Fatalf("stdout = %q", out)
	}
	if s.cfg.Cli != nil {
		t.Fatal("dry run must not persist anything")
	}
}

// 试跑必须用磁盘上的真值跑，而不是表单里回填的 ***。
// 否则"保存后点试跑"对任何带 token 的连接器都是 401，用户会以为权限配错了。
func TestCLITestEndpointRestoresMaskedEnv(t *testing.T) {
	s, _ := newCLITestServer(t)
	if rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/gh", ghConnectorBody()); rec.Code != http.StatusOK {
		t.Fatalf("seed failed: %s", rec.Body.String())
	}

	body := fmt.Sprintf(`{
		"name": "gh",
		"connector": {"command": %q,
			"args": ["-test.run=TestCLIHelperProcess"],
			"env": ["CLI_HELPER=1", "CLI_ECHO_ENV=GH_TOKEN", "GH_TOKEN=%s"],
			"tools": [{"name": "hi", "description": "d", "args": ["x"]}]},
		"tool": "hi"
	}`, os.Args[0], maskedEnvValue)

	rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/test", body)
	var resp struct {
		OK     bool                   `json:"ok"`
		Result map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v (body=%s)", err, rec.Body.String())
	}
	if out, _ := resp.Result["stdout"].(string); out != "real-token" {
		t.Fatalf("stdout = %q, want the stored token (masked value must not be executed)", out)
	}
}

// 新建（未保存）时没有可还原的值，*** 只能按字面量跑——不能因此 panic 或 500。
func TestCLITestEndpointWithoutStoredConnector(t *testing.T) {
	s, _ := newCLITestServer(t)
	body := fmt.Sprintf(`{
		"connector": {"command": %q, "args": ["-test.run=TestCLIHelperProcess"],
			"env": ["CLI_HELPER=1"],
			"tools": [{"name": "hi", "description": "d", "args": ["x"]}]},
		"tool": "hi"
	}`, os.Args[0])
	rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/test", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
