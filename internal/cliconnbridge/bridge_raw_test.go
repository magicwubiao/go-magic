package cliconnbridge

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// raw 开启的连接器：声明工具 + raw 逃生工具都应注册，整体同步后数量一致。
func TestSyncToRegistryRaw(t *testing.T) {
	reg := tool.NewRegistry()
	conn := testConnector()
	conn.Raw = true
	mgr := cliconn.New(map[string]cliconn.Connector{"gh": conn})

	if n := SyncToRegistry(mgr, reg, ""); n != 2 {
		t.Fatalf("registered %d tools, want 2 (declared + raw)", n)
	}
	if !reg.HasTool("cli_gh_pr_list") {
		t.Fatal("declared tool not registered")
	}
	if !reg.HasTool("cli_gh_raw") {
		t.Fatal("raw tool not registered")
	}

	// 关掉 raw 再同步：raw 工具必须被整体清理掉（SyncToRegistry 先清全部 cli_*）
	conn.Raw = false
	mgr = cliconn.New(map[string]cliconn.Connector{"gh": conn})
	if n := SyncToRegistry(mgr, reg, ""); n != 1 {
		t.Fatalf("registered %d tools, want 1", n)
	}
	if reg.HasTool("cli_gh_raw") {
		t.Fatal("raw tool should be removed after the switch is turned off")
	}
}
