package acp

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestStdioTransport(t *testing.T) {
	// Test that we can create a stdio transport (command that echoes input)
	cmd := exec.Command("cat")

	transport, err := NewStdioTransportFromProcess(cmd)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()

	// Send a request
	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "test",
		Params:  json.RawMessage(`{"key":"value"}`),
		ID:      1,
	}

	resp, err := transport.Send(ctx, req)
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	// cat will echo back what we sent
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("expected jsonrpc version %s, got %s", JSONRPCVersion, resp.JSONRPC)
	}
}

func TestHTTPTransport(t *testing.T) {
	transport := NewHTTPTransport("http://localhost:9999/nonexistent", nil)
	defer transport.Close()

	ctx := context.Background()

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "test",
		ID:      1,
	}

	// This should fail since there's no server
	_, err := transport.Send(ctx, req)
	if err == nil {
		t.Error("expected error for non-existent server")
	}
}

func TestJSONRPCRequestResponse(t *testing.T) {
	// Test creating requests and responses
	req := NewJSONRPCRequest("test", map[string]interface{}{"foo": "bar"}, 1)
	if req.JSONRPC != JSONRPCVersion {
		t.Errorf("expected jsonrpc version %s, got %s", JSONRPCVersion, req.JSONRPC)
	}
	if req.Method != "test" {
		t.Errorf("expected method 'test', got '%s'", req.Method)
	}

	resp := NewJSONRPCResponse(1, map[string]string{"result": "ok"})
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("expected jsonrpc version %s, got %s", JSONRPCVersion, resp.JSONRPC)
	}
	if resp.ID != 1 {
		t.Errorf("expected id 1, got %v", resp.ID)
	}

	errResp := NewJSONRPCError(1, ErrCodeMethodNotFound, "method not found")
	if errResp.Error == nil {
		t.Error("expected error in response")
	}
	if errResp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("expected error code %d, got %d", ErrCodeMethodNotFound, errResp.Error.Code)
	}
}

func TestServerCreation(t *testing.T) {
	info := AgentInfo{
		ID:       "test-agent",
		Name:     "Test Agent",
		Version:  "1.0.0",
		Capabilities: []string{"skill/call", "message/send"},
	}

	server := NewServer("test-agent", info)
	if server == nil {
		t.Fatal("failed to create server")
	}

	if server.agentID != "test-agent" {
		t.Errorf("expected agent id 'test-agent', got '%s'", server.agentID)
	}

	if server.GetAgentInfo().Name != "Test Agent" {
		t.Errorf("expected name 'Test Agent', got '%s'", server.GetAgentInfo().Name)
	}

	if server.IsRunning() {
		t.Error("server should not be running yet")
	}
}

func TestServerRegisterSkill(t *testing.T) {
	server := NewServer("test-agent", AgentInfo{})

	skill := Skill{
		Name:        "test_skill",
		Description: "A test skill",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{"type": "string"},
			},
		},
	}

	var called bool
	handler := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		called = true
		return map[string]string{"result": "ok"}, nil
	}

	server.RegisterSkill(skill, handler)

	skills := server.ListSkills()
	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "test_skill" {
		t.Errorf("expected skill name 'test_skill', got '%s'", skills[0].Name)
	}

	// Test handler
	ctx := context.Background()
	result, err := handler(ctx, nil)
	if err != nil {
		t.Errorf("handler error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
	if result == nil {
		t.Error("handler returned nil result")
	}
}

func TestClientCreation(t *testing.T) {
	// Use a dummy transport that will fail
	transport := NewHTTPTransport("http://localhost:9999", nil)
	client := NewClient("test-client", transport)

	if client.agentID != "test-client" {
		t.Errorf("expected agent id 'test-client', got '%s'", client.agentID)
	}

	if client.IsConnected() {
		t.Error("client should not be connected")
	}
}

func TestManager(t *testing.T) {
	manager := NewManager()
	if manager == nil {
		t.Fatal("failed to create manager")
	}

	// List should be empty initially
	connected := manager.ListConnected()
	if len(connected) != 0 {
		t.Errorf("expected empty list, got %d items", len(connected))
	}

	// Get non-existent client should fail
	_, err := manager.GetClient("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent client")
	}

	// Get non-existent server should fail
	_, err = manager.GetServer("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent server")
	}
}

