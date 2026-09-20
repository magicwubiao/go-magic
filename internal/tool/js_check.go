package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// JavaScript / TypeScript diagnostics.
//
// Two very different costs are needed in this codebase, so they are separate
// functions rather than one "check this file" entry point:
//
//   - nodeCheckSyntax: ~100ms, spawns `node --check`. Used by the post-write
//     hook (LintFile goes through it via lintJavaScript) where a per-write
//     budget of a few hundred ms is the limit.
//   - runTSProjectCheck: seconds (measured ~20s for the 1000-file web/ project),
//     runs the project's own tsc/vue-tsc with its tsconfig. Only ever called
//     from the explicit lsp_diagnostics tool.
//
// Both are deliberately false-positive-averse: a checker that invents errors is
// worse than no checker, because the agent will "fix" code that was never
// broken. Anything that cannot be verified (no node, no tsconfig, no tsc
// installed) is reported as "not available", never as an issue.

// nodeCheckSyntax runs `node --check` on a file, which parses it without
// executing it. It reports (line, message) of the first syntax error.
//
// ok is false when no usable node runtime was found — callers must treat that
// as "checker unavailable" rather than "file is fine".
func nodeCheckSyntax(path string) (line int, message string, ok bool) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return 0, "", false
	}

	cmd := exec.Command(nodeBin, "--check", path)
	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout

	if err := cmd.Run(); err == nil {
		return 0, "", true // parsed cleanly
	}

	out := stderr.String()
	if strings.TrimSpace(out) == "" {
		out = stdout.String()
	}
	line, message = parseNodeCheckOutput(out)
	if message == "" {
		message = "syntax error"
	}
	return line, message, true
}

// nodeCheckLineRe matches the location header node prints before the offending
// line ("<file>:<line>").
var nodeCheckLineRe = regexp.MustCompile(`^(.+):(\d+)$`)

// nodeErrorRe matches the final "<Name>Error: <message>" line of a node parse
// failure (SyntaxError, ReferenceError, ...).
var nodeErrorRe = regexp.MustCompile(`^([A-Za-z]*Error):\s*(.*)$`)

// parseNodeCheckOutput extracts the location and message from `node --check`
// stderr, which looks like:
//
//	/tmp/bad.js:1
//	const a = ;
//	        ^
//
//	SyntaxError: Unexpected token ';'
//	    at wrapSafe (node:internal/modules/cjs/loader:1637:18)
func parseNodeCheckOutput(out string) (int, string) {
	line := 0
	message := ""
	for _, raw := range strings.Split(out, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		if line == 0 {
			if m := nodeCheckLineRe.FindStringSubmatch(l); m != nil {
				if n, err := strconv.Atoi(m[2]); err == nil {
					line = n
					continue
				}
			}
		}
		if message == "" {
			if m := nodeErrorRe.FindStringSubmatch(l); m != nil {
				message = strings.TrimSpace(m[1] + ": " + m[2])
			}
		}
	}
	return line, message
}

