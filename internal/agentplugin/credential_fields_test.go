package agentplugin

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
)

// TestCredentialFieldsFallbackForHandwrittenPlugin：手写插件没有目录元数据，
// 至少要保证每个待填 key 都出现在表单里（不能因为查不到 label 就漏项）。
func TestCredentialFieldsFallbackForHandwrittenPlugin(t *testing.T) {
	p := &Plugin{
		Manifest: &Manifest{Name: "demo"},
		MCPServers: []MCPEntryStatus{{
			Name: "svc",
			Spec: MCPServerSpec{Type: TransportStdio, Command: "x", Env: map[string]string{"TOKEN": ""}},
		}},
		CLIConnectors: []CLIConnectorStatus{{
			Name:      "gh",
			Connector: cliConnectorWithEnv("gh", "GH_TOKEN="),
		}},
	}

	fields := CredentialFields(p)
	mcp := fields["svc"]
	if len(mcp) != 1 || mcp[0].Key != "TOKEN" {
		t.Fatalf("mcp fields = %+v", mcp)
	}
	cli := fields[CLICredentialGroup("gh")]
	if len(cli) != 1 || cli[0].Key != "GH_TOKEN" {
		t.Fatalf("cli fields = %+v", cli)
	}
	// 目录没登记就没有获取地址：前端会退回纯输入框。
	if urls := CredentialHelpURLs(mcp); len(urls) != 0 {
		t.Fatalf("unexpected help urls: %v", urls)
	}
}

// TestCredentialFieldsFromCatalogMCP：目录装出来的插件必须带上 label 与
// "去哪里拿 token"的直链——这正是用户最容易卡住的一步。
func TestCredentialFieldsFromCatalogMCP(t *testing.T) {
	p := catalogPlugin(CatalogKindMCP, "github", &Plugin{
		Manifest: &Manifest{Name: "mcp-github"},
		MCPServers: []MCPEntryStatus{{
			Name: "github",
			Spec: MCPServerSpec{
				Type:    TransportStdio,
				Command: "npx",
				Env:     map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": ""},
			},
		}},
	})

	fields := CredentialFields(p)["github"]
	if len(fields) != 1 {
		t.Fatalf("fields = %+v", fields)
	}
	f := fields[0]
	if f.Label != "Personal access token" {
		t.Fatalf("label = %q", f.Label)
	}
	if f.HelpURL != "https://github.com/settings/tokens" {
		t.Fatalf("help url = %q", f.HelpURL)
	}
	if !f.Secret {
		t.Fatal("token field should be marked secret")
	}
	if urls := CredentialHelpURLs(fields); len(urls) != 1 {
		t.Fatalf("help urls = %v", urls)
	}
}

// TestCredentialFieldsFromCatalogCLI：CLI 目录的凭据同样带获取地址。
func TestCredentialFieldsFromCatalogCLI(t *testing.T) {
	p := catalogPlugin(CatalogKindCLI, "supabase", &Plugin{
		Manifest:      &Manifest{Name: "cli-supabase"},
		CLIConnectors: []CLIConnectorStatus{{Name: "supabase", Connector: cliConnectorWithEnv("supabase", "SUPABASE_ACCESS_TOKEN=")}},
	})

	fields := CredentialFields(p)[CLICredentialGroup("supabase")]
	if len(fields) != 1 || fields[0].HelpURL == "" {
		t.Fatalf("fields = %+v", fields)
	}
}

// TestCatalogPacksExposeCredentialFields：目录卡片在安装前就能看到"去获取"，
// 而不是装完才发现要填什么。
func TestCatalogPacksExposeCredentialFields(t *testing.T) {
	var github, supabase *CatalogPack
	for _, packs := range [][]CatalogPack{mcpCatalogPacks(), cliCatalogPacks()} {
		for i := range packs {
			switch {
			case packs[i].Kind == CatalogKindMCP && packs[i].ID == "github":
				github = &packs[i]
			case packs[i].Kind == CatalogKindCLI && packs[i].ID == "supabase":
				supabase = &packs[i]
			}
		}
	}
	if github == nil || supabase == nil {
		t.Fatalf("catalog entries missing (github=%v supabase=%v)", github, supabase)
	}
	for _, tc := range []struct {
		name string
		pack *CatalogPack
	}{{"github", github}, {"supabase", supabase}} {
		if len(tc.pack.CredentialFields) == 0 {
			t.Fatalf("%s: no credential fields", tc.name)
		}
		if urls := CredentialHelpURLs(tc.pack.CredentialFields); len(urls) == 0 {
			t.Fatalf("%s: credential fields carry no help url: %+v", tc.name, tc.pack.CredentialFields)
		}
	}
}

// cliConnectorWithEnv 造一个只关心 env 占位的最小连接器。
func cliConnectorWithEnv(command string, env ...string) cliconn.Connector {
	return cliconn.Connector{Command: command, Env: env}
}

// catalogPlugin 给插件打上"来自哪个目录条目"的标记（与物化时写入的扩展一致）。
func catalogPlugin(kind, id string, p *Plugin) *Plugin {
	p.Extensions = map[string]any{
		catalogPackExtKey: map[string]any{"kind": kind, "id": id},
	}
	return p
}
