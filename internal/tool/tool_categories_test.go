package tool

import (
	"testing"
)

// tool_categories_test.go —— 分类覆盖率的关卡测试。
//
// 新增工具必须登记进 tool_categories.go，否则这里直接红：
// 未分类的工具会掉进前端 "Other" 大杂烩，且无法被工具集开关控制。

func TestToolCategoriesCoverAllRegisteredTools(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(t.TempDir())
	// skill 工具需要 manager，但名字必须已登记分类
	for _, name := range r.List() {
		if cat := CategoryOfTool(name); cat == "" {
			t.Errorf("tool %q is not categorized (add it to tool_categories.go)", name)
		}
	}
}

func TestCategoryOfToolKnownNames(t *testing.T) {
	cases := map[string]string{
		"read_file":                 "file",
		"directory_tree":            "file",
		"diff_patch":                "file",
		"web_search":                "web",
		"browser_navigate":          "browser",
		"execute_command":           "terminal",
		"execute_code":              "coding",
		"format_code":               "coding",
		"image_gen":                 "media",
		"tts":                       "media",
		"computer_use":              "desktop",
		"search_history":            "memory",
		"session_search":            "memory",
		"cronjob":                   "task",
		"kanban_show":               "kanban",
		"delegate_task":             "delegation",
		"skill":                     "skills",
		"clarify":                   "interaction",
		"interrupt":                 "interaction",
		"json":                      "utility",
		"ha_get_state":              "smart_home",
		"mcp_github_create_pr":      "mcp",
		"ap_cli-git_cli_git_status": "plugins",
	}
	for name, want := range cases {
		if got := CategoryOfTool(name); got != want {
			t.Errorf("CategoryOfTool(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestToolCategoriesNoDuplicates(t *testing.T) {
	seen := map[string]string{}
	for _, cat := range toolCategories {
		for _, name := range cat.Tools {
			if prev, ok := seen[name]; ok {
				t.Errorf("tool %q appears in both %q and %q", name, prev, cat.ID)
			}
			seen[name] = cat.ID
		}
	}
}

func TestToolsetFilterAllows(t *testing.T) {
	cases := []struct {
		note         string
		cat, tool    string
		enabled, dis []string
		want         bool
	}{
		{"no filter", "web", "web_search", nil, nil, true},
		{"category disabled", "web", "web_search", nil, []string{"web"}, false},
		{"tool override wins", "web", "web_search", []string{"web_search"}, []string{"web"}, true},
		{"other tool still hidden", "web", "web_fetch", []string{"web_search"}, []string{"web"}, false},
		{"all disabled", "web", "web_search", nil, []string{"all"}, false},
		{"all disabled but cat enabled", "web", "web_search", []string{"web"}, []string{"all"}, true},
		{"enabled alone is not whitelist", "file", "read_file", []string{"web"}, nil, true},
		{"single tool disabled", "file", "read_file", nil, []string{"read_file"}, false},
	}
	for _, c := range cases {
		if got := ToolsetFilterAllows(c.cat, c.tool, c.enabled, c.dis); got != c.want {
			t.Errorf("%s: ToolsetFilterAllows(%q,%q,enabled=%v,disabled=%v) = %v, want %v",
				c.note, c.cat, c.tool, c.enabled, c.dis, got, c.want)
		}
	}
}

func TestRegistryToolsetFilter(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(t.TempDir())

	countSchemas := func() int { return len(r.ListWithSchemas()) }

	base := countSchemas()
	if base == 0 {
		t.Fatal("no tools available after RegisterAll")
	}

	// 关掉整个 browser 分组
	r.SetToolsetFilter(nil, []string{"browser"})
	got := map[string]bool{}
	for _, tl := range r.ListAvailable() {
		got[tl.Name()] = true
	}
	for _, tl := range r.ListAvailable() {
		if CategoryOfTool(tl.Name()) == "browser" {
			t.Errorf("browser tool %q still exposed after disabling group", tl.Name())
		}
	}
	if len(r.ListWithSchemas()) >= base {
		t.Error("disabling browser group should shrink the exposed list")
	}

	// 单工具豁免：browser_snapshot 单独放行
	r.SetToolsetFilter([]string{"browser_snapshot"}, []string{"browser"})
	found := false
	for _, tl := range r.ListAvailable() {
		if tl.Name() == "browser_snapshot" {
			found = true
		} else if CategoryOfTool(tl.Name()) == "browser" {
			t.Errorf("browser tool %q should stay hidden", tl.Name())
		}
	}
	if !found {
		t.Error("browser_snapshot should be exempted by enabled list")
	}

	// 清空过滤恢复全量
	r.SetToolsetFilter(nil, nil)
	if countSchemas() != base {
		t.Errorf("after clearing filter: %d schemas, want %d", countSchemas(), base)
	}
}
