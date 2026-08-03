package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/approval"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/cron"
	"github.com/magicwubiao/go-magic/internal/goal"
	"github.com/magicwubiao/go-magic/internal/groupchat"
	"github.com/magicwubiao/go-magic/internal/kanban"
	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/metrics"
	"github.com/magicwubiao/go-magic/internal/plugin"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/internal/usage"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

//go:embed dist
var distFS embed.FS

type Server struct {
	mu           sync.RWMutex
	startTime    time.Time
	cfg          *appconfig.Config
	sessionStore *session.Store
	provider     provider.Provider
	toolReg      *tool.Registry
	skillMgr     *skills.Manager
	magicHome    string
	execPath     string // Store the executable path for gateway restart
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

	// MCP manager
	mcpMgr *mcp.Manager

	// Plugin manager
	pluginMgr *plugin.Manager

	// GroupChat storage
	groupchatStorage *groupchat.Storage

	// Goal manager
	goalMgr *goal.Manager

	// Cortex Agent manager for memory and context
	cortexMgr *cortex.Manager

	// Approval manager (independent of agents)
	approvalMgr *approval.Manager

	// pendingSSEHandlers 注册每个 session 的 SSE 审批推送回调。
	// 当 ApprovalHook 创建 pending 时，manager 的全局回调按 sessionID 查表分发。
	pendingSSEHandlers   map[string]func(approval.PendingApprovalInfo)
	pendingSSEHandlersMu sync.Mutex

	// Usage manager
	usageMgr *usage.Manager

	// Track cumulative token counts per session to compute deltas
	sessionTokens   map[string][2]int // [inputTokens, outputTokens]
	sessionTokensMu sync.Mutex

	// Background actions tracking
	actions   map[string]*ActionStatus
	actionsMu sync.RWMutex

	// Auth
	authToken string
	authMu    sync.RWMutex

	// CORS allowed origins (empty list => default to local-only)
	allowedOrigins []string

	// Share tokens for temporary read-only file access
	shareTokens   map[string]*ShareToken
	shareTokensMu sync.RWMutex

	// HTTP server reference for graceful shutdown
	httpServer *http.Server

	// Metrics collector ( lazily usable via /metrics endpoint )
	metricsMgr *metrics.Metrics
}

// isAllowedOrigin 校验 origin 是否在允许列表内。
// 当 allowedOrigins 为空时，默认仅允许本地回环地址。
func (s *Server) isAllowedOrigin(origin string) bool {
	if len(s.allowedOrigins) == 0 {
		// 默认仅允许本地
		allowed := []string{
			"http://localhost:8642",
			"http://127.0.0.1:8642",
			"http://localhost:8643",
			"http://127.0.0.1:8643",
		}
		for _, a := range allowed {
			if a == origin {
				return true
			}
		}
		return false
	}
	for _, a := range s.allowedOrigins {
		if a == origin || a == "*" {
			return true
		}
	}
	return false
}

