package agentplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/cliconn"
)

// cli.json 是本实现在 Agent Plugins 1.0.0 之外增加的可选组件。
//
// 为什么不塞进 plugin.json 的 extensions：extensions 的语义是"给宿主看的扩展数据"，
// 值是不透明的 any；把一整套连接器声明塞进去，校验、报错定位、UI 回显都会退化成
// 手写 map 断言。规范只约束 plugin.json / mcp.json 两个文件的内部结构，多一个
// 同层文件对其他客户端完全无副作用（它们根本不会读），本实现的实现成本却低得多。
const cliConfigName = "cli.json"

// CLIConfig 描述 cli.json 文档。
//
// $schema 可选；写了就必须与本实现支持的规范版本一致（与 mcp.json 同一套版本
// 一致性检查，避免"插件说自己是 1.0.0、CLI 组件却是别的版本"这种半新半旧状态）。
type CLIConfig struct {
	Schema     string                       `json:"$schema,omitempty"`
	Connectors map[string]cliconn.Connector `json:"connectors"`
}

// CLIConnectorStatus 是插件内一个 CLI 连接器的加载状态。
//
// Error 非空表示该连接器静态校验失败（命令为空、工具名为空、模板引用未声明参数
// 等），仅该连接器失效，插件其余组件照常工作——与 MCP 条目的失败边界一致。
type CLIConnectorStatus struct {
	Name      string
	Connector cliconn.Connector
	Error     string
}

// loadCLIConfig 读取插件根目录下的 cli.json。
//
// 返回 (cfg, err)：文件不存在时返回 (nil, nil) 表示"没有 CLI 组件"，这不是错误。
// err 非空表示整个 CLI 组件被禁用（文件损坏、版本不匹配），skills/MCP 不受影响。
func loadCLIConfig(pluginRoot, schemaURL string) (*CLIConfig, error) {
	path := filepath.Join(pluginRoot, cliConfigName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", cliConfigName, err)
	}

	var cfg CLIConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", cliConfigName, err)
	}

	// 版本一致性：plugin.json 与 cli.json 的规范版本必须相同。
	if strings.TrimSpace(cfg.Schema) != "" {
		want := schemaVersion(schemaURL)
		got := schemaVersion(cfg.Schema)
		if want != got {
			return nil, fmt.Errorf("version mismatch (plugin.json=%q, %s=%q)", want, cliConfigName, got)
		}
	}
	return &cfg, nil
}

// buildCLIEntries 逐条校验 cli.json 里的连接器声明。
//
// 条目级失败隔离：单个连接器不合法只让它自己失效，不影响其他连接器。
func buildCLIEntries(cfg *CLIConfig) []CLIConnectorStatus {
	if cfg == nil || len(cfg.Connectors) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.Connectors))
	for name := range cfg.Connectors {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]CLIConnectorStatus, 0, len(names))
	for _, name := range names {
		conn := cfg.Connectors[name]
		st := CLIConnectorStatus{Name: name, Connector: conn}
		// 连接器名会拼进工具名 ap_<plugin>_cli_<connector>_<tool>：
		// 先按 cliconn 的字符集校验，再按条目标签名校验（不能含 '/'、不能是 '..'）。
		if err := cliconn.ValidateConnectorName(name); err != nil {
			st.Error = err.Error()
		} else if err := validateEntryName(name); err != nil {
			st.Error = err.Error()
		} else if err := conn.Validate(); err != nil {
			st.Error = err.Error()
		}
		out = append(out, st)
	}
	return out
}

// cliPluginTool 是插件内 CLI 工具在注册表里的适配器。
//
// 用包装而不是给 cliconn.Tool 加前缀字段：连接器工具的名字规则（cli_<connector>_<tool>）
// 是全项目共用的，为插件场景在 cliconn 里开一个"可改名"的口子，会让任何一个
// 调用方都能改出与配置不一致的工具名。这里通过方法遮罩只改 Name，
// 其余方法（Description/Schema/Execute/Timeout/RequiresApproval/ApprovalSummary）
// 全部由内嵌类型提升——注意 RequiresApproval 与 ApprovalSummary **必须**成对提升，
// 少一个 agent 侧的 approvalAware 断言就会失败，等于静默免审批。
type cliPluginTool struct {
	*cliconn.Tool
	plugin string
}

// Name 返回 ap_<plugin>_cli_<connector>_<tool>。
// 加 ap_ 前缀是为了和全局 cli.<connector> 的工具区分：同一台机器上完全可能
// 既装了全局的 gh 连接器，又装了带 gh 工具的插件。
func (t *cliPluginTool) Name() string {
	return fmt.Sprintf("ap_%s_%s", t.plugin, t.Tool.Name())
}

// ApprovalSummary 在原有描述前补上插件名，审批卡片上能看出是谁带的工具。
func (t *cliPluginTool) ApprovalSummary() string {
	return fmt.Sprintf("CLI %s (plugin %s) → %s", t.Tool.ConnectorName(), t.plugin, t.Tool.Spec().Name)
}

