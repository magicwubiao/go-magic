package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The three defects these tests pin down all had the same shape: a path (or a
// language) that looked handled but was not, because the failure was silent.
//
//   - `lint` / `format_code` / `lsp_diagnostics` handed the raw argument to
//     os.Stat/os.ReadFile/exec, so a *relative* path resolved against the server
//     process CWD instead of the session workdir. `lint src/app.ts` then failed
//     with "failed to stat path", and lsp_diagnostics turned the same mistake
//     into a fake "Cannot read file" *diagnostic*.
//   - lsp_diagnostics advertised javascript/typescript/rust/java/c/cpp in its
//     schema but implemented go and python only, so a JS file was answered with
//     "unsupported language: javascript".
//   - LintFile (the post-write hook) only understood .py/.json/.yml/.toml, so
//     Go and JS writes were never syntax-checked at all.

// chdirOutsideWorkDir moves the process CWD somewhere unrelated for the duration
// of the test. Without this the workdir-relative bug is invisible: the test
// process CWD is the package directory, so a relative path happens to resolve to
// something that exists.
func chdirOutsideWorkDir(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mentionsPath reports whether msg contains want, comparing case-insensitively
// on Windows where the same directory can be spelled with different casing.
func mentionsPath(msg, want string) bool {
	if runtime.GOOS == "windows" {
		return strings.Contains(strings.ToLower(msg), strings.ToLower(want))
	}
	return strings.Contains(msg, want)
}

func TestLintToolResolvesRelativePathAgainstWorkDir(t *testing.T) {
	work := t.TempDir()
	badPy := filepath.Join(work, "src", "bad.py")
	mustWriteFile(t, badPy, "def f(:\n    pass\n")
	chdirOutsideWorkDir(t)

	if _, _, ok := pythonSyntaxCheck(badPy); !ok {
		t.Skip("no python interpreter on PATH; cannot assert syntax detection")
	}

	ctx := WithWorkDir(context.Background(), work)
	res, err := NewLintTool().Execute(ctx, map[string]interface{}{"path": "src/bad.py"})
	if err != nil {
		t.Fatalf("lint with a workdir-relative path must work, got error: %v", err)
	}

	results, ok := res.([]LintResult)
	if !ok || len(results) != 1 {
		t.Fatalf("unexpected result shape: %#v", res)
	}
	if len(results[0].Issues) == 0 {
		t.Fatalf("expected a Python syntax error to be reported for %s", results[0].FilePath)
	}
	if !mentionsPath(results[0].FilePath, filepath.Join("src", "bad.py")) {
		t.Errorf("issue file path = %q, want it to point at src%cbad.py", results[0].FilePath, filepath.Separator)
	}
}

// TestLintToolMissingFileErrorNamesResolvedPath checks the failure mode that
// used to be reported as a bare "failed to stat path": the message must show the
// directory that was actually searched so the caller can correct itself.
func TestLintToolMissingFileErrorNamesResolvedPath(t *testing.T) {
	work := t.TempDir()
	chdirOutsideWorkDir(t)
	ctx := WithWorkDir(context.Background(), work)

	_, err := NewLintTool().Execute(ctx, map[string]interface{}{"path": filepath.Join("src", "missing.py")})
	if err == nil {
		t.Fatal("lint on a missing file must fail")
	}
	if !mentionsPath(err.Error(), filepath.Join(work, "src", "missing.py")) {
		t.Errorf("error %q should name the resolved path inside the workdir %q", err, work)
	}
}

func TestFormatToolResolvesRelativePathAgainstWorkDir(t *testing.T) {
	work := t.TempDir()
	chdirOutsideWorkDir(t)
	ctx := WithWorkDir(context.Background(), work)

	_, err := NewFormatTool().Execute(ctx, map[string]interface{}{
		"path": filepath.Join("src", "missing.py"), "language": "python",
	})
	if err == nil {
		t.Fatal("format_code on a missing file must fail")
	}
	if !mentionsPath(err.Error(), filepath.Join(work, "src", "missing.py")) {
		t.Errorf("error %q should name the resolved path inside the workdir %q", err, work)
	}
}

func TestLSPDiagnosticsJavaScriptAndRelativePath(t *testing.T) {
	work := t.TempDir()
	broken := filepath.Join(work, "src", "broken.js")
	clean := filepath.Join(work, "src", "app.js")
	mustWriteFile(t, broken, "const a = ;\n")
	mustWriteFile(t, clean, "export const a = 1;\n")
	chdirOutsideWorkDir(t)

	if _, _, ok := nodeCheckSyntax(broken); !ok {
		t.Skip("node is not on PATH; cannot assert JavaScript diagnostics")
	}

	ctx := WithWorkDir(context.Background(), work)
	tool := NewLSPDiagnosticTool("")

	res, err := tool.Execute(ctx, map[string]interface{}{"file_path": filepath.Join("src", "broken.js")})
	if err != nil {
		t.Fatalf("lsp_diagnostics on a workdir-relative path must work, got error: %v", err)
	}
	r, ok := res.(*LSPDiagnosticResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if r.Language != "javascript" {
		t.Errorf("language = %q, want javascript (auto-detected from .js)", r.Language)
	}
	if len(r.Diagnostics) == 0 {
		t.Fatalf("expected a syntax error for %s", broken)
	}
	if r.Diagnostics[0].Line == 0 {
		t.Errorf("diagnostic carries no line number: %#v", r.Diagnostics[0])
	}
	for _, d := range r.Diagnostics {
		// The old code turned an unreadable path into this diagnostic, so a typo
		// looked like a code error.
		if strings.Contains(d.Message, "Cannot read file") {
			t.Errorf("a path problem must not be reported as a diagnostic: %#v", d)
		}
	}

	// A clean file must produce no diagnostics rather than an error.
	resClean, err := tool.Execute(ctx, map[string]interface{}{"file_path": filepath.Join("src", "app.js")})
	if err != nil {
		t.Fatalf("lsp_diagnostics on a valid file must not error: %v", err)
	}
	if got := resClean.(*LSPDiagnosticResult); len(got.Diagnostics) != 0 {
		t.Errorf("valid file reported diagnostics: %#v", got.Diagnostics)
	}
}

func TestLSPDiagnosticsRejectsUnsupportedLanguageExplicitly(t *testing.T) {
	work := t.TempDir()
	js := filepath.Join(work, "app.js")
	mustWriteFile(t, js, "const a = 1;\n")
	chdirOutsideWorkDir(t)
	ctx := WithWorkDir(context.Background(), work)

	tool := NewLSPDiagnosticTool("")
	_, err := tool.Execute(ctx, map[string]interface{}{"file_path": "app.js", "language": "rust"})
	if err == nil {
		t.Fatal("an unimplemented language must return an error, not an all-clear")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("error should say the language is unsupported: %v", err)
	}
	// The message must list what *is* available so the agent stops retrying.
	for _, lang := range supportedDiagnosticLanguages {
		if !strings.Contains(err.Error(), lang) {
			t.Errorf("error %q should list supported language %q", err, lang)
		}
	}
}

func TestLSPDiagnosticsPathErrors(t *testing.T) {
	work := t.TempDir()
	mustWriteFile(t, filepath.Join(work, "src", "app.js"), "const a = 1;\n")
	chdirOutsideWorkDir(t)
	ctx := WithWorkDir(context.Background(), work)
	tool := NewLSPDiagnosticTool("")

	if _, err := tool.Execute(ctx, map[string]interface{}{"file_path": "src"}); err == nil ||
		!strings.Contains(err.Error(), "directory") {
		t.Errorf("a directory argument should be rejected with a clear message, got: %v", err)
	}

	if _, err := tool.Execute(ctx, map[string]interface{}{"file_path": filepath.Join("src", "nope.js")}); err == nil ||
		!mentionsPath(err.Error(), "nope.js") {
		t.Errorf("a missing file should fail and name the path, got: %v", err)
	}
}

func TestLSPGoDiagnosticsReportsRealSyntaxErrors(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}
	work := t.TempDir()
	broken := filepath.Join(work, "broken.go")
	clean := filepath.Join(work, "clean.go")
	mustWriteFile(t, broken, "package main\n\nfunc main() {\n\tx := \n\t_ = x\n}\n")
	mustWriteFile(t, clean, "package main\n\nfunc main() {}\n")
	chdirOutsideWorkDir(t)

	ctx := WithWorkDir(context.Background(), work)
	tool := NewLSPDiagnosticTool("")

	res, err := tool.Execute(ctx, map[string]interface{}{"file_path": "broken.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := res.(*LSPDiagnosticResult)
	if len(r.Diagnostics) == 0 {
		t.Fatal("expected a Go syntax error diagnostic")
	}
	if r.Diagnostics[0].Line == 0 {
		t.Errorf("diagnostic carries no line number: %#v", r.Diagnostics[0])
	}

	resClean, err := tool.Execute(ctx, map[string]interface{}{"file_path": "clean.go"})
	if err != nil {
		t.Fatalf("unexpected error on a valid file: %v", err)
	}
	if got := resClean.(*LSPDiagnosticResult); len(got.Diagnostics) != 0 {
		t.Errorf("valid Go file reported diagnostics: %#v", got.Diagnostics)
	}
}

// TestLintFilePostWriteCoverage pins which extensions the per-write hook checks
// and, just as importantly, that .ts stays out of it: type-checking a project
// costs ~20s, which must not be paid on every write.
func TestLintFilePostWriteCoverage(t *testing.T) {
	dir := t.TempDir()

	if _, err := exec.LookPath("gofmt"); err == nil {
		goBad := filepath.Join(dir, "bad.go")
		mustWriteFile(t, goBad, "package main\n\nfunc main() {\n")
		issues, err := LintFile(goBad)
		if err != nil {
			t.Errorf("LintFile(%s) returned error: %v", goBad, err)
		}
		if len(issues) == 0 {
			t.Errorf("LintFile(%s) reported no issues for a file that does not parse", goBad)
		}
	}

	jsBad := filepath.Join(dir, "bad.js")
	mustWriteFile(t, jsBad, "const a = ;\n")
	if _, _, ok := nodeCheckSyntax(jsBad); ok {
		issues, err := LintFile(jsBad)
		if err != nil {
			t.Errorf("LintFile(%s) returned error: %v", jsBad, err)
		}
		if len(issues) == 0 {
			t.Errorf("LintFile(%s) reported no issues for a file that does not parse", jsBad)
		}
	}

	badJSON := filepath.Join(dir, "bad.json")
	mustWriteFile(t, badJSON, "{ \"a\": }\n")
	if issues, err := LintFile(badJSON); err != nil || len(issues) == 0 {
		t.Errorf("LintFile(%s) = %v, %v; want a JSON syntax error", badJSON, issues, err)
	}

	tsBad := filepath.Join(dir, "bad.ts")
	mustWriteFile(t, tsBad, "const a: number = ;\n")
	if issues, err := LintFile(tsBad); err != nil || len(issues) != 0 {
		t.Errorf("LintFile must stay silent for .ts (lsp_diagnostics covers it), got %v, %v", issues, err)
	}
}

// TestFindProjectRootWalksUp covers the resolution used to decide which
// tsconfig/vue-tsc to run.
func TestFindProjectRootWalksUp(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), "{}\n")
	deep := filepath.Join(root, "a", "b", "c", "file.ts")
	mustWriteFile(t, deep, "export const a = 1;\n")

	if got, want := findProjectRoot(deep), filepath.Clean(root); got != want {
		t.Errorf("findProjectRoot(%q) = %q, want %q", deep, got, want)
	}

	// A nearer tsconfig wins over a package.json further up.
	nested := filepath.Join(root, "a")
	mustWriteFile(t, filepath.Join(nested, "tsconfig.json"), "{}\n")
	if got, want := findProjectRoot(deep), nested; got != want {
		t.Errorf("findProjectRoot(%q) = %q, want the nearer %q", deep, got, want)
	}

	// Isolated temp dirs have no marker; only assert the lookup terminates.
	if got := findProjectRoot(filepath.Join(t.TempDir(), "x.ts")); got != "" {
		t.Logf("found project root %q above a bare temp file", got)
	}
}

// TestParseTSProjectDiagnostics pins tsc's output contract without paying the
// ~20s of a real project typecheck, and pins the filtering rule: tsc always
// reports the whole project, but lsp_diagnostics must return only the requested
// file's findings.
func TestParseTSProjectDiagnostics(t *testing.T) {
	out := strings.Join([]string{
		"src/other.ts(4,7): error TS2322: Type 'string' is not assignable to type 'number'.",
		"src/main.ts(12,3): error TS2554: Expected 1 arguments, but got 0.",
		"",
		"Found 2 errors in 2 files.",
	}, "\n")

	all := parseTSDiagnostics(out)
	if len(all) != 2 {
		t.Fatalf("parsed %d diagnostics, want 2: %#v", len(all), all)
	}

	root := filepath.Join("proj", "root")
	target := filepath.Join(root, "src", "main.ts")
	kept := 0
	for _, d := range all {
		if !sameFile(d.File, target, root) {
			continue
		}
		kept++
		if d.Line != 12 || d.Column != 3 {
			t.Errorf("diagnostic position = %d:%d, want 12:3", d.Line, d.Column)
		}
		if d.Severity != "error" {
			t.Errorf("severity = %q, want error", d.Severity)
		}
		if !strings.Contains(d.Message, "TS2554") {
			t.Errorf("message should carry the TS code, got %q", d.Message)
		}
	}
	if kept != 1 {
		t.Errorf("kept %d diagnostics for %s, want exactly the 1 belonging to it", kept, target)
	}
}