func NewServer(dbPath string) *Server {
	magicHome := appconfig.GetMagicHome()
	os.MkdirAll(magicHome, 0755)

	// Create default profile directory structure
	defaultProfileDir := filepath.Join(magicHome, "profiles", "default")
	defaultProfileDirs := []string{
		defaultProfileDir,
		filepath.Join(defaultProfileDir, "skills"),
		filepath.Join(defaultProfileDir, "memory"),
		filepath.Join(defaultProfileDir, "sessions"),
		filepath.Join(defaultProfileDir, "plugins"),
		filepath.Join(defaultProfileDir, "cache"),
		filepath.Join(defaultProfileDir, "logs"),
	}
	for _, dir := range defaultProfileDirs {
		os.MkdirAll(dir, 0755)
	}

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
		// Session store unavailable, chat sessions will not be persisted
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

	// Create skills manager with config
	skillCfg := skills.ManagerConfig{
		BuiltinDir: filepath.Join(magicHome, "builtin_skills"),
	}
	skillMgr, _ := skills.NewManagerWithConfig(&skillCfg)

	// Register skill invoke tool with the tool registry
	registry.RegisterSkillTool(skillMgr)

	// Create cron manager
	cronMgr, err := cron.NewManager()
	if err != nil {
		// Cron manager unavailable
	} else if prov != nil && registry != nil {
		// Set LLM provider and tools for cron agent mode
		cronMgr.SetAgentDeps(prov, registry)
	}

	// Initialize Kanban Manager
	var kanbanMgr *kanban.Manager
	kanbanMgr, err = kanban.NewManager(magicHome)
	if err != nil {
		// Kanban manager unavailable
	} else {
		kanbanMgr.Init()
	}

	// Initialize Plugin Manager
	var pluginMgr *plugin.Manager
	pluginMgr, err = plugin.NewManager(nil)
	// Plugin manager may fail, continue without it

	// Initialize GroupChat Storage
	var groupchatStorage *groupchat.Storage
	groupchatStorage, err = groupchat.NewStorageFromHome(magicHome)

	// Initialize Goal Manager
	var goalMgr *goal.Manager
	goalMgr, err = goal.NewManager(magicHome)
	// Goal manager may fail, continue without it

	// Load disabled skills from config
	disabledSkills := make(map[string]bool)
	if cfg != nil {
		for _, name := range cfg.Skills.Disabled {
			disabledSkills[name] = true
		}
	}

	// Initialize Cortex Manager for memory and context
	cortexDir := filepath.Join(magicHome, "cortex")
	cortexConfig := &cortex.ManagerConfig{
		Enabled:             true,
		SkillMinPatternFreq: 3,
	}
	if cfg != nil {
		cortexConfig.Enabled = cfg.Cortex.Enabled
		if cfg.Cortex.SkillMinPatternFreq > 0 {
			cortexConfig.SkillMinPatternFreq = cfg.Cortex.SkillMinPatternFreq
		}
	}
	cortexMgr := cortex.NewManagerWithProfileAndConfig(cortexDir, prov, "", cortexConfig)
	if err := cortexMgr.Start(); err != nil {
		log.Warnf("Cortex manager start failed, continuing without it: %v", err)
	}

	// Bridge cortex's auto skill creator with the skills Manager so
	// auto-generated skills (in <cortexDir>/auto_skills) are registered
	// and visible via /api/skills.
	if skillMgr != nil {
		cortexMgr.BindSkillsManager(skillMgr)
		// Also load any previously generated auto skills from disk.
		loadAutoSkillsIntoManager(skillMgr, filepath.Join(cortexDir, "auto_skills"))
	}

	// Initialize Approval Manager independently (not tied to agents)
	// Read approval config from main config file if available
	approvalCfg := approval.DefaultConfig()
	if cfg != nil && cfg.Approval != nil {
		ac := cfg.Approval
		approvalCfg.Strategy = approval.Strategy(ac.Strategy)
		approvalCfg.TrustThreshold = ac.TrustThreshold
		approvalCfg.EnableLearning = ac.EnableLearning
		approvalCfg.EnableCLIConfirm = ac.EnableCLIConfirm
		// 仅在显式设置 (>0) 时覆盖，否则保留 DefaultConfig 的 60s。
		// 若无条件覆盖，配置文件中 approval 段未写 approval_timeout 时，
		// Go 零值 0 会使 pending 审批立即过期，用户来不及点击批准。
		if ac.ApprovalTimeout > 0 {
			approvalCfg.ApprovalTimeout = ac.ApprovalTimeout
		}
	}
	approvalMgr, err := approval.NewManager(approvalCfg)
	if err != nil {
		approvalMgr = nil
	} else {
		// If no persisted config, use main config values (already set above)
		loadedStrategy := approvalMgr.GetConfig().Strategy
		if loadedStrategy == "" {
			approvalMgr.SetStrategy(approval.StrategySmart)
		}
	}

	// Create usage manager
	usageMgr, err := usage.NewManager(filepath.Join(magicHome, "usage"))
	if err != nil {
		usageMgr = nil
	}

	// Initialize MCP manager
	mcpMgr := mcp.NewManager()

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

	// Get the executable path for gateway restart
	// Use /home/www/magic if it exists, otherwise fall back to os.Executable()
	execPath, _ := os.Executable()
	if _, err := os.Stat("/home/www/magic"); err == nil {
		execPath = "/home/www/magic"
	}

	// Load allowed CORS origins from env (comma-separated). Empty => default local-only.
	var allowedOrigins []string
	if envOrigins := os.Getenv("GO_MAGIC_CORS_ORIGINS"); envOrigins != "" {
		for _, o := range strings.Split(envOrigins, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				allowedOrigins = append(allowedOrigins, o)
			}
		}
	}

	s := &Server{
		mu:                 sync.RWMutex{},
		startTime:          time.Now(),
		cfg:                cfg,
		sessionStore:       store,
		provider:           prov,
		toolReg:            registry,
		skillMgr:           skillMgr,
		magicHome:          magicHome,
		execPath:           execPath,
		version:            version,
		commit:             "unknown",
		buildDate:          "unknown",
		agents:             make(map[string]*agent.Agent),
		disabledSkills:     disabledSkills,
		cronMgr:            cronMgr,
		kanbanMgr:          kanbanMgr,
		pluginMgr:          pluginMgr,
		groupchatStorage:   groupchatStorage,
		goalMgr:            goalMgr,
		cortexMgr:          cortexMgr,
		approvalMgr:        approvalMgr,
		usageMgr:           usageMgr,
		mcpMgr:             mcpMgr,
		actions:            make(map[string]*ActionStatus),
		sessionTokens:      make(map[string][2]int),
		authToken:          authToken,
		allowedOrigins:     allowedOrigins,
		shareTokens:        make(map[string]*ShareToken),
		metricsMgr:         metrics.NewMetrics(),
		pendingSSEHandlers: make(map[string]func(approval.PendingApprovalInfo)),
	}

	// 注册全局 pending 回调：按 sessionID 分发到对应 SSE 流。
	// 仅当 approvalMgr 存在时注册。
	if s.approvalMgr != nil {
		s.approvalMgr.SetOnPendingCreated(func(info approval.PendingApprovalInfo) {
			s.pendingSSEHandlersMu.Lock()
			handler, ok := s.pendingSSEHandlers[info.SessionID]
			s.pendingSSEHandlersMu.Unlock()
			if ok && handler != nil {
				handler(info)
			}
		})
	}

	// Start cron scheduler
	if cronMgr != nil {
		cronMgr.Start()
	}

	// Start agent cleanup goroutine to prevent memory leaks
	go s.agentCleanupLoop()

	return s
}

func safeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[server] PANIC in goroutine: %v\n%s", r, debug.Stack())
			}
		}()
		fn()
	}()
}

func (s *Server) agentCleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.cleanupInactiveAgents()
		}
	}
}

func (s *Server) cleanupInactiveAgents() {
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()

	// For now, simple strategy: if we have more than 20 agents, remove half
	// In the future, agent could track lastUsed timestamp
	if len(s.agents) > 20 {
		count := 0
		removeCount := len(s.agents) / 2
		for id := range s.agents {
			if count >= removeCount {
				break
			}
			delete(s.agents, id)
			count++
		}
	}
}

func createProvider(cfg *appconfig.Config) provider.Provider {
	prov, err := appconfig.CreateProvider(cfg)
	if err != nil {
		return nil
	}
	return prov
}

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
- For complex multi-step tasks (3+ steps), ALWAYS use todo tool first:
  1. Create a todo for each step with action="create"
  2. List todos to show the plan with action="list"
  3. Complete each todo as you finish with action="complete"
  4. If user adds new requirements, create additional todos
- Do not call time, system, math, session_search unless explicitly requested
- Respond in the user's language
- Summarize file lists concisely, do not output raw JSON`

	// Inject goal context if session has linked goals
	if s.goalMgr != nil {
		goals, err := s.goalMgr.GetGoalsBySession(context.Background(), sessionID)
		if err == nil && len(goals) > 0 {
			// Find the most recent active goal
			var activeGoal *goal.Goal
			for _, g := range goals {
				if g.Status == goal.StatusActive {
					activeGoal = g
					break
				}
			}
			if activeGoal != nil {
				systemPrompt += fmt.Sprintf(`

