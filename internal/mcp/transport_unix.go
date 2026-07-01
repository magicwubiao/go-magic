//go:build !windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessAttrs sets platform-specific process attributes for Unix
func setProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killProcessGroup kills the process group on Unix
func killProcessGroup(p *os.Process) {
	pgid, err := syscall.Getpgid(p.Pid)
	if err == nil {
		syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		p.Kill()
	}
}
