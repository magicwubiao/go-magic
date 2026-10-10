package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/catalog"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestCompactionSummaryDoesNotAccumulate 是"压缩雪崩"的行为侧守卫。
//
// 旧实现每次压缩都往 history 追加一条 [CONTEXT COMPACTION] 摘要 system 消息，
// 而重建历史时又**无条件保留所有 system** ⇒ 旧摘要只增不减。实测（150 轮默认
// 参数）摘要条数涨到 69、头部 system 占上下文 96%、真实对话只剩 2 条 user，
// 压缩净收益归零。本用例锁死：无论压缩多少次，history 中的压缩摘要至多 1 条。
func TestCompactionSummaryDoesNotAccumulate(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus(), maxTotalLen: 200000, compressor: compress.NewCompressor(2000)}
	a.history = longChatHistory(20, 2000)

	for round := 0; round < 8; round++ {
		a.compressContext()
		if n := countCompactionSummaries(a); n > 1 {
			t.Fatalf("round %d: history holds %d compaction summaries, want <= 1 "+
				"(old summaries must be rolled into the new one, not kept alongside it)", round, n)
		}
		// 模拟后续回合并继续增长历史
		a.history = append(a.history,
			provider.Message{Role: "user", Content: strings.Repeat("u", 5000)},
			provider.Message{Role: "assistant", Content: "ok"},
		)
	}
}

// TestCompactionSummaryCarriesPriorForward：滚动摘要必须"接力"而不是"重来" ——
// 新的摘要要带着上一版摘要的内容，否则每次压缩都会把更早的历史真正丢掉
// （旧实现虽然留着旧摘要消息，但那些消息随头部一起膨胀，且内容互相重叠）。
func TestCompactionSummaryCarriesPriorForward(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus(), maxTotalLen: 200000, compressor: compress.NewCompressor(2000)}
	a.history = longChatHistory(20, 2000)

	if !a.compressContext() {
		t.Fatal("first compaction did not happen")
	}
	first := latestCompactionSummary(a)
	if first == "" {
		t.Fatal("first compaction produced no summary")
	}

	// 再灌一批消息，触发第二次压缩。
	a.history = append(a.history, longChatHistory(20, 2000)[1:]...)
	if !a.compressContext() {
		t.Fatal("second compaction did not happen")
	}
	if n := countCompactionSummaries(a); n != 1 {
		t.Fatalf("want exactly 1 summary after second compaction, got %d", n)
	}
	second := latestCompactionSummary(a)
	if !strings.Contains(second, "Prior Summary (carried forward)") {
		t.Fatalf("second summary did not carry the prior summary forward:\n%s", second)
	}
}

// latestCompactionSummary 返回 history 中最后一条压缩摘要的正文（去掉 SummaryPrefix）。
func latestCompactionSummary(a *Agent) string {
	for i := len(a.history) - 1; i >= 0; i-- {
		if strings.Contains(a.history[i].Content, compress.SummaryPrefix) {
			return stripCompactionSummary(a.history[i].Content)
		}
	}
	return ""
}

