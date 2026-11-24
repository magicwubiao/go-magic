package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeNamedTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runLint(t *testing.T, path string) []LintResult {
	t.Helper()
	ctx := WithWorkDir(context.Background(), filepath.Dir(path))
	res, err := NewLintTool().Execute(ctx, map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("lint %s: %v", path, err)
	}
	results, ok := res.([]LintResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	return results
}

// 没有内置检查器的类型必须明说，不能返回一个"看起来通过"的 success。
func TestLintUnsupportedTypeIsExplicit(t *testing.T) {
	p := writeNamedTemp(t, "App.vue", "<template><div/></template>\n")
	results := runLint(t, p)
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Checker != "" {
		t.Fatalf("no checker should be reported for .vue, got %q", r.Checker)
	}
	if !strings.Contains(r.Note, "no built-in syntax checker") {
		t.Fatalf("expected an explicit note, got %q", r.Note)
	}
}

// 真的跑了检查器时要报告检查器名。
func TestLintReportsChecker(t *testing.T) {
	p := writeNamedTemp(t, "ok.go", "package main\n\nfunc main() {}\n")
	results := runLint(t, p)
	if results[0].Checker == "" {
		t.Skip("gofmt not installed in this environment")
	}
	if results[0].Checker != "gofmt -e" {
		t.Fatalf("checker: got %q", results[0].Checker)
	}
}

func TestLintFindsSyntaxErrors(t *testing.T) {
	p := writeNamedTemp(t, "bad.json", `{"a": }`)
	results := runLint(t, p)
	if len(results[0].Issues) == 0 {
		t.Fatal("invalid JSON must produce an issue")
	}
	if results[0].Success {
		t.Fatal("Success must be false when issues exist")
	}
}

// info_type=os 的 version 曾是 Go 运行时版本，语义易混淆。
func TestSystemOSReportsGoVersionSeparately(t *testing.T) {
	res, err := NewSystemInfoTool().Execute(context.Background(), map[string]interface{}{"info_type": "os"})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if _, exists := m["version"]; exists {
		t.Fatalf("ambiguous 'version' key must be gone: %v", m)
	}
	if v, _ := m["go_version"].(string); !strings.HasPrefix(v, "go") {
		t.Fatalf("go_version: got %v", m["go_version"])
	}
	if _, ok := m["os_version"]; !ok {
		t.Fatalf("os_version missing: %v", m)
	}
}

// 匹配标记必须跟着文件类型选注释符，非代码文件用裸标记。
func TestMatchMarkerByFileType(t *testing.T) {
	cases := map[string]string{
		"a.go":   "  // <<< MATCH",
		"a.py":   "  # <<< MATCH",
		"a.sql":  "  -- <<< MATCH",
		"a.html": "  <!-- <<< MATCH -->",
		"a.md":   "  <<< MATCH",
		"a.json": "  <<< MATCH",
		"a.txt":  "  <<< MATCH",
	}
	for file, want := range cases {
		if got := matchMarker(file); got != want {
			t.Errorf("matchMarker(%q) = %q, want %q", file, got, want)
		}
	}
}
