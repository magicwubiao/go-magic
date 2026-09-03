package server

import (
	"time"
)

type ShareToken struct {
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	IsDir     bool      `json:"is_dir"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type QRStatus struct {
	Platform  string `json:"platform"`
	Status    string `json:"status"`            // pending, scanning, confirmed, expired, error
	QRCode    string `json:"qr_code,omitempty"` // base64 encoded PNG
	Message   string `json:"message,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"` // seconds remaining
}

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Enabled     bool     `json:"enabled"`
	Source      string   `json:"source"`           // "default" or "user"
	Status      string   `json:"status,omitempty"` // auto-skill status: pending/approved/archived/rejected
}

type ProviderInfo struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	BaseURL string   `json:"base_url"`
	Models  []string `json:"models"`
	APIKey  string   `json:"api_key,omitempty"`
}

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
}

type Session struct {
	ID              string  `json:"id"`
	Profile         string  `json:"profile"`
	Source          string  `json:"source"`
	Model           string  `json:"model"`
	Title           string  `json:"title"`
	WorkDir         string  `json:"work_dir"`
	WorkDirUserSet  bool    `json:"work_dir_user_set"`
	StartedAt       int64   `json:"started_at"`
	EndedAt         *int64  `json:"ended_at"`
	LastActive      int64   `json:"last_active"`
	IsActive        bool    `json:"is_active"`
	MessageCount    int     `json:"message_count"`
	ToolCallCount   int     `json:"tool_call_count"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	Preview         string  `json:"preview"`
	ParentSessionID *string `json:"parent_session_id"`
}

type Message struct {
	ID         string                   `json:"id"`
	SessionID  string                   `json:"session_id"`
	Role       string                   `json:"role"`
	Content    string                   `json:"content"`
	Timestamp  int64                    `json:"timestamp"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolName   string                   `json:"tool_name,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
}

type PlatformStatus struct {
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	State        string `json:"state"`
	UpdatedAt    int64  `json:"updated_at"`
}

type ProgressAnalysis struct {
	SuggestedProgress int    `json:"suggested_progress"`
	Reason            string `json:"reason"`
	Completed         bool   `json:"completed"`
}

type ActionStatus struct {
	Name      string     `json:"name"`
	Running   bool       `json:"running"`
	ExitCode  *int       `json:"exit_code"`
	Lines     []string   `json:"lines"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time,omitempty"`
}

type Toolset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Tools   []string `json:"tools"`
	Enabled bool     `json:"enabled"`
}
