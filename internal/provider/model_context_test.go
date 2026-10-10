package provider

import (
	"context"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/catalog"
)

// TestModelContextLenComesFromCatalog 锁死"窗口只有 pkg/catalog 一个数据源"。
//
// 这正是删掉 model_window.go 之后要保证的事：目录里登记了 ContextLen 的每条模型
// 都能按 ID 查到同一个值（含聚合网关的 "provider/id" 前缀形式），未登记的一律返回
// 0 交给调用方兜底 —— 绝不按名称瞎猜，那样又会变成第二个数据源。
//
// 断言全部由目录自身派生，所以目录换代时本测试不需要跟着改。
func TestModelContextLenComesFromCatalog(t *testing.T) {
	covered := 0
	for _, p := range catalog.All() {
		for _, m := range p.Models {
			if m.ContextLen <= 0 {
				continue
			}
			if got := ModelContextLen(m.ID); got != m.ContextLen {
				t.Fatalf("ModelContextLen(%q) = %d, want %d", m.ID, got, m.ContextLen)
			}
			prefixed := p.Name + "/" + m.ID
			if got := ModelContextLen(prefixed); got != m.ContextLen {
				t.Fatalf("ModelContextLen(%q) = %d, want %d（带供应商前缀的形式）",
					prefixed, got, m.ContextLen)
			}
			covered++
		}
	}
	if covered == 0 {
		t.Fatal("目录里没有任何登记了窗口的模型，本测试已失去意义")
	}
	t.Logf("目录窗口校验：%d 条模型均可按 ID 与 provider/id 查到", covered)

	// 未登记 / 空串 / 空白 → 0（不猜）
	for _, name := range []string{"", "   ", "totally-unknown-model-9000"} {
		if got := ModelContextLen(name); got != 0 {
			t.Fatalf("ModelContextLen(%q) = %d, want 0（未登记必须返回 0，由调用方兜底）", name, got)
		}
	}

	// 大小写不敏感（用户可能原样粘贴文档里的大写 ID）
	lowered := ""
	for _, p := range catalog.All() {
		for _, m := range p.Models {
			if m.ContextLen > 0 {
				lowered = m.ID
				break
			}
		}
		if lowered != "" {
			break
		}
	}
	if got := ModelContextLen("  " + upper(lowered) + " "); got != ModelContextLen(lowered) {
		t.Fatalf("大小写/空白不敏感失效：%q → %d, %q → %d",
			upper(lowered), got, lowered, ModelContextLen(lowered))
	}
}

func upper(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			r = r - 'a' + 'A'
		}
		out = append(out, r)
	}
	return string(out)
}

// modelLookupProvider 是只实现窗口查询所需接口的最小 provider。
type modelLookupProvider struct {
	model  string
	models []ModelInfo
}

func (p *modelLookupProvider) Chat(context.Context, []Message) (*ChatResponse, error) {
	return nil, nil
}
func (p *modelLookupProvider) Name() string            { return "lookup-test" }
func (p *modelLookupProvider) SetModel(m string) error { p.model = m; return nil }
func (p *modelLookupProvider) GetModel() string        { return p.model }
func (p *modelLookupProvider) ListModels() []ModelInfo { return p.models }

// TestModelContextLenForPrefersCatalog 锁死解析顺序：目录 → provider 自报列表 → 0。
func TestModelContextLenForPrefersCatalog(t *testing.T) {
	// 目录命中优先于 provider 自报（目录是权威源；自报列表可能来自用户配置或
	// 在线拉取，带不上窗口，或带的是错的）。
	catalogID := ""
	for _, p := range catalog.All() {
		for _, m := range p.Models {
			if m.ContextLen > 0 {
				catalogID = m.ID
				break
			}
		}
		if catalogID != "" {
			break
		}
	}
	want := ModelContextLen(catalogID)
	if want <= 0 {
		t.Fatal("目录里没有可用样本")
	}
	withSelfReported := &modelLookupProvider{
		model:  catalogID,
		models: []ModelInfo{{ID: catalogID, ContextLen: 4096}},
	}
	if got := ModelContextLenFor(withSelfReported); got != want {
		t.Fatalf("ModelContextLenFor(%q) = %d, want %d（目录优先）", catalogID, got, want)
	}

	// 目录未登记 → 用 provider 自报的窗口（本地/自定义端点常见）
	local := &modelLookupProvider{
		model:  "my-local-llama",
		models: []ModelInfo{{ID: "my-local-llama", ContextLen: 32768}},
	}
	if got := ModelContextLenFor(local); got != 32768 {
		t.Fatalf("ModelContextLenFor(本地模型) = %d, want 32768", got)
	}

	// 都答不上来 → 0（调用方按 DefaultModelContextLen 兜底）
	if got := ModelContextLenFor(&modelLookupProvider{model: "mystery-model-xyz"}); got != 0 {
		t.Fatalf("未知模型应返回 0，got %d", got)
	}
	// nil / 空模型名 / 不支持 Modeler 的 provider → 0
	if got := ModelContextLenFor(nil); got != 0 {
		t.Fatalf("nil provider 应返回 0，got %d", got)
	}
	if got := ModelContextLenFor(&modelLookupProvider{}); got != 0 {
		t.Fatalf("空模型名应返回 0，got %d", got)
	}
	if got := ModelContextLenFor(&plainProvider{}); got != 0 {
		t.Fatalf("未实现 Modeler 的 provider 应返回 0，got %d", got)
	}
}

// plainProvider 只实现 Provider，不实现 Modeler。
type plainProvider struct{}

func (p *plainProvider) Chat(context.Context, []Message) (*ChatResponse, error) { return nil, nil }
func (p *plainProvider) Name() string                                           { return "plain" }
