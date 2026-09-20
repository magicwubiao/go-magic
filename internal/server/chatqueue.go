package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

// ============================================================================
// Per-session message queue
//
// 背景：改造前 handleSessionStream 把整个回合同步跑在 HTTP handler 里，并用
// 一个全局 map（streamCancels）记录"哪个 session 正在跑"。这带来三个问题：
//
//  1. 用户在一个回合进行中再发消息：前端靠 `chatStore.streaming` 把输入拦掉，
//     消息被静默丢弃；绕过前端（第二个标签页、手机 + 电脑、脚本）则会两个
//     回合并发跑同一个 *agent.Agent —— 历史交叉污染、读改写丢消息、cancel
//     func 被后者覆盖导致前一个回合无法停止。
//  2. 回合状态只存在于内存 map 里，既没有队列信息也无法区分"在跑"和"排队"。
//  3. 回合与 SSE 连接一一绑定，无法把已有回合的实时输出接到新连接上。
//
// 现在改为「每会话一个 FIFO 串行队列」：
//
//   - handleSessionStream 只负责解析请求 → Enqueue → 挂 SSE 监听（sink），
//     然后立刻返回；真正的回合由 worker goroutine 串行执行。
//   - 同一会话永远只有一个回合在跑，后续消息排队等待，先到先执行。
//   - SSE sink 与回合解耦：回合进行中也能被续接，客户端断开只影响推送，
//     不影响回合执行（保持原有"回合与连接解耦"的语义）。
//
// 事件总线的设计（写路径唯一 + 写者单飞）：
//
//	sinkMu 保护 sinks，turnMu 串行化所有写者。sink 的 Append 在持有 sinkMu
//	的情况下调用，因此「关闭某个 sink」与「向它追加事件」不会竞争：closeSink
//	把 sink 从 map 摘除（后续 append 不再命中它）后再关闭其 chan，而
//	appendEvent 只有在持锁期间把事件塞进 chan 才会返回 true。于是 appendEvent
//	返回 true 就意味着事件必定被落库，返回 false 则调用方走无人监听的兜底
//	路径（把已生成的文本直接写入会话历史）。
// ============================================================================

const (
	// sessionTurnTimeout 是单个回合的执行上限（与原 handleSessionStream 中
	// 的 30 分钟一致）。超时会被取消，回合按"被中断"处理并照常落库。
	sessionTurnTimeout = 30 * time.Minute
	// turnEventBuffer 是单个 sink 的事件缓冲。一个 30 分钟的长回合会产生
	// 成百上千条 delta，加上工具事件与心跳，1024 与 sseWriter 的缓冲同量级。
	turnEventBuffer = 1024
	// turnKeepAliveInterval 是"附着但没有回合在跑"时的保活心跳间隔。
	turnKeepAliveInterval = 5 * time.Second
	// turnIdleTeardown 是空闲队列在无监听者情况下的回收时间。回合结束后
	// 保留一小段时间，便于客户端稍晚重连拿结果；超过则彻底释放。
	turnIdleTeardown = 60 * time.Second
	// queuedContentPreview 是 /running 里回给前端的排队消息预览长度。
	queuedContentPreview = 120
	// queueWaitWarnThreshold 是"排队超过多久就记一条 WARN"的阈值。用户报告过
	// "发新消息有时候长时间排队"，但事后无从判断是前一个回合拖了太久、还是
	// worker 该醒没醒——这条日志把等待时长与当时队列深度留在日志里，下次
	// 复现就能直接定位，而不是靠猜。
	queueWaitWarnThreshold = 5 * time.Second
)

// turnEvent 是回合向 SSE 连接广播的一条事件。data 是已经序列化好的整帧
// （例如 `{"delta":"hi"}`），由 sink 负责按 SSE 分帧写出。
type turnEvent struct {
	data string
	done bool
	// queueIdle 与 done 配对使用：表示"本回合结束后队列已经彻底排空，不会
	// 再有后续回合"。转发层据此决定是否关闭这条 SSE 连接——没有它就分不清
	// "这一轮完了但后面还有"与"整个队列都干完了"，只能二选一：要么过早关闭
	// （后排的消息推不出去），要么永不关闭（连接泄漏）。
	queueIdle bool
	err       error // 回合已结束但结果未能落库时的错误说明
}

// turnSink 是一个 SSE 订阅者。回合事件被 append 到 evch，由消费 goroutine
// 逐条转发到客户端。完成后 sink 关闭 evch；调用方 close(sink.done) 声明自己
// 不再读取（幂等由 doneOnce 保证），避免队列在缓冲写满时永久阻塞。
type turnSink struct {
	evch     chan turnEvent
	done     chan struct{}
	doneOnce sync.Once
}

func newTurnSink() *turnSink {
	return &turnSink{
		evch: make(chan turnEvent, turnEventBuffer),
		done: make(chan struct{}),
	}
}

// cancel 声明该 sink 的消费方已退出。重复调用安全（例如 SSE 写失败与
// handler defer 同时触发）。
func (snk *turnSink) cancel() {
	snk.doneOnce.Do(func() { close(snk.done) })
}

// queuedTurn 是一条排队等待执行的消息。contentParts 里只保留轻量内容
// （文本、上传引用、物化摘要）——inline base64 在入队前就被剥离了（见
// stripInlineMediaParts），否则几条排队消息就能把内存和会话库撑爆；
// 被剥离的图片在回合开跑时由 rehydrateMediaRefs 从磁盘还原。
type queuedTurn struct {
	id           string
	content      string
	contentParts []types.ContentPart
	// persistedParts 是落库版本（inline 载荷已换成上传引用）。在入队前算好，
	// 因为 persistedContentParts 需要当时还存在的 imageURLRefs/imageNames。
	persistedParts []types.ContentPart
	createdAt      time.Time
	// run 携带回合执行上下文（工作目录、文件变更跟踪器），避免聊天队列
	// 反向依赖 sessions.go 里的类型细节。
	run interface{}
}

// sessionQueue 是一个会话的串行消息队列。所有字段由 mu 保护。
type sessionQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	sinks    map[*turnSink]struct{}
	sinkMu   sync.Mutex // 独立于 mu：关闭 sink 与写入 sink 都需持锁，避免与回合执行互锁
	items    []*queuedTurn
	running  bool
	cancel   context.CancelFunc
	activeID string
	// workerLive 标记该队列的 worker goroutine 是否已启动/仍在运行。
	// 由 chatQueuesMu 保护（而非 mu），保证同一队列只会有一个 worker 被
	// 拉起——两个人同时发消息不会各自 spawn 一个 worker 并行跑同一个 agent。
	workerLive bool
	// cancelRequested 记录"用户点了停止"：用于区分「被取消」和「回合真的出错」，
	// 取消导致的 ctx 错误不应作为 error 事件推送给用户（前端已有停止态）。
	cancelRequested bool
	// turns 是本队列已执行的回合数，仅用于调试与测试断言。
	turns int
}

