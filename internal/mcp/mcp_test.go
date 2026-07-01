package mcp

import (
	"context"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if mgr.clients == nil {
		t.Fatal("clients map is nil")
	}
	if !mgr.autoReconnect {
		t.Fatal("autoReconnect should be true by default")
	}
}

func TestManagerSetAutoReconnect(t *testing.T) {
	mgr := NewManager()
	mgr.SetAutoReconnect(false)
	if mgr.autoReconnect {
		t.Fatal("autoReconnect should be false")
	}
	mgr.SetAutoReconnect(true)
	if !mgr.autoReconnect {
		t.Fatal("autoReconnect should be true")
	}
}

func TestManagerListServers(t *testing.T) {
	mgr := NewManager()
	servers := mgr.ListServers()
	if len(servers) != 0 {
		t.Fatalf("expected empty servers list, got %d", len(servers))
	}
}

func TestManagerDisconnectNonExistent(t *testing.T) {
	mgr := NewManager()
	err := mgr.Disconnect("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestManagerHealthCheckNonExistent(t *testing.T) {
	mgr := NewManager()
	err := mgr.HealthCheck("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestManagerGetServerInfoNonExistent(t *testing.T) {
	mgr := NewManager()
	info, err := mgr.GetServerInfo("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
	if info != nil {
		t.Fatal("expected nil info")
	}
}

func TestManagerRefreshToolsNonExistent(t *testing.T) {
	mgr := NewManager()
	err := mgr.RefreshTools("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestManagerReconnectNonExistent(t *testing.T) {
	mgr := NewManager()
	err := mgr.Reconnect("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestMCPTool(t *testing.T) {
	tool := Tool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	}

	mgr := NewManager()
	mcpTool := &MCPTool{
		serverName: "test_server",
		tool:       tool,
		manager:    mgr,
	}

	if mcpTool.Name() != "mcp_test_server_test_tool" {
		t.Fatalf("expected tool name 'mcp_test_server_test_tool', got '%s'", mcpTool.Name())
	}

	expectedDesc := "[MCP:test_server] A test tool"
	if mcpTool.Description() != expectedDesc {
		t.Fatalf("expected description '%s', got '%s'", expectedDesc, mcpTool.Description())
	}

	params := mcpTool.Parameters()
	if params["type"] != "object" {
		t.Fatal("expected input schema type 'object'")
	}
}

func TestJSONRPCRequest(t *testing.T) {
	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "initialize",
		ID:      1,
	}

	if req.JSONRPC != "2.0" {
		t.Fatal("expected JSONRPC version 2.0")
	}
}

func TestJSONRPCResponse(t *testing.T) {
	resp := &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      1,
	}

	if resp.JSONRPC != "2.0" {
		t.Fatal("expected JSONRPC version 2.0")
	}
}

func TestJSONRPCError(t *testing.T) {
	err := &JSONRPCError{
		Code:    -32600,
		Message: "Invalid Request",
	}

	if err.Code != -32600 {
		t.Fatal("expected error code -32600")
	}
}

func TestServerConfig(t *testing.T) {
	config := ServerConfig{
		Command:   "node",
		Args:      []string{"server.js"},
		Transport: "stdio",
	}

	if config.Command != "node" {
		t.Fatal("expected command 'node'")
	}
	if len(config.Args) != 1 {
		t.Fatal("expected 1 arg")
	}
	if config.Transport != "stdio" {
		t.Fatal("expected transport 'stdio'")
	}
}

func TestNewSSETransport(t *testing.T) {
	transport, err := NewSSETransport("http://localhost:8080/mcp")
	if err != nil {
		t.Fatalf("failed to create SSE transport: %v", err)
	}
	if transport == nil {
		t.Fatal("transport is nil")
	}
	if transport.url != "http://localhost:8080/mcp" {
		t.Fatal("url mismatch")
	}
	if transport.maxRetries != 3 {
		t.Fatal("expected maxRetries 3")
	}
}

func TestSSETransportClose(t *testing.T) {
	transport, _ := NewSSETransport("http://localhost:8080/mcp")
	err := transport.Close()
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if !transport.closed {
		t.Fatal("transport should be closed")
	}
	// Double close should not error
	err = transport.Close()
	if err != nil {
		t.Fatalf("double close failed: %v", err)
	}
}

func TestSSETransportSetReconnect(t *testing.T) {
	transport, _ := NewSSETransport("http://localhost:8080/mcp")
	transport.SetReconnect(false)
	if transport.reconnect {
		t.Fatal("reconnect should be false")
	}
	transport.SetReconnect(true)
	if !transport.reconnect {
		t.Fatal("reconnect should be true")
	}
}

func TestSSETransportSendClosed(t *testing.T) {
	transport, _ := NewSSETransport("http://localhost:8080/mcp")
	transport.Close()

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "ping",
		ID:      1,
	}

	_, err := transport.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for closed transport")
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultTimeout != 30*time.Second {
		t.Fatal("expected DefaultTimeout 30s")
	}
	if DefaultReconnectDelay != 5*time.Second {
		t.Fatal("expected DefaultReconnectDelay 5s")
	}
	if MaxReconnectAttempts != 3 {
		t.Fatal("expected MaxReconnectAttempts 3")
	}
}

func TestMustMarshal(t *testing.T) {
	data := mustMarshal(map[string]string{"key": "value"})
	if data == nil {
		t.Fatal("mustMarshal returned nil")
	}
}

func TestConfigLoader(t *testing.T) {
	loader := &ConfigLoader{}
	if loader == nil {
		t.Fatal("ConfigLoader is nil")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(10, time.Second)
	if rl == nil {
		t.Fatal("rateLimiter is nil")
	}

	// Should allow first request
	if !rl.Allow() {
		t.Fatal("first request should be allowed")
	}

	// Should allow until tokens exhausted
	for i := 0; i < 10; i++ {
		rl.Allow()
	}

	// Should be exhausted
	if rl.Allow() {
		t.Fatal("should be rate limited after exhausting tokens")
	}
}

func TestNewMCPServer(t *testing.T) {
	server := NewMCPServer(8080, nil)
	if server == nil {
		t.Fatal("MCPServer is nil")
	}
	if server.Port != 8080 {
		t.Fatal("port mismatch")
	}
	if server.Path != "/mcp" {
		t.Fatal("path mismatch")
	}
	if !server.EnableCORS {
		t.Fatal("EnableCORS should be true")
	}
	if server.ReadTimeout != 30*time.Second {
		t.Fatal("ReadTimeout should be 30s")
	}
	if server.rateLimiter == nil {
		t.Fatal("rateLimiter is nil")
	}
}

func TestMCPServerIsShuttingDown(t *testing.T) {
	server := NewMCPServer(8080, nil)
	if server.isShuttingDown() {
		t.Fatal("server should not be shutting down initially")
	}
}

func TestSamplingRequest(t *testing.T) {
	req := &SamplingRequest{
		Messages: []SamplingMessage{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	}

	if len(req.Messages) != 1 {
		t.Fatal("expected 1 message")
	}
	if req.Messages[0].Role != "user" {
		t.Fatal("expected role 'user'")
	}
}

func TestSamplingResponse(t *testing.T) {
	resp := &SamplingResponse{
		Content: "Hello world",
		Stopped: "endTurn",
	}

	if resp.Content != "Hello world" {
		t.Fatal("content mismatch")
	}
}

func TestResource(t *testing.T) {
	resource := Resource{
		URI:         "file:///test.txt",
		Name:        "Test File",
		Description: "A test file",
		MimeType:    "text/plain",
	}

	if resource.URI != "file:///test.txt" {
		t.Fatal("URI mismatch")
	}
}

func TestPrompt(t *testing.T) {
	prompt := Prompt{
		Name:        "test_prompt",
		Description: "A test prompt",
		Arguments: []PromptArgument{
			{Name: "arg1", Required: true},
		},
	}

	if prompt.Name != "test_prompt" {
		t.Fatal("prompt name mismatch")
	}
	if len(prompt.Arguments) != 1 {
		t.Fatal("expected 1 argument")
	}
}
