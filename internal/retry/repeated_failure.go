// Package retry — repeated failure detection.
//
// This file closes the architectural gap documented in Hermes Agent Issue #22112:
// the runtime loop could not classify repeated equivalent failures as a failure
// class, retrying the same sequence silently until the turn cap terminated it.
// That produces noisy traces that an evolution engine (GEPA) cannot learn from.
//
// RepeatedFailureDetector recognizes when the same error signature recurs N
// times within a sliding window of M turns. When the threshold is crossed it
// escalates (halts the loop with a structured diagnostic) instead of retrying.
package retry

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FailureSignature is a stable hash identifying an equivalence class of
// failures. Two failures with the same signature are treated as equivalent
// (same root cause) for escalation purposes.
type FailureSignature string

// Escalation is the structured diagnostic produced when a repeated failure
// threshold is crossed. It is returned to the agent loop so the agent can
// halt cleanly and surface actionable information.
type Escalation struct {
	Signature FailureSignature
	Reason    FailoverReason
	ToolName  string
	Count     int           // number of occurrences within the window
	Window    time.Duration // window over which occurrences were counted
	FirstSeen time.Time
	LastSeen  time.Time
	SampleMsg string // a representative error message
}

// Error returns a human-readable diagnostic.
func (e *Escalation) Error() string {
	return fmt.Sprintf("repeated failure escalated: tool=%q reason=%s count=%d within %s; sample: %s",
		e.ToolName, e.Reason, e.Count, e.Window, e.SampleMsg)
}

// RepeatedFailureConfig tunes the detector.
type RepeatedFailureConfig struct {
	// Threshold is the number of equivalent failures within Window that
	// triggers escalation. Must be >= 2.
	Threshold int
	// Window is the look-back duration. Failures older than Window are
	// evicted and do not count toward escalation.
	Window time.Duration
	// MaxMessageLen caps the stored sample error message length.
	MaxMessageLen int
}

// DefaultRepeatedFailureConfig returns a sensible default: escalate after 3
// equivalent failures within 5 minutes.
func DefaultRepeatedFailureConfig() RepeatedFailureConfig {
	return RepeatedFailureConfig{
		Threshold:     3,
		Window:        5 * time.Minute,
		MaxMessageLen: 200,
	}
}

// occurrence is a single recorded failure event.
type occurrence struct {
	at      time.Time
	message string
}

// bucket groups equivalent failures and remembers the originating tool so a
// per-tool reset is possible (the signature is a one-way hash).
type bucket struct {
	tool        string
	reason      FailoverReason
	occurrences []occurrence
}

// RepeatedFailureDetector tracks equivalent failures and escalates when a
// threshold is crossed within a sliding window. It is safe for concurrent use.
type RepeatedFailureDetector struct {
	cfg  RepeatedFailureConfig
	mu   sync.Mutex
	seen map[FailureSignature]*bucket
}

// NewRepeatedFailureDetector constructs a detector with the given config.
func NewRepeatedFailureDetector(cfg RepeatedFailureConfig) *RepeatedFailureDetector {
	if cfg.Threshold < 2 {
		cfg.Threshold = 2
	}
	if cfg.Window <= 0 {
		cfg.Window = 5 * time.Minute
	}
	if cfg.MaxMessageLen <= 0 {
		cfg.MaxMessageLen = 200
	}
	return &RepeatedFailureDetector{
		cfg:  cfg,
		seen: make(map[FailureSignature]*bucket),
	}
}

// SignatureFor computes a stable signature for a classified error scoped to a
// tool. The message is deliberately excluded from the signature so that
// surface-level wording differences (timestamps, request IDs) do not split an
// equivalence class. The reason and tool name are the identity.
func SignatureFor(ce *ClassifiedError, toolName string) FailureSignature {
	reason := FailoverUnknown
	status := 0
	if ce != nil {
		reason = ce.Reason
		status = ce.StatusCode
	}
	// Normalize tool name: empty tool (LLM-level failure) is its own class.
	tool := strings.TrimSpace(strings.ToLower(toolName))
	raw := fmt.Sprintf("tool=%s|reason=%s|status=%d", tool, reason, status)
	sum := sha1.Sum([]byte(raw))
	return FailureSignature(hex.EncodeToString(sum[:]))
}

// Record registers a failure. If the threshold is crossed within the window,
// it returns a non-nil Escalation that the caller should propagate to halt the
// loop. Returns nil when no escalation is warranted (including when ce is nil).
func (d *RepeatedFailureDetector) Record(ce *ClassifiedError, toolName string, errMsg string) *Escalation {
	if d == nil || ce == nil {
		return nil
	}

	sig := SignatureFor(ce, toolName)
	now := time.Now()
	sample := truncateMsg(errMsg, d.cfg.MaxMessageLen)
	tool := strings.TrimSpace(strings.ToLower(toolName))

	d.mu.Lock()
	defer d.mu.Unlock()

	b := d.seen[sig]
	if b == nil {
		b = &bucket{tool: tool, reason: ce.Reason}
		d.seen[sig] = b
	}

	// Evict expired occurrences.
	cutoff := now.Add(-d.cfg.Window)
	kept := b.occurrences[:0]
	for _, o := range b.occurrences {
		if o.at.After(cutoff) {
			kept = append(kept, o)
		}
	}
	kept = append(kept, occurrence{at: now, message: sample})
	b.occurrences = kept

	if len(b.occurrences) < d.cfg.Threshold {
		return nil
	}

	// Threshold crossed — escalate.
	first := b.occurrences[0].at
	esc := &Escalation{
		Signature: sig,
		Reason:    b.reason,
		ToolName:  toolName,
		Count:     len(b.occurrences),
		Window:    d.cfg.Window,
		FirstSeen: first,
		LastSeen:  now,
		SampleMsg: sample,
	}
	// Reset the bucket so the same signature does not immediately re-escalate
	// on the next call; the caller is expected to halt.
	delete(d.seen, sig)
	return esc
}

// RecordError is a convenience wrapper that classifies a raw error before
// recording. Use this when the caller does not already have a ClassifiedError.
func (d *RepeatedFailureDetector) RecordError(err error, statusCode int, provider, model, toolName string) *Escalation {
	if d == nil || err == nil {
		return nil
	}
	ce := ClassifyError(err, statusCode, provider, model)
	return d.Record(ce, toolName, err.Error())
}

// Reset clears all recorded failures. Called when a task completes or the
// agent successfully recovers, so prior failures do not poison future runs.
func (d *RepeatedFailureDetector) Reset() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = make(map[FailureSignature]*bucket)
}

// ResetTool clears recorded failures for a specific tool. Useful after a
// successful tool call resets the failure streak for that tool.
func (d *RepeatedFailureDetector) ResetTool(toolName string) {
	if d == nil {
		return
	}
	tool := strings.TrimSpace(strings.ToLower(toolName))
	if tool == "" {
		d.Reset()
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for sig, b := range d.seen {
		if b != nil && b.tool == tool {
			delete(d.seen, sig)
		}
	}
}

func truncateMsg(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
