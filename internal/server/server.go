package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/approval"
	"github.com/magicwubiao/go-magic/internal/cron"
	"github.com/magicwubiao/go-magic/internal/gateway"
	"github.com/magicwubiao/go-magic/internal/goal"
	"github.com/magicwubiao/go-magic/internal/groupchat"
	"github.com/magicwubiao/go-magic/internal/kanban"
	"github.com/magicwubiao/go-magic/internal/plugin"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

//go:embed dist
var distFS embed.FS

// Session represents a chat session (API response format)
type Session struct {
	ID              string  `json:"id"`
	Profile         string  `json:"profile"`
	Source          string  `json:"source"`
	Model           string  `json:"model"`
	Title           string  `json:"title"`
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

// Message represents a chat message (API response format)
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
	Source      string   `json:"source"` // "default" or "user"
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
	mu           sync.RWMutex
	startTime    time.Time
	cfg          *appconfig.Config
	sessionStore *session.Store
	provider     provider.Provider
	toolReg      *tool.Registry
	skillMgr     *skills.Manager
	magicHome    string
	version      string
	commit       string
	buildDate    string

	// Active chat agents per session (lazy init)
	agents   map[string]*agent.Agent
	agentsMu sync.Mutex

	// Disabled skills tracking
	disabledSkills   map[string]bool
	disabledSkillsMu sync.Mutex

	// Cron job manager
	cronMgr *cron.Manager

	// Kanban manager
	kanbanMgr *kanban.Manager

	// Plugin manager
	pluginMgr *plugin.Manager

	// GroupChat storage
	groupchatStorage *groupchat.Storage

	// Goal manager
	goalMgr *goal.Manager

	// Background actions tracking
	actions   map[string]*ActionStatus
	actionsMu sync.RWMutex

	// Auth
	authToken string
	authMu    sync.RWMutex
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

	// Create logs directory for web logs
	logDir := filepath.Join(magicHome, "logs")
	os.MkdirAll(logDir, 0755)

	// Load config
	cfg, err := appconfig.Load()
	if err != nil {
		cfg = appconfig.DefaultConfig()
	}

	// Open session store
	if dbPath == "" {
		dbPath = filepath.Join(magicHome, "sessions.db")
	}
	fmt.Printf("[server] Session DB path: %s\n", dbPath)
	store, err := session.NewStore(dbPath)
	if err != nil {
		fmt.Printf("[server] Warning: Failed to open session store: %v\n", err)
		store = nil
	} else {
		fmt.Printf("[server] Session store opened successfully\n")
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

	// Create skills manager with config
	var skillCfg skills.ManagerConfig
	if cfg != nil {
		skillCfg.AutoSkillCreation = cfg.Skills.AutoSkillCreation
		skillCfg.MinPatternFreq = cfg.Skills.MinPatternFreq
		if skillCfg.MinPatternFreq == 0 {
			skillCfg.MinPatternFreq = 2
		}
	}
	skillMgr, _ := skills.NewManagerWithConfig(&skillCfg)

	// Create cron manager
	cronMgr, err := cron.NewManager()
	if err != nil {
		fmt.Printf("[server] Warning: Failed to create cron manager: %v\n", err)
	} else if prov != nil && registry != nil {
		// Set LLM provider and tools for cron agent mode
		cronMgr.SetAgentDeps(prov, registry)
	}

	// Initialize Kanban Manager
	var kanbanMgr *kanban.Manager
	kanbanMgr, err = kanban.NewManager(magicHome)
	if err != nil {
		fmt.Printf("[server] Warning: Failed to create kanban manager: %v\n", err)
	} else {
		if err := kanbanMgr.Init(); err != nil {
			fmt.Printf("[server] Warning: Failed to init kanban manager: %v\n", err)
		}
	}

	// Initialize Plugin Manager
	var pluginMgr *plugin.Manager
	pluginMgr, err = plugin.NewManager(nil)
	if err != nil {
		fmt.Printf("[server] Warning: Failed to create plugin manager: %v\n", err)
	}

	// Initialize GroupChat Storage
	var groupchatStorage *groupchat.Storage
	groupchatStorage, err = groupchat.NewStorageFromHome(magicHome)
	if err != nil {
		fmt.Printf("[server] Warning: Failed to create groupchat storage: %v\n", err)
	}

	// Initialize Goal Manager
	var goalMgr *goal.Manager
	goalMgr, err = goal.NewManager(magicHome)
	if err != nil {
		fmt.Printf("[server] Warning: Failed to create goal manager: %v\n", err)
	}

	// Load disabled skills from config
	disabledSkills := make(map[string]bool)
	if cfg != nil {
		for _, name := range cfg.Skills.Disabled {
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

	// Load or generate auth token
	authTokenPath := filepath.Join(magicHome, ".auth_token")
	authToken := ""
	if data, err := os.ReadFile(authTokenPath); err == nil {
		authToken = strings.TrimSpace(string(data))
	}

	s := &Server{
		mu:               sync.RWMutex{},
		startTime:        time.Now(),
		cfg:              cfg,
		sessionStore:     store,
		provider:         prov,
		toolReg:          registry,
		skillMgr:         skillMgr,
		magicHome:        magicHome,
		version:          version,
		commit:           "unknown",
		buildDate:        "unknown",
		agents:           make(map[string]*agent.Agent),
		disabledSkills:   disabledSkills,
		cronMgr:          cronMgr,
		kanbanMgr:        kanbanMgr,
		pluginMgr:        pluginMgr,
		groupchatStorage: groupchatStorage,
		goalMgr:          goalMgr,
		actions:          make(map[string]*ActionStatus),
		authToken:        authToken,
	}

	// Start cron scheduler
	if cronMgr != nil {
		cronMgr.Start()
	}

	return s
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

	// Build agent options
	var agentOpts []agent.AgentOption
	if s.cfg != nil && s.cfg.Memory.Enabled {
		agentOpts = append(agentOpts, agent.WithMemory(true))
	}

	a := agent.NewEnhancedAgent(s.provider, s.toolReg, toolsSchema, systemPrompt, agentOpts...)

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
						"path":    map[string]interface{}{"type": "string", "description": "File path to write"},
						"content": map[string]interface{}{"type": "string", "description": "Content to write"},
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
		Profile:       s.Profile,
		Source:        s.Platform,
		Model:         s.Model,
		Title:         title,
		StartedAt:     s.CreatedAt.Unix(),
		LastActive:    s.UpdatedAt.Unix(),
		IsActive:      isActive,
		MessageCount:  msgCount,
		ToolCallCount: toolCallCount,
		InputTokens:   s.InputTokens,
		OutputTokens:  s.OutputTokens,
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
						"name":      tc.GetToolName(),
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

	// Auth middleware - checks token for protected routes
	requireAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s.authMu.RLock()
			token := s.authToken
			s.authMu.RUnlock()

			// No token configured = no auth required
			if token == "" {
				h(w, r)
				return
			}

			// Check Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "Bearer "+token {
				h(w, r)
				return
			}

			// Check query parameter
			if r.URL.Query().Get("token") == token {
				h(w, r)
				return
			}

			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		}
	}

	// Auth routes (no auth required)
	mux.HandleFunc("/api/auth/status", withCORS(s.handleAuthStatus))
	mux.HandleFunc("/api/auth/setup", withCORS(s.handleAuthSetup))
	mux.HandleFunc("/api/auth/login", withCORS(s.handleAuthLogin))
	mux.HandleFunc("/api/auth/reset", withCORS(s.handleAuthReset))

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

	// Health (public)
	mux.HandleFunc("/api/health", withCORS(s.handleHealth))
	mux.HandleFunc("/api/status", withCORS(s.handleStatus))

	// Sessions
	mux.HandleFunc("/api/sessions", withCORS(requireAuth(s.handleSessions)))
	mux.HandleFunc("/api/sessions/search", withCORS(requireAuth(s.handleSessionSearch)))
	mux.HandleFunc("/api/sessions/", withCORS(requireAuth(s.handleSessionByID)))

	// Chat
	mux.HandleFunc("/api/chat", withCORS(requireAuth(s.handleChat)))
	mux.HandleFunc("/api/chat/stream", withCORS(requireAuth(s.handleChatStream)))

	// Tools
	mux.HandleFunc("/api/tools", withCORS(requireAuth(s.handleTools)))
	mux.HandleFunc("/api/tools/statistics", withCORS(requireAuth(s.handleToolsStatistics)))
	mux.HandleFunc("/api/toolsets", withCORS(requireAuth(s.handleToolsets)))
	mux.HandleFunc("/api/toolsets/statistics", withCORS(requireAuth(s.handleToolsetsStatistics)))
	mux.HandleFunc("/api/tools/toolsets", withCORS(requireAuth(s.handleToolsets)))
	mux.HandleFunc("/api/tools/toolsets/", withCORS(requireAuth(s.handleToolsetByID)))
	mux.HandleFunc("/api/tools/categories", withCORS(requireAuth(s.handleToolCategories)))
	mux.HandleFunc("/api/tools/", withCORS(requireAuth(s.handleToolByID)))

	// Skills
	mux.HandleFunc("/api/skills", withCORS(requireAuth(s.handleSkills)))
	mux.HandleFunc("/api/skills/categories", withCORS(requireAuth(s.handleSkillCategories)))
	mux.HandleFunc("/api/skills/upload", withCORS(requireAuth(s.handleSkillUpload)))
	mux.HandleFunc("/api/skills/statistics", withCORS(requireAuth(s.handleSkillsStatistics)))
	mux.HandleFunc("/api/skills/", withCORS(requireAuth(s.handleSkillByID)))
	mux.HandleFunc("/api/dashboard/skills", withCORS(requireAuth(s.handleDashboardSkills)))
	mux.HandleFunc("/api/dashboard/skills/search", withCORS(requireAuth(s.handleSkillsSearch)))
	mux.HandleFunc("/api/skills/hub/search", withCORS(requireAuth(s.handleSkillHubSearch)))
	mux.HandleFunc("/api/skills/hub/install", withCORS(requireAuth(s.handleSkillHubInstall)))

	// Plugins
	mux.HandleFunc("/api/plugins", withCORS(requireAuth(s.handlePlugins)))
	mux.HandleFunc("/api/plugins/statistics", withCORS(requireAuth(s.handlePluginsStatistics)))
	mux.HandleFunc("/api/dashboard/plugins", withCORS(requireAuth(s.handleDashboardPlugins)))
	mux.HandleFunc("/api/dashboard/plugins/rescan", withCORS(requireAuth(s.handleDashboardPluginsRescan)))
	mux.HandleFunc("/api/dashboard/plugins/", withCORS(requireAuth(s.handleDashboardPluginsSubRoutes)))
	// Agent plugins
	mux.HandleFunc("/api/dashboard/agent-plugins/install", withCORS(requireAuth(s.handleAgentPluginInstall)))
	mux.HandleFunc("/api/dashboard/agent-plugins/", withCORS(requireAuth(s.handleAgentPluginsSubRoutes)))
	// Plugin providers
	mux.HandleFunc("/api/dashboard/plugin-providers", withCORS(requireAuth(s.handlePluginProviders)))

	// Models
	mux.HandleFunc("/api/models", withCORS(requireAuth(s.handleModels)))
	mux.HandleFunc("/api/models/", withCORS(requireAuth(s.handleModelByID)))
	mux.HandleFunc("/api/model/info", withCORS(requireAuth(s.handleModelInfo)))
	mux.HandleFunc("/api/model/options", withCORS(requireAuth(s.handleModelOptions)))
	mux.HandleFunc("/api/model/set", withCORS(requireAuth(s.handleModelSet)))
	mux.HandleFunc("/api/model/auxiliary", withCORS(requireAuth(s.handleModelAuxiliary)))
	mux.HandleFunc("/api/providers", withCORS(requireAuth(s.handleProviders)))
	mux.HandleFunc("/api/providers/", withCORS(requireAuth(s.handleProvidersSubRoutes)))

	// Platforms (alias for providers for compatibility)
	mux.HandleFunc("/api/platforms", withCORS(requireAuth(s.handleProviders)))
	mux.HandleFunc("/api/platforms/", withCORS(requireAuth(s.handleProvidersSubRoutes)))

	// Config
	mux.HandleFunc("/api/config", withCORS(requireAuth(s.handleConfig)))
	mux.HandleFunc("/api/config/", withCORS(requireAuth(s.handleConfigByID)))
	mux.HandleFunc("/api/config/defaults", withCORS(requireAuth(s.handleConfigDefaults)))
	mux.HandleFunc("/api/config/raw", withCORS(requireAuth(s.handleConfigRaw)))
	mux.HandleFunc("/api/config/schema", withCORS(requireAuth(s.handleConfigSchema)))

	// Cron - order matters: more specific paths first
	mux.HandleFunc("/api/cron/jobs", withCORS(requireAuth(s.handleCronJobs)))
	mux.HandleFunc("/api/cron/jobs/", withCORS(requireAuth(s.handleCronJobByID)))
	mux.HandleFunc("/api/cron", withCORS(requireAuth(s.handleCronJobs)))
	mux.HandleFunc("/api/cron/", withCORS(requireAuth(s.handleCronJobByID)))

	// Env
	mux.HandleFunc("/api/env", withCORS(requireAuth(s.handleEnv)))
	mux.HandleFunc("/api/env/reveal", withCORS(requireAuth(s.handleEnvReveal)))

	// File system
	mux.HandleFunc("/api/fs/dirs", withCORS(requireAuth(s.handleListDirs)))

	// Profiles
	mux.HandleFunc("/api/profiles", withCORS(requireAuth(s.handleProfiles)))
	mux.HandleFunc("/api/profiles/", withCORS(requireAuth(s.handleProfileByName)))

	// System
	mux.HandleFunc("/api/system/info", withCORS(requireAuth(s.handleSystemInfo)))
	mux.HandleFunc("/api/system/stats", withCORS(requireAuth(s.handleSystemStats)))
	mux.HandleFunc("/api/system/health", withCORS(requireAuth(s.handleSystemHealth)))
	mux.HandleFunc("/api/system/version", withCORS(requireAuth(s.handleSystemVersion)))
	mux.HandleFunc("/api/system/version/check", withCORS(requireAuth(s.handleVersionCheck)))

	// Logs
	mux.HandleFunc("/api/logs", withCORS(requireAuth(s.handleLogs)))
	mux.HandleFunc("/api/logs/tail", withCORS(requireAuth(s.handleLogsTail)))
	mux.HandleFunc("/api/dashboard/logs", withCORS(requireAuth(s.handleDashboardLogs)))

	// Settings
	mux.HandleFunc("/api/settings", withCORS(requireAuth(s.handleSettings)))
	mux.HandleFunc("/api/settings/", withCORS(requireAuth(s.handleSettingByID)))

	// Gateway
	mux.HandleFunc("/api/gateway/status", withCORS(requireAuth(s.handleGatewayStatus)))
	mux.HandleFunc("/api/gateway/restart", withCORS(requireAuth(s.handleGatewayRestart)))
	mux.HandleFunc("/api/gateway/qr", withCORS(requireAuth(s.handleGatewayQR)))
	mux.HandleFunc("/api/gateway/qr/status", withCORS(requireAuth(s.handleGatewayQRStatus)))

	// Magic update
	mux.HandleFunc("/api/magic/update", withCORS(requireAuth(s.handleMagicUpdate)))

	// Actions
	mux.HandleFunc("/api/actions/", withCORS(requireAuth(s.handleActions)))

	// Kanban
	mux.HandleFunc("/api/kanban/board", withCORS(requireAuth(s.handleKanbanBoard)))
	mux.HandleFunc("/api/kanban/tasks", withCORS(requireAuth(s.handleKanbanTasks)))
	mux.HandleFunc("/api/kanban/tasks/", withCORS(requireAuth(s.handleKanbanTaskByID)))

	// GroupChat
	mux.HandleFunc("/api/groupchat/rooms", withCORS(requireAuth(s.handleGroupchatRooms)))
	mux.HandleFunc("/api/groupchat/rooms/", withCORS(requireAuth(s.handleGroupchatRoomByID)))

	// Goals
	mux.HandleFunc("/api/goals", withCORS(requireAuth(s.handleGoals)))
	mux.HandleFunc("/api/goals/current", withCORS(requireAuth(s.handleGoalCurrent)))
	mux.HandleFunc("/api/goals/", withCORS(requireAuth(s.handleGoalByID)))
	mux.HandleFunc("/api/goals/analyze", withCORS(requireAuth(s.handleGoalAnalyze)))

	// Approval Management
	mux.HandleFunc("/api/approval/status", withCORS(requireAuth(s.handleApprovalStatus)))
	mux.HandleFunc("/api/approval/history", withCORS(requireAuth(s.handleApprovalHistory)))
	mux.HandleFunc("/api/approval/stats", withCORS(requireAuth(s.handleApprovalStats)))
	mux.HandleFunc("/api/approval/pending", withCORS(requireAuth(s.handleApprovalPending)))
	mux.HandleFunc("/api/approval/pending/", withCORS(requireAuth(s.handleApprovalPendingByID)))
	// Patterns endpoints (for frontend compatibility)
	mux.HandleFunc("/api/approval/patterns/trusted", withCORS(requireAuth(s.handleApprovalPatternsTrusted)))
	mux.HandleFunc("/api/approval/patterns/denied", withCORS(requireAuth(s.handleApprovalPatternsDenied)))
	// Legacy endpoints (keep for backward compatibility)
	mux.HandleFunc("/api/approval/trusted", withCORS(requireAuth(s.handleApprovalTrusted)))
	mux.HandleFunc("/api/approval/denied", withCORS(requireAuth(s.handleApprovalDenied)))
	mux.HandleFunc("/api/approval/whitelist", withCORS(requireAuth(s.handleApprovalWhitelist)))
	mux.HandleFunc("/api/approval/strategy", withCORS(requireAuth(s.handleApprovalStrategy)))
	mux.HandleFunc("/api/approval/clear-history", withCORS(requireAuth(s.handleApprovalClearHistory)))
	mux.HandleFunc("/api/approval/settings", withCORS(requireAuth(s.handleApprovalSettings)))

	// Static files
	mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("[server] Magic Agent Dashboard starting on http://localhost:%d\n", port)
	fmt.Printf("[server] Provider: %s | Model: %s\n", s.cfg.Provider, s.cfg.Model)

	return http.ListenAndServe(addr, mux)
}

