package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Sandbox interface {
	Execute(ctx context.Context, cmd string, args ...string) ([]byte, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte) error
	Remove(path string) error
}

type BasicSandbox struct {
	workDir     string
	timeout     time.Duration
	maxFileSize int64
}

func NewBasicSandbox(workDir string) *BasicSandbox {
	if workDir == "" {
		workDir = getTempDir()
	}
	return &BasicSandbox{
		workDir:     workDir,
		timeout:     30 * time.Second,
		maxFileSize: 10 * 1024 * 1024, // 10MB
	}
}

func (s *BasicSandbox) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Execute command (basic - no real sandboxing yet)
	// In production, use containers or other isolation
	if runtime.GOOS == "windows" {
		args = append([]string{"-Command", cmd}, args...)
		cmd = "powershell"
	}

	output, err := exec.CommandContext(ctx, cmd, args...).CombinedOutput()
	return output, err
}

func (s *BasicSandbox) ReadFile(p string) ([]byte, error) {
	target, err := s.resolvePath(p)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}

	if info.Size() > s.maxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes", info.Size())
	}

	return os.ReadFile(target)
}

func (s *BasicSandbox) WriteFile(p string, data []byte) error {
	target, err := s.resolvePath(p)
	if err != nil {
		return err
	}

	if int64(len(data)) > s.maxFileSize {
		return fmt.Errorf("data too large: %d bytes", len(data))
	}

	return os.WriteFile(target, data, 0644)
}

func (s *BasicSandbox) Remove(p string) error {
	target, err := s.resolvePath(p)
	if err != nil {
		return err
	}

	return os.Remove(target)
}

// resolvePath maps a sandbox path onto a real path inside workDir and rejects
// anything that escapes it.
//
// The previous implementation compared absolute paths with strings.HasPrefix,
// which has two defects: it accepted a merely *prefix-matching* sibling
// ("/tmp/abc-evil" passed the check for a workDir of "/tmp/abc"), and it never
// joined the input with workDir, so every relative path was rejected outright
// (filepath.Abs resolves against the process CWD, not the sandbox).
func (s *BasicSandbox) resolvePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("sandbox: empty path")
	}
	// Reject traversal explicitly rather than letting filepath.Join silently
	// clamp it: "../x" would otherwise be rewritten to "<root>/x" and quietly
	// touch a different file than the caller asked for.
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return "", fmt.Errorf("sandbox: path %q escapes sandbox %q", p, s.workDir)
		}
	}

	absWorkDir, err := filepath.Abs(s.workDir)
	if err != nil {
		return "", fmt.Errorf("sandbox: invalid workDir %q: %w", s.workDir, err)
	}
	// workDir itself may sit behind a symlink (on macOS /tmp is a symlink to
	// /private/tmp); resolve it so both sides of the containment check are
	// expressed in the same terms and no false rejection can occur.
	if resolved, err := filepath.EvalSymlinks(absWorkDir); err == nil {
		absWorkDir = resolved
	}

	// The input is always relative to the sandbox root, even when it looks
	// absolute: filepath.Join collapses a leading separator, preserving the
	// historical semantics.
	target := filepath.Join(absWorkDir, p)

	// Join/Clean are purely lexical, so a symlink inside the sandbox could
	// still point outside it. Resolve the parent (the leaf may legitimately not
	// exist yet for WriteFile) before accepting the target.
	parent := filepath.Dir(target)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		parent = resolvedParent
	}
	target = filepath.Join(parent, filepath.Base(target))

	if !within(absWorkDir, target) {
		return "", fmt.Errorf("sandbox: path %q is outside sandbox %q", p, s.workDir)
	}
	return target, nil
}

// within reports whether target is dir itself or lies underneath it.
// filepath.Rel is used instead of strings.HasPrefix so that "/work/evil" is not
// mistaken for a child of "/work/e".
func within(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

func getTempDir() string {
	return os.TempDir()
}
