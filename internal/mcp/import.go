package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// JSON 导入：把外部生态里流通的 MCP 配置片段解析成本项目的 ServerConfig。
//
// 之所以要专门写一层解析，是因为"粘贴一段 JSON 就能加服务器"是这个生态的
// 事实标准（Claude Desktop / Cursor / Cline 的 mcpServers 片段），而各家写
// 法并不统一：传输字段有 transport / type 两种叫法（取值有 stdio / sse /
// http / streamable-http），env 有 {"K":"V"} 与 ["K=V"] 两种形状，args 有数组
// 和整串两种，服务器名可能写在 key 上也可能写在对象里。这里统一收口，让
// HTTP API、CLI 和文档共用同一份宽容度定义。

// serverNamePattern 约束服务器名：它会成为 config.json 里的 map key、磁盘上
// 的定位信息，以及模型看到的工具名前缀（mcp_<server>_<tool>），所以只允许
// 字母数字开头 + 字母数字/-/_/. 。
var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// NamedServerConfig 是"服务器名 + 归一化后的配置"。JSON 导入天然是多服务器的，
// 所以解析结果用切片而不是 map，保持确定的顺序（便于测试与错误定位）。
type NamedServerConfig struct {
	Name   string
	Config ServerConfig
}

// ParseServersJSON 解析一段 MCP 配置 JSON，返回其中的全部服务器。
//
// 接受的形态：
//
//  1. {"mcpServers": {"<name>": {...}}}   外部生态标准写法
//  2. {"servers": {"<name>": {...}}}      本项目 config.json 的 mcp 段
//  3. {"mcp": {"servers": {...}}}         从 config.json 里剪下来的片段
//  4. {"<name>": {...}, ...}              裸的 name → 配置 map
//  5. {"name": "x", "command": "npx"}     单个服务器（必须自带 name）
//  6. [{"name": "x", ...}, ...]           数组形态
//
// 单条配置里 env 可以是对象或 "K=V" 数组，transport 也可以用 type 表达。
// 标记了 "disabled": true 的条目会被跳过（与外部客户端的语义一致）。
func ParseServersJSON(data []byte) ([]NamedServerConfig, error) {
	cleaned := bytes.TrimSpace(stripBOM(data))
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("JSON is empty")
	}

	var raw interface{}
	if err := json.Unmarshal(stripJSONComments(cleaned), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	entries, err := collectServerEntries(raw)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no MCP server found: expected a \"mcpServers\" object, a name → config map, or an array of servers")
	}

	out := make([]NamedServerConfig, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		name := strings.TrimSpace(e.name)
		if name == "" {
			return nil, fmt.Errorf(`server name is missing: add a "name" field, or use the {"mcpServers": {"<name>": {...}}} form`)
		}
		if !serverNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid server name %q: use letters, digits, '-', '_' or '.' (max 64 chars, must start with a letter or digit)", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate server name %q", name)
		}
		seen[name] = true

		cfg, disabled, err := normalizeServerConfig(name, e.value)
		if err != nil {
			return nil, err
		}
		if disabled {
			continue
		}
		out = append(out, NamedServerConfig{Name: name, Config: cfg})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("all MCP servers in the JSON are disabled")
	}
	return out, nil
}

// ParseServerConfigJSON 解析"只该有一个服务器"的配置（PUT /api/mcp/servers/{name}
// 的请求体，以及 CLI 里带 --json 的单服务器写法）。
//
// 与批量解析的区别是它容忍请求体里不写服务器名 —— 表单提交的 body 就只是
// {"command": ..., "args": [...]}，名字来自 URL，此时用 fallbackName 补齐。
// 返回的名字可能与 fallbackName 不同（body 里显式写了 name/key），由调用方
// 决定是否报错。
func ParseServerConfigJSON(data []byte, fallbackName string) (string, ServerConfig, error) {
	cleaned := bytes.TrimSpace(stripBOM(data))
	if len(cleaned) == 0 {
		return "", ServerConfig{}, fmt.Errorf("JSON is empty")
	}

	var raw interface{}
	if err := json.Unmarshal(stripJSONComments(cleaned), &raw); err != nil {
		return "", ServerConfig{}, fmt.Errorf("invalid JSON: %w", err)
	}
	obj, ok := raw.(map[string]interface{})
	if !ok {
		return "", ServerConfig{}, fmt.Errorf("expected a JSON object describing one MCP server, got %s", jsonTypeName(raw))
	}

	// {"mcpServers": {...}} 这类容器形态也接受，但必须只有一个。
	if !looksLikeServerObject(obj) {
		entries, err := collectObjectEntries(obj)
		if err != nil {
			return "", ServerConfig{}, err
		}
		if len(entries) != 1 {
			return "", ServerConfig{}, fmt.Errorf("expected exactly one MCP server, got %d; use POST /api/mcp/servers to add several at once", len(entries))
		}
		return finishSingleServer(entries[0].name, entries[0].value)
	}

	name := strings.TrimSpace(stringField(obj, "name"))
	if name == "" {
		name = strings.TrimSpace(fallbackName)
	}
	if name == "" {
		return "", ServerConfig{}, fmt.Errorf(`server name is missing: add a "name" field to the object`)
	}
	return finishSingleServer(name, obj)
}