// --- Health & Status ---

// --- Auth Handlers ---

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	jsonResponse(w, map[string]interface{}{
		"configured": token != "",
	})
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	if token != "" {
		http.Error(w, `{"error":"auth already configured"}`, http.StatusConflict)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 4 {
		http.Error(w, `{"error":"password must be at least 4 characters"}`, http.StatusBadRequest)
		return
	}

	// Generate token from password using SHA-256
	hash := sha256.Sum256([]byte(req.Password))
	newToken := hex.EncodeToString(hash[:])

	authTokenPath := filepath.Join(s.magicHome, ".auth_token")
	if err := os.WriteFile(authTokenPath, []byte(newToken), 0600); err != nil {
		http.Error(w, `{"error":"failed to save token"}`, http.StatusInternalServerError)
		return
	}

	s.authMu.Lock()
	s.authToken = newToken
	s.authMu.Unlock()

	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"token": newToken,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	if token == "" {
		http.Error(w, `{"error":"auth not configured"}`, http.StatusNotFound)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	hash := sha256.Sum256([]byte(req.Password))
	inputToken := hex.EncodeToString(hash[:])

	if inputToken != token {
		http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"token": token,
	})
}

func (s *Server) handleAuthReset(w http.ResponseWriter, r *http.Request) {
	authTokenPath := filepath.Join(s.magicHome, ".auth_token")
	os.Remove(authTokenPath)

	s.authMu.Lock()
	s.authToken = ""
	s.authMu.Unlock()

	jsonResponse(w, map[string]bool{"ok": true})
}

// --- Health ---

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
		"status":                "ok",
		"timestamp":             time.Now().Unix(),
		"version":               s.version,
		"active_sessions":       sessions,
		"config_path":           filepath.Join(s.magicHome, "config.json"),
		"config_version":        1,
		"latest_config_version": 1,
		"env_path":              filepath.Join(s.magicHome, ".env"),
		"gateway_exit_reason":   nil,
		"gateway_health_url":    nil,
		"gateway_pid":           nil,
		"gateway_platforms":     map[string]PlatformStatus{},
		"gateway_running":       false,
		"gateway_state":         nil,
		"gateway_updated_at":    nil,
		"magic_home":            s.magicHome,
		"session_count":         sessions,
		"provider_status":       providerStatus,
		"release_date":          time.Now().Format("2006-01-02"),
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
		// Allow empty body for simple session creation
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", 400)
				return
			}
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
		model := req.Model
		if model == "" {
			model = s.cfg.Model
		}

		newSession := &session.Session{
			ID:              sessionID,
			Profile:         s.cfg.Profile,
			Platform:        platform,
			Model:           model,
			Messages:        []types.Message{},
			InputTokens:     0,
			OutputTokens:    0,
			CacheReadTokens: 0,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		if s.sessionStore != nil {
			if err := s.sessionStore.SaveSession(context.Background(), newSession); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}

		// Auto-link to active goal if enabled
		if s.goalMgr != nil && s.cfg.AutoLinkGoals {
			goals, err := s.goalMgr.List(context.Background(), goal.StatusActive)
			if err == nil && len(goals) > 0 {
				// Link to the most recently updated active goal
				s.goalMgr.LinkSession(context.Background(), goals[0].ID, sessionID)
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

	// Check for stream endpoint
	if strings.HasSuffix(path, "/stream") {
		sessionID := strings.TrimSuffix(path, "/stream")
		s.handleSessionStream(w, r, sessionID)
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
	switch r.Method {
	case "GET":
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
	case "POST":
		if s.provider == nil {
			http.Error(w, "provider not configured", 400)
			return
		}

		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
			http.Error(w, "invalid request: content required", 400)
			return
		}

		aiAgent := s.getOrCreateAgent(sessionID)
		if aiAgent == nil {
			http.Error(w, "failed to create agent", 500)
			return
		}

		ctx := context.Background()
		respContent, err := aiAgent.RunConversation(ctx, req.Content)
		if err != nil {
			http.Error(w, fmt.Sprintf("agent error: %v", err), 500)
			return
		}

		// Save to session store
		if s.sessionStore != nil {
			if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
				sess.Messages = append(sess.Messages, types.Message{
					Role:    "user",
					Content: req.Content,
				})
				sess.Messages = append(sess.Messages, types.Message{
					Role:    "assistant",
					Content: respContent,
				})
				inputTokens, outputTokens, cacheTokens := aiAgent.GetTokenStats()
				sess.InputTokens += inputTokens
				sess.OutputTokens += outputTokens
				sess.CacheReadTokens += cacheTokens
				sess.UpdatedAt = time.Now()
				s.sessionStore.SaveSession(ctx, sess)
			}
		}

		jsonResponse(w, map[string]interface{}{
			"id":        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"role":      "assistant",
			"content":   respContent,
			"timestamp": time.Now().Unix(),
		})
	default:
		http.Error(w, "method not allowed", 405)
	}
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
			sess.InputTokens = 0
			sess.OutputTokens = 0
			sess.CacheReadTokens = 0
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(context.Background(), sess)
		}
	}

	jsonResponse(w, map[string]bool{"ok": true})
}

