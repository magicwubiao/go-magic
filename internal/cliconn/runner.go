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

	"github.com/magicwubiao/go-magic/pkg/envref"
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

	// 先展开 env（args 模板里的 {{env:NAME}} 引用它的最终值），再展开参数。
	// 每次执行都展开一次，因此在 shell 里换了 token 不需要重启服务。
	envList := conn.Env
	var envMap map[string]string
	if len(envList) > 0 {
		var err error
		envList, err = envref.ExpandList(conn.Env)
		if err != nil {
			return nil, fmt.Errorf("connector env: %w", err)
		}
		envMap = envMapFromList(envList)
	}
	argv, err := ExpandArgs(conn.Args, spec, resolved, envMap)
	if err != nil {
		return nil, err
	}

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
		// ${ENV} 引用已在上面展开过（{{env:NAME}} 与进程环境取同一份值）。
		cmd.Env = mergeEnv(os.Environ(), envList)
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

// envMapFromList 把 "KEY=VALUE" 列表转成 map（后出现者覆盖先出现者，
// 与 mergeEnv 的覆盖语义一致）；不含 "=" 的条目跳过。
func envMapFromList(list []string) map[string]string {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]string, len(list))
	for _, kv := range list {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

// mergeEnv 把连接器声明的环境变量覆盖到继承环境上。
//
// 不能直接 append：无论 Unix 的 getenv 还是 Windows 的环境块解析，遇到重名
// 键都只认**第一个**，把连接器的值追加在最后等于永远不生效——用户明明在
// 配置里填了新 token，命令却拿着进程继承的旧 token 在跑（很隐蔽的 401）。
func mergeEnv(base, overrides []string) []string {
	index := make(map[string]int, len(base)+len(overrides))
	out := make([]string, 0, len(base)+len(overrides))

	put := func(kv string) {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return
		}
		if i, seen := index[normalizeEnvKey(key)]; seen {
			out[i] = kv // 同键就地覆盖，顺序不变
			return
		}
		index[normalizeEnvKey(key)] = len(out)
		out = append(out, kv)
	}
	for _, kv := range base {
		put(kv)
	}
	for _, kv := range overrides {
		put(kv)
	}
	return out
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

func (w *cappedBuffer) Bytes() []byte {
	if w.truncated {
		return trimIncompleteRune(w.buf.Bytes())
	}
	return w.buf.Bytes()
}

func (w *cappedBuffer) Len() int       { return w.buf.Len() }
func (w *cappedBuffer) String() string { return string(w.Bytes()) }

// trimIncompleteRune 去掉尾部"被截断的半个字符"。
//
// 截断是按字节做的，切点很容易落在一个多字节字符中间（中文输出尤其常见）。
// 后果不只是少一个字：decodeOutput 看到 utf8.Valid=false 会去试 GB18030，
// 于是**整段正常的中文输出被当成 GBK 转码**，变成一堆乱码。
func trimIncompleteRune(b []byte) []byte {
	for i := 0; i < utf8.UTFMax && i < len(b); i++ {
		// 注意不能只看 size==1：合法的 ASCII 字符也返回 size==1，
		// 判据必须是"RuneError + 单字节"这个非法序列的特征。
		if r, size := utf8.DecodeLastRune(b[:len(b)-i]); !(r == utf8.RuneError && size == 1) {
			return b[:len(b)-i]
		}
	}
	return b
}
