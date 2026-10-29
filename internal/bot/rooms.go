package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/magicwubiao/go-magic/internal/provider"
	sessionstore "github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// RoomAttachment is one file/image attached to a room message.
type RoomAttachment struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Mime string `json:"mime,omitempty"`
}

// RoomMessage is one entry in a group chat's shared history.
type RoomMessage struct {
	ID          string           `json:"id"`
	From        string           `json:"from"` // bot mention tag or "user"
	Content     string           `json:"content"`
	Timestamp   int64            `json:"timestamp"` // Unix seconds
	Attachments []RoomAttachment `json:"attachments,omitempty"`
}

// RoomUploadItem is one uploaded attachment (canonical uploads copy) that a
// room round copies into each member bot's workdir so sandboxed file tools
// can read it.
type RoomUploadItem struct {
	Name string
	Src  string
}

// RoomResult is the outcome of one SendToRoom round.
type RoomResult struct {
	RoomID    string        `json:"room_id"`
	Messages  []RoomMessage `json:"messages"` // full history after the round
	NeedsUser bool          `json:"needs_user"`
}

// roomRequest is a user message queued for a room coordinator.
type roomRequest struct {
	Text   string
	From   string // "user"
	Target string // optional @bot tag to address the first turn at
	// Persisted carries the ref-form attachment parts (file parts with
	// Name/URL/Mime, images included) for this round; nil = text-only.
	Persisted []types.ContentPart
	Items     []RoomUploadItem
	// gen is the roomRuntime.stopGen snapshot taken at enqueue time.
	// StopRoomRound bumps stopGen, and the coordinator drops requests whose
	// gen is stale — otherwise a request queued behind a stopped round would
	// start a NEW round right after the user pressed stop. The check lives in
	// the coordinator itself, so it cannot race the queue the way an external
	// drain would.
	gen   uint64
	Reply chan *RoomResult
}

// roomRuntime is a room's live state: its config plus the coordinator
// channel. One coordinator goroutine per room serializes all rounds.
type roomRuntime struct {
	cfg       *RoomConfig
	triggerCh chan roomRequest
	stopCh    chan struct{}
	// id is the lowercased room ID, fixed at construction. rt.cfg is replaced
	// wholesale by a metadata-only UpdateRoom (under m.mu), so anything read
	// outside the lock must use id rather than rt.cfg.ID to stay race-free.
	id string
	// closed is set (before stopCh is closed) by DeleteRoom / a hot reload.
	// runRoomRound checks it before every append: a turn that outlived the
	// teardown grace period must not recreate the room log DeleteRoom just
	// removed (the 3s unwind wait is best-effort against a wedged provider).
	closed atomic.Bool
	// roundRunning 为 true 表示协调器正在跑一轮（成员逐个发言）。房间回合
	// 合法耗时可达数分钟，前端靠它判断"还该不该继续轮询房间日志"，而不是
	// 傻等那个阻塞的 POST。
	roundRunning atomic.Bool
	// roundCtx 只在一轮进行中非 nil（m.mu 保护）。StopRoomRound 取消它：
	// 协调器跳过剩余成员/轮次，正在发言的成员回合（runCtx 的父）也一并
	// 被取消，而不是放着把整个回合预算烧完。
	roundCtx    context.Context
	roundCancel context.CancelFunc
	// stopGen 每次 StopRoomRound 自增（m.mu 保护）。入队请求记下当时的
	// stopGen，协调器只跑 gen 仍然新鲜的请求 —— 这是"停止"对排队消息
	// 生效的唯一无竞态写法。
	stopGen uint64
}

// roomRoundBudget bounds one whole coordinated round (all members, all
// rounds). Per-member turns are capped by bot_mode.turn_timeout_minutes, but
// nothing bounded their sum: 6 members × 3 rounds could hold a room for hours.
// Members keep their own budget (whichever fires first
// wins); the round budget exists so a stuck provider cannot pin the room
// forever after the user walked away.
const roomRoundBudget = 30 * time.Minute

// NewRoomID generates a unique room identifier.
func NewRoomID() string {
	return "room_" + uuid.New().String()[:8]
}

// --- Manager room API ---

// CreateRoom persists a new group chat room and starts its coordinator.
func (m *Manager) CreateRoom(cfg *RoomConfig) error {
	if cfg == nil {
		return errors.New("room config is required")
	}
	if cfg.ID == "" {
		cfg.ID = NewRoomID()
	}
	cfg.ID = strings.ToLower(cfg.ID)
	if cfg.Name == "" {
		cfg.Name = cfg.ID
	}
	if len(cfg.Members) < MinRoomMembers {
		return fmt.Errorf("a room needs at least %d members", MinRoomMembers)
	}
	if len(cfg.Members) > MaxRoomMembers {
		return fmt.Errorf("a room supports at most %d members", MaxRoomMembers)
	}
	// Validate members exist and de-duplicate.
	seen := map[string]bool{}
	for i, name := range cfg.Members {
		key := strings.ToLower(name)
		if m.FindByTag(key) == nil {
			return fmt.Errorf("room member %q is not a known bot", name)
		}
		if seen[key] {
			return fmt.Errorf("duplicate room member %q", name)
		}
		seen[key] = true
		cfg.Members[i] = key
	}
	// Persist the clamped values so a hand-written or API-supplied extreme
	// (max_rounds=1000) cannot stay on disk as a landmine.
	cfg.MaxRounds = cfg.Rounds()
	cfg.HistoryCap = cfg.History()
	now := time.Now().Unix()
	cfg.CreatedAt = now
	cfg.UpdatedAt = now
	m.roomMu.Lock()
	if err := m.store.SaveRoom(cfg); err != nil {
		m.roomMu.Unlock()
		return fmt.Errorf("failed to save room: %w", err)
	}
	m.roomMu.Unlock()
	m.startRoomLocked(cfg)
	log.Infof("[BotMode] Room %q created with %d members", cfg.Name, len(cfg.Members))
	return nil
}

