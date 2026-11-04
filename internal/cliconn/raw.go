package cliconn

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RawTool 是"自由命令"逃生工具：cli_<connector>_raw。
//
// 定位对齐 Claude Code 一类通用终端 agent——模型不必等每条子命令都被逐条
// 声明成工具，可以直接对连接器的二进制拼任意 argv。与通用 shell 的差别在于
// 边界收窄到"这一个二进制"：
//   - argv 数组直传，不经 shell（`;` `&&` `|` 不会被解释）；
//   - 只能跑 Connector.Command，模型无法借它拉起别的程序；
//   - 永远需要人工审批（见 RequiresApproval）；
//   - env/OAuth 凭据注入、argv 脱敏、输出截断、超时全部复用 Run 的链路。
//
// 适用场景：目录/声明没覆盖的长尾子命令（`gh api ...`、`kubectl describe ...`）。
// 描述里引导模型优先用声明过的工具（有 schema、审批更顺），raw 兜底。
type RawTool struct {
	connector string
	conn      Connector
}

// RawToolName 是 raw 工具在连接器内的保留短名（全名 cli_<connector>_raw）。
const RawToolName = "raw"

// NewRawTool 构造 raw 工具（连接器定义值拷贝，与 NewTool 同一纪律）。
func NewRawTool(connector string, conn Connector) *RawTool {
	return &RawTool{connector: connector, conn: conn}
}

// Name 返回注册名 cli_<connector>_raw。
func (t *RawTool) Name() string { return ToolName(t.connector, RawToolName) }

// Description 是模型看到的说明。这里要教会模型两件事：raw 能干什么、
// 什么时候该用声明过的工具而不是它。
func (t *RawTool) Description() string {
	label := strings.TrimSpace(t.conn.Description)
	if label == "" {
		label = t.conn.Command + " CLI"
	}
	return label + "\n" +
		fmt.Sprintf("Run ANY subcommand of %q that has no dedicated tool declared here. "+
			"Prefer the dedicated tools when one matches (they have typed parameters and clearer approval). "+
			"Pass the argument list exactly as it would appear after %q on the command line; "+
			"arguments are passed to the process directly, the shell is never involved, so quoting is not needed. "+
			"Use [\"--help\"] (or [\"<subcommand>\", \"--help\"]) to discover capabilities. "+
			"Every call requires user approval.", t.conn.Command, t.conn.Command)
}

// Schema 生成 OpenAI function calling 的 JSON Schema。
//
// args 是字符串数组：每个元素就是 argv 的一个元素。模型不需要（也不应该）
// 做任何引号/转义处理——含空格的值直接作为一个元素传入。
func (t *RawTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"args": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": fmt.Sprintf("Argument list passed to %q, element by element (no shell quoting). Example: [\"pr\", \"list\", \"--limit\", \"20\"]. Empty list runs the bare command (usually prints help).", t.conn.Command),
			},
			"stdin": map[string]interface{}{
				"type":        "string",
				"description": "Optional payload piped to the process stdin (a diff, a JSON document, ...). Prefer a file path argument for large payloads.",
			},
			"cwd": map[string]interface{}{
				"type":        "string",
				"description": "Optional working directory for THIS call (absolute path, or relative to the connector's working directory). Each call is a fresh process, so the directory does NOT persist between calls — pass it explicitly on every call when chaining steps in the same directory (e.g. first call to discover files, then the same cwd to act on them).",
			},
		},
		"required": []string{"args"},
	}
}

// Timeout 返回单次执行超时（连接器级默认；raw 没有 per-tool 覆盖）。
func (t *RawTool) Timeout() time.Duration {
	return time.Duration(t.conn.EffectiveTimeout(ToolSpec{})) * time.Second
}

// Execute 解析 args/stdin/cwd 并执行。
func (t *RawTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	argv, stdin, cwd, err := parseRawParams(params)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, t.Timeout())
	defer cancel()

	res, err := RunRaw(runCtx, t.conn, argv, stdin, cwd)
	if err != nil {
		return nil, err
	}
	return resultOutput(res, t.Timeout()), nil
}

// parseRawParams 从模型给的参数里取 args、stdin 与 cwd。
//
// args 的元素容忍 JSON 数字/布尔（模型经常把 "20" 写成 20），按与声明式参数
// 相同的规则转成字符串；嵌套结构（数组/对象元素）拒绝——argv 元素必须是
// 标量，复杂结构应该先序列化成字符串或走 stdin。
func parseRawParams(params map[string]interface{}) ([]string, string, string, error) {
	var argv []string
	if raw, ok := params["args"]; ok && raw != nil {
		list, ok := raw.([]interface{})
		if !ok {
			return nil, "", "", fmt.Errorf("parameter %q must be an array of strings", "args")
		}
		if len(list) > MaxRawArgsCap {
			return nil, "", "", fmt.Errorf("too many arguments (%d, max %d); chain multiple calls or write a script file and run it", len(list), MaxRawArgsCap)
		}
		argv = make([]string, 0, len(list))
		for i, item := range list {
			s, err := rawArgString(item)
			if err != nil {
				return nil, "", "", fmt.Errorf("args[%d]: %w", i, err)
			}
			argv = append(argv, s)
		}
	}
	stdin := ""
	if raw, ok := params["stdin"]; ok && raw != nil {
		s, ok := raw.(string)
		if !ok {
			return nil, "", "", fmt.Errorf("parameter %q must be a string", "stdin")
		}
		if len(s) > MaxRawStdinBytes {
			return nil, "", "", fmt.Errorf("stdin payload too large (%d bytes, max %d); pass a file path instead", len(s), MaxRawStdinBytes)
		}
		stdin = s
	}
	cwd := ""
	if raw, ok := params["cwd"]; ok && raw != nil {
		s, ok := raw.(string)
		if !ok {
			return nil, "", "", fmt.Errorf("parameter %q must be a string", "cwd")
		}
		cwd = strings.TrimSpace(s)
	}
	return argv, stdin, cwd, nil
}

// rawArgString 把单个 argv 元素转成字符串。
func rawArgString(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), nil
		}
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("must be a string (got %T); serialize complex values to JSON text first", raw)
	}
}

// RequiresApproval 恒为 true。"任意子命令"意味着没人能事前断言它只读，
// read_only 与 require_approval 对 raw 工具都不生效——这是它与声明式工具
// 最大的语义差异，也是它敢存在的前提。
func (t *RawTool) RequiresApproval() bool { return true }

// ApprovalSummary 给审批卡片一句人类可读的动作描述。
// 与 Tool.ApprovalSummary 成对的原因相同：少一个，agent 侧的 approvalAware
// 断言就失败，等于静默免审批（见 cliconn_test.go 的编译期断言）。
func (t *RawTool) ApprovalSummary() string {
	return fmt.Sprintf("CLI %s → arbitrary subcommand (raw)", t.connector)
}

// ApprovalSummaryWith 把模型给的 argv 原样摊到审批卡片上。
//
// raw 的同意按钮等于"允许在这个二进制上执行任意子命令"，而卡片只写
// "arbitrary subcommand" 时，用户根本无从判断这次是 `gh pr list` 还是
// `gh repo delete`。这里不读文件、不取 token，只拼字符串。
func (t *RawTool) ApprovalSummaryWith(params map[string]interface{}) string {
	if cmd := PreviewRawCommand(t.conn, params); cmd != "" {
		return cmd
	}
	return t.ApprovalSummary()
}

// ConnectorName 返回所属连接器名（与 Tool 保持一致的查询口）。
func (t *RawTool) ConnectorName() string { return t.connector }