CURRENT USER GOAL:
- Title: %s
- Description: %s
- Progress: %d%%
- Status: %s

GOAL GUIDANCE:
- Help the user progress toward this goal
- When completing significant steps, suggest updating the goal progress
- Keep responses focused on advancing the goal
- If the conversation drifts, gently remind the user of their goal`,
					activeGoal.Title,
					activeGoal.Description,
					activeGoal.Progress,
					activeGoal.Status)
			}
		}
	}

	// Build agent options
	var agentOpts []agent.AgentOption
	// Enable memory if config says so OR if cortex is available (cortex provides snapshot memory)
	memoryEnabled := (s.cfg != nil && s.cfg.Memory.Enabled) || s.cortexMgr != nil
	if memoryEnabled {
		agentOpts = append(agentOpts, agent.WithMemory(true))
	}
	// Enable Cortex for memory and context management
	if s.cortexMgr != nil {
		agentOpts = append(agentOpts, agent.WithCortex(s.cortexMgr))
	}
	// Share approval manager with agent so web API can see approval history
	if s.approvalMgr != nil {
		agentOpts = append(agentOpts, agent.WithApprovalManager(s.approvalMgr))
	}

	// Set file conversion config
	convertCfg := &provider.ConvertConfig{
		UploadURLPrefix: "",
		StrategyName:    "auto",
		SupportVision:   false,
	}
	// Get model name to check vision support
	if s.cfg != nil && s.provider != nil {
		if m, ok := s.provider.(interface{ GetModel() string }); ok {
			modelName := m.GetModel()
			convertCfg.SupportVision = provider.ModelSupportsVision(modelName)
		}
		// Set upload URL prefix if configured
		if s.cfg.Server.UploadURLPrefix != "" {
			convertCfg.UploadURLPrefix = s.cfg.Server.UploadURLPrefix
		}
		convertCfg.StrategyName = s.cfg.Server.GetFileStrategy()
	}
	agentOpts = append(agentOpts, agent.WithConvertConfig(convertCfg))

	a := agent.NewEnhancedAgent(s.provider, s.toolReg, toolsSchema, systemPrompt, agentOpts...)

	// Enable web approval mode for server-side agents
	if ah := a.GetApprovalHook(); ah != nil {
		ah.SetWebMode(true)
	}

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

// registerApprovalSSEHandler registers an SSE push callback for the given session
// so that approval_required events are delivered into the chat stream. Returns an
// unregister func that removes the handler (call via defer). Both handleChatStream
// and handleSessionStream use this to ensure approval cards appear in the chat bubble.
func (s *Server) registerApprovalSSEHandler(sessionID string, writeSSE func(string) bool) func() {
	if s.approvalMgr == nil {
		return func() {}
	}
	s.pendingSSEHandlersMu.Lock()
	s.pendingSSEHandlers[sessionID] = func(info approval.PendingApprovalInfo) {
		riskLevel := ""
		if info.RiskLevel != "" {
			riskLevel = info.RiskLevel
		}
		data, _ := json.Marshal(map[string]interface{}{
			"type":       "approval_required",
			"id":         info.ID,
			"command":    info.Command,
			"session_id": info.SessionID,
			"work_dir":   info.WorkingDir,
			"risk_level": riskLevel,
			"reason":     info.Reason,
			"context":    info.Context,
			"created_at": info.CreatedAt.Unix(),
			"expires_at": info.ExpiresAt.Unix(),
		})
		writeSSE("data: " + string(data) + "\n\n")
	}
	s.pendingSSEHandlersMu.Unlock()
	return func() {
		s.pendingSSEHandlersMu.Lock()
		delete(s.pendingSSEHandlers, sessionID)
		s.pendingSSEHandlersMu.Unlock()
	}
}

func getToolsSchema(registry *tool.Registry) []map[string]interface{} {
	tools := []map[string]interface{}{}
	for _, tName := range registry.List() {
		t, err := registry.Get(tName)
		if err != nil {
			continue
		}
		name := t.Name()
		if name == "" {
			continue
		}
		desc := t.Description()
		if desc == "" {
			desc = name + " tool"
		}
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": desc,
				"parameters":  t.Schema(),
			},
		})
	}
	return tools
}

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
			runes := []rune(title)
			if len(runes) > 50 {
				title = string(runes[:50]) + "..."
			}
		}
	}

	preview = strings.TrimSpace(preview)
	runes := []rune(preview)
	if len(runes) > 200 {
		preview = string(runes[:200]) + "..."
	}

	// Use custom name if available, otherwise fallback to auto-generated title
	if s.Name != "" {
		title = s.Name
	} else if title == "" {
		title = "Untitled"
	}

	// Determine if session is active (has activity in last 30 minutes)
	isActive := time.Since(s.UpdatedAt) < 30*time.Minute

	return &Session{
		ID:             s.ID,
		Profile:        s.Profile,
		Source:         s.Platform,
		Model:          s.Model,
		Title:          title,
		WorkDir:        s.WorkDir,
		WorkDirUserSet: s.WorkDirUserSet,
		StartedAt:      s.CreatedAt.Unix(),
		LastActive:     s.UpdatedAt.Unix(),
		IsActive:       isActive,
		MessageCount:   msgCount,
		ToolCallCount:  toolCallCount,
		InputTokens:    s.InputTokens,
		OutputTokens:   s.OutputTokens,
		Preview:        preview,
	}
}

func convertDBMessagesToAPI(sessionID string, msgs []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, len(msgs))
	for i, m := range msgs {
		msg := map[string]interface{}{
			"id":         fmt.Sprintf("msg_%d", i),
			"role":       m.Role,
			"content":    m.Content,
			"session_id": sessionID,
		}
		// 仅当有真实 Timestamp 时才返回；没有就留空，前端会直接不显示时间
		// （避免用当前时间/估算时间占位造成误导）
		if !m.Timestamp.IsZero() {
			msg["timestamp"] = m.Timestamp.Format(time.RFC3339Nano)
		} else {
			msg["timestamp"] = ""
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

func (s *Server) Start(port int) error {
	mux := http.NewServeMux()

	// CORS middleware wrapper
	withCORS := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && s.isAllowedOrigin(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Magic-Session-Token")
			w.Header().Set("Access-Control-Max-Age", "86400")
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

			if token == "" {
				// 未设置认证 token 时拒绝所有受保护接口，要求先完成初始化设置
				// 仅 /api/auth/setup、/api/auth/login、/api/health 等公开路由不需要认证
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprintf(w, `{"error":"authentication required, please setup via /api/auth/setup first"}`)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte("Bearer "+token)) == 1 {
				h(w, r)
				return
			}

			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Magic-Session-Token")), []byte(token)) == 1 {
				h(w, r)
				return
			}

			if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(token)) == 1 {
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
	mux.HandleFunc("/api/auth/reset", withCORS(requireAuth(s.handleAuthReset)))

	// Base API handler for CORS preflight
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Magic-Session-Token")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		http.NotFound(w, r)
	})

	// Health (public)
	mux.HandleFunc("/api/health", withCORS(s.handleHealth))
	mux.HandleFunc("/api/status", withCORS(s.handleStatus))

	// Prometheus metrics (public, for scraper access)
	mux.HandleFunc("/metrics", withCORS(s.handleMetrics))

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

	// MCP Servers
	mux.HandleFunc("/api/mcp/servers", withCORS(requireAuth(s.handleMCPServers)))
	mux.HandleFunc("/api/mcp/servers/", withCORS(requireAuth(s.handleMCPServerByID)))
	mux.HandleFunc("/api/mcp/health", withCORS(requireAuth(s.handleMCPHealth)))

	// Skills
	mux.HandleFunc("/api/skills", withCORS(requireAuth(s.handleSkills)))
	mux.HandleFunc("/api/skills/upload", withCORS(requireAuth(s.handleSkillUpload)))
	mux.HandleFunc("/api/skills/statistics", withCORS(requireAuth(s.handleSkillsStatistics)))
	mux.HandleFunc("/api/skills/", withCORS(requireAuth(s.handleSkillByID)))
	mux.HandleFunc("/api/dashboard/skills", withCORS(requireAuth(s.handleDashboardSkills)))
	mux.HandleFunc("/api/dashboard/skills/search", withCORS(requireAuth(s.handleSkillsSearch)))
	mux.HandleFunc("/api/skills/hub/search", withCORS(requireAuth(s.handleSkillHubSearch)))
	mux.HandleFunc("/api/skills/hub/install", withCORS(requireAuth(s.handleSkillHubInstall)))
	// Auto-skill lifecycle management (three-state)
	mux.HandleFunc("/api/skills/auto/status", withCORS(requireAuth(s.handleAutoSkillStatus)))
	mux.HandleFunc("/api/skills/auto/stats", withCORS(requireAuth(s.handleAutoSkillStats)))
	mux.HandleFunc("/api/skills/auto/action", withCORS(requireAuth(s.handleAutoSkillAction)))

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
	mux.HandleFunc("/api/model/circuit-reset", withCORS(requireAuth(s.handleCircuitReset)))
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

	mux.HandleFunc("/api/fs/dirs", withCORS(requireAuth(s.handleListDirs)))
	mux.HandleFunc("/api/fs/list", withCORS(requireAuth(s.handleFSList)))
	mux.HandleFunc("/api/fs/read", withCORS(requireAuth(s.handleFSRead)))
	mux.HandleFunc("/api/fs/download", withCORS(requireAuth(s.handleFSDownload)))
	mux.HandleFunc("/api/fs/zip", withCORS(requireAuth(s.handleFSZip)))
	mux.HandleFunc("/api/fs/share", withCORS(requireAuth(s.handleFSShare)))
	mux.HandleFunc("/api/fs/mkdir", withCORS(requireAuth(s.handleFSCreateDir)))
	mux.HandleFunc("/api/fs/delete", withCORS(requireAuth(s.handleFSDelete)))
	mux.HandleFunc("/api/fs/rename", withCORS(requireAuth(s.handleFSRename)))
	mux.HandleFunc("/api/fs/write", withCORS(requireAuth(s.handleFSWrite)))
	// Shared resources are accessed via token in the URL (no auth required,
	// because the token itself is the credential). Bound by TTL.
	mux.HandleFunc("/api/fs/shared/", withCORS(s.handleFSShared))

	// Profiles
	mux.HandleFunc("/api/profiles", withCORS(requireAuth(s.handleProfiles)))
	mux.HandleFunc("/api/profiles/", withCORS(requireAuth(s.handleProfileByName)))

	// System
	mux.HandleFunc("/api/system/info", withCORS(requireAuth(s.handleSystemInfo)))
	mux.HandleFunc("/api/system/stats", withCORS(requireAuth(s.handleSystemStats)))
	mux.HandleFunc("/api/usage/today", withCORS(requireAuth(s.handleUsageToday)))
	mux.HandleFunc("/api/usage/daily", withCORS(requireAuth(s.handleUsageDaily)))
	mux.HandleFunc("/api/usage/weekly", withCORS(requireAuth(s.handleUsageWeekly)))
	mux.HandleFunc("/api/usage/monthly", withCORS(requireAuth(s.handleUsageMonthly)))
	mux.HandleFunc("/api/usage/insights", withCORS(requireAuth(s.handleUsageInsights)))
	mux.HandleFunc("/api/usage/budget", withCORS(requireAuth(s.handleUsageBudget)))
	mux.HandleFunc("/api/usage/sessions", withCORS(requireAuth(s.handleUsageSessions)))
	mux.HandleFunc("/api/usage/top-sessions", withCORS(requireAuth(s.handleUsageTopSessions)))
	mux.HandleFunc("/api/usage/providers", withCORS(requireAuth(s.handleUsageProviders)))
	mux.HandleFunc("/api/usage/hourly", withCORS(requireAuth(s.handleUsageHourly)))
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
	mux.HandleFunc("/api/gateway/start", withCORS(requireAuth(s.handleGatewayStart)))
	mux.HandleFunc("/api/gateway/stop", withCORS(requireAuth(s.handleGatewayStop)))
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
	mux.HandleFunc("/api/kanban/tasks/", withCORS(requireAuth(s.handleKanbanTaskByIDOrSubroute)))

	// GroupChat
	mux.HandleFunc("/api/groupchat/rooms", withCORS(requireAuth(s.handleGroupchatRooms)))
	mux.HandleFunc("/api/groupchat/rooms/", withCORS(requireAuth(s.handleGroupchatRoomSubroutes)))

	// Goals
	mux.HandleFunc("/api/goals", withCORS(requireAuth(s.handleGoals)))
	mux.HandleFunc("/api/goals/current", withCORS(requireAuth(s.handleGoalCurrent)))
	mux.HandleFunc("/api/goals/analyze", withCORS(requireAuth(s.handleGoalAnalyze)))
	mux.HandleFunc("/api/goals/", withCORS(requireAuth(s.handleGoalByID)))
	// Goal sessions - get linked sessions with details
	mux.HandleFunc("/api/goals/sessions/", withCORS(requireAuth(s.handleGoalSessions)))

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

	// Commands
	mux.HandleFunc("/api/commands", withCORS(requireAuth(s.handleCommands)))
	mux.HandleFunc("/api/commands/execute", withCORS(requireAuth(s.handleCommandExecute)))

	// File upload
	mux.HandleFunc("/api/upload", withCORS(requireAuth(s.handleFileUpload)))
	mux.HandleFunc("/api/files", withCORS(requireAuth(s.handleFileList)))
	mux.HandleFunc("/api/files/", withCORS(requireAuth(s.handleFileDelete)))

	// Serve uploaded files (auth required)
	uploadsDir := filepath.Join(s.magicHome, "uploads")
	os.MkdirAll(uploadsDir, 0755)
	mux.Handle("/api/uploads/", withCORS(requireAuth(http.StripPrefix("/api/uploads/", http.FileServer(http.Dir(uploadsDir))).ServeHTTP)))

	// Static files
	mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("[server] Magic Agent Dashboard starting on http://localhost:%d\n", port)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	s.httpServer = srv

	return srv.ListenAndServe()
}

// Stop gracefully shuts down the HTTP server. It is safe to call from
// signal handlers when the server is no longer needed.
func (s *Server) Stop() {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(ctx)
	}
}

// handleMetrics exposes Prometheus-format metrics at /metrics.
// It is intentionally unauthenticated to allow standard Prometheus scraping.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	if s.metricsMgr != nil {
		w.Write([]byte(s.metricsMgr.ExportPrometheus()))
	}
}

func (s *Server) buildToolsets() []map[string]interface{} {
	if s.toolReg == nil {
		return []map[string]interface{}{}
	}

	allTools := s.toolReg.List()
	if len(allTools) == 0 {
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

func loadAutoSkillsIntoManager(mgr *skills.Manager, autoDir string) {
	if mgr == nil {
		return
	}
	entries, err := os.ReadDir(autoDir)
	if err != nil {
		return // directory may not exist yet
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip non-auto entries (e.g. patterns.json, generation.log).
		if !strings.HasPrefix(name, "auto-") {
			continue
		}
		skillDir := filepath.Join(autoDir, name)

		metaPath := filepath.Join(skillDir, "meta.json")
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var rawMeta struct {
			Name         string   `json:"name"`
			Description  string   `json:"description"`
			PatternTools []string `json:"pattern_tools"`
			CreatedAt    string   `json:"created_at"`
			Frequency    int      `json:"frequency"`
		}
		if err := json.Unmarshal(metaData, &rawMeta); err != nil {
			continue
		}

		skillName := rawMeta.Name
		if skillName == "" {
			skillName = name
		}

		// Read SKILL.md content if present.
		var content string
		if mdData, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md")); err == nil {
			content = string(mdData)
		}

		installedAt := time.Now()
		if rawMeta.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, rawMeta.CreatedAt); err == nil {
				installedAt = t
			}
		}

		mgr.RegisterSkill(&skills.Skill{
			SkillMeta: skills.SkillMeta{
				Name:        skillName,
				Description: rawMeta.Description,
				Version:     "1.0.0",
				Author:      "cortex-auto",
				Tags:        []string{"auto-generated"},
				Source:      skills.SkillSourceAuto,
				InstalledAt: installedAt,
			},
			Tools:   rawMeta.PatternTools,
			Content: content,
			Dir:     skillDir,
		})
	}
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

		// 填充自动技能的状态
		status := ""
		if skill.Source == skills.SkillSourceAuto {
			status = string(skill.Status)
			if status == "" {
				status = string(skills.SkillStatusPending)
			}
		}

		result = append(result, Skill{
			ID:          skill.Name,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        tags,
			Enabled:     !isDisabled,
			Source:      string(skill.Source),
			Status:      status,
		})
	}

	// 按名称排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func (s *Server) getUserSkillsDir() string {
	userDir := "skills"
	if s.cfg != nil && s.cfg.Skills.UserDir != "" {
		userDir = s.cfg.Skills.UserDir
	}
	return filepath.Join(s.magicHome, userDir)
}

func (s *Server) getDefaultSkillsDir() string {
	defaultDir := "skills-default"
	if s.cfg != nil && s.cfg.Skills.DefaultDir != "" {
		defaultDir = s.cfg.Skills.DefaultDir
	}
	return filepath.Join(s.magicHome, defaultDir)
}

// getAllowedFSRoots 返回文件分享允许的根目录白名单。
func (s *Server) getAllowedFSRoots() []string {
	roots := []string{s.magicHome}
	if s.cfg != nil && s.cfg.WorkingDir != "" {
		roots = append(roots, s.cfg.WorkingDir)
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	return roots
}

func (s *Server) scanSkillsDir() []Skill {
	result := make([]Skill, 0)

	s.disabledSkillsMu.Lock()
	defer s.disabledSkillsMu.Unlock()

	// Scan default skills directory
	s.scanSkillsDirRecursive(s.getDefaultSkillsDir(), "", "local", &result)

	// Scan user skills directory
	s.scanSkillsDirRecursive(s.getUserSkillsDir(), "", "global", &result)

	// 按名称排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

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

func (s *Server) scanPluginsDir() []map[string]interface{} {
	pluginsDir := filepath.Join(s.magicHome, "plugins")
	plugins := make([]map[string]interface{}, 0)

	// Ensure plugins directory exists
	os.MkdirAll(pluginsDir, 0755)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return plugins
	}

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
	return plugins
}

func (s *Server) findCronJobByID(id string) *cron.Job {
	return s.cronMgr.GetByID(id)
}

func (s *Server) taskToJSON(t *kanban.Task) map[string]interface{} {
	return map[string]interface{}{
		"id":              t.ID,
		"title":           t.Title,
		"description":     t.Body,
		"status":          t.Status,
		"priority":        priorityToString(t.Priority),
		"assignee":        t.Assignee,
		"tags":            t.Skills,
		"created_at":      t.CreatedAt.Unix(),
		"updated_at":      t.UpdatedAt.Unix(),
		"due_date":        t.DueDate,
		"estimated_hours": t.EstimatedHours,
		"goal_id":         t.GoalID,
		"parent_count":    t.ParentCount,
		"child_count":     t.ChildCount,
		"comment_count":   t.CommentCount,
	}
}

func execCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

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
