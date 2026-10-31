package agentplugin

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// loadMCPConfig 从 pluginRoot/mcp.json 加载 MCP 配置文档。
//
// 返回 (cfg, fatalErr):
//   - 文件不存在 → (nil, nil):缺失的固定位置不是错误,调用方据此跳过 MCP 组件。
//   - 顶层文档无效(解析失败、未知顶层字段、缺 $schema 等)→ fatalErr 非 nil,整个 MCP 被禁用。
//
// 规范:mcp.json 顶层只允许 $schema 与 mcpServers。
func loadMCPConfig(pluginRoot string) (*MCPConfig, error) {
	mcpPath := filepath.Join(pluginRoot, mcpConfigName)
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 缺失固定位置:不是错误
		}
		return nil, fmt.Errorf("read %s: %w", mcpConfigName, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", mcpConfigName, err)
	}

	// 顶层闭包:只允许 $schema 与 mcpServers,其他视为致命。
	for k := range raw {
		if k != "$schema" && k != "mcpServers" {
			return nil, fmt.Errorf("%s: unknown top-level field %q", mcpConfigName, k)
		}
	}

	schemaRaw, ok := raw["$schema"]
	if !ok {
		return nil, fmt.Errorf("%s: missing required $schema", mcpConfigName)
	}
	var schemaVal string
	if err := json.Unmarshal(schemaRaw, &schemaVal); err != nil || schemaVal == "" {
		return nil, fmt.Errorf("%s: $schema must be a non-empty string", mcpConfigName)
	}
	if !isSupportedMCPSchema(schemaVal) {
		return nil, fmt.Errorf("%s: unsupported $schema %q (supported: %s)", mcpConfigName, schemaVal, SpecVersion)
	}

	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", mcpConfigName, err)
	}
	cfg.Schema = schemaVal
	return &cfg, nil
}

func isSupportedMCPSchema(schema string) bool {
	return schema == MCPSchemaURL || schema == SpecVersion ||
		strings.HasSuffix(schema, "/"+SpecVersion+"/mcp.schema.json")
}

// validateAndExpandSpec 校验单个 MCP server 条目并展开运行时变量。
//
// 规则:
//   - type 必须是 stdio/streamable-http/sse 之一。
//   - stdio:command 为单 token(不展开占位符);args/env 值/cwd 展开 ${PLUGIN_ROOT}/${PLUGIN_DATA}。
//     cwd 省略时默认插件根;显式 cwd 须 plugin-relative 或以 ${PLUGIN_ROOT}/${PLUGIN_DATA} 为根,
//     且解析后不逃逸对应目录。command 若以 ./ 开头则解析为 plugin 内路径并校验不逃逸。
//   - streamable-http/sse:url 为绝对 HTTP(S) URL,公网主机须 HTTPS(本机/局域网
//     私网地址允许明文 http,见 localOnlyHost);headers 字面值。
//   - 占位符文本展开,单次,非递归;不展开 command/url/headers/env key。
//
// 返回展开后的 spec 与该条目的错误(条目级错误仅禁用该条目,不影响其他)。
func validateAndExpandSpec(spec MCPServerSpec, pluginRoot, dataDir string) (MCPServerSpec, error) {
	switch spec.Type {
	case TransportStdio:
		if spec.OAuth != nil {
			return spec, fmt.Errorf("oauth is only supported on remote entries (streamable-http / sse)")
		}
		return expandStdioSpec(spec, pluginRoot, dataDir)
	case TransportStreamableHTTP, TransportSSE:
		return validateHTTPSpec(spec)
	default:
		return spec, fmt.Errorf("unsupported transport type %q (allowed: stdio, streamable-http, sse)", spec.Type)
	}
}

func expandStdioSpec(spec MCPServerSpec, pluginRoot, dataDir string) (MCPServerSpec, error) {
	if spec.Command == "" {
		return spec, fmt.Errorf("stdio server requires a command")
	}

	// command 不展开占位符。若以 ./ 开头,解析为插件内路径并校验不逃逸根。
	if strings.HasPrefix(spec.Command, "./") {
		resolved := filepath.Clean(filepath.Join(pluginRoot, spec.Command))
		if err := ensureWithin(resolved, pluginRoot); err != nil {
			return spec, fmt.Errorf("command path escapes plugin root: %w", err)
		}
		spec.Command = resolved
	}

	// args:展开占位符。
	for i, a := range spec.Args {
		spec.Args[i] = expandVars(a, pluginRoot, dataDir)
	}

	// env:值展开占位符,key 不展开。
	for k, v := range spec.Env {
		spec.Env[k] = expandVars(v, pluginRoot, dataDir)
	}

	// cwd:省略=插件根;展开后须在 PLUGIN_ROOT 或 PLUGIN_DATA 内。
	cwd := spec.Cwd
	if cwd == "" {
		spec.Cwd = pluginRoot
	} else {
		cwd = expandVars(cwd, pluginRoot, dataDir)
		resolved, err := resolveContainedPath(cwd, pluginRoot, dataDir)
		if err != nil {
			return spec, fmt.Errorf("cwd: %w", err)
		}
		spec.Cwd = resolved
	}

	return spec, nil
}

