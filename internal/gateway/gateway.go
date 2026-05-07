package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// Message represents an incoming or outgoing message
type Message struct {
	ID        string                 `json:"id"`
	Platform  string                 `json:"platform"`
	ChannelID string                 `json:"channel_id"`
	UserID    string                 `json:"user_id"`
	Content   string                 `json:"content"`
	Role      string                 `json:"role,omitempty"`
	From      string                 `json:"from,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Response represents a response to a message
type Response struct {
	MessageID string                 `json:"message_id"`
	Content  string                 `json:"content"`
	ChannelID string                 `json:"channel_id"`
	Error    error                  `json:"error,omitempty"`
}

// PlatformHandler defines the interface for platform implementations
type PlatformHandler interface {
	// Connect establishes connection to the platform
	Connect(ctx context.Context) error

	// Disconnect closes the connection
	Disconnect() error

	// Send sends a message
	Send(ctx context.Context, resp Response) error

	// Receive returns a channel of incoming messages
	Receive() <-chan Message

	// HandleSlashCommand handles a slash command
	HandleSlashCommand(cmd string, msg Message) (Response, error)

	// Name returns the platform name
	Name() string

	// CheckHealth returns detailed health status
	CheckHealth() *HealthStatus
}

// HealthStatus is redefined later with Platforms field (see line ~682)

// AgentHandler defines the interface for the agent
type AgentHandler interface {
	// Process processes a message and returns the response
	Process(ctx context.Context, msg Message) (string, error)

	// ResetSession resets a user's session
	ResetSession(userID string)
}

// Session represents a user session
type Session struct {
	UserID     string                 `json:"user_id"`
	Platform   string                 `json:"platform"`
	CreatedAt  time.Time              `json:"created_at"`
	LastActive time.Time              `json:"last_active"`
	History    []Message              `json:"history,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	agent      AgentHandler
}

// AgentSession represents a platform-specific agent session
type AgentSession struct {
	UserID    interface{} `json:"user_id"` // Platform-specific user ID (string, int64, etc.)
	SessionID string      `json:"session_id"`
	State     map[string]interface{}
	CreatedAt time.Time
}

// Middleware defines a message processing middleware
type Middleware interface {
	// Process processes a message and returns whether to continue
	Process(msg *Message) (bool, error)
	Name() string
}

// MiddlewareFunc is a function-based middleware
type MiddlewareFunc func(msg *Message) (bool, error)

func (f MiddlewareFunc) Process(msg *Message) (bool, error) { return f(msg) }
func (f MiddlewareFunc) Name() string                      { return "MiddlewareFunc" }

// RateLimitMiddleware limits message rate per user
type RateLimitMiddleware struct {
	mu       sync.Mutex
	limits   map[string][]time.Time
	maxCount int
	window   time.Duration
}

// NewRateLimitMiddleware creates a new rate limit middleware
func NewRateLimitMiddleware(maxCount int, window time.Duration) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		limits:   make(map[string][]time.Time),
		maxCount: maxCount,
		window:   window,
	}
}

func (m *RateLimitMiddleware) Name() string { return "rate_limit" }

func (m *RateLimitMiddleware) Process(msg *Message) (bool, error) {
	key := msg.UserID + ":" + msg.Platform
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	now := time.Now()
	windowStart := now.Add(-m.window)
	
	// Clean old entries
	var valid []time.Time
	for _, t := range m.limits[key] {
		if t.After(windowStart) {
			valid = append(valid, t)
		}
	}
	
	if len(valid) >= m.maxCount {
		m.limits[key] = valid
		return false, fmt.Errorf("rate limit exceeded: max %d messages per %v", m.maxCount, m.window)
	}
	
	m.limits[key] = append(valid, now)
	return true, nil
}

// BlacklistMiddleware blocks users in a blacklist
type BlacklistMiddleware struct {
	blacklist map[string]bool
	mu        sync.RWMutex
}

// NewBlacklistMiddleware creates a new blacklist middleware
func NewBlacklistMiddleware() *BlacklistMiddleware {
	return &BlacklistMiddleware{
		blacklist: make(map[string]bool),
	}
}

func (m *BlacklistMiddleware) Name() string { return "blacklist" }

// Add adds a user to the blacklist
func (m *BlacklistMiddleware) Add(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blacklist[userID] = true
}

// Remove removes a user from the blacklist
func (m *BlacklistMiddleware) Remove(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blacklist, userID)
}

// IsBlocked checks if a user is blocked
func (m *BlacklistMiddleware) IsBlocked(userID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.blacklist[userID]
}

