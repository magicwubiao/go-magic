package privacy

import (
	"strings"
	"testing"
)

// newTestRedactor builds a redactor with the given master switch and
// phone/ID redaction on — mirroring the shapes config.json produces.
func newTestRedactor(enabled bool) *Redactor {
	return NewRedactor(&Config{
		Enabled:        enabled,
		RedactPhone:    true,
		RedactEmail:    true,
		RedactIDCard:   true,
		RedactBankCard: true,
		RedactIP:       true,
		RedactAddress:  true,
		CustomPatterns: make(map[string]string),
	})
}

// TestRedactMasterSwitchDisabled guards the master switch: privacy.enabled
// = false must turn off ALL redaction regardless of the per-type flags.
// 2026-09-23 kanban bug: kanban workers fell back to DefaultConfig
// (enabled=true) because the spawner never wired config.Privacy, so tasks
// stayed masked after the user turned the switch off.
func TestRedactMasterSwitchDisabled(t *testing.T) {
	r := newTestRedactor(false)

	inputs := []string{
		"call 13812345678 now",
		"task_1790159593523920700",
		"D:\\workspace\\kanban\\task_1790159593523920700\\pelican_bike.html",
		"mail me at bob@example.com",
	}
	for _, in := range inputs {
		if got := r.Redact(in); got != in {
			t.Errorf("master switch off must not redact %q, got %q", in, got)
		}
	}
	if got, _ := r.RedactWithContext("13812345678"); got != "13812345678" {
		t.Errorf("RedactWithContext must pass through when disabled, got %q", got)
	}
	if m := r.Detect("13812345678"); len(m) != 0 {
		t.Errorf("Detect must find nothing when disabled, got %v", m)
	}
}

// TestRedactPhoneStandalone checks real phone numbers are still redacted
// once the boundaries were added (standalone or after non-word chars).
func TestRedactPhoneStandalone(t *testing.T) {
	r := newTestRedactor(true)

	cases := []string{
		"call 13812345678 now",
		"tel:13912345678",
		"联系电话 15812345678，谢谢",
	}
	for _, in := range cases {
		got := r.Redact(in)
		if !strings.Contains(got, "[PHONE]") {
			t.Errorf("expected [PHONE] in %q, got %q", in, got)
		}
		if strings.Contains(got, "13812345678") || strings.Contains(got, "13912345678") || strings.Contains(got, "15812345678") {
			t.Errorf("phone still visible after redaction: %q -> %q", in, got)
		}
	}
}

// TestRedactDoesNotEatLongDigitRuns guards against the false positive that
// broke kanban task execution: the unbounded phone regex used to match the
// first 11 digits of longer digit runs (19-digit task IDs, 13-digit
// millisecond timestamps), corrupting task IDs and file paths into
// "task_[PHONE]23920700".
func TestRedactDoesNotEatLongDigitRuns(t *testing.T) {
	r := newTestRedactor(true)

	inputs := []string{
		"task_1790159593523920700",
		"run_1790159665654669400",
		"D:\\workspace\\kanban\\task_1790159593523920700\\pelican_bike.html",
		"file:///D:/workspace/kanban/task_1790159593523920700/pelican_bike.html",
		"created at 1790159593523 ms",
		"order 6225880137523920 total 0", // 16-digit bank-shaped run, no separators
	}
	for _, in := range inputs {
		if got := r.Redact(in); got != in {
			t.Errorf("long digit run must survive redaction:\n in: %q\nout: %q", in, got)
		}
	}
}

// TestRedactIDCardStandalone ensures the ID_CARD pattern still matches a
// real 18-digit card number when standalone (boundary change must not
// disable it).
func TestRedactIDCardStandalone(t *testing.T) {
	r := newTestRedactor(true)

	in := "身份证 110101199003074570 请核实"
	got := r.Redact(in)
	if !strings.Contains(got, "[ID_CARD]") {
		t.Errorf("expected [ID_CARD] in %q, got %q", in, got)
	}
}