// UpdateRoom applies partial updates to a room.
//
// A hot reload (tear down the coordinator, start a fresh one) is only needed
// when the MEMBER LIST changed: that list is what a running round iterates
// over. Renaming a room or editing its topic used to kill the in-flight round
// and orphan every queued request — the update now patches the live config in
// place instead.
func (m *Manager) UpdateRoom(id string, mutate func(*RoomConfig)) (*RoomConfig, error) {
	id = strings.ToLower(id)
	// Serialize the read-modify-write: two concurrent PATCHes each loaded the
	// on-disk snapshot and saved it back, silently dropping one update.
	m.roomMu.Lock()
	defer m.roomMu.Unlock()

	cfg, err := m.store.LoadRoom(id)
	if err != nil {
		return nil, fmt.Errorf("room not found: %s", id)
	}
	oldMembers := append([]string(nil), cfg.Members...)
	mutate(cfg)
	cfg.ID = id
	if len(cfg.Members) < MinRoomMembers {
		return nil, fmt.Errorf("a room needs at least %d members", MinRoomMembers)
	}
	if len(cfg.Members) > MaxRoomMembers {
		return nil, fmt.Errorf("a room supports at most %d members", MaxRoomMembers)
	}
	seen := map[string]bool{}
	for i, name := range cfg.Members {
		key := strings.ToLower(name)
		// Normalize even members whose bot was deleted: the room must stay
		// editable so the dead member can be removed by a later update, and a
		// mixed-case leftover used to slip past the duplicate check.
		cfg.Members[i] = key
		if m.FindByTag(key) == nil {
			continue
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate room member %q", name)
		}
		seen[key] = true
	}
	cfg.MaxRounds = cfg.Rounds()
	cfg.HistoryCap = cfg.History()
	if err := m.store.SaveRoom(cfg); err != nil {
		return nil, fmt.Errorf("failed to save room: %w", err)
	}

	if !sameMembers(oldMembers, cfg.Members) {
		// Hot-reload: stop the old coordinator and start a fresh one so member
		// changes take effect immediately. In-flight rounds are cancelled and
		// queued senders are answered with the current history.
		m.mu.Lock()
		var dropped []roomRequest
		rt, ok := m.rooms[id]
		if ok {
			dropped = m.teardownRoomLocked(rt)
			delete(m.rooms, id)
		}
		m.mu.Unlock()
		m.answerDroppedRequests(rt, dropped)
		m.startRoomLocked(cfg)
		return cfg, nil
	}

	// Metadata-only update: swap the config the live coordinator reads.
	m.mu.Lock()
	if rt, ok := m.rooms[id]; ok && rt != nil {
		rt.cfg = cfg
	}
	m.mu.Unlock()
	return cfg, nil
}

