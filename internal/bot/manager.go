package bot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
	sessionstore "github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// pendingMessage is a queued inbound message waiting to be processed by a bot.
type pendingMessage struct {
	Text      string
	From      string // "user", "bot:<tag>", or "" for routines
	Timestamp time.Time
	// RoutineID is set when From == "": identifies which routine produced
	// this message so the worker can write back last-run status after the
	// turn finishes (success/failed).
	RoutineID string
	// replyCh, when set, receives the turn result synchronously (used by
	// SendToBot so gateway/CLI callers block until this exact turn finishes).
	replyCh chan turnResult
	// onDelta, when set, receives incremental assistant output while the
	// worker streams this turn (SendToBotStream). May be nil.
	onDelta StreamHandler
	// retried marks that this message already went through one automatic
	// retry (transient failure or context compaction), preventing infinite
	// retry loops within a single enqueued message.
	retried bool
	// RoomID, when set, runs this turn inside the named group chat session
	// (bot:<name>:room:<roomID>) instead of the canonical chat, and routes the
	// reply back to the room coordinator via replyCh. Used by SendToRoom.
	RoomID string
}

// turnResult carries one completed agent turn to a synchronous caller.
type turnResult struct {
	Reply       string
	Err         error
	FailureCode FailureCode // machine-readable reason when Err != nil
}

// Manager runs all configured bots: it owns per-bot agents, persists their
// canonical chat sessions, processes inbound messages sequentially per bot,
// routes bot-to-bot DMs, and coordinates group chat rooms.
type Manager struct {
	mu        sync.Mutex
	store     *Store
	cfg       *config.Config
	sessions  *sessionstore.Store
	routines  map[string]*RoutineScheduler
	bots      map[string]*botRuntime
	rooms     map[string]*roomRuntime // room ID (lowercase) -> coordinator
	queueCond *sync.Cond
	stopCh    chan struct{}
	rootCtx   context.Context // lifecycle context passed to Start(); used by late-joined workers
	wg        sync.WaitGroup
}

// botRuntime holds one bot's live agents plus its message queue. A bot has
// one canonical-chat agent (ag) and, when it participates in group chats, one
// dedicated agent per room (roomAgents) so room context never bleeds into the
// canonical conversation (and vice versa).
type botRuntime struct {
	cfg    *Config
	ag     *agent.Agent
	queue  []pendingMessage
	loaded bool // History restored from session store on first use

	// lastActive is the Unix-seconds timestamp of the bot's most recent
	// completed turn (chat, DM, or routine). Guarded by m.mu. 0 = never.
	lastActive int64

	// roomAgents holds per-room agents keyed by room ID. Guarded by m.mu.
	roomAgents map[string]*agent.Agent
	// roomLoaded marks which room histories have been restored (keyed by room
	// ID) so we don't re-append stale turns on agent rebuilds.
	roomLoaded map[string]bool
}

// NewManager creates a bot manager. Returns nil (no error) when no bots are defined.
func NewManager(cfg *config.Config, sessions *sessionstore.Store) (*Manager, error) {
	if cfg == nil || !IsEnabled(cfg) {
		return nil, nil
	}

	magicHome := config.GetMagicHome()
	store, err := NewStore(magicHome)
	if err != nil {
		return nil, err
	}

	botDBPath := filepath.Join(magicHome, "bots.db")
	botSessions, err := sessionstore.NewStore(botDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open bots.db: %w", err)
	}

	m := &Manager{
		store:    store,
		cfg:      cfg,
		sessions: botSessions,
		routines: make(map[string]*RoutineScheduler),
		bots:     make(map[string]*botRuntime),
		rooms:    make(map[string]*roomRuntime),
		stopCh:   make(chan struct{}),
	}
	m.queueCond = sync.NewCond(&m.mu)

	return m, nil
}

// IsEnabled reports whether Bot Mode is turned on in config.
func IsEnabled(cfg *config.Config) bool {
	return cfg.BotMode != nil && cfg.BotMode.Enabled
}

// Start initializes all bots, registers routines, and launches worker goroutines.
func (m *Manager) Start(ctx context.Context) error {
	configs, err := m.store.List()
	if err != nil {
		return fmt.Errorf("failed to list bots: %w", err)
	}

	m.mu.Lock()
	m.rootCtx = ctx
	m.mu.Unlock()

	for _, bc := range configs {
		rt := &botRuntime{cfg: bc}
		m.bots[strings.ToLower(bc.Name)] = rt

		// Register this bot's enabled routines.
		sched := NewRoutineScheduler(m, bc)
		if err := sched.Start(ctx); err != nil {
			log.Warnf("[BotMode] Failed to start routines for %s: %v", bc.Name, err)
		} else {
			m.routines[strings.ToLower(bc.Name)] = sched
		}
		log.Infof("[BotMode] Started bot %q (%s)", bc.Name, bc.Title)
	}

	// Launch one worker per bot so messages within a bot are serialized
	// (canonical chat is single-threaded), while different bots run concurrently.
	for name := range m.bots {
		m.wg.Add(1)
		go m.workerLoop(ctx, name)
	}

	// Bring persisted group chat rooms online.
	if rooms, err := m.store.ListRooms(); err == nil {
		for _, rc := range rooms {
			m.startRoomLocked(rc)
		}
		if len(rooms) > 0 {
			log.Infof("[BotMode] %d group chat room(s) online", len(rooms))
		}
	} else {
		log.Warnf("[BotMode] Failed to list rooms: %v", err)
	}

	count := len(m.bots)
	if count > 0 {
		log.Infof("[BotMode] Running with %d bot(s)", count)
	}
	return nil
}

