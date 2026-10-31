package agentplugin

// 插件凭据占位符：`${CRED:NAME}`。
//
// 为什么需要它：mcp.json 的 env 约定是"值为空 = 需要用户填"，凭据合并时按 key
// 覆盖即可。但有些 server 把凭据放在**命令行参数**里（如飞书官方 MCP 的
// `-a <APP_ID> -s <APP_SECRET>`、Atlassian 的 `--jira-api-token=...`），
// 有些放在**HTTP 头**里。这两处原本没有任何插值能力，于是"目录条目 → 插件包"
// 就会把凭据位置漏掉。
//
// 语义：
//   - url / args / env 值 / headers 值里可以写 `${CRED:NAME}`，连接前用插件凭据替换；
//   - 同一个 key 可以出现在多处（env 与 args 同时引用同一凭据）；
//   - 未填写的凭据不做任何替换，并在连接时以 auth 类错误中止——比带着
//     `${CRED:X}` 字面量去请求、再把 401 抛给用户看要清楚得多。
//
// 与 env 空值约定的关系：env 里值为空的 key 仍然算"待填凭据"（历史行为），
// `${CRED:NAME}` 只是多了一种在 args/headers 里引用同一份凭据的写法。

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/magicwubiao/go-magic/internal/mcp"
)

// credPlaceholderRe 匹配 ${CRED:NAME}；NAME 与 env key 同字符集。
var credPlaceholderRe = regexp.MustCompile(`\$\{CRED:([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolveCredPlaceholders 把 s 里的 ${CRED:NAME} 替换成凭据值，
// 同时返回没有取到值的 key（排序去重）。
func resolveCredPlaceholders(s string, creds map[string]string) (string, []string) {
	if !strings.Contains(s, "${CRED:") {
		return s, nil
	}
	var missing []string
	out := credPlaceholderRe.ReplaceAllStringFunc(s, func(m string) string {
		name := credPlaceholderRe.FindStringSubmatch(m)[1]
		v, ok := creds[name]
		if !ok || strings.TrimSpace(v) == "" {
			missing = append(missing, name)
			return m
		}
		return v
	})
	return out, missing
}

// applyCredentials 返回叠加了用户凭据的 spec 拷贝，并报告缺失的凭据 key。
//
// 四条通道：
//   - env：凭据按 key 覆盖同名 env（历史行为，保持兼容）；
//   - url / args / env 值 / headers 值：替换 ${CRED:NAME}。
//
// URL 也支持占位是为了局域网设备：Home Assistant 这类条目的地址前缀
// （http://192.168.1.10:8123）本身就是用户要填的东西，模板里写成
// {{HASS_URL}}/api/mcp，落盘成 ${CRED:HASS_URL}/api/mcp。
//
// 不改原 spec：重连/重载都以 mcp.json 为基准重新合并。
func applyCredentials(spec MCPServerSpec, creds map[string]string) (MCPServerSpec, []string) {
	out := spec
	var missing []string

	// url：展开后由 connectEntry 复核 scheme/主机（加载期看不到真实主机）。
	resolvedURL, missURL := resolveCredPlaceholders(spec.URL, creds)
	out.URL = resolvedURL
	missing = append(missing, missURL...)

	// env：先复制再覆盖，避免与原始 spec 共享 map。
	if len(spec.Env) > 0 || len(creds) > 0 {
		env := make(map[string]string, len(spec.Env)+len(creds))
		for k, v := range spec.Env {
			env[k] = v
			if resolved, miss := resolveCredPlaceholders(v, creds); resolved != v {
				env[k] = resolved
			} else {
				missing = append(missing, miss...)
			}
		}
		for k, v := range creds {
			if strings.TrimSpace(v) == "" {
				continue
			}
			env[k] = v
		}
		out.Env = env
	}

	// args：逐项替换；必须先复制切片，spec.Args 与原始 spec 共享底层数组。
	if len(spec.Args) > 0 {
		args := make([]string, len(spec.Args))
		for i, a := range spec.Args {
			resolved, miss := resolveCredPlaceholders(a, creds)
			args[i] = resolved
			missing = append(missing, miss...)
		}
		out.Args = args
	}

	// headers：HTTP 传输的认证头。
	if len(spec.Headers) > 0 {
		headers := make(map[string]string, len(spec.Headers))
		for k, v := range spec.Headers {
			resolved, miss := resolveCredPlaceholders(v, creds)
			headers[k] = resolved
			missing = append(missing, miss...)
		}
		out.Headers = headers
	}

	if len(missing) == 0 {
		return out, nil
	}
	return out, uniqueSorted(missing)
}

// SpecCredentialKeys 返回一个 MCP 条目需要用户填写的凭据 key（升序）：
// env 里值为空的键，加上 url/args/headers/env 值里出现的 ${CRED:NAME}。
//
// 供凭据表单渲染输入框与 UI 判断"要不要亮授权按钮"——只看 env 空值会漏掉
// 参数位与请求头里的凭据。
func SpecCredentialKeys(spec MCPServerSpec) []string {
	keys := emptyEnvKeys(spec.Env)
	keys = append(keys, credRefsIn(spec.URL)...)
	for _, v := range spec.Env {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, credRefsIn(v)...)
		}
	}
	for _, a := range spec.Args {
		keys = append(keys, credRefsIn(a)...)
	}
	for _, v := range spec.Headers {
		keys = append(keys, credRefsIn(v)...)
	}
	if len(keys) == 0 {
		return nil
	}
	return uniqueSorted(keys)
}

// credRefsIn 返回字符串里引用的凭据 key。
func credRefsIn(s string) []string {
	if !strings.Contains(s, "${CRED:") {
		return nil
	}
	ms := credPlaceholderRe.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m[1])
	}
	return out
}

// missingCredsError 把缺失凭据包装成 auth 类错误，让 UI 直接亮"去授权"。
func missingCredsError(keys []string) error {
	return mcp.WrapKind(mcp.ErrKindAuth, fmt.Errorf(
		"missing credentials: %s (fill them in the authorize dialog)", strings.Join(keys, ", ")))
}

// uniqueSorted 排序并去重。
func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
