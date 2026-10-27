package connectors

import (
	"strings"
	"testing"
)

// 目录契约测试：办公分类必须存在且条目可用，arg 类 Requirement 必须在
// Args 里有对应的 {{KEY}} 占位符（否则前端替换会静默落空）。

func TestOfficeCategoryAndEntries(t *testing.T) {
	cats := Categories()
	if cats[0].ID != "office" {
		t.Fatalf("first category = %q, want office", cats[0].ID)
	}

	office := Search("", "office")
	if len(office) < 8 {
		t.Fatalf("office entries = %d, want >= 8", len(office))
	}
	ids := map[string]bool{}
	for _, c := range office {
		ids[c.ID] = true
	}
	for _, want := range []string{"lark", "dingtalk", "yuque", "ms365", "asana", "confluence", "jira", "airtable"} {
		if !ids[want] {
			t.Errorf("office catalog missing %q", want)
		}
	}
}

func TestArgRequirementsHavePlaceholders(t *testing.T) {
	for _, c := range All() {
		for _, req := range c.Requires {
			if req.Where != WhereArg {
				continue
			}
			token := "{{" + req.Key + "}}"
			found := false
			for _, a := range c.Args {
				if strings.Contains(a, token) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: arg requirement %s has no %s placeholder in args %v", c.ID, req.Key, token, c.Args)
			}
		}
	}
}

func TestLarkEntryShape(t *testing.T) {
	lark, ok := Get("lark")
	if !ok {
		t.Fatal("lark entry missing")
	}
	if lark.Transport != "stdio" || lark.Command != "npx" {
		t.Fatalf("lark = %s/%s", lark.Transport, lark.Command)
	}
	if !strings.Contains(strings.Join(lark.Args, " "), "@larksuiteoapi/lark-mcp") {
		t.Fatalf("lark args = %v", lark.Args)
	}
}