func TestManagerConnectHTTP(t *testing.T) {
	manager := NewManager()

	// This should fail since there's no server
	err := manager.ConnectHTTP("test", "test-agent", "http://localhost:9999", nil)
	if err == nil {
		t.Error("expected error for non-existent server")
	}
}

func TestAgentInfo(t *testing.T) {
	info := AgentInfo{
		ID:       "agent-1",
		Name:     "Agent One",
		Version:  "1.0.0",
		Capabilities: []string{"skill/call", "message/send"},
		Metadata: map[string]string{
			"env": "test",
		},
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded AgentInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != info.ID {
		t.Errorf("expected id '%s', got '%s'", info.ID, decoded.ID)
	}
	if decoded.Name != info.Name {
		t.Errorf("expected name '%s', got '%s'", info.Name, decoded.Name)
	}
	if len(decoded.Capabilities) != len(info.Capabilities) {
		t.Errorf("expected %d capabilities, got %d", len(info.Capabilities), len(decoded.Capabilities))
	}
}

func TestSkill(t *testing.T) {
	skill := Skill{
		Name:        "calculator",
		Description: "Perform calculations",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "Math expression",
				},
			},
			"required": []string{"expression"},
		},
		Source: "math-agent",
	}

	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Skill
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Name != skill.Name {
		t.Errorf("expected name '%s', got '%s'", skill.Name, decoded.Name)
	}
	if decoded.Source != skill.Source {
		t.Errorf("expected source '%s', got '%s'", skill.Source, decoded.Source)
	}
}