// sameMembers reports whether two member lists are identical (order matters:
// it is also the speaking order of a round).
func sameMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DeleteRoom removes a room and stops its coordinator.
func (m *Manager) DeleteRoom(id string) error {
	id = strings.ToLower(id)
	// roomMu serializes the load->delete cycle against UpdateRoom: without it a
	// concurrent PATCH that loaded the config before this delete could save it
	// back afterwards and resurrect the room. The unwind wait below is kept
	// OUTSIDE roomMu so a wedged provider cannot block other room edits.
	m.roomMu.Lock()
	cfg, err := m.store.LoadRoom(id)
	if err != nil {
		m.roomMu.Unlock()
		return fmt.Errorf("room not found: %s", id)
	}
	m.mu.Lock()
	var stopped *roomRuntime
	var dropped []roomRequest
	if rt, ok := m.rooms[id]; ok {
		stopped = rt
		dropped = m.teardownRoomLocked(rt)
		delete(m.rooms, id)
	}
	m.mu.Unlock()
	if err := m.store.DeleteRoom(cfg.ID); err != nil {
		m.roomMu.Unlock()
		return fmt.Errorf("failed to delete room: %w", err)
	}
	m.roomMu.Unlock()
	m.answerDroppedRequests(stopped, dropped)

	// Let the cancelled round unwind before the session rows go away: its last
	// member turn may still append a message, which would recreate the room log
	// just below. Cancellation is already in flight, so this is usually
	// milliseconds; the deadline only guards against a wedged provider call —
	// and rt.closed (set above) makes any straggler's append a no-op.
	if stopped != nil {
		deadline := time.Now().Add(3 * time.Second)
		for stopped.roundRunning.Load() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
	}
	// Drop persisted room sessions for all members + the shared room log so
	// deleted rooms don't leave stale history in bots.db.
	m.mu.Lock()
	for _, member := range cfg.Members {
		if rt, ok := m.bots[member]; ok && rt != nil {
			if rt.roomAgents != nil {
				delete(rt.roomAgents, strings.ToLower(id))
			}
		}
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, member := range cfg.Members {
		if err := m.sessions.DeleteSession(ctx, RoomSessionID(member, cfg.ID)); err != nil {
			log.Warnf("[BotMode] Failed to delete room session for %s: %v", member, err)
		}
	}
	if err := m.sessions.DeleteSession(ctx, roomHistorySessionID(cfg.ID)); err != nil {
		log.Warnf("[BotMode] Failed to delete room log %s: %v", cfg.ID, err)
	}
	log.Infof("[BotMode] Room %q deleted", cfg.Name)
	return nil
}

// ListRooms returns all room configs with live member status.
// Members whose bot no longer exists are filtered from the returned copy so
// the dashboard never shows ghost chips for deleted bots (the on-disk file is
// cleaned up by DeleteBot's purge; this covers rooms written before that fix).
func (m *Manager) ListRooms() ([]*RoomConfig, error) {
	rooms, err := m.store.ListRooms()
	if err != nil {
		return nil, err
	}
	for _, room := range rooms {
		live := make([]string, 0, len(room.Members))
		for _, mem := range room.Members {
			if m.FindByTag(mem) != nil {
				live = append(live, mem)
			}
		}
		room.Members = live
	}
	return rooms, nil
}

// GetRoom loads one room config.
func (m *Manager) GetRoom(id string) (*RoomConfig, error) {
	return m.store.LoadRoom(strings.ToLower(id))
}

// RoomRoundRunning 报告该房间此刻是否有回合在进行（含已排队尚未开跑的）。
// 供 Web 端判断是否继续轮询房间日志：一轮群聊会跑几分钟，界面不能只靠
// 那个阻塞的 POST 来知道"还在进行"。
func (m *Manager) RoomRoundRunning(roomID string) bool {
	key := strings.ToLower(roomID)
	m.mu.Lock()
	rt, ok := m.rooms[key]
	m.mu.Unlock()
	if !ok || rt == nil {
		return false
	}
	// 排队的请求同样算"进行中"：协调器逐个消费，中途会出现
	// roundRunning=false 的瞬间，只看它会误报"已结束"。
	return rt.roundRunning.Load() || len(rt.triggerCh) > 0
}

// RoomMessages returns the shared message history of a room (most recent
// first, capped at the room's persisted-history cap — NOT at the prompt
// window, so tuning MaxMessages cannot erase what the user already said).
func (m *Manager) RoomMessages(roomID string) ([]RoomMessage, error) {
	roomID = strings.ToLower(roomID)
	cfg, err := m.store.LoadRoom(roomID)
	if err != nil {
		return nil, fmt.Errorf("room not found: %s", roomID)
	}
	msgs, err := m.loadRoomHistory(roomID)
	if err != nil {
		return nil, err
	}
	cap := cfg.History()
	if len(msgs) > cap {
		msgs = msgs[len(msgs)-cap:]
	}
	return msgs, nil
}

// SendToRoom delivers a user message to a group chat and blocks until the
// coordinated multi-bot round finishes (all members had a chance to speak,
// up to MaxRounds). target, when non-empty, is a bot mention tag that gets
// the first word. Returns the full room history and whether a bot escalated
// to @user.
func (m *Manager) SendToRoom(ctx context.Context, roomID, text, target string) (*RoomResult, error) {
	return m.SendToRoomWithMedia(ctx, roomID, text, nil, nil, target)
}

// SendToRoomWithMedia is SendToRoom for multimodal input: persisted carries
// the ref-form attachment parts (file parts with Name/URL/Mime — images
// included) that every member turn receives, and items lists the canonical
// uploads copies to materialize into each member bot's workdir.
func (m *Manager) SendToRoomWithMedia(ctx context.Context, roomID, text string, persisted []types.ContentPart, items []RoomUploadItem, target string) (*RoomResult, error) {
	key := strings.ToLower(roomID)
	if target != "" {
		target = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(target), "@"))
	}

	req := roomRequest{
		Text:      text,
		From:      "user",
		Target:    target,
		Persisted: persisted,
		Items:     items,
		Reply:     make(chan *RoomResult, 1),
	}
	// Snapshot stopGen and enqueue under the same lock: taking the snapshot
	// outside (the old code) left a window where a concurrent StopRoomRound
	// bumped the generation between snapshot and send, and a brand-new user
	// message was mistaken for a stale queued one and silently dropped.
	//
	// Everything touched here must be the *Locked variant: m.mu is not
	// reentrant, and calling FindByTag here deadlocked the whole manager.
	m.mu.Lock()
	rt, ok := m.rooms[key]
	if !ok || rt == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("room not found: %s", roomID)
	}
	unknownTarget := target != "" && target != "user" && m.findByTagLocked(target) == nil
	enqueued := false
	if !unknownTarget {
		req.gen = rt.stopGen
		select {
		case rt.triggerCh <- req:
			enqueued = true
		default:
		}
	}
	m.mu.Unlock()
	if unknownTarget {
		return nil, fmt.Errorf("unknown bot %q; available: %s", target, m.TagList())
	}

	if !enqueued {
		// Queue is full (16 in flight): block for a slot, but stay abortable.
		// Without the ctx case a disconnected client left the handler parked
		// here with no way out until the coordinator caught up.
		select {
		case rt.triggerCh <- req:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.stopCh:
			return nil, errors.New("bot manager shutting down")
		}
	}

	select {
	case res := <-req.Reply:
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.stopCh:
		return nil, errors.New("bot manager shutting down")
	}
}

