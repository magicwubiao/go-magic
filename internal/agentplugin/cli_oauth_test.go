package agentplugin

import (
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/tool"
)

const demoCLIOAuthJSON = `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "connectors": {
    "gh": {
      "command": "git",
      "oauth": {
        "device_authorization_endpoint": "https://github.com/login/device/code",
        "token_endpoint": "https://github.com/login/oauth/access_token",
        "client_id": "cid",
        "token_env": "GH_TOKEN"
      },
      "tools": [{"name": "status", "description": "show status", "args": ["status"], "read_only": true}]
    },
    "plain": {
      "command": "git",
      "tools": [{"name": "log", "description": "show log", "args": ["log", "-1"], "read_only": true}]
    }
  }
}`

type stubTokenSource struct{ tok string }

func (s stubTokenSource) EnsureAccessToken() (string, error) { return s.tok, nil }

// 键必须能区分"同一个连接器名在不同插件里"——否则 A 插件的账号会覆盖 B 插件。
func TestCLIOAuthKeyRoundTrip(t *testing.T) {
	key := CLIOAuthKey("demo", "gh")
	if key != "cliconn:agentplugin/demo/gh" {
		t.Fatalf("key = %q", key)
	}
	plugin, connector, ok := ParseCLIOAuthKey(key)
	if !ok || plugin != "demo" || connector != "gh" {
		t.Fatalf("parse = (%q, %q, %v)", plugin, connector, ok)
	}
	// 全局连接器键与 MCP 条目键都不该被误认为插件 CLI 键。
	for _, other := range []string{"cliconn:gh", "agentplugin:demo/svc", "cliconn:agentplugin/demo", "cliconn:agentplugin//gh"} {
		if _, _, ok := ParseCLIOAuthKey(other); ok {
			t.Fatalf("%q must not parse as a plugin CLI key", other)
		}
	}
}

func TestRegisterCLIToolsWithOAuthAttachesSource(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, demoCLIOAuthJSON)
	p := Load(dir, filepath.Join(dir, "data"))
	if p.CLIDisabled {
		t.Fatalf("cli component disabled: %v", p.Warnings)
	}
	plugins := map[string]*ManagedPlugin{"demo": {Plugin: p}}
	reg := tool.NewRegistry()

	var calls [][2]string
	RegisterCLIToolsWithOAuth(plugins, reg, nil, func(plugin, connector string) cliconn.TokenSource {
		calls = append(calls, [2]string{plugin, connector})
		return stubTokenSource{tok: "tok-" + connector}
	})

	// 只为声明了 oauth 的连接器建源：普通连接器不该被无谓地套一层。
	if len(calls) != 1 || calls[0] != [2]string{"demo", "gh"} {
		t.Fatalf("factory calls = %v, want exactly demo/gh", calls)
	}
	// 带 oauth 的连接器仍然要注册出工具（token 是执行期才取的）。
	if _, err := reg.Get("ap_demo_cli_gh_status"); err != nil {
		t.Fatalf("oauth connector tool missing: %v", err)
	}
	if _, err := reg.Get("ap_demo_cli_plain_log"); err != nil {
		t.Fatalf("plain connector tool missing: %v", err)
	}

	// 插件定义本身绝不能被挂上运行时字段：OAuth 是共享指针，写进去会让
	// 后续重注册读到上一轮的源（甚至测试里的 stub），也会被序列化出去。
	for _, cs := range p.CLIConnectors {
		if cs.Connector.OAuth != nil && cs.Connector.OAuth.Tokens != nil {
			t.Fatalf("connector %q mutated: Tokens leaked into the plugin definition", cs.Name)
		}
	}
}

// 没有工厂时行为必须与改造前完全一致（老调用方不受影响）。
func TestRegisterCLIToolsWithoutFactoryLeavesTokensNil(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, demoCLIOAuthJSON)
	p := Load(dir, filepath.Join(dir, "data"))
	plugins := map[string]*ManagedPlugin{"demo": {Plugin: p}}
	reg := tool.NewRegistry()

	RegisterCLITools(plugins, reg, nil)
	if _, err := reg.Get("ap_demo_cli_gh_status"); err != nil {
		t.Fatalf("tool missing: %v", err)
	}
	for _, cs := range p.CLIConnectors {
		if cs.Connector.OAuth != nil && cs.Connector.OAuth.Tokens != nil {
			t.Fatal("RegisterCLITools must not attach a token source")
		}
	}
}

// 挂源必须发生在副本上：直接写 Tokens 会改到插件共享的 OAuthSpec。
func TestWithCLITokenSourceCopiesSpec(t *testing.T) {
	conn := cliconn.Connector{
		Command: "gh",
		OAuth:   &cliconn.OAuthSpec{ClientID: "cid", TokenEnv: "GH_TOKEN"},
	}
	out := withCLITokenSource(conn, "demo", "gh", func(string, string) cliconn.TokenSource {
		return stubTokenSource{tok: "t"}
	})
	if out.OAuth == conn.OAuth {
		t.Fatal("OAuth spec was shared instead of copied")
	}
	if out.OAuth.Tokens == nil {
		t.Fatal("token source not attached")
	}
	if conn.OAuth.Tokens != nil {
		t.Fatal("original spec was mutated")
	}

	// 没有工厂 / 没声明 oauth：原样返回，不制造空壳 spec。
	if got := withCLITokenSource(conn, "demo", "gh", nil); got.OAuth != conn.OAuth {
		t.Fatal("nil factory must return the connector untouched")
	}
}
