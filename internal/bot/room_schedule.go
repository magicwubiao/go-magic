package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// Room schedules (群聊定时推送).
//
// A schedule is a cron expression plus a prompt that is pushed into the room
// log as a normal round — the same coordinator, the same members, the same
// turn budgets. "每天 9 点让 @researcher 汇报进度" must not be a second,
// parallel delivery path: two paths would race, and a scheduled round that
// overlapped a user's would interleave two conversations in the same log.
//
// Schedules only fire in the process that runs Bot Mode. Like bot routines,
// they are per-process state, so running the web server and `magic gateway`
// with Bot Mode enabled in both would double-fire — the same caveat routines
// already carry.

// roomCronParser accepts both 5-field and 6-field (leading seconds) cron
// expressions, matching the parser used for bot routines and by
// AddRoomSchedule's validation.
func roomCronParser() cron.Parser {
	return cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
}

// roomScheduler owns one room's cron entries. It is created and stopped with
// the room coordinator, so a deleted room can never keep pushing messages.
type roomScheduler struct {
	m      *Manager
	roomID string

	mu      sync.Mutex
	cron    *cron.Cron
	entries map[string]cron.EntryID // schedule ID -> cron entry
	stopped bool
}

func newRoomScheduler(m *Manager, roomID string) *roomScheduler {
	return &roomScheduler{
		m:       m,
		roomID:  roomID,
		cron:    cron.New(cron.WithParser(roomCronParser())),
		entries: make(map[string]cron.EntryID),
	}
}

// start registers the room's enabled schedules and starts the ticker.
func (s *roomScheduler) start(cfg *RoomConfig) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	c := s.cron
	s.mu.Unlock()
	s.reload(cfg)
	c.Start()
}

// reload reconciles the cron entries with the room's current schedule list.
// Entries are replaced rather than diffed field by field: a changed cron
// expression only takes effect when the entry is re-added, and the list is
// tiny (a handful of schedules per room).
func (s *roomScheduler) reload(cfg *RoomConfig) {
	if cfg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.cron == nil {
		return
	}
	for id, entry := range s.entries {
		s.cron.Remove(entry)
		delete(s.entries, id)
	}
	for i := range cfg.Schedules {
		sc := &cfg.Schedules[i]
		if sc.Disabled || strings.TrimSpace(sc.Schedule) == "" || strings.TrimSpace(sc.Prompt) == "" {
			continue
		}
		schedID := sc.ID
		entry, err := s.cron.AddFunc(sc.Schedule, func() {
			s.m.fireRoomSchedule(s.roomID, schedID)
		})
		if err != nil {
			log.Warnf("[BotMode] Room %s: invalid schedule %q (%s): %v", s.roomID, sc.Name, sc.Schedule, err)
			continue
		}
		s.entries[sc.ID] = entry
	}
}

// stop tears the scheduler down. Running jobs are not interrupted: a firing
// schedule is a normal room round and winds down through the coordinator's own
// teardown path.
func (s *roomScheduler) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	c := s.cron
	s.entries = map[string]cron.EntryID{}
	s.mu.Unlock()
	if c != nil {
		c.Stop()
	}
}

// fireRoomSchedule runs one scheduled push as a normal room round.
//
// It re-reads the room from disk instead of using the in-memory snapshot: the
// schedule may have been edited or deleted between the tick and this goroutine
// starting, and pushing a prompt the user just removed is worse than skipping.
func (m *Manager) fireRoomSchedule(roomID, scheduleID string) {
	room, err := m.store.LoadRoom(strings.ToLower(roomID))
	if err != nil {
		return
	}
	sc := room.FindSchedule(scheduleID)
	if sc == nil || sc.Disabled {
		return
	}
	prompt := strings.TrimSpace(sc.Prompt)
	if prompt == "" {
		return
	}
	m.setRoomScheduleStatus(roomID, scheduleID, "triggered", "")
	log.Infof("[BotMode] Room %s: schedule %q fired", room.Name, scheduleLabel(sc))

	// Same ceiling as a user round plus slack: the round budget already bounds
	// the round itself, this only keeps a wedged coordinator from holding the
	// firing goroutine (and its status write-back) forever.
	ctx, cancel := context.WithTimeout(context.Background(), roomRoundBudget+2*time.Minute)
	defer cancel()

	res, err := m.SendToRoomFrom(ctx, roomID, prompt, "schedule", sc.Target)
	if err != nil {
		m.setRoomScheduleStatus(roomID, scheduleID, "failed", err.Error())
		log.Warnf("[BotMode] Room %s: schedule %q failed: %v", room.Name, scheduleLabel(sc), err)
		return
	}
	status := "success"
	if res != nil && res.NeedsUser {
		status = "success (needs user)"
	}
	m.setRoomScheduleStatus(roomID, scheduleID, status, "")
}

// setRoomScheduleStatus writes one run's outcome back to the room config.
// Without it a silently failing schedule looks identical to one that never
// fired, which is exactly the complaint a scheduler must not have.
func (m *Manager) setRoomScheduleStatus(roomID, scheduleID, status, errMsg string) {
	key := strings.ToLower(roomID)
	m.roomMu.Lock()
	cfg, err := m.store.LoadRoom(key)
	if err != nil {
		m.roomMu.Unlock()
		return
	}
	sc := cfg.FindSchedule(scheduleID)
	if sc == nil {
		m.roomMu.Unlock()
		return
	}
	now := time.Now().Unix()
	sc.LastRun = &now
	sc.LastStatus = status
	sc.LastError = errMsg
	if err := m.store.SaveRoom(cfg); err != nil {
		log.Warnf("[BotMode] Room %s: failed to persist schedule status: %v", key, err)
	}
	m.roomMu.Unlock()
	// Status write-back only swaps the config: reloading the cron entries here
	// would remove and re-add every schedule on every firing, which resets the
	// ticker of the schedules that have not fired yet.
	m.applyRoomConfigOnly(cfg)
}

