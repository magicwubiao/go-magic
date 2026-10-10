package catalog

import (
	"strings"
	"testing"
)

// TestCatalogIntegrity 是目录源的一致性守卫：所有消费方（Web UI 目录/预设、
// 构造函数兜底、CLI --list 与 setup 向导）都派生自这里，坏条目会同时污染
// 所有链路。改目录前跑一遍，改完也要跑。
func TestCatalogIntegrity(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range catalog {
		if p.Name == "" {
			t.Fatal("provider with empty Name")
		}
		if seen[p.Name] {
			t.Errorf("duplicate provider name %q", p.Name)
		}
		seen[p.Name] = true

		if p.DisplayName == "" {
			t.Errorf("provider %s: empty DisplayName", p.Name)
		}
		if p.DefaultModel == "" {
			t.Errorf("provider %s: empty DefaultModel", p.Name)
		}
		if len(p.Models) == 0 {
			t.Errorf("provider %s: empty model list", p.Name)
		}
		if p.Group == "" {
			t.Errorf("provider %s: empty Group", p.Name)
		}

		ids := make(map[string]bool, len(p.Models))
		for _, m := range p.Models {
			if strings.TrimSpace(m.ID) == "" {
				t.Errorf("provider %s: blank model ID", p.Name)
			}
			if ids[m.ID] {
				t.Errorf("provider %s: duplicate model ID %q", p.Name, m.ID)
			}
			ids[m.ID] = true
		}
		// custom 的 DefaultModel 只是直连构造时的兜底，不进展示列表
		if p.Name != "custom" && !ids[p.DefaultModel] {
			t.Errorf("provider %s: DefaultModel %q not in model list", p.Name, p.DefaultModel)
		}

		// custom 没有固定端点；其余必须是绝对 http(s) URL，且不带尾斜杠
		if p.Name != "custom" {
			if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
				t.Errorf("provider %s: BaseURL %q must be an absolute http(s) URL", p.Name, p.BaseURL)
			}
			if strings.HasSuffix(p.BaseURL, "/") {
				t.Errorf("provider %s: BaseURL %q should not end with '/'", p.Name, p.BaseURL)
			}
		}
	}

	// 别名必须命中真实条目，且别名不能与主名冲突
	for _, p := range catalog {
		for _, a := range p.Aliases {
			if i, ok := nameIndex[lower(a)]; !ok || i < 0 || i >= len(catalog) || catalog[i].Name != p.Name {
				t.Errorf("provider %s: alias %q does not resolve back to itself", p.Name, a)
			}
		}
	}

	// CLI 向导与文档引用的供应商必须一直在目录里
	for _, required := range []string{
		"openai", "anthropic", "deepseek", "zhipu", "dashscope", "moonshot",
		"longcat", "hunyuan", "huoshan", "wenxin", "ollama", "custom",
	} {
		if _, ok := Find(required); !ok {
			t.Errorf("required provider %q missing from catalog", required)
		}
	}
}

// TestFindAliases 兼容别名（kimi/doubao）必须正确解析到主条目。
func TestFindAliases(t *testing.T) {
	for alias, canonical := range map[string]string{
		"kimi":   "moonshot",
		"KIMI":   "moonshot", // 大小写不敏感
		"doubao": "huoshan",
	} {
		p, ok := Find(alias)
		if !ok || p.Name != canonical {
			t.Errorf("Find(%q) = %q, %v; want %q", alias, p.Name, ok, canonical)
		}
	}
}

// defaultModelRankLimit 是"默认模型"允许出现在模型列表中的最靠后位置（1 起数）。
//
// 为什么需要这条守卫：目录里"新旗舰插到列表最前面、DefaultModel 却忘了跟着改"是
// 反复出现的一类漂移 —— 用户第一次跑起来拿到的仍是上代模型。2026-10 实测就抓到
// 四处：openai 默认 gpt-6-sol（新的是 6.1-sol）、openrouter 同步漂移、groq 默认
// Llama 3.3（列表里已有 Llama 4）、together 默认 V4-Pro（已有 V4.1-Flash）。
// 列表顺序即 Web UI / CLI 的展示顺序，默认值应当落在用户一眼能看到的位置。
const defaultModelRankLimit = 3

// defaultModelRankExceptions 记录"默认模型刻意排在较后位置"的供应商：键为供应商名，
// 值为理由（空理由会被判错）。custom 的默认值不在候选列表里，由
// TestCatalogIntegrity 单独豁免。
var defaultModelRankExceptions = map[string]string{}

// TestDefaultModelIsNearTopOfList 要求每个供应商的 DefaultModel 出现在其模型列表的
// 前 defaultModelRankLimit 条内（不在列表里的跳过，交给 TestCatalogIntegrity）。
func TestDefaultModelIsNearTopOfList(t *testing.T) {
	checked := 0
	for _, p := range catalog {
		if reason, ok := defaultModelRankExceptions[p.Name]; ok {
			if reason == "" {
				t.Errorf("provider %s 列在例外表里但没写理由", p.Name)
			}
			continue
		}
		rank := -1
		for i, m := range p.Models {
			if m.ID == p.DefaultModel {
				rank = i
				break
			}
		}
		if rank < 0 {
			continue // 默认值不在候选列表里（custom 等），另有守卫
		}
		if rank >= defaultModelRankLimit {
			t.Errorf("provider %s: DefaultModel %q 排在列表第 %d 位，超出前 %d 位 —— "+
				"多半是换了新一代模型却忘了同步默认值",
				p.Name, p.DefaultModel, rank+1, defaultModelRankLimit)
		}
		checked++
	}
	if checked < 10 {
		t.Fatalf("只校验到 %d 个供应商，测试自身可能失效了", checked)
	}
	t.Logf("已校验 %d 个供应商的默认模型位置", checked)
}

// TestNoRetiredModels 确保已下线的模型 ID 不回流目录（服务端会硬报错）。
func TestNoRetiredModels(t *testing.T) {
	retired := map[string]string{
		"deepseek-chat":                        "retired 2026-07-24, replaced by deepseek-v4-*",
		"deepseek-reasoner":                    "retired 2026-07-24, replaced by deepseek-v4-*",
		"kimi-k2-0905-preview":                 "retired 2026-05-25, replaced by kimi-k*",
		"kimi-k2-instruct":                     "retired 2026-05-25, replaced by kimi-k*",
		"LongCat-Flash-Chat":                   "Flash series decommissioned 2026-05-29, replaced by LongCat-2.0-Preview",
		"LongCat-Flash-Thinking":               "Flash series decommissioned 2026-05-29",
		"LongCat-Flash-Omni":                   "Flash series decommissioned 2026-05-29",
		"abab6-chat":                           "ancient MiniMax ID, replaced by MiniMax-M*",
		"command-r-plus":                       "ancient Cohere ID, replaced by command-a-*",
		"mistralai/Mixtral-8x7B-Instruct-v0.1": "ancient Together default, replaced by DeepSeek-V4-*",
		"glm-4":                                "ancient Zhipu ID, replaced by glm-5.*",
		"hunyuan-turbo":                        "ancient Hunyuan ID, replaced by hy3/hunyuan-turbos",
	}
	for _, p := range catalog {
		for _, m := range p.Models {
			if reason, bad := retired[m.ID]; bad {
				t.Errorf("provider %s: retired model %q in catalog (%s)", p.Name, m.ID, reason)
			}
		}
		if reason, bad := retired[p.DefaultModel]; bad {
			t.Errorf("provider %s: retired DefaultModel %q (%s)", p.Name, p.DefaultModel, reason)
		}
	}
}