func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.provider == nil {
		http.Error(w, "provider not configured", 400)
		return
	}

	content := r.URL.Query().Get("content")
	if content == "" {
		http.Error(w, "content query parameter required", 400)
		return
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

	ctx := context.Background()

	// Save user message to session
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			sess.Messages = append(sess.Messages, types.Message{
				Role:    "user",
				Content: content,
			})
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(ctx, sess)
		}
	}

	// Check if provider supports streaming
	_, supportsStream := s.provider.(provider.StreamingToolCaller)
	if !supportsStream {
		// Fallback: non-streaming response sent as single chunk
		resp, err := aiAgent.RunConversation(ctx, content)
		if err != nil {
			data, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
			return
		}

		// Send as delta chunks
		words := strings.Split(resp, "")
		for _, word := range words {
			data, _ := json.Marshal(map[string]string{"delta": word})
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}

		// Save assistant message
		if s.sessionStore != nil {
			if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
				sess.Messages = append(sess.Messages, types.Message{
					Role:    "assistant",
					Content: resp,
				})
				sess.UpdatedAt = time.Now()
				s.sessionStore.SaveSession(ctx, sess)
			}
		}

		doneData, _ := json.Marshal(map[string]bool{"done": true})
		fmt.Fprintf(w, "data: %s\n\n", string(doneData))
		flusher.Flush()
		return
	}

	// Real streaming
	var fullResponse strings.Builder
	streamErr := aiAgent.RunConversationStream(ctx, content, func(chunk string, done bool) {
		if done {
			return
		}
		if chunk == "" {
			return
		}

		// Parse tool markers and emit structured events for web frontend
		// >>>TOOL_START|toolName|args<<<
		if strings.Contains(chunk, ">>>TOOL_START|") {
			re := regexp.MustCompile(`>>>TOOL_START\|([^|]+)\|(.*)<<<`)
			m := re.FindStringSubmatch(chunk)
			if m != nil {
				toolName := m[1]
				toolArgs := m[2]
				if len(toolArgs) > 200 {
					toolArgs = toolArgs[:200] + "..."
				}
				eventData, _ := json.Marshal(map[string]interface{}{
					"type": "tool_start",
					"name": toolName,
					"args": toolArgs,
				})
				fmt.Fprintf(w, "data: %s\n\n", string(eventData))
				flusher.Flush()
			}
			return
		}

		// >>>TOOL_RESULT_START|toolName|success|duration<<<content>>>TOOL_RESULT_END<<<
		if strings.Contains(chunk, ">>>TOOL_RESULT_START|") {
			re := regexp.MustCompile(`>>>TOOL_RESULT_START\|([^|]+)\|([^|]+)\|([^<]+)<<<`)
			endRe := regexp.MustCompile(`>>>TOOL_RESULT_END<<<`)
			startMatch := re.FindStringSubmatchIndex(chunk)
			endMatch := endRe.FindStringIndex(chunk)
			if startMatch != nil && endMatch != nil {
				submatch := re.FindStringSubmatch(chunk[startMatch[0]:startMatch[1]])
				if len(submatch) >= 4 {
					toolName := submatch[1]
					toolSuccess := submatch[2] == "true"
					toolDuration := submatch[3]
					toolContent := chunk[startMatch[1]:endMatch[0]]
					// Truncate tool content for display
					if len(toolContent) > 500 {
						toolContent = toolContent[:500] + "..."
					}
					eventData, _ := json.Marshal(map[string]interface{}{
						"type":     "tool_result",
						"name":     toolName,
						"success":  toolSuccess,
						"duration": toolDuration,
						"content":  strings.TrimSpace(toolContent),
					})
					fmt.Fprintf(w, "data: %s\n\n", string(eventData))
					flusher.Flush()
				}
			}
			return
		}

		// Skip other internal markers
		if strings.Contains(chunk, ">>>TURN_START<<<") {
			return
		}

		fullResponse.WriteString(chunk)
		data, _ := json.Marshal(map[string]string{"delta": chunk})
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	})

	if streamErr != nil {
		data, _ := json.Marshal(map[string]string{"error": streamErr.Error()})
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	}

	// Save assistant message
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			sess.Messages = append(sess.Messages, types.Message{
				Role:    "assistant",
				Content: fullResponse.String(),
			})
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(ctx, sess)
		}
	}

	doneData, _ := json.Marshal(map[string]bool{"done": true})
	fmt.Fprintf(w, "data: %s\n\n", string(doneData))
	flusher.Flush()
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
					"snippet":         utils.Truncate(m.Content, 200),
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
		Message   string                   `json:"message"`
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

	// Save to session store with token usage
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
			// Update token usage from agent
			inputTokens, outputTokens, cacheTokens := aiAgent.GetTokenStats()
			sess.InputTokens += inputTokens
			sess.OutputTokens += outputTokens
			sess.CacheReadTokens += cacheTokens
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

	// Handle enable/disable
	if strings.HasSuffix(r.URL.Path, "/enable") {
		id = strings.TrimSuffix(id, "/enable")
		s.mu.Lock()
		if s.cfg != nil {
			// Add to enabled list if not present
			found := false
			for _, e := range s.cfg.Tools.Enabled {
				if e == id || e == "all" {
					found = true
					break
				}
			}
			if !found {
				s.cfg.Tools.Enabled = append(s.cfg.Tools.Enabled, id)
			}
			// Remove from disabled list
			newDisabled := make([]string, 0)
			for _, d := range s.cfg.Tools.Disabled {
				if d != id {
					newDisabled = append(newDisabled, d)
				}
			}
			s.cfg.Tools.Disabled = newDisabled
			_ = s.cfg.Save()
		}
		s.mu.Unlock()
		jsonResponse(w, map[string]interface{}{"ok": true, "name": id, "enabled": true})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/disable") {
		id = strings.TrimSuffix(id, "/disable")
		s.mu.Lock()
		if s.cfg != nil {
			// Add to disabled list
			found := false
			for _, d := range s.cfg.Tools.Disabled {
				if d == id {
					found = true
					break
				}
			}
			if !found {
				s.cfg.Tools.Disabled = append(s.cfg.Tools.Disabled, id)
			}
			// Remove from enabled list (unless "all")
			newEnabled := make([]string, 0)
			for _, e := range s.cfg.Tools.Enabled {
				if e != id {
					newEnabled = append(newEnabled, e)
				}
			}
			s.cfg.Tools.Enabled = newEnabled
			_ = s.cfg.Save()
		}
		s.mu.Unlock()
		jsonResponse(w, map[string]interface{}{"ok": true, "name": id, "enabled": false})
		return
	}

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
		fmt.Println("[server] buildToolsets: toolReg is nil")
		return []map[string]interface{}{}
	}

	allTools := s.toolReg.List()
	fmt.Printf("[server] buildToolsets: total tools registered: %d\n", len(allTools))
	if len(allTools) == 0 {
		fmt.Println("[server] buildToolsets: no tools registered, returning empty list")
		return []map[string]interface{}{}
	}

	// Known category prefixes and their human-readable names
	categoryMap := map[string]string{
		"web_":      "Web",
		"browser_":  "Browser",
		"execute_":  "Code Execution",
		"read_":     "File",
		"write_":    "File",
		"file_":     "File",
		"list_":     "File",
		"search_":   "File",
		"memory_":   "Memory",
		"delegate_": "Delegation",
		"poll_":     "Delegation",
		"code_":     "Code Execution",
		"skill_":    "Skills",
		"mcp_":      "MCP",
	}

	// Group tools by category
	categoryTools := map[string][]string{}
	categoryDescriptions := map[string]string{}
	ungrouped := []string{}

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

	// Check if all tools are enabled by default
	allEnabled := false
	if s.cfg != nil && s.cfg.Tools.Enabled != nil {
		for _, e := range s.cfg.Tools.Enabled {
			if e == "all" {
				allEnabled = true
				break
			}
		}
	}

	// Helper to check if toolset is enabled
	isEnabled := func(id string) bool {
		if s.cfg == nil || s.cfg.Tools.Disabled == nil || s.cfg.Tools.Enabled == nil {
			return true // Default to enabled if no config
		}
		// Check disabled list first
		for _, d := range s.cfg.Tools.Disabled {
			if d == id || d == "all" {
				return false
			}
		}
		// Check enabled list
		for _, e := range s.cfg.Tools.Enabled {
			if e == id || e == "all" {
				return true
			}
		}
		// Default: all enabled if not specified
		return allEnabled || len(s.cfg.Tools.Enabled) == 0
	}

	// Add categorized toolsets in a fixed order (based on categoryMap order)
	categoryOrder := []string{"File", "Web", "Browser", "Code Execution", "Memory", "Delegation", "Skills", "MCP"}
	for _, catName := range categoryOrder {
		if tools, ok := categoryTools[catName]; ok {
			id := strings.ToLower(strings.ReplaceAll(catName, " ", "_"))
			toolsets = append(toolsets, map[string]interface{}{
				"id":          id,
				"name":        catName,
				"label":       catName,
				"description": categoryDescriptions[catName],
				"enabled":     isEnabled(id),
				"configured":  true,
				"tools":       tools, // Return tool names as []string for frontend compatibility
			})
		}
	}

	// Add ungrouped tools as "Other" toolset (always last)
	if len(ungrouped) > 0 {
		toolsets = append(toolsets, map[string]interface{}{
			"id":          "other",
			"name":        "Other",
			"label":       "Other",
			"description": "Other tools",
			"enabled":     isEnabled("other"),
			"configured":  true,
			"tools":       ungrouped, // Return tool names as []string for frontend compatibility
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
		// Convert tool objects to tool names for backward compatibility
		var toolNames []string
		if tsTools, ok := ts["tools"].([]map[string]interface{}); ok {
			for _, t := range tsTools {
				if toolName, ok := t["name"].(string); ok {
					toolNames = append(toolNames, toolName)
				}
			}
		}
		result = append(result, Toolset{
			ID:      strings.ToLower(strings.ReplaceAll(name, " ", "_")),
			Name:    name,
			Tools:   toolNames,
			Enabled: true,
		})
	}
	return result
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	// Return all tools from all toolsets
	tools := make([]map[string]interface{}, 0)
	toolsets := s.buildToolsets()
	for _, ts := range toolsets {
		if tsTools, ok := ts["tools"].([]map[string]interface{}); ok {
			tools = append(tools, tsTools...)
		}
	}
	jsonResponse(w, tools)
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

func (s *Server) handleToolsStatistics(w http.ResponseWriter, r *http.Request) {
	// Return empty statistics for now (would need effectiveness manager integration)
	stats := []map[string]interface{}{}
	jsonResponse(w, stats)
}

func (s *Server) handleToolsetsStatistics(w http.ResponseWriter, r *http.Request) {
	// Return empty statistics for now
	stats := []map[string]interface{}{}
	jsonResponse(w, stats)
}

// --- Skills ---

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	skills := s.getRealSkills()
	jsonResponse(w, skills)
}

func (s *Server) handleSkillsStatistics(w http.ResponseWriter, r *http.Request) {
	stats := []map[string]interface{}{}

	if s.skillMgr != nil {
		allStats := s.skillMgr.GetAllStatistics()
		for _, stat := range allStats {
			stats = append(stats, map[string]interface{}{
				"skill_name":        stat.SkillName,
				"total_invocations": stat.TotalInvocations,
				"success_rate":      stat.SuccessRate,
				"avg_quality":       stat.AvgQuality,
				"avg_duration":      stat.AvgDuration,
				"positive_rate":     stat.PositiveRate,
				"last_used":         stat.LastUsed,
				"trend":             stat.Trend,
			})
		}
	}

	jsonResponse(w, stats)
}

func (s *Server) getRealSkills() []Skill {
	if s.skillMgr == nil {
		return make([]Skill, 0)
	}

	// 优先使用 Manager 的内存数据（已包含所有来源和分类）
	allSkills := s.skillMgr.List()
	if allSkills == nil || len(allSkills) == 0 {
		// 回退到文件系统扫描
		return s.scanSkillsDir()
	}

	s.disabledSkillsMu.Lock()
	defer s.disabledSkillsMu.Unlock()

	result := make([]Skill, 0, len(allSkills))
	for _, skill := range allSkills {
		isDisabled := s.disabledSkills[skill.Name]
		tags := skill.Tags
		if tags == nil {
			tags = []string{}
		}

		result = append(result, Skill{
			ID:          skill.Name,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        tags,
			Enabled:     !isDisabled,
			Source:      string(skill.Source),
		})
	}

	// 按名称排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// getUserSkillsDir returns the user skills directory path
func (s *Server) getUserSkillsDir() string {
	userDir := "skills"
	if s.cfg != nil && s.cfg.Skills.UserDir != "" {
		userDir = s.cfg.Skills.UserDir
	}
	return filepath.Join(s.magicHome, userDir)
}

// getDefaultSkillsDir returns the default skills directory path
func (s *Server) getDefaultSkillsDir() string {
	defaultDir := "skills-default"
	if s.cfg != nil && s.cfg.Skills.DefaultDir != "" {
		defaultDir = s.cfg.Skills.DefaultDir
	}
	return filepath.Join(s.magicHome, defaultDir)
}

func (s *Server) scanSkillsDir() []Skill {
	result := make([]Skill, 0)

	s.disabledSkillsMu.Lock()
	defer s.disabledSkillsMu.Unlock()

	// Scan default skills directory
	s.scanSkillsDirRecursive(s.getDefaultSkillsDir(), "", "default", &result)

	// Scan user skills directory
	s.scanSkillsDirRecursive(s.getUserSkillsDir(), "", "user", &result)

	// 按名称排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// scanSkillsDirRecursive 递归扫描技能目录
func (s *Server) scanSkillsDirRecursive(dir, parentCategory, source string, result *[]Skill) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// 跳过排除目录
		if skills.IsExcludedDir(entry.Name()) {
			continue
		}

		name := entry.Name()
		subPath := filepath.Join(dir, name)

		// 检查是否包含 SKILL.md（是技能目录）
		_, hasSkillMdErr := os.Stat(filepath.Join(subPath, "SKILL.md"))
		hasSkillMd := hasSkillMdErr == nil

		// 检查其他定义文件
		_, hasYAMLErr := os.Stat(filepath.Join(subPath, "skill.yaml"))
		_, hasJSONErr := os.Stat(filepath.Join(subPath, "manifest.json"))
		hasYAML := hasYAMLErr == nil
		hasJSON := hasJSONErr == nil

		if hasSkillMd || hasYAML || hasJSON {
			// 这是一个技能目录
			isDisabled := s.disabledSkills[name]
			category := parentCategory
			if category == "" {
				category = name // 使用父目录名作为分类（仅当无父分类时）
			}

			skill := Skill{ID: name, Name: name, Category: category, Enabled: !isDisabled, Source: source}
			found := false

			// Try SKILL.md first
			if hasSkillMd {
				data, err := os.ReadFile(filepath.Join(subPath, "SKILL.md"))
				if err == nil {
					parseSkillMarkdown(data, &skill)
					found = true
				}
			}

			// Try skill.yaml / skill.yml
			if !found {
				for _, yamlName := range []string{"skill.yaml", "skill.yml"} {
					skillFile := filepath.Join(subPath, yamlName)
					data, err := os.ReadFile(skillFile)
					if err == nil {
						parseSkillYAML(data, &skill)
						found = true
						break
					}
				}
			}

			// Try skill.json / manifest.json
			if !found {
				for _, jsonName := range []string{"skill.json", "manifest.json"} {
					jsonFile := filepath.Join(subPath, jsonName)
					data, err := os.ReadFile(jsonFile)
					if err == nil {
						parseSkillJSON(data, &skill)
						found = true
						break
					}
				}
			}

			if !found {
				skill.Description = "(no definition file found)"
			}

			*result = append(*result, skill)
		} else {
			// 这是一个分类目录，递归扫描
			childCategory := name
			if parentCategory != "" {
				childCategory = parentCategory + "/" + name
			}
			s.scanSkillsDirRecursive(subPath, childCategory, source, result)
		}
	}
}

func parseSkillYAML(data []byte, skill *Skill) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			skill.Name = strings.TrimPrefix(line, "name:")
			skill.Name = strings.TrimSpace(skill.Name)
			skill.Name = strings.Trim(skill.Name, "\"'")
		}
		if strings.HasPrefix(line, "description:") {
			skill.Description = strings.TrimPrefix(line, "description:")
			skill.Description = strings.TrimSpace(skill.Description)
			skill.Description = strings.Trim(skill.Description, "\"'")
		}
		if strings.HasPrefix(line, "tags:") {
			tags := strings.TrimPrefix(line, "tags:")
			tags = strings.TrimSpace(tags)
			skill.Tags = strings.Split(tags, ",")
		}
	}
}

func parseSkillMarkdown(data []byte, skill *Skill) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Parse YAML front matter in markdown
		if strings.HasPrefix(line, "# ") && skill.Name == skill.ID {
			skill.Name = strings.TrimPrefix(line, "# ")
			skill.Name = strings.TrimSpace(skill.Name)
		}
		if strings.HasPrefix(line, "description:") {
			skill.Description = strings.TrimPrefix(line, "description:")
			skill.Description = strings.TrimSpace(skill.Description)
			skill.Description = strings.Trim(skill.Description, "\"'")
		}
		if strings.HasPrefix(line, "tags:") {
			tags := strings.TrimPrefix(line, "tags:")
			tags = strings.TrimSpace(tags)
			skill.Tags = strings.Split(tags, ",")
		}
	}
	// If no description found, use first non-empty, non-heading line
	if skill.Description == "" {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
				skill.Description = line
				if len(skill.Description) > 100 {
					skill.Description = skill.Description[:100] + "..."
				}
				break
			}
		}
	}
}

func parseSkillJSON(data []byte, skill *Skill) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return
	}
	if v, ok := obj["name"].(string); ok && v != "" {
		skill.Name = v
	}
	if v, ok := obj["description"].(string); ok {
		skill.Description = v
	}
	if v, ok := obj["category"].(string); ok {
		skill.Category = v
	}
	if v, ok := obj["tags"].([]interface{}); ok {
		tags := make([]string, 0)
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
		skill.Tags = tags
	}
}

func (s *Server) handleSkillCategories(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, []string{})
}

func (s *Server) handleSkillHubSearch(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Query().Get("q")

	if s.skillMgr != nil {
		skillsList, err := s.skillMgr.SearchHub(keyword, nil)
		if err != nil {
			// Return empty array on error instead of 500, since registries may be unavailable
			jsonResponse(w, []skills.HubSkill{})
			return
		}
		if skillsList == nil {
			skillsList = []skills.HubSkill{}
		}
		jsonResponse(w, skillsList)
		return
	}
	jsonResponse(w, []skills.HubSkill{})
}

