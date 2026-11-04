package cliconn

import (
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/pkg/envref"
)

// previewMaxRunes 是审批卡片里命令行的长度上限。
//
// 审批卡片不是日志：超过这个长度的命令没人会逐字读，截断后仍然能看出
// "跑的是哪个子命令、带了哪些显眼参数"。真实执行不受影响。
const previewMaxRunes = 400

// PreviewCommand 返回一次声明式调用**将要执行**的命令行（已脱敏），
// 供审批卡片展示与风险分析使用。
//
// 与 Run 共用同一套展开逻辑（ResolveParams + ExpandArgs），但**不启动进程、
// 不取 OAuth token、不物化 envfile**：审批发生在执行之前，任何有副作用的
// 动作都不能在这里做。参数不合法（缺必填、enum 不匹配、env 引用为空）时
// 返回空串，让调用方回落到静态摘要——真正的报错留给执行阶段，这里不抢答。
func PreviewCommand(conn Connector, spec ToolSpec, params map[string]any) string {
	if strings.TrimSpace(conn.Command) == "" {
		return ""
	}
	resolved, err := ResolveParams(spec, params)
	if err != nil {
		return ""
	}
	envMap := previewEnv(conn)
	argv, err := ExpandArgs(conn.Args, spec, resolved, envMap)
	if err != nil {
		return ""
	}
	full := append([]string{previewBinary(conn.Command)}, maskEnvFileArgs(argv)...)
	return truncate(strings.Join(MaskArgv(full, SecretEnvValues(envMap)), " "), previewMaxRunes)
}

// PreviewRawCommand 返回一次 raw 调用将要执行的命令行（已脱敏）。
//
// 与 RunRaw 同样只做"拼 argv + 展开 env 前缀"，不创建临时文件、不取 token。
func PreviewRawCommand(conn Connector, params map[string]any) string {
	if strings.TrimSpace(conn.Command) == "" {
		return ""
	}
	argv, _, cwd, err := parseRawParams(params)
	if err != nil {
		return ""
	}
	envMap := previewEnv(conn)
	full := make([]string, 0, len(conn.Args)+len(argv)+1)
	full = append(full, previewBinary(conn.Command))
	for _, tmpl := range conn.Args {
		t, err := expandEnvRefs(tmpl, envMap)
		if err != nil {
			return ""
		}
		full = append(full, t)
	}
	full = append(full, argv...)

	out := truncate(strings.Join(MaskArgv(full, SecretEnvValues(envMap)), " "), previewMaxRunes)
	if cwd != "" {
		out += "  (cwd: " + truncate(cwd, 80) + ")"
	}
	return out
}

// previewBinary 取可执行文件名用于展示。
//
// 保存连接器时 command 会被固定成绝对路径（防 PATH 劫持），但审批卡片上
// `C:\Program Files\GitHub CLI\gh.exe` 这种前缀没有信息量；更重要的是审批层
// 会按"首个 token = 可执行文件"解析命令做风险分类与模式学习，绝对路径会
// 让同一个连接器在不同机器上得到不同的模式。文件名足够识别。
func previewBinary(command string) string {
	if base := filepath.Base(command); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return command
}

// previewEnv 取连接器 env 的展示用视图。
//
// 展开 ${...} 引用是为了让审批卡片显示"这一秒真正会传给子进程的值"；
// 展开失败（引用的 shell 变量不存在）时退回原始字面量——脱敏仍按变量名
// 生效，不至于因为一个无关变量把整条预览丢掉。
func previewEnv(conn Connector) map[string]string {
	if len(conn.Env) == 0 {
		return nil
	}
	if expanded, err := envref.ExpandList(conn.Env); err == nil {
		return envMapFromList(expanded)
	}
	return envMapFromList(conn.Env)
}

// maskEnvFileArgs 把 {{envfile:NAME}} 占位符替换成可读标记。
//
// 预览阶段不会真的创建临时文件（那会在审批被拒绝后留下垃圾），所以这里
// 只告诉用户"这里会是一个凭据文件"，不是一个看起来像路径的假值。
func maskEnvFileArgs(argv []string) []string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		if m := envFileRefRe.FindStringSubmatch(arg); m != nil && m[0] == arg {
			out[i] = "<secret-file:" + m[1] + ">"
			continue
		}
		out[i] = arg
	}
	return out
}