// TestAutoCompressThresholdFollowsModelWindow 锁死触发点的算式：
// min(窗口 × compressThresholdWindowPercent, compressThresholdCeilingTokens)。
//
// 两条边界各管一段：小窗口由比例收紧（给输出/系统提示留余量），大窗口由固定上限
// 封顶（每轮成本与注意力质量可控）。取值沿革：8000（8K 时代）→ 32000 →
// 窗口 × 60%（1M 窗口会得到 600K，每轮背着 60 万 token 输入）→ 本算式。
func TestAutoCompressThresholdFollowsModelWindow(t *testing.T) {
	if got := autoCompressThreshold(nil); got != defaultCompressThresholdTokens {
		t.Fatalf("nil provider should fall back to %d, got %d", defaultCompressThresholdTokens, got)
	}
	if got := autoHistoryByteLimit(nil); got != defaultMaxTotalLen {
		t.Fatalf("nil provider byte limit should fall back to %d, got %d", defaultMaxTotalLen, got)
	}

	// 边界一：大窗口被 ceiling 封顶，**不**随窗口放大。1M 窗口的 60% 是 600K ——
	// 那等于每轮都为 60 万 token 付全价，且长上下文会 context rot，正是要去掉的。
	//
	// 边界用例统一用"合成模型名 + provider 自报窗口"驱动（windowTestProvider 的
	// ListModels 会带上 ContextLen），断言因此只依赖算式本身，不随 pkg/catalog 的
	// 数据更替而失效；目录数据的正确性由 pkg/catalog 自己的测试与下面的全量校验兜。
	p := &windowTestProvider{model: "fake-1m", contextLen: 1050000}
	if got := autoCompressThreshold(p); got != compressThresholdCeilingTokens {
		t.Fatalf("autoCompressThreshold(1M window) = %d, want ceiling %d",
			got, compressThresholdCeilingTokens)
	}
	if got, byWindow := autoCompressThreshold(p), 1050000*compressThresholdWindowPercent/100; got >= byWindow {
		t.Fatalf("1M 窗口阈值 %d 不应达到窗口×%d%% = %d（ceiling 失效）",
			got, compressThresholdWindowPercent, byWindow)
	}
	if got := autoHistoryByteLimit(p); got != 1050000*4 {
		t.Fatalf("autoHistoryByteLimit(1M window) = %d, want %d", got, 1050000*4)
	}

	// 再大的窗口也一样：触发点与窗口大小彻底解耦。
	huge := &windowTestProvider{model: "fake-10m", contextLen: 10000000}
	if got := autoCompressThreshold(huge); got != compressThresholdCeilingTokens {
		t.Fatalf("autoCompressThreshold(10M window) = %d, want ceiling %d",
			got, compressThresholdCeilingTokens)
	}

	// 边界二：窗口 < ~333K 时由窗口比例主导（留出输出与系统提示的余量），
	// ceiling 还没轮到。
	for _, c := range []struct {
		name   string
		window int
	}{
		{"fake-256k", 262144},
		{"fake-200k", 200000},
		{"fake-128k", 128000},
		{"fake-32k", 32768}, // 旧的写死 32000 刚好与它整个窗口相当
		{"fake-8k", 8192},
	} {
		prov := &windowTestProvider{model: c.name, contextLen: c.window}
		if got, want := autoCompressThreshold(prov), c.window*compressThresholdWindowPercent/100; got != want {
			t.Fatalf("autoCompressThreshold(%s window=%d) = %d, want %d",
				c.name, c.window, got, want)
		}
		if got := autoHistoryByteLimit(prov); got != c.window*4 {
			t.Fatalf("autoHistoryByteLimit(%s window=%d) = %d, want %d",
				c.name, c.window, got, c.window*4)
		}
	}

	// 兜底必须由"假设窗口"按**同一比例**派生（并同样经 ceiling 截断），而不是另一套
	// 魔法数字：口径一旦分家，落到兜底的模型（未收录的新模型、本地/自定义端点、
	// Cohere Command、Perplexity Sonar、gpt-oss…）就会被无谓地多压一倍（旧值
	// 32000 正是"128K 窗口的 25%"，与 60% 自相矛盾）。
	if want := provider.DefaultModelContextLen * compressThresholdWindowPercent / 100; defaultCompressThresholdTokens != want {
		t.Fatalf("fallback threshold = %d, want assumed-window × %d%% = %d",
			defaultCompressThresholdTokens, compressThresholdWindowPercent, want)
	}
	if want := provider.DefaultModelContextLen * 4; defaultMaxTotalLen != want {
		t.Fatalf("fallback byte cap = %d, want assumed-window × 4 = %d", defaultMaxTotalLen, want)
	}
	// 字节级硬删必须晚于压缩触发，否则模型会在没有任何摘要的情况下丢中段上下文。
	if defaultMaxTotalLen < defaultCompressThresholdTokens*4 {
		t.Fatalf("fallback byte cap %d preempts the compression trigger %d bytes "+
			"(byte-truncation would drop whole blocks with no summary)",
			defaultMaxTotalLen, defaultCompressThresholdTokens*4)
	}

	// "未知窗口"与"已知 128K 窗口"必须走同一条算式 —— 未知不得有自己的一套写死值。
	unknown := &windowTestProvider{model: "some-brand-new-model-xyz"} // 目录没登记、provider 也答不上来
	known128 := &windowTestProvider{model: "fake-128k", contextLen: provider.DefaultModelContextLen}
	if got, want := autoCompressThreshold(unknown), autoCompressThreshold(known128); got != want {
		t.Fatalf("unknown-window threshold %d != 128K-window threshold %d "+
			"(未知与已知必须同式派生)", got, want)
	}
	if got, want := autoHistoryByteLimit(unknown), autoHistoryByteLimit(known128); got != want {
		t.Fatalf("unknown-window byte cap %d != 128K-window byte cap %d", got, want)
	}

	// 目录里的每一条都要落进同一条算式 —— 这是"目录更新后无需改测试"的自动校验：
	// 阈值必须等于 min(窗口 × 比例, ceiling)，且永不超过 ceiling。
	covered := 0
	for _, cp := range catalog.All() {
		for _, cm := range cp.Models {
			if cm.ContextLen <= 0 {
				continue
			}
			got := autoCompressThreshold(&windowTestProvider{model: cm.ID})
			want := cm.ContextLen * compressThresholdWindowPercent / 100
			if want > compressThresholdCeilingTokens {
				want = compressThresholdCeilingTokens
			}
			if got != want {
				t.Fatalf("目录模型 %s/%s（窗口 %d）算出阈值 %d，期望 %d",
					cp.Name, cm.ID, cm.ContextLen, got, want)
			}
			covered++
		}
	}
	if covered == 0 {
		t.Fatal("目录里没有任何登记了窗口的模型，本测试已失去意义")
	}
	t.Logf("目录覆盖校验：%d 条登记了 ContextLen 的模型全部落进 min(窗口×%d%%, ceiling)",
		covered, compressThresholdWindowPercent)

	// ceiling 的两条不变量 —— 它现在是"按当前世代设定的那个合理值"，是被维护的
	// 唯一数字，所以必须锁住合理区间：
	//   - > 兜底阈值：否则天花板比小窗口的收紧值还低，两级边界互相打架；
	//   - < 主流 1M 窗口 × 比例(600K)：否则又退化成"跟着窗口放大"，1M 模型每轮
	//     背着几十万 token 输入跑，成本与 context rot 都失控；
	//   - >= 100K：低于它长任务会被压得过勤，反而损害任务连续性。
	if compressThresholdCeilingTokens <= defaultCompressThresholdTokens {
		t.Fatalf("ceiling %d 必须大于兜底阈值 %d", compressThresholdCeilingTokens, defaultCompressThresholdTokens)
	}
	byBigWindow := 1050000 * compressThresholdWindowPercent / 100
	if compressThresholdCeilingTokens < 100000 || compressThresholdCeilingTokens >= byBigWindow {
		t.Fatalf("ceiling %d 超出合理区间 [100000, %d)", compressThresholdCeilingTokens, byBigWindow)
	}

	// 假设窗口**仍不跟着当前世代抬到 256K/1M**：猜小只是多压几次（滚动摘要后是
	// 低损软失败），猜大则请求超过真实窗口，而上下文超限不会自愈
	// （FailoverContextOverflow 给出的 Action "compress"/Compress 全仓无人消费）
	// ⇒ 立即重试 → 再撞 → 被重复失败检测升级成回合失败。大窗口模型改用 ceiling
	// 受益，属于另一条支路，不构成抬高本值的理由。
	if w := provider.DefaultModelContextLen; w < 32000 || w > 262144 {
		t.Fatalf("假设窗口 %d 超出合理区间 [32000, 262144]", w)
	}
	t.Logf("触发点算式：min(窗口×%d%%, ceiling %d token)；兜底链：假设窗口 %d → "+
		"阈值 %d token → 历史硬上限 %d 字节（压缩触发点 %d 字节）",
		compressThresholdWindowPercent, compressThresholdCeilingTokens,
		provider.DefaultModelContextLen, defaultCompressThresholdTokens,
		defaultMaxTotalLen, defaultCompressThresholdTokens*4)

	// 代表模型的最终取值（仅打印，便于人工核对）。
	for _, m := range []string{
		"gpt-6.1-sol", "claude-opus-5-5", "deepseek-flash", "kimi-k2.6",
		"claude-haiku-4-5", "some-brand-new-model-xyz",
	} {
		w := provider.ModelContextLen(m) // 0 = 目录未登记（走假设窗口）
		t.Logf("  %-24s window=%-9d threshold=%d token",
			m, w, autoCompressThreshold(&windowTestProvider{model: m}))
	}
}