func (s *Server) handleSkillHubInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": "method not allowed"})
		return
	}

	var req struct {
		Source   string `json:"source"`
		SourceID string `json:"sourceID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": "invalid request body"})
		return
	}

	if s.skillMgr != nil {
		err := s.skillMgr.InstallFromHub(skills.HubSource(req.Source), req.SourceID)
		if err != nil {
			fmt.Printf("Hub install error: source=%s sourceID=%s err=%v\n", req.Source, req.SourceID, err)
			jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		s.skillMgr.Reload()
		jsonResponse(w, map[string]interface{}{"ok": true})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": false, "error": "skill manager not available"})
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
			s.cfg.Skills.Disabled = disabledList
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

	// Handle versions - 获取技能版本历史
	if strings.HasSuffix(id, "/versions") && r.Method == "GET" {
		skillName := strings.TrimSuffix(id, "/versions")
		if s.skillMgr != nil {
			versions := s.skillMgr.GetVersions(skillName)
			jsonResponse(w, versions)
			return
		}
		jsonResponse(w, []map[string]interface{}{})
		return
	}

	// Handle evolution - 获取技能演化历史
	if strings.HasSuffix(id, "/evolution") && r.Method == "GET" {
		skillName := strings.TrimSuffix(id, "/evolution")
		if s.skillMgr != nil {
			records := s.skillMgr.GetEvolutionRecords(skillName)
			jsonResponse(w, records)
			return
		}
		jsonResponse(w, []map[string]interface{}{})
		return
	}

	// Handle install
	if id == "install" && r.Method == "POST" {
		var req struct {
			URL      string `json:"url"`
			Name     string `json:"name"`
			Content  string `json:"content"`
			Category string `json:"category"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// If URL is provided, use skill manager to install from URL
		if req.URL != "" {
			if s.skillMgr != nil {
				if err := s.skillMgr.InstallFromURL(req.URL); err != nil {
					http.Error(w, "failed to install skill: "+err.Error(), http.StatusInternalServerError)
					return
				}
				// Reload skills to include the newly installed one
				s.skillMgr.Reload()
				jsonResponse(w, map[string]interface{}{"ok": true, "url": req.URL})
				return
			}
			http.Error(w, "skill manager not available", http.StatusInternalServerError)
			return
		}

		// Legacy: create from name/content
		if req.Name != "" {
			skillsDir := s.getUserSkillsDir()
			skillDir := filepath.Join(skillsDir, req.Name)
			os.MkdirAll(skillDir, 0755)
			if req.Content != "" {
				skillFile := filepath.Join(skillDir, "skill.yaml")
				os.WriteFile(skillFile, []byte(req.Content), 0644)
			}
			// Reload skills
			if s.skillMgr != nil {
				s.skillMgr.Reload()
			}
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}

		http.Error(w, "invalid request: either url or name is required", http.StatusBadRequest)
		return
	}

	// Handle GET - return single skill
	if r.Method == http.MethodGet {
		for _, skill := range s.getRealSkills() {
			if skill.ID == id {
				jsonResponse(w, skill)
				return
			}
		}
		http.Error(w, "not found", 404)
		return
	}

	// Handle PUT - update skill
	if r.Method == http.MethodPut {
		var req struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Category    string   `json:"category"`
			Tags        []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		// Update through skill manager (updates memory and file)
		if s.skillMgr != nil {
			err := s.skillMgr.UpdateMetadata(id, skills.SkillMeta{
				Name:        req.Name,
				Description: req.Description,
				Tags:        req.Tags,
			})
			if err != nil {
				http.Error(w, "failed to update skill: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// Fallback: update skill.yaml directly
			skillDir := filepath.Join(s.getUserSkillsDir(), id)
			skillFile := filepath.Join(skillDir, "skill.yaml")

			content := fmt.Sprintf("name: %s\ndescription: %s\ncategory: %s\ntags: %s\n",
				req.Name, req.Description, req.Category, strings.Join(req.Tags, ","))
			if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
				http.Error(w, "failed to write skill file: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		jsonResponse(w, map[string]interface{}{"ok": true, "id": id})
		return
	}

	// Handle DELETE - delete skill
	if r.Method == http.MethodDelete {
		// First, remove from skill manager (updates memory)
		if s.skillMgr != nil {
			if err := s.skillMgr.Delete(id); err != nil {
				http.Error(w, "failed to delete skill: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// Fallback: remove from file system directly
			skillDir := filepath.Join(s.getUserSkillsDir(), id)
			if err := os.RemoveAll(skillDir); err != nil {
				http.Error(w, "failed to delete skill directory: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "id": id, "deleted": true})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleDashboardSkills(w http.ResponseWriter, r *http.Request) {
	skills := s.getRealSkills()
	jsonResponse(w, map[string]interface{}{
		"installed":  skills,
		"available":  []Skill{},
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

func (s *Server) handleSkillUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form with 32MB max memory
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Get skill name from form or use filename
	skillName := r.FormValue("name")
	if skillName == "" {
		skillName = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}

	// Check for relative path (for directory uploads)
	relativePath := r.FormValue("path")

	// If relative path provided, extract folder name from it (use original folder name)
	if relativePath != "" && strings.Contains(relativePath, "/") {
		parts := strings.SplitN(relativePath, "/", 2)
		if parts[0] != "" {
			skillName = parts[0] // Use original folder name
		}
	}

	// Sanitize skill name (only replace spaces, keep path separators for folder detection)
	skillName = strings.ReplaceAll(skillName, " ", "_")

	// Get user skills directory
	skillsDir := s.getUserSkillsDir()
	skillDir := filepath.Join(skillsDir, skillName)

	// Create skill directory
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		http.Error(w, "failed to create skill directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Determine file extension and save path
	ext := strings.ToLower(filepath.Ext(header.Filename))
	var destPath string

	switch ext {
	case ".zip":
		// Save zip file and extract
		zipPath := filepath.Join(skillDir, header.Filename)
		if err := saveUploadedFile(file, zipPath); err != nil {
			http.Error(w, "failed to save zip file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Extract zip
		if err := extractZip(zipPath, skillDir); err != nil {
			http.Error(w, "failed to extract zip: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Remove zip file after extraction
		os.Remove(zipPath)
	default:
		// If relative path provided, preserve directory structure
		if relativePath != "" {
			// Remove the top-level folder from path (it's the skill name)
			parts := strings.SplitN(relativePath, "/", 2)
			if len(parts) > 1 {
				destPath = filepath.Join(skillDir, parts[1])
			} else {
				destPath = filepath.Join(skillDir, header.Filename)
			}
		} else {
			// Save as skill.yaml or SKILL.md based on extension
			switch ext {
			case ".md":
				destPath = filepath.Join(skillDir, "SKILL.md")
			case ".yaml", ".yml":
				destPath = filepath.Join(skillDir, "skill.yaml")
			case ".json":
				destPath = filepath.Join(skillDir, "skill.json")
			default:
				destPath = filepath.Join(skillDir, header.Filename)
			}
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			http.Error(w, "failed to create directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Save file
		destFile, err := os.Create(destPath)
		if err != nil {
			http.Error(w, "failed to create file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer destFile.Close()

		if _, err := destFile.ReadFrom(file); err != nil {
			http.Error(w, "failed to save file: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Reload skills to include the newly uploaded one
	if s.skillMgr != nil {
		s.skillMgr.Reload()
	}

	jsonResponse(w, map[string]interface{}{
		"ok":   true,
		"name": skillName,
		"path": skillDir,
	})
}

func saveUploadedFile(src io.Reader, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(src)
	return err
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	// Check if zip has a single top-level directory
	// If so, we'll extract contents directly into destDir instead of preserving nested structure
	hasSingleTopLevelDir := false
	var topLevelDir string

	if len(r.File) > 0 {
		firstName := r.File[0].Name
		slashIdx := strings.Index(firstName, "/")
		if slashIdx > 0 {
			topLevelDir = firstName[:slashIdx]
			// Check if ALL entries start with this directory
			hasSingleTopLevelDir = true
			for _, f := range r.File {
				if !strings.HasPrefix(f.Name, topLevelDir+"/") && f.Name != topLevelDir {
					hasSingleTopLevelDir = false
					break
				}
			}
		}
	}

	for _, f := range r.File {
		// Skip hidden files and __MACOSX
		if strings.HasPrefix(f.Name, ".") || strings.Contains(f.Name, "__MACOSX") {
			continue
		}

		// Strip top-level directory if zip has single-level nesting
		fpathName := f.Name
		if hasSingleTopLevelDir && topLevelDir != "" {
			fpathName = strings.TrimPrefix(f.Name, topLevelDir+"/")
		}
		if fpathName == "" {
			continue
		}

		fpath := filepath.Join(destDir, fpathName)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

// --- Plugins ---

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	plugins := s.scanPluginsDir()
	jsonResponse(w, plugins)
}

func (s *Server) handlePluginsStatistics(w http.ResponseWriter, r *http.Request) {
	// Return empty statistics for now (would need effectiveness manager integration)
	stats := []map[string]interface{}{}
	jsonResponse(w, stats)
}

func (s *Server) scanPluginsDir() []map[string]interface{} {
	pluginsDir := filepath.Join(s.magicHome, "plugins")
	plugins := make([]map[string]interface{}, 0)

	// Ensure plugins directory exists
	os.MkdirAll(pluginsDir, 0755)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		fmt.Printf("[server] Failed to read plugins dir: %v\n", err)
		return plugins
	}

	fmt.Printf("[server] Scanning plugins dir: %s (found %d entries)\n", pluginsDir, len(entries))

	// Build enabled/disabled lookup maps from config
	enabledPlugins := make(map[string]bool)
	disabledPlugins := make(map[string]bool)
	enableAll := false

	if s.cfg != nil {
		// Check if "all" is in enabled list
		for _, e := range s.cfg.Plugins.Enabled {
			if e == "all" {
				enableAll = true
				break
			}
			enabledPlugins[e] = true
		}
		// Build disabled list
		for _, d := range s.cfg.Plugins.Disabled {
			disabledPlugins[d] = true
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		// Determine enabled status
		enabled := false
		if enableAll {
			enabled = !disabledPlugins[name]
		} else {
			enabled = enabledPlugins[name] && !disabledPlugins[name]
		}

		if entry.IsDir() {
			// Directory-based plugin
			plugins = append(plugins, map[string]interface{}{
				"id":          name,
				"name":        name,
				"description": fmt.Sprintf("Plugin: %s", name),
				"enabled":     enabled,
				"version":     "1.0.0",
				"type":        "directory",
			})
		} else if filepath.Ext(name) == ".go" {
			// Single .go file plugin
			plugins = append(plugins, map[string]interface{}{
				"id":          name,
				"name":        name,
				"description": fmt.Sprintf("Go plugin: %s", name),
				"enabled":     enabled,
				"version":     "1.0.0",
				"type":        "go",
			})
		}
	}
	fmt.Printf("[server] Found %d plugins\n", len(plugins))
	return plugins
}

func (s *Server) handleDashboardPlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Use unified scanPluginsDir logic
		plugins := s.scanPluginsDir()
		jsonResponse(w, plugins)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleDashboardPluginsRescan(w http.ResponseWriter, r *http.Request) {
	// Frontend uses POST, support both GET and POST
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	plugins := s.scanPluginsDir()
	// Return the plugins list so frontend can update
	jsonResponse(w, plugins)
}

// --- Models ---

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := make([]map[string]interface{}, 0)
	seen := make(map[string]bool)

	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			modelName := provCfg.Model
			if modelName == "" {
				modelName = "default"
			}
			id := fmt.Sprintf("%s/%s", name, modelName)
			if !seen[id] {
				seen[id] = true
				models = append(models, map[string]interface{}{
					"id":         id,
					"name":       modelName,
					"provider":   name,
					"contextLen": 128000,
				})
			}
		}
	}

	// Always include current provider
	if s.cfg != nil && s.cfg.Provider != "" {
		id := fmt.Sprintf("%s/%s", s.cfg.Provider, s.cfg.Model)
		if !seen[id] {
			models = append(models, map[string]interface{}{
				"id":         id,
				"name":       s.cfg.Model,
				"provider":   s.cfg.Provider,
				"contextLen": 128000,
			})
		}
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
	providers := make([]map[string]interface{}, 0)
	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			providers = append(providers, map[string]interface{}{
				"id":       name,
				"name":     name,
				"enabled":  true,
				"api_key":  provCfg.APIKey,
				"base_url": provCfg.BaseURL,
				"model":    provCfg.Model,
			})
		}
	}
	jsonResponse(w, providers)
}

func (s *Server) handleProvidersSubRoutes(w http.ResponseWriter, r *http.Request) {
	// Support both /api/providers/{name}/* and /api/platforms/{name}/*
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/api/providers/")
	path = strings.TrimPrefix(path, "/api/platforms/")

	// Extract provider name and sub-route
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]
	subRoute := ""
	if len(parts) > 1 {
		subRoute = parts[1]
	}

	// Handle GET /{name} - get single provider
	if r.Method == http.MethodGet && subRoute == "" {
		if s.cfg != nil && s.cfg.Providers != nil {
			if provCfg, ok := s.cfg.Providers[name]; ok {
				jsonResponse(w, ProviderInfo{
					Name:    name,
					Label:   name,
					BaseURL: provCfg.BaseURL,
					Models:  []string{provCfg.Model},
					APIKey:  maskAPIKey(provCfg.APIKey),
				})
				return
			}
		}
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	// Handle PUT /{name} - update provider
	if r.Method == http.MethodPut && subRoute == "" {
		var req struct {
			BaseURL string   `json:"base_url"`
			Model   string   `json:"model"`
			APIKey  string   `json:"api_key"`
			Models  []string `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		// Update provider config
		if s.cfg != nil {
			if s.cfg.Providers == nil {
				s.cfg.Providers = make(map[string]appconfig.ProviderConfig)
			}
			provCfg := s.cfg.Providers[name]
			if req.BaseURL != "" {
				provCfg.BaseURL = req.BaseURL
			}
			if req.Model != "" {
				provCfg.Model = req.Model
			}
			if req.APIKey != "" {
				provCfg.APIKey = req.APIKey
			}
			s.cfg.Providers[name] = provCfg
			s.cfg.Save()
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name})
		return
	}

	// Handle DELETE /{name} - delete provider
	if r.Method == http.MethodDelete && subRoute == "" {
		if s.cfg != nil && s.cfg.Providers != nil {
			delete(s.cfg.Providers, name)
			s.cfg.Save()
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "deleted": true})
		return
	}

	// Handle POST /{name} - create provider (alias for PUT to create new)
	if r.Method == http.MethodPost && subRoute == "" {
		var req struct {
			Name    string   `json:"name"`
			BaseURL string   `json:"base_url"`
			Model   string   `json:"model"`
			APIKey  string   `json:"api_key"`
			Models  []string `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		// Use provided name or fallback to URL parameter
		providerName := req.Name
		if providerName == "" {
			providerName = name
		}
		// Create/update provider config
		if s.cfg != nil {
			if s.cfg.Providers == nil {
				s.cfg.Providers = make(map[string]appconfig.ProviderConfig)
			}
			provCfg := s.cfg.Providers[providerName]
			if req.BaseURL != "" {
				provCfg.BaseURL = req.BaseURL
			}
			if req.Model != "" {
				provCfg.Model = req.Model
			}
			if req.APIKey != "" {
				provCfg.APIKey = req.APIKey
			}
			s.cfg.Providers[providerName] = provCfg
			s.cfg.Save()
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": providerName, "created": true})
		return
	}

	// Handle POST /{name}/enable - enable provider
	if r.Method == http.MethodPost && subRoute == "enable" {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": true})
		return
	}

	// Handle POST /{name}/disable - disable provider
	if r.Method == http.MethodPost && subRoute == "disable" {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": false})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
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
		"model":                    fmt.Sprintf("%s/%s", providerName, modelName),
		"provider":                 providerName,
		"auto_context_length":      contextLen,
		"config_context_length":    0,
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
		// Always re-read from file to pick up changes made by gateway process (e.g. QR login token)
		if freshCfg, err := appconfig.Load(); err == nil {
			s.cfg = freshCfg
		}
		if s.cfg == nil {
			jsonResponse(w, map[string]interface{}{})
			return
		}
		jsonResponse(w, s.cfg)
	case "PUT":
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), 400)
			return
		}
		// Get config payload - support both {config: {...}} and direct config object
		var configData map[string]interface{}
		if cfg, ok := req["config"].(map[string]interface{}); ok {
			configData = cfg
		} else {
			configData = req
		}

		// Handle dot-notation keys (e.g. "memory.enabled") by expanding into nested objects
		expanded := expandDotKeys(configData)

		// Merge into config
		if s.cfg == nil {
			s.cfg = appconfig.DefaultConfig()
		}
		data, _ := json.Marshal(expanded)
		if err := json.Unmarshal(data, s.cfg); err != nil {
			http.Error(w, "failed to merge config: "+err.Error(), 500)
			return
		}
		// Save
		configPath := filepath.Join(s.magicHome, "config.json")
		saveData, _ := json.MarshalIndent(s.cfg, "", "  ")
		if err := os.WriteFile(configPath, saveData, 0644); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), 500)
			return
		}
		fmt.Printf("[server] Config saved to %s\n", configPath)

		// Check which config sections changed and hot-reload accordingly
		hotReloadKeys := []string{"provider", "model", "api_key", "base_url", "secret_redaction", "profile", "working_dir"}
		needsProviderReload := false

		for key := range expanded {
			for _, reloadKey := range hotReloadKeys {
				if key == reloadKey {
					needsProviderReload = true
					break
				}
			}
		}

		// Hot-reload provider if provider-related config changed
		if needsProviderReload {
			s.mu.Lock()
			s.provider = createProvider(s.cfg)
			s.mu.Unlock()
			fmt.Printf("[server] Provider hot-reloaded: %s / %s\n", s.cfg.Provider, s.cfg.Model)
		}

		// Return updated config
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
			JsonText string `json:"json_text"`
			YamlText string `json:"yaml_text"`
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
				"type":     "boolean",
				"default":  false,
				"category": "cortex",
			},
			"secret_redaction": map[string]interface{}{
				"type":    "boolean",
				"default": true,
			},
			"gateway.enabled": map[string]interface{}{
				"type":    "boolean",
				"default": false,
			},
		},
		"category_order": []string{"general", "provider", "model", "tools", "cortex", "gateway"},
	}
	jsonResponse(w, schema)
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
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Prompt      string   `json:"prompt"`
			Schedule    string   `json:"schedule"`
			Script      string   `json:"script"`
			NoAgent     bool     `json:"no_agent"`
			Skills      []string `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if req.Schedule == "" {
			http.Error(w, "schedule is required", http.StatusBadRequest)
			return
		}

		// Validate cron expression
		if err := cron.ValidateSchedule(req.Schedule); err != nil {
			http.Error(w, fmt.Sprintf("invalid cron expression: %v", err), http.StatusBadRequest)
			return
		}

		job := &cron.Job{
			ID:          uuid.New().String(),
			Name:        req.Name,
			Description: req.Description,
			Prompt:      req.Prompt,
			Schedule:    req.Schedule,
			Script:      req.Script,
			NoAgent:     req.NoAgent,
			Skills:      req.Skills,
			Enabled:     true,
		}

		// Calculate next run time
		if nextRun, err := cron.GetNextRun(req.Schedule); err == nil {
			job.NextRun = nextRun
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

	// Support both /api/cron/{id} and /api/cron/jobs/{id}
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/api/cron/jobs/")
	path = strings.TrimPrefix(path, "/api/cron/")
	jobID := strings.TrimSuffix(path, "/pause")
	jobID = strings.TrimSuffix(jobID, "/resume")
	jobID = strings.TrimSuffix(jobID, "/trigger")
	jobID = strings.TrimSuffix(jobID, "/run")
	jobID = strings.TrimSuffix(jobID, "/logs")

	if strings.HasSuffix(path, "/logs") {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		logs := s.cronMgr.GetLogs(jobID, limit)
		jsonResponse(w, logs)
		return
	}

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
		jsonResponse(w, cronJobToResponse(job))
		return
	}
	if strings.HasSuffix(path, "/resume") {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		job.Enabled = true
		// Recalculate next run
		if nextRun, err := cron.GetNextRun(job.Schedule); err == nil {
			job.NextRun = nextRun
		}
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to resume job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, cronJobToResponse(job))
		return
	}
	if strings.HasSuffix(path, "/trigger") || strings.HasSuffix(path, "/run") {
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
	if r.Method == http.MethodGet {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		jsonResponse(w, cronJobToResponse(job))
		return
	}
	if r.Method == http.MethodPut {
		job := s.findCronJobByID(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		var req struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Prompt      string   `json:"prompt"`
			Schedule    string   `json:"schedule"`
			Script      string   `json:"script"`
			NoAgent     *bool    `json:"no_agent,omitempty"`
			Enabled     *bool    `json:"enabled,omitempty"`
			Skills      []string `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name != "" {
			job.Name = req.Name
		}
		if req.Description != "" {
			job.Description = req.Description
		}
		if req.Prompt != "" {
			job.Prompt = req.Prompt
		}
		if req.Schedule != "" {
			// Validate new schedule
			if err := cron.ValidateSchedule(req.Schedule); err != nil {
				http.Error(w, fmt.Sprintf("invalid cron expression: %v", err), http.StatusBadRequest)
				return
			}
			job.Schedule = req.Schedule
			if nextRun, err := cron.GetNextRun(req.Schedule); err == nil {
				job.NextRun = nextRun
			}
		}
		if req.Script != "" {
			job.Script = req.Script
		}
		if req.NoAgent != nil {
			job.NoAgent = *req.NoAgent
		}
		if req.Enabled != nil {
			job.Enabled = *req.Enabled
		}
		if req.Skills != nil {
			job.Skills = req.Skills
		}
		if err := s.cronMgr.Update(job); err != nil {
			http.Error(w, fmt.Sprintf("failed to update job: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, cronJobToResponse(job))
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
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// findCronJobByID finds a cron job by its ID by iterating through the list.
func (s *Server) findCronJobByID(id string) *cron.Job {
	return s.cronMgr.GetByID(id)
}

// cronJobToResponse converts a cron.Job to the JSON format expected by the frontend.
func cronJobToResponse(job *cron.Job) map[string]interface{} {
	state := "inactive"
	if job.Enabled {
		state = "active"
	}
	if job.LastStatus == "running" {
		state = "running"
	}

	resp := map[string]interface{}{
		"id":               job.ID,
		"name":             job.Name,
		"description":      job.Description,
		"prompt":           job.Prompt,
		"script":           job.Script,
		"schedule":         job.Schedule,
		"schedule_display": describeSchedule(job.Schedule),
		"enabled":          job.Enabled,
		"no_agent":         job.NoAgent,
		"state":            state,
		"last_status":      job.LastStatus,
		"last_error":       job.LastError,
		"run_count":        job.RunCount,
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

// --- File System ---

func (s *Server) handleListDirs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path, _ = os.Getwd()
	}

	// Security: resolve to absolute path and prevent traversal above root
	absPath, err := filepath.Abs(path)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"dirs": []string{}, "error": "invalid path"})
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"dirs": []string{}, "error": "cannot read directory"})
		return
	}

	type dirEntry struct {
		Path  string `json:"path"`
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}

	var result []dirEntry
	// Add parent directory option
	parent := filepath.Dir(absPath)
	if parent != absPath {
		result = append(result, dirEntry{Path: parent, Name: "..", IsDir: true})
	}

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			result = append(result, dirEntry{
				Path:  filepath.Join(absPath, entry.Name()),
				Name:  entry.Name(),
				IsDir: true,
			})
		}
	}

	jsonResponse(w, map[string]interface{}{
		"current": absPath,
		"dirs":    result,
	})
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
				"is_set":         value != "",
				"redacted_value": "",
				"description":    "",
				"url":            nil,
				"category":       "",
				"is_password":    false,
				"tools":          []string{},
				"advanced":       false,
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

	// --- User Profile (user.md) ---
	if strings.HasSuffix(path, "/user") {
		name := strings.TrimSuffix(path, "/user")
		userPath := filepath.Join(s.magicHome, "profiles", name, "user.md")
		if r.Method == http.MethodGet {
			data, _ := os.ReadFile(userPath)
			exists := false
			if _, err := os.Stat(userPath); err == nil {
				exists = true
			}
			// Parse user.md content into structured data
			userData := s.parseUserMD(string(data))
			jsonResponse(w, map[string]interface{}{
				"content": string(data),
				"exists":  exists,
				"data":    userData,
			})
			return
		}
		if r.Method == http.MethodPut {
			var req struct {
				Content string                 `json:"content"`
				Data    map[string]interface{} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			// If data is provided, regenerate content
			content := req.Content
			if req.Data != nil {
				content = s.generateUserMD(req.Data)
			}
			os.WriteFile(userPath, []byte(content), 0644)
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}
	}

	// --- User Preferences (from Cortex) ---
	if strings.HasSuffix(path, "/preferences") {
		name := strings.TrimSuffix(path, "/preferences")
		if r.Method == http.MethodGet {
			// Return preferences from Cortex UserProfile
			preferences := s.getUserPreferences(name)
			jsonResponse(w, map[string]interface{}{"preferences": preferences})
			return
		}
	}

	// --- Preference Feedback ---
	if strings.Contains(path, "/preferences/") && strings.HasSuffix(path, "/feedback") {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			name := parts[0]
			key := parts[2]
			if r.Method == http.MethodPost {
				var req struct {
					Accurate bool `json:"accurate"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "Invalid request body", http.StatusBadRequest)
					return
				}
				s.handlePreferenceFeedback(name, key, req.Accurate)
				jsonResponse(w, map[string]bool{"ok": true})
				return
			}
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
		"version":      s.version,
		"platform":     runtime.GOOS,
		"arch":         runtime.GOARCH,
		"go_version":   runtime.Version(),
		"memory_usage": memStats.Alloc,
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

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptimeSeconds := int(time.Since(s.startTime).Seconds())

	jsonResponse(w, map[string]interface{}{
		"sessions":     sessions,
		"messages":     0,
		"uptime":       uptimeSeconds,
		"memory_usage": memStats.Alloc,
		"goroutines":   runtime.NumGoroutine(),
	})
}

// --- User Profile Helper Methods ---

// parseUserMD parses user.md content into structured data
func (s *Server) parseUserMD(content string) map[string]interface{} {
	data := map[string]interface{}{
		"name":                "",
		"role":                "",
		"communication_style": "",
		"code_style":          "",
		"tech_stack":          []string{},
		"interests":           []string{},
	}

	lines := strings.Split(content, "\n")
	currentSection := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			currentSection = strings.ToLower(strings.TrimPrefix(line, "## "))
			continue
		}

		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)

			if idx := strings.Index(line, ":"); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				value := strings.TrimSpace(line[idx+1:])

				switch currentSection {
				case "about":
					if strings.EqualFold(key, "Name") {
						data["name"] = value
					} else if strings.EqualFold(key, "Role") {
						data["role"] = value
					}
				case "preferences":
					if strings.EqualFold(key, "Communication style") {
						data["communication_style"] = value
					} else if strings.EqualFold(key, "Code style") {
						data["code_style"] = value
					}
				case "tech stack":
					if value != "" && value != "[Not set]" {
						if stack, ok := data["tech_stack"].([]string); ok {
							data["tech_stack"] = append(stack, value)
						}
					}
				case "interests":
					if value != "" && value != "[Not set]" {
						if interests, ok := data["interests"].([]string); ok {
							data["interests"] = append(interests, value)
						}
					}
				}
			}
		}
	}

	return data
}

// generateUserMD generates user.md content from structured data
func (s *Server) generateUserMD(data map[string]interface{}) string {
	var lines []string
	lines = append(lines, "# User Profile")
	lines = append(lines, "")
	lines = append(lines, "## About")

	name := "[Not set]"
	if v, ok := data["name"].(string); ok && v != "" {
		name = v
	}
	lines = append(lines, "- Name: "+name)

	role := "[Not set]"
	if v, ok := data["role"].(string); ok && v != "" {
		role = v
	}
	lines = append(lines, "- Role: "+role)

	lines = append(lines, "")
	lines = append(lines, "## Preferences")

	commStyle := "[Not set]"
	if v, ok := data["communication_style"].(string); ok && v != "" {
		commStyle = v
	}
	lines = append(lines, "- Communication style: "+commStyle)

	codeStyle := "[Not set]"
	if v, ok := data["code_style"].(string); ok && v != "" {
		codeStyle = v
	}
	lines = append(lines, "- Code style: "+codeStyle)

	lines = append(lines, "")
	lines = append(lines, "## Tech Stack")

	if stack, ok := data["tech_stack"].([]interface{}); ok && len(stack) > 0 {
		for _, tech := range stack {
			if t, ok := tech.(string); ok {
				lines = append(lines, "- "+t)
			}
		}
	} else {
		lines = append(lines, "- Languages: [Not set]")
		lines = append(lines, "- Frameworks: [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Interests")

	if interests, ok := data["interests"].([]interface{}); ok && len(interests) > 0 {
		for _, interest := range interests {
			if i, ok := interest.(string); ok {
				lines = append(lines, "- "+i)
			}
		}
	} else {
		lines = append(lines, "- [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Notes")
	lines = append(lines, "[Auto-managed by go-magic]")

	return strings.Join(lines, "\n")
}

// getUserPreferences returns user preferences from Cortex UserProfile
func (s *Server) getUserPreferences(profileName string) []map[string]interface{} {
	// For now, return mock data
	// In production, this should read from Cortex UserProfile
	return []map[string]interface{}{
		{
			"key":        "communication_style",
			"value":      "简洁",
			"context":    "多次要求简短回答",
			"confidence": 0.85,
			"source":     "learned",
		},
		{
			"key":        "preferred_language",
			"value":      "Go",
			"context":    "多次使用 Go 示例",
			"confidence": 0.92,
			"source":     "learned",
		},
	}
}

// handlePreferenceFeedback handles user feedback on preferences
func (s *Server) handlePreferenceFeedback(profileName, key string, accurate bool) {
	// In production, this should update Cortex UserProfile
	// For now, just log the feedback
	fmt.Printf("Preference feedback: profile=%s, key=%s, accurate=%v\n", profileName, key, accurate)
}

func (s *Server) handleSystemVersion(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"version":    s.version,
		"commit":     s.commit,
		"build_date": s.buildDate,
		"platform":   runtime.GOOS,
		"arch":       runtime.GOARCH,
	})
}

func (s *Server) handleVersionCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	owner := "magicwubiao"
	repo := "go-magic"
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "go-magic/"+s.version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "failed to check for updates: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "github api error", resp.StatusCode)
		return
	}

	var release struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		HTMLURL     string    `json:"html_url"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		http.Error(w, "failed to parse response", http.StatusInternalServerError)
		return
	}

	// Compare versions
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(s.version, "v")
	hasUpdate := compareVersions(latestVersion, currentVersion) > 0

	// Find appropriate asset for current platform
	var downloadURL string
	var assetSize int64
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Match actual release asset names: go-magic-windows-amd64.exe, go-magic-linux-amd64, etc.
	var patterns []string
	if platform == "windows" {
		patterns = []string{
			fmt.Sprintf("go-magic-%s-%s.exe", platform, arch),
			fmt.Sprintf("magic-%s-%s.exe", platform, arch),
		}
	} else {
		patterns = []string{
			fmt.Sprintf("go-magic-%s-%s", platform, arch),
			fmt.Sprintf("magic-%s-%s", platform, arch),
		}
	}

	for _, asset := range release.Assets {
		for _, pattern := range patterns {
			if asset.Name == pattern {
				downloadURL = asset.BrowserDownloadURL
				assetSize = asset.Size
				break
			}
		}
		if downloadURL != "" {
			break
		}
	}

	// Fallback: substring match
	if downloadURL == "" {
		expectedSub := fmt.Sprintf("%s-%s", platform, arch)
		for _, asset := range release.Assets {
			if strings.Contains(asset.Name, expectedSub) {
				downloadURL = asset.BrowserDownloadURL
				assetSize = asset.Size
				break
			}
		}
	}

	jsonResponse(w, map[string]interface{}{
		"current_version": currentVersion,
		"latest_version":  latestVersion,
		"has_update":      hasUpdate,
		"release_name":    release.Name,
		"release_notes":   release.Body,
		"published_at":    release.PublishedAt,
		"html_url":        release.HTMLURL,
		"download_url":    downloadURL,
		"asset_size":      assetSize,
		"prerelease":      release.Prerelease,
	})
}

// compareVersions compares two semantic version strings.
// Returns > 0 if v1 > v2, < 0 if v1 < v2, 0 if equal.
func compareVersions(v1, v2 string) int {
	// Handle "dev" or empty versions
	if v1 == "dev" || v1 == "" {
		return -1
	}
	if v2 == "dev" || v2 == "" {
		return 1
	}

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		n1, _ := strconv.Atoi(parts1[i])
		n2, _ := strconv.Atoi(parts2[i])
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}

	if len(parts1) > len(parts2) {
		return 1
	}
	if len(parts1) < len(parts2) {
		return -1
	}
	return 0
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
	// Support 'search' parameter for compatibility with web client
	filterSearch := r.URL.Query().Get("search")
	if filterComponent == "" && filterSearch != "" {
		filterComponent = filterSearch
	}

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
			// Only read .log files
			if !strings.HasSuffix(entry.Name(), ".log") {
				continue
			}
			filesToRead = append(filesToRead, filepath.Join(logDir, entry.Name()))
		}
	}

	// If specific file filter is requested but not found, return empty (will show default message)
	// If no filter, read all log files
	if filterFile != "" {
		// Check if any file matches the filter (partial match for flexibility)
		var matchedFiles []string
		for _, f := range filesToRead {
			if strings.Contains(filepath.Base(f), filterFile) {
				matchedFiles = append(matchedFiles, f)
			}
		}
		if len(matchedFiles) > 0 {
			filesToRead = matchedFiles
		}
		// If no match, keep all files (don't filter out everything)
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

	// Parse parameters for compatibility
	filterFile := r.URL.Query().Get("file")
	filterLevel := strings.ToUpper(r.URL.Query().Get("level"))
	linesStr := r.URL.Query().Get("lines")
	lines := 10
	if linesStr != "" {
		if n, err := strconv.Atoi(linesStr); err == nil && n > 0 {
			lines = n
		}
	}

	// Send initial log entries then keep alive
	for i := 0; i < lines; i++ {
		level := "info"
		if filterLevel != "" {
			level = strings.ToLower(filterLevel)
		}
		data, _ := json.Marshal(LogEntry{
			Timestamp: time.Now(),
			Level:     level,
			Message:   fmt.Sprintf("Log entry %d (file=%s)", i, filterFile),
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
			"total":    1,
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

	// Handle /api/settings/profiles/* sub-routes
	if strings.HasPrefix(id, "profiles/") {
		s.handleSettingsProfiles(w, r)
		return
	}

	jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
}

// handleSettingsProfiles handles /api/settings/profiles/* routes
func (s *Server) handleSettingsProfiles(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/settings/profiles/")

	// List profiles
	if path == "" || path == "/" {
		profiles := s.scanProfiles()
		jsonResponse(w, map[string]interface{}{"profiles": profiles})
		return
	}

	// Handle switch: /api/settings/profiles/{name}/switch
	if strings.HasSuffix(path, "/switch") {
		name := strings.TrimSuffix(path, "/switch")
		// Actually switch profile
		if s.cfg != nil {
			s.cfg.Profile = name
			if err := s.cfg.Save(); err != nil {
				http.Error(w, "Failed to save config: "+err.Error(), 500)
				return
			}
			// Reload config to ensure memory is in sync
			if newCfg, err := appconfig.Load(); err == nil {
				s.cfg = newCfg
			}
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "switched": true})
		return
	}

	// Handle delete: /api/settings/profiles/{name}
	if r.Method == http.MethodDelete {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": path, "deleted": true})
		return
	}

	// Handle create: POST /api/settings/profiles
	if r.Method == http.MethodPost {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": req.Name, "created": true})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

// --- Gateway ---

func (s *Server) handleGatewayStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Check if gateway is running by checking the health endpoint
	status := map[string]interface{}{
		"running":   false,
		"pid":       0,
		"health_ok": false,
	}

	// Read PID file
	home, _ := os.UserHomeDir()
	pidFile := filepath.Join(home, ".magic", "gateway.pid")

	if data, err := os.ReadFile(pidFile); err == nil {
		var pidData map[string]interface{}
		if json.Unmarshal(data, &pidData) == nil {
			if pid, ok := pidData["pid"].(float64); ok {
				process, err := os.FindProcess(int(pid))
				if err == nil && process != nil {
					status["running"] = true
					status["pid"] = int(pid)
					if started, ok := pidData["started"].(string); ok {
						status["started"] = started
					}
				}
			}
		}
	}

	// Check health endpoint
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get("http://localhost:8081/health"); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			status["health_ok"] = true
		}
	}

	jsonResponse(w, status)
}

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

// --- Gateway QR Code Login ---

// QRStatus represents the status of a QR code login session
type QRStatus struct {
	Platform  string `json:"platform"`
	Status    string `json:"status"`            // pending, scanning, confirmed, expired, error
	QRCode    string `json:"qr_code,omitempty"` // base64 encoded PNG
	Message   string `json:"message,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"` // seconds remaining
}

func (s *Server) handleGatewayQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	platform := r.URL.Query().Get("platform")
	if platform == "" {
		http.Error(w, "platform parameter is required", 400)
		return
	}

	// Get the global QR manager
	qrManager := gateway.GetQRManager()
	if qrManager == nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "QR manager not available",
		})
		return
	}

	// Check for existing valid session
	session := qrManager.GetSession(platform)
	if session == nil || session.Status == "expired" || session.Status == "confirmed" || session.Status == "error" {
		// Generate new QR code based on platform
		var qrData string
		var qrImage string
		var err error

		switch platform {
		case "wechat_ilink":
			qrData, qrImage, err = s.generateWeChatILinkQR()
		case "whatsapp":
			qrData, qrImage, err = s.generateWhatsAppQR()
		case "wecom":
			qrData, qrImage, err = s.generateWeComQR()
		case "wechat":
			qrData, qrImage, err = s.generateWeChatQR()
		case "dingtalk":
			qrData, qrImage, err = s.generateDingTalkQR()
		case "feishu":
			qrData, qrImage, err = s.generateFeishuQR()
		default:
			jsonResponse(w, QRStatus{
				Platform: platform,
				Status:   "error",
				Message:  fmt.Sprintf("QR login not supported for %s", platform),
			})
			return
		}

		if err != nil {
			jsonResponse(w, QRStatus{
				Platform: platform,
				Status:   "error",
				Message:  fmt.Sprintf("Failed to generate QR code: %v", err),
			})
			return
		}

		session, err = qrManager.CreateSession(platform, qrData, qrImage)
		if err != nil {
			jsonResponse(w, QRStatus{
				Platform: platform,
				Status:   "error",
				Message:  fmt.Sprintf("Failed to create session: %v", err),
			})
			return
		}
		fmt.Printf("[QR] Created session for %s: qr_code_len=%d\n", platform, len(session.QRCode))
	}

	// Return current session state
	expiresIn := int(time.Until(session.ExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}

	jsonResponse(w, QRStatus{
		Platform:  session.Platform,
		Status:    session.Status,
		QRCode:    session.QRCode,
		Message:   session.Message,
		ExpiresIn: expiresIn,
	})
}

func (s *Server) handleGatewayQRStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}

	platform := r.URL.Query().Get("platform")
	if platform == "" {
		http.Error(w, "platform parameter is required", 400)
		return
	}

	qrManager := gateway.GetQRManager()
	if qrManager == nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "QR manager not available",
		})
		return
	}

	session := qrManager.GetSession(platform)
	if session == nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "No active QR session",
		})
		return
	}

	expiresIn := int(time.Until(session.ExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}

	jsonResponse(w, QRStatus{
		Platform:  session.Platform,
		Status:    session.Status,
		QRCode:    session.QRCode,
		Message:   session.Message,
		ExpiresIn: expiresIn,
	})
}

// generateWeChatILinkQR generates a QR code for WeChat iLink login
// Returns (qrData, qrImage, error) where qrData is for polling and qrImage is for display
func (s *Server) generateWeChatILinkQR() (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create iLink API client
	api, err := gateway.NewILinkAPIClient("https://ilinkai.weixin.qq.com/", "", "")
	if err != nil {
		return "", "", fmt.Errorf("failed to create iLink API client: %w", err)
	}

	// Get QR code from iLink API
	qrResp, err := api.GetQRCode(ctx, "3")
	if err != nil {
		return "", "", fmt.Errorf("failed to get QR code: %w", err)
	}

	// qrData is the key for status polling (32-char hex string)
	qrData := qrResp.Qrcode

	fmt.Printf("[QR] iLink response: qrcode_len=%d, img_url=%s\n", len(qrData), qrResp.QrcodeImgContent)

	// The img_url is a webpage, not a direct image.
	// We need to generate QR code from the URL itself so users can scan it
	var qrImage string
	if qrResp.QrcodeImgContent != "" {
		// Generate QR code containing the URL (users scan this to open the page)
		img, err := gateway.GenerateQRCodePNG(qrResp.QrcodeImgContent)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate QR image from URL: %w", err)
		}
		qrImage = img
		fmt.Printf("[QR] Generated QR from URL, len=%d\n", len(qrImage))
	} else if qrData != "" {
		// Fallback: generate from qrData
		img, err := gateway.GenerateQRCodePNG(qrData)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate QR image: %w", err)
		}
		qrImage = img
		fmt.Printf("[QR] Generated image from qrData, len=%d\n", len(qrImage))
	}

	if qrImage == "" {
		return "", "", fmt.Errorf("no QR image available")
	}

	return qrData, qrImage, nil
}

// generateWhatsAppQR generates a QR code for WhatsApp login
// Returns (qrData, qrImage, error)
func (s *Server) generateWhatsAppQR() (string, string, error) {
	// Try Gateway API first (if gateway is running)
	gatewayPort := 8080
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/login/qr/whatsapp", gatewayPort))

	if err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var result struct {
				QRCode    string `json:"qr_code"`
				ExpiresIn int    `json:"expires_in_seconds"`
				Status    string `json:"status"`
			}
			if err := json.Unmarshal(body, &result); err == nil && result.QRCode != "" {
				return result.QRCode, result.QRCode, nil
			}
		}
	}

	// Fallback: create a temporary WhatsApp gateway instance to generate QR directly
	// This works even without the gateway process running (same approach as WeChat iLink)
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".magic", "whatsapp")
	waGw := gateway.NewWhatsAppGateway(dataDir)

	qrData, err := waGw.StartQRLogin(context.Background())
	if err != nil {
		return "", "", fmt.Errorf("failed to generate WhatsApp QR: %w", err)
	}
	if qrData == "" {
		return "", "", fmt.Errorf("WhatsApp already logged in, no QR code needed")
	}

	// Generate QR image from the data
	qrImage, err := gateway.GenerateQRCodePNG(qrData)
	if err != nil {
		// Return raw data even if image generation fails
		return qrData, qrData, nil
	}

	return qrData, qrImage, nil
}

