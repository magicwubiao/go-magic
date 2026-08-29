package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// RoutineScheduler runs one bot's recurring tasks (Routines).
// Mirrors Hermes design: routines are plain cron jobs bound to the bot and
// named "[bot:<name>] <routine>", so they surface in `magic cron list` too.
type RoutineScheduler struct {
	manager  *Manager
	botCfg   *Config
	cron     *cron.Cron
	entryIDs map[string]cron.EntryID // routine ID -> cron entry
	ctx      context.Context
	cancel   context.CancelFunc
}

// RoutineJobName builds the namespaced job name shown in cron listings.
func RoutineJobName(botName, routineName string) string {
	return fmt.Sprintf("bot:%s - %s", botName, routineName)
}

// NewRoutineScheduler creates (but does not start) a scheduler for one bot.
// The cron instance accepts an optional leading seconds field so that both
// "0 9 * * *" (5 fields) and "*/10 * * * * *" (6 fields) schedules work —
// matching the parser used for validation in AddRoutine.
func NewRoutineScheduler(m *Manager, cfg *Config) *RoutineScheduler {
	parser := cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)
	return &RoutineScheduler{
		manager:  m,
		botCfg:   cfg,
		cron:     cron.New(cron.WithParser(parser)),
		entryIDs: make(map[string]cron.EntryID),
	}
}

// Start loads enabled routines into the cron scheduler.
func (s *RoutineScheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	routines, err := s.manager.store.LoadRoutines(s.botCfg.Name)
	if err != nil {
		return err
	}

	for _, r := range routines {
		if !r.Enabled || r.Schedule == "" {
			continue
		}
		if err := s.add(r); err != nil {
			log.Warnf("[BotMode] Failed to schedule routine %q for %s: %v", r.Name, s.botCfg.Name, err)
		}
	}

	s.cron.Start()
	return nil
}

// Stop halts all scheduled routines for this bot.
func (s *RoutineScheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.cron != nil {
		ctx := s.cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

// add registers one routine with the scheduler.
func (s *RoutineScheduler) add(r *RoutineConfig) error {
	routineID := r.ID
	entryID, err := s.cron.AddFunc(r.Schedule, func() {
		s.runRoutine(routineID)
	})
	if err != nil {
		return err
	}
	s.entryIDs[routineID] = entryID
	log.Infof("[BotMode] Scheduled routine %q for bot %s (%s)",
		RoutineJobName(s.botCfg.Name, r.Name), s.botCfg.Name, r.Schedule)
	return nil
}

// runRoutine executes one routine turn inside the bot's canonical chat context.
func (s *RoutineScheduler) runRoutine(routineID string) {
	routines, err := s.manager.store.LoadRoutines(s.botCfg.Name)
	if err != nil {
		return
	}
	var target *RoutineConfig
	for _, r := range routines {
		if r.ID == routineID {
			target = r
			break
		}
	}
	if target == nil || !target.Enabled {
		return
	}

	now := time.Now().Unix()
	prompt := target.Prompt

	// Fire-and-forget into the bot's queue so execution stays serialized
	// with regular chat turns on the same canonical session. RoutineID is
	// carried so the worker writes back last-run status after the turn.
	if err := s.manager.EnqueueMsg(s.botCfg.Name, pendingMessage{
		Text:      prompt,
		From:      "",
		Timestamp: time.Now(),
		RoutineID: target.ID,
	}); err != nil {
		log.Warnf("[BotMode] Routine %q enqueue failed for %s: %v", target.Name, s.botCfg.Name, err)
		target.LastRun = &now
		target.LastStatus = "failed: enqueue error"
		s.persistRoutines(routines)
		return
	}

	// Record that the routine fired (final success/failed status lands via
	// recordRoutineResult once the worker finishes this turn).
	target.LastRun = &now
	target.LastStatus = "triggered"
	s.persistRoutines(routines)
}

// persistRoutines writes the routine list back to disk.
func (s *RoutineScheduler) persistRoutines(routines []*RoutineConfig) {
	if err := s.manager.store.SaveRoutines(s.botCfg.Name, routines); err != nil {
		log.Warnf("[BotMode] Failed to save routines for %s: %v", s.botCfg.Name, err)
	}
}

// AddRoutine validates + persists a new routine for this bot and schedules it live.
func (m *Manager) AddRoutine(botName string, r *RoutineConfig) error {
	m.mu.Lock()
	sched, ok := m.routines[strings.ToLower(botName)]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("bot not found or not running: %s", botName)
	}
	if r.Schedule == "" {
		return fmt.Errorf("schedule is required")
	}
	// Validate schedule by dry-parsing.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.SecondOptional)
	if _, err := parser.Parse(r.Schedule); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", r.Schedule, err)
	}
	if r.ID == "" {
		r.ID = NewRoutineID(botName)
	}
	r.Enabled = true
	r.CreatedAt = time.Now().Unix()

	routines, err := m.store.LoadRoutines(botName)
	if err != nil {
		return err
	}
	routines = append(routines, r)
	if err := m.store.SaveRoutines(botName, routines); err != nil {
		return err
	}

	m.mu.Lock()
	if ok && sched != nil {
		if err := sched.add(r); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("failed to schedule routine: %w", err)
		}
	}
	m.mu.Unlock()
	return nil
}