func validateHTTPSpec(spec MCPServerSpec) (MCPServerSpec, error) {
	if spec.URL == "" {
		return spec, fmt.Errorf("%s server requires a url", spec.Type)
	}
	// 地址本身可能是待填凭据的一部分（目录模板 {{HASS_URL}}/api/mcp 会落成
	// ${CRED:HASS_URL}/api/mcp）。此时主机名要到连接前才可知，静态校验只能
	// 放行——由 connectEntry 在展开后调用本函数复核，规则完全一致。
	if strings.Contains(spec.URL, "${CRED:") {
		return spec, nil
	}
	u, err := url.Parse(spec.URL)
	if err != nil {
		return spec, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return spec, fmt.Errorf("url scheme must be http or https, got %q", u.Scheme)
	}
	if u.User != nil {
		return spec, fmt.Errorf("url must not contain user information")
	}
	if u.Fragment != "" {
		return spec, fmt.Errorf("url must not contain a fragment")
	}
	// 公网端点须 HTTPS；本机/局域网放行明文 http（见 localOnlyHost）。
	if u.Scheme == "http" && !localOnlyHost(u.Hostname()) {
		return spec, fmt.Errorf("non-loopback url must use https (plain http is only allowed for localhost, private networks and .local hostnames)")
	}
	// headers 为字面包数据,规范要求不含凭证;此处不做内容嗅探,仅保留。
	return spec, nil
}

// localOnlyHost 判断主机名是否只可能指向本机或局域网。
//
// 为什么放行明文 http：Home Assistant、NAS、路由器、自建服务几乎都没有
// 证书，一律要求 https 等于把整个"局域网设备"类目挡在门外——用户连装都装
// 不进来。放行范围刻意收窄到"不可能解析到公网"的名字与地址段：
//
//   - 环回：localhost / *.localhost / 127.0.0.0/8 / ::1；
//   - 私有网段：10/8、172.16/12、192.168/16、fc00::/7；
//   - 链路本地：169.254/16、fe80::/10；
//   - 家用/mDNS 域后缀：.local、.lan、.home、.home.arpa、.internal；
//   - 单标签短主机名（http://nas:5244）：公网域名一定带点。
//
// 公网域名与公网 IP 仍然强制 https，避免这条豁免变成"随手关掉 TLS 校验"。
func localOnlyHost(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	for _, suffix := range []string{".local", ".lan", ".home", ".home.arpa", ".internal"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return !strings.Contains(h, ".")
}

// expandVars 单次、非递归文本展开 ${PLUGIN_ROOT} / ${PLUGIN_DATA}。
// 规范明确:展开不递归,避免注入链。
func expandVars(s, pluginRoot, dataDir string) string {
	s = strings.ReplaceAll(s, "${PLUGIN_ROOT}", pluginRoot)
	s = strings.ReplaceAll(s, "${PLUGIN_DATA}", dataDir)
	return s
}

// resolveContainedPath 解析一个声明为 plugin-relative、${PLUGIN_ROOT} 或 ${PLUGIN_DATA} 根的路径,
// 返回其绝对路径并校验不逃逸对应根目录。
//
// 规范:cwd 必须是 plugin-relative,或以 ${PLUGIN_ROOT}/${PLUGIN_DATA} 为根,且不逃逸。
func resolveContainedPath(p, pluginRoot, dataDir string) (string, error) {
	p = filepath.Clean(p)
	// 已是绝对路径:须在 dataDir 或 pluginRoot 内。
	if filepath.IsAbs(p) {
		if within(p, dataDir) {
			return p, nil
		}
		if within(p, pluginRoot) {
			return p, nil
		}
		return "", fmt.Errorf("absolute path %q escapes permitted roots", p)
	}
	// 相对路径:相对 pluginRoot 解析(规范:plugin-relative)。
	resolved := filepath.Clean(filepath.Join(pluginRoot, p))
	if err := ensureWithin(resolved, pluginRoot); err != nil {
		return "", err
	}
	return resolved, nil
}

// ensureWithin 校验 target 经 Clean 后位于 base 目录内(含 base 自身)。
func ensureWithin(target, base string) error {
	target = filepath.Clean(target)
	base = filepath.Clean(base)
	if !within(target, base) {
		return fmt.Errorf("%q escapes %q", target, base)
	}
	return nil
}

// within 判断 target 是否在 base 内(含 base 自身)。使用路径前缀比较,带分隔符避免
// /a/b 误判包含 /a/bb。
func within(target, base string) bool {
	target = filepath.Clean(target)
	base = filepath.Clean(base)
	if target == base {
		return true
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}
