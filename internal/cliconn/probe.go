package cliconn

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ProbeResult 是一条诊断命令的结果。
type ProbeResult struct {
	// Output 是 stdout+stderr 合并后的首行（用于展示版本 / 失败原因）。
	Output string
	// ExitCode 为 0 表示命令成功。
	ExitCode int
	// Err 是启动失败（命令不存在、权限不足），非零退出码不算 Err。
	Err error
	// TimedOut 表示超时被强杀。
	TimedOut bool
}

// Probe 用与连接器执行**同一套纪律**跑一条只读诊断命令：
// argv 直传（不经过 shell）、隐藏窗口、输出按 UTF-8/GB18030 解码、输出截断。
//
// 只给"装没装 / 登没登 / 服务可不可达"这类检测用：命令必须来自内置目录，
// 不能把用户输入直接喂进来——否则这就是一个任意命令执行入口。
func Probe(ctx context.Context, command string, args []string, timeout time.Duration) ProbeResult {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if err := checkExecutable(command); err != nil {
		return ProbeResult{Err: err}
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.WaitDelay = 2 * time.Second
	setProcAttrs(cmd)

	buf := &cappedBuffer{limit: 8 << 10}
	cmd.Stdout = buf
	cmd.Stderr = buf

	runErr := cmd.Run()
	res := ProbeResult{
		Output: firstLine(decodeOutput(buf.Bytes()), 160),
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = 124
		return res
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res
		}
		res.Err = runErr
		return res
	}
	return res
}

// firstLine 取首个非空行并截断到 limit：版本号可能带一堆版权行，
// 失败信息也可能一次吐几十行，展示层只需要"一句话"。
func firstLine(s string, limit int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > limit {
			line = line[:limit] + "…"
		}
		return line
	}
	return ""
}