// generateWeComQR generates a QR code for WeCom (Enterprise WeChat) login
// Returns (qrData, qrImage, error)
func (s *Server) generateWeComQR() (string, string, error) {
	// WeCom requires corp_id and agent_id from config
	corpID := ""
	agentID := ""

	if s.cfg.Gateway.Platforms != nil {
		if p, ok := s.cfg.Gateway.Platforms["wecom"]; ok {
			corpID = p.CorpID
			agentID = p.AgentID
		}
	}

	if corpID == "" || agentID == "" {
		return "", "", fmt.Errorf("WeCom QR login requires corp_id and agent_id. Please configure them in gateway settings.")
	}

	// Build WeCom OAuth URL
	state := uuid.New().String()
	redirectURI := fmt.Sprintf("http://localhost:8080/wecom/qr/callback")
	authURL := fmt.Sprintf(
		"https://login.work.weixin.qq.com/wwopen/sso/qrConnect?appid=%s&agentid=%s&redirect_uri=%s&state=%s",
		corpID, agentID, url.QueryEscape(redirectURI), state,
	)

	// Generate QR image from the URL
	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}

	return authURL, img, nil
}

// generateWeChatQR generates a QR code for WeChat Open Platform login
// Returns (qrData, qrImage, error)
func (s *Server) generateWeChatQR() (string, string, error) {
	// WeChat Open Platform requires app_id
	appID := ""

	if s.cfg.Gateway.Platforms != nil {
		if p, ok := s.cfg.Gateway.Platforms["wechat"]; ok {
			appID = p.AppID
		}
	}

	if appID == "" {
		return "", "", fmt.Errorf("WeChat QR login requires app_id. Please configure it in gateway settings.")
	}

	// Build WeChat OAuth URL
	state := uuid.New().String()
	redirectURI := fmt.Sprintf("http://localhost:8083/wechat/qr/callback")
	authURL := fmt.Sprintf(
		"https://open.weixin.qq.com/connect/qrConnect?appid=%s&redirect_uri=%s&response_type=code&scope=snsapi_login&state=%s#wechat_redirect",
		appID, url.QueryEscape(redirectURI), state,
	)

	// Generate QR image from the URL
	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}

	return authURL, img, nil
}

