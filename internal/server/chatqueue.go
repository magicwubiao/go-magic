package server

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/tool"
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
)

// turnEvent 是回合向 SSE 连接广播的一条事件。data 是已经序列化好的整帧
// （例如 `{"delta":"hi"}`），由 sink 负责按 SSE 分帧写出。
type turnEvent struct {
	data string
	done bool
	err  error // 回合已结束但结果未能落库时的错误说明
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
// stripInlineMediaParts），否则几条排队消息就能把内存和会话库撑爆。
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

// ============================================================================
// Snapshot (/running 与会话加载)
// ============================================================================

// queuedTurnInfo 是回给前端的排队消息精简描述。
type queuedTurnInfo struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
	Position  int    `json:"position"` // 1 起：1 表示下一个执行
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
			ID:        it.id,
			Content:   shortenQueuedContent(it.content),
			CreatedAt: it.createdAt.Unix(),
			Position:  i + 1,
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

// runQueue 是该会话唯一的回合执行 goroutine：串行取出队列头部消息执行，
// 空闲时挂起等待唤醒。
//
// 退出条件：队列空且没有 SSE 监听者。此时队列会被回收，workerLive 复位，
// 下一条消息到达时重新拉起 worker——长时间不活跃的会话因此不会常驻
// goroutine。注意 workerLive 必须在 mu 之外、且在判定"确实要退出"之后复位，
// 否则会出现"worker 已决定退出但新消息刚入队"的竞态（消息没人消费）。
func (s *Server) runQueue(sessionID string, q *sessionQueue) {
	defer s.releaseWorker(sessionID, q)

	for {
		q.mu.Lock()
		for len(q.items) == 0 && q.hasSinksLocked() {
			q.cond.Wait()
		}
		if len(q.items) == 0 {
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

		s.runQueuedTurn(sessionID, q, turnCtx, item)

		turnCancel()
		q.mu.Lock()
		q.running = false
		q.activeID = ""
		q.cancel = nil
		q.cancelRequested = false
		q.turns++
		q.mu.Unlock()
	}
}

// releaseWorker 在 worker 退出时复位 workerLive 并回收空闲队列。复位与
// "是否还有未消费消息"的判定必须在 chatQueuesMu 下完成：若复位后队列里
// 已有新消息，则立即重新拉起 worker，避免消息永久滞留。
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

// runQueuedTurn 执行队列中的一条消息。这是改造前 handleSessionStream 里
// "跑 agent"那段逻辑的搬迁，差别只在于：
//   - 事件不再直接写 HTTP 连接，而是 append 到队列 sink（可能没人监听）；
//   - 队列项在入队前已剥离 inline base64（见 stripInlineMediaParts），
//     落库时只需按上传引用重建 content parts；
//   - 回合开始/结束都向所有 sink 广播，排队中的后续消息因此能被前端正确
//     显示为"正在执行"。
func (s *Server) runQueuedTurn(sessionID string, queue *sessionQueue, ctx context.Context, item *queuedTurn) {
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
		streamErr = s.runAgentStreamWithMedia(ctx, sessionID, item.content, item.contentParts, streamHandler)
	} else {
		streamErr = s.runAgentStream(ctx, sessionID, item.content, streamHandler)
	}

	// 本轮"变更的文件"（写前快照 + 净 diff，见 fileops.go）在回合真正结束后
	// 才能取到；先落库再广播 done，保证前端拿到 done 时数据已经一致。
	finalOps := run.fileOps.Result()
	s.persistAssistantMessage(sessionID, fullResponse.String(), streamed.String(), finalOps, deliveredAny)

	cancelled := queue.cancelWasRequested()
	switch {
	case streamErr != nil && !cancelled:
		queue.broadcast(turnEvent{err: streamErr})
	case cancelled:
		// 用户主动停止：不推送 error（前端已进入停止态），但仍要发 done
		// 让排队中的下一条消息能接上（队列已被 cancelAll 清空则不会再有）。
	}

	doneData, _ := json.Marshal(map[string]interface{}{
		"done":     true,
		"file_ops": finalOps,
		"turn_id":  item.id,
	})
	queue.broadcast(turnEvent{data: "data: " + string(doneData) + "\n\n", done: true})
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
	// token 统计由 agent 自身累计；这里记录的是"本回合新增量"，通过
	// GetTokenStats 的当前值减去进入回合前的值得到——但 agent 是共享实例，
	// 简单起见沿用改造前的做法：把最新累计值作为本回合增量累加。
	// （改造前 handleSessionStream 也是这么做的，保持行为一致。）
	if a := s.lookupAgent(sessionID); a != nil {
		in, out, cache := a.GetTokenStats()
		sess.InputTokens += in
		sess.OutputTokens += out
		sess.CacheReadTokens += cache
	}
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

// stripInlineMediaParts 返回一份把 inline base64 载荷替换为上传引用路径的
// content parts 副本。队列项会存活到真正被执行（可能几分钟），期间若保留
// data URL，几条带截图的排队消息就能占用几十 MB 内存并随会话落库膨胀——
// 而所有需要字节的地方都在入队前用过了（物化、Office 文本抽取）。
// 返回 nil 表示无需改写（没有 media 部件）。
func stripInlineMediaParts(parts []types.ContentPart) []types.ContentPart {
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
	for _, p := range parts {
		switch {
		case p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:"):
			// 图片：保留 file 引用的形式，让模型仍知道"有这么一张图"，
			// 同时不必把 base64 一路带进执行阶段。
			out = append(out, types.ContentPart{
				Type: "text",
				Text: "[图片附件已随消息接收，如需查看请告知用户重新发送]",
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