// Stop gracefully shuts down all workers and routine schedulers.
func (m *Manager) Stop() {
	close(m.stopCh)
	m.mu.Lock()
	m.queueCond.Broadcast()
	m.mu.Unlock()
	m.wg.Wait()

	for _, sched := range m.routines {
		sched.Stop()
	}

	// Close the session store so the SQLite file handle is released
	// (required for TempDir cleanup on Windows, where open files
	// cannot be removed).
	if m.sessions != nil {
		if err := m.sessions.Close(); err != nil {
			log.Infof("[BotMode] Failed to close session store: %v", err)
		}
	}
}

// ReloadConfig swaps in the updated global config so subsequent agent turns
// pick up new bot_mode settings (history window, max_turns, protocol
// injection) without a restart. Toggling bot_mode.enabled itself still
// requires a process restart: the manager is created/destroyed only at
// server start/stop.
func (m *Manager) ReloadConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	log.Infof("[BotMode] Config reloaded (history_window=%d)", m.historyWindow())
}

// workerLoop drains one bot's queue sequentially.
func (m *Manager) workerLoop(ctx context.Context, key string) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		default:
		}

		m.mu.Lock()
		for {
			rt, ok := m.bots[key]
			if !ok {
				m.mu.Unlock()
				return
			}
			if len(rt.queue) > 0 {
				break
			}
			// Re-check stop before sleeping again: Broadcast wakes us here
			// on shutdown, and without this check the worker would loop
			// straight back into Wait() and never observe stopCh.
			select {
			case <-ctx.Done():
				m.mu.Unlock()
				return
			case <-m.stopCh:
				m.mu.Unlock()
				return
			default:
			}
			m.queueCond.Wait()
		}
		msg := m.bots[key].queue[0]
		m.bots[key].queue = m.bots[key].queue[1:]
		m.mu.Unlock()

		m.processMessage(ctx, key, msg)
	}
}

// getOrCreateAgentLocked lazily builds a bot's agent for the given session
// context: roomID=="" selects the canonical chat, otherwise the group-chat
// session for that room. Caller must hold m.mu.
func (m *Manager) getOrCreateAgentLocked(rt *botRuntime, roomID string) (*agent.Agent, error) {
	if roomID != "" {
		if rt.roomAgents == nil {
			rt.roomAgents = make(map[string]*agent.Agent)
		}
		if a, ok := rt.roomAgents[roomID]; ok {
			return a, nil
		}
		ag, err := m.buildAgent(rt.cfg, RoomSessionID(rt.cfg.Name, roomID))
		if err != nil {
			return nil, err
		}
		rt.roomAgents[roomID] = ag
		return ag, nil
	}

	if rt.ag != nil {
		return rt.ag, nil
	}
	ag, err := m.buildAgent(rt.cfg, CanonicalSessionID(rt.cfg.Name))
	if err != nil {
		return nil, err
	}
	rt.ag = ag
	return ag, nil
}

// buildAgent constructs a fresh agent for one bot session, restoring its
// persisted history. Caller must hold m.mu (buildBotSystemPrompt reads cfg).
func (m *Manager) buildAgent(bc *Config, sessionID string) (*agent.Agent, error) {
	prov, registry, err := buildBotDeps(m.cfg, bc)
	if err != nil {
		return nil, err
	}

	// Register the bot-to-bot messaging tool (message_agent), Hermes-style.
	registry.Register(newMessageAgentTool(m, bc.MentionTag()))

	systemPrompt := m.buildBotSystemPrompt(bc)

	// Restore history from the session store.
	history := m.loadHistory(sessionID)
	ag := agent.NewEnhancedAgent(prov, registry, getToolsSchema(registry), systemPrompt)
	// Bot mode uses a moderate tool-loop cap.  Bots are conversational
	// agents — a single user message typically needs 3–10 rounds of tool
	// calls.  A reasonable cap prevents runaway loops while allowing
	// complex multi-step tasks.
	const botMaxTurns = 15
	if m.cfg.Agent.MaxTurns > 0 {
		ag.ApplyOption(agent.WithMaxTurns(m.cfg.Agent.MaxTurns))
	} else {
		ag.ApplyOption(agent.WithMaxTurns(botMaxTurns))
	}
	if m.cfg.Agent.MaxIterations > 0 || m.cfg.Agent.MaxTokenBudget > 0 {
		ag.ApplyOption(agent.WithSteering(agent.SteeringConfig{
			MaxIterations:  m.cfg.Agent.MaxIterations,
			MaxTokenBudget: m.cfg.Agent.MaxTokenBudget,
		}))
	}
	ag.SetSession(sessionID)
	if len(history) > 0 {
		ag.SetHistory(history)
	}
	return ag, nil
}

