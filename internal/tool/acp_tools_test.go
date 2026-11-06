package tool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/acp"
)

func writeACPConfig(t *testing.T, enabled bool) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "acp.json")
	cfg := &acp.Config{Agents: map[string]acp.AgentConfig{
		"claude": {
			Command:     "npx",
			Description: "coding agent",
			Enabled:     &enabled,
		},
	}}
	if err := acp.SaveConfig(path, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestACPToolsHiddenWithoutConfig(t *testing.T) {
	mgr := acp.NewManager(filepath.Join(t.TempDir(), "missing.json"))
	for _, tl := range ACPTools(mgr) {
		ac, ok := tl.(AvailabilityChecker)
		if !ok {
			t.Fatalf("%s should implement AvailabilityChecker", tl.Name())
		}
		if ac.Available() {
			t.Errorf("%s should be unavailable when no agent is configured", tl.Name())
		}
	}
}

func TestACPToolsAvailableAndListed(t *testing.T) {
	path := writeACPConfig(t, true)
	mgr := acp.NewManager(path)

	tools := ACPTools(mgr)
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Name())
		if !IsToolAvailable(tl) {
			t.Errorf("%s should be available with an enabled agent", tl.Name())
		}
		if CategoryOfTool(tl.Name()) != "acp" {
			t.Errorf("%s is not categorized under acp", tl.Name())
		}
	}
	want := []string{"acp_agents", "acp_prompt", "acp_reset_session"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", names, want)
	}

	// acp_agents 只读配置，不需要拉起子进程。
	res, err := tools[0].Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("acp_agents: %v", err)
	}
	out, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if out["count"] != 1 {
		t.Errorf("count = %v, want 1", out["count"])
	}
}

func TestACPPromptRequiresApproval(t *testing.T) {
	mgr := acp.NewManager(writeACPConfig(t, true))

	var prompt Tool
	for _, tl := range ACPTools(mgr) {
		if tl.Name() == "acp_prompt" {
			prompt = tl
		}
	}
	if prompt == nil {
		t.Fatal("acp_prompt not found")
	}

	aw, ok := prompt.(interface {
		RequiresApproval() bool
		ApprovalSummary() string
	})
	if !ok {
		t.Fatal("acp_prompt must gate on human approval")
	}
	if !aw.RequiresApproval() {
		t.Error("acp_prompt must require approval")
	}

	detail, ok := prompt.(interface {
		ApprovalSummaryWith(map[string]interface{}) string
	})
	if !ok {
		t.Fatal("acp_prompt should describe the action with its args")
	}
	summary := detail.ApprovalSummaryWith(map[string]interface{}{
		"agent":  "claude",
		"prompt": "refactor the auth module",
	})
	if !strings.Contains(summary, "claude") || !strings.Contains(summary, "refactor") {
		t.Errorf("summary = %q", summary)
	}

	// 未配置的 agent 必须在拉起任何进程之前就报错。
	if _, err := prompt.Execute(context.Background(), map[string]interface{}{
		"agent": "nope", "prompt": "hi",
	}); err == nil {
		t.Error("unknown agent should fail")
	}
}

func TestACPPromptTimeoutIsExtended(t *testing.T) {
	mgr := acp.NewManager(writeACPConfig(t, true))

	for _, tl := range ACPTools(mgr) {
		if tl.Name() != "acp_prompt" {
			continue
		}
		tp, ok := tl.(TimeoutProvider)
		if !ok {
			t.Fatal("acp_prompt must declare its own timeout")
		}
		if tp.Timeout() <= DefaultTimeout {
			t.Errorf("timeout = %s, coding agents need more than the 60s default", tp.Timeout())
		}
		return
	}
	t.Fatal("acp_prompt not found")
}
