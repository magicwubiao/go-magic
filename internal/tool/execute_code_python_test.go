package tool

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestResolvePythonCommand verifies the Windows-safe interpreter resolution.
// On Windows "python3" is either missing or the Microsoft Store shim (exits
// 9009 after printing "Python was not found") — the resolver must skip such
// candidates and return a command that actually runs.
func TestResolvePythonCommand(t *testing.T) {
	bin, prefix, ok := ResolvePythonCommand()
	if !ok {
		t.Skip("no python interpreter on PATH (python/python3/py -3 all unusable)")
	}

	// The resolved bin must exist on disk.
	if _, err := exec.LookPath(bin); err != nil {
		t.Fatalf("resolved bin %q not found: %v", bin, err)
	}

	// The command must actually execute: `--version` through the same
	// bin+prefix combination execute_code would use.
	args := append(append([]string{}, prefix...), "--version")
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("resolved command %q %v failed: %v (%s)", bin, args, err, out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "python") {
		t.Fatalf("unexpected --version output: %q", out)
	}
}

// TestExecutePythonEndToEnd runs real Python code through the public
// execute_code entry, exercising the resolved interpreter (pip install path is
// covered by ExecutePythonSmokePackages below only when packages are given —
// kept package-free here so the test runs on any machine with Python).
func TestExecutePythonEndToEnd(t *testing.T) {
	if _, _, ok := ResolvePythonCommand(); !ok {
		t.Skip("no python interpreter available")
	}

	tl := NewExecuteCodeTool()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := tl.Execute(ctx, map[string]interface{}{
		"code":     "print('hello from python')",
		"language": "python",
		"timeout":  float64(20),
	})
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if code := m["exit_code"]; code != 0 {
		t.Fatalf("exit_code = %v, stderr=%q", code, m["stderr"])
	}
	stdout, _ := m["stdout"].(string)
	if !strings.Contains(stdout, "hello from python") {
		t.Fatalf("stdout missing expected text: %q", stdout)
	}
}
