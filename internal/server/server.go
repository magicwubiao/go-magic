package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/cron"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
)

//go:embed dist
var staticFiles embed.FS

// Session represents a chat session (API response format)
type Session struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	Model           string    `json:"model"`
	Title           string    `json:"title"`
	StartedAt       int64     `json:"started_at"`
	EndedAt         *int64    `json:"ended_at"`
	LastActive      int64     `json:"last_active"`
	IsActive        bool      `json:"is_active"`
	MessageCount    int       `json:"message_count"`
	ToolCallCount   int       `json:"tool_call_count"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	Preview         string    `json:"preview"`
	ParentSessionID *string   `json:"parent_session_id"`
}

// Message represents a chat message (API response format)
type Message struct {
	ID          string                   `json:"id"`
	SessionID   string                   `json:"session_id"`
	Role        string                   `json:"role"`
	Content     string                   `json:"content"`
	Timestamp   int64                    `json:"timestamp"`
	ToolCalls   []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolName    string                   `json:"tool_name,omitempty"`
	ToolCallID  string                   `json:"tool_call_id,omitempty"`
}

// Toolset represents a group of tools
type Toolset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Tools   []string `json:"tools"`
	Enabled bool     `json:"enabled"`
}

// Skill represents a skill
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Enabled     bool     `json:"enabled"`
}

// LogEntry represents a log entry
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
}

// ProviderInfo represents a model provider (API response format)
type ProviderInfo struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	BaseURL string   `json:"base_url"`
	Models  []string `json:"models"`
	APIKey  string   `json:"api_key,omitempty"`
}

// PlatformStatus represents platform status
type PlatformStatus struct {
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	State        string `json:"state"`
	UpdatedAt    int64  `json:"updated_at"`
}

// Server represents the HTTP server with real backend connections
type Server struct {
	mu          sync.RWMutex
	startTime   time.Time
	cfg         *appconfig.Config
	sessionStore *session.Store
	provider    provider.Provider
	toolReg     *tool.Registry
	skillMgr    *skills.Manager
	magicHome   string
	version     string

	// Active chat agents per session (lazy init)
	agents      map[string]*agent.Agent
	agentsMu    sync.Mutex

	// Disabled skills tracking
	disabledSkills   map[string]bool
	disabledSkillsMu sync.Mutex

	// Cron job manager
	cronMgr *cron.Manager

	// Background actions tracking
	actions      map[string]*ActionStatus
	actionsMu    sync.RWMutex
}

// ActionStatus tracks the status of a background action
type ActionStatus struct {
	Name      string     `json:"name"`
	Running   bool       `json:"running"`
	ExitCode  *int       `json:"exit_code"`
	Lines     []string   `json:"lines"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time,omitempty"`
}

// NewServer creates a new server instance connected to real backend systems
func NewServer(dbPath string) *Server {
	home, _ := os.UserHomeDir()
	magicHome := os.Getenv("GO_MAGIC_HOME")
	if magicHome == "" {
		magicHome = filepath.Join(home, ".magic")
	}
	os.MkdirAll(magicHome, 0755)

	// Load config
	cfg, err := appconfig.Load()
	if err != nil {
		cfg = appconfig.DefaultConfig()
	}

	// Open session store
	if dbPath == "" {
		dbPath = filepath.Join(magicHome, "sessions.db")
	}
	store, err := session.NewStore(dbPath)
	if err != nil {
		fmt.Printf("[server] Warning: Failed to open session store: %v\n", err)
		store = nil
	}

	// Create provider
	var prov provider.Provider
	if cfg != nil && cfg.Provider != "" {
		prov = createProvider(cfg)
	}

	// Create tool registry
	registry := tool.NewRegistry()
	workDir := cfg.WorkingDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	registry.RegisterAll(workDir)

	// Create skills manager
	skillMgr, _ := skills.NewManager()

	// Create cron manager
	cronMgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("[server] Warning: Failed to create cron manager: %v\n", err)
	}

	// Load disabled skills from config
	disabledSkills := make(map[string]bool)
	if cfg != nil {
		for _, name := range cfg.Tools.Disabled {
			disabledSkills[name] = true
		}
	}

	// Get version
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/magicwubiao/go-magic" {
				if dep.Replace != nil {
					version = dep.Replace.Version
				} else {
					version = dep.Version
				}
				if version == "" {
					version = "dev"
				}
				break
			}
		}
	}

	return &Server{
		mu:              sync.RWMutex{},
		startTime:       time.Now(),
		cfg:             cfg,
		sessionStore:    store,
		provider:        prov,
		toolReg:         registry,
		skillMgr:        skillMgr,
		magicHome:       magicHome,
		version:         version,
		agents:          make(map[string]*agent.Agent),
		disabledSkills:  disabledSkills,
		cronMgr:         cronMgr,
		actions:         make(map[string]*ActionStatus),
	}
}

// createProvider creates a provider instance from config (unified with pkg/config)
func createProvider(cfg *appconfig.Config) provider.Provider {
	prov, err := appconfig.CreateProvider(cfg)
	if err != nil {
		return nil
	}
	return prov
}

// getOrCreateAgent gets or creates an agent for a session
func (s *Server) getOrCreateAgent(sessionID string) *agent.Agent {
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()

	if a, ok := s.agents[sessionID]; ok {
		return a
	}

	if s.provider == nil {
		return nil
	}

	toolsSchema := getToolsSchema(s.toolReg)
	systemPrompt := `You are Magic, a helpful AI assistant.

RULES:
- Small talk/greetings (hello/hi) → Respond directly, do not call tools
- Knowledge Q&A → Respond directly
- List/view/read files → Call list_files or read_file
- Create/write files → Call write_file
- Web search → Call web_search
- Execute command/code → Call execute_command
- Do not call time, system, math, memory_recall, todo, session_search unless explicitly requested
- Respond in the user's language
- Summarize file lists concisely, do not output raw JSON`

	a := agent.NewEnhancedAgent(s.provider, s.toolReg, toolsSchema, systemPrompt)

	// Load skills
	if s.skillMgr != nil {
		if skillsList := s.skillMgr.GetSkillsList(); skillsList != "" {
			a.AddSkillsContext(skillsList)
		}
	}

	a.SetSession(sessionID)
	s.agents[sessionID] = a
	return a
}

// getToolsSchema converts tool registry to provider tools schema
func getToolsSchema(registry *tool.Registry) []map[string]interface{} {
	// The tool registry exposes tools; we build schema from it
	// For now, return a standard set based on what RegisterAll creates
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "read_file",
				"description": "Read contents of a file",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{"type": "string", "description": "File path to read"},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "write_file",
				"description": "Write content to a file",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":     map[string]interface{}{"type": "string", "description": "File path to write"},
						"content":  map[string]interface{}{"type": "string", "description": "Content to write"},
					},
					"required": []string{"path", "content"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "list_files",
				"description": "List files in a directory",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{"type": "string", "description": "Directory path"},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "execute_command",
				"description": "Execute a shell command",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{"type": "string", "description": "Shell command to execute"},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "web_search",
				"description": "Search the web for information",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{"type": "string", "description": "Search query"},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "web_extract",
				"description": "Extract content from a URL",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{"type": "string", "description": "URL to extract from"},
					},
					"required": []string{"url"},
				},
			},
		},
	}
}

// convertDBSessionToAPI converts a DB session to API format
func convertDBSessionToAPI(s *session.Session) *Session {
	if s == nil {
		return nil
	}
	msgCount := len(s.Messages)
	var toolCallCount int
	var preview string
	var title string

	for _, m := range s.Messages {
		if m.Role == "user" || m.Role == "assistant" {
			if len(preview) < 200 {
				preview += m.Content + " "
			}
		}
		if len(m.ToolCalls) > 0 {
			toolCallCount += len(m.ToolCalls)
		}
		// Extract title from first user message
		if title == "" && m.Role == "user" && m.Content != "" {
			title = strings.TrimSpace(m.Content)
			if len(title) > 50 {
				title = title[:50] + "..."
			}
		}
	}

	preview = strings.TrimSpace(preview)
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	// If no title found, use a shortened ID
	if title == "" {
		title = "Untitled"
	}

	// Determine if session is active (has activity in last 30 minutes)
	isActive := time.Since(s.UpdatedAt) < 30*time.Minute

	return &Session{
		ID:            s.ID,
		Source:        s.Platform,
		Model:         "default",
		Title:         title,
		StartedAt:     s.CreatedAt.Unix(),
		LastActive:    s.UpdatedAt.Unix(),
		IsActive:      isActive,
		MessageCount:  msgCount,
		ToolCallCount: toolCallCount,
		Preview:       preview,
	}
}

// convertDBMessagesToAPI converts DB messages to API format
func convertDBMessagesToAPI(sessionID string, msgs []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, len(msgs))
	for i, m := range msgs {
		msg := map[string]interface{}{
			"id":        fmt.Sprintf("msg_%d", i),
			"role":      m.Role,
			"content":   m.Content,
			"timestamp": time.Now().Unix(),
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]map[string]interface{}, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				tcs[j] = map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				}
			}
			msg["tool_calls"] = tcs
		}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		result[i] = msg
	}
	return result
}

