package agent

import (
	"strings"
	"testing"
)

// TestSetSkillsContextReplaces verifies the skills block is replaced in place
// rather than appended: repeated refreshes must not stack duplicates, and a
// refresh with an empty list must clear the previous block.
// Regression: skills were only injected once at agent creation, so approving an
// auto skill mid-session never reached the running agent.
func TestSetSkillsContextReplaces(t *testing.T) {
	a := NewAIAgent(nil, nil, nil, "base system prompt")

	a.SetSkillsContext("SKILLS_V1")
	sys := systemContent(t, a)
	if !strings.Contains(sys, "SKILLS_V1") || !strings.Contains(sys, "base system prompt") {
		t.Fatalf("first injection missing: %q", sys)
	}

	a.SetSkillsContext("SKILLS_V2")
	sys = systemContent(t, a)
	if strings.Contains(sys, "SKILLS_V1") {
		t.Fatalf("old skills block not removed: %q", sys)
	}
	if !strings.Contains(sys, "SKILLS_V2") || !strings.Contains(sys, "base system prompt") {
		t.Fatalf("new skills block missing or base prompt lost: %q", sys)
	}
	if strings.Count(sys, "Available skills") > 1 {
		t.Fatalf("skills block duplicated: %q", sys)
	}

	// 清空：移除技能块但保留基础系统提示
	a.SetSkillsContext("")
	sys = systemContent(t, a)
	if strings.Contains(sys, "SKILLS_V2") {
		t.Fatalf("skills block not cleared: %q", sys)
	}
	if !strings.Contains(sys, "base system prompt") {
		t.Fatalf("base system prompt lost after clearing skills: %q", sys)
	}

	// 系统提示恰好只由技能块构成时，清空后不应残留分隔符
	b := NewAIAgent(nil, nil, nil, "")
	b.SetSkillsContext("ONLY_SKILLS")
	b.SetSkillsContext("")
	if got := systemContent(t, b); strings.TrimSpace(got) != "" {
		t.Fatalf("expected empty system prompt, got %q", got)
	}
}

func systemContent(t *testing.T, a *Agent) string {
	t.Helper()
	for _, m := range a.GetHistory() {
		if m.Role == "system" {
			return m.Content
		}
	}
	return ""
}
