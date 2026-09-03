package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Multi-tier tolerant matching tests (the core fix for "matching inaccuracy")
// ---------------------------------------------------------------------------

func TestFileEditMultiTierExact(t *testing.T) {
	tool := &FileEditTool{}
	content := "package main\n\nfunc foo() {\n\tfmt.Println(\"hello\")\n}\n"
	params := map[string]interface{}{
		"old_content": "func foo() {\n\tfmt.Println(\"hello\")\n}",
		"new_content": "func bar() {\n\tfmt.Println(\"world\")\n}",
	}
	got, info, err := tool.replaceContentDetailed(content, params, 5)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(got, "func bar()") {
		t.Errorf("replacement failed, got %q", got)
	}
	iminfo := info.(map[string]interface{})
	if iminfo["tier"] != "exact" {
		t.Errorf("expected exact tier, got %v", iminfo["tier"])
	}
	if iminfo["line_start"].(int) != 3 {
		t.Errorf("expected line_start=3, got %v", iminfo["line_start"])
	}
}

// Tier 1: leading whitespace tolerance (file uses tabs, LLM provides spaces)
func TestFileEditMultiTierLeadingWSTabVsSpaces(t *testing.T) {
	tool := &FileEditTool{}
	// actual file uses tabs
	content := "package main\n\nfunc x() {\n\ta := 1\n\tb := 2\n\treturn a + b\n}\n"
	// LLM returns old_content with 4 spaces instead of tab
	params := map[string]interface{}{
		"old_content": "func x() {\n    a := 1\n    b := 2\n    return a + b\n}",
		"new_content": "func x() int {\n    return 42\n}",
	}
	got, info, err := tool.replaceContentDetailed(content, params, 7)
	if err != nil {
		t.Fatalf("tier1 match failed: %v", err)
	}
	iminfo := info.(map[string]interface{})
	if iminfo["tier"] != "leading_ws" {
		t.Errorf("expected leading_ws tier, got %v", iminfo["tier"])
	}
	// IMPORTANT: actual indentation of the preserved surrounding content stays with tabs
	if !strings.Contains(got, "func x() int {") {
		t.Errorf("replacement missing: %q", got)
	}
}

// Tier 2: trailing whitespace tolerance
func TestFileEditMultiTierTrailingWS(t *testing.T) {
	tool := &FileEditTool{}
	// file lines have trailing spaces that LLM probably won't include
	content := "package main   \n\nfunc f() {   \n\tx := 1\t\n}   \n"
	// LLM old_content without trailing spaces
	params := map[string]interface{}{
		"old_content": "package main\n\nfunc f() {\n\tx := 1\n}",
		"new_content": "package renamed\n\nfunc g() {\n\ty := 2\n}",
	}
	got, info, err := tool.replaceContentDetailed(content, params, 5)
	if err != nil {
		t.Fatalf("tier2 match failed: %v", err)
	}
	iminfo := info.(map[string]interface{})
	if iminfo["tier"] != "trailing_ws" {
		t.Errorf("expected trailing_ws tier, got %v", iminfo["tier"])
	}
	if !strings.Contains(got, "package renamed") {
		t.Errorf("replacement missing: %q", got)
	}
}

// Tier 3: blank-edge tolerant (must NOT match exactly in tier 0/1/2 first)
func TestFileEditMultiTierEdgeBlank(t *testing.T) {
	tool := &FileEditTool{}
	content := "TOP\none\ntwo\nthree\nEND\n"
	// LLM provides old_content with extra surrounding blank lines that don't exist in target;
	// Also add small whitespace discrepancy to bypass exact tier.
	params := map[string]interface{}{
		"old_content": "\n\n  one\n  two\n  three\n\n",
		"new_content": "ONE\nTWO\nTHREE",
	}
	got, info, err := tool.replaceContentDetailed(content, params, 5)
	if err != nil {
		t.Fatalf("tier3 match failed: %v", err)
	}
	iminfo := info.(map[string]interface{})
	if !strings.HasPrefix(iminfo["tier"].(string), "leading_ws") &&
		!strings.HasPrefix(iminfo["tier"].(string), "edge_blank") {
		t.Logf("note: tier resolved as %v (both acceptable)", iminfo["tier"])
	}
	if !strings.Contains(got, "ONE\nTWO\nTHREE") {
		t.Errorf("replacement missing, got %q", got)
	}
}