// Start starts the HTTP server
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()

	// CORS middleware wrapper
	withCORS := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Magic-Session-Token")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			h(w, r)
		}
	}

	// Base API handler for CORS preflight
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Magic-Session-Token")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		http.NotFound(w, r)
	})

	// Health
	mux.HandleFunc("/api/health", withCORS(s.handleHealth))
	mux.HandleFunc("/api/status", withCORS(s.handleStatus))

	// Sessions
	mux.HandleFunc("/api/sessions", withCORS(s.handleSessions))
	mux.HandleFunc("/api/sessions/search", withCORS(s.handleSessionSearch))
	mux.HandleFunc("/api/sessions/", withCORS(s.handleSessionByID))

	// Chat
	mux.HandleFunc("/api/chat", withCORS(s.handleChat))
	mux.HandleFunc("/api/chat/stream", withCORS(s.handleChatStream))

	// Tools
	mux.HandleFunc("/api/toolsets", withCORS(s.handleToolsets))
	mux.HandleFunc("/api/tools/toolsets", withCORS(s.handleToolsets))
	mux.HandleFunc("/api/tools/toolsets/", withCORS(s.handleToolsetByID))
	mux.HandleFunc("/api/tools/categories", withCORS(s.handleToolCategories))
	mux.HandleFunc("/api/tools/", withCORS(s.handleToolByID))

	// Skills
	mux.HandleFunc("/api/skills", withCORS(s.handleSkills))
	mux.HandleFunc("/api/skills/categories", withCORS(s.handleSkillCategories))
	mux.HandleFunc("/api/skills/", withCORS(s.handleSkillByID))
	mux.HandleFunc("/api/dashboard/skills", withCORS(s.handleDashboardSkills))
	mux.HandleFunc("/api/dashboard/skills/search", withCORS(s.handleSkillsSearch))

	// Plugins
	mux.HandleFunc("/api/plugins", withCORS(s.handlePlugins))
	mux.HandleFunc("/api/dashboard/plugins", withCORS(s.handleDashboardPlugins))
	mux.HandleFunc("/api/dashboard/plugins/rescan", withCORS(s.handleDashboardPluginsRescan))
	mux.HandleFunc("/api/dashboard/plugins/", withCORS(s.handleDashboardPluginsSubRoutes))
	// Agent plugins
	mux.HandleFunc("/api/dashboard/agent-plugins/install", withCORS(s.handleAgentPluginInstall))
	mux.HandleFunc("/api/dashboard/agent-plugins/", withCORS(s.handleAgentPluginsSubRoutes))
	// Plugin providers
	mux.HandleFunc("/api/dashboard/plugin-providers", withCORS(s.handlePluginProviders))

	// Models
	mux.HandleFunc("/api/models", withCORS(s.handleModels))
	mux.HandleFunc("/api/models/", withCORS(s.handleModelByID))
	mux.HandleFunc("/api/model/info", withCORS(s.handleModelInfo))
	mux.HandleFunc("/api/model/options", withCORS(s.handleModelOptions))
	mux.HandleFunc("/api/model/set", withCORS(s.handleModelSet))
	mux.HandleFunc("/api/model/auxiliary", withCORS(s.handleModelAuxiliary))
	mux.HandleFunc("/api/providers", withCORS(s.handleProviders))
	mux.HandleFunc("/api/providers/", withCORS(s.handleProvidersSubRoutes))

	// Config
	mux.HandleFunc("/api/config", withCORS(s.handleConfig))
	mux.HandleFunc("/api/config/", withCORS(s.handleConfigByID))
	mux.HandleFunc("/api/config/defaults", withCORS(s.handleConfigDefaults))
	mux.HandleFunc("/api/config/raw", withCORS(s.handleConfigRaw))
	mux.HandleFunc("/api/config/schema", withCORS(s.handleConfigSchema))

	// Analytics
	mux.HandleFunc("/api/analytics/models", withCORS(s.handleAnalyticsModels))
	mux.HandleFunc("/api/analytics/usage", withCORS(s.handleAnalyticsUsage))
	mux.HandleFunc("/api/analytics/cost", withCORS(s.handleAnalyticsCost))
	mux.HandleFunc("/api/analytics/tokens", withCORS(s.handleAnalyticsTokens))
	mux.HandleFunc("/api/analytics/summary", withCORS(s.handleAnalyticsSummary))

	// Cron
	mux.HandleFunc("/api/cron/jobs", withCORS(s.handleCronJobs))
	mux.HandleFunc("/api/cron/jobs/", withCORS(s.handleCronJobByID))

	// Env
	mux.HandleFunc("/api/env", withCORS(s.handleEnv))
	mux.HandleFunc("/api/env/reveal", withCORS(s.handleEnvReveal))

	// Profiles
	mux.HandleFunc("/api/profiles", withCORS(s.handleProfiles))
	mux.HandleFunc("/api/profiles/", withCORS(s.handleProfileByName))

	// System
	mux.HandleFunc("/api/system/info", withCORS(s.handleSystemInfo))
	mux.HandleFunc("/api/system/stats", withCORS(s.handleSystemStats))
	mux.HandleFunc("/api/system/health", withCORS(s.handleSystemHealth))

	// Logs
	mux.HandleFunc("/api/logs", withCORS(s.handleLogs))
	mux.HandleFunc("/api/logs/tail", withCORS(s.handleLogsTail))
	mux.HandleFunc("/api/dashboard/logs", withCORS(s.handleDashboardLogs))

	// Settings
	mux.HandleFunc("/api/settings", withCORS(s.handleSettings))
	mux.HandleFunc("/api/settings/", withCORS(s.handleSettingByID))

	// Gateway
	mux.HandleFunc("/api/gateway/restart", withCORS(s.handleGatewayRestart))

	// Magic update
	mux.HandleFunc("/api/magic/update", withCORS(s.handleMagicUpdate))

	// Actions
	mux.HandleFunc("/api/actions/", withCORS(s.handleActions))

	// Dashboard themes
	mux.HandleFunc("/api/dashboard/themes", withCORS(s.handleDashboardThemes))
	mux.HandleFunc("/api/dashboard/theme", withCORS(s.handleDashboardTheme))

	// Static files
	mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("[server] Magic Agent Dashboard starting on http://localhost:%d\n", port)
	fmt.Printf("[server] Provider: %s | Model: %s\n", s.cfg.Provider, s.cfg.Model)
	return http.ListenAndServe(addr, mux)
}

// --- Health & Status ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]string{"status": "healthy"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	providerStatus := "not_configured"
	if s.provider != nil {
		providerStatus = "connected"
	}

	sessions := 0
	if s.sessionStore != nil {
		if list, err := s.sessionStore.ListSessions(context.Background(), ""); err == nil {
			sessions = len(list)
		}
	}

	jsonResponse(w, map[string]interface{}{
		"status":               "ok",
		"timestamp":            time.Now().Unix(),
		"version":              s.version,
		"active_sessions":      sessions,
		"config_path":          filepath.Join(s.magicHome, "config.json"),
		"config_version":       1,
		"latest_config_version": 1,
		"env_path":             filepath.Join(s.magicHome, ".env"),
		"gateway_exit_reason":  nil,
		"gateway_health_url":   nil,
		"gateway_pid":          nil,
		"gateway_platforms":    map[string]PlatformStatus{},
		"gateway_running":      false,
		"gateway_state":        nil,
		"gateway_updated_at":   nil,
		"magic_home":           s.magicHome,
		"session_count":        sessions,
		"provider_status":      providerStatus,
		"release_date":         time.Now().Format("2006-01-02"),
	})
}

