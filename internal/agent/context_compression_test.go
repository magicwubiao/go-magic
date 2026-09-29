package agent

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestMaybeCompressContextPreservesToolFields is the regression test for the
// data corruption behind the bot-mode crash. The compaction path converted
// a.history into compress.Message and back, copying only Role+Content — so every
// protected assistant lost its ToolCalls, every protected tool result lost its
// ToolCallID, and multimodal parts were dropped. The orphans that produced were
// persisted into bots.db, where the bot's load-time sanitizer had to throw them
// away (and, before the index fix, panicked on them).
func TestMaybeCompressContextPreservesToolFields(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus()}
	a.compressor = compress.NewCompressor(1) // threshold 1: always compress

	tc := func(id string) []types.ToolCall {
		return []types.ToolCall{{ID: id, Type: "function", Function: types.Function{Name: "read_file"}}}
	}
	a.history = []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "", ToolCalls: tc("c1")}, // head side
		{Role: "tool", ToolCallID: "c1", Content: "r1"},
		{Role: "user", Content: "u2"}, // summarised away
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "", ToolCalls: tc("c2")}, // tail side
		{Role: "tool", ToolCallID: "c2", Content: "r2"},
		{Role: "user", Content: "u4", ContentParts: []types.ContentPart{
			{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,AAAA"}},
		}},
	}
	before := len(a.history)

	a.maybeCompressContext()

	if len(a.history) >= before {
		t.Fatalf("history was not compacted: %d -> %d", before, len(a.history))
	}
	if a.history[0].Role != "system" || a.history[0].Content != "sys" {
		t.Fatalf("bot system prompt lost its head position: %+v", a.history[0])
	}
	summaries := 0
	for _, m := range a.history {
		if strings.Contains(m.Content, compress.SummaryPrefix) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("expected exactly 1 compaction summary, got %d", summaries)
	}

	// Both tool exchanges must still be pairable: the id on the assistant and
	// the id on its result.
	for _, id := range []string{"c1", "c2"} {
		var haveCall, haveResult bool
		for _, m := range a.history {
			if m.Role == "assistant" {
				for _, c := range m.ToolCalls {
					if c.ID == id {
						haveCall = true
					}
				}
			}
			if m.Role == "tool" && m.ToolCallID == id {
				haveResult = true
			}
		}
		if !haveCall || !haveResult {
			t.Fatalf("compaction broke tool exchange %s (call=%v result=%v): %+v",
				id, haveCall, haveResult, a.history)
		}
	}

	// Multimodal parts must survive: losing them silently turns an image turn
	// into a text turn.
	parts := 0
	for _, m := range a.history {
		for _, p := range m.ContentParts {
			if p.Type == "image_url" {
				parts++
			}
		}
	}
	if parts != 1 {
		t.Fatalf("content_parts lost by compaction: %+v", a.history)
	}

	// The compacted history must still be a payload a provider would accept.
	if v := ValidateMessageAlternation(a.history); len(v) > 0 {
		t.Fatalf("compacted history invalid: %+v", v)
	}
}

// TestMaybeCompressContextNoopWhenDisabled guards the refactor: the extraction
// of the inline block must keep the compressor optional.
func TestMaybeCompressContextNoopWhenDisabled(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus()}
	a.history = []provider.Message{{Role: "user", Content: strings.Repeat("x", 10000)}}
	a.maybeCompressContext()
	if len(a.history) != 1 {
		t.Fatalf("history changed with no compressor: %+v", a.history)
	}
}
