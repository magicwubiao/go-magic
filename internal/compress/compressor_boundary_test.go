package compress

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// toolPairFixture builds a history whose protected head/tail cuts would land
// inside tool exchanges:
//
//	idx 0  user            ┐ head (ProtectFirstN=2)
//	idx 1  assistant(c1)   ┘
//	idx 2  tool(c1)          ← head cut would orphan this result
//	idx 3  user              ┐ middle (summarised)
//	idx 4  assistant         │
//	idx 5  user              ┘
//	idx 6  assistant(c7)   ┐
//	idx 7  tool(c7)        ┘ ← tail cut (len-ProtectLastN=7) lands on this tool
//	idx 8  user              ┐ tail
//	idx 9  assistant         │
//	idx 10 user            ┘
func toolPairFixture() []Message {
	tc := func(id string) []types.ToolCall {
		return []types.ToolCall{{ID: id, Type: "function", Function: types.Function{Name: "read_file"}}}
	}
	return []Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "", ToolCalls: tc("c1")},
		{Role: "tool", ToolCallID: "c1", Content: "r1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u2b"},
		{Role: "assistant", Content: "", ToolCalls: tc("c7")},
		{Role: "tool", ToolCallID: "c7", Content: "r7"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "a3"},
		{Role: "user", Content: "u4"},
	}
}

// assertNoOrphanTools verifies every tool message in msgs is preceded by an
// assistant that announces its tool_call id — the invariant providers enforce.
func assertNoOrphanTools(t *testing.T, msgs []Message) {
	t.Helper()
	claimed := map[string]bool{}
	for i, m := range msgs {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				claimed[tc.ID] = true
			}
		}
		if m.Role == "tool" && !claimed[m.ToolCallID] {
			t.Fatalf("message %d is an orphan tool result (tool_call_id=%q): %+v", i, m.ToolCallID, msgs)
		}
	}
}

// TestCompressDoesNotSplitToolExchanges is the regression test for compaction
// corrupting tool pairs: the head/tail cut points are chosen by message count,
// so they landed between an assistant's tool_calls and its results. The kept
// half then held a tool result with no owner (or an announcement with no
// results), which providers reject and the history sanitizers silently drop —
// costing the model the observations it just made.
func TestCompressDoesNotSplitToolExchanges(t *testing.T) {
	c := NewCompressor(1) // threshold 1 token: always compress
	msgs := toolPairFixture()

	result, err := c.Compress(msgs, "")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if result.CompressedCount >= result.OriginalCount {
		t.Fatalf("fixture did not compress: %d -> %d", result.OriginalCount, result.CompressedCount)
	}

	assertNoOrphanTools(t, result.Messages)

	// Both tool exchanges must survive with their ids intact: c1 (head side,
	// which the fix pulls into the head) and c7 (tail side, which the fix pulls
	// its owner along for).
	for _, id := range []string{"c1", "c7"} {
		var haveCall, haveResult bool
		for _, m := range result.Messages {
			if m.Role == "assistant" {
				for _, tc := range m.ToolCalls {
					if tc.ID == id {
						haveCall = true
					}
				}
			}
			if m.Role == "tool" && m.ToolCallID == id {
				haveResult = true
			}
		}
		if !haveCall || !haveResult {
			t.Fatalf("tool exchange %s split apart (call=%v result=%v): %+v",
				id, haveCall, haveResult, result.Messages)
		}
	}
}

// TestCompressPreservesToolCallIDThroughRoundTrip pins the field-level contract:
// whatever the caller sets on a protected message comes back out unchanged. Both
// the agent's compaction path and any other caller rely on it — a dropped
// tool_call_id is exactly what produced the orphan tool rows that crashed bot
// mode (sanitizeBotHistory panicked on them before the index fix).
func TestCompressPreservesToolCallIDThroughRoundTrip(t *testing.T) {
	c := NewCompressor(1)
	msgs := toolPairFixture()
	msgs[10].ContentParts = []types.ContentPart{{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,AAAA"}}}

	result, err := c.Compress(msgs, "")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	var seenC1, seenC7, seenParts bool
	for _, m := range result.Messages {
		if m.Role == "tool" && m.ToolCallID == "c1" {
			seenC1 = true
		}
		if m.Role == "tool" && m.ToolCallID == "c7" {
			seenC7 = true
		}
		if len(m.ContentParts) == 1 && m.ContentParts[0].Type == "image_url" {
			seenParts = true
		}
	}
	if !seenC1 || !seenC7 {
		t.Fatalf("tool_call_id dropped in round trip (c1=%v c7=%v): %+v", seenC1, seenC7, result.Messages)
	}
	if !seenParts {
		t.Fatalf("content_parts dropped in round trip: %+v", result.Messages)
	}
	if strings.TrimSpace(result.Summary) == "" {
		t.Fatalf("expected a generated summary, got empty")
	}
}

// TestCompressSkipsUnsplitExchange checks the degenerate case: one assistant
// announcing a tool call consumed by a long run of results leaves no cut point
// that keeps a provider-valid payload, so Compress must return the list
// untouched instead of emitting something the provider will reject.
func TestCompressSkipsUnsplitExchange(t *testing.T) {
	c := NewCompressor(1)
	msgs := []Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "", ToolCalls: []types.ToolCall{{
			ID: "c1", Type: "function", Function: types.Function{Name: "ls"},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "r1"},
		{Role: "tool", ToolCallID: "c1", Content: "r2"},
		{Role: "tool", ToolCallID: "c1", Content: "r3"},
		{Role: "tool", ToolCallID: "c1", Content: "r4"},
		{Role: "tool", ToolCallID: "c1", Content: "r5"},
		{Role: "tool", ToolCallID: "c1", Content: "r6"},
	}
	result, err := c.Compress(msgs, "")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if result.CompressedCount != result.OriginalCount {
		t.Fatalf("unsplittable exchange was compressed anyway: %d -> %d",
			result.OriginalCount, result.CompressedCount)
	}
	assertNoOrphanTools(t, result.Messages)
}