// windowTestProvider 实现 Provider + Modeler，供窗口推导测试使用。
//
// contextLen > 0 时它像"用户配置了模型列表 / 在线拉取了模型"的 provider 一样自报
// 窗口；为 0 时 Modeler 答不上来（模拟自填 ID 带不上窗口的情形）。
// 注意 ModelContextLenFor 是**目录优先**：若 model 名恰好登记在 pkg/catalog 里，
// 会以目录值为准，测试要用合成名才能确定性地控制窗口。
type windowTestProvider struct {
	model      string
	contextLen int
}

func (p *windowTestProvider) Chat(ctx context.Context, msgs []provider.Message) (*provider.ChatResponse, error) {
	return nil, nil
}
func (p *windowTestProvider) Name() string            { return "window-test" }
func (p *windowTestProvider) SetModel(m string) error { p.model = m; return nil }
func (p *windowTestProvider) GetModel() string        { return p.model }
func (p *windowTestProvider) ListModels() []provider.ModelInfo {
	if p.contextLen <= 0 {
		return nil
	}
	return []provider.ModelInfo{{ID: p.model, ContextLen: p.contextLen}}
}

// TestLongRunContextStaysBounded 是"几百轮长任务"的端到端回归。
//
// 修复前的实测曲线（同样参数、同样 150 轮）：压缩摘要 1→4→16→31→45→57→69 条，
// 头部 system 占上下文 12%→43%→91%→96%，真实对话被挤到只剩 2 条 user。根因是
// 每次压缩都追加一条摘要、旧摘要又无条件保留。本用例同时锁两件事：
//  1. history 中的压缩摘要永远 <= 1（滚动摘要生效）；
//  2. 头部 system 占上下文的比重保持在低位（不再被历次摘要填满）。
func TestLongRunContextStaysBounded(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus(), maxTotalLen: 200000, compressor: compress.NewCompressor(32000)}
	a.history = longChatHistory(4, 500)

	maxHeadShare := 0
	for turn := 1; turn <= 150; turn++ {
		a.history = append(a.history, provider.Message{Role: "user", Content: strings.Repeat("u", 400)})
		a.history = append(a.history, provider.Message{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "c1", Name: "read_file", Arguments: map[string]interface{}{"path": "x.go"}},
			},
		})
		a.history = append(a.history, provider.Message{Role: "tool", ToolCallID: "c1",
			Content: strings.Repeat("f", 6000)})

		a.maybeCompressContext()
		a.truncateHistory()

		if n := countCompactionSummaries(a); n > 1 {
			t.Fatalf("turn %d: history holds %d compaction summaries, want <= 1", turn, n)
		}
		head := 0
		for _, m := range a.history {
			if m.Role == "system" {
				head += len(m.Content)
			}
		}
		if total := a.GetHistoryLength(); total > 0 {
			if share := head * 100 / total; share > maxHeadShare {
				maxHeadShare = share
			}
		}
	}

	// 修复前该值在 150 轮时达到 96%。留足余量后仍能拦住"摘要重新堆积"的回归。
	if maxHeadShare > 60 {
		t.Fatalf("head system peaked at %d%% of the context over 150 turns (want <= 60%%); "+
			"compaction summaries are accumulating again", maxHeadShare)
	}
	t.Logf("head system peak share over 150 turns: %d%%", maxHeadShare)
}
