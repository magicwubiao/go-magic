//go:build windows

package acp

import (
	"os"
	"os/exec"
)

// setProcessAttrs 在 Windows 上没有进程组可设，保持默认。
func setProcessAttrs(cmd *exec.Cmd) {}

// killProcessGroup 在 Windows 上只能杀直接子进程；npx 之类的包装器
// 可能留下孙进程，这在 ACP 场景可接受（会话结束由 agent 自己负责收尾）。
func killProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Kill()
}
