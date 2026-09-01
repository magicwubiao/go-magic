// Package bot implements Bot Mode: named agent profiles with persistent
// canonical chat sessions, per-bot model/persona configuration, and
// bot-to-bot messaging.
//
// Design (inspired by Hermes Agent's Bot Mode):
//   - A Bot is a profile: isolated config (model/provider/system prompt),
//     a dedicated session ID namespace, and its own routines.
//   - Each Bot has one canonical, persistent "bot chat" conversation that
//     survives gateway restarts (persisted via internal/session store).
//   - Bots can message each other with the message_agent tool; delivery is
//     fire-and-forget into the target Bot's canonical chat queue.
package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Config is the on-disk definition of a single Bot (bots/<name>.json).
type Config struct {
	Name string `json:"name"` // Unique handle; also used as @mention tag

	Title        string `json:"title,omitempty"`         // Short role label, e.g. "Research Assistant"
	Description  string `json:"description,omitempty"`   // What this bot does / when to use it
	SystemPrompt string `json:"system_prompt,omitempty"` // Persona & standing instructions (SOUL.md equivalent)
	Model        string `json:"model,omitempty"`         // Model pin, e.g. "deepseek-chat"; empty = inherit global
	Provider     string `json:"provider,omitempty"`      // Provider pin; empty = inherit global

	// Tools is an allowlist of tool names this bot may call. Empty = inherit
	// the full bot toolset. message_agent (fleet messaging) is always kept.
	Tools []string `json:"tools,omitempty"`
	// Skills is an allowlist of skill names this bot may load. Empty = all.
	Skills []string `json:"skills,omitempty"`
	// Memory is the bot's long-term memory block (markdown), injected into its
	// system prompt — the equivalent of a profile MEMORY.md.
	Memory string `json:"memory,omitempty"`
	// Avatar is a display avatar: an emoji (e.g. "🦊") or image URL.
	Avatar string `json:"avatar,omitempty"`
	// Env holds per-bot credentials/overrides. Written to the bot's isolated
	// workdir (<workdir>/bots/<name>/.env) so tools and scripts run by this bot
	// can source them without leaking into other bots' processes.
	Env map[string]string `json:"env,omitempty"`
	// Hidden removes the bot from the default dashboard list. Hidden bots keep
	// running (routines, DMs, rooms) and are reachable by name; the UI can show
	// them behind a "show hidden" toggle.
	Hidden bool `json:"hidden,omitempty"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// RoutineConfig is a recurring task bound to one Bot.
// Routines are plain cron jobs namespaced "[bot:<name>]" so they also show
// up in `magic cron list`, mirroring Hermes' design.
type RoutineConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`     // Human-readable routine name
	Schedule   string `json:"schedule"` // Cron expression (5 or 6 fields)
	Prompt     string `json:"prompt"`   // Task prompt executed in the bot's context
	Enabled    bool   `json:"enabled"`
	LastRun    *int64 `json:"last_run,omitempty"` // Unix seconds
	LastStatus string `json:"last_status,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// ValidateName checks that a bot name is usable as an @mention tag and filename.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid bot name %q: use letters, digits, '-' or '_' (max 64 chars, must start alphanumeric)", name)
	}
	lower := strings.ToLower(name)
	switch lower {
	case "default", "new", "list", "remove", "delete", "run", "show", "help", "routine", "routines":
		return fmt.Errorf("bot name %q is reserved", name)
	}
	return nil
}

// MentionTag returns the lowercase tag used for @mentions (e.g. "@researcher").
func (c *Config) MentionTag() string {
	tag := strings.ToLower(c.Name)
	tag = strings.ReplaceAll(tag, "_", "-")
	return strings.Trim(tag, "-")
}

// EffectiveSystemPrompt builds the full system prompt for the bot,
// combining its persona, long-term memory, and role metadata.
func (c *Config) EffectiveSystemPrompt() string {
	var sb strings.Builder
	sb.WriteString("You are ")
	if c.Title != "" {
		fmt.Fprintf(&sb, "%s", c.Title)
	} else {
		fmt.Fprintf(&sb, "Bot %s", c.Name)
	}
	if c.Description != "" {
		fmt.Fprintf(&sb, ". Role: %s", c.Description)
	}
	sb.WriteString(".\n\n")
	if c.SystemPrompt != "" {
		sb.WriteString(strings.TrimSpace(c.SystemPrompt))
		sb.WriteString("\n\n")
	}
	if strings.TrimSpace(c.Memory) != "" {
		sb.WriteString("LONG-TERM MEMORY (persistent facts about you, your preferences, and what you have learned):\n")
		sb.WriteString(strings.TrimSpace(c.Memory))
		sb.WriteString("\n\n")
	}
	sb.WriteString("You are running inside Magic's Bot Mode. Stay in character and complete tasks using your available tools.")
	return sb.String()
}

// Store persists bot configs and routines under <magicHome>/bots/.
type Store struct {
	dir string
}

// NewStore creates the bots directory if needed.
func NewStore(magicHome string) (*Store, error) {
	dir := filepath.Join(magicHome, "bots")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bots dir: %w", err)
	}
	s := &Store{dir: dir}
	routinesDir := filepath.Join(dir, "routines")
	if err := os.MkdirAll(routinesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create routines dir: %w", err)
	}
	roomsDir := filepath.Join(dir, "rooms")
	if err := os.MkdirAll(roomsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create rooms dir: %w", err)
	}
	return s, nil
}

// safeBase strips any path separators from a caller-supplied name so file
// paths always stay inside the store's own directories. The API layer also
// validates names up front; this is a defense-in-depth backstop for every
// caller (API, CLI, tests).
func safeBase(name string) string {
	return filepath.Base(name)
}

func (s *Store) botPath(name string) string {
	return filepath.Join(s.dir, safeBase(name)+".json")
}

func (s *Store) routinesPath(name string) string {
	return filepath.Join(s.dir, "routines", safeBase(name)+".json")
}

// Save writes a bot config to disk.
func (s *Store) Save(cfg *Config) error {
	cfg.UpdatedAt = time.Now().Unix()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.botPath(cfg.Name), data, 0644)
}

// Load reads a bot config by name.
func (s *Store) Load(name string) (*Config, error) {
	data, err := os.ReadFile(s.botPath(name))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse bot config %s: %w", name, err)
	}
	return &cfg, nil
}

// Delete removes a bot's config file.
func (s *Store) Delete(name string) error {
	err := os.Remove(s.botPath(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// List returns all bot configs sorted by name.
func (s *Store) List() ([]*Config, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var configs []*Config
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		cfg, err := s.Load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue // Skip corrupted files
		}
		configs = append(configs, cfg)
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	return configs, nil
}

// --- Routines ---

// SaveRoutines persists a bot's routine list.
func (s *Store) SaveRoutines(botName string, routines []*RoutineConfig) error {
	data, err := json.MarshalIndent(routines, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.routinesPath(botName), data, 0644)
}

// LoadRoutines reads a bot's routine list (empty slice if none).
func (s *Store) LoadRoutines(botName string) ([]*RoutineConfig, error) {
	data, err := os.ReadFile(s.routinesPath(botName))
	if err != nil {
		if os.IsNotExist(err) {
			return []*RoutineConfig{}, nil
		}
		return nil, err
	}
	var routines []*RoutineConfig
	if err := json.Unmarshal(data, &routines); err != nil {
		return []*RoutineConfig{}, nil // Corrupt file -> start fresh
	}
	if routines == nil {
		routines = []*RoutineConfig{}
	}
	return routines, nil
}

// NewRoutineID generates a unique routine identifier.
func NewRoutineID(botName string) string {
	id := uuid.New().String()[:8]
	return fmt.Sprintf("bot_%s_%s", botName, id)
}

// RuntimeState tracks a bot's live activity for status display.
type RuntimeState struct {
	Name           string `json:"name"`
	SessionID      string `json:"session_id"`
	QueueDepth     int    `json:"queue_depth"`
	HistoryLength  int    `json:"history_length"`
	ActiveRoutines int    `json:"active_routines"`
	// LastActiveUnix is the Unix-seconds timestamp of the bot's most recent
	// completed turn (0 = never active). Used for the "Active now" UI strip.
	LastActiveUnix int64 `json:"last_active_unix,omitempty"`
}

// --- Group chat rooms ---

// RoomConfig is the on-disk definition of a group chat room (2-6 bots).
type RoomConfig struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Topic   string   `json:"topic,omitempty"`
	Members []string `json:"members"` // bot names (lowercase)
	// MaxRounds caps how many full speaking rounds run per user message
	// (default 3, matching Hermes).
	MaxRounds int `json:"max_rounds,omitempty"`
	// MaxMessages caps how many recent room messages are included in each
	// bot's context prompt (default 10, matching Hermes' 10-message cap).
	MaxMessages int   `json:"max_messages,omitempty"`
	CreatedAt   int64 `json:"created_at"`
	UpdatedAt   int64 `json:"updated_at"`
}

// RoomDefaults mirror Hermes' hard caps.
const (
	DefaultRoomMaxRounds   = 3
	DefaultRoomMaxMessages = 10
	MaxRoomMembers         = 6
	MinRoomMembers         = 2
)

func (r *RoomConfig) Rounds() int {
	if r.MaxRounds <= 0 {
		return DefaultRoomMaxRounds
	}
	return r.MaxRounds
}

func (r *RoomConfig) MessagesCap() int {
	if r.MaxMessages <= 0 {
		return DefaultRoomMaxMessages
	}
	return r.MaxMessages
}

func (s *Store) roomPath(id string) string {
	return filepath.Join(s.dir, "rooms", safeBase(id)+".json")
}

// SaveRoom persists a room config.
func (s *Store) SaveRoom(r *RoomConfig) error {
	r.UpdatedAt = time.Now().Unix()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.roomPath(r.ID), data, 0644)
}

// LoadRoom reads a room config by ID.
func (s *Store) LoadRoom(id string) (*RoomConfig, error) {
	data, err := os.ReadFile(s.roomPath(id))
	if err != nil {
		return nil, err
	}
	var r RoomConfig
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("failed to parse room %s: %w", id, err)
	}
	return &r, nil
}

// DeleteRoom removes a room config file.
func (s *Store) DeleteRoom(id string) error {
	err := os.Remove(s.roomPath(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ListRooms returns all room configs sorted by name.
func (s *Store) ListRooms() ([]*RoomConfig, error) {
	dir := filepath.Join(s.dir, "rooms")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*RoomConfig{}, nil
		}
		return nil, err
	}
	var rooms []*RoomConfig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		r, err := s.LoadRoom(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		rooms = append(rooms, r)
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].Name < rooms[j].Name })
	return rooms, nil
}
