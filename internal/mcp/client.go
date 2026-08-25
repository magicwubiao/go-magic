package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// Protocol constants
const (
	JSONRPCVersion        = "2.0"
	ProtocolVersion       = "2026-07-28" // MCP 2.0 stable (released 2026-07-28)
	ProtocolName          = "modelcontextprotocol"
	DefaultTimeout        = 30 * time.Second
	DefaultReconnectDelay = 5 * time.Second
	MaxReconnectAttempts  = 3
	ClientName            = "go-magic"
	ClientVersion         = "0.5.8"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// MCPMessage represents an MCP protocol message
type MCPMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Tool represents an MCP tool (2025-03-26 protocol compatible)
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Annotations *ToolAnnotations       `json:"annotations,omitempty"` // 2025-03-26
}

// ToolAnnotations holds metadata hints for clients (2025-03-26)
type ToolAnnotations struct {
	Title               string   `json:"title,omitempty"`
	ReadOnlyHint        bool     `json:"readOnlyHint,omitempty"`
	DestructiveHint     bool     `json:"destructiveHint,omitempty"`
	IdempotentHint      bool     `json:"idempotentHint,omitempty"`
	OpenWorldHint       bool     `json:"openWorldHint,omitempty"`
	ReasoningEffortHint string   `json:"reasoningEffortHint,omitempty"` // low | medium | high
	Tags                []string `json:"tags,omitempty"`
	Privacy             []string `json:"privacy,omitempty"`
}

// ServerConfig represents MCP server configuration
type ServerConfig struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Env       []string `json:"env,omitempty"`
	Transport string   `json:"transport"`     // "stdio" or "sse"
	URL       string   `json:"url,omitempty"` // for SSE transport
}

// Client represents an MCP client
type Client struct {
	name            string
	config          ServerConfig
	transport       Transport
	tools           map[string]Tool
	mu              sync.RWMutex
	connected       bool
	lastHealthCheck time.Time
	timeout         time.Duration
	reconnect       bool
	reconnectDelay  time.Duration
}

// Transport interface for MCP transport layers
type Transport interface {
	Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error)
	Close() error
}

// Manager manages multiple MCP server connections
type Manager struct {
	mu             sync.RWMutex
	clients        map[string]*Client
	autoReconnect  bool
	reconnectDelay time.Duration
}

// NewManager creates a new MCP manager
func NewManager() *Manager {
	return &Manager{
		clients:        make(map[string]*Client),
		autoReconnect:  true,
		reconnectDelay: DefaultReconnectDelay,
	}
}

// SetAutoReconnect enables or disables automatic reconnection
func (m *Manager) SetAutoReconnect(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoReconnect = enabled
}

// ConnectStdio connects to an MCP server using stdio transport
func (m *Manager) ConnectStdio(name string, config ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("MCP server '%s' already connected", name)
	}

	client := &Client{
		name:           name,
		config:         config,
		tools:          make(map[string]Tool),
		timeout:        DefaultTimeout,
		reconnect:      true,
		reconnectDelay: DefaultReconnectDelay,
	}

	transport, err := NewStdioTransport(config.Command, config.Args, config.Env)
	if err != nil {
		return fmt.Errorf("failed to create stdio transport: %w", err)
	}

	client.transport = transport
	client.connected = true

	// Initialize and discover tools
	if err := client.initialize(); err != nil {
		transport.Close()
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	m.clients[name] = client
	return nil
}

// ConnectSSE connects to an MCP server using SSE transport
func (m *Manager) ConnectSSE(name string, config ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("MCP server '%s' already connected", name)
	}

	client := &Client{
		name:           name,
		config:         config,
		tools:          make(map[string]Tool),
		timeout:        DefaultTimeout,
		reconnect:      true,
		reconnectDelay: DefaultReconnectDelay,
	}

	transport, err := NewSSETransport(config.URL)
	if err != nil {
		return fmt.Errorf("failed to create SSE transport: %w", err)
	}

	client.transport = transport
	client.connected = true

	// Initialize and discover tools
	if err := client.initialize(); err != nil {
		transport.Close()
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	m.clients[name] = client
	return nil
}

