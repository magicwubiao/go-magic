package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	sessionstore "github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// RoomMessage is one entry in a group chat's shared history.
type RoomMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"` // bot mention tag or "user"
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"` // Unix seconds
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
	Reply  chan *RoomResult
}

// roomRuntime is a room's live state: its config plus the coordinator
// channel. One coordinator goroutine per room serializes all rounds.
type roomRuntime struct {
	cfg       *RoomConfig
	triggerCh chan roomRequest
	stopCh    chan struct{}
}

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
	now := time.Now().Unix()
	cfg.CreatedAt = now
	cfg.UpdatedAt = now
	if err := m.store.SaveRoom(cfg); err != nil {
		return fmt.Errorf("failed to save room: %w", err)
	}
	m.startRoomLocked(cfg)
	log.Infof("[BotMode] Room %q created with %d members", cfg.Name, len(cfg.Members))
	return nil
}

// UpdateRoom applies partial updates to a room and hot-reloads its runtime.
func (m *Manager) UpdateRoom(id string, mutate func(*RoomConfig)) (*RoomConfig, error) {
	cfg, err := m.store.LoadRoom(id)
	if err != nil {
		return nil, fmt.Errorf("room not found: %s", id)
	}
	mutate(cfg)
	if len(cfg.Members) < MinRoomMembers {
		return nil, fmt.Errorf("a room needs at least %d members", MinRoomMembers)
	}
	if len(cfg.Members) > MaxRoomMembers {
		return nil, fmt.Errorf("a room supports at most %d members", MaxRoomMembers)
	}
	seen := map[string]bool{}
	for i, name := range cfg.Members {
		key := strings.ToLower(name)
		if m.FindByTag(key) == nil {
			return nil, fmt.Errorf("room member %q is not a known bot", name)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate room member %q", name)
		}
		seen[key] = true
		cfg.Members[i] = key
	}
	if err := m.store.SaveRoom(cfg); err != nil {
		return nil, fmt.Errorf("failed to save room: %w", err)
	}

	// Hot-reload: stop the old coordinator and start a fresh one so member
	// changes take effect immediately. In-flight requests are dropped.
	m.mu.Lock()
	if rt, ok := m.rooms[strings.ToLower(id)]; ok {
		close(rt.stopCh)
		delete(m.rooms, strings.ToLower(id))
	}
	m.mu.Unlock()
	m.startRoomLocked(cfg)
	return cfg, nil
}

// DeleteRoom removes a room and stops its coordinator.
func (m *Manager) DeleteRoom(id string) error {
	cfg, err := m.store.LoadRoom(id)
	if err != nil {
		return fmt.Errorf("room not found: %s", id)
	}
	m.mu.Lock()
	if rt, ok := m.rooms[strings.ToLower(id)]; ok {
		close(rt.stopCh)
		delete(m.rooms, strings.ToLower(id))
	}
	m.mu.Unlock()

	if err := m.store.DeleteRoom(cfg.ID); err != nil {
		return fmt.Errorf("failed to delete room: %w", err)
	}
	// Drop persisted room sessions for all members + the shared room log.
	m.mu.Lock()
	for _, member := range cfg.Members {
		if rt, ok := m.bots[member]; ok && rt != nil {
			if rt.roomAgents != nil {
				delete(rt.roomAgents, strings.ToLower(id))
			}
		}
	}
	m.mu.Unlock()
	log.Infof("[BotMode] Room %q deleted", cfg.Name)
	return nil
}

// ListRooms returns all room configs with live member status.
func (m *Manager) ListRooms() ([]*RoomConfig, error) {
	return m.store.ListRooms()
}

// GetRoom loads one room config.
func (m *Manager) GetRoom(id string) (*RoomConfig, error) {
	return m.store.LoadRoom(id)
}

// RoomMessages returns the shared message history of a room (most recent
// first, capped at maxMessages).
func (m *Manager) RoomMessages(roomID string) ([]RoomMessage, error) {
	cfg, err := m.store.LoadRoom(roomID)
	if err != nil {
		return nil, fmt.Errorf("room not found: %s", roomID)
	}
	msgs, err := m.loadRoomHistory(roomID)
	if err != nil {
		return nil, err
	}
	cap := cfg.MessagesCap()
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
	key := strings.ToLower(roomID)
	m.mu.Lock()
	rt, ok := m.rooms[key]
	m.mu.Unlock()
	if !ok || rt == nil {
		return nil, fmt.Errorf("room not found: %s", roomID)
	}

	if target != "" {
		target = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(target), "@"))
		if target != "user" && m.FindByTag(target) == nil {
			return nil, fmt.Errorf("unknown bot %q; available: %s", target, m.TagList())
		}
	}

	req := roomRequest{
		Text:   text,
		From:   "user",
		Target: target,
		Reply:  make(chan *RoomResult, 1),
	}
	select {
	case rt.triggerCh <- req:
	case <-m.stopCh:
		return nil, errors.New("bot manager shutting down")
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
func (m *Manager) startRoomLocked(cfg *RoomConfig) {
	key := strings.ToLower(cfg.ID)
	m.mu.Lock()
	if _, exists := m.rooms[key]; exists {
		m.mu.Unlock()
		return
	}
	rt := &roomRuntime{
		cfg:       cfg,
		triggerCh: make(chan roomRequest, 16),
		stopCh:    make(chan struct{}),
	}
	m.rooms[key] = rt
	m.mu.Unlock()

	m.wg.Add(1)
	go m.roomLoop(rt)
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
			m.runRoomRound(rt, req)
		}
	}
}

