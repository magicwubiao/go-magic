package tool

import (
	"bytes"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Python syntax checking, shared by the post-write hook (LintFile) and the
// lsp_diagnostics tool.

// pyLocRe matches the location line Python prints for a syntax error:
//
//	File "bad.py", line 1
var pyLocRe = regexp.MustCompile(`File "(.+)", line (\d+)`)

// pyErrorRe matches "SyntaxError: invalid syntax".
var pyErrorRe = regexp.MustCompile(`^([A-Za-z]*Error):\s*(.*)$`)

// pySyntaxProbe parses the target file with CPython's own parser.
//
// `python -m py_compile` would report the same SyntaxError but also writes
// __pycache__/*.pyc next to the file — a diagnostics tool has no business
// dirtying the user's working tree, and the hook runs on every write.
const pySyntaxProbe = `import ast, sys
path = sys.argv[1]
try:
    src = open(path, 'rb').read()
except OSError as exc:
    sys.stderr.write('cannot read %s: %s\n' % (path, exc))
    sys.exit(2)
try:
    ast.parse(src, filename=path)
except SyntaxError as exc:
    sys.stderr.write('  File "%s", line %s\n' % (path, exc.lineno or 1))
    sys.stderr.write('%s: %s\n' % (type(exc).__name__, exc.msg))
    sys.exit(1)
`

// pyInterpreterCandidates lists interpreters to try, in order.
//
// On Windows "python3" is normally the Microsoft Store shim, which prints
// "Python was not found" and exits without running anything, so the plain
// "python" name is tried first there. On Unix python3 is the correct name.
func pyInterpreterCandidates() [][]string {
	if runtime.GOOS == "windows" {
		return [][]string{{"python"}, {"python3"}, {"py", "-3"}}
	}
	return [][]string{{"python3"}, {"python"}}
}

// pythonCmdCache caches the resolved interpreter so execute_code does not pay
// the probe cost (LookPath + a real --version subprocess) on every call.
var pythonCmdCache struct {
	sync.Once
	bin    string
	prefix []string
}

// ResolvePythonCommand returns a runnable Python interpreter command.
//
// It returns (bin, prefixArgs, ok). The command must be invoked as
// exec.Command(bin, append(prefixArgs, userArgs...)...): on Windows the best
// candidate may be the "py" launcher, which needs a "-3" argument before the
// user's own flags.
//
// Each candidate is validated with a real `--version` run, not just LookPath —
// the Microsoft Store's app-execution aliases (python.exe / python3.exe in
// WindowsApps) resolve on LookPath but only print "Python was not found" and
// exit 9009, which is exactly the "command not found" symptom users report.
// The result is cached for the process lifetime; interpreter installs rarely
// appear mid-run, and re-probing on every call would add startup latency to
// every execute_code invocation.
func ResolvePythonCommand() (string, []string, bool) {
	pythonCmdCache.Do(func() {
		for _, cand := range pyInterpreterCandidates() {
			bin, err := exec.LookPath(cand[0])
			if err != nil {
				continue
			}
			probe := append(append([]string{}, cand[1:]...), "--version")
			cmd := exec.Command(bin, probe...)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				continue
			}
			text := strings.ToLower(out.String())
			if strings.Contains(text, "python was not found") {
				// Microsoft Store shim: resolves but never runs anything.
				continue
			}
			pythonCmdCache.bin = bin
			pythonCmdCache.prefix = append([]string{}, cand[1:]...)
			return
		}
	})
	return pythonCmdCache.bin, pythonCmdCache.prefix, pythonCmdCache.bin != ""
}

// pythonSyntaxCheck parses a file with the host interpreter's own parser and
// returns (line, message) of the first syntax error.
//
// ok is false when no usable interpreter was found — callers must treat that as
// "not checked" rather than "no issues found". This replaces an earlier guard
// built on `exec.Command("which", "python3")`, which never matched on Windows
// (there is no `which` binary), so Python files were silently never checked.
func pythonSyntaxCheck(path string) (line int, message string, ok bool) {
	for _, cand := range pyInterpreterCandidates() {
		bin, err := exec.LookPath(cand[0])
		if err != nil {
			continue
		}
		args := append(append([]string{}, cand[1:]...), "-c", pySyntaxProbe, path)
		cmd := exec.Command(bin, args...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		runErr := cmd.Run()
		text := out.String()

		if runErr == nil {
			return 0, "", true // parsed cleanly
		}
		if l, msg := parsePythonDiagnostics(text); msg != "" {
			return l, msg, true
		}
		// Nothing parseable: the interpreter itself refused to run (Store shim,
		// broken install) — try the next candidate.
	}

	return 0, "", false
}

// parsePythonDiagnostics turns interpreter stderr into (line, message).
func parsePythonDiagnostics(out string) (int, string) {
	line := 0
	message := ""
	for _, raw := range strings.Split(out, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		if line == 0 {
			if m := pyLocRe.FindStringSubmatch(l); m != nil {
				if n, err := strconv.Atoi(m[2]); err == nil {
					line = n
				}
				continue
			}
		}
		if message == "" {
			if m := pyErrorRe.FindStringSubmatch(l); m != nil {
				message = strings.TrimSpace(m[1] + ": " + m[2])
			}
		}
	}
	return line, message
}
