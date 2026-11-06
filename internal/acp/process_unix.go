//go:build !windows

package acp

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessAttrs 让子进程独立成组，便于连同它拉起的孩子一起收掉。
func setProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup 先礼后兵：SIGTERM 整个进程组，拿不到就 Kill 单个进程。
func killProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	if pgid, err := syscall.Getpgid(p.Pid); err == nil {
		if syscall.Kill(-pgid, syscall.SIGTERM) == nil {
			return
		}
	}
	_ = p.Kill()
}