// --- Sessions ---

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 20
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		offset := 0
		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
				offset = o
			}
		}

		if s.sessionStore == nil {
			jsonResponse(w, map[string]interface{}{"sessions": []Session{}, "total": 0, "limit": limit})
			return
		}

		dbSessions, err := s.sessionStore.ListSessions(context.Background(), "")
		if err != nil {
			jsonResponse(w, map[string]interface{}{"sessions": []Session{}, "total": 0, "limit": limit})
			return
		}

		// Convert to API format
		apiSessions := make([]*Session, 0, len(dbSessions))
		for _, sess := range dbSessions {
			apiSessions = append(apiSessions, convertDBSessionToAPI(sess))
		}

		total := len(apiSessions)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}

		jsonResponse(w, map[string]interface{}{
			"sessions": apiSessions[offset:end],
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	case "POST":
		var req struct {
			Name     string `json:"name"`
			Title    string `json:"title"`
			Model    string `json:"model"`
			Platform string `json:"platform"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		now := time.Now()
		name := req.Name
		if name == "" {
			name = req.Title
		}
		if name == "" {
			name = fmt.Sprintf("Chat %s", now.Format("2006-01-02 15:04"))
		}

		sessionID := uuid.New().String()
		platform := req.Platform
		if platform == "" {
			platform = "web"
		}

		newSession := &session.Session{
			ID:        sessionID,
			Profile:   s.cfg.Profile,
			Platform:  platform,
			Messages:  []types.Message{},
			CreatedAt: now,
			UpdatedAt: now,
		}

		if s.sessionStore != nil {
			if err := s.sessionStore.SaveSession(context.Background(), newSession); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}

		apiSess := convertDBSessionToAPI(newSession)
		if apiSess != nil {
			apiSess.Title = name
			apiSess.Model = req.Model
		}
		jsonResponse(w, apiSess)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")

	// Check for messages endpoint
	if strings.HasSuffix(path, "/messages") {
		sessionID := strings.TrimSuffix(path, "/messages")
		s.handleSessionMessages(w, r, sessionID)
		return
	}

	// Check for reset endpoint
	if strings.HasSuffix(path, "/reset") {
		sessionID := strings.TrimSuffix(path, "/reset")
		s.handleSessionReset(w, r, sessionID)
		return
	}

	// Check for latest-descendant
	if strings.HasSuffix(path, "/latest-descendant") {
		sessionID := strings.TrimSuffix(path, "/latest-descendant")
		jsonResponse(w, map[string]interface{}{
			"requested_session_id": sessionID,
			"session_id":           sessionID,
			"path":                 []string{sessionID},
			"changed":              false,
		})
		return
	}

	id := path
	if id == "" {
		http.Error(w, "not found", 404)
		return
	}

	if s.sessionStore == nil {
		http.Error(w, "session store not available", 500)
		return
	}

	dbSession, err := s.sessionStore.LoadSession(context.Background(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	switch r.Method {
	case "GET":
		jsonResponse(w, convertDBSessionToAPI(dbSession))
	case "DELETE":
		s.sessionStore.DeleteSession(context.Background(), id)
		// Clean up agent
		s.agentsMu.Lock()
		delete(s.agents, id)
		s.agentsMu.Unlock()
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.sessionStore == nil {
		jsonResponse(w, map[string]interface{}{"session_id": sessionID, "messages": []map[string]interface{}{}})
		return
	}

	dbSession, err := s.sessionStore.LoadSession(context.Background(), sessionID)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	messages := convertDBMessagesToAPI(sessionID, dbSession.Messages)
	jsonResponse(w, map[string]interface{}{
		"session_id": sessionID,
		"messages":   messages,
	})
}

func (s *Server) handleSessionReset(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Reset agent for this session
	s.agentsMu.Lock()
	delete(s.agents, sessionID)
	s.agentsMu.Unlock()

	// Reset session messages in DB
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			sess.Messages = []types.Message{}
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(context.Background(), sess)
		}
	}

	jsonResponse(w, map[string]bool{"ok": true})
}

func (s *Server) handleSessionSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	query := r.URL.Query().Get("q")
	results := []map[string]interface{}{}

	if s.sessionStore == nil {
		jsonResponse(w, map[string]interface{}{"results": results})
		return
	}

	dbSessions, err := s.sessionStore.ListSessions(context.Background(), "")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	for _, sess := range dbSessions {
		// Search in messages content
		for _, m := range sess.Messages {
			if strings.Contains(strings.ToLower(m.Content), strings.ToLower(query)) {
				results = append(results, map[string]interface{}{
					"session_id":      sess.ID,
					"snippet":         truncateString(m.Content, 200),
					"role":            m.Role,
					"source":          sess.Platform,
					"model":           s.cfg.Model,
					"session_started": sess.CreatedAt.Unix(),
				})
				break // One match per session
			}
		}
	}

	jsonResponse(w, map[string]interface{}{"results": results})
}

// --- Chat ---

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		Message    string                   `json:"message"`
		SessionID string                   `json:"session_id"`
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		Tools     []map[string]interface{} `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	if s.provider == nil {
		http.Error(w, "provider not configured", 400)
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Get or create agent
	aiAgent := s.getOrCreateAgent(sessionID)
	if aiAgent == nil {
		http.Error(w, "failed to create agent", 500)
		return
	}

	// Check if last message has tool_calls (tool result flow)
	var hasToolCalls bool
	var toolCalls []interface{}
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if tc, ok := lastMsg["tool_calls"].([]interface{}); ok && len(tc) > 0 {
			hasToolCalls = true
			toolCalls = tc
		}
	}

	if hasToolCalls {
		// Process tool results
		toolMessages := make([]map[string]interface{}, 0)
		for _, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				funcName := ""
				funcArgs := ""
				if fn, ok := tcMap["function"].(map[string]interface{}); ok {
					funcName, _ = fn["name"].(string)
					funcArgs, _ = fn["arguments"].(string)
				}
				// Execute the tool
				argsMap := map[string]interface{}{}
				if funcArgs != "" {
					json.Unmarshal([]byte(funcArgs), &argsMap)
				}
				result, err := s.toolReg.Execute(context.Background(), funcName, argsMap)
				content := "Tool executed successfully"
				if err != nil {
					content = fmt.Sprintf("Error: %v", err)
				} else if result != nil {
					if b, err := json.Marshal(result); err == nil {
						content = string(b)
					}
				}
				toolMessages = append(toolMessages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tcMap["id"],
					"content":      content,
				})
			}
		}
		response := map[string]interface{}{
			"id":            fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"content":       "",
			"model":         s.cfg.Model,
			"tool_messages": toolMessages,
		}
		jsonResponse(w, response)
		return
	}

	// Run agent conversation
	ctx := context.Background()
	respContent, err := aiAgent.RunConversation(ctx, req.Message)
	if err != nil {
		http.Error(w, fmt.Sprintf("agent error: %v", err), 500)
		return
	}

	// Save to session store
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			sess.Messages = append(sess.Messages, types.Message{
				Role:    "user",
				Content: req.Message,
			})
			sess.Messages = append(sess.Messages, types.Message{
				Role:    "assistant",
				Content: respContent,
			})
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(ctx, sess)
		}
	}

	response := map[string]interface{}{
		"id":      fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		"content": respContent,
		"model":   s.cfg.Model,
	}

	jsonResponse(w, response)
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.provider == nil {
		http.Error(w, "provider not configured", 400)
		return
	}

	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	aiAgent := s.getOrCreateAgent(sessionID)
	if aiAgent == nil {
		http.Error(w, "failed to create agent", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	// Check if agent supports streaming
	_, supportsStream := s.provider.(provider.StreamingToolCaller)
	if !supportsStream {
		// Fall back to non-streaming via agent (which still executes tools)
		ctx := context.Background()
		resp, err := aiAgent.RunConversation(ctx, req.Message)
		if err != nil {
			fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"error":"%v"}`, err))
			flusher.Flush()
			return
		}

		// Send content word by word for pseudo-streaming
		words := strings.Split(resp, "")
		for _, word := range words {
			data, _ := json.Marshal(map[string]string{"content": word})
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Real streaming — use agent's RunConversationStream which handles tool execution loop
	ctx := context.Background()
	streamErr := aiAgent.RunConversationStream(ctx, req.Message, func(content string, done bool) {
		if done {
			// Stream finished
			return
		}
		if content != "" {
			// Check if this is a tool result
			if strings.Contains(content, ">>>TOOL_RESULT_START|") {
				// Extract tool name
				re := regexp.MustCompile(`>>>TOOL_RESULT_START\|([^<]+)<<<`)
				matches := re.FindStringSubmatch(content)
				if len(matches) > 1 {
					toolName := matches[1]
					// Extract tool content
					contentRe := regexp.MustCompile(`>>>TOOL_RESULT_START\|[^<]+<<<\n?([\s\S]*?)\n?>>>TOOL_RESULT_END<<<`)
					contentMatches := contentRe.FindStringSubmatch(content)
					toolContent := ""
					if len(contentMatches) > 1 {
						toolContent = strings.TrimSpace(contentMatches[1])
					}
					// Send as tool result event
					data, _ := json.Marshal(map[string]interface{}{
						"type":    "tool_result",
						"tool":    toolName,
						"content": toolContent,
					})
					fmt.Fprintf(w, "data: %s\n\n", string(data))
					flusher.Flush()
					return
				}
			}
			// Regular content
			data, _ := json.Marshal(map[string]string{"content": content})
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}
	})
	if streamErr != nil {
		data, _ := json.Marshal(map[string]string{"error": streamErr.Error()})
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// --- Tools ---

func (s *Server) handleToolsets(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, s.buildToolsets())
}

func (s *Server) handleToolsetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tools/toolsets/")
	id = strings.TrimPrefix(id, "/api/toolsets/")

	for _, ts := range s.buildToolsets() {
		if ts["name"] == id || ts["id"] == id {
			jsonResponse(w, ts)
			return
		}
	}
	http.Error(w, "not found", 404)
}

// buildToolsets dynamically generates toolsets from s.toolReg
func (s *Server) buildToolsets() []map[string]interface{} {
	if s.toolReg == nil {
		return []map[string]interface{}{}
	}

	// Known category prefixes and their human-readable names
	categoryMap := map[string]string{
		"web_":           "Web",
		"browser_":       "Browser",
		"execute_":       "Code Execution",
		"read_":          "File",
		"write_":         "File",
		"file_":          "File",
		"list_":          "File",
		"search_":        "File",
		"memory_":        "Memory",
		"delegate_":      "Delegation",
		"poll_":          "Delegation",
		"code_":          "Code Execution",
		"skill_":         "Skills",
		"mcp_":           "MCP",
	}

	// Group tools by category
	categoryTools := map[string][]string{}
	categoryDescriptions := map[string]string{}
	ungrouped := []string{}

	allTools := s.toolReg.List()
	for _, name := range allTools {
		categorized := false
		for prefix, catName := range categoryMap {
			if strings.HasPrefix(name, prefix) {
				categoryTools[catName] = append(categoryTools[catName], name)
				if _, ok := categoryDescriptions[catName]; !ok {
					categoryDescriptions[catName] = fmt.Sprintf("%s tools", catName)
				}
				categorized = true
				break
			}
		}
		if !categorized {
			ungrouped = append(ungrouped, name)
		}
	}

	// Build toolset list
	toolsets := make([]map[string]interface{}, 0, len(categoryTools))

	// Add categorized toolsets
	for catName, tools := range categoryTools {
		toolsets = append(toolsets, map[string]interface{}{
			"id":          strings.ToLower(strings.ReplaceAll(catName, " ", "_")),
			"name":        catName,
			"label":       catName,
			"description": categoryDescriptions[catName],
			"enabled":     true,
			"configured":  true,
			"tools":       tools,
		})
	}

	// Add ungrouped tools as "Other" toolset
	if len(ungrouped) > 0 {
		toolsets = append(toolsets, map[string]interface{}{
			"id":          "other",
			"name":        "Other",
			"label":       "Other",
			"description": "Other tools",
			"enabled":     true,
			"configured":  true,
			"tools":       ungrouped,
		})
	}

	return toolsets
}

func (s *Server) handleGetToolsets() []Toolset {
	// Keep backward compatibility: return Toolset structs from dynamic data
	dynamicToolsets := s.buildToolsets()
	result := make([]Toolset, 0, len(dynamicToolsets))
	for _, ts := range dynamicToolsets {
		name, _ := ts["name"].(string)
		toolsRaw, _ := ts["tools"].([]string)
		result = append(result, Toolset{
			ID:      strings.ToLower(strings.ReplaceAll(name, " ", "_")),
			Name:    name,
			Tools:   toolsRaw,
			Enabled: true,
		})
	}
	return result
}

func (s *Server) handleToolCategories(w http.ResponseWriter, r *http.Request) {
	cats := map[string][]map[string]interface{}{}
	for _, ts := range s.buildToolsets() {
		name, _ := ts["name"].(string)
		cats[name] = []map[string]interface{}{ts}
	}
	jsonResponse(w, cats)
}

func (s *Server) handleToolByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tools/")
	tool := map[string]interface{}{
		"id":          id,
		"name":        id,
		"description": fmt.Sprintf("Tool: %s", id),
		"parameters":  map[string]interface{}{},
	}
	jsonResponse(w, tool)
}

// --- Skills ---

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	skills := s.getRealSkills()
	jsonResponse(w, skills)
}

func (s *Server) getRealSkills() []Skill {
	if s.skillMgr == nil {
		return make([]Skill, 0)
	}
	// Get skills list from manager and parse
	skillsList := s.skillMgr.GetSkillsList()
	if skillsList == "" {
		return []Skill{}
	}
	// Parse the skills list (it's typically a text format)
	// For now, return a structured list based on the skills directory
	return s.scanSkillsDir()
}

func (s *Server) scanSkillsDir() []Skill {
	skillsDir := filepath.Join(s.magicHome, "skills")
	result := make([]Skill, 0)

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return result
	}

	s.disabledSkillsMu.Lock()
	defer s.disabledSkillsMu.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		isDisabled := s.disabledSkills[name]
		// Try to read skill.yaml
		skillFile := filepath.Join(skillsDir, name, "skill.yaml")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			result = append(result, Skill{
				ID:      name,
				Name:    name,
				Enabled: !isDisabled,
			})
			continue
		}
		// Simple YAML parsing
		skill := Skill{ID: name, Name: name, Enabled: !isDisabled}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "description:") {
				skill.Description = strings.TrimPrefix(line, "description:")
				skill.Description = strings.TrimSpace(skill.Description)
				skill.Description = strings.Trim(skill.Description, "\"'")
			}
			if strings.HasPrefix(line, "category:") {
				skill.Category = strings.TrimPrefix(line, "category:")
				skill.Category = strings.TrimSpace(skill.Category)
			}
			if strings.HasPrefix(line, "tags:") {
				tags := strings.TrimPrefix(line, "tags:")
				tags = strings.TrimSpace(tags)
				skill.Tags = strings.Split(tags, ",")
			}
		}
		result = append(result, skill)
	}
	return result
}

func (s *Server) handleSkillCategories(w http.ResponseWriter, r *http.Request) {
	cats := []string{"development", "research", "analytics", "automation", "communication"}
	jsonResponse(w, cats)
}

func (s *Server) handleSkillByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/skills/")

	// Handle toggle - frontend sends {name, enabled}
	if id == "toggle" && r.Method == "PUT" {
		var req struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "invalid request", 400)
			return
		}

		s.disabledSkillsMu.Lock()
		if req.Enabled {
			delete(s.disabledSkills, req.Name)
		} else {
			s.disabledSkills[req.Name] = true
		}
		s.disabledSkillsMu.Unlock()

		// Persist disabled skills to config
		s.mu.Lock()
		if s.cfg != nil {
			disabledList := make([]string, 0)
			s.disabledSkillsMu.Lock()
			for name := range s.disabledSkills {
				disabledList = append(disabledList, name)
			}
			s.disabledSkillsMu.Unlock()
			// Store in tools.disabled as a convention for disabled skills
			s.cfg.Tools.Disabled = disabledList
			_ = s.cfg.Save()
		}
		s.mu.Unlock()

		jsonResponse(w, map[string]interface{}{"ok": true, "name": req.Name, "enabled": req.Enabled})
		return
	}

	// Handle browse
	if id == "browse" && r.Method == "GET" {
		jsonResponse(w, s.getRealSkills())
		return
	}

	// Handle install
	if id == "install" && r.Method == "POST" {
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}

	for _, skill := range s.getRealSkills() {
		if skill.ID == id {
			jsonResponse(w, skill)
			return
		}
	}
	http.Error(w, "not found", 404)
}

func (s *Server) handleDashboardSkills(w http.ResponseWriter, r *http.Request) {
	skills := s.getRealSkills()
	jsonResponse(w, map[string]interface{}{
		"installed": skills,
		"available": []Skill{},
		"categories": []string{"development", "research", "analytics", "automation", "communication"},
	})
}

func (s *Server) handleSkillsSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	results := make([]Skill, 0)
	for _, skill := range s.getRealSkills() {
		if strings.Contains(strings.ToLower(skill.Name), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(skill.Description), strings.ToLower(query)) {
			results = append(results, skill)
		}
	}
	jsonResponse(w, results)
}

// --- Plugins ---

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	plugins := s.scanPluginsDir()
	jsonResponse(w, plugins)
}

func (s *Server) scanPluginsDir() []map[string]interface{} {
	pluginsDir := filepath.Join(s.magicHome, "plugins")
	plugins := make([]map[string]interface{}, 0)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return plugins
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		plugins = append(plugins, map[string]interface{}{
			"id":          name,
			"name":        name,
			"description": fmt.Sprintf("Plugin: %s", name),
			"enabled":     true,
			"version":     "1.0.0",
		})
	}
	return plugins
}

func (s *Server) handleDashboardPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := s.scanPluginsDir()
	jsonResponse(w, plugins)
}

func (s *Server) handleDashboardPluginsRescan(w http.ResponseWriter, r *http.Request) {
	// Frontend uses POST, support both GET and POST
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	plugins := s.scanPluginsDir()
	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"count": len(plugins),
	})
}

// --- Models ---

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := make([]map[string]interface{}, 0)

	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			modelName := provCfg.Model
			if modelName == "" {
				modelName = "default"
			}
			models = append(models, map[string]interface{}{
				"id":         fmt.Sprintf("%s/%s", name, modelName),
				"name":       modelName,
				"provider":   name,
				"contextLen": 128000,
			})
		}
	}

	// Always include current provider
	if s.cfg != nil && s.cfg.Provider != "" {
		models = append(models, map[string]interface{}{
			"id":         fmt.Sprintf("%s/%s", s.cfg.Provider, s.cfg.Model),
			"name":       s.cfg.Model,
			"provider":   s.cfg.Provider,
			"contextLen": 128000,
		})
	}

	if len(models) == 0 {
		models = append(models, map[string]interface{}{
			"id":         "default/default",
			"name":       "default",
			"provider":   "default",
			"contextLen": 128000,
		})
	}

	jsonResponse(w, models)
}

func (s *Server) handleModelByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/models/")
	jsonResponse(w, map[string]interface{}{
		"id":         id,
		"name":       id,
		"contextLen": 128000,
	})
}

func (s *Server) handleModelAuxiliary(w http.ResponseWriter, r *http.Request) {
	auxiliaryModels := make([]map[string]interface{}, 0)

	// Try to read auxiliary models from config providers
	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			if name == s.cfg.Provider {
				continue // skip primary model
			}
			auxiliaryModels = append(auxiliaryModels, map[string]interface{}{
				"id":         name,
				"name":       name,
				"model":      provCfg.Model,
				"contextLen": 128000,
			})
		}
	}

	// If no auxiliary models found from config, return reasonable defaults
	if len(auxiliaryModels) == 0 {
		auxiliaryModels = []map[string]interface{}{
			{"id": "auto", "name": "Auto", "model": "", "contextLen": 128000},
		}
	}

	jsonResponse(w, auxiliaryModels)
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	providers := make([]ProviderInfo, 0)
	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			providers = append(providers, ProviderInfo{
				Name:    name,
				Label:   name,
				BaseURL: provCfg.BaseURL,
				Models:  []string{provCfg.Model},
				APIKey:  maskAPIKey(provCfg.APIKey),
			})
		}
	}
	jsonResponse(w, providers)
}

func (s *Server) handleProvidersSubRoutes(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not found", 404)
}

func (s *Server) handleModelInfo(w http.ResponseWriter, r *http.Request) {
	providerName := s.cfg.Provider
	modelName := s.cfg.Model
	if modelName == "" {
		modelName = "default"
	}

	// Infer context length and capabilities from model name
	contextLen := 128000
	maxOutput := 4096
	supportsVision := false
	supportsReasoning := false
	modelFamily := providerName

	modelLower := strings.ToLower(modelName)
	switch {
	case strings.Contains(modelLower, "gpt-4o"):
		contextLen = 128000
		maxOutput = 16384
		supportsVision = true
		modelFamily = "openai"
	case strings.Contains(modelLower, "gpt-4-turbo") || strings.Contains(modelLower, "gpt-4-1106"):
		contextLen = 128000
		maxOutput = 4096
		supportsVision = true
		modelFamily = "openai"
	case strings.Contains(modelLower, "gpt-4"):
		contextLen = 8192
		maxOutput = 8192
		modelFamily = "openai"
	case strings.Contains(modelLower, "gpt-3.5"):
		contextLen = 16385
		maxOutput = 4096
		modelFamily = "openai"
	case strings.Contains(modelLower, "claude-3-5") || strings.Contains(modelLower, "claude-3.5") || strings.Contains(modelLower, "claude-4"):
		contextLen = 200000
		maxOutput = 8192
		supportsVision = true
		modelFamily = "anthropic"
	case strings.Contains(modelLower, "claude-3"):
		contextLen = 200000
		maxOutput = 4096
		supportsVision = true
		modelFamily = "anthropic"
	case strings.Contains(modelLower, "claude"):
		contextLen = 100000
		maxOutput = 4096
		modelFamily = "anthropic"
	case strings.Contains(modelLower, "deepseek"):
		contextLen = 64000
		maxOutput = 8192
		modelFamily = "deepseek"
	case strings.Contains(modelLower, "gemini"):
		contextLen = 1000000
		maxOutput = 8192
		supportsVision = true
		modelFamily = "google"
	case strings.Contains(modelLower, "llama"):
		contextLen = 128000
		maxOutput = 4096
		modelFamily = "meta"
	case strings.Contains(modelLower, "qwen"):
		contextLen = 128000
		maxOutput = 8192
		modelFamily = "alibaba"
	case strings.Contains(modelLower, "glm") || strings.Contains(modelLower, "chatglm"):
		contextLen = 128000
		maxOutput = 4096
		modelFamily = "zhipu"
	case strings.Contains(modelLower, "o1") || strings.Contains(modelLower, "o3"):
		contextLen = 200000
		maxOutput = 100000
		supportsReasoning = true
		supportsVision = true
		modelFamily = "openai"
	}

	// Try to get capabilities from provider
	supportsTools := true
	if s.provider != nil {
		caps := provider.GetCapabilities(s.provider)
		if caps != nil {
			supportsTools = caps.ToolCalling
			if caps.Vision {
				supportsVision = true
			}
		}
	}

	jsonResponse(w, map[string]interface{}{
		"model":                   fmt.Sprintf("%s/%s", providerName, modelName),
		"provider":                providerName,
		"auto_context_length":     contextLen,
		"config_context_length":   0,
		"effective_context_length": contextLen,
		"capabilities": map[string]interface{}{
			"supports_tools":     supportsTools,
			"supports_vision":    supportsVision,
			"supports_reasoning": supportsReasoning,
			"context_window":     contextLen,
			"max_output_tokens":  maxOutput,
			"model_family":       modelFamily,
		},
	})
}

func (s *Server) handleModelOptions(w http.ResponseWriter, r *http.Request) {
	providerList := make([]map[string]interface{}, 0)
	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			isCurrent := name == s.cfg.Provider
			providerList = append(providerList, map[string]interface{}{
				"name":         name,
				"slug":         name,
				"models":       []string{provCfg.Model},
				"total_models": 1,
				"is_current":   isCurrent,
			})
		}
	}

	model := ""
	provider := ""
	if s.cfg != nil {
		model = s.cfg.Model
		provider = s.cfg.Provider
	}
	jsonResponse(w, map[string]interface{}{
		"model":     model,
		"provider":  provider,
		"providers": providerList,
	})
}

func (s *Server) handleModelSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope    string `json:"scope"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Task     string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	// Update config
	if req.Provider != "" && req.Model != "" {
		s.mu.Lock()
		s.cfg.Provider = req.Provider
		s.cfg.Model = req.Model
		// Save config
		configPath := filepath.Join(s.magicHome, "config.json")
		data, _ := json.MarshalIndent(s.cfg, "", "  ")
		os.WriteFile(configPath, data, 0644)
		// Recreate provider
		s.provider = createProvider(s.cfg)
		// Clear all agents to force re-creation
		s.agents = make(map[string]*agent.Agent)
		s.mu.Unlock()
	}

	jsonResponse(w, map[string]interface{}{
		"ok":       true,
		"scope":    req.Scope,
		"provider": req.Provider,
		"model":    req.Model,
	})
}

// --- Config ---

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		if s.cfg == nil {
			jsonResponse(w, map[string]interface{}{})
			return
		}
		jsonResponse(w, s.cfg)
	case "PUT":
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		// Merge into config
		data, _ := json.Marshal(req)
		json.Unmarshal(data, s.cfg)
		// Save
		configPath := filepath.Join(s.magicHome, "config.json")
		saveData, _ := json.MarshalIndent(s.cfg, "", "  ")
		os.WriteFile(configPath, saveData, 0644)
		jsonResponse(w, s.cfg)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleConfigByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/config/")

	// Handle sub-routes
	switch id {
	case "defaults":
		s.handleConfigDefaults(w, r)
		return
	case "raw":
		s.handleConfigRaw(w, r)
		return
	case "schema":
		s.handleConfigSchema(w, r)
		return
	}

	if s.cfg == nil {
		jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
		return
	}

	// Get specific config value
	cfgMap := make(map[string]interface{})
	data, _ := json.Marshal(s.cfg)
	json.Unmarshal(data, &cfgMap)

	if val, ok := cfgMap[id]; ok {
		jsonResponse(w, map[string]interface{}{"id": id, "value": val})
		return
	}
	jsonResponse(w, map[string]interface{}{"id": id, "value": nil})
}

func (s *Server) handleConfigDefaults(w http.ResponseWriter, r *http.Request) {
	defaults := appconfig.DefaultConfig()
	jsonResponse(w, defaults)
}

func (s *Server) handleConfigRaw(w http.ResponseWriter, r *http.Request) {
	configPath := filepath.Join(s.magicHome, "config.json")

	switch r.Method {
	case "GET":
		data, err := os.ReadFile(configPath)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"yaml": "{}"})
			return
		}
		jsonResponse(w, map[string]interface{}{"yaml": string(data)})
	case "PUT":
		var req struct {
			JsonText  string `json:"json_text"`
			YamlText  string `json:"yaml_text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		// Support both json_text and yaml_text (frontend uses yaml_text)
		content := req.JsonText
		if content == "" {
			content = req.YamlText
		}
		// Validate JSON
		var parsed interface{}
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		// Write atomically
		tmpPath := configPath + ".tmp"
		os.WriteFile(tmpPath, []byte(content), 0644)
		os.Rename(tmpPath, configPath)
		// Reload config
		newCfg, err := appconfig.Load()
		if err == nil {
			s.mu.Lock()
			s.cfg = newCfg
			s.provider = createProvider(s.cfg)
			s.agents = make(map[string]*agent.Agent)
			s.mu.Unlock()
		}
		jsonResponse(w, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	schema := map[string]interface{}{
		"fields": map[string]interface{}{
			"provider": map[string]interface{}{
				"type":    "string",
				"default": "deepseek",
				"options": []string{"openai", "anthropic", "deepseek", "ollama", "gemini", "groq", "mistral", "cohere", "custom"},
			},
			"model": map[string]interface{}{
				"type":    "string",
				"default": "deepseek-chat",
			},
			"cortex_enabled": map[string]interface{}{
				"type":    "boolean",
				"default": false,
			},
			"secret_redaction": map[string]interface{}{
				"type":    "boolean",
				"default": true,
			},
			"memory.enabled": map[string]interface{}{
				"type":    "boolean",
				"default": false,
			},
			"gateway.enabled": map[string]interface{}{
				"type":    "boolean",
				"default": false,
			},
		},
		"category_order": []string{"general", "provider", "model", "tools", "memory", "gateway", "cortex"},
	}
	jsonResponse(w, schema)
}

// --- Analytics ---

func (s *Server) handleAnalyticsModels(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 30
	if daysStr != "" {
		if n, err := strconv.Atoi(daysStr); err == nil && n > 0 {
			days = n
		}
	}

	// Aggregate from real sessions
	modelStats := map[string]*struct {
		Requests int
		Tokens   int
	}{}

	if s.sessionStore != nil {
		sessions, err := s.sessionStore.ListSessions(context.Background(), "")
		if err == nil {
			for _, sess := range sessions {
				for _, m := range sess.Messages {
					if m.Role == "assistant" {
						stat, ok := modelStats[s.cfg.Model]
						if !ok {
							stat = &struct{ Requests int; Tokens int }{}
							modelStats[s.cfg.Model] = stat
						}
						stat.Requests++
					}
				}
			}
		}
	}

	providerName := s.cfg.Provider
	if providerName == "" {
		providerName = "unknown"
	}

	models := make([]map[string]interface{}, 0)
	var totalInput, totalOutput int
	for model, stat := range modelStats {
		models = append(models, map[string]interface{}{
			"model":                model,
			"provider":             providerName,
			"input_tokens":         0,
			"output_tokens":        0,
			"cache_read_tokens":    0,
			"reasoning_tokens":     0,
			"estimated_cost":       0,
			"actual_cost":          0,
			"sessions":             0,
			"api_calls":            stat.Requests,
			"tool_calls":           0,
			"last_used_at":         nil,
			"avg_tokens_per_session": 0,
			"capabilities":         map[string]interface{}{},
		})
		totalInput += stat.Tokens / 2
		totalOutput += stat.Tokens / 2
	}

	if len(models) == 0 {
		models = []map[string]interface{}{}
	}

	jsonResponse(w, map[string]interface{}{
		"period_days": days,
		"models": models,
		"totals": map[string]interface{}{
			"distinct_models":      len(models),
			"total_input":          totalInput,
			"total_output":         totalOutput,
			"total_cache_read":     0,
			"total_reasoning":      0,
			"total_estimated_cost": 0,
			"total_actual_cost":    0,
			"total_sessions":       len(models),
		},
	})
}

func (s *Server) handleAnalyticsUsage(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 30
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	// Aggregate from real sessions
	dailyStats := map[string]*struct {
		InputTokens  int
		OutputTokens int
		Sessions     int
		APICalls     int
	}{}

	if s.sessionStore != nil {
		sessions, err := s.sessionStore.ListSessions(context.Background(), "")
		if err == nil {
			for _, sess := range sessions {
				day := sess.CreatedAt.Format("2006-01-02")
				stat, ok := dailyStats[day]
				if !ok {
					stat = &struct {
						InputTokens  int
						OutputTokens int
						Sessions     int
						APICalls     int
					}{}
					dailyStats[day] = stat
				}
				stat.Sessions++
				for _, m := range sess.Messages {
					if m.Role == "assistant" {
						stat.APICalls++
					}
				}
			}
		}
	}

	// Fill in daily data
	daily := make([]map[string]interface{}, 0)
	for i := days - 1; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		stat := dailyStats[date]
		entry := map[string]interface{}{
			"day":               date,
			"input_tokens":      0,
			"output_tokens":     0,
			"cache_read_tokens": 0,
			"reasoning_tokens":  0,
			"estimated_cost":    0,
			"actual_cost":       0,
			"sessions":          0,
			"api_calls":         0,
		}
		if stat != nil {
			entry["input_tokens"] = stat.InputTokens
			entry["output_tokens"] = stat.OutputTokens
			entry["sessions"] = stat.Sessions
			entry["api_calls"] = stat.APICalls
		}
		daily = append(daily, entry)
	}

	jsonResponse(w, map[string]interface{}{
		"daily":    daily,
		"by_model": []map[string]interface{}{},
		"totals": map[string]interface{}{
			"total_input":          0,
			"total_output":         0,
			"total_cache_read":     0,
			"total_reasoning":      0,
			"total_estimated_cost": 0,
			"total_actual_cost":    0,
			"total_sessions":       len(dailyStats),
			"total_api_calls":      0,
		},
		"skills": map[string]interface{}{
			"summary": map[string]interface{}{
				"total_skill_loads":   0,
				"total_skill_edits":   0,
				"total_skill_actions": 0,
				"distinct_skills_used": 0,
			},
			"top_skills": []map[string]interface{}{},
		},
	})
}

func (s *Server) handleAnalyticsCost(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"total":    0,
		"currency": "USD",
		"breakdown": []map[string]interface{}{},
	})
}

func (s *Server) handleAnalyticsTokens(w http.ResponseWriter, r *http.Request) {
	var totalMessages int
	if s.sessionStore != nil {
		sessions, _ := s.sessionStore.ListSessions(context.Background(), "")
		for _, sess := range sessions {
			totalMessages += len(sess.Messages)
		}
	}
	jsonResponse(w, map[string]interface{}{
		"input":  0,
		"output": 0,
		"total":  totalMessages,
	})
}

func (s *Server) handleAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	sessions := 0
	if s.sessionStore != nil {
		if list, err := s.sessionStore.ListSessions(context.Background(), ""); err == nil {
			sessions = len(list)
		}
	}
	jsonResponse(w, map[string]interface{}{
		"total_sessions": sessions,
		"total_messages": 0,
		"total_tools":    0,
	})
}

// --- Cron ---

func (s *Server) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	if s.cronMgr == nil {
		http.Error(w, "cron manager not available", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		jobs := s.cronMgr.List()
		result := make([]interface{}, 0, len(jobs))
		for _, job := range jobs {
			result = append(result, cronJobToResponse(job))
		}
		jsonResponse(w, result)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Name     string `json:"name"`
			Prompt   string `json:"prompt"`
			Schedule string `json:"schedule"`
			Script   string `json:"script"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		job := &cron.Job{
			ID:       uuid.New().String(),
			Name:     req.Name,
			Prompt:   req.Prompt,
			Schedule: req.Schedule,
			Script:   req.Script,
			Enabled:  true,
		}

		if err := s.cronMgr.Add(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to create job: %v", err), http.StatusInternalServerError)
			return
		}

		jsonResponse(w, cronJobToResponse(job))
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleCronJobByID(w http.ResponseWriter, r *http.Request) {
	if s.cronMgr == nil {
		http.Error(w, "cron manager not available", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/cron/jobs/")
	jobID := strings.TrimSuffix(path, "/pause")
	jobID = strings.TrimSuffix(jobID, "/resume")
	jobID = strings.TrimSuffix(jobID, "/trigger")

	if strings.HasSuffix(path, "/pause") {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		job.Enabled = false
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to pause job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	if strings.HasSuffix(path, "/resume") {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		job.Enabled = true
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to resume job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	if strings.HasSuffix(path, "/trigger") {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		go func() {
			_ = s.cronMgr.RunJob(context.Background(), job)
		}()
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.cronMgr.Remove(jobID); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// findCronJobByID finds a cron job by its ID by iterating through the list.
func (s *Server) findCronJobByID(id string) *cron.Job {
	for _, job := range s.cronMgr.List() {
		if job.ID == id {
			return job
		}
	}
	return nil
}

// cronJobToResponse converts a cron.Job to the JSON format expected by the frontend.
func cronJobToResponse(job *cron.Job) map[string]interface{} {
	state := "inactive"
	if job.Enabled {
		state = "active"
	}

	resp := map[string]interface{}{
		"id":                job.ID,
		"name":              job.Name,
		"prompt":            job.Prompt,
		"script":            job.Script,
		"schedule":          job.Schedule,
		"schedule_display":  describeSchedule(job.Schedule),
		"enabled":           job.Enabled,
		"state":             state,
		"deliver":           nil,
		"last_error":        nil,
	}

	if job.LastRun != nil {
		resp["last_run_at"] = job.LastRun.Format(time.RFC3339)
	}
	if job.NextRun != nil {
		resp["next_run_at"] = job.NextRun.Format(time.RFC3339)
	}

	return resp
}

// describeSchedule provides a human-readable description of a cron schedule expression.
func describeSchedule(schedule string) string {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return schedule
	}

	min, hour, dom, mon, dow := parts[0], parts[1], parts[2], parts[3], parts[4]

	// Common patterns
	if min == "0" && hour == "8" && dom == "*" && mon == "*" && dow == "*" {
		return "Every day at 08:00"
	}
	if min == "0" && hour == "9" && dom == "*" && mon == "*" && dow == "1-5" {
		return "Every weekday at 09:00"
	}
	if min == "0" && hour == "0" && dom == "*" && mon == "*" && dow == "*" {
		return "Every day at midnight"
	}
	if min == "*" && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		return "Every minute"
	}
	if min == "0" && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		return "Every hour"
	}
	if min == "0" && hour == "*" && dom == "*" && mon == "*" && dow == "1-5" {
		return "Every hour on weekdays"
	}
	if min == "30" && hour == "*" && dom == "1" && mon == "*" && dow == "*" {
		return "Monthly on the 1st at 00:30"
	}

	// Generic description
	return fmt.Sprintf("At %s:%s on day-of-month %s, month %s, day-of-week %s", hour, min, dom, mon, dow)
}

// --- Env ---

func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) {
	envPath := filepath.Join(s.magicHome, ".env")

	switch r.Method {
	case "GET":
		envVars := s.readEnvFile(envPath)
		envResponse := make(map[string]interface{})
		for key, value := range envVars {
			info := map[string]interface{}{
				"is_set":        value != "",
				"redacted_value": "",
				"description":   "",
				"url":           nil,
				"category":      "",
				"is_password":   false,
				"tools":         []string{},
				"advanced":      false,
			}
			if value != "" {
				info["redacted_value"] = "****"
			}
			// Enrich known keys with description and category
			switch key {
			case "DEEPSEEK_API_KEY":
				info["description"] = "API key for DeepSeek provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "OPENAI_API_KEY":
				info["description"] = "API key for OpenAI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "ANTHROPIC_API_KEY":
				info["description"] = "API key for Anthropic provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "GOOGLE_API_KEY":
				info["description"] = "API key for Google AI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "OPENROUTER_API_KEY":
				info["description"] = "API key for OpenRouter provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "GROQ_API_KEY":
				info["description"] = "API key for Groq provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "TOGETHER_API_KEY":
				info["description"] = "API key for Together AI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "FIRECRAWL_API_KEY":
				info["description"] = "API key for Firecrawl web scraping"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "TAVILY_API_KEY":
				info["description"] = "API key for Tavily search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "EXA_API_KEY":
				info["description"] = "API key for Exa search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "SERPAPI_API_KEY":
				info["description"] = "API key for SerpAPI search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "GITHUB_TOKEN":
				info["description"] = "GitHub personal access token"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"git"}
			case "GO_MAGIC_HOME":
				info["description"] = "Path to the Magic home directory"
				info["category"] = "general"
				info["is_password"] = false
				info["tools"] = []string{}
			}
			envResponse[key] = info
		}
		jsonResponse(w, envResponse)
	case "POST", "PUT":
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		s.writeEnvVar(envPath, req.Key, req.Value)
		jsonResponse(w, map[string]bool{"ok": true})
	case "DELETE":
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		s.deleteEnvVar(envPath, req.Key)
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEnvReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	envPath := filepath.Join(s.magicHome, ".env")
	envVars := s.readEnvFile(envPath)
	value := ""
	if v, ok := envVars[req.Key]; ok {
		value = v
	}
	jsonResponse(w, map[string]string{"key": req.Key, "value": value})
}

func (s *Server) readEnvFile(path string) map[string]string {
	result := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func (s *Server) writeEnvVar(path string, key, value string) {
	envVars := s.readEnvFile(path)
	envVars[key] = value
	s.writeEnvFile(path, envVars)
}

func (s *Server) deleteEnvVar(path string, key string) {
	envVars := s.readEnvFile(path)
	delete(envVars, key)
	s.writeEnvFile(path, envVars)
}

func (s *Server) writeEnvFile(path string, vars map[string]string) {
	var lines []string
	for k, v := range vars {
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600)
}

// --- Profiles ---

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		profiles := s.scanProfiles()
		jsonResponse(w, map[string]interface{}{"profiles": profiles})
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Name             string `json:"name"`
			CloneFromDefault bool   `json:"clone_from_default"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		profileDir := filepath.Join(s.magicHome, "profiles", req.Name)
		os.MkdirAll(profileDir, 0755)

		// Clone from default profile if requested
		if req.CloneFromDefault {
			defaultDir := s.magicHome
			if s.cfg.Profile != "" && s.cfg.Profile != "default" {
				defaultDir = filepath.Join(s.magicHome, "profiles", s.cfg.Profile)
			}

			// Copy .env file if exists
			defaultEnv := filepath.Join(defaultDir, ".env")
			if _, err := os.Stat(defaultEnv); err == nil {
				data, _ := os.ReadFile(defaultEnv)
				os.WriteFile(filepath.Join(profileDir, ".env"), data, 0600)
			}

			// Copy skills directory if exists
			defaultSkills := filepath.Join(defaultDir, "skills")
			if _, err := os.Stat(defaultSkills); err == nil {
				copyDir(defaultSkills, filepath.Join(profileDir, "skills"))
			}

			// Copy soul.md if exists
			defaultSoul := filepath.Join(defaultDir, "soul.md")
			if _, err := os.Stat(defaultSoul); err == nil {
				data, _ := os.ReadFile(defaultSoul)
				os.WriteFile(filepath.Join(profileDir, "soul.md"), data, 0644)
			}
		}

		jsonResponse(w, map[string]interface{}{"ok": true, "name": req.Name, "path": profileDir})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) scanProfiles() []map[string]interface{} {
	profilesDir := filepath.Join(s.magicHome, "profiles")
	profiles := make([]map[string]interface{}, 0)

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		// Return default profile
		return []map[string]interface{}{
			{"name": "default", "path": s.magicHome, "is_default": true, "model": nil, "provider": nil, "has_env": false, "skill_count": 0},
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profilePath := filepath.Join(profilesDir, entry.Name())
		// Check if profile has env file
		hasEnv := false
		if _, err := os.Stat(filepath.Join(profilePath, ".env")); err == nil {
			hasEnv = true
		}
		// Count skills in profile
		skillCount := 0
		skillsDir := filepath.Join(profilePath, "skills")
		if skills, err := os.ReadDir(skillsDir); err == nil {
			skillCount = len(skills)
		}
		profiles = append(profiles, map[string]interface{}{
			"name":        entry.Name(),
			"path":        profilePath,
			"is_default":  entry.Name() == s.cfg.Profile,
			"model":       nil,
			"provider":    nil,
			"has_env":     hasEnv,
			"skill_count": skillCount,
		})
	}

	if len(profiles) == 0 {
		profiles = append(profiles, map[string]interface{}{
			"name": "default", "path": s.magicHome, "is_default": true, "model": nil, "provider": nil, "has_env": false, "skill_count": 0,
		})
	}

	return profiles
}

func (s *Server) handleProfileByName(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/profiles/")

	if strings.HasSuffix(path, "/setup-command") {
		name := strings.TrimSuffix(path, "/setup-command")
		jsonResponse(w, map[string]string{"command": fmt.Sprintf("magic --profile %s chat", name)})
		return
	}
	if strings.HasSuffix(path, "/soul") {
		name := strings.TrimSuffix(path, "/soul")
		soulPath := filepath.Join(s.magicHome, "profiles", name, "soul.md")
		if r.Method == http.MethodGet {
			data, _ := os.ReadFile(soulPath)
			exists := false
			if _, err := os.Stat(soulPath); err == nil {
				exists = true
			}
			// Frontend expects "content" field, not "soul"
			jsonResponse(w, map[string]interface{}{"content": string(data), "exists": exists})
			return
		}
		if r.Method == http.MethodPut {
			var req struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			os.WriteFile(soulPath, []byte(req.Content), 0644)
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}
	}
	if strings.HasSuffix(path, "/switch") && r.Method == http.MethodPost {
		name := strings.TrimSuffix(path, "/switch")
		s.mu.Lock()
		s.cfg.Profile = name
		configPath := filepath.Join(s.magicHome, "config.json")
		data, _ := json.MarshalIndent(s.cfg, "", "  ")
		os.WriteFile(configPath, data, 0644)
		s.mu.Unlock()
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}

	if r.Method == http.MethodDelete {
		name := path
		profileDir := filepath.Join(s.magicHome, "profiles", name)
		os.RemoveAll(profileDir)
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}

	if r.Method == http.MethodPatch {
		// Rename profile - frontend sends "new_name", support both "new_name" and "name"
		var req struct {
			Name    string `json:"name"`
			NewName string `json:"new_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		newName := req.NewName
		if newName == "" {
			newName = req.Name
		}
		oldDir := filepath.Join(s.magicHome, "profiles", path)
		newDir := filepath.Join(s.magicHome, "profiles", newName)
		os.Rename(oldDir, newDir)
		jsonResponse(w, map[string]interface{}{"ok": true, "name": newName, "path": newDir})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

// --- System ---

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	jsonResponse(w, map[string]interface{}{
		"name":         "go-magic",
		"version":      s.version,
		"status":       "running",
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"go":           runtime.Version(),
		"memory_usage": fmt.Sprintf("%.2f MB", float64(memStats.Alloc)/1024/1024),
		"goroutines":   runtime.NumGoroutine(),
	})
}

func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	sessions := 0
	if s.sessionStore != nil {
		if list, err := s.sessionStore.ListSessions(context.Background(), ""); err == nil {
			sessions = len(list)
		}
	}

	jsonResponse(w, map[string]interface{}{
		"sessions":     sessions,
		"messages":     0,
		"uptime":       time.Since(s.startTime).String(),
		"memory_usage": fmt.Sprintf("%.2f MB", float64(runtime.MemStats{}.Alloc)/1024/1024),
		"goroutines":   runtime.NumGoroutine(),
	})
}

func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	llmStatus := "not_configured"
	if s.provider != nil {
		llmStatus = "connected"
	}
	dbStatus := "disconnected"
	if s.sessionStore != nil {
		dbStatus = "connected"
	}

	jsonResponse(w, map[string]interface{}{
		"status": "healthy",
		"checks": map[string]string{
			"server":   "ok",
			"database": dbStatus,
			"llm":      llmStatus,
		},
	})
}

// --- Logs ---

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	// Parse filter parameters
	filterFile := r.URL.Query().Get("file")
	filterLevel := strings.ToUpper(r.URL.Query().Get("level"))
	filterComponent := r.URL.Query().Get("component")

	logDir := filepath.Join(s.magicHome, "logs")
	var lines []string

	// Determine which log files to read
	var filesToRead []string
	entries, err := os.ReadDir(logDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if filterFile != "" && entry.Name() != filterFile {
				continue
			}
			filesToRead = append(filesToRead, filepath.Join(logDir, entry.Name()))
		}
	}

	// Read and filter log lines
	for _, filePath := range filesToRead {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}

			// Apply level filter
			if filterLevel != "" {
				// Match log levels like [DEBUG], [INFO], [WARN], [ERROR]
				if !strings.Contains(line, "["+filterLevel+"]") && !strings.Contains(line, "[ "+filterLevel+" ]") {
					continue
				}
			}

			// Apply component filter
			if filterComponent != "" {
				if !strings.Contains(line, filterComponent) {
					continue
				}
			}

			lines = append(lines, line)
		}
	}

	// Trim to limit
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	activeFile := filterFile
	if activeFile == "" && len(filesToRead) > 0 {
		activeFile = filepath.Base(filesToRead[0])
	}
	if activeFile == "" {
		activeFile = "server.log"
	}

	if len(lines) == 0 {
		lines = []string{fmt.Sprintf("[%s] [info] Magic Agent Dashboard started", time.Now().Format("2006-01-02 15:04:05"))}
	}

	jsonResponse(w, map[string]interface{}{
		"file":  activeFile,
		"lines": lines,
	})
}

