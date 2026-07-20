package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// MCPServer represents an MCP server that can be connected to by MCP clients
type MCPServer struct {
	// Server configuration
	Port         int
	Path         string
	AuthToken    string
	EnableCORS   bool
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// Handler for MCP requests
	Handler MCPServerHandler

	// Internal state
	server      *http.Server
	upgrader    websocket.Upgrader
	mu          sync.RWMutex
	connected   map[string]*websocket.Conn
	clientMu    sync.RWMutex
	rateLimiter *rateLimiter
	shutdown    int32

	// ctx is cancelled when the server stops, so handlers can short-circuit
	// long-running operations instead of relying on context.Background().
	ctx    context.Context
	cancel context.CancelFunc
}

// rateLimiter implements simple token bucket rate limiting
type rateLimiter struct {
	mu         sync.Mutex
	tokens     int
	maxTokens  int
	refillRate time.Duration
	lastRefill time.Time
}

// newRateLimiter creates a new rate limiter
func newRateLimiter(maxTokens int, refillRate time.Duration) *rateLimiter {
	return &rateLimiter{
		maxTokens:  maxTokens,
		tokens:     maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed
func (r *rateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastRefill)
	if elapsed >= r.refillRate {
		tokensToAdd := int(elapsed / r.refillRate)
		r.tokens = min(r.maxTokens, r.tokens+tokensToAdd)
		r.lastRefill = now
	}

	if r.tokens > 0 {
		r.tokens--
		return true
	}
	return false
}

// MCPServerHandler defines the interface for handling MCP server requests
type MCPServerHandler interface {
	// HandleToolCall handles a tool call request
	HandleToolCall(ctx context.Context, tool string, args map[string]interface{}) (json.RawMessage, error)

	// HandleResourceRead reads a resource
	HandleResourceRead(ctx context.Context, uri string) (json.RawMessage, error)

	// HandlePromptGet retrieves a prompt
	HandlePromptGet(ctx context.Context, name string, args map[string]interface{}) (string, error)

	// ListTools returns available tools
	ListTools() []Tool

	// ListResources returns available resources
	ListResources() []Resource

	// ListPrompts returns available prompts
	ListPrompts() []Prompt

	// HandleSamplingRequest handles LLM sampling requests from MCP clients
	HandleSamplingRequest(ctx context.Context, req *SamplingRequest) (*SamplingResponse, error)
}

// Tool represents an MCP tool definition

// Resource represents an MCP resource
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Prompt represents an MCP prompt
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument represents a prompt argument
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// SamplingRequest represents an MCP sampling request
type SamplingRequest struct {
	Method       string            `json:"method,omitempty"`
	Messages     []SamplingMessage `json:"messages"`
	MaxTokens    int               `json:"maxTokens,omitempty"`
	Temperature  float64           `json:"temperature,omitempty"`
	SystemPrompt string            `json:"systemPrompt,omitempty"`
	Tools        []Tool            `json:"tools,omitempty"`
}

// SamplingMessage represents a message in a sampling request
type SamplingMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SamplingResponse represents a sampling response
type SamplingResponse struct {
	Content string `json:"content"`
	Stopped string `json:"stoppedReason,omitempty"`
}

// NewMCPServer creates a new MCP server
func NewMCPServer(port int, handler MCPServerHandler) *MCPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &MCPServer{
		Port:         port,
		Path:         "/mcp",
		EnableCORS:   true,
		Handler:      handler,
		connected:    make(map[string]*websocket.Conn),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		rateLimiter:  newRateLimiter(100, time.Second), // 100 requests per second
		ctx:          ctx,
		cancel:       cancel,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in development
			},
		},
	}
}

// Start starts the MCP server
func (s *MCPServer) Start() error {
	mux := http.NewServeMux()

	// WebSocket endpoint for MCP communication
	mux.HandleFunc(s.Path, s.handleWebSocket)

	// Health check endpoint
	mux.HandleFunc("/health", s.handleHealth)

	// List tools endpoint (HTTP fallback)
	mux.HandleFunc("/tools", s.handleListTools)

	// Call tool endpoint (HTTP fallback)
	mux.HandleFunc("/tools/call", s.handleCallTool)

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	return s.server.ListenAndServe()
}

// StartTLS starts the MCP server with TLS
func (s *MCPServer) StartTLS(certFile, keyFile string) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.Path, s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/tools", s.handleListTools)
	mux.HandleFunc("/tools/call", s.handleCallTool)

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	return s.server.ListenAndServeTLS(certFile, keyFile)
}

// Stop stops the MCP server gracefully
func (s *MCPServer) Stop() error {
	if !atomic.CompareAndSwapInt32(&s.shutdown, 0, 1) {
		return nil // Already shutting down
	}

	// Cancel the server-level context so any in-flight handler using it
	// can short-circuit instead of relying on context.Background().
	if s.cancel != nil {
		s.cancel()
	}

	// Close all connected WebSocket clients
	s.clientMu.Lock()
	for clientID, conn := range s.connected {
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutdown"))
		conn.Close()
		delete(s.connected, clientID)
	}
	s.clientMu.Unlock()

	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}