// --- Coordinator ---

// startRoomLocked brings a room online (idempotent). Safe before Start().
// No-op once the manager is stopping (a wg.Add after Stop's wg.Wait is a race).
func (m *Manager) startRoomLocked(cfg *RoomConfig) {
	key := strings.ToLower(cfg.ID)
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		log.Debugf("[BotMode] Ignoring start of room %q: manager is stopping", cfg.Name)
		return
	}
	if _, exists := m.rooms[key]; exists {
		m.mu.Unlock()
		return
	}
	rt := &roomRuntime{
		cfg:       cfg,
		id:        key,
		triggerCh: make(chan roomRequest, 16),
		stopCh:    make(chan struct{}),
	}
	m.rooms[key] = rt
	// Add while mu is held so it can't race Stop()'s wg.Wait().
	m.wg.Add(1)
	go m.roomLoop(rt)
	m.mu.Unlock()

	log.Infof("[BotMode] Room %q is online", cfg.Name)
}

// roomLoop is one room's coordinator: it serializes user messages and runs
// the multi-round speaking sequence for each.
func (m *Manager) roomLoop(rt *roomRuntime) {
	defer m.wg.Done()
	for {
		select {
		case <-m.stopCh:
			return
		case <-rt.stopCh:
			return
		case req := <-rt.triggerCh:
			if m.roomRequestStale(rt, req) {
				// Stopped while this request sat in the queue: answer the
				// sender with the current history instead of starting a round
				// the user just asked to stop.
				m.dropRoomRequest(rt, req)
				continue
			}
			rt.roundRunning.Store(true)
			m.runRoomRound(rt, req)
			rt.roundRunning.Store(false)
		}
	}
}

// roomRequestStale reports whether req was queued before the most recent
// StopRoomRound (see roomRequest.gen).
func (m *Manager) roomRequestStale(rt *roomRuntime, req roomRequest) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return req.gen != rt.stopGen
}

// dropRoomRequest answers a stale queued request with the current history so
// its sender does not wait out a round that will never run. The user message
// was never logged (appendRoomMessage runs at round start), so the UI keeps
// showing the optimistic bubble with no reply attached.
func (m *Manager) dropRoomRequest(rt *roomRuntime, req roomRequest) {
	history, _ := m.loadRoomHistory(rt.id)
	res := &RoomResult{RoomID: rt.id, Messages: history}
	select {
	case req.Reply <- res:
	default:
	}
}

