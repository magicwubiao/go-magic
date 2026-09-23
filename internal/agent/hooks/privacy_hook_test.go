package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// TestPrivacyHookNilConfigStillRedacts: NewPrivacyHook(nil) falls back to
// DefaultConfig — redaction stays ON for callers that never wire a config
// (backward compatibility).
func TestPrivacyHookNilConfigStillRedacts(t *testing.T) {
	h := NewPrivacyHook(nil)
	if !h.redactor.IsEnabled() {
		t.Fatal("nil config must fall back to DefaultConfig (enabled=true)")
	}

	req := &LLMHookRequest{
		Messages: []provider.Message{{Role: "user", Content: "my phone is 13812345678"}},
	}
	out, _, err := h.BeforeLLM(context.Background(), req)
	if err != nil {
		t.Fatalf("BeforeLLM error: %v", err)
	}
	if !strings.Contains(out.Messages[0].Content, "[PHONE]") {
		t.Errorf("expected [PHONE] in content, got %q", out.Messages[0].Content)
	}
}

// TestPrivacyHookRespectsMasterSwitch guards the 2026-09-23 kanban bug:
// with privacy.enabled=false, BeforeLLM and BeforeTool must pass text
// through unchanged — kanban task IDs and workdirs (task_1790159593523920700)
// must never be rewritten to task_[PHONE]23920700.
func TestPrivacyHookRespectsMasterSwitch(t *testing.T) {
	h := NewPrivacyHook(&privacy.Config{
		Enabled:        false,
		RedactPhone:    true,
		RedactEmail:    true,
		RedactIDCard:   true,
		RedactBankCard: true,
		RedactIP:       true,
		RedactAddress:  true,
		CustomPatterns: make(map[string]string),
	})

	llmReq := &LLMHookRequest{
		Messages: []provider.Message{
			{Role: "user", Content: "open D:\\workspace\\kanban\\task_1790159593523920700\\pelican_bike.html, phone 13812345678"},
		},
	}
	out, _, err := h.BeforeLLM(context.Background(), llmReq)
	if err != nil {
		t.Fatalf("BeforeLLM error: %v", err)
	}
	if got := out.Messages[0].Content; got != llmReq.Messages[0].Content {
		t.Errorf("master switch off must not rewrite content:\n in: %q\nout: %q", llmReq.Messages[0].Content, got)
	}

	toolReq := &ToolCallHookRequest{
		ToolName: "kanban_show",
		ToolArgs: map[string]interface{}{
			"id":   "task_1790159593523920700",
			"path": "D:\\workspace\\kanban\\task_1790159593523920700",
		},
	}
	toolOut, _, err := h.BeforeTool(context.Background(), toolReq)
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if got := toolOut.ToolArgs["id"]; got != "task_1790159593523920700" {
		t.Errorf("task id must survive, got %v", got)
	}
}

// TestPrivacyHookEnabledRedactsToolArgs: with the switch ON, tool args that
// are not URL-like still get redacted (phone numbers), while the task ID
// shape (long digit run) survives thanks to the \b boundaries.
func TestPrivacyHookEnabledRedactsToolArgs(t *testing.T) {
	h := NewPrivacyHook(&privacy.Config{
		Enabled:        true,
		RedactPhone:    true,
		RedactEmail:    true,
		RedactIDCard:   true,
		RedactBankCard: true,
		RedactIP:       true,
		RedactAddress:  true,
		CustomPatterns: make(map[string]string),
	})

	toolReq := &ToolCallHookRequest{
		ToolName: "send_sms",
		ToolArgs: map[string]interface{}{
			"note": "to 13812345678 re task_1790159593523920700",
		},
	}
	out, _, err := h.BeforeTool(context.Background(), toolReq)
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	got := out.ToolArgs["note"].(string)
	if !strings.Contains(got, "[PHONE]") {
		t.Errorf("expected [PHONE] in %q", got)
	}
	if !strings.Contains(got, "task_1790159593523920700") {
		t.Errorf("task id must survive redaction, got %q", got)
	}
}
