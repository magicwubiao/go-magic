package agentplugin

// 凭据自动探测：能直接从本机拿到的凭据，不要让用户手打。
//
// 三类来源，按可信度从高到低：
//  1. 服务进程继承的**同名环境变量**（用户 shell 里已经配好的 token）；
//  2. 同一份 token 在其他生态里的常见变量名（GITHUB_TOKEN ↔ GH_TOKEN ↔
//     GITHUB_PERSONAL_ACCESS_TOKEN），这是"配过但名字不一样"的主要来源；
//  3. 本机已登录的官方 CLI：`gh auth token` 一条命令就能取到 GitHub token，
//     用户明明登录过，没道理再让他去网页上生成一个 PAT。
//
// 边界：
//   - 只读，不写任何配置、不改环境；
//   - 探测值只在服务端内部使用（DetectedCredential.Value 不参与 JSON），
//     API 只回"哪个 key 被自动填上了、来自哪里"，不回值本身；
//   - 单条 CLI 探测 3 秒超时，失败即当作"探测不到"，不阻塞授权流程。

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DetectedCredential 是一条自动探测结果。
//
// Value 刻意不带 json tag：它会随结构体一起被 json.Marshal 忽略，
// 保证"内部能用、响应里没有"。
type DetectedCredential struct {
	Key    string `json:"key"`
	Source string `json:"source"`
	Value  string `json:"-"`
}

// credentialAliases 是同一份凭据在不同生态里的常见名字。
//
// 只登记"确实是同一份东西"的别名：值可以在不同服务间通用才会写在这里，
// 写错一条就等于把 A 服务的 token 填进 B 服务，属于凭据串味。
var credentialAliases = map[string][]string{
	"GITHUB_PERSONAL_ACCESS_TOKEN": {"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_PAT"},
	"GITHUB_TOKEN":                 {"GH_TOKEN", "GITHUB_PERSONAL_ACCESS_TOKEN", "GITHUB_PAT"},
	"GH_TOKEN":                     {"GITHUB_TOKEN", "GITHUB_PERSONAL_ACCESS_TOKEN"},
	"GITLAB_PERSONAL_ACCESS_TOKEN": {"GITLAB_TOKEN", "GLAB_TOKEN"},
	"GITLAB_TOKEN":                 {"GITLAB_PERSONAL_ACCESS_TOKEN", "GLAB_TOKEN"},
	"SLACK_BOT_TOKEN":              {"SLACK_MCP_XOXP_TOKEN", "SLACK_TOKEN"},
	"SLACK_MCP_XOXP_TOKEN":         {"SLACK_BOT_TOKEN", "SLACK_TOKEN"},
	"NOTION_API_KEY":               {"NOTION_TOKEN", "NOTION_INTEGRATION_TOKEN"},
	"NOTION_TOKEN":                 {"NOTION_API_KEY", "NOTION_INTEGRATION_TOKEN"},
	"AIRTABLE_API_KEY":             {"AIRTABLE_PERSONAL_ACCESS_TOKEN", "AIRTABLE_TOKEN"},
	"BRAVE_API_KEY":                {"BRAVE_SEARCH_API_KEY", "BRAVE_TOKEN"},
	"TAVILY_API_KEY":               {"TAVILY_KEY"},
	"EXA_API_KEY":                  {"EXA_KEY"},
	"FIRECRAWL_API_KEY":            {"FIRECRAWL_KEY"},
	"LINEAR_API_KEY":               {"LINEAR_TOKEN"},
	"GRAFANA_API_KEY":              {"GRAFANA_TOKEN"},
	"SENTRY_AUTH_TOKEN":            {"SENTRY_TOKEN"},
	"OPENAI_API_KEY":               {"OPENAI_KEY"},
	"ANTHROPIC_API_KEY":            {"ANTHROPIC_KEY", "CLAUDE_API_KEY"},
}

// cliProbe 是一条"从已登录 CLI 直接取 token"的探测。
type cliProbe struct {
	bin  string
	args []string
}