func TestMemoryItem(t *testing.T) {
	item := MemoryItem{
		ID:      "mem-1",
		Content: "Important information",
		Type:    "fact",
		Metadata: map[string]interface{}{
			"importance": "high",
		},
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded MemoryItem
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != item.ID {
		t.Errorf("expected id '%s', got '%s'", item.ID, decoded.ID)
	}
	if decoded.Content != item.Content {
		t.Errorf("expected content '%s', got '%s'", item.Content, decoded.Content)
	}
}

func TestConnectionRequestResponse(t *testing.T) {
	req := ConnectionRequest{
		AgentID: "client-1",
		AgentInfo: AgentInfo{
			ID:      "client-1",
			Name:    "Client Agent",
			Version: "1.0.0",
		},
		Capabilities: []string{"skill/call"},
	}

	resp := ConnectionResponse{
		Success: true,
		AgentInfo: AgentInfo{
			ID:      "server-1",
			Name:    "Server Agent",
			Version: "1.0.0",
		},
		Capabilities: []string{"skill/call", "message/send"},
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var decodedReq ConnectionRequest
	if err := json.Unmarshal(reqData, &decodedReq); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	var decodedResp ConnectionResponse
	if err := json.Unmarshal(respData, &decodedResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !decodedResp.Success {
		t.Error("expected success")
	}
	if decodedResp.AgentInfo.ID != "server-1" {
		t.Errorf("expected server id 'server-1', got '%s'", decodedResp.AgentInfo.ID)
	}
}

func TestConcurrentAccess(t *testing.T) {
	manager := NewManager()

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Try concurrent access
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// This should all fail but not panic
			name := "test-agent"
			manager.ConnectHTTP(name, "agent", "http://localhost:9999", nil)
			manager.GetClient(name)
			manager.ListConnected()
		}(i)
	}

	wg.Wait()
}

func TestToolAdapter(t *testing.T) {
	manager := NewManager()
	adapter := NewToolAdapter(manager)

	ctx := context.Background()

	// Test list agents (should work with no connections)
	result, err := adapter.Execute(ctx, "acp_list_agents", nil)
	if err != nil {
		t.Errorf("acp_list_agents failed: %v", err)
	}
	if result == nil {
		t.Error("expected result")
	}

	// Test get skills (should work with no connections)
	result, err = adapter.Execute(ctx, "acp_get_skills", nil)
	if err != nil {
		t.Errorf("acp_get_skills failed: %v", err)
	}
	if result == nil {
		t.Error("expected result")
	}

	// Test ping all (should work with no connections)
	result, err = adapter.Execute(ctx, "acp_ping", map[string]interface{}{})
	if err != nil {
		t.Errorf("acp_ping failed: %v", err)
	}
	if result == nil {
		t.Error("expected result")
	}

	// Test unknown tool
	_, err = adapter.Execute(ctx, "unknown_tool", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestBuiltinTools(t *testing.T) {
	tools := BuiltinTools()
	if len(tools) == 0 {
		t.Error("expected some built-in tools")
	}

	expectedTools := []string{
		"acp_connect",
		"acp_call_skill",
		"acp_share_memory",
		"acp_list_agents",
		"acp_get_skills",
		"acp_ping",
	}

	for _, name := range expectedTools {
		found := false
		for _, tool := range tools {
			if tool["name"] == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool '%s' not found", name)
		}
	}
}

func TestServerStartStop(t *testing.T) {
	info := AgentInfo{
		ID:      "test-server",
		Name:    "Test Server",
		Version: "1.0.0",
	}

	server := NewServer("test-server", info)

	ctx := context.Background()

	// Start server
	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	if !server.IsRunning() {
		t.Error("server should be running")
	}

	// Start again should fail
	if err := server.Start(ctx); err == nil {
		t.Error("expected error when starting again")
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	if server.IsRunning() {
		t.Error("server should not be running")
	}

	// Stop again should be ok
	if err := server.Stop(); err != nil {
		t.Error("second stop should not error")
	}
}

func TestParseSkillCall(t *testing.T) {
	params := json.RawMessage(`{"agent":"test-agent","skill":"test-skill","params":{"key":"value"}}`)

	agent, skill, p, err := ParseSkillCall(params)
	if err != nil {
		t.Fatalf("parse skill call failed: %v", err)
	}

	if agent != "test-agent" {
		t.Errorf("expected agent 'test-agent', got '%s'", agent)
	}
	if skill != "test-skill" {
		t.Errorf("expected skill 'test-skill', got '%s'", skill)
	}
	if p["key"] != "value" {
		t.Errorf("expected params key='value', got '%v'", p["key"])
	}

	// Test missing agent
	_, _, _, err = ParseSkillCall(json.RawMessage(`{"skill":"test"}`))
	if err == nil {
		t.Error("expected error for missing agent")
	}

	// Test missing skill
	_, _, _, err = ParseSkillCall(json.RawMessage(`{"agent":"test"}`))
	if err == nil {
		t.Error("expected error for missing skill")
	}
}

func TestTimeout(t *testing.T) {
	// Create client with short timeout
	transport := NewHTTPTransport("http://localhost:9999", nil)
	client := NewClientWithTimeout("test", transport, 100*time.Millisecond)

	ctx := context.Background()

	start := time.Now()
	_, err := client.Connect(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error")
	}

	// Should have timed out reasonably quickly
	if elapsed < 100*time.Millisecond {
		t.Error("request completed too quickly, timeout may not be working")
	}
}

func TestGenerateRequestID(t *testing.T) {
	id1 := GenerateRequestID()
	id2 := GenerateRequestID()

	if id1 == "" {
		t.Error("expected non-empty id")
	}
	if id1 == id2 {
		t.Error("expected unique ids")
	}
}