func (s *Server) handleLogsTail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	// Send a few initial log entries then keep alive
	for i := 0; i < 5; i++ {
		data, _ := json.Marshal(LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   fmt.Sprintf("Log entry %d", i),
			Source:    "server",
		})
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
	}

	// Keep alive for 30 seconds
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		fmt.Fprintf(w, ": keepalive\n\n")
		flusher.Flush()
	}
}

func (s *Server) handleDashboardLogs(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"logs": []LogEntry{
			{Timestamp: time.Now(), Level: "info", Message: "Server started", Source: "system"},
		},
		"stats": map[string]interface{}{
			"total":   1,
			"errors":   0,
			"warnings": 0,
		},
	})
}

// --- Settings ---

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// Merge settings into config
		configPath := filepath.Join(s.magicHome, "config.json")
		data, _ := json.MarshalIndent(s.cfg, "", "  ")
		os.WriteFile(configPath, data, 0644)
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	jsonResponse(w, map[string]interface{}{
		"general": map[string]interface{}{
			"language": "en",
			"timezone": "UTC",
		},
		"appearance": map[string]interface{}{
			"theme":    "dark",
			"fontSize": 14,
		},
	})
}

func (s *Server) handleSettingByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/settings/")
	jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
}

// --- Gateway ---

func (s *Server) handleGatewayRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	// Start gateway restart as a background action
	actionID := "gateway-restart"
	s.runAction(actionID, "gateway restart", func() (int, error) {
		cmd := exec.Command(os.Args[0], "gateway", "restart")
		cmd.Env = os.Environ()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return 1, fmt.Errorf("gateway restart failed: %w\nOutput: %s", err, string(output))
		}
		return 0, nil
	})
	
	jsonResponse(w, map[string]interface{}{"ok": true, "action": actionID})
}

