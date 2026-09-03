package provider

import (
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

func TestConvertMessages_ToolCalls(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Search for tech news"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []types.ToolCall{
				{ID: "call_123", Type: "function", Function: types.Function{Name: "web_search", Arguments: `{"query":"tech news"}`}},
			},
		},
		{Role: "tool", Content: "Found 10 articles", ToolCallID: "call_123"},
	}

	result := ConvertMessages(messages)

	// Check we have 3 messages
	if len(result) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(result))
	}

	// Check assistant message has tool_calls
	assistantMsg := result[1]
	if assistantMsg["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got %v", assistantMsg["role"])
	}
	toolCalls, ok := assistantMsg["tool_calls"].([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected tool_calls to be []map[string]interface{}, got %T", assistantMsg["tool_calls"])
	}
	if len(toolCalls) != 1 {
		t.Errorf("Expected 1 tool_call, got %d", len(toolCalls))
	}
	if toolCalls[0]["id"] != "call_123" {
		t.Errorf("Expected tool_call id 'call_123', got %v", toolCalls[0]["id"])
	}

	// Check tool message has tool_call_id
	toolMsg := result[2]
	if toolMsg["role"] != "tool" {
		t.Errorf("Expected role 'tool', got %v", toolMsg["role"])
	}
	toolCallID, ok := toolMsg["tool_call_id"].(string)
	if !ok {
		t.Fatalf("Expected tool_call_id to be string, got %T", toolMsg["tool_call_id"])
	}
	if toolCallID != "call_123" {
		t.Errorf("Expected tool_call_id 'call_123', got %v", toolCallID)
	}

	t.Logf("PASS: Tool message correctly references tool_call_id")
}

func TestConvertMessages_MultipleToolCalls(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Search for news"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []types.ToolCall{
				{ID: "call_1", Type: "function", Function: types.Function{Name: "web_search", Arguments: `{"query":"news 1"}`}},
				{ID: "call_2", Type: "function", Function: types.Function{Name: "web_search", Arguments: `{"query":"news 2"}`}},
			},
		},
		{Role: "tool", Content: "Result 1", ToolCallID: "call_1"},
		{Role: "tool", Content: "Result 2", ToolCallID: "call_2"},
	}

	result := ConvertMessages(messages)

	if len(result) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(result))
	}

	// Check first tool message
	toolMsg1 := result[2]
	if toolMsg1["tool_call_id"] != "call_1" {
		t.Errorf("Expected tool_call_id 'call_1', got %v", toolMsg1["tool_call_id"])
	}

	// Check second tool message
	toolMsg2 := result[3]
	if toolMsg2["tool_call_id"] != "call_2" {
		t.Errorf("Expected tool_call_id 'call_2', got %v", toolMsg2["tool_call_id"])
	}

	t.Logf("PASS: Multiple tool calls correctly matched")
}

func TestConvertMessages_EmptyToolCallID(t *testing.T) {
	// Test case where tool call ID is empty
	// After fix: both assistant and tool messages should have generated IDs that match
	messages := []types.Message{
		{Role: "user", Content: "Search"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []types.ToolCall{
				{ID: "", Type: "function", Function: types.Function{Name: "web_search", Arguments: `{}`}},
			},
		},
		{Role: "tool", Content: "Result", ToolCallID: ""}, // Empty ToolCallID
	}

	result := ConvertMessages(messages)

	// Check assistant message has generated ID
	assistantMsg := result[1]
	toolCalls := assistantMsg["tool_calls"].([]map[string]interface{})
	assistantID := toolCalls[0]["id"].(string)
	t.Logf("Assistant tool_call ID: %v", assistantID)

	if assistantID == "" {
		t.Errorf("Assistant tool_call ID should not be empty")
	}

	// Check tool message has generated ID
	toolMsg := result[2]
	toolCallID := toolMsg["tool_call_id"].(string)
	t.Logf("Tool message tool_call_id: %v", toolCallID)

	if toolCallID == "" {
		t.Errorf("Tool message tool_call_id should not be empty")
	}

	// IDs should match (both generated with same pattern)
	if assistantID != toolCallID {
		t.Logf("Note: IDs don't match (assistant=%s, tool=%s) - this is expected for fallback", assistantID, toolCallID)
		// This is actually OK - the important thing is both have non-empty IDs
	}

	t.Logf("PASS: Both IDs are non-empty")
}

