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
	// Argv 是回显用的命令行（含可执行文件本身），**已脱敏**：由 env 提供、
	// 且判定为凭据的值被替换成 ***（见 SecretEnvValues）。
	//
	// 这里刻意不保存真实 argv：它唯一的去向是 Tool.Execute 的 "command" 字段，
	// 也就是模型上下文、会话历史与日志。真实 argv 只存在于本函数的局部变量里。
	Argv       []string
	Stdout     string
	Stderr     string
	ExitCode   int
	Duration   time.Duration
	Truncated  bool
	TimedOut   bool
	StdinBytes int
	// OutputLimit 是本次执行实际生效的输出上限（连接器级覆盖后的值），
	// 截断 hint 用它而不是全局默认值，模型看到的数字与真实截断点一致。
	OutputLimit int
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

	envList, oauthToken, err := prepareEnv(conn)
	if err != nil {
		return nil, err
	}
	envMap := envMapFromList(envList)

	argv, err := ExpandArgs(conn.Args, spec, resolved, envMap)
	if err != nil {
		return nil, err
	}

	stdinPayload := ""
	if spec.StdinParam != "" {
		stdinPayload = resolved.str[spec.StdinParam]
	}
	// {{envfile:NAME}} 在模板展开之后物化：值来自 envMap（已含 ${...} 展开
	// 与 OAuth 覆盖），落成 0600 临时文件，进程结束后删除。
	argv, cleanup, err := materializeEnvFiles(argv, envMap)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return executeArgv(ctx, conn, envList, oauthToken, argv, stdinPayload, "")
}

// RawParamLimits 是 raw 工具的输入上限。
const (
	// MaxRawArgs 是单次调用允许的 argv 元素默认上限。正常 CLI 调用远用不到 64 个
	// 参数，设上限是为了让"模型抽风输出几万个参数"变成一个明确的校验错误，
	// 而不是一次失控的进程拉起。连接器可用 RawMaxArgs 覆盖，硬上限见下。
	MaxRawArgs = 64
	// MaxRawArgsCap 是 raw_max_args 的硬上限：覆盖再大也到此为止。
	MaxRawArgsCap = 256
	// MaxRawStdinBytes 是 stdin 载荷上限。喂 diff / JSON 这类载荷通常几 KB；
	// 真正的大文件应该走文件路径参数，而不是整份塞进 stdin。
	MaxRawStdinBytes = 1 << 20
)

// RunRaw 执行"自由命令"：argv 由调用方（raw 工具）直接给出，不经模板展开，
// 其余链路（env 展开、OAuth 注入、脱敏、截断、超时）与 Run 完全一致。
//
// cwd 是本次调用的可选工作目录（覆盖连接器 working_dir；相对路径按它解析）。
// 每次调用都是新进程，cwd 无法从上一次调用延续——模型想"在同一个目录里连做
// 几步"，就必须显式传这个参数（主流通用终端 agent 的模型侧约定一致）。
//
// argv 的每个元素原样成为一个参数——这里的输入来自模型而非配置作者，
// 但不经 shell 这条底线不变：元素里的 `;` `&&` `|` 不会被解释。
func RunRaw(ctx context.Context, conn Connector, argv []string, stdin, cwd string) (*Result, error) {
	if strings.TrimSpace(conn.Command) == "" {
		return nil, errors.New("connector has no command configured")
	}
	maxArgs := conn.EffectiveRawMaxArgs()
	if len(argv) > maxArgs {
		return nil, fmt.Errorf("too many arguments (%d, max %d); chain multiple calls or write a script file and run it", len(argv), maxArgs)
	}
	if len(stdin) > MaxRawStdinBytes {
		return nil, fmt.Errorf("stdin payload too large (%d bytes, max %d); pass a file path instead", len(stdin), MaxRawStdinBytes)
	}
	envList, oauthToken, err := prepareEnv(conn)
	if err != nil {
		return nil, err
	}
	// 连接器级固定前缀（conn.Args）与声明式 Run 保持一致地拼在用户 argv 之前：
	// 例如 `docker` 连接器配了 ["--context", "remote"]，raw 调用同样必须带上，
	// 否则同一个连接器下声明工具与 raw 工具会打向不同的目标。
	// 前缀只展开 {{env:NAME}}；raw 没有声明参数可填，残留的参数占位符直接报错，
	// 而不是把模板字面量当参数塞给 CLI。
	full := make([]string, 0, len(conn.Args)+len(argv))
	envMap := envMapFromList(envList)
	for _, tmpl := range conn.Args {
		t, err := expandEnvRefs(tmpl, envMap)
		if err != nil {
			return nil, fmt.Errorf("connector args: %w", err)
		}
		if m := placeholderRe.FindString(t); m != "" {
			return nil, fmt.Errorf("connector args contain parameter placeholder %s, which raw calls cannot fill; run it through a declared tool instead", m)
		}
		full = append(full, t)
	}
	full = append(full, argv...)
	return executeArgv(ctx, conn, envList, oauthToken, full, stdin, cwd)
}

