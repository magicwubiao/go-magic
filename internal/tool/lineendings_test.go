package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Line ending helpers
// ---------------------------------------------------------------------------

func TestDetectLineEnding(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"empty", "", "\n"},
		{"lf only", "a\nb\n", "\n"},
		{"crlf only", "a\r\nb\r\n", "\r\n"},
		{"crlf dominant", "a\r\nb\r\nc\n", "\r\n"},
		{"lf dominant", "a\nb\nc\r\n", "\n"},
		{"mixed equal -> lf", "a\r\nb\n", "\n"},
		{"single line no ending", "abc", "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectLineEnding(tt.content); got != tt.want {
				t.Errorf("detectLineEnding(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	if got := normalizeLineEndings("a\r\nb\rc\n"); got != "a\nb\nc\n" {
		t.Errorf("normalizeLineEndings = %q", got)
	}
	if got := normalizeLineEndings("no endings"); got != "no endings" {
		t.Errorf("normalizeLineEndings = %q", got)
	}
}

func TestConvertLineEndings(t *testing.T) {
	if got := convertLineEndings("a\nb\n", "\r\n"); got != "a\r\nb\r\n" {
		t.Errorf("convert to CRLF = %q", got)
	}
	if got := convertLineEndings("a\r\nb\r\n", "\n"); got != "a\nb\n" {
		t.Errorf("convert to LF = %q", got)
	}
	// 已混合行尾也应统一为 CRLF
	if got := convertLineEndings("a\r\nb\n", "\r\n"); got != "a\r\nb\r\n" {
		t.Errorf("convert mixed to CRLF = %q", got)
	}
}

// ---------------------------------------------------------------------------
// FileEditTool pure functions on CRLF content
// ---------------------------------------------------------------------------

func TestFileEditToolReplaceCRLFSingleLine(t *testing.T) {
	tool := &FileEditTool{}
	content := "line one\r\nline two\r\nline three\r\n"

	// old_content 用 LF 行尾（单行无行尾），匹配 CRLF 文件
	params := map[string]interface{}{
		"old_content": "line two",
		"new_content": "line TWO",
	}
	got, err := tool.replaceContent(content, params)
	if err != nil {
		t.Fatalf("replaceContent: %v", err)
	}
	want := "line one\r\nline TWO\r\nline three\r\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !strings.Contains(got, "\r\n") {
		t.Errorf("CRLF lost: %q", got)
	}
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
}

func TestFileEditToolReplaceCRLFMultiline(t *testing.T) {
	tool := &FileEditTool{}
	content := "package approval\n\nconst (\n\tRiskLow RiskLevel = 1\n\tRiskMedium RiskLevel = 2\n)\n"
	content = convertLineEndings(content, "\r\n")

	// 多行 old_content 使用 LF，目标文件为 CRLF
	oldText := "const (\n\tRiskLow RiskLevel = 1"
	newText := "// TimeoutStrategy comment\nconst (\n\tRiskLow RiskLevel = 1"
	params := map[string]interface{}{
		"old_content": oldText,
		"new_content": newText,
	}
	got, err := tool.replaceContent(content, params)
	if err != nil {
		t.Fatalf("replaceContent: %v", err)
	}

	norm := normalizeLineEndings(got)
	if !strings.Contains(norm, "// TimeoutStrategy comment\nconst (\n\tRiskLow RiskLevel = 1") {
		t.Errorf("replacement missing: %q", got)
	}
	// 写回内容无混合行尾
	crlf := strings.Count(got, "\r\n")
	lf := strings.Count(got, "\n")
	if lf != crlf {
		t.Errorf("mixed line endings: crlf=%d lf=%d, got %q", crlf, lf, got)
	}
}

func TestFileEditToolInsertCRLF(t *testing.T) {
	tool := &FileEditTool{}
	content := "a\r\nb\r\nc\r\n"
	params := map[string]interface{}{
		"line_start":  2,
		"new_content": "X\nY",
	}
	got, err := tool.insertContent(content, params)
	if err != nil {
		t.Fatalf("insertContent: %v", err)
	}
	want := "a\r\nb\r\nX\r\nY\r\nc\r\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
}

