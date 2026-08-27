package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncateWithSpill_NoTruncation(t *testing.T) {
	in := strings.Repeat("a", 100)
	got := TruncateWithSpill(in, "test", 200)
	if got != in {
		t.Fatalf("expected unchanged content, got len=%d", len(got))
	}
}

func TestTruncateWithSpill_TruncatesAndSpills(t *testing.T) {
	if os.Getenv("HOME") == "" {
		t.Skip("no HOME set")
	}
	in := strings.Repeat("x", 5000)
	got := TruncateWithSpill(in, "unit_test_tool", 100)
	if !strings.Contains(got, "[truncated") {
		t.Fatalf("expected truncation marker, got: %q", got[:min(len(got), 300)])
	}
	idx := strings.Index(got, "[full output")
	if idx < 0 {
		t.Fatalf("expected spill pointer in output")
	}
	path := strings.TrimSuffix(strings.TrimSpace(got[idx:]), "]")
	path = strings.TrimPrefix(path, "[full output ("+itoa(5000)+" chars) saved to ")
	if !strings.HasSuffix(path, ".txt") || !strings.Contains(path, "tool_outputs") {
		t.Fatalf("unexpected spill path: %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spilled file unreadable: %v", err)
	}
	if string(data) != in {
		t.Fatalf("spilled content mismatch")
	}
	defer os.Remove(path)
}

func TestSanitizeFilenameLabel(t *testing.T) {
	got := SanitizeFilenameLabel("../../etc/passwd!", 64)
	if strings.ContainsAny(got, "/.!") {
		t.Fatalf("unsafe chars remained: %q", got)
	}
	long := SanitizeFilenameLabel(strings.Repeat("中", 100), 64)
	if runeCount(long) > 64+3 { // "..." suffix
		t.Fatalf("label not length-limited: %d runes", runeCount(long))
	}
}

func itoa(n int) string {
	return fmt_Sprint(n)
}

func fmt_Sprint(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func runeCount(s string) int {
	count := 0
	for range s {
		count++
	}
	return count
}

func TestSpillToFile_EmptyLabel(t *testing.T) {
	p := SpillToFile("hello", "")
	if p == "" {
		t.Skip("spill unavailable in this environment")
	}
	defer os.Remove(p)
	if filepath.Base(p) == "" || len(filepath.Base(p)) < 5 {
		t.Fatalf("bad filename: %q", p)
	}
}