// buildBotSystemPrompt assembles the persona prompt plus the optional
// fleet messaging protocol section. Caller must hold m.mu (it reads
// m.cfg.BotMode and the roster via rosterLocked).
//
// Injection is idempotent: if the persona already contains the protocol
// section (e.g. hand-written SOUL text or a bot created from a template
// that embeds it), we skip appending to avoid duplicated instructions.
func (m *Manager) buildBotSystemPrompt(cfg *Config) string {
	base := cfg.EffectiveSystemPrompt()
	inject := true // Default on, matching Hermes' agent.bot_mode_protocol default
	if m.cfg.BotMode != nil && m.cfg.BotMode.InjectBotProtocol != nil {
		inject = *m.cfg.BotMode.InjectBotProtocol
	}
	if !inject {
		return base
	}

	if strings.Contains(base, "BOT FLEET PROTOCOL") {
		return base // Already embedded; do not inject twice.
	}

	if roster := m.rosterLocked(); roster != "" {
		return base + fleetProtocol(roster)
	}
	return base
}

// fleetProtocol builds the bot-to-bot messaging protocol prompt section.
func fleetProtocol(teammates string) string {
	return fmt.Sprintf(`

BOT FLEET PROTOCOL:
- Teammate bots: %s
- To contact a teammate directly, call the message_agent tool with target=<tag> and your composed message. Replies arrive later as "reply from @<tag>: ..." user messages; treat them as that teammate speaking.
- Mention teammates by their tag when referring to them in your replies.
- Escalate judgment calls to the human with plain text addressed to @user - do not use message_agent for that.`, teammates)
}

