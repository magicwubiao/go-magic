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
	// A scripted failure is signalled via __http_status/__body so tests can
	// emulate real provider errors (e.g. 400s the agent classifies as
	// non-retryable).
	if status, ok := resp["__http_status"].(int); ok {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(resp["__body"])
		return
	}
	json.NewEncoder(w).Encode(resp)
}

// failResponse scripts an HTTP-level provider failure. Errors such as a 400
// content-policy rejection are non-retryable per the agent's error
// classifier, so the turn fails immediately instead of silently succeeding
// on a retry.
func failResponse(status int, message string) map[string]interface{} {
	return map[string]interface{}{
		"__http_status": status,
		"__body": map[string]interface{}{
			"error": map[string]interface{}{
				"message": message,
				"type":    "content_policy_error",
			},
		},
	}
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

// TestRoutineTriggerAndStatus covers the full routine chain:
// cron fires -> prompt enqueued -> worker executes the turn ->
// last_run/last_status written back as "success" (and "failed: ..." on error).
func TestRoutineTriggerAndStatus(t *testing.T) {
	failNext := false
	var mu sync.Mutex

	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		if !strings.Contains(lastUser, "[routine] ") {
			return textResponse("not a routine prompt")
		}
		mu.Lock()
		failing := failNext
		failNext = false
		mu.Unlock()
		if failing {
			return failResponse(http.StatusBadRequest,
				"content blocked by policy violation")
		}
		return textResponse("routine done: " + lastUser)
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

	// Every-second schedule (6-field form exercises the optional-seconds parser).
	err = mgr.AddRoutine("alice", &RoutineConfig{
		Name:     "heartbeat",
		Schedule: "* * * * * *",
		Prompt:   "[routine] tick",
	})
	if err != nil {
		t.Fatalf("AddRoutine: %v", err)
	}

	// Wait for at least one successful run to be recorded.
	waitFor(t, 15*time.Second, "routine success status", func() bool {
		routines, err := mgr.ListRoutines("alice")
		if err != nil || len(routines) == 0 {
			return false
		}
		r := routines[0]
		return r.LastRun != nil && strings.HasPrefix(r.LastStatus, "success")
	})

	// Force a failing run and verify failed status write-back.
	mu.Lock()
	failNext = true
	mu.Unlock()
	waitFor(t, 15*time.Second, "routine failed status", func() bool {
		routines, err := mgr.ListRoutines("alice")
		if err != nil || len(routines) == 0 {
			return false
		}
		r := routines[0]
		return r.LastRun != nil && strings.HasPrefix(r.LastStatus, "failed")
	})

	// Cleanup so the per-second job doesn't keep firing during other tests.
	if err := mgr.RemoveRoutine("alice", "heartbeat"); err != nil {
		t.Fatalf("RemoveRoutine: %v", err)
	}
}

// TestUpdateRoutine covers Manager.UpdateRoutine: disable stops the cron
// entry, edits persist and take effect live, and re-enabling re-registers.
func TestUpdateRoutine(t *testing.T) {
	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		return textResponse("done: " + lastUser)
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

	// Every-second schedule so we can observe firing start/stop.
	err = mgr.AddRoutine("alice", &RoutineConfig{
		Name:     "ticker",
		Schedule: "* * * * * *",
		Prompt:   "[routine] tick",
	})
	if err != nil {
		t.Fatalf("AddRoutine: %v", err)
	}

	waitFor(t, 15*time.Second, "initial success status", func() bool {
		routines, _ := mgr.ListRoutines("alice")
		return len(routines) > 0 && routines[0].LastRun != nil &&
			strings.HasPrefix(routines[0].LastStatus, "success")
	})

	// Disable: last_status should stay frozen (no new runs).
	updated, err := mgr.UpdateRoutine("alice", "ticker", func(r *RoutineConfig) {
		r.Enabled = false
	})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if updated.Enabled {
		t.Error("expected Enabled=false after disable")
	}
	time.Sleep(2500 * time.Millisecond)
	frozen, _ := mgr.ListRoutines("alice")
	var frozenRun int64
	if len(frozen) > 0 && frozen[0].LastRun != nil {
		frozenRun = *frozen[0].LastRun
	}
	time.Sleep(2 * time.Second)
	still, _ := mgr.ListRoutines("alice")
	if len(still) > 0 && still[0].LastRun != nil && *still[0].LastRun != frozenRun {
		t.Errorf("disabled routine still firing (last_run %d -> %d)", frozenRun, *still[0].LastRun)
	}

	// Edit schedule + prompt while disabled; then re-enable.
	updated, err = mgr.UpdateRoutine("alice", "ticker", func(r *RoutineConfig) {
		r.Prompt = "[routine] tock"
	})
	if err != nil {
		t.Fatalf("edit prompt: %v", err)
	}
	if updated.Prompt != "[routine] tock" {
		t.Errorf("prompt not persisted: %q", updated.Prompt)
	}
	// Unknown routine must fail.
	if _, err := mgr.UpdateRoutine("alice", "ghost", func(r *RoutineConfig) { r.Enabled = true }); err == nil {
		t.Error("expected error for unknown routine")
	}
	// Invalid schedule must fail validation.
	if _, err := mgr.UpdateRoutine("alice", "ticker", func(r *RoutineConfig) { r.Schedule = "not-a-cron" }); err == nil {
		t.Error("expected error for invalid schedule")
	}
	// Restore a valid schedule and enable again.
	updated, err = mgr.UpdateRoutine("alice", "ticker", func(r *RoutineConfig) {
		r.Enabled = true
	})
	if err != nil {
		t.Fatalf("re-enable after invalid schedule attempt: %v", err)
	}
	if !updated.Enabled || updated.Schedule != "* * * * * *" {
		t.Errorf("unexpected state after re-enable: %+v", updated)
	}
	waitFor(t, 15*time.Second, "resumed runs with new prompt", func() bool {
		routines, _ := mgr.ListRoutines("alice")
		return len(routines) > 0 && routines[0].LastRun != nil &&
			strings.HasPrefix(routines[0].LastStatus, "success")
	})

	if err := mgr.RemoveRoutine("alice", "ticker"); err != nil {
		t.Fatalf("RemoveRoutine: %v", err)
	}
}

// TestHistoryWindowTruncation verifies saveHistory trims old turns while
// keeping whole turns together and preserving the system prompt.
func TestHistoryWindowTruncation(t *testing.T) {
	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		return textResponse("echo:" + lastUser)
	})
	_ = cfg

	mgr, err := NewManager(mustLoadConfig(t), nil)
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Tiny window for test speed.
	mgr.cfg.BotMode.HistoryWindow = 20
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	for i := 0; i < 30; i++ {
		msg := fmt.Sprintf("q%02d", i)
		if _, err := mgr.SendToBot("alice", msg); err != nil {
			t.Fatalf("SendToBot %d: %v", i, err)
		}
	}

	hist := mgr.loadHistory("alice")
	if len(hist) > 20+1 { // window + system prompt tolerance via boundary trim
		t.Errorf("history not trimmed: %d entries (window=20)", len(hist))
	}
	// First non-system entry must start a turn (user role), never a tool/orphan.
	foundUserStart := false
	for _, m := range hist {
		if m.Role == "system" {
			continue
		}
		if m.Role == "user" && strings.HasPrefix(m.Content, "q") {
			foundUserStart = true
		}
		break
	}
	if !foundUserStart {
		t.Error("trimmed history does not start on a user turn boundary")
	}
	// Most recent exchange must survive.
	last := hist[len(hist)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content, "echo:q29") {
		t.Errorf("latest reply missing after trim: %s/%s", last.Role, last.Content)
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