func newSessionQueue() *sessionQueue {
	q := &sessionQueue{sinks: make(map[*turnSink]struct{})}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// ============================================================================
// Queue registry (Server methods)
// ============================================================================

// sessionQueueFor 返回（必要时创建）指定会话的队列。返回 nil 表示该会话
// 已空闲回收，调用方应重试或忽略。
func (s *Server) sessionQueueFor(sessionID string) *sessionQueue {
	s.chatQueuesMu.Lock()
	q := s.chatQueues[sessionID]
	if q == nil {
		q = newSessionQueue()
		s.chatQueues[sessionID] = q
	}
	s.chatQueuesMu.Unlock()
	return q
}

// lookupSessionQueue 只查不建（用于 /running、会话加载等只读路径）。
func (s *Server) lookupSessionQueue(sessionID string) *sessionQueue {
	s.chatQueuesMu.Lock()
	q := s.chatQueues[sessionID]
	s.chatQueuesMu.Unlock()
	return q
}

// dropSessionQueue 在队列空闲且无监听者时回收，避免 Server 长时间运行后
// chatQueues 无限增长。
func (s *Server) dropSessionQueue(sessionID string, q *sessionQueue) {
	s.chatQueuesMu.Lock()
	if s.chatQueues[sessionID] == q {
		q.mu.Lock()
		idle := !q.running && len(q.items) == 0
		q.mu.Unlock()
		if idle {
			delete(s.chatQueues, sessionID)
		}
	}
	s.chatQueuesMu.Unlock()
}

// ============================================================================
// Sink management
// ============================================================================

// addSink 注册一个 SSE 订阅者。
func (q *sessionQueue) addSink() *turnSink {
	snk := newTurnSink()
	q.sinkMu.Lock()
	q.sinks[snk] = struct{}{}
	q.sinkMu.Unlock()
	return snk
}

// closeSink 摘除并关闭 sink。先摘除再关闭 evch：摘除之后 appendEvent 再也
// 不会命中它，因此关闭是安全的（不会向已关闭 channel 发送）。
func (q *sessionQueue) closeSink(snk *turnSink) {
	q.sinkMu.Lock()
	if _, ok := q.sinks[snk]; ok {
		delete(q.sinks, snk)
		close(snk.evch)
	}
	q.sinkMu.Unlock()
}

// removeSink 只摘除不关闭（用于消费方已经自行退出的场景）。
func (q *sessionQueue) removeSink(snk *turnSink) {
	q.sinkMu.Lock()
	if _, ok := q.sinks[snk]; ok {
		delete(q.sinks, snk)
	}
	q.sinkMu.Unlock()
}

// appendEvent 把一条事件广播给所有监听者。返回 true 表示至少有一个监听者
// 确实收到了事件（即该事件"有承接方"）。缓冲写满时会等待监听者消费或
// 监听者声明退出——后者保证不会因为浏览器卡住而阻塞回合执行。
func (q *sessionQueue) appendEvent(ev turnEvent) bool {
	q.sinkMu.Lock()
	defer q.sinkMu.Unlock()

	delivered := false
	for snk := range q.sinks {
		select {
		case snk.evch <- ev:
			delivered = true
		case <-snk.done:
			// 消费方已退出，交给下面统一清理
		default:
			select {
			case snk.evch <- ev:
				delivered = true
			case <-snk.done:
			case <-time.After(5 * time.Second):
				// 监听者长时间不消费（挂起的标签页 / 假死连接）：
				// 放弃本次投递，回合继续执行，避免整条流水线被拖住。
			}
		}
	}
	return delivered
}

// ============================================================================
// Enqueue / Cancel
// ============================================================================

// enqueue 把一条消息追加到队列尾部并唤醒 worker。
func (q *sessionQueue) enqueue(item *queuedTurn) *queuedTurn {
	if item.id == "" {
		item.id = uuid.NewString()
	}
	if item.createdAt.IsZero() {
		item.createdAt = time.Now()
	}
	q.mu.Lock()
	q.items = append(q.items, item)
	q.mu.Unlock()
	q.cond.Signal()
	return item
}

// cancelSignal 是 /cancel 的结果，供 handler 组装响应。
type cancelSignal struct {
	// active 表示确实有一个正在执行的回合被取消。
	active bool
	// pending 是被丢弃的排队消息数（仅统计尚未落库的队列项）。
	pending int
}

// cancelAll 实现"停止"语义：丢弃全部排队消息，并取消正在执行的回合。
// 前端在用户点停止时会清空本地排队气泡，因此这里必须同步清空服务端队列，
// 否则刷新后那些消息会重新冒出来执行。
func (q *sessionQueue) cancelAll() cancelSignal {
	q.mu.Lock()
	pending := len(q.items)
	q.items = nil
	cancel := q.cancel
	q.cancelRequested = cancel != nil
	q.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return cancelSignal{active: cancel != nil, pending: pending}
}

// clearPending 只丢弃排队等待的消息，**不动正在执行的回合**。
//
// 这是第三种停止语义，与另外两个刻意区分开：
//
//   - cancelAll    ：停止这一切——取消当前回合 + 清空队列（用户点"停止"）；
//   - dropItem     ：只丢用户点名的那一条，其余排队消息照旧；
//   - clearPending ：保留当前回合跑完（用户还想看这条回答），只把后面
//     等着的一串撤掉。
//
// 为什么必须单独有一个：把"我不想看后面那些"也做成 cancelAll，用户就得为了
// 删掉几条待发消息而牺牲正在生成的回答；反过来若复用 dropItem，用户得一条
// 一条点删除。运行中的回合不受影响这件事由实现保证——这里只切 items、不碰
// q.cancel，也不置 cancelRequested。
//
// 返回被丢弃的条数。
func (q *sessionQueue) clearPending() int {
	q.mu.Lock()
	pending := len(q.items)
	q.items = nil
	q.mu.Unlock()
	return pending
}

// dropItem 从队列中移除指定的一条尚未执行的排队消息。
//
// 与 cancelAll 的区别：cancelAll 是"停止这一切"（连正在跑的回合一起杀），
// dropItem 只针对用户明确点名的那一条，正在执行的回合与其它排队消息都不受影响。
//
// 返回 false 表示该 id 不在队列里（已被 worker 认领开始执行、或是别人刚删过）。
// 这个区分很重要：若该条已经开始执行，前端不能把它当作"删掉了"处理。
func (q *sessionQueue) dropItem(id string) bool {
	if id == "" {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, it := range q.items {
		if it.id != id {
			continue
		}
		q.items = append(q.items[:i], q.items[i+1:]...)
		return true
	}
	return false
}

// updateItem 就地替换一条尚未执行的排队消息的内容。
//
// 保留原 id 与 created_at，因此前端不需要重新对账——排队气泡的 id 不变，
// 只换内容，视觉上就是"这条改了"。返回 false 表示该 id 已不在队列里。
func (q *sessionQueue) updateItem(id string, content string, contentParts, persistedParts []types.ContentPart) bool {
	if id == "" {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.id != id {
			continue
		}
		it.content = content
		it.contentParts = contentParts
		it.persistedParts = persistedParts
		return true
	}
	return false
}

// findItem 返回指定排队项的**完整**内容与附件快照。
//
// 存在的理由：/running 里的 queued[].content 是给人看的一行预览，被
// shortenQueuedContent 截到 120 字符。前端"编辑排队消息"要把内容回填进
// 输入框，拿预览去填就等于把用户写的东西砍掉大半——刷新页面后尤其明显
// （那时本地已无原件，只能问服务端）。因此编辑路径必须能取到全文。
//
// 返回 ok=false 表示该条已不在队列里（被 worker 认领执行、或被删了）。
// 注意这里刻意不返回 *queuedTurn：调用方只该拿到一份拷贝，避免在锁外
// 读写队列内部状态。
func (q *sessionQueue) findItem(id string) (content string, parts []types.ContentPart, ok bool) {
	if id == "" {
		return "", nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.id != id {
			continue
		}
		// 附件与快照同源（queuedAttachmentsOf 已是轻量引用，不含 base64）。
		return it.content, queuedAttachmentsOf(it), true
	}
	return "", nil, false
}

// itemExists 判断某条消息是否仍在队列中等待执行（尚未被 worker 认领）。
func (q *sessionQueue) itemExists(id string) bool {
	if id == "" {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.id == id {
			return true
		}
	}
	return false
}

// moveItem 把某条排队消息挪到指定位置（0 起，超界自动夹到末尾）。
//
// 队列本身仍是 FIFO 串行执行的，这里改的是"等待中的顺序"。正在执行的回合
// 不受影响（它已经不在 items 里了），因此拖动排队项永远不会打断当前回答。
//
// 返回 (是否存在, 是否真的发生了位移)。第二项用于让 handler 区分
// "拖到原处"（无需广播，否则多端会因为一次空操作各自重排、列表闪一下）与
// "确实换了位置"。id 为空或不在队列里时返回 (false, false)。
func (q *sessionQueue) moveItem(id string, to int) (bool, bool) {
	if id == "" {
		return false, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	from := -1
	for i, it := range q.items {
		if it.id == id {
			from = i
			break
		}
	}
	if from < 0 {
		return false, false
	}
	if to < 0 {
		to = 0
	}
	if to > len(q.items)-1 {
		to = len(q.items) - 1
	}
	if to == from {
		return true, false
	}
	it := q.items[from]
	q.items = append(q.items[:from], q.items[from+1:]...)
	// 摘除之后切片短了一位，插入位置需按插入前的目标下标还原：
	// 目标在原位置之后时，摘除动作已经让它左移了一格。
	q.items = append(q.items, nil)
	copy(q.items[to+1:], q.items[to:])
	q.items[to] = it
	return true, true
}

// ============================================================================
// Snapshot (/running 与会话加载)
// ============================================================================

// queuedTurnInfo 是回给前端的排队消息精简描述。
type queuedTurnInfo struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
	Position  int    `json:"position"` // 1 起：1 表示下一个执行
	// Attachments 是这条排队消息携带的附件（图片与文件统一为 file 形态），
	// 与 content 一样是「预览」：页面刷新后前端手上只剩服务端快照，而
	// queuedAttachments 是纯内存 Map（刷新即丢）——不带上附件的话，一条
	// 「图 + 一句话」的排队消息刷新后就只剩文字，点重试会把图弄丢。
	// 因此这里回传落库版本（persistedParts），它本身就是轻量引用，
	// 不含 inline base64，序列化开销可以忽略。
	Attachments []types.ContentPart `json:"attachments,omitempty"`
}

// queuedAttachmentsOf 从一条排队消息中挑出附件部件。
//
// 优先取 persistedParts（落库版本：inline base64 已换成上传引用路径，图片
// 也已归一成带 name/url/mime 的 file 部件），它正是前端重发时要还原的东西。
// persistedParts 为空（早期入队路径没算）时退回 contentParts，同样把
// image_url 部件翻译成 file 形态——前端只需要一种形态。
func queuedAttachmentsOf(item *queuedTurn) []types.ContentPart {
	parts := item.persistedParts
	if len(parts) == 0 {
		parts = item.contentParts
	}
	var out []types.ContentPart
	for _, p := range parts {
		switch {
		case p.Type == "file" && p.File != nil:
			// Contents 里可能还留着 inline base64（persistedParts 已清空），
			// 这里同样清掉：快照只用于展示与重发引用，回传 MB 级 base64 会
			// 让 /running 的响应体无谓地膨胀。
			out = append(out, types.ContentPart{
				Type: "file",
				File: &types.FileInfo{
					Name:     p.File.Name,
					MimeType: p.File.MimeType,
					URL:      p.File.URL,
				},
			})
		case p.Type == "image_url" && p.ImageURL != nil:
			out = append(out, types.ContentPart{
				Type: "file",
				File: &types.FileInfo{
					Name:     "",
					MimeType: imageMimeForRef(p.ImageURL.URL, ""),
					URL:      p.ImageURL.URL,
				},
			})
		}
	}
	return out
}

// uploadRef 是前端重发一条带附件的排队消息（retry）时提交的轻量引用。
// 与 chatPayload.Files 的元素同构子集，便于后端复用同一条解析路径。
type uploadRef struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

// queueSnapshot 是队列的只读快照。
type queueSnapshot struct {
	running  bool
	activeID string
	items    []queuedTurnInfo
}

func (q *sessionQueue) snapshot() queueSnapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	snap := queueSnapshot{running: q.running, activeID: q.activeID}
	for i, it := range q.items {
		snap.items = append(snap.items, queuedTurnInfo{
			ID:          it.id,
			Content:     shortenQueuedContent(it.content),
			CreatedAt:   it.createdAt.Unix(),
			Position:    i + 1,
			Attachments: queuedAttachmentsOf(it),
		})
	}
	return snap
}

// queueInfoFor 是 Server 层的便捷封装：不存在队列时返回空闲快照。
func (s *Server) queueInfoFor(sessionID string) queueSnapshot {
	if q := s.lookupSessionQueue(sessionID); q != nil {
		return q.snapshot()
	}
	return queueSnapshot{}
}

// shortenQueuedContent 截断排队消息的预览文本，按 rune 计数避免切断多字节字符。
func shortenQueuedContent(content string) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= queuedContentPreview {
		return content
	}
	return string(runes[:queuedContentPreview]) + "…"
}

