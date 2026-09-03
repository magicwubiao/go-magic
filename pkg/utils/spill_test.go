package utils

import (
	"strings"
	"testing"
)

func TestErrTruncateDetailed_NoError(t *testing.T) {
	if got := ErrTruncateDetailed("", "test", 100); got != "" {
		t.Fatalf("expected empty output for empty error text, got %q", got)
	}
}

func TestErrTruncateDetailed_NoTruncation(t *testing.T) {
	in := "Error: something failed"
	got := ErrTruncateDetailed(in, "test", 100)
	if got != in {
		t.Fatalf("expected unchanged content, got %q", got)
	}
}

func TestErrTruncateDetailed_Truncates(t *testing.T) {
	in := "Error: " + strings.Repeat("x", 5000)
	got := ErrTruncateDetailed(in, "unit_test_tool", 100)
	if !strings.Contains(got, "[truncated") {
		t.Fatalf("expected truncation marker, got: %q", got[:min(len(got), 300)])
	}
	if len([]rune(got)) > 100+50 { // 截断标记额外长度
		t.Fatalf("output not truncated: %d runes", len([]rune(got)))
	}
}
