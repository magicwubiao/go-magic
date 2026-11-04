package server

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/internal/cliconn"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// required_env 表达的是"清单里声明了待填位置"（cli.json 里留空的 env），
// 凭据保存后摘要里的 required_env 必须把已填的 key 剪掉——否则 UI 会在
// 凭据已经生效的情况下继续喊"缺少凭据：GH_TOKEN"（实测反馈，2026-11-04）。
func TestDecorateSummariesPrunesFilledRequiredEnv(t *testing.T) {
	home := t.TempDir()
	conn := cliconn.Connector{
		Command: "gh",
		Env:     []string{"GH_TOKEN="},
		Tools:   []cliconn.ToolSpec{{Name: "pr_list", Description: "d", Args: []string{"pr", "list"}}},
	}
	plugin := &agentplugin.Plugin{
		Manifest:      &agentplugin.Manifest{Name: "cli-gh"},
		CLIConnectors: []agentplugin.CLIConnectorStatus{{Name: "gh", Connector: conn}},
	}
	plugins := map[string]*agentplugin.ManagedPlugin{"cli-gh": {Plugin: plugin}}

	s := &Server{
		cfg:          &appconfig.Config{},
		magicHome:    home,
		agentPlugins: plugins,
		pluginCreds:  newPluginCredentialStore(home),
	}
	if err := s.setPluginCredentials("cli-gh", map[string]string{"GH_TOKEN": "github_pat_test"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	cliEntry := func() map[string]any {
		t.Helper()
		list := agentplugin.Summary(plugins)
		s.decoratePluginSummaries(list, plugins)
		entries, _ := list[0]["cli_connectors"].([]map[string]any)
		if len(entries) != 1 {
			t.Fatalf("cli_connectors = %v", list[0]["cli_connectors"])
		}
		return entries[0]
	}

	// 已填凭据：required_env 剪空，不再亮"缺少凭据"。
	if req := cliEntry()["required_env"].([]string); len(req) != 0 {
		t.Fatalf("filled credential should prune required_env, got %v", req)
	}

	// 清掉凭据后恢复提示（空值 = 删除）。
	if err := s.setPluginCredentials("cli-gh", map[string]string{"GH_TOKEN": ""}); err != nil {
		t.Fatalf("clear credentials: %v", err)
	}
	if req := cliEntry()["required_env"].([]string); len(req) != 1 || req[0] != "GH_TOKEN" {
		t.Fatalf("cleared credential should restore required_env, got %v", req)
	}
}

// 凭据表单里写小写键（gh_token）与清单里的 GH_TOKEN 是同一个键：
// 剪枝与 blocker 判定都必须大小写不敏感（与 withCLICredentials 同口径）。
func TestRequiredEnvCaseInsensitiveCredentialMatch(t *testing.T) {
	if !filledCredKeys(map[string]string{"gh_token": "x"})["GH_TOKEN"] {
		t.Fatal("filledCredKeys should normalize keys to upper case")
	}
	got := pruneFilledRequiredEnv([]string{"GH_TOKEN"}, filledCredKeys(map[string]string{"gh_token": "x"}))
	if len(got) != 0 {
		t.Fatalf("lower-case credential should prune GH_TOKEN, got %v", got)
	}
}