// cliCredentialProbes 把凭据 key 映射到取 token 的命令。
//
// 只收"一条命令、非交互、直接回 token"的：`gh auth login` 这类交互式命令
// 永远留给用户自己的终端跑。
var cliCredentialProbes = map[string]cliProbe{
	"GITHUB_PERSONAL_ACCESS_TOKEN": {bin: "gh", args: []string{"auth", "token"}},
	"GITHUB_TOKEN":                 {bin: "gh", args: []string{"auth", "token"}},
	"GH_TOKEN":                     {bin: "gh", args: []string{"auth", "token"}},
}

// DetectCredentials 对给定的凭据 key 逐个探测本机已有的值。
//
// 返回 key → 探测结果；探测不到的 key 不会出现在返回里（不是错误，
// 只是"需要用户自己填"）。keys 允许重复与大小写混写。
func DetectCredentials(keys []string) map[string]DetectedCredential {
	if len(keys) == 0 {
		return nil
	}
	env := lowerEnv()
	out := make(map[string]DetectedCredential, len(keys))
	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if _, seen := out[key]; seen {
			continue
		}
		if v, src, ok := detectFromEnv(key, env); ok {
			out[key] = DetectedCredential{Key: key, Source: src, Value: v}
			continue
		}
		if v, src, ok := detectFromCLI(key); ok {
			out[key] = DetectedCredential{Key: key, Source: src, Value: v}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lowerEnv 把当前环境变量整理成"小写 key → 值"。
//
// Windows 的环境变量名本身不分大小写，用户可能写成 gh_token 或 GH_TOKEN；
// 统一转小写再比，省掉一堆大小写分支。
func lowerEnv() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		env[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return env
}

// detectFromEnv 先找同名变量，再找别名。
func detectFromEnv(key string, env map[string]string) (string, string, bool) {
	candidates := append([]string{key}, credentialAliases[strings.ToUpper(key)]...)
	for _, cand := range candidates {
		v, ok := env[strings.ToLower(cand)]
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		return strings.TrimSpace(v), "env:" + cand, true
	}
	return "", "", false
}

// probeCache 缓存同一次进程里已跑过的 CLI 探测（多个 key 共享一次 gh 调用）。
var probeCache = struct {
	mu sync.Mutex
	m  map[string]string
}{m: map[string]string{}}

// probeTimeout 是单条 CLI 探测的上限：gh auth token 正常毫秒级返回。
const probeTimeout = 3 * time.Second

// detectFromCLI 尝试用本机已登录的 CLI 取 token。
func detectFromCLI(key string) (string, string, bool) {
	probe, ok := cliCredentialProbes[strings.ToUpper(key)]
	if !ok {
		return "", "", false
	}
	token := runProbe(probe)
	if token == "" {
		return "", "", false
	}
	return token, "cli:" + probe.bin + " " + strings.Join(probe.args, " "), true
}

// runProbe 执行一条探测命令并返回首行输出；失败返回空串。
func runProbe(probe cliProbe) string {
	cacheKey := probe.bin + " " + strings.Join(probe.args, " ")
	probeCache.mu.Lock()
	if v, hit := probeCache.m[cacheKey]; hit {
		probeCache.mu.Unlock()
		return v
	}
	probeCache.mu.Unlock()

	value := ""
	if bin, err := exec.LookPath(probe.bin); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		out, err := exec.CommandContext(ctx, bin, probe.args...).Output()
		cancel()
		if err == nil {
			value = firstLine(string(out))
		}
	}

	probeCache.mu.Lock()
	probeCache.m[cacheKey] = value
	probeCache.mu.Unlock()
	return value
}

// firstLine 取输出的第一行非空内容（gh 的输出来自终端，偶尔带尾随空行）。
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// RequiredCredentialKeys 汇总一个插件里所有"需要用户填"的凭据 key
// （MCP 条目的 env 空值 + ${CRED:NAME}，CLI 连接器的 env 空值），升序去重。
//
// 供"自动授权"一次性判断还差哪些 key。
func RequiredCredentialKeys(p *Plugin) []string {
	if p == nil {
		return nil
	}
	var keys []string
	for _, ms := range p.MCPServers {
		keys = append(keys, SpecCredentialKeys(ms.Spec)...)
	}
	for _, cs := range p.CLIConnectors {
		keys = append(keys, RequiredCLIEnv(cs.Connector)...)
	}
	if len(keys) == 0 {
		return nil
	}
	return uniqueSorted(keys)
}