// ============================================================================
// Worker loop
// ============================================================================

// shouldWorkerExit 报告 worker 此刻是否应当退出。调用方须持有 q.mu。
//
// 判据只有一条：**队列已排空**。与是否存在 SSE 监听者无关。
//
// 这一条曾经写成「队列为空 **且** 无监听者才退出」，并在取件前先判
// `if len(q.items) == 0 { return }`——两者叠加出的行为是：
//
//   - 无监听者 + 队列为空 → 在上面那个 for 条件里根本不 Wait，直接落到
//     `len(q.items) == 0` 的 return，队列被回收；
//   - 而"无监听者"在真实使用中极易出现：前端收到 done 时若 state.queued
//     为空（正在执行的那条已被移出，后续消息还没排进来）就会 close() 掉
//     SSE 连接。于是"用户停止观看"被误当成"没人需要队列了"。
//
// 分开看：有 sink 时 worker 等待新消息是对的（复用连接）；但**没有 sink
// 绝不意味着没有工作**——排队消息必须照常执行，否则用户点完发送关了页面，
// 回来会发现消息卡死。因此退出只由 items 决定。
func shouldWorkerExit(q *sessionQueue) bool {
	return len(q.items) == 0 && !q.hasSinksLocked()
}

// runQueue 是该会话唯一的回合执行 goroutine：串行取出队列头部消息执行，
// 空闲时挂起等待唤醒。
//
// 退出条件：队列空且没有 SSE 监听者（见 shouldWorkerExit）。此时队列会被
// 回收，workerLive 复位，下一条消息到达时重新拉起 worker——长时间不活跃的
// 会话因此不会常驻 goroutine。注意 workerLive 必须在 mu 之外、且在判定
// "确实要退出"之后复位，否则会出现"worker 已决定退出但新消息刚入队"的
// 竞态（消息没人消费）。
func (s *Server) runQueue(sessionID string, q *sessionQueue) {
	defer s.releaseWorker(sessionID, q)

	for {
		q.mu.Lock()
		// 有监听者时挂起等待；没有监听者时不 Wait（直接走到下面的退出判定）。
		for len(q.items) == 0 && q.hasSinksLocked() {
			q.cond.Wait()
		}
		if shouldWorkerExit(q) {
			q.mu.Unlock()
			return
		}
		item := q.items[0]
		q.items = q.items[1:]
		q.running = true
		q.activeID = item.id
		q.cancelRequested = false
		turnCtx, turnCancel := context.WithTimeout(context.Background(), sessionTurnTimeout)
		q.cancel = turnCancel
		q.mu.Unlock()

		// 排队时长观测：等待明显偏长时留一条 WARN（含当时的队列深度），
		// 用于区分"前一个回合跑太久"和"worker 没被唤醒"这两类原因。
		if waited := time.Since(item.createdAt); waited > queueWaitWarnThreshold {
			log.Warnf("[queue] session %s: turn %s waited %s in queue before starting (pending=%d)",
				sessionID, item.id, waited.Truncate(time.Millisecond), q.pendingCount())
		}

		finalOps := s.runQueuedTurnSafely(sessionID, q, turnCtx, item)

		turnCancel()

		// 回合收尾（顺序不可变）：
		//   1) running 翻负与「残留引导回收」在同一 q.mu 临界区内完成——
		//      tryInjectGuide 依据同一把锁判定 running，因此它判定为真时
		//      注入的引导，要么已被本回合迭代顶部排水（模型看到），要么被
		//      这里的回收转成新排队回合，绝不悬空到之后某个无关回合（那会
		//      把引导错误地拼进下一条消息的开头）。
		//   2) 残留入队之后才广播 done：done 帧的 queue_depth/queue_idle
		//      必须把残留回合算进去，否则 SSE 连接会在还有后续回合时被
		//      前端/转发层误导关闭——下一回合零监听者，delta 全走"只落库
		//      不推送"的兜底（血债：排队消息执行完却不刷新）。
		var leftovers []string
		q.mu.Lock()
		wasCancelled := q.cancelRequested
		q.running = false
		q.activeID = ""
		q.cancel = nil
		q.cancelRequested = false
		q.turns++
		if a := s.lookupAgent(sessionID); a != nil {
			leftovers = a.DrainGuides()
			if wasCancelled {
				leftovers = nil // 用户已停止：未消费的引导一并丢弃
			}
		}
		q.mu.Unlock()

		for _, g := range leftovers {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			leftoverRun := &turnRunCtx{fileOps: NewTurnFileOpTracker()}
			if s.sessionStore != nil {
				if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
					leftoverRun.workDir = sess.WorkDir
					leftoverRun.workDirUserSet = sess.WorkDirUserSet
				}
			}
			// 引导残留转成普通排队回合（内容不带 [Guide] 前缀——对模型而言
			// 它就是一条新的用户消息）。enqueueChatTurn 的查重会把与队列中
			// 已有项完全相同的残留合并掉。
			//
			// 已知边界（issue 4）：残留回收发生在「回合已结束、引导尚未被模型
			// 消费」的时刻，而前端在注入那一刻就已把引导气泡转正（user_<id>）。
			// 于是用户视角会出现「已转正的气泡 + 新排队项」并存——引导从
			// 即时指引退化为一条待执行的普通消息，气泡会被后置排队项「打回」。
			// 这是注入即消费与收尾回收两条路径的固有语义差：注入时无法预知
			// 回合是否会及时消费。彻底消除该跳变需要服务端在回收时向客户端
			// 发送「引导被回收为排队项」的专用事件，由前端撤销已转正的气泡；
			// 当前以低频边界场景接受此轻微跳变，暂不实现。
			s.enqueueChatTurn(sessionID, g, nil, nil, leftoverRun, "")
		}

		// done 广播（自 runQueuedTurn 上移至此，见上）。
		pending := q.pendingCount()
		doneData, _ := json.Marshal(map[string]interface{}{
			"done":     true,
			"file_ops": finalOps,
			"turn_id":  item.id,
			// queue_depth 是"本回合结束后还剩多少条待执行消息"。前端据此决定
			// 是否保留 SSE 连接：队列非空时必须留着，否则下一条的
			// stream_started/delta 推不到客户端，用户看到的就是"消息执行完了，
			// 排队的那条没被发送"。仅靠前端本地 state.queued 判断不够——新消息
			// 可能在 done 之后才入队，那一刻本地是空的。
			"queue_depth": pending,
			"queue_idle":  pending == 0,
		})
		q.broadcast(turnEvent{data: "data: " + string(doneData) + "\n\n", done: true, queueIdle: pending == 0})
	}
}

