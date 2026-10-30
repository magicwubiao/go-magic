// Package policy 是连接器的准入白名单（对应 codex 的 requirements.toml 那层）。
//
// 为什么需要：连接器配置一旦能被"别人"写入（导入别人的配置、装插件、被脚本改
// config.json），就等于把"在这台机器上执行什么程序、连哪台服务器"的决定权交出去了。
// 白名单是最后一道锁：**配置里写了什么都不算数，必须命中名单才允许落地执行**。
//
// 三条语义：
//  1. `enforce=false`（默认）时全部放行——单机自用场景不该被这层挡路。
//  2. `enforce=true` 且某类名单为空 = **该类全部禁止**（不是"不限制"）。
//     空表最容易被人当成"没配置"，而安全默认值只能是拒绝。
//  3. 名单里写 `*` = 允许该类全部（显式表达"我确实要放开"）。
package policy

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// Policy 是 config.json 里的 policy 段。
type Policy struct {
	// Enforce 打开后，下面的名单才会生效。
	Enforce bool `json:"enforce,omitempty"`

	// MCPAllowedHosts 允许 MCP(HTTP/SSE) 连接的主机，如 "api.example.com"、
	// "*.example.com"（匹配任意子域，不含裸域）。端口不参与匹配。
	MCPAllowedHosts []string `json:"mcp_allowed_hosts,omitempty"`
	// MCPAllowedCommands 允许 MCP(stdio) 启动的可执行文件，按 basename 匹配，
	// 如 "npx"、"uvx"、"/usr/local/bin/my-server"。
	MCPAllowedCommands []string `json:"mcp_allowed_commands,omitempty"`
	// CLIAllowedCommands 允许 CLI 连接器执行的命令，按 basename 匹配。
	CLIAllowedCommands []string `json:"cli_allowed_commands,omitempty"`
}

// CheckMCP 校验一个 MCP server 配置是否允许落地。
//
// url 非空按主机名单校验，否则按命令名单校验——两类入口的名单必须分开：
// 用一条 "npx" 的允许项顺带放行了任意 URL，等于把名单写成了摆设。
func (p *Policy) CheckMCP(name, command, urlStr string) error {
	if p == nil || !p.Enforce {
		return nil
	}
	if strings.TrimSpace(urlStr) != "" {
		host, err := hostOf(urlStr)
		if err != nil {
			return fmt.Errorf("MCP server %q: invalid url: %w", name, err)
		}
		if !matchHost(host, p.MCPAllowedHosts) {
			return blocked(name, "host", host, p.MCPAllowedHosts)
		}
		return nil
	}
	if !matchCommand(command, p.MCPAllowedCommands) {
		return blocked(name, "command", command, p.MCPAllowedCommands)
	}
	return nil
}

// CheckCLI 校验一个 CLI 连接器是否允许执行。
func (p *Policy) CheckCLI(name, command string) error {
	if p == nil || !p.Enforce {
		return nil
	}
	if !matchCommand(command, p.CLIAllowedCommands) {
		return blocked(name, "command", command, p.CLIAllowedCommands)
	}
	return nil
}

// blocked 生成可行动的错误信息：把"被什么挡住、名单里现在有什么"一次说清。
// 只说 "denied" 的错误在排查时会浪费一整轮。
func blocked(name, kind, got string, allow []string) error {
	if len(allow) == 0 {
		return fmt.Errorf("%s %q is blocked by policy: no %s allowlist configured (enforce=true and an empty list means deny all)", kind, got, kind)
	}
	return fmt.Errorf("%s %q of %q is not in the policy allowlist (%s)", kind, got, name, strings.Join(allow, ", "))
}

func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in %q", raw)
	}
	return host, nil
}

// matchHost 支持三种写法：精确、"*.suffix"、以及 "*"（全放行）。
// 刻意不支持 "*.example.com" 顺带匹配裸域 example.com：那是一个常见的误解，
// 让"我只放开子域"变成"我放开了整站"。要裸域就再写一行。
func matchHost(host string, allow []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, pattern := range allow {
		p := strings.ToLower(strings.TrimSpace(pattern))
		if p == "" {
			continue
		}
		if p == "*" || p == host {
			return true
		}
		if strings.HasPrefix(p, "*.") && strings.HasSuffix(host, p[1:]) {
			return true
		}
	}
	return false
}

// matchCommand 按可执行文件 basename 匹配（大小写不敏感，Windows 上路径不分大小写）。
func matchCommand(command string, allow []string) bool {
	got := strings.TrimSpace(command)
	if got == "" {
		return false
	}
	base := filepath.Base(got)
	if runtime.GOOS == "windows" {
		base = strings.ToLower(base)
		if ext := filepath.Ext(base); ext == ".exe" || ext == ".cmd" || ext == ".bat" {
			base = strings.TrimSuffix(base, ext)
		}
	}
	for _, pattern := range allow {
		p := strings.TrimSpace(pattern)
		if p == "" {
			continue
		}
		if p == "*" {
			return true
		}
		// 名单里既可以写 basename（npx），也可以写完整路径（/usr/local/bin/x）。
		cand := filepath.Base(p)
		if runtime.GOOS == "windows" {
			cand = strings.ToLower(cand)
			if ext := filepath.Ext(cand); ext == ".exe" || ext == ".cmd" || ext == ".bat" {
				cand = strings.TrimSuffix(cand, ext)
			}
		}
		if cand == base {
			return true
		}
	}
	return false
}
