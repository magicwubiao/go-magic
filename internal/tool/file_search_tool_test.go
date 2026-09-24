package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// searchable 覆盖 FileSearchTool 与 SearchInFilesTool 两个入口。
type searchable interface {
	Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// newSearchFixture 造一组覆盖换行风格 / 超长行 / 子目录 / 相邻匹配的样本。
// 每个文件都带一行 "// zzmark"，便于按文件名集合精确断言 file_pattern 的过滤结果。
func newSearchFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("crlf.go", "package foo\r\n\r\nfunc Alpha() {}\r\nvar Beta = 1\r\n// zzmark\r\n")
	write("lf.go", "package foo\n\nfunc Alpha() {}\nvar Beta = 1\n// zzmark\n")
	write("alt.go", "package x\n\nfunc Foo() {}\nfunc Bar() {}\n// zzmark\n")
	write("sym.go", "package x\n// @foo decorator\n// zzmark\n")
	write("plain.go", "package x\n// NEEDLE base\n// zzmark\n")
	write("plain.txt", "zzmark\r\nNEEDLE in txt\r\n")
	// 单行超过 bufio.Scanner 默认的 64KB 上限，旧实现会整文件静默跳过。
	write("long.txt", strings.Repeat("x", 70*1024)+"NEEDLE"+"\nzzmark\n")
	// 相邻匹配：旧实现的上下文去重会把标记行整段丢掉。
	write("adj.txt", "l1\nNEEDLE one\nNEEDLE two\nNEEDLE three\nl5\nl6\nzzmark\n")
	// 孤立 \r（经典 Mac 换行）。
	write("cr.txt", "first\rNEEDLE lone cr\rthird\rzzmark")
	write("sub/deep.go", "package deep\n// MATCHME\n// zzmark\n")
	write("docs/readme.md", "MATCHME in md\nzzmark\n")
	return dir
}

func runSearch(t *testing.T, tl searchable, dir string, params map[string]interface{}) *SearchResult {
	t.Helper()
	p := make(map[string]interface{}, len(params)+1)
	for k, v := range params {
		p[k] = v
	}
	if _, ok := p["path"]; !ok {
		p["path"] = dir
	}
	out, err := tl.Execute(context.Background(), p)
	if err != nil {
		t.Fatalf("search %v: unexpected error: %v", params, err)
	}
	res, ok := out.(*SearchResult)
	if !ok {
		t.Fatalf("search %v: unexpected result type %T", params, out)
	}
	return res
}

// matchedRel 返回命中的文件相对 dir 的路径集合。
func matchedRel(res *SearchResult, dir string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range res.Matches {
		rel, err := filepath.Rel(dir, m.File)
		if err != nil {
			rel = m.File
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func assertFiles(t *testing.T, res *SearchResult, dir string, want []string) {
	t.Helper()
	got := matchedRel(res, dir)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matched files = %v, want %v (hint: %s)", got, want, res.Hint)
	}
}

// TestFileSearchLineEndings 固化换行风格行为：CRLF 与 LF 完全等价，
// 且返回的 Content 不带尾部 CR；孤立的 CR 也当换行处理。
func TestFileSearchLineEndings(t *testing.T) {
	dir := newSearchFixture(t)

	for _, name := range []string{"crlf.go", "lf.go"} {
		res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
			"pattern": "alpha", "file_pattern": name,
		})
		if res.TotalMatches != 1 {
			t.Fatalf("%s: got %d matches, want 1", name, res.TotalMatches)
		}
		if got := res.Matches[0].Content; got != "func Alpha() {}" {
			t.Fatalf("%s: content = %q", name, got)
		}
		if res.Matches[0].Line != 3 {
			t.Fatalf("%s: line = %d, want 3", name, res.Matches[0].Line)
		}
	}

	// CRLF 下 $ 必须落在行尾（尾部 CR 已剥离），否则 EOL 锚点永远不生效。
	res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": `Alpha\(\) \{\}$`, "use_regex": true, "file_pattern": "crlf.go",
	})
	if res.TotalMatches != 1 {
		t.Fatalf("CRLF $ anchor: got %d matches, want 1", res.TotalMatches)
	}

	// 孤立 CR：整文件不能被当成一行。
	res = runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "NEEDLE", "file_pattern": "cr.txt",
	})
	if res.TotalMatches != 1 || res.Matches[0].Line != 2 || res.Matches[0].Column != 1 {
		t.Fatalf("lone CR: got %+v, want line 2 col 1", res.Matches)
	}
}