// isShuttingDown returns true if the server is shutting down
func (s *MCPServer) isShuttingDown() bool {
	return atomic.LoadInt32(&s.shutdown) == 1
}

// handleWebSocket handles WebSocket connections
func (s *MCPServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check if server is shutting down
	if s.isShuttingDown() {
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	}

	// Apply rate limiting
	if !s.rateLimiter.Allow() {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Check auth token if configured
	if s.AuthToken != "" {
		token := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(token), []byte("Bearer "+s.AuthToken)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Register connection
	clientID := fmt.Sprintf("%p", conn)
	s.clientMu.Lock()
	s.connected[clientID] = conn
	s.clientMu.Unlock()

	defer func() {
		s.clientMu.Lock()
		delete(s.connected, clientID)
		s.clientMu.Unlock()
	}()

	// Set read/write deadlines
	conn.SetReadDeadline(time.Now().Add(s.ReadTimeout))
	conn.SetWriteDeadline(time.Now().Add(s.WriteTimeout))

	// Handle incoming messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// Process JSON-RPC message
		var msg JSONRPCMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			s.sendError(conn, msg.ID, -32700, "Parse error")
			continue
		}

		// Handle the message
		s.handleMessage(conn, &msg)
	}
}

// handleMessage handles an MCP JSON-RPC message
func (s *MCPServer) handleMessage(conn *websocket.Conn, msg *JSONRPCMessage) {
	switch msg.Method {
	case "initialize":
		s.handleInitialize(conn, msg)
	case "tools/list":
		s.handleToolsList(conn, msg)
	case "tools/call":
		s.handleToolsCall(conn, msg)
	case "resources/list":
		s.handleResourcesList(conn, msg)
	case "resources/read":
		s.handleResourcesRead(conn, msg)
	case "prompts/list":
		s.handlePromptsList(conn, msg)
	case "prompts/get":
		s.handlePromptsGet(conn, msg)
	case "sampling/createMessage":
		s.handleSamplingCreateMessage(conn, msg)
	default:
		s.sendError(conn, msg.ID, -32601, fmt.Sprintf("Method not found: %s", msg.Method))
	}
}

// handleInitialize handles the initialize request
func (s *MCPServer) handleInitialize(conn *websocket.Conn, msg *JSONRPCMessage) {
	capabilities := map[string]interface{}{
		"tools": map[string]interface{}{
			"listChanged": true,
		},
		"resources": map[string]interface{}{
			"listChanged": true,
		},
		"prompts": map[string]interface{}{
			"listChanged": true,
		},
		"sampling": map[string]interface{}{},
	}

	protocolVersion := "2024-11-05"
	var params map[string]interface{}
	if msg.Params != nil {
		json.Unmarshal(*msg.Params, &params)
		if v, ok := params["protocolVersion"].(string); ok {
			protocolVersion = v
		}
	}

	result := map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities":    capabilities,
		"serverInfo": map[string]interface{}{
			"name":    "go-magic",
			"version": "0.0.1",
		},
	}

	s.sendResponse(conn, msg.ID, result)
}

// handleToolsList handles the tools/list request
func (s *MCPServer) handleToolsList(conn *websocket.Conn, msg *JSONRPCMessage) {
	if s.Handler == nil {
		s.sendError(conn, msg.ID, -32603, "Handler not configured")
		return
	}

	tools := s.Handler.ListTools()
	s.sendResponse(conn, msg.ID, map[string]interface{}{
		"tools": tools,
	})
}

// handleToolsCall handles the tools/call request
func (s *MCPServer) handleToolsCall(conn *websocket.Conn, msg *JSONRPCMessage) {
	if s.Handler == nil {
		s.sendError(conn, msg.ID, -32603, "Handler not configured")
		return
	}

	var req struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments,omitempty"`
	}
	if msg.Params != nil {
		data, _ := json.Marshal(*msg.Params)
		json.Unmarshal(data, &req)
	}

	if req.Name == "" {
		s.sendError(conn, msg.ID, -32602, "Invalid params: tool name required")
		return
	}

	// Use the server's lifecycle context so in-flight tool calls are
	// cancelled when Stop() is invoked, instead of relying on context.Background().
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := s.Handler.HandleToolCall(ctx, req.Name, req.Arguments)
	if err != nil {
		s.sendError(conn, msg.ID, -32603, err.Error())
		return
	}

	s.sendResponse(conn, msg.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": string(result),
			},
		},
	})
}

// handleResourcesList handles the resources/list request
func (s *MCPServer) handleResourcesList(conn *websocket.Conn, msg *JSONRPCMessage) {
	if s.Handler == nil {
		s.sendResponse(conn, msg.ID, map[string]interface{}{
			"resources": []interface{}{},
		})
		return
	}

	resources := s.Handler.ListResources()
	s.sendResponse(conn, msg.ID, map[string]interface{}{
		"resources": resources,
	})
}

