package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockClarifyBridge struct {
	ans *ClarifyAnswer
	err error
}

func (m *mockClarifyBridge) Ask(ctx context.Context, sessionID string, req ClarifyRequest) (*ClarifyAnswer, error) {
	return m.ans, m.err
}

// sessionCtx returns a context carrying a session id.
func sessionCtx(sid string) context.Context {
	return WithSessionID(context.Background(), sid)
}

func TestClarifyExecuteNoSessionFallsBack(t *testing.T) {
	defer SetClarifyBridge(nil)
	tool := NewClarifyTool()

	// 无 sessionID：即便 bridge 存在也不能挂起，回落结构化结果。
	SetClarifyBridge(&mockClarifyBridge{ans: &ClarifyAnswer{Choices: []string{"X"}}})
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"question": "A or B?",
		"options":  []interface{}{"A", "B"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cr, ok := res.(*ClarifyResult)
	if !ok {
		t.Fatalf("expected *ClarifyResult, got %T", res)
	}
	if cr.Status != "clarification_needed" || len(cr.Options) != 2 || !cr.RenderAsButtons {
		t.Fatalf("unexpected fallback result: %+v", cr)
	}
}

func TestClarifyExecuteNoBridgeFallsBack(t *testing.T) {
	defer SetClarifyBridge(nil)
	SetClarifyBridge(nil)
	tool := NewClarifyTool()
	res, err := tool.Execute(sessionCtx("s1"), map[string]interface{}{"question": "Q?"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.(*ClarifyResult); !ok {
		t.Fatalf("expected fallback *ClarifyResult, got %T", res)
	}
}

func TestClarifyExecuteBridgeUnavailableFallsBack(t *testing.T) {
	defer SetClarifyBridge(nil)
	SetClarifyBridge(&mockClarifyBridge{err: ErrClarifyUnavailable})
	tool := NewClarifyTool()
	res, err := tool.Execute(sessionCtx("s1"), map[string]interface{}{"question": "Q?"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.(*ClarifyResult); !ok {
		t.Fatalf("expected fallback *ClarifyResult, got %T", res)
	}
}

func TestClarifyExecuteBridgeAnswer(t *testing.T) {
	defer SetClarifyBridge(nil)
	SetClarifyBridge(&mockClarifyBridge{
		ans: &ClarifyAnswer{Choices: []string{"B"}, Note: "选 B 因为快"},
	})
	tool := NewClarifyTool()
	res, err := tool.Execute(sessionCtx("s1"), map[string]interface{}{
		"question": "A or B?",
		"options":  []interface{}{"A", "B"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text, ok := res.(string)
	if !ok {
		t.Fatalf("expected answer text (string), got %T", res)
	}
	if !strings.Contains(text, "A or B?") || !strings.Contains(text, "B") || !strings.Contains(text, "选 B 因为快") {
		t.Fatalf("answer text missing content: %q", text)
	}
	if _, ok := res.(*ClarifyResult); ok {
		t.Fatal("must not return ClarifyResult when bridge answered")
	}
}

func TestClarifyExecuteBridgeError(t *testing.T) {
	defer SetClarifyBridge(nil)
	bridgeErr := errors.New("clarification channel closed")
	SetClarifyBridge(&mockClarifyBridge{err: bridgeErr})
	tool := NewClarifyTool()
	_, err := tool.Execute(sessionCtx("s1"), map[string]interface{}{"question": "Q?"})
	if !errors.Is(err, bridgeErr) {
		t.Fatalf("expected bridge error to propagate, got %v", err)
	}
}

func TestClarifyExecuteRequiresQuestion(t *testing.T) {
	defer SetClarifyBridge(nil)
	tool := NewClarifyTool()
	if _, err := tool.Execute(sessionCtx("s1"), map[string]interface{}{}); err == nil {
		t.Fatal("expected error when question missing")
	}
}