// TestFileSearchLongLine 超长行（>64KB）必须仍能匹配。
// 旧实现用 bufio.Scanner 默认 64KB 上限，会返回 ErrTooLong 并把整个文件当成
// "读不了"静默跳过 —— 压缩过的 JS/JSON、单行大日志全部搜不到。
func TestFileSearchLongLine(t *testing.T) {
	dir := newSearchFixture(t)
	res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "NEEDLE", "file_pattern": "long.txt",
	})
	if res.TotalMatches != 1 {
		t.Fatalf("long line: got %d matches, want 1", res.TotalMatches)
	}
	if res.Matches[0].Line != 1 {
		t.Fatalf("long line: line = %d, want 1", res.Matches[0].Line)
	}
}

// TestFileSearchCaseSensitivity 大小写默认不敏感，case_sensitive=true 才敏感。
func TestFileSearchCaseSensitivity(t *testing.T) {
	dir := newSearchFixture(t)

	if res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "ALPHA", "file_pattern": "lf.go",
	}); res.TotalMatches != 1 {
		t.Fatalf("default should be case-insensitive, got %d matches", res.TotalMatches)
	}
	if res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "alpha", "case_sensitive": true, "file_pattern": "lf.go",
	}); res.TotalMatches != 0 {
		t.Fatalf("case_sensitive=true should not match lowercase, got %d", res.TotalMatches)
	}
	if res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "Alpha", "case_sensitive": true, "file_pattern": "lf.go",
	}); res.TotalMatches != 1 {
		t.Fatalf("case_sensitive=true should match 'Alpha', got %d", res.TotalMatches)
	}
}

// TestFileSearchFilePattern file_pattern 需要同时支持文件名与相对路径两种语义。
// 旧实现只做 filepath.Match(pattern, d.Name())，带目录的模式必然全部落空。
func TestFileSearchFilePattern(t *testing.T) {
	dir := newSearchFixture(t)

	allGo := []string{"alt.go", "crlf.go", "lf.go", "plain.go", "sub/deep.go", "sym.go"}
	tests := []struct {
		pattern string
		want    []string
	}{
		{"*.go", allGo},                         // 不含 "/"：按文件名匹配，任意层级
		{"**/*.go", allGo},                      // "**" 跨层
		{"sub/*.go", []string{"sub/deep.go"}},   // 含 "/"：按相对路径匹配
		{"**/deep.go", []string{"sub/deep.go"}}, // "**/" 前缀
		{"docs/*", []string{"docs/readme.md"}},  // 子目录
		{"*.{go,md}", append(append([]string{}, allGo...), "docs/readme.md")}, // 花括号分组
		{"*.txt", []string{"adj.txt", "cr.txt", "long.txt", "plain.txt"}},
	}

	for _, tc := range tests {
		res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
			"pattern": "zzmark", "file_pattern": tc.pattern, "max_results": float64(200),
		})
		sort.Strings(tc.want)
		assertFiles(t, res, dir, tc.want)
		if res.TotalFiles != len(tc.want) {
			t.Fatalf("file_pattern %q: total_files = %d, want %d", tc.pattern, res.TotalFiles, len(tc.want))
		}
	}
}

// TestFileSearchWholeWord whole_word 只在模式端点为单词字符时补 \b。
// 旧实现无条件两侧加 \b，导致 "@foo"、"#tag" 这类模式永远匹配不上。
func TestFileSearchWholeWord(t *testing.T) {
	dir := newSearchFixture(t)

	tests := []struct {
		pattern string
		want    int
	}{
		{"@foo", 1}, // 端点非单词字符：不应被 \b 卡死
		{"foo", 1},  // 纯单词：两侧都要有边界
		{"oo", 0},   // "foo" 内部的 "oo" 不构成整词
		{"zzmark", 1},
	}
	for _, tc := range tests {
		res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
			"pattern": tc.pattern, "whole_word": true, "file_pattern": "sym.go",
		})
		if res.TotalMatches != tc.want {
			t.Fatalf("whole_word %q: got %d matches, want %d", tc.pattern, res.TotalMatches, tc.want)
		}
	}
}