// processMessage runs one agent turn and delivers the reply.
func (m *Manager) processMessage(ctx context.Context, key string, msg pendingMessage) {
	m.mu.Lock()
	rt, ok := m.bots[key]
	if !ok {
		m.mu.Unlock()
		return
	}
	ag, err := m.getOrCreateAgentLocked(rt, msg.RoomID)
	m.mu.Unlock()

	if err != nil {
		log.Errorf("[BotMode] Agent init failed for %s: %v", rt.cfg.Name, err)
		if msg.replyCh != nil {
			msg.replyCh <- turnResult{Err: err}
		}
		return
	}

	turnTimeout := 5 * time.Minute
	if m.cfg != nil && m.cfg.BotMode != nil && m.cfg.BotMode.TurnTimeoutMinutes > 0 {
		turnTimeout = time.Duration(m.cfg.BotMode.TurnTimeoutMinutes) * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()

	// runTurn executes one agent turn (streaming or blocking) and returns the
	// full assistant text. Deltas are forwarded to msg.onDelta when set.
	runTurn := func(stream bool, forwardDeltas bool) (string, error) {
		if !stream {
			return ag.RunConversation(runCtx, msg.Text)
		}
		var sb strings.Builder
		err := ag.RunConversationStream(runCtx, msg.Text, func(content string, done bool) {
			if done || content == "" {
				return
			}
			// Skip internal protocol markers that should never be shown to users.
			if isInternalMarker(content) {
				return
			}
			sb.WriteString(content)
			if forwardDeltas && msg.onDelta != nil {
				msg.onDelta(content, false)
			}
		})
		return sb.String(), err
	}

	streaming := msg.onDelta != nil
	reply, err := runTurn(streaming, true)
	if err != nil {
		cls := classifyFailure(err)
		log.Errorf("[BotMode] Bot %s turn failed: %v (code=%s)", rt.cfg.Name, err, cls.Code)

		// Automatic retry policy:
		//   - transient failures (rate limit, 5xx, offline, timeout) retry once;
		//   - context overflow compacts the history then retries once.
		if !msg.retried && cls.Transient {
			msg.retried = true
			if cls.Code == FailureContextOverflow {
				compact := compactHistory(ag.GetHistory())
				ag.SetHistory(compact)
				log.Warnf("[BotMode] Bot %s context overflow: compacted history to %d messages, retrying", rt.cfg.Name, len(compact))
			} else {
				log.Warnf("[BotMode] Bot %s transient failure (%s): retrying once", rt.cfg.Name, cls.Code)
			}
			// On retry, don't re-forward deltas to the caller — the client
			// already saw the first attempt's partial stream and will get the
			// authoritative final text via replyCh.
			reply, err = runTurn(streaming, false)
			if err != nil {
				log.Errorf("[BotMode] Bot %s retry failed: %v", rt.cfg.Name, err)
			}
		}

		if err != nil {
			var failCode FailureCode
			reply, failCode = turnFailureReplyCoded(err, turnTimeout)
			err = &TurnError{Err: err, Code: failCode}
		}
	}

	// Persist the turn's session (canonical chat or room session).
	sessionID := CanonicalSessionID(rt.cfg.Name)
	if msg.RoomID != "" {
		sessionID = RoomSessionID(rt.cfg.Name, msg.RoomID)
	}
	if trimmed, didTrim := m.saveHistory(sessionID, ag.GetHistory()); didTrim {
		// Keep the live agent's in-memory history aligned with what was saved,
		// otherwise context grows unbounded even though disk stays trimmed.
		ag.SetHistory(trimmed)
	}

	// Mark the bot active (used for the "Active now" indicator). Counts every
	// completed turn regardless of success — a failed attempt is still work.
	m.mu.Lock()
	rt.lastActive = time.Now().Unix()
	m.mu.Unlock()

	// Write back routine last-run status when this turn came from a routine.
	if msg.RoutineID != "" {
		status := "success"
		if err != nil {
			status = "failed: " + err.Error()
		}
		m.recordRoutineResult(rt.cfg.Name, msg.RoutineID, status)
	}

	// Deliver the result to a synchronous caller if one is waiting.
	if msg.replyCh != nil {
		code := FailureUnknown
		var te *TurnError
		if errors.As(err, &te) {
			code = te.Code
		}
		select {
		case msg.replyCh <- turnResult{Reply: reply, Err: err, FailureCode: code}:
		default:
		}
	}

	// Route the reply for async sources (bot DMs, routines). Room turns are
	// excluded: their result already went through replyCh above and the room
	// coordinator owns delivery.
	switch {
	case msg.RoomID != "":
		log.Debugf("[BotMode] Room %s turn finished for %s", msg.RoomID, rt.cfg.Name)
	case strings.HasPrefix(msg.From, "bot:"):
		// Bot-to-bot DM reply goes back to the sender's canonical chat queue.
		// From carries the sender's mention tag; resolve it back to the bot name
		// (the queue is keyed by name, which may differ from the tag).
		senderTag := strings.TrimPrefix(msg.From, "bot:")
		if senderCfg := m.FindByTag(senderTag); senderCfg != nil {
			log.Debugf("[BotMode] Bot %s -> %s", rt.cfg.Name, senderCfg.Name)
			// NOTE: avoid square brackets — this text reaches the LLM as a user
			// message and GLM mimics the bracket format. Use "reply from @tag:".
			_ = m.Enqueue(senderCfg.Name, fmt.Sprintf("reply from @%s: %s", rt.cfg.MentionTag(), reply), "bot:"+rt.cfg.MentionTag())
		} else {
			log.Warnf("[BotMode] Reply target bot %q no longer exists", senderTag)
		}
	case msg.From == "":
		// Routine output: log only (routines run headless).
		log.Debugf("[BotMode] Routine result for %s", rt.cfg.Name)
	default:
		// Human user turns are delivered via replyCh above.
		log.Debugf("[BotMode] Result for %s -> user", rt.cfg.Name)
	}
}

// EnqueueMsg appends a pre-built message to a bot's queue.
func (m *Manager) EnqueueMsg(botName string, msg pendingMessage) error {
	key := strings.ToLower(botName)

	m.mu.Lock()
	rt, ok := m.bots[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("bot not found: %s", botName)
	}
	rt.queue = append(rt.queue, msg)
	m.mu.Unlock()

	m.queueCond.Broadcast()
	return nil
}

// Enqueue adds an inbound message to a bot's processing queue.
// from identifies the source: "user", "bot:<tag>", or "" for routines.
func (m *Manager) Enqueue(botName, text, from string) error {
	return m.EnqueueMsg(botName, pendingMessage{Text: text, From: from, Timestamp: time.Now()})
}

// SendToBot delivers a user message to a bot's canonical chat and blocks
// until that turn completes. All turns (user, routine, bot-DM) are serialized
// through the same per-bot queue, so concurrent callers can't corrupt the
// shared conversation history.
func (m *Manager) SendToBot(botName, text string) (string, error) {
	key := strings.ToLower(botName)

	m.mu.Lock()
	_, ok := m.bots[key]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("bot not found: %s", botName)
	}

	msg := pendingMessage{
		Text:    text,
		From:    "user",
		replyCh: make(chan turnResult, 1),
	}

	if err := m.EnqueueMsg(key, msg); err != nil {
		return "", err
	}

	select {
	case res := <-msg.replyCh:
		return res.Reply, res.Err
	case <-m.stopCh:
		return "", fmt.Errorf("bot manager shutting down")
	}
}

// StreamHandler receives incremental assistant output for one streaming turn.
type StreamHandler func(content string, done bool)