func finishSingleServer(name string, raw interface{}) (string, ServerConfig, error) {
	name = strings.TrimSpace(name)
	if !serverNamePattern.MatchString(name) {
		return "", ServerConfig{}, fmt.Errorf("invalid server name %q: use letters, digits, '-', '_' or '.' (max 64 chars, must start with a letter or digit)", name)
	}
	cfg, disabled, err := normalizeServerConfig(name, raw)
	if err != nil {
		return "", ServerConfig{}, err
	}
	if disabled {
		return name, ServerConfig{}, fmt.Errorf("server %q is marked disabled", name)
	}
	return name, cfg, nil
}

func stringField(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

type rawServerEntry struct {
	name  string
	value interface{}
}

func collectServerEntries(raw interface{}) ([]rawServerEntry, error) {
	switch v := raw.(type) {
	case map[string]interface{}:
		return collectObjectEntries(v)
	case []interface{}:
		entries := make([]rawServerEntry, 0, len(v))
		for i, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("entry #%d must be an object, got %s", i+1, jsonTypeName(item))
			}
			name, _ := m["name"].(string)
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf(`entry #%d has no "name"`, i+1)
			}
			entries = append(entries, rawServerEntry{name: name, value: m})
		}
		return entries, nil
	default:
		return nil, fmt.Errorf("expected a JSON object or an array of servers, got %s", jsonTypeName(raw))
	}
}

func collectObjectEntries(m map[string]interface{}) ([]rawServerEntry, error) {
	// 容器形态优先：{"mcpServers": {...}} / {"servers": {...}} / {"mcp": {"servers": {...}}}
	for _, key := range []string{"mcpServers", "mcp_servers", "servers"} {
		inner, ok := m[key]
		if !ok {
			continue
		}
		container, ok := inner.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%q must be an object mapping server names to configs, got %s", key, jsonTypeName(inner))
		}
		return sortedEntries(container), nil
	}
	if inner, ok := m["mcp"]; ok {
		nested, ok := inner.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf(`"mcp" must be an object, got %s`, jsonTypeName(inner))
		}
		return collectObjectEntries(nested)
	}

	// 单个服务器：靠"配置特征字段"识别，而不是靠 name 的缺失。
	if looksLikeServerObject(m) {
		name, _ := m["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf(`server name is missing: add "name" to the object, or wrap it as {"mcpServers": {"<name>": {...}}}`)
		}
		return []rawServerEntry{{name: name, value: m}}, nil
	}

	// 裸的 name → 配置 map
	for _, e := range sortedEntries(m) {
		if _, ok := e.value.(map[string]interface{}); !ok {
			return nil, fmt.Errorf("server %q must be an object with \"command\" or \"url\", got %s", e.name, jsonTypeName(e.value))
		}
	}
	return sortedEntries(m), nil
}

// looksLikeServerObject 判断一个对象本身是不是"一个服务器配置"，而不是
// name → 配置 的 map。判据是它带有配置字段（command/url/env/args/type…）。
func looksLikeServerObject(m map[string]interface{}) bool {
	for _, key := range []string{
		"command", "args", "env", "url", "baseUrl", "base_url",
		"serverUrl", "server_url", "endpoint", "transport", "type",
	} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

// sortedEntries 按名字排序，保证解析顺序稳定（map 迭代是随机的）。
func sortedEntries(m map[string]interface{}) []rawServerEntry {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]rawServerEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, rawServerEntry{name: name, value: m[name]})
	}
	return entries
}