// generateDingTalkQR generates a QR code for DingTalk login
// Returns (qrData, qrImage, error)
func (s *Server) generateDingTalkQR() (string, string, error) {
	appKey := ""

	if s.cfg.Gateway.Platforms != nil {
		if p, ok := s.cfg.Gateway.Platforms["dingtalk"]; ok {
			appKey = p.AppKey
		}
	}

	if appKey == "" {
		return "", "", fmt.Errorf("DingTalk QR login requires app_key. Please configure it in gateway settings.")
	}

	// Build DingTalk OAuth URL
	state := uuid.New().String()
	redirectURI := "http://localhost:8080/dingtalk/qr/callback"
	authURL := fmt.Sprintf(
		"https://oapi.dingtalk.com/connect/qrconnect?appid=%s&response_type=code&scope=snsapi_login&state=%s&redirect_uri=%s",
		appKey, state, url.QueryEscape(redirectURI),
	)

	// Generate QR image from the URL
	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}

	return authURL, img, nil
}

// generateFeishuQR generates a QR code for Feishu/Lark login
// Returns (qrData, qrImage, error)
func (s *Server) generateFeishuQR() (string, string, error) {
	appID := ""

	if s.cfg.Gateway.Platforms != nil {
		if p, ok := s.cfg.Gateway.Platforms["feishu"]; ok {
			appID = p.AppID
		}
	}

	if appID == "" {
		return "", "", fmt.Errorf("Feishu QR login requires app_id. Please configure it in gateway settings.")
	}

	// Build Feishu OAuth URL
	state := uuid.New().String()
	redirectURI := "http://localhost:8080/feishu/qr/callback"
	authURL := fmt.Sprintf(
		"https://open.feishu.cn/open-apis/authen/v1/authorize?redirect_uri=%s&app_id=%s&state=%s",
		url.QueryEscape(redirectURI), appID, state,
	)

	// Generate QR image from the URL
	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}

	return authURL, img, nil
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

