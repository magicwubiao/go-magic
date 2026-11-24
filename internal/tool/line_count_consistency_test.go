package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 三个入口对同一个文件必须报同一个行数。历史问题：read_file=3、
// batch_read=4、file_edit=4（末尾换行被当成额外一行）。
func TestLineCountConsistency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkDir(context.Background(), dir)

	readRes, err := (&ReadFileTool{}).Execute(ctx, map[string]interface{}{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	readTotal := readRes.(map[string]interface{})["total"]

	batchRes, err := NewBatchFileOpsTool().Execute(ctx, map[string]interface{}{
		"operation": "batch_read",
		"files":     []interface{}{path},
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := batchRes.(map[string]interface{})["results"].(map[string]interface{})[path].(map[string]interface{})

	editRes, err := NewFileEditTool().Execute(ctx, map[string]interface{}{
		"path":        path,
		"operation":   "replace",
		"old_content": "two",
		"new_content": "TWO",
	})
	if err != nil {
		t.Fatal(err)
	}

	if readTotal != 3 {
		t.Fatalf("read_file total: got %v, want 3", readTotal)
	}
	if entry["total"] != readTotal {
		t.Fatalf("batch_read total: got %v, want %v (same as read_file)", entry["total"], readTotal)
	}
	if editRes.(map[string]interface{})["total_lines"] != readTotal {
		t.Fatalf("file_edit total_lines: got %v, want %v", editRes.(map[string]interface{})["total_lines"], readTotal)
	}
}

// 改 1 行必须报 add=1/remove=1，且 hunk 头不出现幻影行。
func TestDiffCountsSingleLineChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := NewDiffPatchTool().Execute(context.Background(), map[string]interface{}{
		"action":      "show_diff",
		"path":        path,
		"new_content": "one\nTWO\nthree\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]interface{})
	if m["additions"] != 1 || m["removals"] != 1 {
		t.Fatalf("additions/removals: got %v/%v, want 1/1\ndiff:\n%v", m["additions"], m["removals"], m["diff"])
	}
	diff := m["diff"].(string)
	if strings.Contains(diff, "@@ -1,4 +1,4 @@") {
		t.Fatalf("hunk header still counts the phantom trailing line:\n%s", diff)
	}
	if !strings.Contains(diff, "@@ -1,3 +1,3 @@") {
		t.Fatalf("unexpected hunk header:\n%s", diff)
	}
}

// 空文件与"仅一个换行"的边界：行数分别为 0 和 1。
func TestSplitContentLinesEdgeCases(t *testing.T) {
	cases := map[string]int{
		"":         0,
		"a":        1,
		"a\n":      1,
		"a\nb":     2,
		"a\n\n":    2,
		"\n":       1,
		"a\r\nb\n": 2,
	}
	for content, want := range cases {
		if got := len(splitContentLines(normalizeLineEndings(content))); got != want {
			t.Errorf("splitContentLines(%q) = %d lines, want %d", content, got, want)
		}
	}
}