// runRoomRound executes the full speaking sequence for one user message:
// members take turns (target first), up to MaxRounds rounds; each bot speaks
// at most once per round; the round stops early when a bot escalates to
// @user or when nobody has anything to add.
//
// It also aborts as soon as the room is torn down (UpdateRoom hot-reload or
// DeleteRoom), the manager shuts down, or the user stops the round
// (StopRoomRound): without that check a removed room kept delivering turns to
// its old members for the rest of the round, and a stopped round kept burning
// turn after turn.
func (m *Manager) runRoomRound(rt *roomRuntime, req roomRequest) {
	// The coordinator may have been torn down (DeleteRoom / UpdateRoom hot
	// reload) between dequeuing this request and starting the round. Append
	// writes below recreate the room log that DeleteRoom just deleted, so bail
	// out before touching the store.
	if m.roomClosed(rt) {
		m.dropRoomRequest(rt, req)
		return
	}

	roundCtx, cancelRound := context.WithTimeout(context.Background(), roomRoundBudget)
	m.mu.Lock()
	room := rt.cfg
	rt.roundCtx = roundCtx
	rt.roundCancel = cancelRound
	m.mu.Unlock()
	defer func() {
		// Cancel first so in-flight member turns (whose cancelCh rides on
		// roundCtx) wind down even if we are exiting via an early return.
		cancelRound()
		m.mu.Lock()
		if rt.roundCtx == roundCtx {
			rt.roundCtx = nil
			rt.roundCancel = nil
		}
		m.mu.Unlock()
	}()

	// Keep the room log in memory for the whole round. Every append used to
	// re-read the whole log from SQLite and rewrite it (on top of the prompt
	// build doing its own read), i.e. two full round-trips per single message.
	// One read + in-memory appends gives every member exactly the same context
	// with a fraction of the I/O.
	logHistory, err := m.loadRoomHistory(room.ID)
	if err != nil {
		// Don't fail the round: the persisted log is best-effort context, and a
		// transient read error must not silently produce an empty prompt.
		log.Warnf("[BotMode] Room %s: failed to load log at round start: %v", room.Name, err)
	}
	// Persist the human's message into the room log (with attachment refs so
	// reloads still show what was shared).
	logHistory, _ = m.appendRoomMessageTo(rt, room, logHistory, "user", req.Text, roomAttachmentsFromParts(req.Persisted))

	members := room.Members
	if req.Target != "" && req.Target != "user" {
		if tc := m.FindByTag(req.Target); tc != nil {
			// Move the targeted member to the front of this round.
			members = prependMember(members, strings.ToLower(tc.Name))
		}
	}
	// Skip bots that are offline before the round starts. A paused member used
	// to be enqueued anyway and dropped by its own worker, which wrote
	// "(no reply: bot X is paused)" into the room log once per member per round
	// — noise the human sees and every later prompt re-reads, for something
	// that is not a failure at all.
	speaking := make([]string, 0, len(members))
	for _, mem := range members {
		if mc := m.FindByTag(mem); mc != nil && mc.IsActive() {
			speaking = append(speaking, mem)
		}
	}
	members = speaking
	if len(members) == 0 {
		logHistory, _ = m.appendRoomMessageTo(rt, room, logHistory, "@system",
			"没有在线成员，本轮没有开始（成员被暂停或不在了）。", nil)
		res := &RoomResult{RoomID: room.ID, Messages: logHistory}
		select {
		case req.Reply <- res:
		default:
		}
		return
	}

	maxRounds := room.Rounds()
	needsUser := false

	for round := 0; round < maxRounds; round++ {
		if m.roomClosed(rt) || m.roundStopped(rt) {
			break
		}
		// 活图只跟首轮投递：req.Persisted 会被每一个成员、每一轮重复使用，
		// 一次用户发图会变成 4 成员 × 3 轮 = 12 次投递，每投一次成员就重新
		// 识别一次（单次识别十几秒到几分钟，这正是群聊"卡住"的主因之一）。
		// 第二轮起成员不再拿到实时字节：房间日志里有附件记录，工作目录里的
		// 副本也还在，需要时可以用文件工具重新读取。
		turnReq := req
		if round > 0 {
			turnReq.Persisted = nil
			turnReq.Items = nil
		}
		anySpoke := false
		for _, member := range members {
			if m.roomClosed(rt) || m.roundStopped(rt) {
				break
			}
			// Skip members that were removed or paused mid-round.
			if mc := m.FindByTag(member); mc == nil || !mc.IsActive() {
				continue
			}
			prompt := m.buildRoomPrompt(room, member, logHistory, round, maxRounds)
			reply, err := m.sendRoomTurn(roundCtx, rt, member, prompt, turnReq)
			if err != nil {
				// Room torn down mid-turn or round stopped by the user: stop
				// silently. Appending the error here would recreate the room
				// log DeleteRoom just removed, and a stopped round has no
				// business leaving "(no reply: context canceled)" spam behind.
				if m.roomClosed(rt) || m.roundStopped(rt) {
					break
				}
				log.Warnf("[BotMode] Room %s member %s turn failed: %v", room.Name, member, err)
				// A round-budget expiry is a coordinated stop, not a member
				// failure: note it once below instead of writing a fake reply.
				if errors.Is(err, context.DeadlineExceeded) {
					break
				}
				// Member deleted or paused while its turn was queued: nothing
				// failed, and an "(no reply: bot X is paused)" row would only
				// teach the next prompt that the room is broken.
				if mc := m.FindByTag(member); mc == nil || !mc.IsActive() {
					continue
				}
				logHistory, _ = m.appendRoomMessageTo(rt, room, logHistory, member, "(no reply: "+err.Error()+")", nil)
				continue
			}
			// 房间日志是广播面：人和其他成员都会读它，而 buildRoomPrompt
			// 又会把整份日志灌进每个成员的下一轮提示词。模型内部的
			// <think> 独白绝不能进这里——实测它让每个成员的提示词多背
			// ~12KB 他人内心独白，还带着"视觉不可用，去装 OCR"这类已经
			// 过期的结论，下一轮成员照着重做一遍。
			reply = broadcastReplyText(reply)
			if reply == "" {
				continue
			}
			anySpoke = true
			logHistory, _ = m.appendRoomMessageTo(rt, room, logHistory, member, reply, nil)
			if needsHuman(reply) {
				needsUser = true
				log.Infof("[BotMode] Room %s: @%s escalated to user", room.Name, member)
				break
			}
		}
		if needsUser || !anySpoke {
			break
		}
	}

	// The round budget (not the user) ended the round: say so in the log so the
	// silence is explainable instead of looking like every bot went mute.
	if !needsUser && !m.roomClosed(rt) && errors.Is(roundCtx.Err(), context.DeadlineExceeded) {
		logHistory, _ = m.appendRoomMessageTo(rt, room, logHistory, "@system",
			"本轮已达到群聊时间上限，剩余发言被跳过。", nil)
	}

	// logHistory already holds every message this round wrote (appends are
	// kept in sync above), so answering the caller needs no extra disk read.
	res := &RoomResult{RoomID: room.ID, Messages: logHistory, NeedsUser: needsUser}
	select {
	case req.Reply <- res:
	default:
	}
}