// runRoomRound executes the full speaking sequence for one user message:
// members take turns (target first), up to MaxRounds rounds; each bot speaks
// at most once per round; the round stops early when a bot escalates to
// @user or when nobody has anything to add.
func (m *Manager) runRoomRound(rt *roomRuntime, req roomRequest) {
	room := rt.cfg
	// Persist the human's message into the room log.
	m.appendRoomMessage(room, "user", req.Text)

	members := room.Members
	if req.Target != "" && req.Target != "user" {
		if tc := m.FindByTag(req.Target); tc != nil {
			// Move the targeted member to the front of this round.
			members = prependMember(members, strings.ToLower(tc.Name))
		}
	}

	maxRounds := room.Rounds()
	needsUser := false

	for round := 0; round < maxRounds; round++ {
		anySpoke := false
		for _, member := range members {
			// Skip members that were removed mid-round.
			if m.FindByTag(member) == nil {
				continue
			}
			history, _ := m.loadRoomHistory(room.ID)
			prompt := m.buildRoomPrompt(room, member, history, round, maxRounds)
			reply, err := m.sendRoomTurn(room.ID, member, prompt)
			if err != nil {
				log.Warnf("[BotMode] Room %s member %s turn failed: %v", room.Name, member, err)
				m.appendRoomMessage(room, member, "(no reply: "+err.Error()+")")
				continue
			}
			reply = strings.TrimSpace(reply)
			if reply == "" {
				continue
			}
			anySpoke = true
			m.appendRoomMessage(room, member, reply)
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

	history, _ := m.loadRoomHistory(room.ID)
	res := &RoomResult{RoomID: room.ID, Messages: history, NeedsUser: needsUser}
	select {
	case req.Reply <- res:
	default:
	}
}

// sendRoomTurn enqueues one room turn to a member bot and waits for its
// worker to finish (serialized with the bot's other chats).
func (m *Manager) sendRoomTurn(roomID, botName, text string) (string, error) {
	msg := pendingMessage{
		Text:    text,
		From:    "room:" + roomID,
		RoomID:  strings.ToLower(roomID),
		replyCh: make(chan turnResult, 1),
	}
	if err := m.EnqueueMsg(botName, msg); err != nil {
		return "", err
	}
	select {
	case res := <-msg.replyCh:
		return res.Reply, res.Err
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
		entry := "@" + mem
		if mc := m.FindByTag(mem); mc != nil && mc.Title != "" {
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
		fmt.Fprintf(&sb, "- @%s: %s\n", msg.From, msg.Content)
	}
	fmt.Fprintf(&sb, "\nYour turn (round %d/%d). You are @%s.\n", round+1, maxRounds, member)
	sb.WriteString("Reply to the room conversation directly. Address @<member> when your message is meant for a specific bot. ")
	sb.WriteString("If the task needs the human's input or approval, start your reply with @user to stop the round and escalate.")
	return sb.String()
}

// appendRoomMessage persists one message into the room's shared session log
// and keeps only the last MessagesCap entries (10-message hard cap).
func (m *Manager) appendRoomMessage(room *RoomConfig, from, content string) {
	msgs, err := m.loadRoomHistory(room.ID)
	if err != nil {
		msgs = nil
	}
	msgs = append(msgs, RoomMessage{
		ID:        "m_" + uuid.New().String()[:8],
		From:      from,
		Content:   content,
		Timestamp: time.Now().Unix(),
	})
	capN := room.MessagesCap()
	if len(msgs) > capN {
		msgs = msgs[len(msgs)-capN:]
	}
	m.saveRoomHistory(room.ID, msgs)
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
			ID:        msg.ID,
			From:      msg.From,
			Content:   msg.Content,
			Timestamp: msg.Timestamp.Unix(),
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
		})
	}
	if err := m.sessions.SaveSession(ctx, sess); err != nil {
		log.Warnf("[BotMode] Failed to persist room %s log: %v", roomID, err)
	}
}

// needsHuman detects an escalation request: reply opens with @user.
func needsHuman(reply string) bool {
	t := strings.TrimSpace(reply)
	lower := strings.ToLower(t)
	return strings.HasPrefix(lower, "@user") ||
		strings.HasPrefix(lower, "@ user") ||
		strings.Contains(lower, "needs your input") ||
		strings.Contains(lower, "escalating to user")
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
