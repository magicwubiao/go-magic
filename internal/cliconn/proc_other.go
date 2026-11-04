//go:build !windows

package cliconn

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcAttrs 让子进程进入独立进程组。
//
// 这是 Unix 侧"回收整棵进程树"的前提：超时或取消时向 -pgid 发信号，shell
// 脚本派生的孙进程才会一起结束。仅仅 kill 直接子进程会把 `sh -c '...'` 里的
// 真正工作留在后台（构建、rsync、docker build 都可能跑很久）。
func setProcAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// procGuard 持有本次调用的进程组 id。
type procGuard struct {
	pid  int
	proc *os.Process
}

// startCommand 启动子进程；进程组已在 setProcAttrs 里设好。
func startCommand(cmd *exec.Cmd) (*procGuard, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &procGuard{pid: cmd.Process.Pid, proc: cmd.Process}, nil
}

// Kill 向整个进程组发 SIGKILL；组已经不在了就退回单进程 Kill。
func (g *procGuard) Kill() {
	if g == nil {
		return
	}
	if g.pid > 0 {
		if err := syscall.Kill(-g.pid, syscall.SIGKILL); err == nil {
			return
		}
	}
	if g.proc != nil {
		_ = g.proc.Kill()
	}
}

// Close 在 Unix 上没有额外资源要释放。
func (g *procGuard) Close() {}

// normalizeEnvKey 在类 Unix 平台保持原样：环境变量名大小写敏感。
func normalizeEnvKey(key string) string { return key }

// isBatchScript 在非 Windows 平台恒为 false（.bat/.cmd 只是普通文件名）。
func isBatchScript(name string) bool { return false }
