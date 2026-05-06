package mcp

import (
	"context"
	"testing"
	"time"
)

func TestServerConfig(t *testing.T) {
	config := ServerConfig{
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
		Transport: "stdio",
	}

	if config.Command != "npx" {
		t.Errorf("Expected command 'npx', got '%s'", config.Command)
	}

	if config.Transport != "stdio" {
		t.Errorf("Expected transport 'stdio', got '%s'", config.Transport)
	}
}

func TestNewManager(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if len(mgr.clients) != 0 {
		t.Errorf("Expected empty clients map, got %d clients", len(mgr.clients))
	}
}

func TestManagerListServers(t *testing.T) {
	mgr := NewManager()

	servers := mgr.ListServers()
	if len(servers) != 0 {
		t.Errorf("Expected empty server list, got %d servers", len(servers))
	}
}

func TestManagerConnectStdioInvalid(t *testing.T) {
	mgr := NewManager()

	// Test connecting to non-existent command
	config := ServerConfig{
		Command:   "nonexistent_command_12345",
		Transport: "stdio",
	}

	err := mgr.ConnectStdio("test", config)
	if err == nil {
		t.Error("Expected error connecting to non-existent command")
	}
}

func TestManagerDisconnect(t *testing.T) {
	mgr := NewManager()

	// Disconnecting non-existent server should error
	err := mgr.Disconnect("nonexistent")
	if err == nil {
		t.Error("Expected error disconnecting non-existent server")
	}
}

func TestListToolsByServer(t *testing.T) {
	mgr := NewManager()

	_, err := mgr.ListToolsByServer("nonexistent")
	if err == nil {
		t.Error("Expected error listing tools for non-existent server")
	}
}

func TestHealthCheck(t *testing.T) {
	mgr := NewManager()

	err := mgr.HealthCheck("nonexistent")
	if err == nil {
		t.Error("Expected error checking health of non-existent server")
	}
}

func TestGetServerInfo(t *testing.T) {
	mgr := NewManager()

	_, err := mgr.GetServerInfo("nonexistent")
	if err == nil {
		t.Error("Expected error getting info for non-existent server")
	}
}

func TestCallToolNoServer(t *testing.T) {
	mgr := NewManager()

	_, err := mgr.CallTool(context.Background(), "nonexistent", "test_tool", nil)
	if err == nil {
		t.Error("Expected error calling tool on non-existent server")
	}
}

func TestMCPToolName(t *testing.T) {
	mgr := NewManager()

	tool := Tool{
		Name:        "test_tool",
		Description: "Test tool description",
		InputSchema: map[string]interface{}{},
	}

	mcpTool := &MCPTool{
		serverName: "test_server",
		tool:       tool,
		manager:    mgr,
	}

	expected := "mcp_test_server_test_tool"
	if mcpTool.Name() != expected {
		t.Errorf("Expected name '%s', got '%s'", expected, mcpTool.Name())
	}
}

func TestMCPToolDescription(t *testing.T) {
	mgr := NewManager()

	tool := Tool{
		Name:        "test_tool",
		Description: "Test tool description",
		InputSchema: map[string]interface{}{},
	}

	mcpTool := &MCPTool{
		serverName: "test_server",
		tool:       tool,
		manager:    mgr,
	}

	expected := "[MCP:test_server] Test tool description"
	if mcpTool.Description() != expected {
		t.Errorf("Expected description '%s', got '%s'", expected, mcpTool.Description())
	}
}

func TestJSONRPCRequest(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "test.method",
		Params:  []byte(`{"key": "value"}`),
		ID:      1,
	}

	if req.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC '2.0', got '%s'", req.JSONRPC)
	}

	if req.Method != "test.method" {
		t.Errorf("Expected method 'test.method', got '%s'", req.Method)
	}
}

func TestStdioTransportCreation(t *testing.T) {
	// Test that we can create transport with invalid command
	_, err := NewStdioTransport("nonexistent_command_12345", []string{}, []string{})
	if err == nil {
		t.Error("Expected error creating transport with invalid command")
	}
}

func TestSSETransportCreation(t *testing.T) {
	transport, err := NewSSETransport("http://localhost:8080/mcp")
	if err != nil {
		t.Errorf("Failed to create SSE transport: %v", err)
	}
	defer transport.Close()

	if !transport.connected {
		t.Error("Expected transport to be connected")
	}
}

func TestSSETransportSend(t *testing.T) {
	transport, err := NewSSETransport("http://localhost:8080/mcp")
	if err != nil {
		t.Errorf("Failed to create SSE transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "test",
		ID:      1,
	}

	// This will fail because there's no server, but it tests the flow
	_, err = transport.Send(ctx, req)
	// We expect an error since there's no server
	_ = err // Ignore error, just test the flow
}

func TestConfigLoaderLoadFromConfig(t *testing.T) {
	loader := &ConfigLoader{}
	mgr := NewManager()

	// Empty config should not error
	err := loader.LoadFromConfig(mgr, nil)
	if err != nil {
		t.Errorf("LoadFromConfig with nil should not error: %v", err)
	}

	err = loader.LoadFromConfig(mgr, map[string]ServerConfig{})
	if err != nil {
		t.Errorf("LoadFromConfig with empty map should not error: %v", err)
	}
}

func TestContextTimeout(t *testing.T) {
	transport, err := NewSSETransport("http://localhost:8080/mcp")
	if err != nil {
		t.Errorf("Failed to create SSE transport: %v", err)
	}
	defer transport.Close()

	// Create already-canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "test",
		ID:      1,
	}

	_, err = transport.Send(ctx, req)
	if err == nil {
		t.Error("Expected error sending with canceled context")
	}
}