// TestFileSearchContextLines 每个匹配的上下文都必须带上 "<<< MATCH" 标记，
// 相邻行的多个匹配也不例外（旧实现去重时会把整段上下文丢掉）。
func TestFileSearchContextLines(t *testing.T) {
	dir := newSearchFixture(t)
	res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "NEEDLE", "file_pattern": "adj.txt", "context_lines": 2,
	})
	if res.TotalMatches != 3 {
		t.Fatalf("got %d matches, want 3", res.TotalMatches)
	}
	for i, m := range res.Matches {
		if len(m.Context) == 0 {
			t.Fatalf("match %d (%q) has empty context", i, m.Content)
		}
		marked := false
		for _, line := range m.Context {
			if strings.Contains(line, "<<< MATCH") {
				marked = true
			}
		}
		if !marked {
			t.Fatalf("match %d (%q) context lost the MATCH marker: %v", i, m.Content, m.Context)
		}
	}
}

// TestFileSearchParamCoercion 参数类型必须容错：int / json.Number / "true" 字符串
// 都不能被静默吃掉（旧实现只认 float64 和 bool 字面量）。
func TestFileSearchParamCoercion(t *testing.T) {
	dir := newSearchFixture(t)

	// context_lines 传 int（Go 直接构造 map 时的自然类型）
	res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "NEEDLE", "file_pattern": "adj.txt", "context_lines": 2,
	})
	if len(res.Matches) == 0 || len(res.Matches[0].Context) == 0 {
		t.Fatalf("int context_lines was ignored: %+v", res.Matches)
	}

	// max_results 传 json.Number
	res = runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "NEEDLE", "file_pattern": "adj.txt", "max_results": json.Number("2"),
	})
	if res.TotalMatches != 2 {
		t.Fatalf("json.Number max_results: got %d matches, want 2", res.TotalMatches)
	}

	// use_regex 传字符串 "true"
	res = runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "Foo|Bar", "file_pattern": "alt.go", "use_regex": "true",
	})
	if res.TotalMatches != 2 || !res.UseRegex {
		t.Fatalf("string use_regex: got %d matches (use_regex=%v), want 2", res.TotalMatches, res.UseRegex)
	}
}

// TestFileSearchEmptyResultHint 零匹配必须能自解释：回显生效选项 + 给出提示。
func TestFileSearchEmptyResultHint(t *testing.T) {
	dir := newSearchFixture(t)

	// 纯文本模式下 "Foo|Bar" 是字面量，搜不到；提示要点明 use_regex。
	res := runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "Foo|Bar", "file_pattern": "alt.go",
	})
	if res.TotalMatches != 0 {
		t.Fatalf("plain-text 'Foo|Bar' should not match, got %d", res.TotalMatches)
	}
	if !strings.Contains(res.Hint, "use_regex=true") {
		t.Fatalf("hint should mention use_regex, got %q", res.Hint)
	}
	if res.UseRegex {
		t.Fatalf("use_regex should echo false, got %v", res.UseRegex)
	}
	if res.FilesScanned != 1 {
		t.Fatalf("files_scanned = %d, want 1", res.FilesScanned)
	}

	// file_pattern 落空时提示要点名它。
	res = runSearch(t, &FileSearchTool{}, dir, map[string]interface{}{
		"pattern": "zzmark", "file_pattern": "*.rs",
	})
	if res.TotalMatches != 0 || !strings.Contains(res.Hint, "file_pattern") {
		t.Fatalf("hint should mention file_pattern, got %q", res.Hint)
	}
	if res.FilesScanned != 0 {
		t.Fatalf("files_scanned = %d, want 0", res.FilesScanned)
	}
}

// TestSearchInFilesToolPassthrough search_in_files 是面向调用方的入口，
// 参数必须原样透传并做同样的类型容错。
func TestSearchInFilesToolPassthrough(t *testing.T) {
	dir := newSearchFixture(t)

	res := runSearch(t, &SearchInFilesTool{}, dir, map[string]interface{}{
		"pattern": "Foo|Bar", "file_pattern": "alt.go", "use_regex": "true", "context_lines": 1,
	})
	if res.TotalMatches != 2 {
		t.Fatalf("got %d matches, want 2", res.TotalMatches)
	}
	if !res.UseRegex {
		t.Fatalf("use_regex not passed through: %+v", res)
	}
	if len(res.Matches) == 0 || len(res.Matches[0].Context) == 0 {
		t.Fatalf("int context_lines not passed through: %+v", res.Matches)
	}

	// pattern 缺失仍然报错。
	if _, err := (&SearchInFilesTool{}).Execute(context.Background(), map[string]interface{}{"path": dir}); err == nil {
		t.Fatal("expected error for missing pattern")
	}
}
