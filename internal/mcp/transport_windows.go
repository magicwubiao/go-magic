//go:build windows

package mcp

import (
	"os"
	"os/exec"
)

// setProcessAttrs sets platform-specific process attributes for Windows
func setProcessAttrs(cmd *exec.Cmd) {
	// Windows doesn't support process groups in the same way
	// No special attributes needed
}

// killProcessGroup kills the process on Windows
func killProcessGroup(p *os.Process) {
	p.Kill()
}