func (m *BlacklistMiddleware) Process(msg *Message) (bool, error) {
	if m.IsBlocked(msg.UserID) {
		return false, fmt.Errorf("user %s is blocked", msg.UserID)
	}
	return true, nil
}

// SensitiveWordMiddleware filters sensitive words
type SensitiveWordMiddleware struct {
	words []string
}

// NewSensitiveWordMiddleware creates a new sensitive word middleware
func NewSensitiveWordMiddleware(words []string) *SensitiveWordMiddleware {
	return &SensitiveWordMiddleware{words: words}
}

func (m *SensitiveWordMiddleware) Name() string { return "sensitive_word" }

func (m *SensitiveWordMiddleware) Process(msg *Message) (bool, error) {
	content := msg.Content
	for _, word := range m.words {
		if containsSensitiveWord(content, word) {
			return false, fmt.Errorf("sensitive word detected")
		}
	}
	return true, nil
}

func containsSensitiveWord(content, word string) bool {
	// Simple case-insensitive substring check
	lowerContent := toLower(content)
	lowerWord := toLower(word)
	for i := 0; i <= len(lowerContent)-len(lowerWord); i++ {
		if lowerContent[i:i+len(lowerWord)] == lowerWord {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

// Gateway manages multiple platform connections and routes messages to the agent
type Gateway struct {
	platforms  map[string]PlatformHandler
	sessions   map[string]*Session
	agent      AgentHandler
	mu         sync.RWMutex
	running    bool
	stopCh     chan struct{}
	config     *GatewayConfig
	middleware []Middleware

	// Event handlers
	onMessage    func(Message)
	onSessionEnd func(Session)

	// HTTP API server
	apiServer *http.Server
	apiPort   int
}

// GatewayConfig holds gateway configuration
type GatewayConfig struct {
	MaxSessions     int
	SessionTimeout  time.Duration
	EnableSlashCmd  bool
	PlatformTimeout time.Duration
	APIPort         int
	EnableAPI       bool
}

// DefaultGatewayConfig returns default gateway configuration
func DefaultGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		MaxSessions:     1000,
		SessionTimeout:  24 * time.Hour,
		EnableSlashCmd:  true,
		PlatformTimeout: 30 * time.Second,
		APIPort:         8080,
		EnableAPI:       true,
	}
}

// NewGateway creates a new gateway
func NewGateway(agent AgentHandler, config *GatewayConfig) *Gateway {
	if config == nil {
		config = DefaultGatewayConfig()
	}

	return &Gateway{
		platforms: make(map[string]PlatformHandler),
		sessions:  make(map[string]*Session),
		agent:     agent,
		stopCh:    make(chan struct{}),
		config:    config,
		middleware: make([]Middleware, 0),
		apiPort:   config.APIPort,
	}
}

// RegisterPlatform registers a platform handler
func (g *Gateway) RegisterPlatform(name string, handler PlatformHandler) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.platforms[name] = handler
}

// Use adds a middleware to the gateway
func (g *Gateway) Use(m Middleware) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.middleware = append(g.middleware, m)
}

// Start starts the gateway
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return fmt.Errorf("gateway already running")
	}
	g.running = true
	g.mu.Unlock()

	// Connect all platforms
	for name, handler := range g.platforms {
		if err := handler.Connect(ctx); err != nil {
			log.Errorf("Failed to connect %s: %v", name, err)
		} else {
			log.Infof("Platform %s connected", name)
		}

		// Start receiving messages
		go g.handleMessages(name, handler)
	}

	// Start session cleanup
	go g.cleanupSessions()

	// Start HTTP API server if enabled
	if g.config.EnableAPI {
		go g.startAPIServer()
	}

	log.Info("Gateway started")
	return nil
}

// Stop stops the gateway
func (g *Gateway) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	close(g.stopCh)
	g.running = false

	// Stop API server
	if g.apiServer != nil {
		g.apiServer.Shutdown(context.Background())
	}

	// Disconnect all platforms
	for name, handler := range g.platforms {
		handler.Disconnect()
		log.Infof("Platform %s disconnected", name)
	}

	log.Info("Gateway stopped")
	return nil
}

// handleMessages handles incoming messages from a platform
func (g *Gateway) handleMessages(platform string, handler PlatformHandler) {
	msgs := handler.Receive()

	for {
		select {
		case <-g.stopCh:
			return
		case msg, ok := <-msgs:
			if !ok {
				log.Warnf("Platform %s message channel closed", platform)
				return
			}
			g.processMessage(platform, msg, handler)
		}
	}
}

