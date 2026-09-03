package agent

import (
	"fmt"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// emptyAssistantPlaceholder is a stable, non-empty content value used to
// patch assistant messages whose content was lost to mid-stream truncation
// (or a tool-call-only turn). Rationale: the strictness axis between
// providers is the single biggest reason "switch to deepseek works / zhipu
// keeps 1214". Zhipu/GLM throws error 1214 ("messages 参数非法") on
// assistant content that is either (a) an empty string, (b) a pure whitespace
// string, or (c) nil. ValidateMessageAlternation mirrors Zhipu's strictness
// by checking strings.TrimSpace(msg.Content) == "".
//
// The placeholder must ALSO be invisible to the model. Every readable marker
// tried so far became part of the model's visible history and was echoed back
// as output:
//   - "[no content]"        -> model wrapped every reply in square brackets
//   - "..."                 -> model echoed the ellipsis as its whole reply
//   - "No response content" -> model repeated the sentence verbatim, flooding
//     non-zhipu providers' output with the phrase
//
// A single ZERO WIDTH SPACE (U+200B) is the only marker that is non-empty to
// TrimSpace (it is not a Unicode White_Space codepoint, so it survives) while
// rendering as nothing and carrying no natural-language signal for the model
// to copy. The trade-off — it is invisible in logs — is accepted because the
// visible alternatives all leaked into output.
//
// IMPORTANT: empty content is patched only in the outbound copy
// (buildLLMMessages + convert.go) right before sending to the provider, never
// in the stored history.
const emptyAssistantPlaceholder = "\u200b"

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
			// NOTE: empty assistant content is intentionally NOT flagged here.
			// The stored history (a.history) legitimately contains assistants
			// whose only output was tool_calls with no text — flagging them
			// would produce false violations and cause [no content] placeholder
			// leakage into the UI. Empty content is patched only in the outbound
			// copy (buildLLMMessages + convert.go) right before sending to the
			// provider, never in the stored history.
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

	// (R2) Removed: empty assistant content is no longer patched here.
	// SanitizeMessageHistory operates on a.history (the stored copy shown to
	// the UI), so writing [no content] here would leak the placeholder into
	// chat. Empty content is patched only in buildLLMMessages (outbound copy
	// to LLM) and convert.go (provider API payload), never in stored history.

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

	// ===== STAGE 2: GREEDY DROP OF UNREPAIRED OFFENDERS =====
	//
	// The index-based drop loop below is the reason violations appeared to
	// grow 26→30 across identical LLM calls in a single turn. The old
	// implementation collected violation Indices, rebuilt by dropping those
	// Indices, then tried to re-map *rebuilt* violation Indices back to the
	// *original* repaired array on the next iteration. After the first drop
	// pass the original array was unchanged, so re-scanning it over- and
	// under-counted kept positions depending on where the first drops had
	// been. The practical outcome: a minority of offenders were actually
	// dropped, Stage-2 returned a history that still had N violations, and
	// on the next LLM call Validate+Sanitize ran on (effectively) the same
	// input → same WARN line again. Consecutive tool calls made it look
	// like violations were monotonically growing because each new turn
	// added a fresh assistant that had to be fixed on top of the leftover
	// unrepaired ones.
	//
	// Greedy single-pass construction: walk messages in order and only keep
	// a message if, together with the tail of the already-kept prefix, it
	// does not introduce a violation that Stage 1 could not repair. This
	// is guaranteed to converge in O(n) and deterministically terminates
	// regardless of how pathological the input is. Preferred drops:
	//   • duplicate user      -> drop the *older* of the pair (keeps newest prompt)
	//   • duplicate assistant -> drop the *newer* one (keeps prior answer+TCs intact)
	//   • illegal tool        -> drop the tool (tool without header is useless)
	//   • leading tool        -> drop
	if len(repaired) == 0 {
		return repaired
	}
	out := make([]provider.Message, 0, len(repaired))
	for i := 0; i < len(repaired); i++ {
		m := repaired[i]
		role := m.Role

		// Always drop messages with truly empty role / unknown role — no
		// provider will accept these and keeping them cascades later
		// alternation failures.
		if role == "" {
			continue
		}
		switch role {
		case "system":
			out = append(out, m)
			continue
		case "tool":
			// Tool messages with empty tool_call_id can NEVER be legal
			// (stage-1 R3 only fills when prev assistant has exactly 1 TC).
			if m.ToolCallID == "" {
				continue
			}
			if len(out) == 0 {
				// leading tool, no possible preceding assistant → drop.
				continue
			}
			last := out[len(out)-1]
			if last.Role == "user" || last.Role == "system" {
				// tool directly after user/system → drop.
				continue
			}
			if last.Role == "assistant" && len(last.ToolCalls) == 0 {
				// tool follows assistant that did not declare any tool_calls → drop.
				continue
			}
			if last.Role == "assistant" {
				// Extra sanity: even if Stage-1 already ran orphan stripping
				// on the assistant's ToolCalls, ensure this tool's ID is
				// actually in the immediately-preceding assistant's kept
				// ToolCalls. If not, the tool is a late stray after a tool
				// block was sliced mid-way → drop.
				found := false
				for _, tc := range last.ToolCalls {
					if tc.ID == m.ToolCallID {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			out = append(out, m)
			continue
		case "user":
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				// two users in a row → drop older (replace tail with newer).
				out[len(out)-1] = m
				continue
			}
			out = append(out, m)
			continue
		case "assistant":
			// Stage-1 R2/R4 already fixed empty content + leading roles,
			// so here we only need to handle the duplicate-assistant case.
			// Two assistants in a row → drop the *new* one: dropping the
			// older would lose whatever tool_calls header anchored the
			// tool results that appear later in the stream.
			if len(out) > 0 && out[len(out)-1].Role == "assistant" {
				continue
			}
			out = append(out, m)
			continue
		default:
			// Unknown role → drop.
			continue
		}
	}
	return out
}
