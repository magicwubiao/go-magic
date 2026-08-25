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
// combining its persona with role metadata.
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
	return s, nil
}

func (s *Store) botPath(name string) string {
	return filepath.Join(s.dir, name+".json")
}

func (s *Store) routinesPath(name string) string {
	return filepath.Join(s.dir, "routines", name+".json")
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
}