// handleResourcesRead handles the resources/read request
func (s *MCPServer) handleResourcesRead(conn *websocket.Conn, msg *JSONRPCMessage) {
	if s.Handler == nil {
		s.sendError(conn, msg.ID, -32603, "Handler not configured")
		return
	}

	var req struct {
		URI string `json:"uri"`
	}
	if msg.Params != nil {
		data, _ := json.Marshal(*msg.Params)
		json.Unmarshal(data, &req)
	}

	if req.URI == "" {
		s.sendError(conn, msg.ID, -32602, "Invalid params: URI required")
		return
	}

	ctx := context.Background()
	result, err := s.Handler.HandleResourceRead(ctx, req.URI)
	if err != nil {
		s.sendError(conn, msg.ID, -32603, err.Error())
		return
	}

	s.sendResponse(conn, msg.ID, map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      req.URI,
				"mimeType": "application/json",
				"text":     string(result),
			},
		},
	})
}

// handlePromptsList handles the prompts/list request
func (s *MCPServer) handlePromptsList(conn *websocket.Conn, msg *JSONRPCMessage) {
	if s.Handler == nil {
		s.sendResponse(conn, msg.ID, map[string]interface{}{
			"prompts": []interface{}{},
		})
		return
	}

	prompts := s.Handler.ListPrompts()
	s.sendResponse(conn, msg.ID, map[string]interface{}{
		"prompts": prompts,
	})
}

// handlePromptsGet handles the prompts/get request
func (s *MCPServer) handlePromptsGet(conn *websocket.Conn, msg *JSONRPCMessage) {
	if s.Handler == nil {
		s.sendError(conn, msg.ID, -32603, "Handler not configured")
		return
	}

	var req struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments,omitempty"`
	}
	if msg.Params != nil {
		data, _ := json.Marshal(*msg.Params)
		json.Unmarshal(data, &req)
	}

	if req.Name == "" {
		s.sendError(conn, msg.ID, -32602, "Invalid params: prompt name required")
		return
	}

	ctx := context.Background()
	content, err := s.Handler.HandlePromptGet(ctx, req.Name, req.Arguments)
	if err != nil {
		s.sendError(conn, msg.ID, -32603, err.Error())
		return
	}

	s.sendResponse(conn, msg.ID, map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"role":    "assistant",
				"content": content,
			},
		},
	})
}

// handleSamplingCreateMessage handles the sampling/createMessage request
func (s *MCPServer) handleSamplingCreateMessage(conn *websocket.Conn, msg *JSONRPCMessage) {
	if s.Handler == nil {
		s.sendError(conn, msg.ID, -32603, "Handler not configured")
		return
	}

	var req SamplingRequest
	if msg.Params != nil {
		data, _ := json.Marshal(*msg.Params)
		json.Unmarshal(data, &req)
	}

	ctx := context.Background()
	resp, err := s.Handler.HandleSamplingRequest(ctx, &req)
	if err != nil {
		s.sendError(conn, msg.ID, -32603, err.Error())
		return
	}

	s.sendResponse(conn, msg.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": resp.Content,
			},
		},
		"stoppedReason": resp.Stopped,
	})
}

// JSONRPCMessage represents a JSON-RPC message
type JSONRPCMessage struct {
	ID     interface{}      `json:"id"`
	Method string           `json:"method"`
	Params *json.RawMessage `json:"params,omitempty"`
}

// sendResponse sends a JSON-RPC response
func (s *MCPServer) sendResponse(conn *websocket.Conn, id interface{}, result interface{}) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, _ := json.Marshal(response)
	conn.WriteMessage(websocket.TextMessage, data)
}

// sendError sends a JSON-RPC error
func (s *MCPServer) sendError(conn *websocket.Conn, id interface{}, code int, message string) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(response)
	conn.WriteMessage(websocket.TextMessage, data)
}

// handleHealth handles the health check endpoint
func (s *MCPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleListTools handles the tools list HTTP endpoint
func (s *MCPServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	if s.Handler == nil {
		http.Error(w, "Handler not configured", http.StatusInternalServerError)
		return
	}

	tools := s.Handler.ListTools()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": tools,
	})
}

// handleCallTool handles the tool call HTTP endpoint
func (s *MCPServer) handleCallTool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.Handler == nil {
		http.Error(w, "Handler not configured", http.StatusInternalServerError)
		return
	}

	var req struct {
		Tool string                 `json:"tool"`
		Args map[string]interface{} `json:"args,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err != io.EOF {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Use the request context so the call is cancelled when the client disconnects.
	ctx := r.Context()
	result, err := s.Handler.HandleToolCall(ctx, req.Tool, req.Args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"result": json.RawMessage(result),
	})
}
