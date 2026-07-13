package agent

import (
	"fmt"

	"github.com/magicwubiao/go-magic/internal/provider"
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
		case "tool":
			// A tool message must follow an assistant that had tool_calls, or
			// another tool (parallel results). Following a user/system is illegal.
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
	violations := ValidateMessageAlternation(messages)
	if len(violations) == 0 {
		// Return a shallow copy for safety.
		out := make([]provider.Message, len(messages))
		copy(out, messages)
		return out
	}

	// Build a set of indices to drop. Dropping a message can change the
	// alternation of its neighbors, so we iterate until stable.
	drop := make(map[int]bool)
	for _, v := range violations {
		drop[v.Index] = true
	}

	// Iterate: rebuild, re-validate, drop new offenders. Cap iterations to
	// avoid pathological loops.
	for iter := 0; iter < 4; iter++ {
		rebuilt := make([]provider.Message, 0, len(messages)-len(drop))
		for i, m := range messages {
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
			for i := 0; i < len(messages); i++ {
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
	out := make([]provider.Message, 0, len(messages)-len(drop))
	for i, m := range messages {
		if !drop[i] {
			out = append(out, m)
		}
	}
	return out
}