// roomClosed reports whether this room's coordinator was torn down (room
// deleted or hot-reloaded) or the whole manager is shutting down.
func (m *Manager) roomClosed(rt *roomRuntime) bool {
	select {
	case <-rt.stopCh:
		return true
	case <-m.stopCh:
		return true
	default:
		return false
	}
}

// roundStopped reports whether the current round was aborted via
// StopRoomRound. Distinct from roomClosed so the round can exit quietly
// (no "(no reply: ...)" noise) while the room itself stays alive.
func (m *Manager) roundStopped(rt *roomRuntime) bool {
	m.mu.Lock()
	ctx := rt.roundCtx
	m.mu.Unlock()
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// cancelRoomRoundLocked cancels the room's in-flight round, if any. Caller must
// hold m.mu.
//
// Closing stopCh alone only makes the coordinator skip the NEXT member: the
// member turn currently in flight is a child of roundCtx, not of stopCh, so it
// kept running for up to the whole turn budget. A room that was hot-reloaded or
// deleted therefore left a turn burning tokens for minutes against a
// configuration that no longer existed. Cancelling roundCtx ends it now, and
// roundStopped() keeps the exit quiet (no "(no reply …)" row in a log that is
// about to be dropped anyway).
func cancelRoomRoundLocked(rt *roomRuntime) {
	if rt == nil || rt.roundCancel == nil {
		return
	}
	cancel := rt.roundCancel
	rt.roundCancel = nil
	cancel()
}

// teardownRoomLocked takes a coordinator offline and hands back the requests
// still sitting in its queue. Caller must hold m.mu.
//
// Closing stopCh makes roomLoop return, but queued senders are waiting on
// their own replyCh — nobody would ever write it, so the HTTP handler hung
// until the client aborted (a room rename/delete with two messages in flight
// leaked one handler per queued message). The caller answers them outside the
// lock via answerDroppedRequests.
func (m *Manager) teardownRoomLocked(rt *roomRuntime) []roomRequest {
	if rt == nil {
		return nil
	}
	// Mark closed BEFORE stopping: a member turn that outlives the teardown
	// must not append to (and recreate) a room log that is being deleted.
	rt.closed.Store(true)
	cancelRoomRoundLocked(rt)
	close(rt.stopCh)
	var dropped []roomRequest
	for {
		select {
		case req := <-rt.triggerCh:
			dropped = append(dropped, req)
		default:
			return dropped
		}
	}
}

// answerDroppedRequests replies to senders whose round will never run. Must be
// called WITHOUT m.mu (it reads the room log from disk).
func (m *Manager) answerDroppedRequests(rt *roomRuntime, reqs []roomRequest) {
	if rt == nil || len(reqs) == 0 {
		return
	}
	history, _ := m.loadRoomHistory(rt.id)
	for _, req := range reqs {
		select {
		case req.Reply <- &RoomResult{RoomID: rt.id, Messages: history}:
		default:
		}
	}
}

// StopRoomRound aborts the room's in-flight round so nothing keeps running
// after the user asked to stop: the in-flight member turn is canceled (not
// merely abandoned), remaining members and rounds are skipped, and queued
// requests are dropped by the coordinator via the stopGen check. Every
// waiting sender gets the partial history back.
//
// Queued requests are invalidated even when no round is currently executing:
// RoomRoundRunning reports "running" for a non-empty queue too, so a stop
// pressed between two rounds used to return false and let the queued round
// start anyway — the exact "群聊停不下来" report. Returns true when something
// was actually stopped; an idle room with an empty queue is a no-op (false).
func (m *Manager) StopRoomRound(roomID string) bool {
	key := strings.ToLower(roomID)
	m.mu.Lock()
	rt, ok := m.rooms[key]
	if !ok || rt == nil {
		m.mu.Unlock()
		return false
	}
	cancel := rt.roundCancel
	rt.roundCancel = nil
	queued := len(rt.triggerCh) > 0
	if cancel == nil && !queued {
		m.mu.Unlock()
		return false
	}
	// Bump unconditionally: this is what invalidates requests already sitting
	// in the queue (their gen no longer matches, so the coordinator answers
	// them with the current history instead of starting a new round).
	rt.stopGen++
	name := rt.cfg.Name
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	log.Infof("[BotMode] Room %s round stopped by user", name)
	return true
}

// sendRoomTurn enqueues one room turn to a member bot and waits for its
// worker to finish (serialized with the bot's other chats). The wait also
// ends when the room is closed, so a hot-reloaded room releases its
// coordinator immediately instead of after the slowest member replies.
//
// roundCtx ties the member turn to this round's lifetime: StopRoomRound
// cancels the round, which cancels the in-flight member turn via
// pendingMessage.cancelCh instead of leaving it running in the background.
//
// When the round carries attachments (req.Persisted non-empty), the prompt
// rides as the leading text part and the attachment refs ride alongside —
// the provider conversion sends only parts in that case. Each member gets
// the canonical uploads copies materialized into its own workdir so
// sandboxed file tools can read them; images additionally rehydrate into
// real bytes at turn start (same path as single-bot chat).
func (m *Manager) sendRoomTurn(roundCtx context.Context, rt *roomRuntime, botName, text string, req roomRequest) (string, error) {
	msg := pendingMessage{
		From:   "room:" + rt.id,
		RoomID: rt.id,
		// persistedParts 是落库形态：回合开始时 ContentParts 里的图片引用会被
		// 还原成带字节的 base64 部件送给模型，这份轻量副本让 manager 在写盘前
		// 把该条消息换回上传引用（与单聊 SendToBotWithMedia 同一语义）。少了它，
		// base64 会进 bots.db 并留在 agent 内存历史里，被之后每一轮重放。
		persistedParts: req.Persisted,
	}
	// 取消联动：回合被停止/房间被删除时，这条成员回合（无论在跑还是仍在
	// 排队）都会随 roundCtx 一起被取消，而不是把预算烧完。
	if roundCtx != nil {
		msg.cancelCh = roundCtx.Done()
	}
	if len(req.Persisted) > 0 {
		parts := append([]types.ContentPart{{Type: "text", Text: text}}, req.Persisted...)
		if summary := m.materializeRoomUploads(req.Items, botName); summary != "" {
			parts = append(parts, types.ContentPart{Type: "text", Text: summary})
		}
		msg.ContentParts = parts
	} else {
		msg.Text = text
	}
	msg.replyCh = make(chan turnResult, 1)
	if err := m.EnqueueMsg(botName, msg); err != nil {
		return "", err
	}
	// roundDone keeps the coordinator from waiting on a reply it no longer
	// needs: after StopRoomRound (or a room hot-reload) the member turn may
	// still sit in the bot's queue behind other work, and pop only when the
	// worker gets to it. The round is over the moment the context is cancelled.
	var roundDone <-chan struct{}
	if roundCtx != nil {
		roundDone = roundCtx.Done()
	}
	select {
	case res := <-msg.replyCh:
		return res.Reply, res.Err
	case <-roundDone:
		return "", roundCtx.Err()
	case <-rt.stopCh:
		return "", errors.New("room closed")
	case <-m.stopCh:
		return "", errors.New("bot manager shutting down")
	}
}

// buildRoomPrompt assembles the per-member turn prompt with shared history
// (last MaxMessages entries) and the fleet roster.
func (m *Manager) buildRoomPrompt(room *RoomConfig, member string, history []RoomMessage, round, maxRounds int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Group chat room: %s]\n", room.Name)
	if room.Topic != "" {
		fmt.Fprintf(&sb, "Topic: %s\n", room.Topic)
	}
	sb.WriteString("Members: ")
	parts := make([]string, 0, len(room.Members))
	for _, mem := range room.Members {
		// Deleted bots must not reach the model: it would address a ghost
		// member, and the ghost's "turn" silently produces nothing.
		mc := m.FindByTag(mem)
		if mc == nil {
			continue
		}
		entry := "@" + mem
		if mc.Title != "" {
			entry += " (" + mc.Title + ")"
		}
		parts = append(parts, entry)
	}
	sb.WriteString(strings.Join(parts, ", "))
	sb.WriteString("\n\nRecent room messages:\n")
	capN := room.MessagesCap()
	if len(history) > capN {
		history = history[len(history)-capN:]
	}
	if len(history) == 0 {
		sb.WriteString("(no messages yet)\n")
	}
	for _, msg := range history {
		if len(msg.Attachments) > 0 {
			names := make([]string, 0, len(msg.Attachments))
			for _, a := range msg.Attachments {
				names = append(names, a.Name)
			}
			fmt.Fprintf(&sb, "- @%s: %s [附件: %s]\n", msg.From, msg.Content, strings.Join(names, ", "))
			continue
		}
		fmt.Fprintf(&sb, "- @%s: %s\n", msg.From, msg.Content)
	}
	fmt.Fprintf(&sb, "\nYour turn (round %d/%d). You are @%s.\n", round+1, maxRounds, member)
	sb.WriteString("Reply to the room conversation directly. Address @<member> when your message is meant for a specific bot. ")
	sb.WriteString("If the task needs the human's input or approval, start your reply with @user to stop the round and escalate.")
	return sb.String()
}

