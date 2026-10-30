package cliconn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Result 是一次 CLI 调用的结果。非零退出码不算"执行失败"——很多 CLI 用退出码
// 表达业务结论（grep 没匹配到=1、diff 有差异=1），把它变成 Go error 会让模型
// 看不到 stdout。真正的失败（命令不存在、超时）才走 error。
type Result struct {
	Argv       []string
	Stdout     string
	Stderr     string
	ExitCode   int
	Duration   time.Duration
	Truncated  bool
	TimedOut   bool
	StdinBytes int
}

// Run 展开参数并执行一次命令。
//
// ctx 应该由调用方带上超时（见 EffectiveTimeout）；这里再兜一层，
// 防止上游传了一个没有 deadline 的 ctx 把子进程挂到进程结束。
func Run(ctx context.Context, conn Connector, spec ToolSpec, params map[string]any) (*Result, error) {
	if strings.TrimSpace(conn.Command) == "" {
		return nil, errors.New("connector has no command configured")
	}

	resolved, err := ResolveParams(spec, params)
	if err != nil {
		return nil, err
	}
	argv := ExpandArgs(conn.Args, spec, resolved)

	timeout := time.Duration(conn.EffectiveTimeout(spec)) * time.Second
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if err := checkExecutable(conn.Command); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, conn.Command, argv...)
	cmd.WaitDelay = 3 * time.Second // ctx 取消后给子进程收尸的宽限

	if len(conn.Env) > 0 {
		cmd.Env = append(os.Environ(), conn.Env...)
	}
	if dir := ExpandWorkingDir(conn.WorkingDir); dir != "" {
		cmd.Dir = dir
	}

	stdinBytes := 0
	if spec.StdinParam != "" {
		if payload := resolved.str[spec.StdinParam]; payload != "" {
			cmd.Stdin = strings.NewReader(payload)
			stdinBytes = len(payload)
		}
	}

	stdout := &cappedBuffer{limit: DefaultMaxOutputBytes}
	stderr := &cappedBuffer{limit: DefaultMaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	setProcAttrs(cmd)

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	res := &Result{
		Argv:       append([]string{conn.Command}, argv...),
		Stdout:     decodeOutput(stdout.Bytes()),
		Stderr:     decodeOutput(stderr.Bytes()),
		Duration:   elapsed,
		Truncated:  stdout.truncated || stderr.truncated,
		StdinBytes: stdinBytes,
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			res.TimedOut = true
			res.ExitCode = 124
			return res, nil
		case errors.Is(ctx.Err(), context.Canceled):
			return res, fmt.Errorf("command cancelled: %w", ctx.Err())
		case errors.As(runErr, &exitErr):
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		default:
			// 启动失败（权限不足、不是可执行文件等）：这类问题重试无意义，
			// 交给调用方按错误展示。
			return res, fmt.Errorf("failed to run %q: %w", conn.Command, runErr)
		}
	}
	return res, nil
}

// checkExecutable 在启动前给出可读的错误，而不是让 exec 抛一个
// "file not found" 让用户猜是路径写错还是没装。
func checkExecutable(name string) error {
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) {
		if _, err := os.Stat(name); err != nil {
			return fmt.Errorf("command %q not found: %w", name, err)
		}
	}
	// Windows 下 .bat/.cmd 不能直接 CreateProcess。这里显式拒绝并给出改法，
	// 而不是偷偷用 cmd.exe /c 重新解析一遍——那会把参数里的 & | 变成命令分隔符，
	// 等于把模型给的值当 shell 代码执行。
	if isBatchScript(name) {
		return fmt.Errorf("command %q is a Windows batch script; point `command` at the .exe, or use command \"cmd\" with args [\"/c\", %q, ...]", name, filepath.Base(name))
	}
	return nil
}

// decodeOutput 把子进程输出转成可读文本。
//
// Windows 上不少系统命令（ipconfig、chcp 936 环境下的老工具）输出 GBK，
// 直接当 UTF-8 塞给模型会得到一堆替换字符。只在"不是合法 UTF-8"时尝试
// GB18030 解码，不影响正常输出。
func decodeOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	return strings.ToValidUTF8(string(b), "\uFFFD")
}

// cappedBuffer 是带上限的输出缓冲：超过 limit 的部分丢弃并置 truncated。
//
// 为什么截断而不是报错：一次 `kubectl get pods -A` 或 `git log` 的输出可能是
// 几 MB，全塞进上下文会直接顶掉对话历史。前 limit 字节通常已包含有效信息。
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *cappedBuffer) Bytes() []byte  { return w.buf.Bytes() }
func (w *cappedBuffer) Len() int       { return w.buf.Len() }
func (w *cappedBuffer) String() string { return w.buf.String() }