// releaseWorker 在 worker 退出时复位 workerLive 并回收空闲队列。复位与
// "是否还有未消费消息"的判定必须在 chatQueuesMu 下完成：若复位后队列里
// 已有新消息，则立即重新拉起 worker，避免消息永久滞留。
//
// "队列里还有消息"这件事必须优先于一切回收动作。历史上这里漏掉了
// workerLive 复位的原子性：释放在 chatQueuesMu 内、但重新拉起 worker 却在
// 锁外，于是存在一个窗口——worker A 已复位 workerLive，新消息入队时看到
// workerLive==false 而拉起 worker B，同时 A（或另一个 defer）又拉起一个
// worker C，两个 worker 并行消费同一个 agent。现在统一在锁内决策、锁外启动，
// 且启动前再确认一次 workerLive 已被自己置位。
func (s *Server) releaseWorker(sessionID string, q *sessionQueue) {
	s.chatQueuesMu.Lock()
	if s.chatQueues[sessionID] != q || !q.workerLive {
		s.chatQueuesMu.Unlock()
		return
	}
	q.workerLive = false

	q.mu.Lock()
	pending := len(q.items)
	idle := pending == 0
	q.mu.Unlock()

	if pending > 0 {
		// worker 退出与新消息入队发生竞态：把 worker 交还给新消息。
		// 队列绝不能在有待执行消息时被回收，因此这里不删 map。
		q.workerLive = true
		s.chatQueuesMu.Unlock()
		safeGo(func() { s.runQueue(sessionID, q) })
		return
	}
	if idle && !q.hasAnySink() {
		delete(s.chatQueues, sessionID)
	}
	s.chatQueuesMu.Unlock()
}