// SendToBotStream is the streaming variant of SendToBot: the turn still runs
// serialized on the bot's worker queue (same canonical session guarantees),
// but assistant deltas are forwarded to onDelta as they are generated.
// onDelta may be nil; the full reply is also returned when the turn ends.
func (m *Manager) SendToBotStream(botName, text string, onDelta StreamHandler) (string, error) {
	key := strings.ToLower(botName)

	m.mu.Lock()
	rt, ok := m.bots[key]
	m.mu.Unlock()
	if !ok || rt == nil {
		return "", fmt.Errorf("bot not found: %s", botName)
	}
	if onDelta == nil {
		// No callback: behave exactly like SendToBot.
		return m.SendToBot(botName, text)
	}

	msg := pendingMessage{
		Text:    text,
		From:    "user",
		replyCh: make(chan turnResult, 1),
		onDelta: onDelta,
	}

	if err := m.EnqueueMsg(key, msg); err != nil {
		return "", err
	}

	select {
	case res := <-msg.replyCh:
		return res.Reply, res.Err
	case <-m.stopCh:
		return "", fmt.Errorf("bot manager shutting down")
	}
}

// SendMessageAgent implements fire-and-forget bot-to-bot messaging
// (the message_agent tool backend).
func (m *Manager) SendMessageAgent(senderTag, targetTag, message string) error {
	target := m.FindByTag(targetTag)
	if target == nil {
		return fmt.Errorf("unknown agent %q; available tags: %s", targetTag, m.TagList())
	}
	return m.Enqueue(target.Name, message, "bot:"+senderTag)
}

// HasBot reports whether a bot with the given name/tag exists.
// Used as the resolver callback for mention parsing.
func (m *Manager) HasBot(tag string) bool {
	return m.FindByTag(tag) != nil
}

// FindByTag resolves a bot by its mention tag or exact name.
func (m *Manager) FindByTag(tag string) *Config {
	tag = strings.ToLower(strings.TrimPrefix(tag, "@"))
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.findByTagLocked(tag)
}

// findByTagLocked is the unlocked variant; caller must hold m.mu.
func (m *Manager) findByTagLocked(tag string) *Config {
	if rt, ok := m.bots[tag]; ok {
		return rt.cfg
	}
	for _, rt := range m.bots {
		if rt.cfg.MentionTag() == tag {
			return rt.cfg
		}
	}
	return nil
}

// TagList returns all mention tags, comma-separated (for tool errors/prompts).
func (m *Manager) TagList() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tagListLocked()
}

// tagListLocked is the unlocked variant; caller must hold m.mu.
func (m *Manager) tagListLocked() string {
	var tags []string
	for _, rt := range m.bots {
		tags = append(tags, "@"+rt.cfg.MentionTag())
	}
	return strings.Join(tags, ", ")
}

// rosterLocked returns the human-readable teammate roster with roles, e.g.
// "@researcher (Research Assistant), @coder (Coding Bot)". Caller must hold
// m.mu. Bots without a Title fall back to their tag only.
func (m *Manager) rosterLocked() string {
	var entries []string
	for _, rt := range m.bots {
		entry := "@" + rt.cfg.MentionTag()
		if rt.cfg.Title != "" && !strings.EqualFold(rt.cfg.Title, rt.cfg.Name) {
			entry += " (" + rt.cfg.Title + ")"
		}
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	return strings.Join(entries, ", ")
}

// Roster returns the public roster string (role-aware teammate list) for
// tool descriptions and CLI output.
func (m *Manager) Roster() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rosterLocked()
}

// List returns all bot configs.
func (m *Manager) List() []*Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Config
	for _, rt := range m.bots {
		out = append(out, rt.cfg)
	}
	return out
}

// CanonicalSessionID returns the persistent session ID for a bot's canonical chat.
func CanonicalSessionID(name string) string {
	return fmt.Sprintf("bot:%s:chat", strings.ToLower(name))
}

// RoomSessionID returns the persistent session ID for one bot inside a group
// chat room (Hermes' "Group: <name>" persistence, namespaced per bot so each
// member keeps its own room memory).
func RoomSessionID(name, roomID string) string {
	return fmt.Sprintf("bot:%s:room:%s", strings.ToLower(name), strings.ToLower(roomID))
}

// loadHistory restores persisted session messages as provider messages.
func (m *Manager) loadHistory(sessionID string) []provider.Message {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := m.sessions.LoadSession(ctx, sessionID)
	if err != nil || sess == nil {
		return nil
	}
	out := make([]provider.Message, 0, len(sess.Messages))
	for _, msg := range sess.Messages {
		out = append(out, provider.Message{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		})
	}
	// Sanitize: drop orphaned tool messages that have no preceding assistant
	// message with matching tool_calls. This can happen if older sessions were
	// saved with a loadHistory that dropped ToolCalls/ToolCallID fields.
	out = sanitizeBotHistory(out)
	return out
}