// Disconnect disconnects an MCP server
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[name]
	if !exists {
		return fmt.Errorf("MCP server '%s' not found", name)
	}

	if err := client.transport.Close(); err != nil {
		return fmt.Errorf("failed to close transport: %w", err)
	}

	delete(m.clients, name)
	return nil
}

// ListServers lists all connected MCP servers
func (m *Manager) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// ListTools lists all available tools from all connected servers
func (m *Manager) ListTools() []Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allTools []Tool
	for _, client := range m.clients {
		client.mu.RLock()
		for _, tool := range client.tools {
			allTools = append(allTools, tool)
		}
		client.mu.RUnlock()
	}
	return allTools
}

// ListToolsByServer lists tools available from a specific server
func (m *Manager) ListToolsByServer(name string) ([]Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[name]
	if !exists {
		return nil, fmt.Errorf("MCP server '%s' not found", name)
	}

	client.mu.RLock()
	tools := make([]Tool, 0, len(client.tools))
	for _, tool := range client.tools {
		tools = append(tools, tool)
	}
	client.mu.RUnlock()

	return tools, nil
}

// CallTool calls an MCP tool
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (interface{}, error) {
	m.mu.RLock()
	client, exists := m.clients[serverName]
	if !exists {
		return nil, fmt.Errorf("MCP server '%s' not found", serverName)
	}
	m.mu.RUnlock()

	return client.callTool(ctx, toolName, arguments)
}

// initialize initializes the MCP server and discovers tools (MCP 2.0 / 2026-07-28 protocol)
func (c *Client) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Send initialize request (MCP 2.0 includes protocolName + extended capabilities)
	initReq := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "initialize",
		Params: jsonMarshal(map[string]interface{}{
			"protocolName":    ProtocolName, // MCP 2.0 required field
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
					"batch":       false, // MCP 2.0 batch tool call support (client opt-in)
				},
				"logging": map[string]interface{}{},
				"prompts": map[string]interface{}{
					"listChanged": false,
				},
				"resources": map[string]interface{}{
					"listChanged": false,
					"subscribe":   false,
				},
				// MCP 2.0: progress notification support
				"progress": map[string]interface{}{},
				// MCP 2.0: security/privacy labels
				"security": map[string]interface{}{
					"dataClassification": []string{"public", "internal", "restricted"},
				},
			},
			"clientInfo": map[string]interface{}{
				"name":    ClientName,
				"version": ClientVersion,
			},
		}),
		ID: 1,
	}

	resp, err := c.transport.Send(ctx, initReq)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Validate protocol version (negotiate: accept 2024-11-05 or newer)
	var initResult struct {
		ProtocolName    string `json:"protocolName"` // MCP 2.0
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		// Not fatal; some older servers may return minimal payloads
		log.Warnf("[MCP] Failed to parse initialize result: %v", err)
	} else if initResult.ProtocolVersion != "" {
		if initResult.ProtocolName != "" && initResult.ProtocolName != ProtocolName {
			log.Warnf("[MCP] Server '%s' reports protocolName=%q, expected %q",
				c.name, initResult.ProtocolName, ProtocolName)
		}
		switch {
		case initResult.ProtocolVersion < "2024-11-05":
			log.Warnf("[MCP] Server '%s' reports protocol %s (older than 2024-11-05); features may be limited",
				c.name, initResult.ProtocolVersion)
		case initResult.ProtocolVersion < ProtocolVersion:
			log.Infof("[MCP] Server '%s' using protocol %s (client supports %s)",
				c.name, initResult.ProtocolVersion, ProtocolVersion)
		case initResult.ProtocolVersion > ProtocolVersion:
			log.Warnf("[MCP] Server '%s' protocol %s is newer than client %s; upgrading client is recommended",
				c.name, initResult.ProtocolVersion, ProtocolVersion)
		default:
			log.Infof("[MCP] Server '%s' negotiated MCP 2.0 (%s) successfully", c.name, ProtocolVersion)
		}
	}

	// Send initialized notification
	notif := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	}
	_, _ = c.transport.Send(context.Background(), notif)

	// Discover tools
	return c.discoverTools(ctx)
}

