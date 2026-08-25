package bot

import (
	"context"
	"fmt"
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
	// replyCh, when set, receives the turn result synchronously (used by
	// SendToBot so gateway/CLI callers block until this exact turn finishes).
	replyCh chan turnResult
}

// turnResult carries one completed agent turn to a synchronous caller.
type turnResult struct {
	Reply string
	Err   error
}

// Manager runs all configured bots: it owns per-bot agents, persists their
// canonical chat sessions, processes inbound messages sequentially per bot,
// and routes bot-to-bot DMs.
type Manager struct {
	mu        sync.Mutex
	store     *Store
	cfg       *config.Config
	sessions  *sessionstore.Store
	routines  map[string]*RoutineScheduler
	bots      map[string]*botRuntime
	queueCond *sync.Cond
	stopCh    chan struct{}
	rootCtx   context.Context // lifecycle context passed to Start(); used by late-joined workers
	wg        sync.WaitGroup
}

// botRuntime holds one bot's live agent plus its message queue.
type botRuntime struct {
	cfg    *Config
	ag     *agent.Agent
	queue  []pendingMessage
	loaded bool // History restored from session store on first use
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
	if sessions == nil {
		dbPath := magicHome + "/sessions.db"
		if sessions, err = sessionstore.NewStore(dbPath); err != nil {
			return nil, fmt.Errorf("failed to open session store for bots: %w", err)
		}
	}

	m := &Manager{
		store:    store,
		cfg:      cfg,
		sessions: sessions,
		routines: make(map[string]*RoutineScheduler),
		bots:     make(map[string]*botRuntime),
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

// getOrCreateAgent lazily builds a bot's agent, restoring its canonical chat history.
// Caller must hold m.mu.
func (m *Manager) getOrCreateAgentLocked(rt *botRuntime) (*agent.Agent, error) {
	if rt.ag != nil {
		return rt.ag, nil
	}

	prov, registry, err := buildBotDeps(m.cfg, rt.cfg)
	if err != nil {
		return nil, err
	}

	// Register the bot-to-bot messaging tool (message_agent), Hermes-style.
	registry.Register(newMessageAgentTool(m, rt.cfg.MentionTag()))

	systemPrompt := m.buildBotSystemPrompt(rt.cfg)

	// Restore canonical chat history from the session store.
	history := m.loadHistory(rt.cfg.Name)
	ag := agent.NewEnhancedAgent(prov, registry, getToolsSchema(registry), systemPrompt)
	// Apply configurable loop caps (agent.max_turns etc.) so bots honor the
	// same tuning knobs as the web server agents instead of the 60 default.
	if m.cfg.Agent.MaxTurns > 0 {
		ag.ApplyOption(agent.WithMaxTurns(m.cfg.Agent.MaxTurns))
	}
	if m.cfg.Agent.MaxIterations > 0 || m.cfg.Agent.MaxTokenBudget > 0 {
		ag.ApplyOption(agent.WithSteering(agent.SteeringConfig{
			MaxIterations:  m.cfg.Agent.MaxIterations,
			MaxTokenBudget: m.cfg.Agent.MaxTokenBudget,
		}))
	}
	ag.SetSession(CanonicalSessionID(rt.cfg.Name))
	if len(history) > 0 {
		ag.SetHistory(history)
		rt.loaded = true
	}

	rt.ag = ag
	return ag, nil
}

// buildBotSystemPrompt assembles the persona prompt plus the optional
// fleet messaging protocol section. Caller must hold m.mu (it reads
// m.cfg.BotMode and the roster via tagListLocked).
func (m *Manager) buildBotSystemPrompt(cfg *Config) string {
	base := cfg.EffectiveSystemPrompt()
	inject := true // Default on, matching Hermes' agent.bot_mode_protocol default
	if m.cfg.BotMode != nil && m.cfg.BotMode.InjectBotProtocol != nil {
		inject = *m.cfg.BotMode.InjectBotProtocol
	}
	if !inject {
		return base
	}

	if teammates := m.tagListLocked(); teammates != "" {
		return base + fleetProtocol(teammates)
	}
	return base
}

// fleetProtocol builds the bot-to-bot messaging protocol prompt section.
func fleetProtocol(teammates string) string {
	return fmt.Sprintf(`

BOT FLEET PROTOCOL:
- Teammate bots: %s
- To contact a teammate directly, call the message_agent tool with target=<tag> and your composed message. Replies arrive later as "[reply from @<tag>] ..." user messages; treat them as that teammate speaking.
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
	ag, err := m.getOrCreateAgentLocked(rt)
	m.mu.Unlock()

	if err != nil {
		log.Errorf("[BotMode] Agent init failed for %s: %v", rt.cfg.Name, err)
		if msg.replyCh != nil {
			msg.replyCh <- turnResult{Err: err}
		}
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	reply, err := ag.RunConversation(runCtx, msg.Text)
	if err != nil {
		log.Errorf("[BotMode] Bot %s turn failed: %v", rt.cfg.Name, err)
		reply = fmt.Sprintf("(error: %v)", err)
	}

	// Persist canonical chat after every turn.
	m.saveHistory(rt.cfg.Name, ag.GetHistory())

	// Deliver the result to a synchronous caller if one is waiting.
	if msg.replyCh != nil {
		select {
		case msg.replyCh <- turnResult{Reply: reply, Err: err}:
		default:
		}
	}

	// Route the reply for async sources (bot DMs, routines).
	switch {
	case strings.HasPrefix(msg.From, "bot:"):
		// Bot-to-bot DM reply goes back to the sender's canonical chat queue.
		// From carries the sender's mention tag; resolve it back to the bot name
		// (the queue is keyed by name, which may differ from the tag).
		senderTag := strings.TrimPrefix(msg.From, "bot:")
		if senderCfg := m.FindByTag(senderTag); senderCfg != nil {
			log.Infof("[BotMode] Bot %s -> %s: %.80s", rt.cfg.Name, senderCfg.Name, reply)
			_ = m.Enqueue(senderCfg.Name, fmt.Sprintf("[reply from @%s] %s", rt.cfg.MentionTag(), reply), "bot:"+rt.cfg.MentionTag())
		} else {
			log.Warnf("[BotMode] Reply target bot %q no longer exists", senderTag)
		}
	case msg.From == "":
		// Routine output: log only (routines run headless).
		log.Infof("[BotMode] Routine result for %s: %.120s", rt.cfg.Name, reply)
	default:
		// Human user turns are delivered via replyCh above.
		log.Infof("[BotMode] Result for %s -> user: %.120s", rt.cfg.Name, reply)
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

// loadHistory restores persisted canonical-chat messages as provider messages.
func (m *Manager) loadHistory(name string) []provider.Message {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := m.sessions.LoadSession(ctx, CanonicalSessionID(name))
	if err != nil || sess == nil {
		return nil
	}
	out := make([]provider.Message, 0, len(sess.Messages))
	for _, msg := range sess.Messages {
		out = append(out, provider.Message{Role: msg.Role, Content: msg.Content})
	}
	return out
}

// saveHistory persists the canonical chat (skipping the system prompt entry,
// which is rebuilt fresh each launch).
func (m *Manager) saveHistory(name string, history []provider.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessID := CanonicalSessionID(name)

	sess := &sessionstore.Session{
		ID:       sessID,
		Name:     fmt.Sprintf("Bot Chat: %s", name),
		Profile:  name,
		Platform: "bot",
		Messages: make([]types.Message, 0, len(history)),
	}
	for _, msg := range history {
		sess.Messages = append(sess.Messages, msg)
	}
	if err := m.sessions.SaveSession(ctx, sess); err != nil {
		log.Warnf("[BotMode] Failed to persist chat for %s: %v", name, err)
	}
}

// --- Dynamic lifecycle management (used by the Web dashboard API) ---

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
		// Reset cached agent so the next turn picks up new persona/model.
		rt.cfg = cfg
		rt.ag = nil
		rt.loaded = false
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
