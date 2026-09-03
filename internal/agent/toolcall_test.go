package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Mock provider that simulates tool calls
type mockToolProvider struct {
	responses []*provider.ChatResponse
	callCount int
}

func (m *mockToolProvider) Name() string { return "mock" }

func (m *mockToolProvider) Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	if m.callCount >= len(m.responses) {
		return &provider.ChatResponse{Content: "done"}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockToolProvider) ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error) {
	return m.Chat(ctx, messages)
}

// Mock tool registry
type mockRegistry struct {
	tools map[string]func(map[string]interface{}) (string, error)
}

func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if fn, ok := m.tools[name]; ok {
		return fn(args)
	}
	return nil, fmt.Errorf("tool not found: %s", name)
}

func TestAgent_ToolCallIDSync(t *testing.T) {
	// Simulate a provider that returns tool calls with empty IDs
	// This tests that the agent correctly generates IDs and syncs them

	mockProvider := &mockToolProvider{
		responses: []*provider.ChatResponse{
			{
				Content: "",
				ToolCalls: []types.ToolCall{
					{
						ID:   "", // Empty ID - this is what causes the bug
						Type: "function",
						Function: types.Function{
							Name:      "test_tool",
							Arguments: `{"query":"test"}`,
						},
					},
				},
			},
			{
				Content: "Final response after tool execution",
			},
		},
	}

	// Create mock registry
	registry := &mockRegistry{
		tools: map[string]func(map[string]interface{}) (string, error){
			"test_tool": func(args map[string]interface{}) (string, error) {
				return "tool result", nil
			},
		},
	}

	// Create agent
	ag := NewAIAgent(mockProvider, registry, nil, "You are a helpful assistant.")

	// Run conversation
	ctx := context.Background()
	response, err := ag.RunConversation(ctx, "test input")
	if err != nil {
		t.Fatalf("RunConversation failed: %v", err)
	}

	t.Logf("Response: %s", response)

	// Check history for correct ID matching
	history := ag.GetHistory()

	var assistantToolCallIDs []string
	var toolMessageIDs []string

	for _, msg := range history {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				assistantToolCallIDs = append(assistantToolCallIDs, tc.ID)
				t.Logf("Assistant tool_call ID: %s", tc.ID)
			}
		}
		if msg.Role == "tool" {
			toolMessageIDs = append(toolMessageIDs, msg.ToolCallID)
			t.Logf("Tool message tool_call_id: %s", msg.ToolCallID)
		}
	}

	// Verify IDs match
	if len(assistantToolCallIDs) != len(toolMessageIDs) {
		t.Fatalf("ID count mismatch: assistant has %d, tool has %d", len(assistantToolCallIDs), len(toolMessageIDs))
	}

	for i := range assistantToolCallIDs {
		if assistantToolCallIDs[i] != toolMessageIDs[i] {
			t.Errorf("ID mismatch at %d: assistant=%s, tool=%s", i, assistantToolCallIDs[i], toolMessageIDs[i])
		}
		if assistantToolCallIDs[i] == "" {
			t.Errorf("ID at %d is empty!", i)
		}
	}

	t.Logf("PASS: All IDs match and are non-empty")
}

func TestAgent_MultipleToolCallIDs(t *testing.T) {
	// Test multiple tool calls with empty IDs
	mockProvider := &mockToolProvider{
		responses: []*provider.ChatResponse{
			{
				Content: "",
				ToolCalls: []types.ToolCall{
					{ID: "", Type: "function", Function: types.Function{Name: "tool1", Arguments: `{}`}},
					{ID: "", Type: "function", Function: types.Function{Name: "tool2", Arguments: `{}`}},
				},
			},
			{
				Content: "Done",
			},
		},
	}

	registry := &mockRegistry{
		tools: map[string]func(map[string]interface{}) (string, error){
			"tool1": func(args map[string]interface{}) (string, error) {
				return "result1", nil
			},
			"tool2": func(args map[string]interface{}) (string, error) {
				return "result2", nil
			},
		},
	}

	ag := NewAIAgent(mockProvider, registry, nil, "")

	ctx := context.Background()
	_, err := ag.RunConversation(ctx, "test")
	if err != nil {
		t.Fatalf("RunConversation failed: %v", err)
	}

	history := ag.GetHistory()

	// Find assistant message with tool_calls
	for _, msg := range history {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for i, tc := range msg.ToolCalls {
				t.Logf("Tool call %d: ID=%s", i, tc.ID)
				if tc.ID == "" {
					t.Errorf("Tool call %d has empty ID!", i)
				}
			}
		}
		if msg.Role == "tool" {
			t.Logf("Tool message: ToolCallID=%s", msg.ToolCallID)
			if msg.ToolCallID == "" {
				t.Errorf("Tool message has empty ToolCallID!")
			}
		}
	}

	t.Logf("PASS: Multiple tool calls handled correctly")
}