// newRoomMessage builds one log entry.
func newRoomMessage(from, content string, atts []RoomAttachment) RoomMessage {
	return RoomMessage{
		ID:          "m_" + uuid.New().String()[:8],
		From:        from,
		Content:     content,
		Timestamp:   time.Now().Unix(),
		Attachments: atts,
	}
}

// appendRoomMessage persists one message into the room's shared session log
// and keeps only the last History() entries. It returns the entry it wrote.
//
// Self-loading form, kept for callers that hold no in-memory log (tests, CLI
// helpers). A round uses appendRoomMessageTo instead — re-reading the whole
// log for every single message was pure overhead.
func (m *Manager) appendRoomMessage(room *RoomConfig, from, content string, atts []RoomAttachment) RoomMessage {
	msgs, err := m.loadRoomHistory(room.ID)
	if err != nil {
		// A broken read must not lose the message: start a fresh log rather
		// than appending to nil silently (saveRoomHistory keeps it durable).
		log.Warnf("[BotMode] Room %s: failed to load log before append: %v", room.Name, err)
		msgs = nil
	}
	entry := newRoomMessage(from, content, atts)
	msgs = capRoomHistory(append(msgs, entry), room.History())
	m.saveRoomHistory(room.ID, msgs)
	return entry
}

// appendRoomMessageTo appends to the log a round already holds in memory and
// writes the capped result once. It returns the updated log and the new entry.
//
// It refuses to write once the coordinator has been torn down: DeleteRoom only
// waits ~3s for the cancelled round to unwind, so a member turn stuck in a
// wedged provider call could otherwise append afterwards and recreate the room
// log the delete had just removed.
func (m *Manager) appendRoomMessageTo(rt *roomRuntime, room *RoomConfig, history []RoomMessage, from, content string, atts []RoomAttachment) ([]RoomMessage, RoomMessage) {
	entry := newRoomMessage(from, content, atts)
	out := capRoomHistory(append(history, entry), room.History())
	if rt != nil && rt.closed.Load() {
		return out, entry // in-memory only; the room (and its log) is gone
	}
	m.saveRoomHistory(room.ID, out)
	return out, entry
}

