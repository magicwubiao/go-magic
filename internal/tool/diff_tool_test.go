package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp creates a temp file with the given content (LF endings).
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return path
}

// runApply is a helper that executes apply_patch with the given patches.
func runApply(t *testing.T, path string, patches [][2]string) (interface{}, error) {
	t.Helper()
	items := make([]interface{}, 0, len(patches))
	for _, p := range patches {
		items = append(items, map[string]interface{}{
			"old_text": p[0],
			"new_text": p[1],
		})
	}
	return (&DiffPatchTool{}).Execute(context.Background(), map[string]interface{}{
		"action":  "apply_patch",
		"path":    path,
		"patches": items,
	})
}

// readBack reads the file and normalizes CRLF for assertions.
func readBack(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return normalizeLineEndings(string(data))
}

// TestApplyPatchGrowingReplacement 复现并守护核心重复行 bug：
// 当 newLines 比 oldLines 长（增长型替换）时，旧实现的 append 切片别名
// 会把插入内容的尾部在文件中写两份。修复后必须恰好出现一次。
func TestApplyPatchGrowingReplacement(t *testing.T) {
	path := writeTemp(t, "alpha\nbeta\ngamma\n")

	_, err := runApply(t, path, [][2]string{
		{"beta\n", "beta\ninserted-1\ninserted-2\n"},
	})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	got := readBack(t, path)
	want := "alpha\nbeta\ninserted-1\ninserted-2\ngamma\n"
	if got != want {
		t.Fatalf("growing replacement corrupted file:\nwant: %q\n got: %q", want, got)
	}
	if strings.Count(got, "inserted-1") != 1 || strings.Count(got, "inserted-2") != 1 {
		t.Fatalf("duplicated lines detected:\n%s", got)
	}
}

// TestApplyPatchSequentialDependency 验证顺序补丁语义：
// 补丁 N 的输出是补丁 N+1 的输入（旧实现按行号切片，无法保证此语义）。
func TestApplyPatchSequentialDependency(t *testing.T) {
	path := writeTemp(t, "step-one\nkeep\n")

	_, err := runApply(t, path, [][2]string{
		{"step-one", "step-two"},
		{"step-two", "step-three"},
	})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	got := readBack(t, path)
	want := "step-three\nkeep\n"
	if got != want {
		t.Fatalf("sequential replacement wrong:\nwant: %q\n got: %q", want, got)
	}
}

// TestApplyPatchShrinkingReplacement 收缩型替换（newLines 比 oldLines 短）。
func TestApplyPatchShrinkingReplacement(t *testing.T) {
	path := writeTemp(t, "head\na\nb\ntail\n")

	_, err := runApply(t, path, [][2]string{
		{"a\nb\n", "merged\n"},
	})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	got := readBack(t, path)
	want := "head\nmerged\ntail\n"
	if got != want {
		t.Fatalf("shrinking replacement wrong:\nwant: %q\n got: %q", want, got)
	}
}

// TestApplyPatchAtomicFailure 后序补丁失配时必须报错且文件保持原样
// （真原子性：任何一步失败都不落盘）。
func TestApplyPatchAtomicFailure(t *testing.T) {
	original := "one\ntwo\nthree\n"
	path := writeTemp(t, original)

	_, err := runApply(t, path, [][2]string{
		{"two", "TWO"},             // 会成功
		{"not-exist-anymore", "x"}, // 在原始内容上存在性校验能过？不能——直接失配
	})
	if err == nil {
		t.Fatal("expected error for unmatched second patch, got nil")
	}
	if !strings.Contains(err.Error(), "patch[1]") {
		t.Fatalf("error should identify patch index, got: %v", err)
	}

	if got := readBack(t, path); got != original {
		t.Fatalf("file was modified despite atomic failure:\n%q", got)
	}
}

// TestApplyPatchAmbiguousRejected 同一 old_text 出现多次必须拒绝。
func TestApplyPatchAmbiguousRejected(t *testing.T) {
	path := writeTemp(t, "dup\nmid\ndup\n")

	_, err := runApply(t, path, [][2]string{
		{"dup", "X"},
	})
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error should mention ambiguity, got: %v", err)
	}

	if got := readBack(t, path); got != "dup\nmid\ndup\n" {
		t.Fatalf("ambiguous patch should not modify file, got:\n%s", got)
	}
}

// TestApplyPatchCRLFPreserved CRLF 文件替换后行尾风格保持不变。
func TestApplyPatchCRLFPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crlf.txt")
	if err := os.WriteFile(path, []byte("aa\r\nbb\r\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := runApply(t, path, [][2]string{{"bb", "B"}}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "\r\n") {
		t.Fatalf("CRLF line endings not preserved: %q", data)
	}
	if normalizeLineEndings(string(data)) != "aa\nB\n" {
		t.Fatalf("content wrong after CRLF patch: %q", normalizeLineEndings(string(data)))
	}
}
