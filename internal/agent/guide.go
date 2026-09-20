package agent

import (
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// ============================================================================
// 引导（guide）注入：回合进行中用户追加的补充指示。
//
// 语义：不打断正在生成的回合。引导消息进入收件箱，回合 goroutine 在下一次
// LLM 调用前排水并入历史——模型在当前回合内就看到它并调整方向；若回合在
// 排水前已经结束（模型进入最终回答、不再有迭代），server 侧的残留回收会把
// 未消费的引导转成新的排队回合（见 internal/server/chatqueue.go
// reclaimLeftoverGuides）——回收出的回合沿用引导注入时分配的 id，因此会话
// 历史里仍然只有一条该内容的消息（引导注入时已落库），不会重复。
//
// 并发契约（不可违反）：
//   - push（InjectGuide）：HTTP handler goroutine，任意时刻可调用；
//   - drain（DrainGuides）：只允许两个身份调用——本回合的执行 goroutine
//     （迭代顶部排水）与 server 的回合收尾（残留回收）。两条路径都发生在
//     「本回合已经不会再消费」或「尚未开始消费」的边界上，与 server 侧
//     q.running 的握手保证不会抢走模型本该看到的内容。
//   - a.history 只由回合 goroutine 读改，因此 applyGuides（排水后并入历史）
//     必须只在回合 goroutine 上调用。
// ============================================================================

// guidePrefix 是并入历史时给引导文本加的前缀：模型据此知道这是用户在回合
// 进行中追加的补充指示，而不是重开的话题。
const guidePrefix = "[Guide]"

// guideJoiner 是多条引导合并进同一条尾随 user 消息时的分隔。
const guideJoiner = "\n\n"

// GuideItem 是收件箱里的一条待消费引导。
//
// ID 由注入方（server 的 tryInjectGuide）分配，agent 侧不解释、不使用它，
// 只负责原样保管到排水时刻：若这条引导没被模型消费（回合在排水前就结束了），
// server 的收尾回收会把它转成一条新的排队回合——届时要让「回收出的回合」与
// 「注入时已落库的引导消息」共用同一个 id，否则同一句话会在会话历史里出现
// 两次（用户看到"引导发了两次"）。见 internal/server/chatqueue.go 的
// reclaimLeftoverGuides 与 persistUserMessage。
type GuideItem struct {
	ID   string
	Text string
}

// guideInbox 是 Agent 上的引导收件箱。零值即可用（sync.Mutex 零值为未锁定，
// 切片零值为 nil），因此 Agent 结构体无需任何初始化改动。
type guideInbox struct {
	mu    sync.Mutex
	items []GuideItem
}

func (g *guideInbox) push(item GuideItem) {
	g.mu.Lock()
	g.items = append(g.items, item)
	g.mu.Unlock()
}

func (g *guideInbox) drain() []GuideItem {
	g.mu.Lock()
	out := g.items
	g.items = nil
	g.mu.Unlock()
	return out
}

// InjectGuide 向正在运行的回合注入一条用户引导消息。非阻塞、线程安全。
// server 侧必须在「当前会话确有回合在跑」的判定下调用（见 tryInjectGuide）；
// 空白文本被静默忽略。id 是注入方给这条引导分配的稳定标识（见 GuideItem），
// 可为空——为空时仅退化为"回收时无法与落库消息对齐"，不影响注入本身。
func (a *Agent) InjectGuide(id, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	a.guideInbox.push(GuideItem{ID: id, Text: text})
}

// DrainGuideItems 取走当前积累的全部引导（调用后收件箱清空），保留 id。
// 见文件头的并发契约：只有回合执行 goroutine 与 server 回合收尾可以调用。
func (a *Agent) DrainGuideItems() []GuideItem {
	return a.guideInbox.drain()
}

// DrainGuides 是 DrainGuideItems 的纯文本视图（**同一次排水**，先调用者拿走
// 全部内容，后调用者拿到空切片；不可在同一处期待两次结果）。模型侧迭代顶部
// 排水只需要文本，故保留此便捷形式。
func (a *Agent) DrainGuides() []string {
	items := a.guideInbox.drain()
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Text)
	}
	return out
}

// applyGuides 把排水出的引导并入会话历史（只在回合 goroutine 上调用）。
//
// 合并策略——为什么不能无脑 append 新 user 消息：
//   - 迭代 0 时历史尾部就是本回合输入的 user 消息，append 会产生连续两条
//     user，而 SanitizeMessageHistory 对这种情况的处置是「丢旧的、留新的」
//     （见 messages.go STAGE 2 的 user 分支）——本回合的原始输入会被整个
//     丢掉，只剩引导文本，对话上下文断裂。
//   - 因此尾随消息是 user 时必须「并入」：纯文本消息把引导接在 Content
//     之后；多模态消息（ContentParts，如带截图的输入）追加一个 text part，
//     保持多模态结构不变（同时写 Content 和 ContentParts 的消息在出站
//     转换时行为未定义，不能碰 Content）。
//   - 尾随是 assistant/tool（工具结果）时，追加带前缀的新 user 消息是
//     合法序列（user 跟在 tool 之后不违反 alternation 规则）。
func (a *Agent) applyGuides(guides []string) {
	for _, g := range guides {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if n := len(a.history); n > 0 && a.history[n-1].Role == "user" {
			last := &a.history[n-1]
			if len(last.ContentParts) > 0 {
				last.ContentParts = append(last.ContentParts, types.ContentPart{
					Type: "text",
					Text: guidePrefix + " " + g,
				})
			} else if last.Content == "" {
				last.Content = guidePrefix + " " + g
			} else {
				last.Content += guideJoiner + guidePrefix + " " + g
			}
			continue
		}
		a.history = append(a.history, provider.Message{
			Role:      "user",
			Content:   guidePrefix + " " + g,
			Timestamp: time.Now(),
		})
	}
}

// drainGuidesIntoHistory 是回合迭代顶部的固定动作：排水 + 并入历史 + 裁剪。
// 只在回合 goroutine 上调用。
func (a *Agent) drainGuidesIntoHistory() {
	guides := a.DrainGuides()
	if len(guides) == 0 {
		return
	}
	a.applyGuides(guides)
	a.truncateHistory()
}
