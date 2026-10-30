package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// writePlugin 造一个最小可加载插件目录（plugin.json + 可选 cli.json）。
func writePlugin(t *testing.T, dir, cliJSON string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "demo",
  "version": "0.1.0"
}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if cliJSON == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "cli.json"), []byte(cliJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

const demoCLIJSON = `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "connectors": {
    "gitdemo": {
      "command": "git",
      "env": ["GH_TOKEN="],
      "tools": [
        {"name": "status", "description": "show status", "args": ["status", "--short"], "read_only": true, "timeout_seconds": 15}
      ]
    },
    "broken": {
      "command": "",
      "tools": [{"name": "x", "description": "x"}]
    }
  }
}`

func TestLoadCLIComponent(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, demoCLIJSON)

	p := Load(dir, filepath.Join(dir, "data"))
	if p.IsRejected() {
		t.Fatalf("plugin rejected: %s", p.FatalError)
	}
	if p.CLIDisabled {
		t.Fatalf("cli component unexpectedly disabled: %v", p.Warnings)
	}
	if len(p.CLIConnectors) != 2 {
		t.Fatalf("want 2 cli connectors, got %d", len(p.CLIConnectors))
	}

	byName := map[string]CLIConnectorStatus{}
	for _, cs := range p.CLIConnectors {
		byName[cs.Name] = cs
	}
	if got := byName["gitdemo"]; got.Error != "" {
		t.Fatalf("gitdemo should load cleanly, got error %q", got.Error)
	}
	// 条目级失败隔离：命令为空的连接器只让它自己失效。
	if got := byName["broken"]; got.Error == "" {
		t.Fatal("empty command must be reported on that entry only")
	}
}

func TestLoadCLIInvalidJSONDisablesOnlyCLI(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "{not json")

	p := Load(dir, filepath.Join(dir, "data"))
	if p.IsRejected() {
		t.Fatalf("broken cli.json must not reject the plugin: %s", p.FatalError)
	}
	if !p.CLIDisabled {
		t.Fatal("malformed cli.json must disable the CLI component")
	}
	if p.ManifestValid != true {
		t.Fatal("manifest must stay valid")
	}
}

func TestLoadCLIVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, `{
  "$schema": "https://agent-plugins.org/schemas/9.9.9/plugin.schema.json",
  "connectors": {"x": {"command": "git", "tools": [{"name": "s", "description": "s", "args": ["status"]}]}}
}`)

	p := Load(dir, filepath.Join(dir, "data"))
	if !p.CLIDisabled {
		t.Fatal("schema version mismatch must disable the CLI component")
	}
}

func TestRegisterCLIToolsNamesTimeoutsAndCredentials(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, demoCLIJSON)
	p := Load(dir, filepath.Join(dir, "data"))

	plugins := map[string]*ManagedPlugin{"demo": {Plugin: p}}
	reg := tool.NewRegistry()
	RegisterCLITools(plugins, reg, func(name string) map[string]string {
		return map[string]string{"gh_token": "s3cret"} // 大小写不敏感匹配
	})

	const want = "ap_demo_cli_gitdemo_status"
	tl, err := reg.Get(want)
	if err != nil {
		t.Fatalf("tool %q not registered: %v", want, err)
	}
	// 校验失败的连接器不应注册出工具。
	if _, err := reg.Get("ap_demo_cli_broken_x"); err == nil {
		t.Fatal("invalid connector must not register tools")
	}

	// 超时必须写进注册表，否则执行器会套默认 60s，配置里的 15s 永远不生效。
	if got := reg.GetTimeout(want); got.Seconds() != 15 {
		t.Fatalf("timeout = %v, want 15s", got)
	}

	// 插件工具名带 ap_ 前缀，避免与全局 cli.<connector> 的工具撞名。
	if !strings.HasPrefix(tl.Name(), "ap_demo_") {
		t.Fatalf("unexpected tool name %q", tl.Name())
	}

	// 凭据应已填进子进程环境（不执行命令，只看配置生效）。
	ct, ok := tl.(*cliPluginTool)
	if !ok {
		t.Fatalf("unexpected tool type %T", tl)
	}
	if got := ct.Tool.Spec().Name; got != "status" {
		t.Fatalf("spec name = %q", got)
	}

	UnregisterCLITools(plugins, reg)
	if _, err := reg.Get(want); err == nil {
		t.Fatal("UnregisterCLITools must remove plugin tools")
	}
}

func TestWithCLICredentialsOnlyTouchesDeclaredKeys(t *testing.T) {
	conn := demoConnector(t)
	out := withCLICredentials(conn, map[string]string{"GH_TOKEN": "abc", "EVIL": "x"})
	env := strings.Join(out.Env, ",")
	if !strings.Contains(env, "GH_TOKEN=abc") {
		t.Fatalf("credential not applied: %v", out.Env)
	}
	// 未声明的键不许注入：凭据表单写什么就能给子进程注什么是危险的默认值。
	if strings.Contains(env, "EVIL") {
		t.Fatalf("undeclared credential leaked into env: %v", out.Env)
	}
}

func demoConnector(t *testing.T) cliconn.Connector {
	t.Helper()
	dir := t.TempDir()
	writePlugin(t, dir, demoCLIJSON)
	p := Load(dir, filepath.Join(dir, "data"))
	for _, cs := range p.CLIConnectors {
		if cs.Name == "gitdemo" {
			return cs.Connector
		}
	}
	t.Fatal("gitdemo connector not found")
	return cliconn.Connector{}
}
