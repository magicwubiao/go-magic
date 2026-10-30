package cliconnbridge

import (
	"context"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// stub 是一个最小工具，用来验证前缀清理不会误伤内置工具。
type stub struct{ name string }

func (s stub) Name() string                   { return s.name }
func (s stub) Description() string            { return "stub" }
func (s stub) Schema() map[string]interface{} { return map[string]interface{}{"type": "object"} }
func (s stub) Execute(context.Context, map[string]interface{}) (interface{}, error) {
	return nil, nil
}

func testConnector() cliconn.Connector {
	return cliconn.Connector{
		Command: "gh",
		Tools: []cliconn.ToolSpec{{
			Name:        "pr_list",
			Description: "list pull requests",
			Args:        []string{"pr", "list"},
		}},
	}
}

func TestSyncToRegistry(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(stub{name: "read_file"})
	reg.Register(stub{name: "cli_gone_old"}) // 上一次同步留下的僵尸工具

	mgr := cliconn.New(map[string]cliconn.Connector{"gh": testConnector()})
	if n := SyncToRegistry(mgr, reg, ""); n != 1 {
		t.Fatalf("registered %d tools, want 1", n)
	}
	if !reg.HasTool("cli_gh_pr_list") {
		t.Fatal("cli_gh_pr_list not registered")
	}
	if reg.HasTool("cli_gone_old") {
		t.Fatal("stale cli_ tool should be removed")
	}
	if !reg.HasTool("read_file") {
		t.Fatal("built-in tool must survive the sync")
	}

	// 重复同步幂等
	if n := SyncToRegistry(mgr, reg, ""); n != 1 {
		t.Fatalf("second sync registered %d tools, want 1", n)
	}
	if reg.Count() != 2 {
		t.Fatalf("registry has %d tools, want 2", reg.Count())
	}
}

// 注册表没有超时条目时执行器会套默认的 60s，连接器配置的 timeout_seconds
// 就永远不生效（构建/同步类命令会莫名在 60s 被杀）。
func TestSyncAppliesConnectorTimeout(t *testing.T) {
	reg := tool.NewRegistry()
	conn := testConnector()
	conn.TimeoutSeconds = 180
	conn.Tools[0].TimeoutSeconds = 240
	SyncToRegistry(cliconn.New(map[string]cliconn.Connector{"gh": conn}), reg, "")

	if got := reg.GetTimeout("cli_gh_pr_list"); got.Seconds() != 240 {
		t.Fatalf("tool timeout = %v, want 240s (tool-level override)", got)
	}

	// 工具级留空 → 用连接器级
	conn2 := testConnector()
	conn2.TimeoutSeconds = 180
	SyncToRegistry(cliconn.New(map[string]cliconn.Connector{"gh": conn2}), reg, "")
	if got := reg.GetTimeout("cli_gh_pr_list"); got.Seconds() != 180 {
		t.Fatalf("tool timeout = %v, want 180s (connector default)", got)
	}
}

func TestSyncToRegistryDisabledAndProfile(t *testing.T) {
	off := false
	reg := tool.NewRegistry()
	mgr := cliconn.New(map[string]cliconn.Connector{
		"gh": testConnector(),
		"kubectl": {
			Command:  "kubectl",
			Profiles: []string{"work"},
			Tools:    []cliconn.ToolSpec{{Name: "get", Description: "get", Args: []string{"get"}}},
		},
		"off": {
			Command: "x",
			Enabled: &off,
			Tools:   []cliconn.ToolSpec{{Name: "y", Description: "y", Args: []string{"y"}}},
		},
	})

	if n := SyncToRegistry(mgr, reg, "home"); n != 1 {
		t.Fatalf("home profile registered %d tools, want 1", n)
	}
	if n := SyncToRegistry(mgr, reg, "work"); n != 2 {
		t.Fatalf("work profile registered %d tools, want 2", n)
	}
	if reg.HasTool("cli_off_y") {
		t.Fatal("disabled connector must not register tools")
	}
}

func TestRemoveConnectorToolsDoesNotCrossMatch(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := cliconn.New(map[string]cliconn.Connector{
		"gh":  testConnector(),
		"ghx": {Command: "ghx", Tools: []cliconn.ToolSpec{{Name: "t", Description: "t", Args: []string{"t"}}}},
	})
	SyncToRegistry(mgr, reg, "")

	RemoveConnectorTools(reg, "gh")
	if reg.HasTool("cli_gh_pr_list") {
		t.Fatal("gh tools should be removed")
	}
	if !reg.HasTool("cli_ghx_t") {
		t.Fatal("prefix removal must not touch ghx")
	}
}

func TestSyncHandlesNil(t *testing.T) {
	reg := tool.NewRegistry()
	if n := SyncToRegistry(nil, reg, ""); n != 0 {
		t.Fatalf("nil manager returned %d", n)
	}
	if n := SyncToRegistry(cliconn.New(nil), nil, ""); n != 0 {
		t.Fatalf("nil registry returned %d", n)
	}
	RemoveConnectorTools(nil, "gh") // 不应 panic
}