// sanitizeBotHistory removes orphaned tool messages from bot chat history.
func sanitizeBotHistory(history []provider.Message) []provider.Message {
	cleaned := make([]provider.Message, 0, len(history))
	for i, msg := range history {
		if msg.Role == "tool" {
			hasCaller := false
			for j := i - 1; j >= 0; j-- {
				if cleaned[j].Role == "assistant" && len(cleaned[j].ToolCalls) > 0 {
					for _, tc := range cleaned[j].ToolCalls {
						if tc.ID == msg.ToolCallID {
							hasCaller = true
							break
						}
					}
				}
				if hasCaller {
					break
				}
				// Don't look past another tool or user message boundary
				if cleaned[j].Role == "user" {
					break
				}
			}
			if !hasCaller {
				log.Warnf("[BotMode] Dropping orphaned tool message from history (tool_call_id=%s)", msg.ToolCallID)
				continue
			}
		}
		cleaned = append(cleaned, msg)
	}
	return cleaned
}

// defaultHistoryWindow is the fallback cap for canonical chat length when
// bot_mode.history_window is unset.
const defaultHistoryWindow = 200

// historyWindow returns the configured canonical-chat message cap (min 20).
func (m *Manager) historyWindow() int {
	w := 0
	if m.cfg != nil && m.cfg.BotMode != nil {
		w = m.cfg.BotMode.HistoryWindow
	}
	if w <= 0 {
		w = defaultHistoryWindow
	}
	if w < 20 {
		w = 20
	}
	return w
}

// truncateHistoryAtTurnBoundary trims a provider-message history to at most
// window messages, cutting back whole turns (a turn starts at a "user"
// message and includes everything before the next "user" message) so tool
// calls/results are never separated from their prompt. The system prompt
// entry (leading "system" roles) is always preserved. Returns the possibly-
// unmodified slice and whether any trim happened.
func truncateHistoryAtTurnBoundary(history []provider.Message, window int) ([]provider.Message, bool) {
	if window <= 0 || len(history) <= window {
		return history, false
	}
	// Keep the leading system prompt(s).
	start := 0
	for start < len(history) && history[start].Role == "system" {
		start++
	}
	// Drop whole turns from the front until we fit in the window.
	for len(history)-start > window && start < len(history) {
		if history[start].Role != "user" {
			// Orphaned tool/result entry without a leading user message.
			start++
			continue
		}
		end := start + 1
		for end < len(history) && history[end].Role != "user" {
			end++
		}
		if len(history)-end == 0 {
			break // Would erase the entire conversation; keep as-is.
		}
		start = end
	}
	if start >= len(history) || len(history)-start > window {
		return history, false
	}
	return history[start:], true
}

// saveHistory persists a session (canonical chat or room session), skipping
// the system prompt entry (rebuilt fresh each launch). The stored history is
// capped by the configured history window so long-running bots don't grow
// without bound; trimming happens on user-turn boundaries only.
// Returns the (possibly trimmed) history and whether a trim happened, so the
// caller can keep its live agent's in-memory history aligned with disk.
func (m *Manager) saveHistory(sessionID string, history []provider.Message) ([]provider.Message, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	trimmed, didTrim := truncateHistoryAtTurnBoundary(history, m.historyWindow())

	sess := &sessionstore.Session{
		ID:       sessionID,
		Name:     "Bot Chat: " + sessionID,
		Profile:  sessionID,
		Platform: "bot",
		Messages: make([]types.Message, 0, len(trimmed)),
	}
	for _, msg := range trimmed {
		sess.Messages = append(sess.Messages, msg)
	}
	if err := m.sessions.SaveSession(ctx, sess); err != nil {
		log.Warnf("[BotMode] Failed to persist chat %s: %v", sessionID, err)
	}
	return trimmed, didTrim
}

// --- Dynamic lifecycle management (used by the Web dashboard API) ---

// ClearHistory wipes a bot's canonical conversation: the persisted session
// is deleted and any live in-memory agent history is reset so subsequent
// turns start fresh. Persona/system prompt are kept — only chat turns go.
func (m *Manager) ClearHistory(name string) error {
	if _, err := m.GetBot(name); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessID := CanonicalSessionID(name)
	if err := m.sessions.DeleteSession(ctx, sessID); err != nil {
		// Missing session is fine (nothing to clear); anything else we log
		// but still reset the live agent below.
		log.Warnf("[BotMode] Failed to delete session %s for %s: %v", sessID, name, err)
	}

	key := strings.ToLower(name)
	m.mu.Lock()
	if rt, ok := m.bots[key]; ok && rt != nil && rt.ag != nil {
		rt.ag.SetHistory(nil)
		rt.loaded = true // disk is empty; don't restore stale turns next use
	}
	m.mu.Unlock()

	log.Infof("[BotMode] Cleared canonical chat for bot %q", name)
	return nil
}