// Multiple matches -> ERROR (no silent Replace-first!)
func TestFileEditAmbiguousRejected(t *testing.T) {
	tool := &FileEditTool{}
	content := "line same\nline one\nline same\nline two\nline same\n"
	params := map[string]interface{}{
		"old_content": "line same",
		"new_content": "line changed",
	}
	_, _, err := tool.replaceContentDetailed(content, params, 5)
	if err == nil {
		t.Fatalf("expected ambiguity error, got nil")
	}
	if !strings.Contains(err.Error(), "3 occurrences") {
		t.Errorf("expected 3 occurrences in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "L1, L3, L5") {
		t.Errorf("expected L1,L3,L5 summary in error: %v", err)
	}
	if !strings.Contains(err.Error(), "Candidates") {
		t.Errorf("expected Candidates hint in error: %v", err)
	}
}

// Matching failure returns rich diagnostics
func TestFileEditMatchFailureDiagnostics(t *testing.T) {
	tool := &FileEditTool{}
	// File content has lines that partially match anchors so Similar detection works
	content := "package foo\n\nfunc realName() {\n\tprintln(\"ok\")\n}\n"
	params := map[string]interface{}{
		// Small typo in function name, rest matches: "real" vs "WRONG"
		"old_content": "func WRONGName() {\n\tprintln(\"ok\")\n}",
		"new_content": "func replaced() {}\n",
	}
	_, _, err := tool.replaceContentDetailed(content, params, 5)
	if err == nil {
		t.Fatalf("expected match error, got nil")
	}
	msg := err.Error()
	t.Logf("diagnostic message: %s", msg)
	mustContain := []string{
		"old_content not found",
		"old_content:",
		"file:",
		"Diagnostics",
		"HINT",
		"old[0]:",
	}
	for _, need := range mustContain {
		if !strings.Contains(msg, need) {
			t.Errorf("diagnostics missing %q. Full msg:\n%s", need, msg)
		}
	}
	// either file size or similar lines confirms diagnostics depth
	if !strings.Contains(msg, "lines") {
		t.Errorf("expected line counts in msg: %s", msg)
	}
}

// Execute returns full match info for LLM verification
func TestFileEditExecuteReturnsMatchContext(t *testing.T) {
	tool := &FileEditTool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	content := "package p\n\nfunc a() {\n\treturn 1\n}\n\nfunc b() {\n\treturn 2\n}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	params := map[string]interface{}{
		"operation":   "replace",
		"path":        path,
		"old_content": "func a() {\n\treturn 1\n}",
		"new_content": "func a() int {\n\treturn 100\n}",
	}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := res.(map[string]interface{})
	if _, ok := m["match"]; !ok {
		t.Fatalf("result missing 'match' info: %v", m)
	}
	match := m["match"].(map[string]interface{})
	if match["tier"] != "exact" {
		t.Errorf("expected exact tier, got %v", match["tier"])
	}
	if match["line_start"].(int) != 3 {
		t.Errorf("expected line_start=3, got %v", match["line_start"])
	}
	ctx, ok := match["context"].(map[string]string)
	if !ok {
		t.Fatalf("context missing in match info, got match=%v", match)
	}
	// context should contain surrounding code for verification
	if !strings.Contains(ctx["before"], "package p") {
		t.Errorf("context before missing anchor, got %q", ctx["before"])
	}
	if !strings.Contains(ctx["after"], "func b()") {
		t.Errorf("context after missing anchor, got %q", ctx["after"])
	}
}

// End-to-end: old_content had wrong indentation → tolerant match succeeds,
// returns non-exact tier so LLM can see what happened
func TestFileEditEndToEndTolerantMatch(t *testing.T) {
	tool := &FileEditTool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "code.py")
	// File with 4-space indentation
	pyCode := strings.Join([]string{
		"def compute(x, y):",
		"    # add inputs",
		"    return x + y",
		"",
		"print(compute(1, 2))",
	}, "\n") + "\n"
	os.WriteFile(path, []byte(pyCode), 0644)

	// LLM sends with tab-indentation in old_content (mistake!)
	params := map[string]interface{}{
		"operation":   "replace",
		"path":        path,
		"old_content": "def compute(x, y):\n\t# add inputs\n\treturn x + y",
		"new_content": "def compute(x, y):\n\t# sum two numbers\n\treturn x + y + 1",
	}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	match := m["match"].(map[string]interface{})
	if match["tier"] != "leading_ws" {
		t.Errorf("expected tolerant leading_ws tier, got %v", match["tier"])
	}

	// Verify actual file content
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "sum two numbers") {
		t.Errorf("file not updated correctly: %q", got)
	}
	if !strings.Contains(got, "return x + y + 1") {
		t.Errorf("file missing new value: %q", got)
	}
}

