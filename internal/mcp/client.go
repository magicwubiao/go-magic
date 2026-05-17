package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Protocol constants
const (
	JSONRPCVersion = "2.0"
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

// Tool represents an MCP tool
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
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
	name      string
	config    ServerConfig
	transport Transport
	tools     map[string]Tool
	mu        sync.RWMutex
	connected bool
}

// Transport interface for MCP transport layers
type Transport interface {
	Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error)
	Close() error
}

// Manager manages multiple MCP server connections
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewManager creates a new MCP manager
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]*Client),
	}
}

// ConnectStdio connects to an MCP server using stdio transport
func (m *Manager) ConnectStdio(name string, config ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("MCP server '%s' already connected", name)
	}

	client := &Client{
		name:   name,
		config: config,
		tools:  make(map[string]Tool),
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
		name:   name,
		config: config,
		tools:  make(map[string]Tool),
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

// initialize initializes the MCP server and discovers tools
func (c *Client) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Send initialize request
	initReq := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "initialize",
		Params:  mustMarshal(map[string]interface{}{"protocolVersion": "2024-11-05"}),
		ID:      1,
	}

	resp, err := c.transport.Send(ctx, initReq)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Send initialized notification
	notif := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	}

	c.transport.Send(context.Background(), notif)

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

// callTool calls a tool on the MCP server
func (c *Client) callTool(ctx context.Context, toolName string, arguments map[string]interface{}) (interface{}, error) {
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	}

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/call",
		Params:  mustMarshal(params),
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
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}

	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}

	return nil, nil
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
		return fmt.Errorf("MCP server '%s' not found", name)
	}
	m.mu.RUnlock()

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

	return nil
}

// GetServerInfo returns information about a connected MCP server
func (m *Manager) GetServerInfo(name string) (map[string]interface{}, error) {
	m.mu.RLock()
	client, exists := m.clients[name]
	if !exists {
		return nil, fmt.Errorf("MCP server '%s' not found", name)
	}
	client.mu.RLock()
	toolCount := len(client.tools)
	client.mu.RUnlock()
	m.mu.RUnlock()

	return map[string]interface{}{
		"name":       name,
		"connected":  client.connected,
		"tool_count": toolCount,
		"transport":  client.config.Transport,
	}, nil
}

func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
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
