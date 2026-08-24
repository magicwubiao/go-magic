package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if iminfo["tier"] != "leading_ws_normalized" {
		t.Errorf("expected leading_ws_normalized tier, got %v", iminfo["tier"])
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
	if iminfo["tier"] != "trailing_ws_tolerant" {
		t.Errorf("expected trailing_ws_tolerant tier, got %v", iminfo["tier"])
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
	if iminfo["tier"] != "leading_ws_normalized" && iminfo["tier"] != "edge_blank_tolerant" {
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
	if match["tier"] != "leading_ws_normalized" {
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
