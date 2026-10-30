package server

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/clicatalog"
)

// 目录列表要能直接驱动 UI：每条都得有标题、工具、当前平台的安装命令。
func TestCLICatalogListEndpoint(t *testing.T) {
	s, _ := newCLITestServer(t)

	rec := doCLIRequest(t, s, http.MethodGet, "/api/cli/catalog", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []struct {
			ID              string   `json:"id"`
			Name            string   `json:"name"`
			DefaultName     string   `json:"default_name"`
			Installed       bool     `json:"installed"`
			InstallCommands []string `json:"install_commands"`
			Connector       struct {
				Command string `json:"command"`
				Tools   []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"connector"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(resp.Entries) < 4 {
		t.Fatalf("catalog returned %d entries, want at least 4", len(resp.Entries))
	}
	for _, e := range resp.Entries {
		if e.ID == "" || e.Name == "" || e.DefaultName == "" {
			t.Fatalf("entry missing identity: %+v", e)
		}
		if len(e.InstallCommands) == 0 {
			t.Fatalf("%s: no install commands for this platform", e.ID)
		}
		if len(e.Connector.Tools) == 0 {
			t.Fatalf("%s: no tools", e.ID)
		}
	}
}

func TestCLICatalogInstallValidation(t *testing.T) {
	s, _ := newCLITestServer(t)

	if rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/catalog/install", `{"id": "nope"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// 连接器名不合法要在落盘前拦住（名字同时是工具前缀）。
	if rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/catalog/install", `{"id": "git", "name": "bad name!"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if s.cfg.Cli != nil {
		t.Fatal("failed install must not persist anything")
	}
}

// 明文凭据防线：gh 条目声明了可选的 GH_TOKEN。提交 ghp_ 开头的明文要被拦下
// （config.json 会长期落盘），改用 ${VAR} 引用或显式 allow_plaintext 才放行。
func TestCLICatalogInstallRejectsPlaintextSecret(t *testing.T) {
	s, home := newCLITestServer(t)

	body := `{"id": "gh", "env": {"GH_TOKEN": "ghp_0123456789abcdefghijklmnopqrstuvwxyz"}}`
	rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/catalog/install", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("plaintext token: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "plaintext credential") {
		t.Fatalf("error should explain the plaintext policy, body = %s", rec.Body.String())
	}
	if s.cfg.Cli != nil {
		t.Fatal("rejected install must not persist anything")
	}

	// ${VAR} 引用不是明文，直接放行（gh 没装时会走到"命令不存在"，那是另一条错误）。
	// overwrite=true：这台机器可能已经装过 gh，别让 409 盖住要验证的结论。
	rec = doCLIRequest(t, s, http.MethodPost, "/api/cli/catalog/install",
		`{"id": "gh", "env": {"GH_TOKEN": "${GH_TOKEN}"}, "overwrite": true}`)
	if rec.Code == http.StatusBadRequest && strings.Contains(rec.Body.String(), "plaintext") {
		t.Fatalf("env reference must pass the plaintext check, body = %s", rec.Body.String())
	}

	// 显式 allow_plaintext 是逃生口：不再被明文检查拦（命令不存在时是别的报错）。
	body = `{"id": "gh", "env": {"GH_TOKEN": "ghp_0123456789abcdefghijklmnopqrstuvwxyz"}, "allow_plaintext": true, "overwrite": true}`
	rec = doCLIRequest(t, s, http.MethodPost, "/api/cli/catalog/install", body)
	if rec.Code == http.StatusBadRequest && strings.Contains(rec.Body.String(), "plaintext") {
		t.Fatalf("allow_plaintext must bypass the check, body = %s", rec.Body.String())
	}

	// gh 已安装且放行明文时，配置里就是明文（用户被明确告知过后果）。
	// gh 没装时，请求应在命令检测处停下，报错必须是"找不到命令"而不是明文检查。
	if _, err := exec.LookPath("gh"); err == nil {
		if rec.Code != http.StatusOK {
			t.Fatalf("allow_plaintext install: status = %d, body = %s", rec.Code, rec.Body.String())
		}
		cfg := readPersistedConfig(t, home)
		if cfg.Cli == nil {
			t.Fatal("cli section was not persisted")
		}
		conn, ok := cfg.Cli.Connectors["gh"]
		if !ok {
			t.Fatalf("connector %q missing from persisted config", "gh")
		}
		want := "GH_TOKEN=ghp_0123456789abcdefghijklmnopqrstuvwxyz"
		found := false
		for _, kv := range conn.Env {
			if kv == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("plaintext env %q not persisted, env = %v", want, conn.Env)
		}
	} else if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not found") {
		t.Fatalf("gh missing: expect a not-found error, got status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// 成功路径：挑一个本机确实存在的命令（git），装完要落盘并注册工具。
func TestCLICatalogInstallRegistersTools(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed on this machine")
	}
	s, home := newCLITestServer(t)

	rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/catalog/install", `{"id": "git"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var view struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	var entry clicatalog.Entry
	for _, e := range clicatalog.All() {
		if e.ID == "git" {
			entry = e
		}
	}
	if view.Name != entry.DefaultName {
		t.Fatalf("installed name = %q, want %q", view.Name, entry.DefaultName)
	}

	// 工具已经能被模型调到（注册名是 cli_<connector>_<tool>）。
	for _, tool := range entry.Connector.Tools {
		want := "cli_" + entry.DefaultName + "_" + tool.Name
		if !s.toolReg.HasTool(want) {
			t.Fatalf("tool %s was not registered", want)
		}
	}

	// 落盘的配置里必须有这条连接器（加完重启就没了是最容易被当成 bug 的行为）。
	cfg := readPersistedConfig(t, home)
	if cfg.Cli == nil {
		t.Fatal("cli section was not persisted")
	}
	if _, ok := cfg.Cli.Connectors[entry.DefaultName]; !ok {
		t.Fatalf("connector %q missing from persisted config", entry.DefaultName)
	}

	// 重复安装要 409，避免把用户改过的配置冲掉。
	if rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/catalog/install", `{"id": "git"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate install: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
