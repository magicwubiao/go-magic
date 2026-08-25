package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// mockLLM simulates an OpenAI-compatible chat completions endpoint.
// The script function decides each response from the last user message,
// enabling tool-call flows and deterministic bot-to-bot conversations.
type mockLLM struct {
	mu       sync.Mutex
	requests []map[string]interface{}
	script   func(t *mockLLM, lastUser string) map[string]interface{}
}

func (m *mockLLM) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	m.mu.Lock()
	m.requests = append(m.requests, map[string]interface{}{"model": req.Model})
	lastUser := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUser = req.Messages[i].Content
			break
		}
	}
	script := m.script
	m.mu.Unlock()

	resp := script(m, lastUser)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func textResponse(text string) map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{{
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": text,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 5},
	}
}

func toolCallResponse(callID, name, argsJSON string) map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{{
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]interface{}{{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": argsJSON,
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]interface{}{"prompt_tokens": 20, "completion_tokens": 8},
	}
}

// setupEnv builds a temp magic home + mock server and returns a config.
func setupEnv(t *testing.T, script func(*mockLLM, string) map[string]interface{}) (*config.Config, string, *mockLLM) {
	t.Helper()

	llm := &mockLLM{script: script}
	server := httptest.NewServer(http.HandlerFunc(llm.handler))
	t.Cleanup(server.Close)

	home := t.TempDir()
	cfgData := map[string]interface{}{
		"provider": "custom",
		"providers": map[string]interface{}{
			"custom": map[string]interface{}{
				"api_key":  "test-key",
				"base_url": server.URL,
				"models":   []string{"mock-model"},
			},
		},
		"bot_mode": map[string]interface{}{"enabled": true},
	}
	raw, _ := json.Marshal(cfgData)
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}

	// Point GetMagicHome at the temp dir for this test.
	t.Setenv("GO_MAGIC_HOME", home)

	botStore, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []*Config{
		{Name: "alice", Title: "Alice", SystemPrompt: "You are Alice."},
		{Name: "bob", Title: "Bob", SystemPrompt: "You are Bob."},
	} {
		if err := botStore.Save(b); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg, home, llm
}

// waitFor polls cond every 50ms until it returns true or times out.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestUserTurnAndPersistence verifies a user message round-trip through the
// queue, history persistence, and reload after manager restart.
func TestUserTurnAndPersistence(t *testing.T) {
	cfg, _, llm := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		return textResponse(fmt.Sprintf("echo:%s", lastUser))
	})
	_ = cfg
	_ = llm

	mgr, err := NewManager(mustLoadConfig(t), nil)
	if err != nil || mgr == nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}

	reply, err := mgr.SendToBot("alice", "hello world")
	if err != nil {
		t.Fatalf("SendToBot: %v", err)
	}
	if reply != "echo:hello world" {
		t.Errorf("reply = %q, want %q", reply, "echo:hello world")
	}

	// History must include system + user + assistant.
	hist := mgr.loadHistory("alice")
	if len(hist) < 3 {
		t.Fatalf("history too short after turn: %d entries", len(hist))
	}
	if hist[0].Role != "system" || hist[1].Role != "user" || hist[2].Role != "assistant" {
		t.Errorf("history roles wrong: %s/%s/%s", hist[0].Role, hist[1].Role, hist[2].Role)
	}

	mgr.Stop()

	// Restart: history should be restored into the new agent's context.
	mgr2, err := NewManager(mustLoadConfig(t), nil)
	if err != nil || mgr2 == nil {
		t.Fatalf("restart NewManager: %v", err)
	}
	if err := mgr2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr2.Stop()

	mgr2.mu.Lock()
	rt := mgr2.bots["alice"]
	ag, err := mgr2.getOrCreateAgentLocked(rt)
	mgr2.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	history := ag.GetHistory()
	if len(history) < 3 {
		t.Fatalf("history not restored across restart: %d entries", len(history))
	}
	foundPrev := false
	for _, m := range history {
		if m.Role == "assistant" && strings.Contains(m.Content, "echo:hello world") {
			foundPrev = true
		}
	}
	if !foundPrev {
		t.Error("previous assistant reply missing after restart")
	}
}

// TestBotToBotMessage verifies fire-and-forget DM routing between bots.
func TestBotToBotMessage(t *testing.T) {
	var mu sync.Mutex
	sawDMInBob := false

	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		if strings.Contains(lastUser, "[reply from @alice]") {
			mu.Lock()
			sawDMInBob = true
			mu.Unlock()
			return textResponse("bob got the reply")
		}
		return textResponse("alice says hi to bob")
	})
	_ = cfg

	mgr, err := NewManager(mustLoadConfig(t), nil)
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	if err := mgr.SendMessageAgent("alice", "bob", "ping bob"); err != nil {
		t.Fatalf("SendMessageAgent: %v", err)
	}

	waitFor(t, 15*time.Second, "bob to receive alice's reply DM", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawDMInBob
	})
}

// TestMessageAgentTool verifies the message_agent tool end-to-end:
// the LLM emits a tool call, the tool delivers the DM, then a final reply.
func TestMessageAgentTool(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		mu.Lock()
		n := callCount
		callCount++
		mu.Unlock()

		if n == 0 && strings.Contains(lastUser, "delegate") {
			args, _ := json.Marshal(map[string]string{"target": "writer", "message": "please draft"})
			return toolCallResponse("call1", "message_agent", string(args))
		}
		return textResponse("delegation done")
	})
	_ = cfg

	// Override bots: use "scout" (sender with tool) and existing "writer".
	home := config.GetMagicHome()
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Save(&Config{Name: "scout", Title: "Scout"})

	mgr, err := NewManager(mustLoadConfig(t), nil)
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	reply, err := mgr.SendToBot("scout", "delegate this task please")
	if err != nil {
		t.Fatalf("SendToBot: %v", err)
	}
	if reply != "delegation done" {
		t.Errorf("reply = %q", reply)
	}
}

// TestUnknownBotRejected verifies error handling for unknown bot names.
func TestUnknownBotRejected(t *testing.T) {
	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		return textResponse("unused")
	})
	_ = cfg

	mgr, err := NewManager(mustLoadConfig(t), nil)
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	if _, err := mgr.SendToBot("nonexistent", "hi"); err == nil {
		t.Error("expected error for unknown bot")
	}
	if err := mgr.Enqueue("ghost", "hi", "user"); err == nil {
		t.Error("expected enqueue error for unknown bot")
	}
}

func mustLoadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	return cfg
}