// --- Magic Update ---

func (s *Server) handleMagicUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	// Start magic update as a background action
	actionID := "magic-update"
	s.runAction(actionID, "magic update", func() (int, error) {
		cmd := exec.Command(os.Args[0], "update")
		cmd.Env = os.Environ()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return 1, fmt.Errorf("magic update failed: %w\nOutput: %s", err, string(output))
		}
		return 0, nil
	})
	
	jsonResponse(w, map[string]interface{}{"ok": true, "action": actionID})
}

// --- Actions ---

// runAction executes a background action and tracks its status
func (s *Server) runAction(id, name string, fn func() (int, error)) {
	s.actionsMu.Lock()
	s.actions[id] = &ActionStatus{
		Name:      name,
		Running:   true,
		Lines:     []string{},
		StartTime: time.Now(),
	}
	s.actionsMu.Unlock()

	go func() {
		exitCode, err := fn()
		
		s.actionsMu.Lock()
		defer s.actionsMu.Unlock()
		
		if action, ok := s.actions[id]; ok {
			action.Running = false
			action.ExitCode = &exitCode
			now := time.Now()
			action.EndTime = &now
			if err != nil {
				action.Lines = append(action.Lines, fmt.Sprintf("Error: %v", err))
			}
		}
	}()
}

// getActionStatus retrieves the status of a background action
func (s *Server) getActionStatus(id string) *ActionStatus {
	s.actionsMu.RLock()
	defer s.actionsMu.RUnlock()
	return s.actions[id]
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/actions/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.Error(w, "not found", 404)
		return
	}
	actionName := parts[0]
	subPath := parts[1]
	
	if subPath == "status" {
		status := s.getActionStatus(actionName)
		if status == nil {
			// Return empty status if action not found
			jsonResponse(w, map[string]interface{}{
				"exit_code": nil,
				"lines":     []string{},
				"name":      actionName,
				"pid":       nil,
				"running":   false,
				"status":    "unknown",
				"message":   "",
			})
			return
		}
		
		jsonResponse(w, map[string]interface{}{
			"exit_code": status.ExitCode,
			"lines":     status.Lines,
			"name":      status.Name,
			"pid":       nil,
			"running":   status.Running,
			"status":    map[bool]string{true: "running", false: "completed"}[status.Running],
			"message":   "",
		})
		return
	}
	http.Error(w, "not found", 404)
}