// capRoomHistory trims an in-memory room log to the same cap the persisted log
// uses, so a round that keeps the log in memory never hands a member more (or
// less) context than a fresh disk read would.
func capRoomHistory(history []RoomMessage, capN int) []RoomMessage {
	if capN <= 0 || len(history) <= capN {
		return history
	}
	return history[len(history)-capN:]
}

// roomHistorySessionID is the shared session key for a room's message log.
func roomHistorySessionID(roomID string) string {
	return "bot:room:" + strings.ToLower(roomID)
}

// loadRoomHistory reads a room's shared message log from the session store.
func (m *Manager) loadRoomHistory(roomID string) ([]RoomMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := m.sessions.LoadSession(ctx, roomHistorySessionID(roomID))
	if err != nil || sess == nil {
		return nil, nil
	}
	out := make([]RoomMessage, 0, len(sess.Messages))
	for _, msg := range sess.Messages {
		out = append(out, RoomMessage{
			ID:          msg.ID,
			From:        msg.From,
			Content:     msg.Content,
			Timestamp:   msg.Timestamp.Unix(),
			Attachments: roomAttachmentsFromParts(msg.ContentParts),
		})
	}
	return out, nil
}

// saveRoomHistory writes a room's shared message log to the session store.
func (m *Manager) saveRoomHistory(roomID string, msgs []RoomMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess := &sessionstore.Session{
		ID:       roomHistorySessionID(roomID),
		Name:     "Room Log: " + roomID,
		Profile:  roomID,
		Platform: "bot-room",
		Messages: make([]types.Message, 0, len(msgs)),
	}
	for _, msg := range msgs {
		t := time.Unix(msg.Timestamp, 0)
		sess.Messages = append(sess.Messages, types.Message{
			ID:        msg.ID,
			Role:      "user", // room log is informational; role not meaningful
			From:      msg.From,
			Content:   msg.Content,
			Timestamp: t,
			// Attachments ride as ref-form file parts so the session store
			// schema stays untouched (same encoding bot chat history uses).
			ContentParts: roomPartsFromAttachments(msg.Attachments),
		})
	}
	if err := m.sessions.SaveSession(ctx, sess); err != nil {
		log.Warnf("[BotMode] Failed to persist room %s log: %v", roomID, err)
	}
}

// broadcastReplyText prepares one member's reply for the shared room log.
//
// The room log is a broadcast surface: humans read it in the dashboard, and
// buildRoomPrompt pastes the whole thing into every member's next prompt. The
// model's internal <think> trail therefore must not ride along — it bloats
// every member's prompt (~12KB of other bots' monologue per turn in practice),
// pushes members to imitate each other's deliberation, and carries stale
// conclusions ("vision is unavailable, install OCR") that the next member then
// acts on. Zero-width placeholders left by an empty reply are stripped as
// well, so a reply that was nothing but thinking collapses to "" and the
// caller skips it instead of broadcasting a blank bubble.
func broadcastReplyText(reply string) string {
	return provider.SanitizeAssistantContent(provider.StripThinkTrails(reply))
}

// needsHuman detects an escalation request: the member prompt tells every bot
// to START its reply with @user, so only the first line is inspected.
//
// The previous whole-body substring match ended a round whenever a member
// merely quoted the protocol ("... then I would escalate to user") — a false
// positive that silently skipped every remaining member and round.
func needsHuman(reply string) bool {
	line := strings.TrimSpace(reply)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "@user") ||
		strings.HasPrefix(lower, "@ user") ||
		strings.HasPrefix(lower, "needs your input") ||
		strings.HasPrefix(lower, "escalating to user")
}

// prependMember moves a member to the front of the speaking order.
func prependMember(members []string, name string) []string {
	out := make([]string, 0, len(members))
	out = append(out, name)
	for _, mem := range members {
		if mem != name {
			out = append(out, mem)
		}
	}
	return out
}