// hasSinksLocked 报告是否还有 SSE 监听者（调用方须持有 q.mu）。
// 读取 sinks 需要 sinkMu，注意两把锁的获取顺序始终是 mu -> sinkMu，
// 与 appendEvent（只持 sinkMu）不会形成环。
func (q *sessionQueue) hasSinksLocked() bool {
	q.sinkMu.Lock()
	n := len(q.sinks)
	q.sinkMu.Unlock()
	return n > 0
}

// ============================================================================
// Idle teardown
// ============================================================================

func (q *sessionQueue) hasAnySink() bool {
	q.sinkMu.Lock()
	n := len(q.sinks)
	q.sinkMu.Unlock()
	return n > 0
}

// keepAlive 在"已附着但没有回合在跑"时维持 SSE 连接（心跳）。回合开始时
// worker 会广播 stream_started，前端据此从"空闲附着"切换到流式渲染。
func (s *Server) keepAlive(ctx context.Context, writeSSE func(string) bool, q *sessionQueue) {
	ticker := time.NewTicker(turnKeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !writeSSE("data: {\"type\":\"ping\"}\n\n") {
				return
			}
		}
	}
}

// ============================================================================
// Turn execution
// ============================================================================

// runQueuedTurnSafely 执行一个回合并把 panic 收敛在回合边界内。
//
// 为什么必须收敛：runQueue 的收尾（running 翻负、cancel 复位、残留引导回收、
// done 广播）写在执行回合之后。一旦回合内部 panic，收尾整段被跳过，于是
// q.running 会**永远停在 true**、q.cancel 不再复位——/running 一直回答
// "有回合在跑"，前端据此把新消息显示成"排队中"并等一个永远不会来的
// stream_started，而实际上没有任何 worker 在干活。用户看到的现象正是
// "对话早就执行完/停止过了，发新消息却长时间排队"。
//
// 历史上这条路径真的被触发过（Cortex 禁用时 Trigger 为 nil，每个回合都
// panic），而 safeGo 的 recover 只保证进程不死，不负责让队列状态复原。
//
// panic 被恢复后按"回合已结束但结果异常"处理：记日志、向监听者广播一条
// error 事件，随后收尾照常执行（done 依旧会广播，排队中的下一条能接上）。
func (s *Server) runQueuedTurnSafely(sessionID string, queue *sessionQueue, ctx context.Context, item *queuedTurn) (ops []types.FileOp) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[server] PANIC in queued turn %s (session %s): %v\n%s",
				item.id, sessionID, r, debug.Stack())
			queue.broadcast(turnEvent{err: fmt.Errorf("internal error while running the turn: %v", r)})
		}
	}()
	return s.runQueuedTurn(sessionID, queue, ctx, item)
}