// publishRoomConfig makes an already-persisted config visible to the live
// coordinator and reconciles its cron entries. Call it WITHOUT roomMu/m.mu.
func (m *Manager) publishRoomConfig(cfg *RoomConfig) {
	if cfg == nil {
		return
	}
	sched := m.applyRoomConfigOnly(cfg)
	if sched != nil {
		sched.reload(cfg)
	}
}

// applyRoomConfigOnly swaps the config the live coordinator reads and returns
// its scheduler (nil when the room is offline).
func (m *Manager) applyRoomConfigOnly(cfg *RoomConfig) *roomScheduler {
	if cfg == nil {
		return nil
	}
	key := strings.ToLower(cfg.ID)
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.rooms[key]
	if !ok || rt == nil {
		return nil
	}
	rt.cfg = cfg
	return rt.sched
}

// AddRoomSchedule validates and persists a new schedule, then registers it
// live (no restart needed).
func (m *Manager) AddRoomSchedule(roomID string, sc *RoomSchedule) (*RoomConfig, error) {
	if sc == nil {
		return nil, fmt.Errorf("schedule is required")
	}
	sc.Schedule = strings.TrimSpace(sc.Schedule)
	sc.Prompt = strings.TrimSpace(sc.Prompt)
	if sc.Schedule == "" {
		return nil, fmt.Errorf("schedule is required")
	}
	if sc.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if _, err := roomCronParser().Parse(sc.Schedule); err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", sc.Schedule, err)
	}
	if sc.ID == "" {
		sc.ID = NewRoomScheduleID()
	}
	sc.CreatedAt = time.Now().Unix()
	sc.Target = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(sc.Target), "@"))

	key := strings.ToLower(roomID)
	m.roomMu.Lock()
	cfg, err := m.store.LoadRoom(key)
	if err != nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("room not found: %s", key)
	}
	cfg.Schedules = append(cfg.Schedules, *sc)
	if err := m.store.SaveRoom(cfg); err != nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("failed to save room: %w", err)
	}
	m.roomMu.Unlock()
	m.publishRoomConfig(cfg)
	log.Infof("[BotMode] Room %s: schedule %q added (%s)", cfg.Name, scheduleLabel(sc), sc.Schedule)
	return cfg, nil
}

// UpdateRoomSchedule applies a partial update to one schedule (by ID or name)
// and re-registers it live.
func (m *Manager) UpdateRoomSchedule(roomID, idOrName string, mutate func(*RoomSchedule)) (*RoomConfig, error) {
	if mutate == nil {
		return nil, fmt.Errorf("no changes supplied")
	}
	key := strings.ToLower(roomID)
	m.roomMu.Lock()
	cfg, err := m.store.LoadRoom(key)
	if err != nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("room not found: %s", key)
	}
	sc := cfg.FindSchedule(idOrName)
	if sc == nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("schedule not found: %s", idOrName)
	}
	mutate(sc)
	sc.Schedule = strings.TrimSpace(sc.Schedule)
	sc.Prompt = strings.TrimSpace(sc.Prompt)
	sc.Target = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(sc.Target), "@"))
	if sc.Schedule == "" {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("schedule is required")
	}
	if sc.Prompt == "" {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("prompt is required")
	}
	if _, err := roomCronParser().Parse(sc.Schedule); err != nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("invalid schedule %q: %w", sc.Schedule, err)
	}
	if err := m.store.SaveRoom(cfg); err != nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("failed to save room: %w", err)
	}
	m.roomMu.Unlock()
	m.publishRoomConfig(cfg)
	return cfg, nil
}

// RemoveRoomSchedule deletes one schedule and unschedules it immediately.
func (m *Manager) RemoveRoomSchedule(roomID, idOrName string) (*RoomConfig, error) {
	key := strings.ToLower(roomID)
	m.roomMu.Lock()
	cfg, err := m.store.LoadRoom(key)
	if err != nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("room not found: %s", key)
	}
	kept := make([]RoomSchedule, 0, len(cfg.Schedules))
	removed := false
	for _, sc := range cfg.Schedules {
		if sc.ID == idOrName || (sc.Name != "" && strings.EqualFold(sc.Name, idOrName)) {
			removed = true
			continue
		}
		kept = append(kept, sc)
	}
	if !removed {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("schedule not found: %s", idOrName)
	}
	cfg.Schedules = kept
	if err := m.store.SaveRoom(cfg); err != nil {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("failed to save room: %w", err)
	}
	m.roomMu.Unlock()
	m.publishRoomConfig(cfg)
	return cfg, nil
}

// scheduleLabel is the human label used in logs and run status.
func scheduleLabel(sc *RoomSchedule) string {
	if sc == nil {
		return ""
	}
	if strings.TrimSpace(sc.Name) != "" {
		return sc.Name
	}
	return sc.ID
}
