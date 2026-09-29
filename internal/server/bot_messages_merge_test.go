package server

import (
	"strings"
	"testing"
)

// The dashboard renders one bubble per merged assistant group. A group that
// contains a tool step carries that step's reasoning as its text — the turn's
// answer only exists once the model stops calling tools. An interrupted turn
// (the user cancels, the bot is stopped, the client switches chats) leaves the
// tool step as the LAST assistant message, and the UI then saw a bubble whose
// text is pure <think> reasoning. Without hasToolCalls the dashboard promoted
// that reasoning into the visible answer, so the model's private monologue was
// shown as the bot's reply (seen in the wild, verbatim, in bot:design:chat).
//
// This pins the flag onto the merge result, which is the only signal the UI has.
func TestMergeBotChatMessagesMarksToolStep(t *testing.T) {
	reasoned := "<think>The user sent an image. Let me analyze the actual uploaded file to be sure.</think>\n"
	entries := []botChatMsg{
		{id: "u0", role: "user", timestamp: 100},
		{id: "a1", role: "assistant", content: "<think>step one</think>\n", timestamp: 101, hasToolCalls: true},
		{id: "a2", role: "assistant", content: "<think>step two</think>\n\u200b", timestamp: 102, hasToolCalls: true},
		// Interrupted here: no answer message follows, and the trailing tool
		// result / error messages are not part of the chat projection at all.
		{id: "u1", role: "user", timestamp: 200},
		{id: "a3", role: "assistant", content: reasoned, timestamp: 201, hasToolCalls: true},
		{id: "a4", role: "assistant", content: "<think>done</think>\nHere is the answer.", timestamp: 202},
	}

	merged := mergeBotChatMessages(entries)
	if len(merged) != 4 {
		t.Fatalf("want 4 bubbles (2 user + 2 assistant groups), got %d: %+v", len(merged), merged)
	}

	// User bubbles are never folded into an assistant group.
	if merged[0].role != "user" || merged[0].id != "u0" {
		t.Fatalf("bubble 0 = %+v, want the user message", merged[0])
	}

	// Group 1: two tool steps, merged. The flag must survive the merge.
	if merged[1].role != "assistant" {
		t.Fatalf("bubble 1 = %+v, want assistant", merged[1])
	}
	if !merged[1].hasToolCalls {
		t.Fatalf("merged tool-step group lost hasToolCalls: %+v", merged[1])
	}
	if !strings.Contains(merged[1].content, "step one") || !strings.Contains(merged[1].content, "step two") {
		t.Fatalf("merged content lost a step: %q", merged[1].content)
	}
	if merged[1].timestamp != 102 {
		t.Fatalf("merged timestamp = %d, want the newest (102)", merged[1].timestamp)
	}

	// Group 2: an interrupted turn — the bubble the dashboard must NOT present as
	// an answer. hasToolCalls is what makes the UI keep the reasoning collapsed.
	if merged[3].role != "assistant" {
		t.Fatalf("bubble 3 = %+v, want assistant", merged[3])
	}
	if !merged[3].hasToolCalls {
		t.Fatalf("interrupted tool step lacks hasToolCalls: %+v", merged[3])
	}

	// A single answer-only assistant message stays promotable (no tool step).
	single := mergeBotChatMessages([]botChatMsg{
		{id: "u0", role: "user", timestamp: 1},
		{id: "a1", role: "assistant", content: "<think>only reasoning</think>\n", timestamp: 2},
	})
	if len(single) != 2 || single[1].hasToolCalls {
		t.Fatalf("answer-only assistant must not be flagged: %+v", single)
	}

	// Consecutive user messages each keep their own bubble.
	two := mergeBotChatMessages([]botChatMsg{
		{id: "u0", role: "user", timestamp: 1},
		{id: "u1", role: "user", timestamp: 2},
	})
	if len(two) != 2 {
		t.Fatalf("user messages must not be merged: %+v", two)
	}
}