// processMessage processes a single message
func (g *Gateway) processMessage(platform string, msg Message, handler PlatformHandler) {
	// Apply middleware
	g.mu.RLock()
	middleware := g.middleware
	g.mu.RUnlock()

	for _, m := range middleware {
		ok, err := m.Process(&msg)
		if !ok {
			if err != nil {
				log.Warnf("Middleware %s rejected message: %v", m.Name(), err)
			}
			return
		}
	}

	// Check for slash commands
	if g.config.EnableSlashCmd && len(msg.Content) > 0 && msg.Content[0] == '/' {
		cmd := msg.Content[1:]
		if resp, err := handler.HandleSlashCommand(cmd, msg); err == nil {
			handler.Send(context.Background(), resp)
			return
		}
	}

	// Get or create session
	session := g.getOrCreateSession(msg.UserID, platform)

	// Update session
	session.LastActive = time.Now()
	session.History = append(session.History, msg)

	// Call message handler if set
	if g.onMessage != nil {
		g.onMessage(msg)
	}

	// Process with agent
	resp, err := g.agent.Process(context.Background(), msg)
	if err != nil {
		resp = fmt.Sprintf("Error: %v", err)
	}

	// Send response
	response := Response{
		MessageID: msg.ID,
		Content:   resp,
		ChannelID: msg.ChannelID,
	}
	handler.Send(context.Background(), response)
}

// getOrCreateSession gets or creates a session for a user
func (g *Gateway) getOrCreateSession(userID, platform string) *Session {
	key := fmt.Sprintf("%s:%s", platform, userID)

	g.mu.RLock()
	session, exists := g.sessions[key]
	g.mu.RUnlock()

	if exists {
		return session
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Double-check after acquiring write lock
	if session, exists = g.sessions[key]; exists {
		return session
	}

	// Create new session
	session = &Session{
		UserID:     userID,
		Platform:   platform,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		History:    make([]Message, 0),
		Metadata:   make(map[string]interface{}),
		agent:      g.agent,
	}

	// Check max sessions
	if len(g.sessions) >= g.config.MaxSessions {
		// Remove oldest session
		g.removeOldestSession()
	}

	g.sessions[key] = session
	return session
}

// cleanupSessions removes old sessions periodically
func (g *Gateway) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.cleanupExpiredSessions()
		}
	}
}

// cleanupExpiredSessions removes expired sessions
func (g *Gateway) cleanupExpiredSessions() {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	for key, session := range g.sessions {
		if now.Sub(session.LastActive) > g.config.SessionTimeout {
			if g.onSessionEnd != nil {
				g.onSessionEnd(*session)
			}
			delete(g.sessions, key)
		}
	}
}

// removeOldestSession removes the oldest session
func (g *Gateway) removeOldestSession() {
	var oldest *Session
	var oldestKey string

	for key, session := range g.sessions {
		if oldest == nil || session.CreatedAt.Before(oldest.CreatedAt) {
			oldest = session
			oldestKey = key
		}
	}

	if oldest != nil {
		if g.onSessionEnd != nil {
			g.onSessionEnd(*oldest)
		}
		delete(g.sessions, oldestKey)
	}
}

// GetSession gets a session by user ID and platform
func (g *Gateway) GetSession(userID, platform string) *Session {
	key := fmt.Sprintf("%s:%s", platform, userID)

	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.sessions[key]
}

// ResetSession resets a user's session
func (g *Gateway) ResetSession(userID, platform string) {
	key := fmt.Sprintf("%s:%s", platform, userID)

	g.mu.Lock()
	defer g.mu.Unlock()

	if session, exists := g.sessions[key]; exists {
		if g.onSessionEnd != nil {
			g.onSessionEnd(*session)
		}
		delete(g.sessions, key)
	}

	// Also reset agent session
	g.agent.ResetSession(userID)
}

// ListSessions returns all active sessions
func (g *Gateway) ListSessions() []Session {
	g.mu.RLock()
	defer g.mu.RUnlock()

	sessions := make([]Session, 0, len(g.sessions))
	for _, s := range g.sessions {
		sessions = append(sessions, *s)
	}
	return sessions
}

// Stats returns gateway statistics
type GatewayStats struct {
	Platforms      int      `json:"platforms"`
	ActiveSessions int      `json:"active_sessions"`
	TotalProcessed int      `json:"total_processed"`
	PlatformList   []string `json:"platform_list"`
}