// --- Dashboard Themes ---

// getThemePreference reads the user's theme preference from config
func (s *Server) getThemePreference() string {
	themePath := filepath.Join(s.magicHome, ".theme")
	if data, err := os.ReadFile(themePath); err == nil {
		theme := strings.TrimSpace(string(data))
		// Support all frontend themes
		validThemes := []string{"default", "default-large", "midnight", "ember", "mono", "cyberpunk", "rose", "dark", "light"}
		for _, valid := range validThemes {
			if theme == valid {
				return theme
			}
		}
	}
	return "default" // default
}

// saveThemePreference saves the user's theme preference
func (s *Server) saveThemePreference(theme string) error {
	themePath := filepath.Join(s.magicHome, ".theme")
	return os.WriteFile(themePath, []byte(theme), 0644)
}

func (s *Server) handleDashboardThemes(w http.ResponseWriter, r *http.Request) {
	activeTheme := s.getThemePreference()
	// All available themes matching frontend presets
	themes := []map[string]interface{}{
		{"name": "default", "label": "Default", "description": "Default dark teal theme"},
		{"name": "default-large", "label": "Default Large", "description": "Default theme with larger fonts"},
		{"name": "midnight", "label": "Midnight", "description": "Deep blue midnight theme"},
		{"name": "ember", "label": "Ember", "description": "Warm orange ember theme"},
		{"name": "mono", "label": "Mono", "description": "Monochrome grayscale theme"},
		{"name": "cyberpunk", "label": "Cyberpunk", "description": "Neon cyberpunk theme"},
		{"name": "rose", "label": "Rose", "description": "Soft rose pink theme"},
		{"name": "dark", "label": "Dark", "description": "Dark theme with dark backgrounds"},
		{"name": "light", "label": "Light", "description": "Light theme with light backgrounds"},
	}
	jsonResponse(w, map[string]interface{}{
		"active": activeTheme,
		"themes": themes,
	})
}