// discoverTools discovers available tools from the MCP server
func (c *Client) discoverTools(ctx context.Context) error {
	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/list",
		ID:      2,
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}

	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("failed to parse tools list: %w", err)
	}

	c.mu.Lock()
	for _, tool := range result.Tools {
		c.tools[tool.Name] = tool
	}
	c.mu.Unlock()

	return nil
}

// callTool calls a tool on the MCP server (2025-03-26 compatible)
func (c *Client) callTool(ctx context.Context, toolName string, arguments map[string]interface{}) (interface{}, error) {
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	}

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/call",
		Params:  jsonMarshal(params),
		ID:      uuid.New().String(),
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tool call error: %s", resp.Error.Message)
	}

	var result struct {
		Content []struct {
			// Text content
			Type string `json:"type"`
			Text string `json:"text,omitempty"`

			// Image content (2025-03-26)
			Data     string `json:"data,omitempty"`     // base64
			MimeType string `json:"mimeType,omitempty"` // e.g. image/png

			// Embedded resource (2025-03-26)
			URI      string `json:"uri,omitempty"`
			BlobData string `json:"blobData,omitempty"`

			// Audio content (2025-03-26)
			// Transcript optional field for audio/image

		} `json:"content"`
		IsError    bool   `json:"isError,omitempty"`    // 2025-03-26
		NextCursor string `json:"nextCursor,omitempty"` // 2025-03-26 paginated response
	}

	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}

	// Server explicitly signaled an error via the 2025-03-26 isError field
	if result.IsError {
		if len(result.Content) > 0 && result.Content[0].Text != "" {
			return nil, fmt.Errorf("tool error: %s", result.Content[0].Text)
		}
		return nil, fmt.Errorf("tool call returned isError=true")
	}

	// Concatenate all text-type contents; non-text (images, resources) are
	// preserved as annotations in the raw result but summarized as text for LLM.
	if len(result.Content) == 0 {
		return "", nil
	}

	var combined strings.Builder
	for i, item := range result.Content {
		if i > 0 {
			combined.WriteString("\n")
		}
		switch item.Type {
		case "", "text":
			combined.WriteString(item.Text)
		case "image":
			desc := fmt.Sprintf("[image %s (%d bytes)]", item.MimeType, base64DecodedLen(item.Data))
			combined.WriteString(desc)
		case "embedded_resource":
			name := item.URI
			if name == "" {
				name = "embedded-resource"
			}
			combined.WriteString(fmt.Sprintf("[resource: %s]", name))
			if item.Text != "" {
				combined.WriteString("\n")
				combined.WriteString(item.Text)
			}
		case "audio":
			combined.WriteString("[audio content]")
		default:
			if item.Text != "" {
				combined.WriteString(item.Text)
			} else {
				combined.WriteString(fmt.Sprintf("[%s content]", item.Type))
			}
		}
	}

	return combined.String(), nil
}

// base64DecodedLen returns the approximate byte size of a base64-encoded
// payload without decoding the full string (cheap for logging).
func base64DecodedLen(s string) int {
	if s == "" {
		return 0
	}
	n := len(s)
	pad := 0
	if n >= 1 && s[n-1] == '=' {
		pad++
	}
	if n >= 2 && s[n-2] == '=' {
		pad++
	}
	return (n*3)/4 - pad
}

// MCPTool wraps an MCP tool for integration with the tool registry
type MCPTool struct {
	serverName string
	tool       Tool
	manager    *Manager
}

// Name returns the tool name
func (t *MCPTool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", t.serverName, t.tool.Name)
}

// Description returns the tool description
func (t *MCPTool) Description() string {
	return fmt.Sprintf("[MCP:%s] %s", t.serverName, t.tool.Description)
}

