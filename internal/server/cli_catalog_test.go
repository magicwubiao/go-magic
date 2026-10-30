package server

import (
	"encoding/json"
	"net/http"
	"os/exec"
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