// UpdateRoutine applies partial updates to one routine (by ID or name) and
// re-registers it with the live scheduler. Supported mutations:
//   - Enabled: false removes the cron entry; true validates the schedule
//     again and registers it.
//   - Schedule/Prompt/Name: persisted immediately; if the routine is enabled,
//     the cron entry is replaced so changes take effect without a restart.
func (m *Manager) UpdateRoutine(botName, idOrName string, mutate func(*RoutineConfig)) (*RoutineConfig, error) {
	routines, err := m.store.LoadRoutines(botName)
	if err != nil {
		return nil, err
	}
	var target *RoutineConfig
	for _, r := range routines {
		if r.ID == idOrName || strings.EqualFold(r.Name, idOrName) {
			target = r
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("routine not found: %s", idOrName)
	}

	mutate(target)

	if target.Schedule == "" {
		return nil, fmt.Errorf("schedule is required")
	}
	// Validate schedule by dry-parsing (same parser as AddRoutine).
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.SecondOptional)
	if _, err := parser.Parse(target.Schedule); err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", target.Schedule, err)
	}

	if err := m.store.SaveRoutines(botName, routines); err != nil {
		return nil, err
	}

	// Re-register the cron entry so live state matches the stored config.
	key := strings.ToLower(botName)
	m.mu.Lock()
	if sched, ok := m.routines[key]; ok && sched != nil {
		if entryID, exists := sched.entryIDs[target.ID]; exists {
			sched.cron.Remove(entryID)
			delete(sched.entryIDs, target.ID)
		}
		if target.Enabled && target.Schedule != "" {
			if err := sched.add(target); err != nil {
				m.mu.Unlock()
				return nil, fmt.Errorf("failed to schedule routine: %w", err)
			}
		}
	}
	m.mu.Unlock()
	return target, nil
}

// ListRoutines returns all routines configured for a bot.
func (m *Manager) ListRoutines(botName string) ([]*RoutineConfig, error) {
	return m.store.LoadRoutines(botName)
}

// RemoveRoutine deletes a routine by ID or name.
func (m *Manager) RemoveRoutine(botName string, idOrName string) error {
	routines, err := m.store.LoadRoutines(botName)
	if err != nil {
		return err
	}
	var kept []*RoutineConfig
	var removed *RoutineConfig
	for _, r := range routines {
		if r.ID == idOrName || strings.EqualFold(r.Name, idOrName) {
			removed = r
			continue
		}
		kept = append(kept, r)
	}
	if removed == nil {
		return fmt.Errorf("routine not found: %s", idOrName)
	}
	if err := m.store.SaveRoutines(botName, kept); err != nil {
		return err
	}

	// Unschedule if running live.
	m.mu.Lock()
	if sched, ok := m.routines[strings.ToLower(botName)]; ok {
		if entryID, exists := sched.entryIDs[removed.ID]; exists {
			sched.cron.Remove(entryID)
			delete(sched.entryIDs, removed.ID)
		}
	}
	m.mu.Unlock()
	return nil
}