// prepareEnv 展开连接器 env 的 ${...} 引用，并取得 OAuth token（追加到 env 尾部，
// 覆盖可能残留的旧值）。Run 与 RunRaw 共用。
func prepareEnv(conn Connector) (envList []string, oauthToken string, err error) {
	envList = conn.Env
	if len(envList) > 0 {
		envList, err = envref.ExpandList(conn.Env)
		if err != nil {
			return nil, "", fmt.Errorf("connector env: %w", err)
		}
	}
	// OAuth 设备码取得的 token 最后覆盖：它是"当前这一秒"的最新凭据，
	// 必须盖过 env 里可能残留的旧值（用户上次手填的、或上一枚已过期的）。
	if oauth := conn.OAuth; oauth != nil && oauth.TokenEnv != "" {
		if oauth.Tokens == nil {
			return nil, "", fmt.Errorf("connector %q uses OAuth but no token store is wired", conn.Command)
		}
		tok, err := oauth.Tokens.EnsureAccessToken()
		if err != nil {
			return nil, "", fmt.Errorf("oauth token unavailable: %w", err)
		}
		oauthToken = tok
		envList = append(envList, oauth.TokenEnv+"="+tok)
	}
	return envList, oauthToken, nil
}

// executeArgv 是执行尾巴：超时兜底、可执行检查、拉起子进程、收集与脱敏输出。
// Run（模板展开后）与 RunRaw（argv 直传）在这里汇合。
// dirOverride 非空时覆盖连接器的 working_dir（raw 的 cwd 参数用）。
func executeArgv(ctx context.Context, conn Connector, envList []string, oauthToken string, argv []string, stdinPayload, dirOverride string) (*Result, error) {
	outputLimit := conn.EffectiveMaxOutputBytes()
	timeout := time.Duration(conn.EffectiveTimeout(ToolSpec{})) * time.Second
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

	if len(envList) > 0 {
		// ${ENV} 引用已在 prepareEnv 展开过（{{env:NAME}} 与进程环境取同一份值）。
		cmd.Env = mergeEnv(os.Environ(), envList)
	}
	if dir := ExpandWorkingDir(conn.WorkingDir); dir != "" {
		cmd.Dir = dir
	}
	if dirOverride != "" {
		// raw 的 cwd 参数：本次调用显式指定。相对路径按连接器 working_dir 解析，
		// 绝对路径原样使用；目录不存在是明确的参数错误，立即返回。
		base := ExpandWorkingDir(conn.WorkingDir)
		dir := dirOverride
		if !filepath.IsAbs(dir) && base != "" {
			dir = filepath.Join(base, dir)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("cwd %q is not an accessible directory", dirOverride)
		}
		cmd.Dir = dir
	}

	stdinBytes := 0
	if stdinPayload != "" {
		cmd.Stdin = strings.NewReader(stdinPayload)
		stdinBytes = len(stdinPayload)
	}

	stdout := &cappedBuffer{limit: outputLimit}
	stderr := &cappedBuffer{limit: outputLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	setProcAttrs(cmd)

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	res := &Result{
		// 回显的 argv 必须脱敏：{{env:NAME}} 展开后就是明文凭据，而这条命令
		// 会进模型上下文、会话历史和日志（Tool.Execute 的 "command" 字段）。
		// 真实的 argv 只用于上面的 exec，不往外带。
		Argv:        MaskArgv(append([]string{conn.Command}, argv...), SecretEnvValues(envMapFromList(envList), oauthToken)),
		Stdout:      decodeOutput(stdout.Bytes()),
		Stderr:      decodeOutput(stderr.Bytes()),
		Duration:    elapsed,
		Truncated:   stdout.truncated || stderr.truncated,
		StdinBytes:  stdinBytes,
		OutputLimit: outputLimit,
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

// PinCommandPath 把连接器的 command 解析成绝对路径并返回副本。
//
// 为什么在保存/注册时固定：裸命令名每次执行都重新走 PATH——保存时探测通过的
// 二进制，执行时可能因 PATH 被前置劫持而换成另一个程序，而凭据 env 会原样
// 注入给它。固定成绝对路径后执行不再依赖 PATH。解析失败（二进制还没装）时
// 保留原名：由就绪探测如实报告"找不到命令"，而不是把连接器配成废的。
// 已是绝对路径、或含路径分隔符的显式路径，规范化后原样返回。
func (c Connector) PinCommandPath() Connector {
	cmd := strings.TrimSpace(c.Command)
	if cmd == "" {
		return c
	}
	if filepath.IsAbs(cmd) {
		return c
	}
	if strings.ContainsRune(cmd, '/') || strings.ContainsRune(cmd, '\\') {
		// 显式的相对路径：按当前进程 cwd 转绝对，仍是"固定"语义。
		if abs, err := filepath.Abs(cmd); err == nil {
			out := c
			out.Command = abs
			return out
		}
		return c
	}
	p, err := exec.LookPath(cmd)
	if err != nil || p == "" {
		return c
	}
	out := c
	out.Command = p
	return out
}

// materializeEnvFiles 把 argv 里的 {{envfile:NAME}} 占位符物化成 0600 临时文件。
//
// 占位符必须独占一个 arg（`--token={{envfile:X}}` 这种拼进选项值的写法没有
// 意义——CLI 要的是文件路径，不是"路径拼在选项名里"）；NAME 未声明或值为空的
// 错误语义与 {{env:NAME}} 一致。返回替换后的 argv 与清理函数——清理在子进程
// 退出后运行（Run/RunRaw 的 defer），绝不提前：进程可能还要读这些文件。
func materializeEnvFiles(argv []string, env map[string]string) ([]string, func(), error) {
	noop := func() {}
	needs := false
	for _, arg := range argv {
		if strings.Contains(arg, "{{envfile:") {
			needs = true
			break
		}
	}
	if !needs {
		return argv, noop, nil
	}
	var files []string
	cleanup := func() {
		for _, f := range files {
			os.Remove(f)
		}
	}
	out := make([]string, len(argv))
	for i, arg := range argv {
		if !strings.Contains(arg, "{{envfile:") {
			out[i] = arg
			continue
		}
		m := envFileRefRe.FindStringSubmatch(arg)
		if m == nil || m[0] != arg {
			return nil, cleanup, fmt.Errorf("%q: {{envfile:NAME}} must be the entire argument; use a --token-file style flag that accepts a path", arg)
		}
		val, ok := env[m[1]]
		if !ok {
			return nil, cleanup, fmt.Errorf("args reference envfile %q but it is not declared in connector env", m[1])
		}
		if strings.TrimSpace(val) == "" {
			return nil, cleanup, fmt.Errorf("env %q is empty: fill its value in the connector settings (or write ${NAME:-default} in env)", m[1])
		}
		f, err := os.CreateTemp("", "cliconn-secret-")
		if err != nil {
			return nil, cleanup, fmt.Errorf("materialize envfile %q: %w", m[1], err)
		}
		name := f.Name()
		files = append(files, name)
		if _, err := f.WriteString(val); err != nil {
			f.Close()
			return nil, cleanup, fmt.Errorf("materialize envfile %q: %w", m[1], err)
		}
		if err := f.Close(); err != nil {
			return nil, cleanup, fmt.Errorf("materialize envfile %q: %w", m[1], err)
		}
		// CreateTemp 在 POSIX 上已是 0600；chmod 兜 Windows/异常实现。
		if err := os.Chmod(name, 0o600); err != nil {
			return nil, cleanup, fmt.Errorf("materialize envfile %q: %w", m[1], err)
		}
		out[i] = name
	}
	return out, cleanup, nil
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