func TestFileEditToolDeleteCRLF(t *testing.T) {
	tool := &FileEditTool{}
	content := "a\r\nb\r\nc\r\nd\r\n"
	params := map[string]interface{}{
		"line_start": 2,
		"line_end":   3,
	}
	got, err := tool.deleteContent(content, params)
	if err != nil {
		t.Fatalf("deleteContent: %v", err)
	}
	want := "a\r\nd\r\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFileEditToolExecuteReplaceCRLF(t *testing.T) {
	tool := &FileEditTool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("line1\r\nline2\r\nline3\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	params := map[string]interface{}{
		"operation":   "replace",
		"path":        path,
		"old_content": "line2",
		"new_content": "lineTWO",
	}
	if _, err := tool.Execute(context.Background(), params); err != nil {
		t.Fatalf("execute: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "lineTWO") {
		t.Errorf("replacement missing: %q", got)
	}
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
}

// ---------------------------------------------------------------------------
// DiffPatchTool on CRLF files
// ---------------------------------------------------------------------------

func TestDiffPatchToolApplyPatchCRLF(t *testing.T) {
	tool := &DiffPatchTool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	original := "line one\nline two\nline three\nline four\n"
	if err := os.WriteFile(path, []byte(convertLineEndings(original, "\r\n")), 0644); err != nil {
		t.Fatal(err)
	}

	// old_text 使用 LF 行尾，匹配 CRLF 文件
	params := map[string]interface{}{
		"action": "apply_patch",
		"path":   path,
		"patches": []interface{}{
			map[string]interface{}{
				"old_text": "line one\nline two",
				"new_text": "line ONE\nline two",
			},
		},
	}
	if _, err := tool.Execute(context.Background(), params); err != nil {
		t.Fatalf("applyPatch: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "line ONE") {
		t.Errorf("patch not applied: %q", got)
	}
	// 写回保持 CRLF，无混合行尾
	if !strings.Contains(got, "\r\n") {
		t.Errorf("CRLF lost: %q", got)
	}
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
}

func TestDiffPatchToolShowDiffCRLF(t *testing.T) {
	tool := &DiffPatchTool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("line1\r\nline2\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	params := map[string]interface{}{
		"action":      "show_diff",
		"path":        path,
		"new_content": "line1\nline2 changed\n",
	}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("showDiff: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", res)
	}
	diff, _ := m["diff"].(string)
	if strings.Contains(diff, "\r") {
		t.Errorf("diff contains raw CR: %q", diff)
	}
	if !strings.Contains(diff, "line2 changed") {
		t.Errorf("diff missing change: %q", diff)
	}
}

// ---------------------------------------------------------------------------
// BatchFileOpsTool on CRLF files
// ---------------------------------------------------------------------------

func TestBatchSearchReplaceCRLF(t *testing.T) {
	tool := &BatchFileOpsTool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello world\r\nsecond line\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	params := map[string]interface{}{
		"operation": "batch_search_replace",
		"operations": []interface{}{
			map[string]interface{}{
				"path":     path,
				"old_text": "hello world",
				"new_text": "hi there",
			},
		},
	}
	if _, err := tool.Execute(context.Background(), params); err != nil {
		t.Fatalf("batchSearchReplace: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "hi there") {
		t.Errorf("replacement missing: %q", got)
	}
	if !strings.Contains(got, "\r\n") {
		t.Errorf("CRLF lost: %q", got)
	}
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
}

func TestBatchSearchReplaceOldTextNotFoundCRLF(t *testing.T) {
	tool := &BatchFileOpsTool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	params := map[string]interface{}{
		"operation": "batch_search_replace",
		"operations": []interface{}{
			map[string]interface{}{
				"path":     path,
				"old_text": "not present",
				"new_text": "x",
			},
		},
	}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("batchSearchReplace: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", res)
	}
	results := m["results"].(map[string]interface{})
	entry := results[path].(map[string]interface{})
	if msg, _ := entry["error"].(string); !strings.Contains(msg, "not found") {
		t.Errorf("expected not-found error, got: %v", entry)
	}
	// 文件不应被修改
	data, _ := os.ReadFile(path)
	if string(data) != "hello\r\n" {
		t.Errorf("file should be unchanged: %q", string(data))
	}
}
