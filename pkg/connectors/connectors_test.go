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

// 设计分类契约：常用设计工具必须内置，且分类要在分类表里（前端按目录里的
// category 反查标签，漏登记会显示成裸 id）。
func TestDesignCategoryAndEntries(t *testing.T) {
	found := false
	for _, c := range Categories() {
		if c.ID == "design" {
			found = true
		}
	}
	if !found {
		t.Fatal("design category missing from Categories()")
	}

	design := Search("", "design")
	if len(design) < 10 {
		t.Fatalf("design entries = %d, want >= 10", len(design))
	}
	ids := map[string]bool{}
	for _, c := range design {
		ids[c.ID] = true
	}
	for _, want := range []string{
		"figma", "figma-api", "miro", "framer", "spline",
		"pixso", "mastergo", "modao", "jsdesign", "lanhu",
	} {
		if !ids[want] {
			t.Errorf("design catalog missing %q", want)
		}
	}
	// Canva 已从内置移除：它的 OAuth 端点本身没问题，但 www.canva.com 对中国
	// 大陆 IP 一律 302 到 canva.cn，授权页打不开（详见 connectors.go 的
	// Design 段注释）。别再加回来——加回来就是一条点了必然失败的条目。
	if ids["canva"] {
		t.Error("canva must not be re-added to the built-in catalog")
	}
}

// 令牌型远程条目的凭据必须落在 header 上，且写清头名。
//
// 远程 MCP 只认 header/query 里的令牌，若把这些条目错写成 WhereEnv，凭据会变成
// 子进程环境变量——对 http 传输完全没有意义，连接时必然 401，而用户看到的只是
// "连不上"。头名与凭据名也常常不同（MasterGo 的 x-mg-useraccesstoken），
// 空着会被退化成同名头。
func TestRemoteTokenEntriesDeclareHeader(t *testing.T) {
	for _, id := range []string{"mastergo", "modao"} {
		c, ok := Get(id)
		if !ok {
			t.Fatalf("missing entry %q", id)
		}
		if c.Transport != "http" {
			t.Errorf("%s: transport = %q, want http", id, c.Transport)
		}
		if len(c.Requires) == 0 {
			t.Fatalf("%s: no credential requirement", id)
		}
		for _, req := range c.Requires {
			if req.Where != WhereHeader {
				t.Errorf("%s: %s where = %q, want header", id, req.Key, req.Where)
			}
			if req.Header == "" {
				t.Errorf("%s: %s has empty header name", id, req.Key)
			}
			if !req.Secret || !req.Required {
				t.Errorf("%s: %s should be a required secret", id, req.Key)
			}
		}
	}
}

// OAuth 条目必须是远程 http/sse 且 URL 为绝对地址。
//
// 这条不是形式主义：把 oauth 挂在 stdio 条目上（例如用 mcp-remote 桥接 /sse），
// startOAuth 会直接报 "OAuth only applies to remote connectors"，用户点了授权
// 永远没有反应；而相对/残缺的 URL 会让发现阶段静默失败。
func TestOAuthEntriesAreRemote(t *testing.T) {
	for _, c := range All() {
		if c.OAuth == nil {
			continue
		}
		if c.Transport != "http" && c.Transport != "sse" {
			t.Errorf("%s: OAuth entry has transport %q, want http/sse", c.ID, c.Transport)
		}
		if !strings.HasPrefix(c.URL, "https://") && !strings.HasPrefix(c.URL, "http://") {
			t.Errorf("%s: OAuth entry url = %q, want absolute http(s) url", c.ID, c.URL)
		}
	}
}