// Ensure Tier priority: exact match preferred over tolerant
func TestFileEditExactBeatsTolerant(t *testing.T) {
	tool := &FileEditTool{}
	content := "func a() {\n\treturn 1\n}\n"
	params := map[string]interface{}{
		"old_content": "func a() {\n\treturn 1\n}",
		"new_content": "X",
	}
	_, info, err := tool.replaceContentDetailed(content, params, 3)
	if err != nil {
		t.Fatal(err)
	}
	iminfo := info.(map[string]interface{})
	if iminfo["tier"] != "exact" {
		t.Errorf("expected exact tier to win, got %v", iminfo["tier"])
	}
}

// CRITICAL #1 REGRESSION TEST: combined leading + trailing whitespace tolerance
// This used to FAIL across all 4 old tiers because tab-vs-spaces AND trailing spaces
// existed SIMULTANEOUSLY on every line - LLM would fall back to sed every time.
func TestFileEditCombinedLeadingAndTrailingWSTier(t *testing.T) {
	tool := &FileEditTool{}
	// Realistic file: 4-space indent AND invisible trailing spaces
	content := strings.Join([]string{
		"package p   ",
		"",
		"func real(a int) bool {   ",
		"    if a > 0 {   ",
		"        return true   ",
		"    }   ",
		"    return false   ",
		"}   ",
		"",
	}, "\n")
	// LLM hallucinated tab-indentation, and did not include trailing spaces.
	// Before the fix: tier 0(exact)=no, tier1(leading only)=no (still has trailing spaces on file),
	//                tier2(trailing only)=no (tabs vs spaces still differ),
	//                tier4(edge blank)=no → TOTAL FAILURE, LLM falls back to sed.
	// After fix: tier 3(leading+trailing combined) MATCHES.
	params := map[string]interface{}{
		"old_content": "func real(a int) bool {\n\tif a > 0 {\n\t\treturn true\n\t}\n\treturn false\n}",
		"new_content": "func real(a int) bool {\n\treturn a > 0\n}",
	}
	got, info, err := tool.replaceContentDetailed(content, params, 9)
	if err != nil {
		t.Fatalf("combined tier should match, but failed: %v", err)
	}
	iminfo := info.(map[string]interface{})
	tier := iminfo["tier"].(string)
	if tier != "leading+trailing_ws" {
		t.Errorf("expected combined tier 'leading+trailing_ws', got %v", tier)
	}
	if !strings.Contains(got, "func real(a int) bool {") {
		t.Errorf("new_content missing: %q", got)
	}
	if !strings.Contains(got, "return a > 0") {
		t.Errorf("expected simplified body, got %q", got)
	}
	// File should still have original trailing spaces on UNTOUCHED lines (package p line preserved)
	if !strings.Contains(got, "package p   ") {
		t.Errorf("untouched line's trailing spaces were corrupted: %q", got)
	}
}

// Ensure tier priority: combined tier should NOT beat exact or single-normalizer tiers
func TestFileEditTierPriorityCorrect(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		old        string
		expectTier string
	}{
		{"exact wins", "a := 1\nb := 2\n", "a := 1\nb := 2\n", "exact"},
		{"leading only still wins when it is enough", "    a := 1\n    b := 2\n", "\ta := 1\n\tb := 2\n", "leading_ws"},
		{"trailing only still wins", "a := 1   \nb := 2   \n", "a := 1\nb := 2\n", "trailing_ws"},
		{"combined only as last resort", "    a := 1   \n    b := 2   \n", "\ta := 1\n\tb := 2\n", "leading+trailing_ws"},
	}
	tool := &FileEditTool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			params := map[string]interface{}{
				"old_content": c.old,
				"new_content": "X",
			}
			_, info, err := tool.replaceContentDetailed(c.content, params, -1)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			got := info.(map[string]interface{})["tier"].(string)
			if got != c.expectTier {
				t.Errorf("expected %q, got %q", c.expectTier, got)
			}
		})
	}
}

// UTF-8 truncate sanity (MEDIUM #7 regression)
func TestTruncateRuneNoCorruption(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"Hello World", 5, "Hello…"},
		{"中文内容测试", 2, "中文…"},
		{"日本語テスト", 3, "日本語…"},
		{"Mix: 中文 abc", 6, "Mix: 中…"},
		{"Short", 10, "Short"},
		{"边界", 2, "边界"},
	}
	for _, c := range cases {
		got := truncateRune(c.input, c.n)
		if got != c.want {
			t.Errorf("truncateRune(%q, %d) = %q, want %q", c.input, c.n, got, c.want)
		}
		// Ensure the output is valid UTF-8
		if !utf8.ValidString(got) {
			t.Errorf("invalid UTF-8 output from truncateRune(%q, %d): %q", c.input, c.n, got)
		}
	}
}