// RunRoutineNow triggers an immediate one-off execution of a routine,
// bypassing its cron schedule. Returns an error when the bot or routine
// does not exist. The run itself happens asynchronously (same path as a
// scheduled firing), updating last_run/last_status on completion.
func (m *Manager) RunRoutineNow(botName, routineIDOrName string) error {
	routines, err := m.ListRoutines(botName)
	if err != nil {
		return err
	}
	var target *RoutineConfig
	for _, rt := range routines {
		if rt.ID == routineIDOrName || strings.EqualFold(rt.Name, routineIDOrName) {
			target = rt
			break
		}
	}
	if target == nil {
		return fmt.Errorf("routine not found: %s", routineIDOrName)
	}
	if !target.Enabled {
		return fmt.Errorf("routine %q is disabled; enable it first", target.Name)
	}

	m.mu.Lock()
	sched := m.routines[strings.ToLower(botName)]
	m.mu.Unlock()
	if sched == nil {
		return fmt.Errorf("bot %q is not running", botName)
	}

	go sched.runRoutine(target.ID)
	return nil
}

// CreateBot persists a new bot config and, when the manager is running,
// brings it online immediately (worker + routine scheduler).
func (m *Manager) CreateBot(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("bot config is required")
	}
	if err := ValidateName(cfg.Name); err != nil {
		return err
	}
	if existing, err := m.store.Load(cfg.Name); err == nil && existing != nil {
		return fmt.Errorf("bot %q already exists", cfg.Name)
	}
	now := time.Now().Unix()
	cfg.CreatedAt = now
	cfg.UpdatedAt = now
	if err := m.store.Save(cfg); err != nil {
		return fmt.Errorf("failed to save bot: %w", err)
	}
	m.startBotLocked(cfg)
	return nil
}

// CloneBot duplicates an existing bot's full profile (persona, model pin,
// tools/skills allowlists, memory, avatar, env) under a new name with a fresh
// identity and empty chat history. Routines are NOT copied: a clone starts
// clean. The clone comes online immediately when the manager is running.
func (m *Manager) CloneBot(name, newName string) (*Config, error) {
	src, err := m.store.Load(name)
	if err != nil {
		return nil, fmt.Errorf("bot not found: %s", name)
	}
	if err := ValidateName(newName); err != nil {
		return nil, err
	}
	if existing, err := m.store.Load(newName); err == nil && existing != nil {
		return nil, fmt.Errorf("bot %q already exists", newName)
	}
	if strings.EqualFold(src.Name, newName) {
		return nil, fmt.Errorf("new name must differ from the source bot")
	}

	clone := *src // shallow copy; deep-copy the slice/map fields below
	clone.Name = newName
	clone.Tools = append([]string(nil), src.Tools...)
	clone.Skills = append([]string(nil), src.Skills...)
	if src.Env != nil {
		clone.Env = make(map[string]string, len(src.Env))
		for k, v := range src.Env {
			clone.Env[k] = v
		}
	}
	now := time.Now().Unix()
	clone.CreatedAt = now
	clone.UpdatedAt = now

	if err := m.store.Save(&clone); err != nil {
		return nil, fmt.Errorf("failed to save cloned bot: %w", err)
	}
	m.startBotLocked(&clone)
	log.Infof("[BotMode] Cloned bot %q -> %q", name, newName)
	return &clone, nil
}

// UpdateBot applies partial updates to an existing bot and hot-reloads its
// runtime (agent + routine scheduler) without dropping queued messages.
func (m *Manager) UpdateBot(name string, mutate func(*Config)) (*Config, error) {
	cfg, err := m.store.Load(name)
	if err != nil {
		return nil, fmt.Errorf("bot not found: %s", name)
	}
	mutate(cfg)
	if cfg.Name != name {
		// Renames are not supported: mention tags and session IDs key off Name.
		return nil, fmt.Errorf("renaming bots is not supported")
	}
	if err := m.store.Save(cfg); err != nil {
		return nil, fmt.Errorf("failed to save bot: %w", err)
	}

	key := strings.ToLower(name)
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.bots[key]
	if ok {
		// Reset cached agents so the next turn picks up new persona/model.
		// This includes per-room agents: group chat turns must reflect the
		// updated config too, not just the canonical chat.
		rt.cfg = cfg
		rt.ag = nil
		rt.loaded = false
		rt.roomAgents = nil
		rt.roomLoaded = nil
	}
	if sched, ok2 := m.routines[key]; ok2 && sched != nil {
		sched.Stop()
		delete(m.routines, key)
	}
	if ok {
		sched := NewRoutineScheduler(m, cfg)
		if err := sched.Start(m.rootCtxOrBackground()); err != nil {
			log.Warnf("[BotMode] Failed to restart routines for %s: %v", name, err)
		} else {
			m.routines[key] = sched
		}
	}
	return cfg, nil
}

