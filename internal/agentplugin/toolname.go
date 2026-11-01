package agentplugin

// 插件工具名的构造规则。
//
// 工具名会被交给 LLM provider，绝大多数 provider（OpenAI 函数调用、Anthropic
// tools）只接受 [a-zA-Z0-9_-]{1,64}：空格、中文、括号在注册时不报错，
// 直到模型真去调用才返回 400，排查成本极高。插件又是**第三方内容**，
// mcp.json / cli.json 里的名字完全由作者决定，必须在拼装处统一清洗。
//
// 早先的实现是 fmt.Sprintf("ap_%s_%s_%s", …) 直接拼接，于是：
//   - (server=a_b, tool=c) 与 (server=a, tool=b_c) 撞成同一个名字，
//     后者静默覆盖前者（Registry.Register 是 map 赋值）；
//   - 带空格/中文的工具名原样透给 provider。
//
// 现在的规则：逐段清洗 → 拼接 → 超长时保留可读前缀并用名字哈希做后缀，
// 既保证唯一性，也让模型仍然能看出这是哪个插件的工具。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// maxToolNameLen 是工具名的长度上限（OpenAI 函数名上限 64）。
const maxToolNameLen = 64

// toolNameHashLen 是超长截断时保留的哈希字符数（8 位十六进制 = 32 bit，
// 对"同一插件内几十个工具"这个量级足够避免碰撞）。
const toolNameHashLen = 8

// PluginToolName 返回插件 MCP 工具在注册表里的名字：
// ap_<plugin>_<server>_<tool>，各段已清洗并按总长截断。
func PluginToolName(pluginName, serverName, toolName string) string {
	return joinToolName("ap_", pluginName, serverName, toolName)
}

// PluginCLIToolName 返回插件 CLI 工具在注册表里的名字：
// ap_<plugin>_cli_<connector>_<tool>。
//
// 中间的 "cli" 段是刻意保留的：全局连接器的工具名是 cli_<connector>_<tool>，
// 插件版只是多一层前缀，用户在日志/审批卡片里能一眼看出这是插件带的
// CLI 工具，而不是 MCP 工具。
func PluginCLIToolName(pluginName, connectorName, toolName string) string {
	return joinToolName("ap_", pluginName, "cli", connectorName, toolName)
}

// joinToolName 按 "prefix + 各段用 _ 连接" 组装，并做清洗与长度控制。
//
// 清洗是**有损**的（"srv 1" 与 "srv-1" 都变成 "srv-1"）：只要任何一段
// 被改动过，就给结果追加原始拼接串的短哈希。这样：
//   - 本来就合法的名字保持可读（ap_demo_cli_gh_status 不变）；
//   - 有损的名字彼此不会撞车（哈希覆盖了原始分段信息）。
func joinToolName(prefix string, parts ...string) string {
	rawKey := strings.Join(parts, "\x00")
	clean := make([]string, 0, len(parts))
	lossy := false
	for _, p := range parts {
		c := sanitizeToolSegment(p)
		if c != strings.TrimSpace(p) {
			lossy = true
		}
		clean = append(clean, c)
	}
	name := prefix + strings.Join(clean, "_")
	if lossy {
		name += "_" + shortHash(rawKey)
	}
	if len(name) <= maxToolNameLen {
		return name
	}
	// 超长：保留可读前缀，用哈希（含原始分段信息）保证截断后不互撞。
	suffix := "_" + shortHash(rawKey)
	keep := maxToolNameLen - len(suffix)
	if keep <= len(prefix) {
		// 退化情况（前缀本身就超长）：直接返回"前缀+哈希"，至少保证合法。
		return prefix[:min(len(prefix), maxToolNameLen-len(suffix))] + suffix
	}
	// 避免在中间切开一个单词：回退到最后一个 '_' 再拼后缀。
	head := name[:keep]
	if idx := strings.LastIndexByte(head, '_'); idx > len(prefix) {
		head = head[:idx]
	}
	return head + suffix
}

// shortHash 返回 key 的短哈希（8 位十六进制 = 32 bit，对同一插件内
// 几十个工具这个量级足够避免碰撞）。
func shortHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:toolNameHashLen]
}

// sanitizeToolSegment 把一段名字清洗成 provider 能接受的字符集。
//
// 规则：只保留字母、数字、连字符；下划线**也转成连字符**；其余字符
// （空格、中文、斜杠、花括号…）折叠成一个连字符。
//
// 为什么下划线也要转：拼接时用的分隔符是 '_'，如果段内还能出现 '_'，
// (server=a_b, tool=c) 与 (server=a, tool=b_c) 会拼出同一个名字，后者
// 静默覆盖前者（Registry.Register 就是一次 map 赋值）。把 '_' 从段内
// 彻底消除后，'_' 只表示段边界，这个歧义就不存在了。
func sanitizeToolSegment(seg string) string {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return "x"
	}
	var b strings.Builder
	b.Grow(len(seg))
	lastDash := false
	for _, r := range seg {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		// 连续的非字母数字只产出一个连字符，避免 "a  b" → "a---b"。
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

// DescribeToolCollision 生成"工具名撞车"的可读说明，供注册日志与告警使用。
func DescribeToolCollision(name, existing, incoming string) string {
	return fmt.Sprintf("tool name %q is already registered by %s; %s will replace it — rename one of the entries", name, existing, incoming)
}
