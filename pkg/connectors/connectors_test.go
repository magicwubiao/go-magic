package connectors

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if len(design) < 8 {
		t.Fatalf("design entries = %d, want >= 8", len(design))
	}
	ids := map[string]bool{}
	for _, c := range design {
		ids[c.ID] = true
	}
	for _, want := range []string{
		"figma-api", "miro", "framer", "spline",
		"pixso", "mastergo", "modao", "lanhu",
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
	// Figma 桌面版 / 即时设计依赖本机客户端，见 TestLocalAppEntriesLiveInExamples。
	if ids["figma"] || ids["jsdesign"] {
		t.Error("local-app design entries must not be built in (they break on server deployments)")
	}
}

// 依赖"同一台机器上装着宿主软件"的条目不再内置，改为随示例插件分发。
//
// 这类 MCP（Blender / Unity / Godot、Figma 桌面版、即时设计）server 侧都没问题，
// 但真正干活的是本机的引擎 / 设计客户端 / 桥接插件。go-magic 经常部署在服务器
// 或另一台机器上，内置进目录就等于一条"点了必然连不上"的条目——用户看到的只是
// 超时或空工具列表，没法自己判断是环境问题，只能来问我们。
//
// 这个测试守两件事：
//  1. 它们不能回到内置目录（`game` 分类也一并删掉：它下面的条目全属这类，
//     留着就是点进去一片空白）；
//  2. 它们必须真的出现在 examples/plugins 的某个 mcp.json 里——移走了却没留下
//     替代路径，等于静默丢功能。
func TestLocalAppEntriesLiveInExamples(t *testing.T) {
	for _, c := range Categories() {
		if c.ID == "game" {
			t.Fatal("game category should be gone: every entry in it needed local software")
		}
	}

	local := []string{"blender", "unity", "godot", "figma", "jsdesign"}
	for _, id := range local {
		if _, ok := Get(id); ok {
			t.Errorf("%q must not be built in (it needs software on the same machine)", id)
		}
	}

	paths, err := filepath.Glob("../../examples/plugins/*/mcp.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("scan example plugins: %v (matched %d files)", err, len(paths))
	}

	type exampleServer struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		URL     string            `json:"url"`
		Env     map[string]string `json:"env"`
	}
	// server 键名 → 条目定义 + 所在示例目录（报错时能直接指路）。
	found := map[string]exampleServer{}
	dirOf := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		var doc struct {
			Servers map[string]exampleServer `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		dir := filepath.Base(filepath.Dir(p))
		for name, s := range doc.Servers {
			found[name] = s
			dirOf[name] = dir
		}
	}

	for _, id := range local {
		if _, ok := found[id]; !ok {
			t.Errorf("%q is neither built in nor present in examples/plugins/*/mcp.json", id)
		}
	}

	// stdio 类必须写清 command，http 类必须写清 url——示例就是给用户抄的，
	// 抄到一条缺字段的条目比没有示例更糟。
	for name, s := range found {
		switch s.Type {
		case "stdio":
			if s.Command == "" {
				t.Errorf("example server %q (%s) is stdio without command", name, dirOf[name])
			}
		case "streamable-http", "sse":
			if s.URL == "" {
				t.Errorf("example server %q (%s) is %s without url", name, dirOf[name], s.Type)
			}
		default:
			t.Errorf("example server %q (%s) has unknown type %q", name, dirOf[name], s.Type)
		}
	}

	// Godot 唯一的配置项就是"可执行文件在哪"，示例里丢了 GODOT_PATH 等于
	// 让用户拿一条永远找不到引擎的配置去调试。
	if gd, ok := found["godot"]; ok {
		if _, has := gd.Env["GODOT_PATH"]; !has {
			t.Errorf("example godot server must declare GODOT_PATH env (in %s)", dirOf["godot"])
		}
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
