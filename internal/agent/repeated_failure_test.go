package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// failingProvider returns the same error for every call, simulating a
// permanently failing endpoint (e.g. zhipu 1214 on a poisoned history).
type failingProvider struct {
	err error
}

func (m *failingProvider) Name() string { return "failing-mock" }

func (m *failingProvider) Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	return nil, m.err
}

func (m *failingProvider) ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error) {
	return nil, m.err
}

// TestAgent_RepeatedErrorAbortsBeforeMaxTurns verifies the repeated-failure
// guard: a permanently failing provider must abort after the detector's
// threshold instead of burning the full maxTurns budget with two API calls
// per iteration (stream + fallback).
func TestAgent_RepeatedErrorAbortsBeforeMaxTurns(t *testing.T) {
	sentinel := errors.New("api error 1214: messages 参数非法")
	p := &failingProvider{err: sentinel}

	ag := NewAIAgent(p, nil, nil, "test system prompt")
	// Default detector threshold is 3. Give the loop far more headroom than
	// the threshold so an unguarded loop would be detectable.
	ag.ApplyOption(WithMaxTurns(50))

	_, err := ag.RunConversation(context.Background(), "hello")

	if err == nil {
		t.Fatal("expected error from permanently failing provider")
	}
	if !errors.Is(err, sentinel) && !strings.Contains(err.Error(), "repeated failure escalated") {
		t.Fatalf("expected sentinel or escalation error, got: %v", err)
	}

	// The loop must have aborted well before 50 turns. Threshold 3 means the
	// detector escalates on the 3rd identical failure; allow a small margin
	// for the final iteration to unwind, but nowhere near maxTurns.
	if got := ag.iterationCount; got > 6 {
		t.Errorf("loop ran %d iterations; expected abort near detector threshold (3), not maxTurns", got)
	}
	t.Logf("aborted after %d iteration(s) with: %v", ag.iterationCount, err)
}

// TestAgent_RepeatedErrorStreamPath verifies the same guard on the streaming
// loop (RunConversationStream), where stream failures fall through to the
// non-streaming fallback each turn.
func TestAgent_RepeatedErrorStreamPath(t *testing.T) {
	sentinel := errors.New("api error 1214: messages 参数非法")
	p := &failingProvider{err: sentinel}

	ag := NewAIAgent(p, nil, nil, "test system prompt")
	ag.ApplyOption(WithMaxTurns(50))

	err := ag.RunConversationStream(context.Background(), "hello", func(content string, done bool) {})

	if err == nil {
		t.Fatal("expected error from permanently failing provider")
	}
	if got := ag.iterationCount; got > 6 {
		t.Errorf("stream loop ran %d iterations; expected abort near detector threshold (3)", got)
	}
	t.Logf("stream loop aborted after %d iteration(s) with: %v", ag.iterationCount, err)
}