// runQueuedTurn 执行队列中的一条消息。这是改造前 handleSessionStream 里
// "跑 agent"那段逻辑的搬迁，差别只在于：
//   - 事件不再直接写 HTTP 连接，而是 append 到队列 sink（可能没人监听）；
//   - 队列项在入队前已剥离 inline base64（见 stripInlineMediaParts），
//     执行时先经 rehydrateMediaRefs 还原，落库只需按上传引用重建 content parts；
//   - 回合开始/结束都向所有 sink 广播，排队中的后续消息因此能被前端正确
//     显示为"正在执行"；
//   - done 广播在 runQueue 的收尾临界区之后（残留引导回收 + 残留入队），
//     本函数只返回本轮 file ops 供 done 帧使用。
func (s *Server) runQueuedTurn(sessionID string, queue *sessionQueue, ctx context.Context, item *queuedTurn) []types.FileOp {
	run, ok := item.run.(*turnRunCtx)
	if !ok || run == nil {
		run = &turnRunCtx{fileOps: NewTurnFileOpTracker()}
	}
	ctx = run.applyTo(ctx)

	queue.broadcast(turnEvent{data: sseTurnStarted(item)})

	// 用户消息在本回合真正开跑时落库（而不是入队时）：入队后可能等待很久
	// 甚至被 /cancel 清掉，提前落库会让"被丢弃的排队消息"污染会话历史。
	// 落库顺序因此天然与执行顺序一致，不会出现历史里两条 user 紧邻。
	s.persistUserMessage(sessionID, item)

	// 在 SSE 不可用（无人监听）时，事件里的文本就是唯一产物。这里把已生成
	// 的文本累积起来，回合结束后直接写入会话历史，避免"后台跑的回合"结果
	// 完全丢失——回合与连接解耦之后这是必须的兜底。
	var streamed strings.Builder
	deliveredAny := false

	var fullResponse strings.Builder
	streamHandler := func(chunk string, done bool) {
		if done || chunk == "" {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		streamed.WriteString(chunk)

		// 工具标记 → 结构化事件（与改造前一致的解析逻辑）
		if strings.Contains(chunk, ">>>TOOL_START|") {
			re := regexp.MustCompile(`>>>TOOL_START\|([^|]+)\|(.*)<<<`)
			if m := re.FindStringSubmatch(chunk); m != nil {
				toolName, toolArgs := m[1], m[2]
				argsSummary := toolArgs
				if len(argsSummary) > 200 {
					argsSummary = truncateRunes(argsSummary, 200) + "..."
				}
				ev, _ := json.Marshal(map[string]interface{}{
					"type":     "tool_start",
					"name":     toolName,
					"args":     argsSummary,
					"file_ops": extractFileOps(toolName, toolArgs, ""),
				})
				if queue.broadcast(turnEvent{data: "data: " + string(ev) + "\n\n"}) {
					deliveredAny = true
				}
			}
			return
		}

		if strings.Contains(chunk, ">>>TOOL_RESULT_START|") {
			re := regexp.MustCompile(`>>>TOOL_RESULT_START\|([^|]+)\|([^|]+)\|([^<]+)<<<`)
			endRe := regexp.MustCompile(`>>>TOOL_RESULT_END<<<`)
			startMatch := re.FindStringSubmatchIndex(chunk)
			endMatch := endRe.FindStringIndex(chunk)
			if startMatch != nil && endMatch != nil {
				submatch := re.FindStringSubmatch(chunk[startMatch[0]:startMatch[1]])
				if len(submatch) >= 4 {
					toolName := submatch[1]
					toolSuccess := submatch[2] == "true"
					toolDuration := submatch[3]
					toolContent := chunk[startMatch[1]:endMatch[0]]
					if len(toolContent) > 500 {
						toolContent = utils.Truncate(toolContent, 500)
					}
					ev, _ := json.Marshal(map[string]interface{}{
						"type":     "tool_result",
						"name":     toolName,
						"success":  toolSuccess,
						"duration": toolDuration,
						"content":  strings.TrimSpace(toolContent),
						"file_ops": extractFileOps(toolName, "{}", toolContent),
					})
					if queue.broadcast(turnEvent{data: "data: " + string(ev) + "\n\n"}) {
						deliveredAny = true
					}
				}
			}
			return
		}

		if strings.Contains(chunk, ">>>TURN_START<<<") {
			return
		}

		fullResponse.WriteString(chunk)
		ev, _ := json.Marshal(map[string]string{"delta": chunk})
		if queue.broadcast(turnEvent{data: "data: " + string(ev) + "\n\n"}) {
			deliveredAny = true
		}
	}

	// 用户的 stop 会取消 ctx；agent 自身的超时收尾（gracefulDeadlineFinish）
	// 会正常返回 nil。两者都在这里统一收尾。
	var streamErr error
	if len(item.contentParts) > 0 {
		// 入队时 inline base64 被剥离成 "ref:" 引用（见 stripInlineMediaParts），
		// 这里在真正送进模型之前从 uploads 磁盘读回字节还原成图片部件。
		// 没有这一步，模型只能看到占位文字——用户粘贴的截图会整个"失踪"。
		parts := s.rehydrateMediaRefs(item.contentParts)
		streamErr = s.runAgentStreamWithMedia(ctx, sessionID, item.content, parts, streamHandler)
	} else {
		streamErr = s.runAgentStream(ctx, sessionID, item.content, streamHandler)
	}

	// 先取本回合的取消标记，再落库：这两个顺序不能颠倒——落库过程本身会
	// 触发记忆沉淀等副作用，期间队列状态可能已经翻页。
	cancelled := queue.cancelWasRequested()

	// 本轮"变更的文件"（写前快照 + 净 diff，见 fileops.go）在回合真正结束后
	// 才能取到；先落库再广播 done，保证前端拿到 done 时数据已经一致。
	finalOps := run.fileOps.Result()
	s.persistAssistantMessage(sessionID, fullResponse.String(), streamed.String(), finalOps, deliveredAny)

	// 用量记账：队列路径的回合跑在 worker 里，不会经过旧 /api/chat 那套收尾，
	// 这里不显式记一笔，/usage 页面就再也不会有新数据（表现为"用量统计不到"）。
	// 即使本回合一个字都没产出（fullResponse 为空、上面没落库），已经消耗的
	// token 照样要记账。
	s.accountTurnUsage(sessionID)

	switch {
	case streamErr != nil && !cancelled:
		queue.broadcast(turnEvent{err: streamErr})
	case cancelled:
		// 用户主动停止：不推送 error（前端已进入停止态），但仍要发 done
		// 让排队中的下一条消息能接上（队列已被 cancelAll 清空则不会再有）。
	}

	// done 广播已上移至 runQueue（在残留引导回收与残留入队之后执行），
	// 保证 done 帧的 queue_depth/queue_idle 把残留回合计算在内——见
	// runQueue 收尾处的竞态与顺序说明。
	return finalOps
}

// pendingCount 返回尚未执行的排队消息条数（不含正在执行的这一条）。
func (q *sessionQueue) pendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// runAgentStream 调用会话对应的 agent。取不到 agent 时返回错误，由调用方
// 以 error 事件的形式推给客户端。
func (s *Server) runAgentStream(ctx context.Context, sessionID, input string, handler agent.StreamHandler) error {
	a := s.getOrCreateAgent(sessionID)
	if a == nil {
		return errProviderNotConfigured{}
	}
	return a.RunConversationStream(ctx, input, handler)
}

func (s *Server) runAgentStreamWithMedia(ctx context.Context, sessionID, input string, parts []types.ContentPart, handler agent.StreamHandler) error {
	a := s.getOrCreateAgent(sessionID)
	if a == nil {
		return errProviderNotConfigured{}
	}
	return a.RunConversationStreamWithMedia(ctx, input, parts, handler)
}

// ============================================================================
// 引导（guide）注入
// ============================================================================

// tryInjectGuide 在「当前会话确有回合在跑」时把引导消息注入运行中的回合、
// 落库并广播 guide_added，返回给客户端的响应体；返回 nil 表示此刻无法注入
// （没有运行中回合 / 纯附件无文本 / 拿不到 agent），调用方应回落普通入队——
// 引导就退化为一条普通的排队消息，绝不静默丢弃。带图/附件的多模态引导会
// 提取 text 部件作为纯文本即时注入（图片不随注入路径携带），详见函数体内。
//
// 竞态封闭性：running 判定与 InjectGuide 在 q.mu 同一临界区内完成，而
// runQueue 的收尾把 running 翻负与残留引导回收放进同一临界区——两侧在此
// 握手：这里判定 running 为真时，注入的引导要么被本回合的迭代顶部排水
// （模型看到），要么被收尾回收转成新排队回合，绝不会悬空到之后某个无关
// 回合（那会把引导错误地拼进下一条消息的开头）。
func (s *Server) tryInjectGuide(sessionID string, parsed *parsedChatPayload) map[string]interface{} {
	content := strings.TrimSpace(parsed.content)
	if content == "" {
		return nil
	}
	// 多模态（带图/附件）引导：注入路径没有 media 还原管线（它绑定在
	// 「入队 → 回合开跑」的 stripInlineMediaParts → rehydrateMediaRefs 上），
	// 无法把图片即时注入运行中的回合。这里退而求其次：仅提取 text 部件作为
	// 纯文本引导即时注入，保住用户文本指引不被丢；图片不随注入路径携带
	// （模型本轮看不到图，只能靠文本描述）。若连文本都没有（纯附件），才
	// 回落普通入队——附件必须走完整还原管线，不能静默丢弃。
	if len(parsed.contentParts) > 0 {
		textParts := make([]string, 0, len(parsed.contentParts))
		for _, p := range parsed.contentParts {
			if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
				textParts = append(textParts, strings.TrimSpace(p.Text))
			}
		}
		if len(textParts) == 0 {
			// 纯附件引导：无文本可注入，回落入队走完整还原管线。
			return nil
		}
		content = strings.Join(textParts, "\n")
	}
	a := s.lookupAgent(sessionID)
	if a == nil {
		return nil
	}
	q := s.lookupSessionQueue(sessionID)
	if q == nil {
		return nil
	}

	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return nil
	}
	a.InjectGuide(content)
	q.mu.Unlock()

	// 引导在采集后即已被消费（"停止"只会丢弃尚未消费的收件箱残留），直接落库
	// 为普通 user 消息即可，历史顺序天然正确：本回合输入 < 引导 < 本回合
	// assistant 回复。
	//
	// id 必须在落库前生成并同时用于广播/响应：前端以 user_<id> 作为气泡去重键，
	// 落库消息若不带同一个 id，刷新页面后会与 guide_added 广播产生重复气泡
	// （见 issue 3）。
	id := uuid.NewString()
	s.persistGuideMessage(sessionID, id, content)

	ev, _ := json.Marshal(map[string]interface{}{
		"type":    "guide_added",
		"id":      id,
		"content": content,
	})
	// 广播给本会话所有 SSE 监听者（多标签页同步刷新气泡）。无人监听也无妨：
	// 消息已注入 agent 并落库，客户端刷新后从会话历史拿回。
	q.broadcast(turnEvent{data: "data: " + string(ev) + "\n\n"})

	return map[string]interface{}{
		"guided":    true,
		"id":        id,
		"role":      "user",
		"content":   content,
		"timestamp": time.Now().Unix(),
	}
}