func (g *Gateway) Stats() GatewayStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var processed int
	for _, s := range g.sessions {
		processed += len(s.History)
	}

	platforms := make([]string, 0, len(g.platforms))
	for name := range g.platforms {
		platforms = append(platforms, name)
	}

	return GatewayStats{
		Platforms:      len(g.platforms),
		ActiveSessions: len(g.sessions),
		TotalProcessed: processed,
		PlatformList:   platforms,
	}
}

// HealthCheck performs a health check on the gateway
func (g *Gateway) HealthCheck() *HealthStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()

	status := &HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now(),
		Platforms: make(map[string]PlatformStatus),
	}

	// Check all registered platforms
	for name, handler := range g.platforms {
		platformStatus := PlatformStatus{
			Name:   name,
			Status: "connected",
		}

		// Check if handler supports health check
		if h, ok := handler.(HealthCheckable); ok {
			if err := h.CheckHealth(); err != nil {
				platformStatus.Status = "unhealthy"
				platformStatus.Error = err.Error()
				status.Status = "degraded"
			}
		}

		status.Platforms[name] = platformStatus
	}

	// Check if gateway is running
	if !g.running {
		status.Status = "stopped"
	}

	return status
}

// Restart restarts the gateway
func (g *Gateway) Restart(ctx context.Context) error {
	if err := g.Stop(); err != nil {
		return err
	}
	return g.Start(ctx)
}

// IsRunning returns whether the gateway is running
func (g *Gateway) IsRunning() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}

// SetMessageHandler sets the message handler callback
func (g *Gateway) SetMessageHandler(handler func(Message)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onMessage = handler
}

// SetSessionEndHandler sets the session end handler callback
func (g *Gateway) SetSessionEndHandler(handler func(Session)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onSessionEnd = handler
}

// HealthStatus represents the health status of the gateway
type HealthStatus struct {
	Status    string                    `json:"status"`
	Timestamp time.Time                 `json:"timestamp"`
	Platforms map[string]PlatformStatus `json:"platforms"`
	
	// 兼容旧代码字段
	Platform      string                 `json:"platform,omitempty"`
	Connected    bool                   `json:"connected,omitempty"`
	CallbackOK   bool                   `json:"callback_ok,omitempty"`
	CallbackPort int                    `json:"callback_port,omitempty"`
	HTTPClientOK bool                   `json:"http_client_ok,omitempty"`
	TokenValid   bool                   `json:"token_valid,omitempty"`
	TokenExpiry  *time.Time             `json:"token_expiry,omitempty"`
	LatencyMs     int64                  `json:"latency_ms,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
	Error         string                 `json:"error,omitempty"`
}

// PlatformStatus represents the status of a platform
type PlatformStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// HealthCheckable is an interface for health-checkable handlers
type HealthCheckable interface {
	CheckHealth() error
}

// startAPIServer starts the HTTP API server
func (g *Gateway) startAPIServer() {
	mux := http.NewServeMux()
	
	mux.HandleFunc("/api/status", g.handleStatus)
	mux.HandleFunc("/api/sessions", g.handleSessions)
	mux.HandleFunc("/api/broadcast", g.handleBroadcast)
	mux.HandleFunc("/api/health", g.handleHealth)
	
	g.apiServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.apiPort),
		Handler: mux,
	}
	
	log.Infof("Gateway API server starting on port %d", g.apiPort)
	if err := g.apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("API server error: %v", err)
	}
}

func (g *Gateway) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g.Stats())
}

func (g *Gateway) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	if r.Method == "GET" {
		json.NewEncoder(w).Encode(g.ListSessions())
	} else if r.Method == "DELETE" {
		// Reset specific session
		userID := r.URL.Query().Get("user_id")
		platform := r.URL.Query().Get("platform")
		if userID != "" && platform != "" {
			g.ResetSession(userID, platform)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
		} else {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing user_id or platform"})
		}
	}
}

func (g *Gateway) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}
	
	// Broadcast to all sessions
	g.mu.RLock()
	defer g.mu.RUnlock()
	
	for key, handler := range g.platforms {
		for _, session := range g.sessions {
			resp := Response{
				Content: req.Content,
			}
			handler.Send(context.Background(), resp)
			log.Infof("Broadcast to %s:%s", key, session.UserID)
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "broadcast"})
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g.HealthCheck())
}

// Broadcast sends a message to all connected users
func (g *Gateway) Broadcast(content string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	
	for _, handler := range g.platforms {
		for range g.sessions {
			resp := Response{
				Content: content,
			}
			handler.Send(context.Background(), resp)
		}
	}
}