// findProjectRoot walks up from a file (or directory) looking for the nearest
// directory that identifies a JS/TS project.
func findProjectRoot(path string) string {
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	dir = filepath.Clean(dir)

	for {
		for _, marker := range []string{"tsconfig.json", "jsconfig.json", "package.json"} {
			if pathExists(filepath.Join(dir, marker)) {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// tsDiagnosticRe matches one tsc/vue-tsc diagnostic line in --pretty false
// output:  <file>(<line>,<col>): error TS2322: Type 'string' is not ...
var tsDiagnosticRe = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\):\s*(error|warning|suggestion)\s+(TS\d+):\s*(.*)$`)

// tsProjectCheckTimeout bounds a project-wide typecheck. Measured cost on this
// repository's web/ project is ~20s, so 120s leaves generous headroom for a
// cold machine without letting one call hang a turn forever.
const tsProjectCheckTimeout = 120 * time.Second

// resolveTypeScriptChecker returns the node entrypoint of the best available
// TypeScript checker for a project directory, preferring vue-tsc when the
// project can contain .vue files (plain tsc cannot parse them and would emit
// bogus "Cannot find module './X.vue'" errors).
func resolveTypeScriptChecker(projectRoot string, targetFile string) (checker string, label string) {
	// The config that defines the project (tsconfig.json) and the installed
	// toolchain (node_modules) are frequently in different directories: a nested
	// package or a monorepo workspace has its own tsconfig but the dependencies
	// are installed at the repo root. Look in the project first, then upward.
	installRoot := findNodeModulesRoot(projectRoot)
	nodeModules := filepath.Join(installRoot, "node_modules")

	usesVue := strings.EqualFold(filepath.Ext(targetFile), ".vue") || projectHasVueFiles(installRoot)

	if usesVue {
		for _, p := range []string{
			filepath.Join(nodeModules, "vue-tsc", "bin", "vue-tsc.js"),
			filepath.Join(nodeModules, "@vue", "language-tools", "bin", "vue-tsc.js"),
		} {
			if pathExists(p) {
				return p, "vue-tsc"
			}
		}
	}

	for _, p := range []string{
		filepath.Join(nodeModules, "typescript", "lib", "tsc.js"),
		filepath.Join(nodeModules, "typescript", "bin", "tsc"),
	} {
		if pathExists(p) {
			return p, "tsc"
		}
	}
	if p, err := exec.LookPath("tsc"); err == nil {
		return p, "tsc"
	}
	return "", ""
}

// findNodeModulesRoot walks up from dir to the nearest directory that actually
// contains node_modules, falling back to dir itself.
func findNodeModulesRoot(dir string) string {
	for d := filepath.Clean(dir); ; {
		if pathExists(filepath.Join(d, "node_modules")) {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return filepath.Clean(dir)
		}
		d = parent
	}
}

// projectHasVueFiles reports whether the project declares a Vue dependency.
func projectHasVueFiles(projectRoot string) bool {
	if pathExists(filepath.Join(projectRoot, "node_modules", "vue")) {
		return true
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"vue"`)
}

// tsProjectDiagnostic is the raw result of a project typecheck.
type tsProjectDiagnostic struct {
	File     string
	Line     int
	Column   int
	Severity string
	Message  string
}

// runTSProjectCheck type-checks the project that owns targetFile and returns
// the diagnostics belonging to that file, plus how many the whole project has.
//
// It returns an error when no checker can be run at all, so callers can tell
// "clean" apart from "never checked" — the previous implementation returned a
// confident "no issues" for every language it had no support for.
func runTSProjectCheck(ctx context.Context, targetFile string) (target []LSPDiagnostic, projectTotal int, label string, note string, err error) {
	projectRoot := findProjectRoot(targetFile)
	if projectRoot == "" {
		return nil, 0, "", "", fmt.Errorf("no package.json/tsconfig.json found above %s", targetFile)
	}

	checker, label := resolveTypeScriptChecker(projectRoot, targetFile)
	if checker == "" {
		return nil, 0, "", "", fmt.Errorf(
			"no TypeScript checker found for project %s: install typescript (or vue-tsc for .vue files) in the project, or use the `lint` tool instead",
			projectRoot)
	}

	nodeBin, lookErr := exec.LookPath("node")
	if lookErr != nil {
		return nil, 0, "", "", fmt.Errorf("node is required to run %s but was not found on PATH", label)
	}

	args := []string{checker, "--noEmit", "--pretty", "false", "--noErrorTruncation"}
	// Use the project config when there is one; without -p tsc would type-check
	// the single file in isolation and report every aliased import as missing.
	config := ""
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		if p := filepath.Join(projectRoot, name); pathExists(p) {
			config = p
			break
		}
	}
	if config != "" {
		args = append(args, "-p", config)
	}

	cctx, cancel := context.WithTimeout(ctx, tsProjectCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, nodeBin, args...)
	cmd.Dir = projectRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	if cctx.Err() == context.DeadlineExceeded {
		return nil, 0, label, "", fmt.Errorf("%s timed out after %s on %s", label, tsProjectCheckTimeout, projectRoot)
	}
	// tsc exits 1 when it reports diagnostics (expected); a non-zero exit with
	// no parseable diagnostic means the checker itself failed (bad config,
	// missing dependency) and must not be silently reported as "clean".
	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		out = stderr.String()
	}
	all := parseTSDiagnostics(out)
	if len(all) == 0 && runErr != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = runErr.Error()
		}
		if len(msg) > 800 {
			msg = msg[:800] + "..."
		}
		return nil, 0, label, "", fmt.Errorf("%s failed on %s: %s", label, projectRoot, msg)
	}

	note = fmt.Sprintf("%s --noEmit on %s took %s and produced %d diagnostic(s) project-wide; only those for the requested file are listed",
		label, projectRoot, elapsed.Round(time.Millisecond), len(all))

	want := filepath.Clean(targetFile)
	for _, d := range all {
		if sameFile(d.File, want, projectRoot) {
			target = append(target, LSPDiagnostic{
				File:     targetFile,
				Line:     d.Line,
				Column:   d.Column,
				Severity: d.Severity,
				Message:  d.Message,
			})
		}
	}
	return target, len(all), label, note, nil
}

// sameFile compares a diagnostic's file reference (tsc prints paths as written
// in the config, i.e. usually relative to the project root) with the file the
// caller asked about.
func sameFile(diagFile, target, projectRoot string) bool {
	if diagFile == "" {
		return false
	}
	candidate := diagFile
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(projectRoot, candidate)
	}
	candidate = filepath.Clean(candidate)

	if runtime.GOOS == "windows" {
		return strings.EqualFold(candidate, target)
	}
	return candidate == target
}

// parseTSDiagnostics pulls structured diagnostics out of tsc output.
func parseTSDiagnostics(out string) []tsProjectDiagnostic {
	var diags []tsProjectDiagnostic
	for _, raw := range strings.Split(out, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		m := tsDiagnosticRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		diags = append(diags, tsProjectDiagnostic{
			File:     m[1],
			Line:     line,
			Column:   col,
			Severity: m[4],
			Message:  m[5] + ": " + m[6],
		})
	}
	return diags
}