// persistGuideMessage 把引导消息作为普通 user 消息写入会话历史，并携带与
// 广播/响应一致的 id（前端 user_<id> 去重键）。与排队消息「回合开跑才落库」
// 不同：引导注入即消费，不存在被丢弃的窗口，直接落库即可。
//
// 注意落库内容是不带 [Guide] 前缀的纯文本（用户输入的原样）。这与
// applyGuides 并入 agent history 时加前缀并不冲突：落库给用户看（所见即
// 所输），history 前缀给模型看（提示这是回合中追加的补充指示）。二者语义
// 目标不同，落库形态是正确的，勿误判为不一致。
func (s *Server) persistGuideMessage(sessionID, id, content string) {
	if s.sessionStore == nil {
		return
	}
	sess, err := s.sessionStore.LoadSession(context.Background(), sessionID)
	if err != nil {
		return
	}
	sess.Messages = append(sess.Messages, types.Message{
		ID:        id,
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	sess.UpdatedAt = time.Now()
	_ = s.sessionStore.SaveSession(context.Background(), sess)
}

// errProviderNotConfigured 表示服务端尚未配置 LLM provider。
type errProviderNotConfigured struct{}

func (errProviderNotConfigured) Error() string {
	return "LLM provider not configured. Please set up a provider in Settings."
}

// persistUserMessage 把队列项对应的用户消息写入会话历史。
//
// 时机选择很关键：不能在入队时写。排队消息可能等待很久，也可能被用户的
// "停止"直接丢弃（cancelAll 会清空队列）——提前落库会让这些从未执行的消息
// 永久留在会话历史里。等到回合真正开始执行再写，历史顺序自然与执行顺序
// 一致，也不会出现两条 user 消息紧邻（中间缺 assistant）的畸形序列。
func (s *Server) persistUserMessage(sessionID string, item *queuedTurn) {
	if s.sessionStore == nil {
		return
	}
	sess, err := s.sessionStore.LoadSession(context.Background(), sessionID)
	if err != nil {
		return
	}
	sess.Messages = append(sess.Messages, types.Message{
		Role:         "user",
		Content:      item.content,
		ContentParts: item.persistedParts,
		Timestamp:    time.Now(),
	})
	sess.UpdatedAt = time.Now()
	_ = s.sessionStore.SaveSession(context.Background(), sess)
}

// persistAssistantMessage 落库本回合的 assistant 消息。
//
// 回合与 SSE 连接解耦之后，"没人监听"变成常态（用户在别的标签页发起、
// 手机锁屏、排队消息在后台执行），因此文本必须由服务端主动落库：
//   - 文本优先取 fullResponse（已过滤 >>>TOOL_* 标记的最终回答）；
//   - 若模型一个字都没产出而只有工具事件，退回 streamed 原文，避免整轮
//     执行痕迹消失（改造前只在有文本时落库，排队场景下会整轮丢失）。
func (s *Server) persistAssistantMessage(sessionID, fullResponse, streamed string, finalOps []types.FileOp, _ bool) {
	if s.sessionStore == nil {
		return
	}
	if strings.TrimSpace(fullResponse) == "" {
		return
	}
	sess, err := s.sessionStore.LoadSession(context.Background(), sessionID)
	if err != nil {
		return
	}
	sess.Messages = append(sess.Messages, types.Message{
		Role:      "assistant",
		Content:   fullResponse,
		Timestamp: time.Now(),
		FileOps:   finalOps,
	})
	// token 不在这里累加：GetTokenStats 是会话级累计值，每回合加一次全量会
	// 让会话 token 数越滚越大。增量由 accountTurnUsage 统一记账（它同时写
	// usage 统计与本会话字段），本回合只管消息文本。
	sess.UpdatedAt = time.Now()
	_ = s.sessionStore.SaveSession(context.Background(), sess)
}

// ============================================================================
// Broadcast helpers
// ============================================================================

// broadcast 向所有监听者推送一条事件，返回是否至少有一个监听者收到。
func (q *sessionQueue) broadcast(ev turnEvent) bool {
	return q.appendEvent(ev)
}

// pushSessionCardEvent 把一张"交互卡片"事件（审批 / 澄清）推给该会话的所有
// SSE 连接。返回是否至少有一个页面收到了它。
//
// 为什么必须有这条路：队列改造后回合跑在 worker 里，SSE 连接只是挂在会话
// 队列上的 sink（可能多条、可随时附着/断开），handler 里那个"当前流的
// writeSSE"不再存在——卡片事件若还走它，Web 会话永远收不到，clarify 工具
// 会因为拿不到通道直接回落成普通结果（表现为"澄清卡片不弹出"）。
func (s *Server) pushSessionCardEvent(sessionID string, payload map[string]interface{}) bool {
	b, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	q := s.lookupSessionQueue(sessionID)
	if q == nil {
		return false
	}
	return q.broadcast(turnEvent{data: "data: " + string(b) + "\n\n"})
}

// cancelWasRequested 报告当前回合是否是被用户显式取消的。
func (q *sessionQueue) cancelWasRequested() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.cancelRequested
}

// sseTurnStarted 是回合开始的广播载荷。前端据此把某条排队消息切换为
// "正在执行"并开始渲染流式内容。
func sseTurnStarted(item *queuedTurn) string {
	b, _ := json.Marshal(map[string]interface{}{
		"type":    "stream_started",
		"started": true,
		"id":      item.id,
		"content": shortenQueuedContent(item.content),
	})
	return "data: " + string(b) + "\n\n"
}

// ============================================================================
// Shared agent-runner context plumbing
// ============================================================================

// turnRunCtx 聚合执行一个回合所需的上下文注入（工作目录、文件变更跟踪）。
// 与改造前一致：这些值由 handleSessionStream 在解析阶段注入，worker 直接使用。
type turnRunCtx struct {
	workDir        string
	workDirUserSet bool
	fileOps        *TurnFileOpTracker
}

// applyTo 把工作目录等上下文值注入回合 ctx。
func (t *turnRunCtx) applyTo(ctx context.Context) context.Context {
	ctx = agent.WithToolOps(ctx, t.fileOps)
	ctx = tool.WithWorkDir(ctx, t.workDir)
	ctx = tool.WithWorkDirUserSet(ctx, t.workDirUserSet)
	return ctx
}

// stripInlineMediaParts 返回一份把 inline base64 载荷替换为轻量引用的
// content parts 副本。队列项会存活到真正被执行（可能几分钟），期间若保留
// data URL，几条带截图的排队消息就能占用几十 MB 内存并随会话落库膨胀——
// 而所有需要字节的地方都在入队前用过了（物化、Office 文本抽取）。
//
// 图片部件换成 "ref:<uploads 路径>" 引用部件，runQueuedTurn 在回合开跑时
// 通过 rehydrateMediaRefs 从磁盘读回字节还原成 image_url 部件——模型必须
// 真正看到图，绝不能只收到一句占位文字（血债：占位文案导致用户粘贴的
// 截图整个排队重构期间模型都看不见）。没有上传引用可回捞的图片（老客户
// 端直接内联、从未上传）保留原载荷：宁可排队时占内存，也不能丢图。
// 返回 nil 表示无需改写（没有 media 部件）。
// imageURLRefs/imageNames 与 parts 里的 image 部件按出现顺序一一对应
// （parseChatPayload 保证），imgIdx 用于维护这份对齐。
func stripInlineMediaParts(parts []types.ContentPart, imageURLRefs, imageNames []string) []types.ContentPart {
	needs := false
	for _, p := range parts {
		if p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:") {
			needs = true
			break
		}
		if p.File != nil && strings.HasPrefix(p.File.Contents, "data:") && len(p.File.Contents) > 64*1024 {
			needs = true
			break
		}
	}
	if !needs {
		return nil
	}

	out := make([]types.ContentPart, 0, len(parts))
	imgIdx := 0
	for _, p := range parts {
		switch {
		case p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:"):
			ref, name := "", ""
			if imgIdx < len(imageURLRefs) {
				ref = imageURLRefs[imgIdx]
			}
			if imgIdx < len(imageNames) {
				name = imageNames[imgIdx]
			}
			imgIdx++
			if ref == "" {
				// 没有上传引用（图片从未落到 uploads 磁盘），无从回捞：
				// 保留原 data URL，保证模型一定能看到图。
				out = append(out, p)
				continue
			}
			// 图片：保留 file 引用的形式，让模型仍知道"有这么一张图"，
			// 同时不必把 base64 一路带进执行阶段。执行前由 rehydrateMediaRefs
			// 还原（见 runQueuedTurn）。
			out = append(out, types.ContentPart{
				Type: "file",
				File: &types.FileInfo{
					Name:     name,
					MimeType: dataURLMime(p.ImageURL.URL),
					URL:      ref,
					Contents: "ref:" + ref,
				},
			})
		case p.File != nil && strings.HasPrefix(p.File.Contents, "data:") && len(p.File.Contents) > 64*1024:
			out = append(out, types.ContentPart{
				Type: "text",
				Text: "[大附件 " + p.File.Name + " 已保存到服务器，读取请使用工作目录中的副本]",
			})
		default:
			out = append(out, p)
		}
	}
	return out
}