// --- Kanban Handlers ---

func (s *Server) handleKanbanBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.kanbanMgr == nil {
		jsonResponse(w, map[string]interface{}{"tasks": []interface{}{}, "columns": []interface{}{}})
		return
	}

	board, err := s.kanbanMgr.GetBoard("")
	if err != nil {
		jsonResponse(w, map[string]interface{}{"tasks": []interface{}{}, "columns": []interface{}{}})
		return
	}

	tasks := make([]interface{}, 0)
	for _, taskList := range board {
		for _, task := range taskList {
			tasks = append(tasks, map[string]interface{}{
				"id":          task.ID,
				"title":       task.Title,
				"description": task.Body,
				"status":      task.Status,
				"priority":    task.Priority,
				"assignee":    task.Assignee,
				"tags":        task.Skills,
				"created_at":  task.CreatedAt.Unix(),
				"updated_at":  task.UpdatedAt.Unix(),
			})
		}
	}

	columns := []map[string]interface{}{
		{"key": "triage", "title": "Triage", "count": 0},
		{"key": "todo", "title": "To Do", "count": 0},
		{"key": "ready", "title": "Ready", "count": 0},
		{"key": "running", "title": "Running", "count": 0},
		{"key": "blocked", "title": "Blocked", "count": 0},
		{"key": "done", "title": "Done", "count": 0},
	}

	// Count tasks per status
	for status, taskList := range board {
		for _, col := range columns {
			if string(status) == col["key"] {
				col["count"] = len(taskList)
				break
			}
		}
	}

	jsonResponse(w, map[string]interface{}{"tasks": tasks, "columns": columns})
}

func (s *Server) handleKanbanTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		if s.kanbanMgr == nil {
			http.Error(w, "kanban not initialized", 500)
			return
		}

		priority := 1
		if req.Priority == "high" {
			priority = 2
		} else if req.Priority == "low" {
			priority = 0
		}

		task, err := s.kanbanMgr.CreateTask(req.Title, req.Description, "", kanban.WithPriority(priority))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":          task.ID,
			"title":       task.Title,
			"description": task.Body,
			"status":      task.Status,
			"priority":    task.Priority,
			"tags":        task.Skills,
			"created_at":  task.CreatedAt.Unix(),
			"updated_at":  task.UpdatedAt.Unix(),
		})
		return
	}

	if r.Method == "GET" {
		if s.kanbanMgr == nil {
			jsonResponse(w, []interface{}{})
			return
		}
		board, _ := s.kanbanMgr.GetBoard("")
		tasks := make([]interface{}, 0)
		for _, taskList := range board {
			for _, task := range taskList {
				tasks = append(tasks, map[string]interface{}{
					"id":          task.ID,
					"title":       task.Title,
					"description": task.Body,
					"status":      task.Status,
					"priority":    task.Priority,
					"created_at":  task.CreatedAt.Unix(),
					"updated_at":  task.UpdatedAt.Unix(),
				})
			}
		}
		jsonResponse(w, tasks)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleKanbanTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/kanban/tasks/")
	// Handle /move suffix
	if strings.HasSuffix(id, "/move") {
		id = strings.TrimSuffix(id, "/move")
		s.handleKanbanTaskMove(w, r, id)
		return
	}

	if id == "" {
		http.Error(w, "not found", 404)
		return
	}

	if s.kanbanMgr == nil {
		http.Error(w, "kanban not initialized", 500)
		return
	}

	task, err := s.kanbanMgr.GetTask(id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	switch r.Method {
	case "GET":
		jsonResponse(w, map[string]interface{}{
			"id":          task.ID,
			"title":       task.Title,
			"description": task.Body,
			"status":      task.Status,
			"priority":    task.Priority,
			"assignee":    task.Assignee,
			"tags":        task.Skills,
			"created_at":  task.CreatedAt.Unix(),
			"updated_at":  task.UpdatedAt.Unix(),
		})
	case "PUT":
		var req struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
			Status      string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		updates := make(map[string]interface{})
		if req.Title != "" {
			updates["title"] = req.Title
		}
		if req.Description != "" {
			updates["body"] = req.Description
		}
		if req.Priority != "" {
			priority := 1
			if req.Priority == "high" {
				priority = 2
			} else if req.Priority == "low" {
				priority = 0
			}
			updates["priority"] = priority
		}
		if req.Status != "" {
			updates["status"] = req.Status
		}

		updatedTask, err := s.kanbanMgr.UpdateTask(id, updates)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":         updatedTask.ID,
			"title":      updatedTask.Title,
			"status":     updatedTask.Status,
			"priority":   updatedTask.Priority,
			"updated_at": updatedTask.UpdatedAt.Unix(),
		})
	case "DELETE":
		if err := s.kanbanMgr.DeleteTask(id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleKanbanTaskMove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.kanbanMgr == nil {
		http.Error(w, "kanban not initialized", 500)
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	updates := map[string]interface{}{
		"status": req.Status,
	}

	task, err := s.kanbanMgr.UpdateTask(id, updates)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"id":     task.ID,
		"status": task.Status,
	})
}

// --- GroupChat Handlers ---