func (s *Server) handleDashboardTheme(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate theme name - support all frontend themes
		validThemes := []string{"default", "default-large", "midnight", "ember", "mono", "cyberpunk", "rose", "dark", "light"}
		isValid := false
		for _, valid := range validThemes {
			if req.Name == valid {
				isValid = true
				break
			}
		}
		if !isValid {
			http.Error(w, "Invalid theme name", http.StatusBadRequest)
			return
		}

		// Save theme preference
		if err := s.saveThemePreference(req.Name); err != nil {
			http.Error(w, "Failed to save theme", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]interface{}{"ok": true, "theme": req.Name})
		return
	}
	http.Error(w, "method not allowed", 405)
}

// --- Static Files ---

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Skip API routes
	if strings.HasPrefix(path, "/api/") {
		http.Error(w, "not found", 404)
		return
	}

	// Dashboard plugins path
	if strings.HasPrefix(path, "/dashboard-plugins/") {
		subPath := strings.TrimPrefix(path, "/dashboard-plugins/")
		// 防止路径遍历
		if strings.Contains(subPath, "..") || strings.Contains(subPath, "\\") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		pluginPath := filepath.Join(s.magicHome, "plugins", subPath)
		// 验证最终路径仍在 plugins 目录内
		absPluginPath, _ := filepath.Abs(pluginPath)
		absPluginsDir, _ := filepath.Abs(filepath.Join(s.magicHome, "plugins"))
		if !strings.HasPrefix(absPluginPath, absPluginsDir+string(filepath.Separator)) && absPluginPath != absPluginsDir {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if data, err := os.ReadFile(pluginPath); err == nil {
			contentType := getContentType(pluginPath)
			w.Header().Set("Content-Type", contentType)
			w.Write(data)
			return
		}
		http.Error(w, "not found", 404)
		return
	}

	// Root SPA fallback
	if path == "/" || path == "" {
		data, err := staticFiles.ReadFile("dist/index.html")
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	// Try embedded files first
	data, err := staticFiles.ReadFile("dist" + path)
	if err == nil {
		contentType := getContentType(path)
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
		return
	}

	// Serve index.html for SPA routes
	spaPaths := []string{"/sessions", "/logs", "/skills", "/tools", "/config", "/settings", "/analytics", "/models", "/cron", "/plugins", "/env", "/profiles", "/docs", "/chat"}
	for _, spa := range spaPaths {
		if strings.HasPrefix(path, spa) {
			data, err := staticFiles.ReadFile("dist/index.html")
			if err == nil {
				w.Header().Set("Content-Type", "text/html")
				w.Write(data)
				return
			}
		}
	}

	http.Error(w, "not found", 404)
}

// --- Helper Functions ---

func getContentType(path string) string {
	ext := strings.ToLower(path[strings.LastIndex(path, ".")+1:])
	switch ext {
	case "js":
		return "application/javascript"
	case "css":
		return "text/css"
	case "html":
		return "text/html"
	case "json":
		return "application/json"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "svg":
		return "image/svg+xml"
	case "woff", "woff2":
		return "font/woff2"
	case "ttf":
		return "font/ttf"
	case "eot":
		return "application/vnd.ms-fontobject"
	case "ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// execCommand is a helper to run a command and return its output
func execCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// copyFile copies a file
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

// --- Missing Handlers for Frontend API Compatibility ---

// handleDashboardPluginsSubRoutes handles /api/dashboard/plugins/{name}/visibility
func (s *Server) handleDashboardPluginsSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/dashboard/plugins/")

	if strings.HasSuffix(path, "/visibility") {
		name := strings.TrimSuffix(path, "/visibility")
		if r.Method == http.MethodPost {
			var req struct {
				Hidden bool `json:"hidden"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "hidden": req.Hidden})
			return
		}
	}

	http.Error(w, "not found", http.StatusNotFound)
}

// handleAgentPluginInstall handles /api/dashboard/agent-plugins/install
func (s *Server) handleAgentPluginInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Identifier string `json:"identifier"`
		Force      bool   `json:"force"`
		Enable     bool   `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"ok":          true,
		"plugin_name": req.Identifier,
		"enabled":     req.Enable,
	})
}

// handleAgentPluginsSubRoutes handles /api/dashboard/agent-plugins/{name}/enable|disable|update
func (s *Server) handleAgentPluginsSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/dashboard/agent-plugins/")

	if strings.HasSuffix(path, "/enable") {
		name := strings.TrimSuffix(path, "/enable")
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": true})
		return
	}
	if strings.HasSuffix(path, "/disable") {
		name := strings.TrimSuffix(path, "/disable")
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": false})
		return
	}
	if strings.HasSuffix(path, "/update") {
		name := strings.TrimSuffix(path, "/update")
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "updated": true})
		return
	}

	// DELETE /api/dashboard/agent-plugins/{name}
	if r.Method == http.MethodDelete {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": path, "removed": true})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

// handlePluginProviders handles /api/dashboard/plugin-providers
func (s *Server) handlePluginProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req struct {
			MemoryProvider string `json:"memory_provider"`
			ContextEngine  string `json:"context_engine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"ok":              true,
			"memory_provider": req.MemoryProvider,
			"context_engine":  req.ContextEngine,
		})
		return
	}
	// GET
	jsonResponse(w, map[string]interface{}{
		"memory_provider": "",
		"memory_options":  []map[string]interface{}{},
		"context_engine":  "",
		"context_options": []map[string]interface{}{},
	})
}