// dataURLMime 从 data URL 前缀取 MIME（"data:image/png;base64,..." →
// "image/png"）；解析不出时回退 image/png——调用方只会对 data: 图片调它。
func dataURLMime(dataURL string) string {
	rest := strings.TrimPrefix(dataURL, "data:")
	if i := strings.Index(rest, ";"); i >= 0 {
		if m := rest[:i]; m != "" {
			return m
		}
	}
	return "image/png"
}

// rehydrateMediaRefs 在回合真正开跑前，把 stripInlineMediaParts 留下的
// "ref:<uploads 路径>" 部件还原成带真实字节的 image_url 部件。引用指向
// /api/uploads/<bucket>/<file>，对应磁盘 <magicHome>/uploads/<bucket>/<file>；
// 查找顺序与 parseChatPayload 的附件解析一致（session 桶 → _shared → 根）。
// 读取失败（文件被清理等）时降级为一句明确的文字，回合照常进行。
func (s *Server) rehydrateMediaRefs(parts []types.ContentPart) []types.ContentPart {
	needs := false
	for _, p := range parts {
		if p.File != nil && strings.HasPrefix(p.File.Contents, "ref:") {
			needs = true
			break
		}
	}
	if !needs {
		return parts
	}

	root := s.uploadsRoot()
	out := make([]types.ContentPart, 0, len(parts))
	for _, p := range parts {
		if p.File == nil || !strings.HasPrefix(p.File.Contents, "ref:") {
			out = append(out, p)
			continue
		}
		name := p.File.Name
		data := readUploadRef(root, strings.TrimPrefix(p.File.Contents, "ref:"))
		if len(data) == 0 {
			label := name
			if label == "" {
				label = "未命名图片"
			}
			out = append(out, types.ContentPart{
				Type: "text",
				Text: "[图片附件 " + label + " 的原始文件已丢失（可能被上传清理任务回收），请让用户重新发送]",
			})
			continue
		}
		mime := p.File.MimeType
		if mime == "" || mime == "application/octet-stream" {
			if name != "" {
				if m := mimeFromFilename(name); m != "" && m != "application/octet-stream" {
					mime = m
				}
			}
			if mime == "" || mime == "application/octet-stream" {
				sniffed := http.DetectContentType(data[:min(len(data), 512)])
				if i := strings.Index(sniffed, ";"); i >= 0 {
					sniffed = strings.TrimSpace(sniffed[:i])
				}
				if sniffed != "" {
					mime = sniffed
				}
			}
		}
		if mime == "" {
			mime = "image/png"
		}
		out = append(out, types.ContentPart{
			Type:     "image_url",
			ImageURL: &types.MediaURL{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)},
		})
	}
	return out
}

// readUploadRef 解析 "ref:" 后的引用（/api/uploads/<bucket>/<file>，可能带
// token 查询串），从磁盘读回文件字节。
func readUploadRef(root, ref string) []byte {
	p := resolveUploadLocalPath(root, ref)
	if p == "" {
		return nil
	}
	d, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return d
}

// resolveUploadLocalPath 把 "/api/uploads/<bucket>/<file>" 引用解析成磁盘
// 路径，查找顺序与 parseChatPayload 的附件解析一致（引用桶 → _shared → 根）。
// 所有路径段都过 filepath.Base，杜绝引用里的 .. 逃出 uploads 根；
// 解析不到现有文件时返回 ""。
func resolveUploadLocalPath(root, ref string) string {
	clean := ref
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	clean = strings.TrimPrefix(clean, "/api/uploads/")
	clean = strings.TrimPrefix(clean, "api/uploads/")
	clean = strings.Trim(clean, "/")
	if clean == "" {
		return ""
	}
	segments := strings.Split(filepath.ToSlash(clean), "/")
	file := filepath.Base(segments[len(segments)-1])
	if file == "" || file == "." || file == ".." {
		return ""
	}
	bucket := ""
	if len(segments) >= 2 {
		bucket = filepath.Base(segments[0])
	}
	candidates := []string{}
	if bucket != "" && bucket != "." && bucket != ".." {
		candidates = append(candidates, filepath.Join(root, bucket, file))
	}
	candidates = append(candidates,
		filepath.Join(root, "_shared", file),
		filepath.Join(root, file),
	)
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
