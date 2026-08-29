package agent

import (
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Message alternation validation, after Hermes Agent's strict pre-provider
// enforcement. Providers reject malformed histories (two assistants in a row,
// a tool message without a preceding assistant tool_call, etc.) with opaque
// 400 errors. Validating before the call turns those into actionable, typed
// diagnostics and lets the loop self-correct instead of burning a retry.
//
// Rules enforced (OpenAI-compatible convention used across providers):
//   - "system" may appear anywhere but is conventionally leading; consecutive
//     system messages are tolerated.
//   - After system: user → assistant → user → assistant → ...
//   - "tool" messages may repeat (parallel tool results) but MUST be preceded
//     by an "assistant" message carrying tool_calls.
//   - Two "user" messages in a row are illegal.
//   - Two "assistant" messages in a row are illegal (a tool block must sit
//     between them).
//   - An empty/unknown role is illegal.

// Violation describes a single alternation problem in a message history.
type Violation struct {
	Index    int    // index of the offending message
	Role     string // role of the offending message
	Reason   string // human-readable reason
	PrevRole string // role of the preceding message, if any
}

// Error renders a violation as a string.
func (v Violation) Error() string {
	return fmt.Sprintf("message alternation violation at index %d (role=%q, prev=%q): %s",
		v.Index, v.Role, v.PrevRole, v.Reason)
}

// ValidateMessageAlternation checks a message history against the alternation
// rules and returns all violations. An empty/nil history is valid. A history
// containing only system messages is valid.
func ValidateMessageAlternation(messages []provider.Message) []Violation {
	if len(messages) == 0 {
		return nil
	}
	var violations []Violation

	for i, msg := range messages {
		role := msg.Role
		if role == "" {
			violations = append(violations, Violation{
				Index: i, Role: role, Reason: "empty role",
			})
			continue
		}

		// First message: system or user (or assistant/tool in rare resumed
		// histories — we only flag clearly illegal starts).
		if i == 0 {
			if role == "tool" {
				violations = append(violations, Violation{
					Index: i, Role: role, Reason: "history starts with a tool message (no preceding assistant tool_call)",
				})
			}
			continue
		}

		prev := messages[i-1].Role
		switch role {
		case "system":
			// Consecutive system messages are tolerated (some providers merge).
			continue
		case "user":
			if prev == "user" {
				violations = append(violations, Violation{
					Index: i, Role: role, PrevRole: prev,
					Reason: "two user messages in a row",
				})
			}
		case "assistant":
			if prev == "assistant" {
				violations = append(violations, Violation{
					Index: i, Role: role, PrevRole: prev,
					Reason: "two assistant messages in a row (tool results must sit between them)",
				})
			} else if prev == "system" && i > 0 {
				// assistant right after system is legal in some flows; tolerate.
			}
			// RC2: assistant content must be non-empty (after trimming).
			// Providers (Zhipu/GLM error 1214 "messages 参数非法", Anthropic
			// "messages must contain non-empty content") reject truly-empty
			// assistants REGARDLESS of whether ToolCalls are also present — a
			// tool-calling assistant with "" content is still 1214.
			if strings.TrimSpace(msg.Content) == "" {
				violations = append(violations, Violation{
					Index: i, Role: role, PrevRole: prev,
					Reason: "assistant message with empty content (provider 1214: messages 参数非法)",
				})
			}
			// RC1: assistant carries ToolCalls but not all of them have a
			// matching tool result in the immediately-following consecutive
			// tool block. This is the #1 26-turn 1214 root cause:
			// truncateHistory deletes tool results from the tail of a
			// parallel-tool-call block while leaving the assistant header
			// with its ToolCalls slice untouched.
			if len(msg.ToolCalls) > 0 {
				seen := make(map[string]struct{}, len(msg.ToolCalls))
				for j := i + 1; j < len(messages) && messages[j].Role == "tool"; j++ {
					if messages[j].ToolCallID != "" {
						seen[messages[j].ToolCallID] = struct{}{}
					}
				}
				missing := 0
				for _, tc := range msg.ToolCalls {
					if tc.ID == "" {
						missing++
						continue
					}
					if _, ok := seen[tc.ID]; !ok {
						missing++
					}
				}
				if missing > 0 {
					violations = append(violations, Violation{
						Index: i, Role: role, PrevRole: prev,
						Reason: fmt.Sprintf(
							"assistant declares %d tool_call(s) but %d tool result(s) are missing in the following tool block (provider 1214: messages 参数非法)",
							len(msg.ToolCalls), missing),
					})
				}
			}
		case "tool":
			// A tool message must follow an assistant that had tool_calls, or
			// another tool (parallel results). Following a user/system is illegal.
			if msg.ToolCallID == "" {
				violations = append(violations, Violation{
					Index: i, Role: role, PrevRole: prev,
					Reason: "tool message with empty tool_call_id (provider 1214: messages 参数非法)",
				})
			}
			if prev == "user" || prev == "system" {
				violations = append(violations, Violation{
					Index: i, Role: role, PrevRole: prev,
					Reason: "tool message not preceded by an assistant tool_call",
				})
			} else if prev == "assistant" && len(messages[i-1].ToolCalls) == 0 {
				violations = append(violations, Violation{
					Index: i, Role: role, PrevRole: prev,
					Reason: "tool message follows an assistant with no tool_calls",
				})
			}
		default:
			violations = append(violations, Violation{
				Index: i, Role: role, Reason: fmt.Sprintf("unknown role %q", role),
			})
		}
	}
	return violations
}

// SanitizeMessageHistory returns a copy of the history with offending messages
// dropped so the result passes ValidateMessageAlternation. It is a best-effort
// repair: it preserves system messages and the maximal legal prefix/suffix,
// dropping only the messages that break alternation. Tool messages that lose
// their preceding assistant are also dropped (they are meaningless alone).
//
// This is used as a defensive last resort before sending to a provider; the
// agent loop should ideally not produce illegal histories, but streaming
// fallbacks and partial failures can.
func SanitizeMessageHistory(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return messages
	}

	// ===== STAGE 1: IN-PLACE REPAIRS (preserve as much content as possible) =====
	//
	// Many violations are repairable without dropping a whole message —
	// dropping an assistant is destructive because you lose whatever partial
	// answer content the model did write before the truncation break. These
	// are the RC1/RC2 cases that trigger Zhipu 1214 mid-conversation.
	repaired := make([]provider.Message, len(messages))
	for i := range messages {
		repaired[i] = messages[i]
	}

	// (R1) Strip orphan / partial ToolCalls from assistant messages that
	// declare more tool_calls than have a matching tool result in the
	// immediately-following tool block. This is RC1's preferred fix: keep
	// the assistant content + the tool_calls that DID have results, strip
	// only the orphan subset.
	for i := 0; i < len(repaired); i++ {
		m := &repaired[i]
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		// Collect IDs in the contiguous following tool block.
		seen := make(map[string]struct{}, len(m.ToolCalls))
		for j := i + 1; j < len(repaired) && repaired[j].Role == "tool"; j++ {
			if repaired[j].ToolCallID != "" {
				seen[repaired[j].ToolCallID] = struct{}{}
			}
		}
		kept := make([]types.ToolCall, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if tc.ID == "" {
				continue
			}
			if _, ok := seen[tc.ID]; ok {
				kept = append(kept, tc)
			}
		}
		m.ToolCalls = kept
	}

	// (R2) Repair empty assistant content with a single-space placeholder.
	// Zhipu/GLM throws 1214 on truly-empty assistant content regardless of
	// whether ToolCalls are present; providers treat single-space as
	// non-empty. The repair is benign for providers whose validators ignore
	// content entirely, and keeps the history intact.
	for i := range repaired {
		m := &repaired[i]
		if m.Role == "assistant" && strings.TrimSpace(m.Content) == "" {
			m.Content = " "
		}
	}

	// (R3) Fill empty tool_call_id on tool messages whose id was blanked
	// during a truncation or partial stream recovery. We only assign an id
	// when the preceding assistant has exactly one ToolCall — in that case
	// the mapping is unambiguous. If the assistant declared several
	// tool_calls the only safe fix remains dropping the offender in stage 2.
	for i := 1; i < len(repaired); i++ {
		m := &repaired[i]
		if m.Role != "tool" || m.ToolCallID != "" {
			continue
		}
		prev := &repaired[i-1]
		if prev.Role == "assistant" && len(prev.ToolCalls) == 1 && prev.ToolCalls[0].ID != "" {
			m.ToolCallID = prev.ToolCalls[0].ID
		}
		// If prev is another tool, carry its id forward only if the prior
		// tool block is of size 1 (already handled) or 2 and 1st was blank
		// filled in a prior iteration. Otherwise leave blank for stage-2
		// drop.
	}

	// (R4) Drop leading illegal roles (assistant/tool with no preceding
	// system or user) — truncateHistory cut-off after system/user deleted.
	for len(repaired) > 0 && repaired[0].Role != "system" && repaired[0].Role != "user" {
		repaired = repaired[1:]
	}

	// ===== STAGE 2: DROP OFFENDERS THAT COULD NOT BE REPAIRED =====
	violations := ValidateMessageAlternation(repaired)
	if len(violations) == 0 {
		return repaired
	}

	drop := make(map[int]bool)
	for _, v := range violations {
		drop[v.Index] = true
	}

	// Iterate: rebuild, re-validate, drop new offenders. Cap iterations to
	// avoid pathological loops.
	for iter := 0; iter < 4; iter++ {
		rebuilt := make([]provider.Message, 0, len(repaired)-len(drop))
		for i, m := range repaired {
			if drop[i] {
				continue
			}
			rebuilt = append(rebuilt, m)
		}
		newVio := ValidateMessageAlternation(rebuilt)
		if len(newVio) == 0 {
			return rebuilt
		}
		// Map rebuilt indices back to original indices by re-scanning.
		origIdx := 0
		for _, v := range newVio {
			// Find the original index of the v.Index-th kept message.
			kept := -1
			for i := 0; i < len(repaired); i++ {
				if drop[i] {
					continue
				}
				kept++
				if kept == v.Index {
					origIdx = i
					break
				}
			}
			drop[origIdx] = true
		}
	}

	// Final rebuild.
	out := make([]provider.Message, 0, len(repaired)-len(drop))
	for i, m := range repaired {
		if !drop[i] {
			out = append(out, m)
		}
	}
	return out
}
