package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Binary / content classification
// ---------------------------------------------------------------------------

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"plain ascii", []byte("hello world\nline two\n"), false},
		{"go source", []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), false},
		{"chinese utf8", []byte("package main\n// 你好世界\nfunc main() {}\n"), false},
		{"png with NULs", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...), true},
		{"many nulls", append([]byte("abc"), bytesOf(0, 200)...), true},
		{"single NUL in 8KB text", append([]byte(strings.Repeat("x", 8000)), 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinaryContent(tt.data); got != tt.want {
				t.Errorf("isBinaryContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func bytesOf(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestIsCodeFile(t *testing.T) {
	cases := map[string]bool{
		"foo.go":           true,
		"foo.py":           true,
		"foo.jsx":          true,
		"foo.scss":         true,
		"foo.md":           true,
		"Dockerfile":       true,
		"Makefile":         true,
		"Web.Dockerfile":   true,
		"report.pdf":       false,
		"image.png":        false,
		"lib.so":           false,
		"noext":            false,
	}
	for p, want := range cases {
		if got := isCodeFile(p); got != want {
			t.Errorf("isCodeFile(%q) = %v, want %v", p, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Match finding and replace accuracy
// ---------------------------------------------------------------------------

func TestFindAllMatchesCaseSensitive(t *testing.T) {
	s := "Foo foo FOO foo\nfoo bar Foo"
	matches := findAllMatches(s, "foo", true)
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3: %+v", len(matches), matches)
	}
	// Check exact byte positions of "foo" (lowercase)
	// s = "Foo foo FOO foo\nfoo bar Foo"
	//       0123456789...
	if matches[0].ByteStart != 4 {
		t.Errorf("match[0].start = %d, want 4", matches[0].ByteStart)
	}
	if matches[0].LineStart != 1 || matches[0].LineEnd != 1 {
		t.Errorf("match[0] lines = %+v, want {1,1}", matches[0])
	}
	if matches[2].LineStart != 2 {
		t.Errorf("match[2].LineStart = %d, want 2", matches[2].LineStart)
	}
}

func TestFindAllMatchesCaseInsensitive(t *testing.T) {
	s := "Foo foo FOO"
	matches := findAllMatches(s, "foo", false)
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3", len(matches))
	}
}

func TestReplaceAllExactMulti(t *testing.T) {
	s := "a b a c a"
	out, n := replaceAllExact(s, "a", "X", true, -1)
	if n != 3 {
		t.Errorf("changes = %d, want 3", n)
	}
	if out != "X b X c X" {
		t.Errorf("out = %q", out)
	}
}

func TestReplaceAllExactLimit(t *testing.T) {
	s := "a a a a a"
	out, n := replaceAllExact(s, "a", "X", true, 2)
	if n != 2 {
		t.Errorf("changes = %d, want 2", n)
	}
	if out != "X X a a a" {
		t.Errorf("out = %q", out)
	}
}

// ---------------------------------------------------------------------------
// batchSearchReplace: replace_all + case_sensitive + dry_run + require_unique
// ---------------------------------------------------------------------------

func runBatch(t *testing.T, operation string, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	tool := NewBatchFileOpsTool()
	params := map[string]interface{}{"operation": operation}
	for k, v := range payload {
		params[k] = v
	}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("result not a map: %T", res)
	}
	return m
}

func TestBatchSearchReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.txt")
	if err := os.WriteFile(path, []byte("ab ab ab"), 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_search_replace", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":        path,
				"old_text":    "ab",
				"new_text":    "XY",
				"replace_all": true,
			},
		},
	})
	results := res["results"].(map[string]interface{})
	entry := results[path].(map[string]interface{})
	if entry["changes"] != 3 {
		t.Errorf("changes = %v, want 3", entry["changes"])
	}
	data, _ := os.ReadFile(path)
	if string(data) != "XY XY XY" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestBatchSearchReplaceCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(path, []byte("Apple BANANA apple"), 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_search_replace", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":           path,
				"old_text":       "apple",
				"new_text":       "FRUIT",
				"replace_all":    true,
				"case_sensitive": false,
			},
		},
	})
	entry := res["results"].(map[string]interface{})[path].(map[string]interface{})
	if entry["changes"] != 2 {
		t.Errorf("changes = %v, want 2", entry["changes"])
	}
	data, _ := os.ReadFile(path)
	if string(data) != "FRUIT BANANA FRUIT" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestBatchSearchReplaceDryRunNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.txt")
	orig := "keep keep keep"
	if err := os.WriteFile(path, []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_search_replace", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":        path,
				"old_text":    "keep",
				"new_text":    "changed",
				"replace_all": true,
				"dry_run":     true,
			},
		},
	})
	entry := res["results"].(map[string]interface{})[path].(map[string]interface{})
	if entry["dry_run"] != true {
		t.Errorf("dry_run flag missing")
	}
	if entry["changes"] != 3 {
		t.Errorf("changes = %v, want 3 (reported but not written)", entry["changes"])
	}
	if matches, ok := entry["matches"].([]MatchInfo); !ok || len(matches) != 3 {
		t.Errorf("matches missing or wrong len: %+v", entry["matches"])
	}
	data, _ := os.ReadFile(path)
	if string(data) != orig {
		t.Errorf("dry_run still modified file: %q", string(data))
	}
}

func TestBatchSearchReplaceRequireUniqueFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "u.txt")
	if err := os.WriteFile(path, []byte("same same same"), 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_search_replace", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":           path,
				"old_text":       "same",
				"new_text":       "diff",
				"require_unique": true,
			},
		},
	})
	entry := res["results"].(map[string]interface{})[path].(map[string]interface{})
	if entry["changes"] != 0 {
		t.Errorf("changes should be 0, got %v", entry["changes"])
	}
	if errMsg, _ := entry["error"].(string); !strings.Contains(errMsg, "require_unique") {
		t.Errorf("expected require_unique error, got %v", errMsg)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "same same same" {
		t.Errorf("failed require_unique still modified file: %q", string(data))
	}
}

func TestBatchSearchReplaceAmbiguousWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.txt")
	if err := os.WriteFile(path, []byte("a a a"), 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_search_replace", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":     path,
				"old_text": "a",
				"new_text": "B",
				// replace_all defaults to false
			},
		},
	})
	entry := res["results"].(map[string]interface{})[path].(map[string]interface{})
	if entry["changes"] != 1 {
		t.Errorf("changes = %v, want 1", entry["changes"])
	}
	if warn, _ := entry["warning"].(string); !strings.Contains(warn, "occurrences") {
		t.Errorf("expected ambiguous warning, got %v", warn)
	}
	data, _ := os.ReadFile(path)
	// Only the first "a" should be replaced
	if string(data) != "B a a" {
		t.Errorf("expected only first replaced, got %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// batchSearchReplace CRLF preservation (regression)
// ---------------------------------------------------------------------------

func TestBatchSearchReplaceAllPreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")
	if err := os.WriteFile(path, []byte("ab\r\nab\r\nab\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runBatch(t, "batch_search_replace", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":        path,
				"old_text":    "ab",
				"new_text":    "XY",
				"replace_all": true,
			},
		},
	})
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "XY\r\nXY\r\nXY\r\n") {
		t.Errorf("content / line endings wrong: %q", got)
	}
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
}

// ---------------------------------------------------------------------------
// batchWrite: line-ending preservation + atomicity backup flag
// ---------------------------------------------------------------------------

func TestBatchWritePreservesExistingCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.txt")
	if err := os.WriteFile(path, []byte("old\r\nlines\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runBatch(t, "batch_write", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":    path,
				"content": "new\nlines\n",
			},
		},
	})
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "\r\n") {
		t.Errorf("expected CRLF preserved, got %q", got)
	}
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
}

func TestBatchWriteBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bk.txt")
	orig := []byte("original\n")
	if err := os.WriteFile(path, orig, 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_write", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"path":    path,
				"content": "replaced\n",
				"backup":  true,
			},
		},
	})
	entry := res["results"].(map[string]interface{})[path].(map[string]interface{})
	if entry["backup"] != "bk.txt.bak" {
		t.Errorf("backup marker wrong: %v", entry["backup"])
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("missing backup: %v", err)
	}
	if string(bak) != "original\n" {
		t.Errorf("backup content wrong: %q", string(bak))
	}
}

// ---------------------------------------------------------------------------
// batchRead: binary detection + size classification
// ---------------------------------------------------------------------------

func TestBatchReadDetectsBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	payload := append([]byte("header"), bytesOf(0, 500)...)
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_read", map[string]interface{}{
		"files": []interface{}{path},
	})
	entry := res["results"].(map[string]interface{})[path].(map[string]interface{})
	if errMsg, _ := entry["error"].(string); !strings.Contains(errMsg, "binary") {
		t.Errorf("expected binary error, got entry=%+v", entry)
	}
	if entry["is_binary"] != true {
		t.Errorf("expected is_binary=true, got %v", entry)
	}
}

func TestBatchReadCodeFileMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(path, []byte("package main\nfunc main(){}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := runBatch(t, "batch_read", map[string]interface{}{
		"files": []interface{}{path},
	})
	entry := res["results"].(map[string]interface{})[path].(map[string]interface{})
	if entry["is_code"] != true {
		t.Errorf("expected is_code=true, got %v", entry)
	}
	if entry["is_binary"] != false {
		t.Errorf("expected is_binary=false, got %v", entry)
	}
	if entry["line_ending"] != "\n" {
		t.Errorf("line_ending wrong: %v", entry["line_ending"])
	}
	if entry["total"] != 3 {
		t.Errorf("total lines = %v, want 3", entry["total"])
	}
}