// Parameters returns the tool parameters
func (t *MCPTool) Parameters() map[string]interface{} {
	return t.tool.InputSchema
}

// Execute executes the MCP tool
func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return t.manager.CallTool(ctx, t.serverName, t.tool.Name, args)
}

// RegisterAsTools registers all MCP tools with the tool registry
func (m *Manager) RegisterAsTools(registry interface {
	Register(interface {
		Name() string
		Description() string
		Parameters() map[string]interface{}
		Execute(context.Context, map[string]interface{}) (interface{}, error)
	})
}) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, client := range m.clients {
		client.mu.RLock()
		for _, tool := range client.tools {
			mcpTool := &MCPTool{
				serverName: name,
				tool:       tool,
				manager:    m,
			}
			registry.Register(mcpTool)
		}
		client.mu.RUnlock()
	}
}

// HealthCheck checks the health of a connected MCP server
func (m *Manager) HealthCheck(name string) error {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	client.mu.RLock()
	connected := client.connected
	client.mu.RUnlock()
	m.mu.RUnlock()

	if !connected {
		return fmt.Errorf("MCP server '%s' is disconnected", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "ping",
		ID:      1,
	}

	resp, err := client.transport.Send(ctx, req)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("ping error: %s", resp.Error.Message)
	}

	// Update last health check time
	client.mu.Lock()
	client.lastHealthCheck = time.Now()
	client.mu.Unlock()

	return nil
}

// Reconnect attempts to reconnect to an MCP server
func (m *Manager) Reconnect(name string) error {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	config := client.config
	client.mu.RUnlock()
	m.mu.RUnlock()

	// Disconnect first
	if err := m.Disconnect(name); err != nil {
		// Ignore disconnect errors
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Reconnect with same config
	client = &Client{
		name:           name,
		config:         config,
		tools:          make(map[string]Tool),
		timeout:        DefaultTimeout,
		reconnect:      true,
		reconnectDelay: DefaultReconnectDelay,
	}

	var transport Transport
	var err error

	switch config.Transport {
	case "sse":
		transport, err = NewSSETransport(config.URL)
	default:
		transport, err = NewStdioTransport(config.Command, config.Args, config.Env)
	}

	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	client.transport = transport
	client.connected = true

	if err := client.initialize(); err != nil {
		transport.Close()
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	m.clients[name] = client
	return nil
}

// RefreshTools rediscovers tools from a connected MCP server
func (m *Manager) RefreshTools(name string) error {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	m.mu.RUnlock()

	if err := client.discoverTools(context.Background()); err != nil {
		return fmt.Errorf("failed to refresh tools: %w", err)
	}

	return nil
}

// GetServerInfo returns information about a connected MCP server
func (m *Manager) GetServerInfo(name string) (map[string]interface{}, error) {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("MCP server '%s' not found", name)
	}
	client.mu.RLock()
	toolCount := len(client.tools)
	lastCheck := client.lastHealthCheck
	client.mu.RUnlock()
	m.mu.RUnlock()

	return map[string]interface{}{
		"name":              name,
		"connected":         client.connected,
		"tool_count":        toolCount,
		"transport":         client.config.Transport,
		"last_health_check": lastCheck,
	}, nil
}

func jsonMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		log.Errorf("[MCP] Failed to marshal JSON: %v", err)
		return nil
	}
	return data
}

// ConfigLoader loads MCP configuration
type ConfigLoader struct{}

// LoadFromConfig loads MCP servers from configuration
func (cl *ConfigLoader) LoadFromConfig(mgr *Manager, servers map[string]ServerConfig) error {
	for name, config := range servers {
		var err error
		switch config.Transport {
		case "sse":
			err = mgr.ConnectSSE(name, config)
		default:
			err = mgr.ConnectStdio(name, config)
		}

		if err != nil {
			return fmt.Errorf("failed to connect to MCP server '%s': %w", name, err)
		}
	}
	return nil
}
