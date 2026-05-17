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

func (s *BasicSandbox) ReadFile(path string) ([]byte, error) {
	// Check if path is within workDir
	if !s.isPathSafe(path) {
		return nil, fmt.Errorf("path %s is outside sandbox", path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.Size() > s.maxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes", info.Size())
	}

	return os.ReadFile(path)
}

func (s *BasicSandbox) WriteFile(path string, data []byte) error {
	if !s.isPathSafe(path) {
		return fmt.Errorf("path %s is outside sandbox", path)
	}

	if int64(len(data)) > s.maxFileSize {
		return fmt.Errorf("data too large: %d bytes", len(data))
	}

	return os.WriteFile(path, data, 0644)
}

func (s *BasicSandbox) Remove(path string) error {
	if !s.isPathSafe(path) {
		return fmt.Errorf("path %s is outside sandbox", path)
	}

	return os.Remove(path)
}

func (s *BasicSandbox) isPathSafe(path string) bool {
	// Simple check: ensure path is within workDir
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	absWorkDir, _ := filepath.Abs(s.workDir)
	return strings.HasPrefix(absPath, absWorkDir)
}

func getTempDir() string {
	return os.TempDir()
}