func normalizeServerConfig(name string, raw interface{}) (ServerConfig, bool, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return ServerConfig{}, false, fmt.Errorf("server %q: expected an object, got %s", name, jsonTypeName(raw))
	}
	if disabled, _ := m["disabled"].(bool); disabled {
		return ServerConfig{}, true, nil
	}
	if enabled, ok := m["enabled"].(bool); ok && !enabled {
		return ServerConfig{}, true, nil
	}

	var cfg ServerConfig

	// command：字符串，或 ["npx", "-y", "pkg"] 数组形态（首项即命令）。
	switch v := m["command"].(type) {
	case nil:
	case string:
		cfg.Command = strings.TrimSpace(v)
	case []interface{}:
		parts, err := toStringSlice(v)
		if err != nil {
			return ServerConfig{}, false, fmt.Errorf("server %q: %w", name, err)
		}
		if len(parts) > 0 {
			cfg.Command = strings.TrimSpace(parts[0])
			cfg.Args = append(cfg.Args, parts[1:]...)
		}
	default:
		return ServerConfig{}, false, fmt.Errorf("server %q: \"command\" must be a string or an array of strings", name)
	}

	// args：数组，或一整串（按 shell 规则切分，支持引号）。
	switch v := m["args"].(type) {
	case nil:
	case []interface{}:
		parts, err := toStringSlice(v)
		if err != nil {
			return ServerConfig{}, false, fmt.Errorf("server %q: %w", name, err)
		}
		cfg.Args = append(cfg.Args, parts...)
	case string:
		cfg.Args = append(cfg.Args, SplitCommandLine(v)...)
	default:
		return ServerConfig{}, false, fmt.Errorf("server %q: \"args\" must be an array or a string", name)
	}

	if v, ok := m["env"]; ok {
		env, err := normalizeEnv(v)
		if err != nil {
			return ServerConfig{}, false, fmt.Errorf("server %q: %w", name, err)
		}
		cfg.Env = env
	}

	for _, key := range []string{"url", "baseUrl", "base_url", "serverUrl", "server_url", "endpoint"} {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			cfg.URL = strings.TrimSpace(s)
			break
		}
	}

	rawTransport := ""
	for _, key := range []string{"transport", "type"} {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			rawTransport = strings.TrimSpace(s)
			break
		}
	}
	transport, err := NormalizeTransport(rawTransport)
	if err != nil {
		return ServerConfig{}, false, fmt.Errorf("server %q: %w", name, err)
	}
	cfg.Transport = transport

	// 没写传输方式时按字段推断：有 command 就是 stdio，有 url 就是 sse。
	if cfg.Transport == "" {
		switch {
		case cfg.Command != "":
			cfg.Transport = "stdio"
		case cfg.URL != "":
			cfg.Transport = "sse"
		default:
			return ServerConfig{}, false, fmt.Errorf("server %q: needs either \"command\" (stdio) or \"url\" (sse)", name)
		}
	}
	switch cfg.Transport {
	case TransportStdio:
		if cfg.Command == "" {
			return ServerConfig{}, false, fmt.Errorf("server %q: stdio transport requires \"command\"", name)
		}
	case TransportSSE:
		if cfg.URL == "" {
			return ServerConfig{}, false, fmt.Errorf("server %q: sse transport requires \"url\"", name)
		}
	}

	return cfg, false, nil
}

// NormalizeTransport 把外部写法归一到本项目支持的两种传输方式。
//
// 空字符串返回空（由调用方按字段推断）。http / streamable-http 一律映射到
// sse：本项目的 SSETransport 实际是"POST JSON-RPC，读 SSE 响应"，
// NewSSETransport 直接用 config.URL 发 POST，与 streamable HTTP 端点线级兼容，
// 所以没必要为它再引入第三种传输。
func NormalizeTransport(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case TransportStdio:
		return TransportStdio, nil
	case TransportSSE:
		return TransportSSE, nil
	case "http", "https", "streamable-http", "streamable_http", "streamablehttp", "sse+http":
		return TransportSSE, nil
	default:
		return "", fmt.Errorf("unsupported transport %q: use \"stdio\" or \"sse\" (http/streamable-http are treated as sse)", raw)
	}
}