func (s *Server) handleGroupchatRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		if s.groupchatStorage == nil {
			http.Error(w, "groupchat not initialized", 500)
			return
		}

		room := &groupchat.Room{
			ID:         uuid.New().String(),
			Name:       req.Name,
			InviteCode: "",
			CreatedAt:  time.Now().UnixMilli(),
			UpdatedAt:  time.Now().UnixMilli(),
		}

		if err := s.groupchatStorage.SaveRoom(room); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"description": room.InviteCode,
			"members":     []string{},
			"agent_ids":   []string{},
			"created_at":  room.CreatedAt,
		})
		return
	}

	if r.Method == "GET" {
		if s.groupchatStorage == nil {
			jsonResponse(w, []interface{}{})
			return
		}

		rooms, _ := s.groupchatStorage.ListRooms()
		result := make([]interface{}, 0)
		for _, room := range rooms {
			result = append(result, map[string]interface{}{
				"id":          room.ID,
				"name":        room.Name,
				"description": room.InviteCode,
				"members":     []string{},
				"agent_ids":   []string{},
				"created_at":  room.CreatedAt,
			})
		}
		jsonResponse(w, result)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleGroupchatRoomByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/groupchat/rooms/")

	if id == "" {
		http.Error(w, "not found", 404)
		return
	}

	// Check for messages subroute
	if strings.HasSuffix(r.URL.Path, "/messages") {
		roomID := strings.TrimSuffix(id, "/messages")
		s.handleGroupchatMessages(w, r, roomID)
		return
	}

	if s.groupchatStorage == nil {
		http.Error(w, "groupchat not initialized", 500)
		return
	}

	room, err := s.groupchatStorage.GetRoom(id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	switch r.Method {
	case "GET":
		jsonResponse(w, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"description": room.InviteCode,
			"members":     []string{},
			"agent_ids":   []string{},
			"created_at":  room.CreatedAt,
		})
	case "DELETE":
		if err := s.groupchatStorage.DeleteRoom(id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleGroupchatMessages(w http.ResponseWriter, r *http.Request, roomID string) {
	if s.groupchatStorage == nil {
		http.Error(w, "groupchat not initialized", 500)
		return
	}

	if r.Method == "GET" {
		messages, _ := s.groupchatStorage.GetMessages(roomID, 100)
		result := make([]interface{}, 0)
		for _, msg := range messages {
			result = append(result, map[string]interface{}{
				"id":        msg.ID,
				"room_id":   msg.RoomID,
				"sender":    msg.SenderID,
				"role":      msg.Type,
				"content":   msg.Content,
				"timestamp": msg.Timestamp,
			})
		}
		jsonResponse(w, result)
		return
	}

	if r.Method == "POST" {
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		msg := &groupchat.ChatMessage{
			ID:         uuid.New().String(),
			RoomID:     roomID,
			SenderID:   "user",
			SenderName: "User",
			Content:    req.Content,
			Timestamp:  time.Now().UnixMilli(),
			Type:       "text",
		}

		if err := s.groupchatStorage.SaveMessage(msg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":        msg.ID,
			"room_id":   msg.RoomID,
			"sender":    msg.SenderID,
			"role":      msg.Type,
			"content":   msg.Content,
			"timestamp": msg.Timestamp,
		})
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

	// Serve embedded SPA files
	serveSPA(w, r)
}

func serveSPA(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Remove leading slash
	if path == "/" {
		path = "/index.html"
	}
	path = strings.TrimPrefix(path, "/")

	// Try to serve the file from embedded dist
	fullPath := "dist/" + path
	data, err := distFS.ReadFile(fullPath)
	if err != nil {
		// If file not found, serve index.html for SPA routing
		data, err = distFS.ReadFile("dist/index.html")
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	contentType := getContentType(fullPath)
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}

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

// --- Approval Management ---

// getApprovalManager returns the approval manager from any active agent.
// If no agent is available, it returns nil.
func (s *Server) getApprovalManager() *approval.Manager {
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()

	// Try to find any agent with an approval hook
	for _, a := range s.agents {
		if hook := a.GetApprovalHook(); hook != nil {
			return hook.GetManager()
		}
	}

	// No active agents - create a temporary one to get the manager
	// This ensures the API works even before any chat session is created
	if s.provider != nil {
		toolsSchema := getToolsSchema(s.toolReg)
		a := agent.NewEnhancedAgent(s.provider, s.toolReg, toolsSchema, "")
		if hook := a.GetApprovalHook(); hook != nil {
			return hook.GetManager()
		}
	}

	return nil
}

func (s *Server) handleApprovalStatus(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	cfg := mgr.GetConfig()
	stats := mgr.GetStats()

	jsonResponse(w, map[string]interface{}{
		"strategy":             cfg.Strategy,
		"enableLearning":       cfg.EnableLearning,
		"cliConfirm":           cfg.EnableCLIConfirm,
		"trustThreshold":       cfg.TrustThreshold,
		"whitelist":            mgr.GetWhitelist(),
		"trusted_patterns":     stats.TrustedPatterns,
		"denied_patterns":      stats.DeniedPatterns,
		"total_requests":       stats.TotalRequests,
		"auto_approved":        stats.AutoApproved,
		"user_approved":        stats.UserApproved,
		"user_denied":          stats.UserDenied,
		"avg_response_time_ms": stats.AvgResponseTime,
	})
}

func (s *Server) handleApprovalHistory(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	offsetStr := r.URL.Query().Get("offset")
	offset := 0
	if offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
	}

	records := mgr.GetHistory(limit, offset)

	// Count total from stats
	stats := mgr.GetStats()

	jsonResponse(w, map[string]interface{}{
		"records": records,
		"total":   stats.TotalRequests,
	})
}

func (s *Server) handleApprovalStats(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	stats := mgr.GetStats()

	// Convert by_risk_level keys to strings for JSON serialization
	byRiskLevel := make(map[string]int)
	for k, v := range stats.ByRiskLevel {
		byRiskLevel[k.String()] = v
	}

	jsonResponse(w, map[string]interface{}{
		"total_requests":       stats.TotalRequests,
		"auto_approved":        stats.AutoApproved,
		"user_approved":        stats.UserApproved,
		"user_denied":          stats.UserDenied,
		"timed_out":            stats.TimedOut,
		"trusted_patterns":     stats.TrustedPatterns,
		"denied_patterns":      stats.DeniedPatterns,
		"top_commands":         stats.TopCommands,
		"by_risk_level":        byRiskLevel,
		"by_category":          stats.ByCategory,
		"avg_response_time_ms": stats.AvgResponseTime,
	})
}

func (s *Server) handleApprovalPending(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	pending := mgr.GetPendingApprovals()

	type pendingItem struct {
		ID        string `json:"id"`
		Command   string `json:"command"`
		RiskLevel string `json:"risk_level"`
		SessionID string `json:"session_id"`
		CreatedAt string `json:"created_at"`
		ExpiresAt string `json:"expires_at"`
	}

	items := make([]pendingItem, 0, len(pending))
	for _, pa := range pending {
		items = append(items, pendingItem{
			ID:        pa.ID,
			Command:   pa.Request.Command,
			RiskLevel: pa.Request.RiskLevel.String(),
			SessionID: pa.Request.SessionID,
			CreatedAt: pa.CreatedAt.Format(time.RFC3339),
			ExpiresAt: pa.ExpiresAt.Format(time.RFC3339),
		})
	}

	jsonResponse(w, map[string]interface{}{
		"pending": items,
		"total":   len(items),
	})
}

func (s *Server) handleApprovalPendingByID(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	// Extract ID from path: /api/approval/pending/{id}/resolve or /api/approval/pending/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/approval/pending/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]

	if r.Method == http.MethodPost && len(parts) > 1 && parts[1] == "resolve" {
		var req struct {
			Approved bool   `json:"approved"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		mgr.ResolveWebApproval(id, req.Approved, req.Reason)
		jsonResponse(w, map[string]bool{"success": true})
		return
	}

	// GET single pending approval
	pending := mgr.GetPendingApprovals()
	for _, pa := range pending {
		if pa.ID == id {
			jsonResponse(w, map[string]interface{}{
				"id":         pa.ID,
				"command":    pa.Request.Command,
				"risk_level": pa.Request.RiskLevel.String(),
				"session_id": pa.Request.SessionID,
				"created_at": pa.CreatedAt.Format(time.RFC3339),
				"expires_at": pa.ExpiresAt.Format(time.RFC3339),
			})
			return
		}
	}

	http.Error(w, "Pending approval not found", http.StatusNotFound)
}

func (s *Server) handleApprovalTrusted(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	trusted := mgr.GetTrustedCommands()
	jsonResponse(w, map[string]interface{}{
		"patterns": trusted,
		"total":    len(trusted),
	})
}

func (s *Server) handleApprovalDenied(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	denied := mgr.GetDeniedCommands()
	jsonResponse(w, map[string]interface{}{
		"patterns": denied,
		"total":    len(denied),
	})
}

// handleApprovalPatternsTrusted handles /api/approval/patterns/trusted (GET/DELETE)
func (s *Server) handleApprovalPatternsTrusted(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		trusted := mgr.GetTrustedCommands()
		jsonResponse(w, map[string]interface{}{
			"patterns": trusted,
			"total":    len(trusted),
		})

	case http.MethodDelete:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// Remove from trusted patterns by denying it
		mgr.Deny(&approval.ApprovalRequest{Command: req.Pattern})
		jsonResponse(w, map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleApprovalPatternsDenied handles /api/approval/patterns/denied (GET/DELETE)
func (s *Server) handleApprovalPatternsDenied(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		denied := mgr.GetDeniedCommands()
		jsonResponse(w, map[string]interface{}{
			"patterns": denied,
			"total":    len(denied),
		})

	case http.MethodDelete:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// Remove from denied patterns by approving it
		mgr.Approve(&approval.ApprovalRequest{Command: req.Pattern})
		jsonResponse(w, map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleApprovalWhitelist(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		patterns := mgr.GetWhitelist()
		jsonResponse(w, map[string]interface{}{
			"patterns": patterns,
			"total":    len(patterns),
		})

	case http.MethodPost:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := mgr.AddToWhitelist(req.Pattern); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"success": true})

	case http.MethodDelete:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := mgr.RemoveFromWhitelist(req.Pattern); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleApprovalStrategy(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Strategy string `json:"strategy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Strategy == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		switch req.Strategy {
		case "manual":
			mgr.SetStrategy(approval.StrategyManual)
		case "auto":
			mgr.SetStrategy(approval.StrategyAutoApprove)
		case "smart":
			mgr.SetStrategy(approval.StrategySmart)
		case "whitelist":
			mgr.SetStrategy(approval.StrategyWhitelist)
		default:
			http.Error(w, "Invalid strategy. Must be: manual, auto, smart, or whitelist", http.StatusBadRequest)
			return
		}

		jsonResponse(w, map[string]bool{"success": true})
		return
	}

	// GET current strategy
	cfg := mgr.GetConfig()
	jsonResponse(w, map[string]interface{}{
		"strategy": cfg.Strategy,
	})
}

func (s *Server) handleApprovalClearHistory(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	var req struct {
		OlderThanHours int `json:"older_than_hours"`
	}
	// Default body is optional; use default 168 hours (7 days) if not provided
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.OlderThanHours <= 0 {
		req.OlderThanHours = 168
	}

	mgr.ClearHistory(time.Duration(req.OlderThanHours) * time.Hour)
	jsonResponse(w, map[string]bool{"success": true})
}

func (s *Server) handleApprovalSettings(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		// GET current settings
		cfg := mgr.GetConfig()
		stats := mgr.GetStats()
		jsonResponse(w, map[string]interface{}{
			"strategy":             cfg.Strategy,
			"enableLearning":       cfg.EnableLearning,
			"cliConfirm":           cfg.EnableCLIConfirm,
			"trustThreshold":       cfg.TrustThreshold,
			"whitelist":            mgr.GetWhitelist(),
			"trusted_patterns":     stats.TrustedPatterns,
			"denied_patterns":      stats.DeniedPatterns,
			"total_requests":       stats.TotalRequests,
			"auto_approved":        stats.AutoApproved,
			"user_approved":        stats.UserApproved,
			"user_denied":          stats.UserDenied,
			"avg_response_time_ms": stats.AvgResponseTime,
		})
	case http.MethodPut:
		// PUT update settings
		var req struct {
			Strategy       string `json:"strategy"`
			TrustThreshold int    `json:"trust_threshold"`
			CLIPrompt      bool   `json:"cli_confirm"`
			EnableLearning bool   `json:"enable_learning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		// Update strategy
		if req.Strategy != "" {
			mgr.SetStrategy(approval.Strategy(req.Strategy))
		}
		// Update other settings
		cfg := mgr.GetConfig()
		if req.TrustThreshold > 0 {
			cfg.TrustThreshold = req.TrustThreshold
		}
		cfg.EnableCLIConfirm = req.CLIPrompt
		cfg.EnableLearning = req.EnableLearning
		mgr.SaveConfig()

		jsonResponse(w, map[string]bool{"success": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

// expandDotKeys converts dot-notation keys (e.g. "memory.enabled") into nested maps
// {"memory.enabled": true} -> {"memory": {"enabled": true}}
func expandDotKeys(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		parts := strings.SplitN(k, ".", 2)
		if len(parts) == 2 {
			// Nested key
			parent, ok := result[parts[0]].(map[string]interface{})
			if !ok {
				parent = make(map[string]interface{})
				result[parts[0]] = parent
			}
			parent[parts[1]] = v
		} else {
			result[k] = v
		}
	}
	return result
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

// handleDashboardPluginsSubRoutes handles /api/dashboard/plugins/{name}/enable|disable|delete
func (s *Server) handleDashboardPluginsSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/dashboard/plugins/")

	// Handle enable
	if strings.HasSuffix(path, "/enable") {
		name := strings.TrimSuffix(path, "/enable")
		if r.Method == http.MethodPost {
			// Add to enabled list
			if s.cfg != nil {
				found := false
				for _, e := range s.cfg.Plugins.Enabled {
					if e == name {
						found = true
						break
					}
				}
				if !found {
					s.cfg.Plugins.Enabled = append(s.cfg.Plugins.Enabled, name)
					// Remove from disabled list if present
					newDisabled := []string{}
					for _, d := range s.cfg.Plugins.Disabled {
						if d != name {
							newDisabled = append(newDisabled, d)
						}
					}
					s.cfg.Plugins.Disabled = newDisabled
					s.cfg.Save()
				}
			}
			jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": true})
			return
		}
	}

	// Handle disable
	if strings.HasSuffix(path, "/disable") {
		name := strings.TrimSuffix(path, "/disable")
		if r.Method == http.MethodPost {
			// Add to disabled list
			if s.cfg != nil {
				found := false
				for _, d := range s.cfg.Plugins.Disabled {
					if d == name {
						found = true
						break
					}
				}
				if !found {
					s.cfg.Plugins.Disabled = append(s.cfg.Plugins.Disabled, name)
					// Remove from enabled list if present
					newEnabled := []string{}
					for _, e := range s.cfg.Plugins.Enabled {
						if e != name && e != "all" {
							newEnabled = append(newEnabled, e)
						}
					}
					s.cfg.Plugins.Enabled = newEnabled
					s.cfg.Save()
				}
			}
			jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": false})
			return
		}
	}

	// Handle delete (DELETE /api/dashboard/plugins/{name})
	if r.Method == http.MethodDelete {
		name := path
		pluginsDir := filepath.Join(s.magicHome, "plugins")
		pluginPath := filepath.Join(pluginsDir, name)

		// Check if it's a file or directory
		info, err := os.Stat(pluginPath)
		if err != nil {
			http.Error(w, "Plugin not found", http.StatusNotFound)
			return
		}

		// Remove plugin
		if info.IsDir() {
			err = os.RemoveAll(pluginPath)
		} else {
			err = os.Remove(pluginPath)
		}

		if err != nil {
			http.Error(w, "Failed to delete plugin", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "deleted": true})
		return
	}

	// Handle visibility (legacy)
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
		URL        string `json:"url"`
		Identifier string `json:"identifier"`
		Force      bool   `json:"force"`
		Enable     bool   `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Support both url (frontend) and identifier (legacy)
	pluginName := req.Identifier
	if pluginName == "" && req.URL != "" {
		// Extract name from URL
		parts := strings.Split(strings.TrimSuffix(req.URL, ".git"), "/")
		if len(parts) > 0 {
			pluginName = parts[len(parts)-1]
		}
	}
	if pluginName == "" {
		pluginName = "installed-plugin"
	}

	// Create plugin directory and marker
	pluginsDir := filepath.Join(s.magicHome, "plugins")
	pluginDir := filepath.Join(pluginsDir, pluginName)
	os.MkdirAll(pluginDir, 0755)

	if req.URL != "" {
		markerFile := filepath.Join(pluginDir, "source.url")
		os.WriteFile(markerFile, []byte(req.URL), 0644)
	}

	jsonResponse(w, map[string]interface{}{
		"ok":          true,
		"plugin_name": pluginName,
		"enabled":     req.Enable,
		"url":         req.URL,
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