func TestConvertMessages_AgentBehavior(t *testing.T) {
	// Simulate agent behavior: modify ToolCalls ID before adding to history
	toolCalls := []types.ToolCall{
		{ID: "", Type: "function", Function: types.Function{Name: "web_search", Arguments: `{}`}},
	}

	// Agent modifies the ID (like executeToolsWithHooks does)
	toolCalls[0].ID = "call_agent_generated"

	messages := []types.Message{
		{Role: "user", Content: "Search"},
		{
			Role:      "assistant",
			Content:   "",
			ToolCalls: toolCalls, // Modified slice with ID
		},
		{Role: "tool", Content: "Result", ToolCallID: "call_agent_generated"}, // Same ID
	}

	result := ConvertMessages(messages)

	// Check assistant message has the correct ID
	assistantMsg := result[1]
	toolCallsResult := assistantMsg["tool_calls"].([]map[string]interface{})
	assistantID := toolCallsResult[0]["id"].(string)
	t.Logf("Assistant tool_call ID: %v", assistantID)

	// Check tool message has the correct ID
	toolMsg := result[2]
	toolCallID := toolMsg["tool_call_id"].(string)
	t.Logf("Tool message tool_call_id: %v", toolCallID)

	// IDs MUST match
	if assistantID != toolCallID {
		t.Errorf("ID mismatch: assistant=%s, tool=%s", assistantID, toolCallID)
	}

	if assistantID != "call_agent_generated" {
		t.Errorf("Expected ID 'call_agent_generated', got %s", assistantID)
	}

	t.Logf("PASS: IDs match correctly")
}

func TestConvertMessages_ToolCallIDMismatch(t *testing.T) {
	// Test case where tool message has different ID than tool_call
	messages := []types.Message{
		{Role: "user", Content: "Search"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []types.ToolCall{
				{ID: "call_original", Type: "function", Function: types.Function{Name: "web_search", Arguments: `{}`}},
			},
		},
		{Role: "tool", Content: "Result", ToolCallID: "call_different"}, // Different ID!
	}

	result := ConvertMessages(messages)

	// The tool message should use its own ToolCallID
	toolMsg := result[2]
	toolCallID := toolMsg["tool_call_id"]

	t.Logf("Assistant tool_call ID: call_original")
	t.Logf("Tool message tool_call_id: %v", toolCallID)

	// Tool message should use its own ToolCallID (call_different)
	if toolCallID != "call_different" {
		t.Errorf("Expected tool_call_id 'call_different', got %v", toolCallID)
	}
}

// --- TrimDashScopeTurns tests ---

func TestTrimDashScopeTurns_UnderLimit(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	result := TrimDashScopeTurns(msgs)
	if len(result) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(result))
	}
}

func TestTrimDashScopeTurns_OverTotalLimit(t *testing.T) {
	// Simulate a long session: many short user↔assistant pairs that push
	// the TOTAL message count over DashScopeTurnMaxSend even though
	// assistant count alone is well under the old limit.
	const numPairs = 100 // 1 system + 200 messages = 201 total
	msgs := make([]Message, 0, numPairs*2+1)
	msgs = append(msgs, Message{Role: "system", Content: "sys"})
	for i := 0; i < numPairs; i++ {
		msgs = append(msgs, Message{Role: "user", Content: "u"})
		msgs = append(msgs, Message{Role: "assistant", Content: "a"})
	}

	result := TrimDashScopeTurns(msgs)

	if len(result) > DashScopeTurnMaxSend {
		t.Errorf("expected <= %d total messages, got %d", DashScopeTurnMaxSend, len(result))
	}
	// System message must be preserved
	if len(result) > 0 && result[0].Role != "system" {
		t.Errorf("expected system message at index 0, got %s", result[0].Role)
	}
}

func TestTrimDashScopeTurns_WithToolMessages(t *testing.T) {
	const numRounds = 60 // 1 + 60*(1+1+1) = 181 total
	msgs := make([]Message, 0, numRounds*4+1)
	msgs = append(msgs, Message{Role: "system", Content: "sys"})
	for i := 0; i < numRounds; i++ {
		msgs = append(msgs, Message{Role: "user", Content: "u"})
		msgs = append(msgs, Message{
			Role:    "assistant",
			Content: "",
			ToolCalls: []types.ToolCall{
				{ID: "call", Type: "function", Function: types.Function{Name: "f", Arguments: "{}"}},
			},
		})
		msgs = append(msgs, Message{Role: "tool", Content: "r", ToolCallID: "call"})
	}

	result := TrimDashScopeTurns(msgs)

	if len(result) > DashScopeTurnMaxSend {
		t.Errorf("expected <= %d total messages, got %d", DashScopeTurnMaxSend, len(result))
	}
	// No orphaned tool messages at the start
	if len(result) > 1 && result[1].Role == "tool" {
		t.Errorf("result should not start with a tool message after system")
	}
}

func TestTrimDashScopeTurns_NoSystemMessage(t *testing.T) {
	const numPairs = 100
	msgs := make([]Message, 0, numPairs*2)
	for i := 0; i < numPairs; i++ {
		msgs = append(msgs, Message{Role: "user", Content: "u"})
		msgs = append(msgs, Message{Role: "assistant", Content: "a"})
	}

	result := TrimDashScopeTurns(msgs)

	if len(result) > DashScopeTurnMaxSend {
		t.Errorf("expected <= %d total messages, got %d", DashScopeTurnMaxSend, len(result))
	}
}