// normalizeEnv 接受 {"K":"V"} 与 ["K=V"] 两种写法，统一成 ServerConfig.Env 的
// "K=V" 切片。对象形态按 key 排序输出，避免同一份配置每次落盘顺序都不同。
func normalizeEnv(raw interface{}) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		env := make([]string, 0, len(keys))
		for _, k := range keys {
			val, err := scalarToString(v[k])
			if err != nil {
				return nil, fmt.Errorf("env %q: %w", k, err)
			}
			if strings.TrimSpace(k) == "" {
				return nil, fmt.Errorf("env contains an empty key")
			}
			env = append(env, k+"="+val)
		}
		return env, nil
	case []interface{}:
		env := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("env entry #%d must be a \"KEY=value\" string, got %s", i+1, jsonTypeName(item))
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !strings.Contains(s, "=") {
				return nil, fmt.Errorf("env entry #%d (%q) must be in \"KEY=value\" form", i+1, s)
			}
			env = append(env, s)
		}
		if len(env) == 0 {
			return nil, nil
		}
		return env, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		if !strings.Contains(s, "=") {
			return nil, fmt.Errorf("env %q must be in \"KEY=value\" form", s)
		}
		return []string{s}, nil
	default:
		return nil, fmt.Errorf(`"env" must be an object or a "KEY=value" array, got %s`, jsonTypeName(raw))
	}
}

// EnvMapToSlice 把 {"K":"V"} 转成排序后的 "K=V" 切片（CLI --env 与 API 共用）。
func EnvMapToSlice(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+m[k])
	}
	return env
}

// EnvSliceToMap 把 "K=V" 切片转成 map（后者覆盖前者；无 "=" 的项被忽略）。
func EnvSliceToMap(env []string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SplitCommandLine 按 shell 规则切分一行参数，支持单引号与双引号。
// 用于 "args": "-y @modelcontextprotocol/server-filesystem /data" 这种整串写法，
// 以及参数里带空格的路径：`"C:\Program Files\node\node.exe" -v`。
//
// 反斜杠的处理刻意区别于 POSIX shell：双引号内只把 \" 与 \\ 当转义，其余反斜杠
// 原样保留 —— 否则 Windows 路径（C:\Program Files\...）会被吃成 C:Program Files...。
func SplitCommandLine(s string) []string {
	runes := []rune(s)
	var (
		fields  []string
		current strings.Builder
		quote   rune
		started bool
	)
	flush := func() {
		if started {
			fields = append(fields, current.String())
			current.Reset()
			started = false
		}
	}

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			started = true
		case quote == '"':
			switch {
			case r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
				current.WriteRune(runes[i+1])
				i++
			case r == '"':
				quote = 0
			default:
				current.WriteRune(r)
			}
			started = true
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == '\\' && i+1 < len(runes):
			current.WriteRune(runes[i+1])
			i++
			started = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			current.WriteRune(r)
			started = true
		}
	}
	flush()
	return fields
}

// toStringSlice 把 JSON 数组转成字符串切片，元素必须是字符串。
func toStringSlice(v []interface{}) ([]string, error) {
	out := make([]string, 0, len(v))
	for i, item := range v {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("entry #%d must be a string, got %s", i+1, jsonTypeName(item))
		}
		out = append(out, s)
	}
	return out, nil
}

// scalarToString 宽容地接受 env 里的数字/布尔值（{"PORT": 8080}）。
func scalarToString(v interface{}) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	case bool:
		if t {
			return "true", nil
		}
		return "false", nil
	case float64:
		return trimFloat(t), nil
	default:
		return "", fmt.Errorf("value must be a string, got %s", jsonTypeName(v))
	}
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%g", f)
	// %g 对大整数会退化成科学计数法，env 里更常见的期望是十进制。
	if strings.ContainsAny(s, "eE") {
		return fmt.Sprintf("%.0f", f)
	}
	return s
}

func jsonTypeName(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]interface{}:
		return "an object"
	case []interface{}:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

// stripJSONComments 去掉 // 行注释、/* */ 块注释与尾随逗号，让用户可以把
// jsonc 片段（本项目 config.json 与文档里的示例就是 jsonc）原样粘进来。
// 字符串内部的 // 与 , 会被正确跳过。
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}

		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i < len(data) && !(data[i] == '*' && i+1 < len(data) && data[i+1] == '/') {
				i++
			}
			i++ // 停在收尾的 '/' 上，外层 i++ 会跳过它
		case c == ',':
			j := i + 1
			for j < len(data) && isJSONSpace(data[j]) {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue // 尾随逗号，直接丢掉
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
