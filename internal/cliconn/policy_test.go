package cliconn

import (
	"fmt"
	"testing"
)

// 被策略拒绝的连接器不应该注册出工具——模型看不见一个注定报错的工具。
func TestSecurityCheckFiltersTools(t *testing.T) {
	m := New(map[string]Connector{
		"gh": {
			Command: "gh",
			Tools:   []ToolSpec{{Name: "pr_list", Description: "list prs", Args: []string{"pr", "list"}}},
		},
		"sh": {
			Command: "bash",
			Tools:   []ToolSpec{{Name: "run", Description: "run", Args: []string{"-c", "echo hi"}}},
		},
	})
	m.SetSecurityCheck(func(name string, c Connector) error {
		if c.Command != "gh" {
			return fmt.Errorf("command %q is not in the policy allowlist", c.Command)
		}
		return nil
	})

	tools := m.ToolsForProfile("")
	if len(tools) != 1 {
		t.Fatalf("want 1 allowed tool, got %d", len(tools))
	}
	if got := tools[0].Name(); got != "cli_gh_pr_list" {
		t.Fatalf("unexpected tool: %s", got)
	}

	blocked := m.Blocked()
	if len(blocked) != 1 {
		t.Fatalf("want 1 blocked connector, got %v", blocked)
	}
	if reason := blocked["sh"]; reason == "" {
		t.Fatal("blocked connector must carry a reason for the UI")
	}
}

// 没有注入校验时行为不变（默认放行）。
func TestNoSecurityCheckAllowsAll(t *testing.T) {
	m := New(map[string]Connector{
		"gh": {Command: "gh", Tools: []ToolSpec{{Name: "x", Description: "x", Args: []string{"version"}}}},
	})
	if len(m.ToolsForProfile("")) != 1 {
		t.Fatal("nil check must not filter anything")
	}
	if len(m.Blocked()) != 0 {
		t.Fatal("nothing should be blocked without a check")
	}
}
