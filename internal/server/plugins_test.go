package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/tool"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

func newPluginTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)
	return &Server{
		cfg:       &appconfig.Config{},
		magicHome: home,
		toolReg:   tool.NewRegistry(),
	}, home
}

func writeTestPlugin(t *testing.T, home, name, body string) {
	t.Helper()
	dir := filepath.Join(home, "plugins", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 列表要在没有任何安装记录时也能工作，并且把 conflicts / missing_env 算出来。
func TestHandlePluginsListsDiscovered(t *testing.T) {
	s, home := newPluginTestServer(t)
	t.Setenv("PLUGIN_TEST_TOKEN_UNSET", "")
	writeTestPlugin(t, home, "demo", `{
		"name": "demo",
		"version": "1.0.0",
		"connectors": {"git": {"command": "git", "tools": [
			{"name": "status", "description": "status", "args": ["status"], "read_only": true}
		]}},
		"requires_env": ["PLUGIN_TEST_TOKEN_UNSET"]
	}`)

	rec := httptest.NewRecorder()
	s.handlePlugins(rec, httptest.NewRequest(http.MethodGet, "/api/plugins", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Dir     string       `json:"dir"`
		Plugins []pluginView `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Plugins) != 1 || resp.Plugins[0].Name != "demo" {
		t.Fatalf("unexpected plugins: %+v", resp.Plugins)
	}
	if resp.Plugins[0].Installed {
		t.Fatal("plugin without a record must not report installed")
	}
	if len(resp.Plugins[0].MissingEnv) != 1 {
		t.Fatalf("missing_env should list the unset variable, got %v", resp.Plugins[0].MissingEnv)
	}
	if !strings.HasSuffix(resp.Dir, "plugins") {
		t.Fatalf("unexpected dir %q", resp.Dir)
	}
}

// 未知插件名必须在写配置之前就失败：安装是"要么整套生效、要么什么都没变"。
func TestHandlePluginInstallRejectsUnknown(t *testing.T) {
	s, _ := newPluginTestServer(t)

	rec := httptest.NewRecorder()
	body := `{"name":"nope"}`
	s.handlePluginInstall(rec, httptest.NewRequest(http.MethodPost, "/api/plugins/install", strings.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.magicHome, "config.json")); err == nil {
		t.Fatal("config.json must not be created when the manifest is unknown")
	}
}

// 清单里有未设置的 requires_env 时也必须拒绝，并点名变量。
func TestHandlePluginInstallRequiresEnv(t *testing.T) {
	s, _ := newPluginTestServer(t)
	writeTestPlugin(t, s.magicHome, "needs-token", `{
		"name": "needs-token",
		"connectors": {"git": {"command": "git", "tools": [
			{"name": "status", "description": "status", "args": ["status"], "read_only": true}
		]}},
		"requires_env": ["PLUGIN_TEST_MISSING_TOKEN"]
	}`)

	rec := httptest.NewRecorder()
	body := `{"name":"needs-token"}`
	s.handlePluginInstall(rec, httptest.NewRequest(http.MethodPost, "/api/plugins/install", strings.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PLUGIN_TEST_MISSING_TOKEN") {
		t.Fatalf("error must name the variable, got %s", rec.Body.String())
	}
}