// timeoutSetter 是 *tool.Registry 上设置单工具超时的方法。
// 不直接依赖 internal/tool：本包已通过 tool.Tool 命名接口做到最薄耦合，
// 超时注册也走可选接口，注册表不支持时静默跳过。
type timeoutSetter interface {
	SetTimeout(name string, d time.Duration)
}

// RegisterCLITools 把插件声明的 CLI 工具注册到注册表（跳过被禁用的插件与校验失败的条目）。
//
// creds 按插件名返回用户凭据（key → 值），用于填充 cli.json 里留空的 env 值：
// cli.json 中 `"env": ["GH_TOKEN="]` 是"需要用户填写的凭据"占位，与 mcp.json 同一套约定。
// 只覆盖已声明的 key——不把凭据里多出来的键塞进子进程环境，
// 否则用户在凭据表单里写什么就能给子进程注什么。
func RegisterCLITools(plugins map[string]*ManagedPlugin, reg ToolRegistrar, creds func(pluginName string) map[string]string) {
	if reg == nil {
		return
	}
	var setter timeoutSetter
	if s, ok := reg.(timeoutSetter); ok {
		setter = s
	}
	for pluginName, mp := range plugins {
		if mp.Disabled || mp.Plugin == nil {
			continue
		}
		var pluginCreds map[string]string
		if creds != nil {
			pluginCreds = creds(pluginName)
		}
		for _, st := range mp.Plugin.CLIConnectors {
			if st.Error != "" {
				continue
			}
			conn := withCLICredentials(st.Connector, pluginCreds)
			for _, spec := range conn.Tools {
				t := &cliPluginTool{Tool: cliconn.NewTool(st.Name, conn, spec), plugin: pluginName}
				reg.Register(t)
				if setter != nil {
					// 不注册的话执行器会套 tool.DefaultTimeout（60s），
					// 连接器里配的 timeout_seconds 就永远不生效。
					setter.SetTimeout(t.Name(), t.Timeout())
				}
			}
		}
	}
}

// UnregisterCLITools 注销插件 CLI 工具。
//
// 名字可完全由插件定义推导（不像 MCP 工具要依赖运行时记录的已注册名单），
// 因此这里不需要旧插件集的运行时，只要 Plugin 结构还在就能清理干净。
func UnregisterCLITools(plugins map[string]*ManagedPlugin, reg ToolRegistrar) {
	if reg == nil {
		return
	}
	u, ok := reg.(ToolUnregistrar)
	if !ok {
		return
	}
	for pluginName, mp := range plugins {
		if mp.Plugin == nil {
			continue
		}
		for _, st := range mp.Plugin.CLIConnectors {
			if st.Error != "" {
				continue
			}
			for _, spec := range st.Connector.Tools {
				u.Unregister(fmt.Sprintf("ap_%s_%s", pluginName, cliconn.ToolName(st.Name, spec.Name)))
			}
		}
	}
}

// RequiredCLIEnv 返回连接器声明里"值为空、需要用户填"的 env 键（升序）。
// 供凭据表单渲染输入框：没有它，纯 CLI 插件在凭据弹窗里会是一片空白。
func RequiredCLIEnv(conn cliconn.Connector) []string { return envPlaceholderKeys(conn.Env) }

// envPlaceholderKeys 返回 env 中值为空的键（升序）。
//
// 约定：cli.json 里 `"env": ["GH_TOKEN="]` 表示"这个凭据要用户填"。
// 值为空不是错误——它是 UI 上要渲染成输入框的那一项。
func envPlaceholderKeys(env []string) []string {
	var keys []string
	for _, kv := range env {
		key, val, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		if strings.TrimSpace(val) == "" {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	sort.Strings(keys)
	return keys
}

// withCLICredentials 用用户凭据填充/覆盖连接器 env 中已声明的键。
//
// 大小写不敏感匹配（Windows 的环境变量名不分大小写，凭据表单里写 gh_token
// 与 cli.json 里的 GH_TOKEN 应该能对上）。
func withCLICredentials(conn cliconn.Connector, creds map[string]string) cliconn.Connector {
	if len(creds) == 0 || len(conn.Env) == 0 {
		return conn
	}
	lower := make(map[string]string, len(creds))
	for k, v := range creds {
		lower[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	env := make([]string, 0, len(conn.Env))
	changed := false
	for _, kv := range conn.Env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			env = append(env, kv)
			continue
		}
		if v, hit := lower[strings.ToUpper(strings.TrimSpace(key))]; hit {
			env = append(env, strings.TrimSpace(key)+"="+v)
			changed = true
			continue
		}
		env = append(env, kv)
	}
	if !changed {
		return conn
	}
	out := conn
	out.Env = env
	return out
}