// DeleteBot removes a bot's config and stops its runtime. Its canonical chat
// session is kept on disk for audit/history purposes.
func (m *Manager) DeleteBot(name string) error {
	cfg, err := m.store.Load(name)
	if err != nil {
		return fmt.Errorf("bot not found: %s", name)
	}

	key := strings.ToLower(cfg.Name)
	m.mu.Lock()
	if sched, ok := m.routines[key]; ok && sched != nil {
		sched.Stop()
		delete(m.routines, key)
	}
	if rt, ok := m.bots[key]; ok {
		// Drop any pending messages so the worker exits promptly.
		rt.queue = nil
		delete(m.bots, key)
	}
	m.mu.Unlock()

	m.queueCond.Broadcast()

	if err := m.store.Delete(cfg.Name); err != nil {
		return fmt.Errorf("failed to delete bot: %w", err)
	}
	return nil
}

// GetBot loads one bot's config from disk.
func (m *Manager) GetBot(name string) (*Config, error) {
	return m.store.Load(name)
}

// RuntimeStatus reports live state for one bot (zero-value if not running).
func (m *Manager) RuntimeStatus(botName string) RuntimeState {
	state := RuntimeState{Name: botName, SessionID: CanonicalSessionID(botName)}
	key := strings.ToLower(botName)

	m.mu.Lock()
	rt, ok := m.bots[key]
	if ok {
		state.QueueDepth = len(rt.queue)
		state.LastActiveUnix = rt.lastActive
	}
	routines, _ := m.store.LoadRoutines(rtName(key))
	for _, r := range routines {
		if r.Enabled {
			state.ActiveRoutines++
		}
	}
	m.mu.Unlock()

	if ag := m.AgentFor(botName); ag != nil {
		state.HistoryLength = len(ag.GetHistory())
	}
	return state
}

func rtName(key string) string { return key }

// AgentFor exposes a bot's live agent (nil when offline). Used by the web
// layer to read history without racing the message queue.
func (m *Manager) AgentFor(botName string) *agent.Agent {
	key := strings.ToLower(botName)
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.bots[key]
	if !ok || rt == nil {
		return nil
	}
	return rt.ag
}

// Sessions exposes the session store backing bots' canonical chats.
func (m *Manager) Sessions() *sessionstore.Store {
	return m.sessions
}

// startBotLocked brings one bot online (idempotent). Safe to call while
// running or before Start().
func (m *Manager) startBotLocked(cfg *Config) {
	key := strings.ToLower(cfg.Name)

	m.mu.Lock()
	if _, exists := m.bots[key]; exists {
		m.mu.Unlock()
		return
	}
	rt := &botRuntime{cfg: cfg}
	m.bots[key] = rt

	var ctx context.Context
	if m.rootCtx != nil {
		ctx = m.rootCtx
	} else {
		ctx = context.Background()
	}

	sched := NewRoutineScheduler(m, cfg)
	if err := sched.Start(ctx); err != nil {
		log.Warnf("[BotMode] Failed to start routines for %s: %v", cfg.Name, err)
	} else {
		m.routines[key] = sched
	}
	m.mu.Unlock()

	m.wg.Add(1)
	go m.workerLoop(ctx, key)
	log.Infof("[BotMode] Bot %q is now online (%s)", cfg.Name, cfg.Title)
}

// rootCtxOrBackground returns the lifecycle context captured by Start(),
// falling back to Background for managers created but never started.
func (m *Manager) rootCtxOrBackground() context.Context {
	if m.rootCtx != nil {
		return m.rootCtx
	}
	return context.Background()
}

// recordRoutineResult writes back a routine's last-run timestamp and status
// after its turn finishes in the worker ("success" or "failed: <err>").
// Called from processMessage; safe to call for unknown routine IDs.
func (m *Manager) recordRoutineResult(botName, routineID, status string) {
	routines, err := m.store.LoadRoutines(botName)
	if err != nil {
		log.Warnf("[BotMode] Failed to load routines for status write-back (%s): %v", botName, err)
		return
	}
	now := time.Now().Unix()
	found := false
	for _, r := range routines {
		if r.ID == routineID {
			r.LastRun = &now
			r.LastStatus = status
			found = true
			break
		}
	}
	if !found {
		// Routine was deleted while its message sat in the queue.
		return
	}
	if err := m.store.SaveRoutines(botName, routines); err != nil {
		log.Warnf("[BotMode] Failed to save routine status for %s: %v", botName, err)
	}
}

// isInternalMarker returns true if the content is an internal protocol marker
// (TURN_START, TOOL_START, TOOL_RESULT_START/END) that should never be
// forwarded to end users in bot chat streams.
func isInternalMarker(content string) bool {
	t := strings.TrimSpace(content)
	if t == ">>>TURN_START<<<" {
		return true
	}
	if strings.Contains(t, ">>>TOOL_START|") {
		return true
	}
	if strings.Contains(t, ">>>TOOL_RESULT_START|") {
		return true
	}
	if strings.Contains(t, ">>>TOOL_RESULT_END<<<") {
		return true
	}
	return false
}
