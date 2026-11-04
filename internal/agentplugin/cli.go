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
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/log"
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

// Name 返回 ap_<plugin>_cli_<connector>_<tool>（各段已清洗、超长带哈希后缀）。
// 加 ap_ 前缀是为了和全局 cli.<connector> 的工具区分：同一台机器上完全可能
// 既装了全局的 gh 连接器，又装了带 gh 工具的插件。
func (t *cliPluginTool) Name() string {
	return PluginCLIToolName(t.plugin, t.Tool.ConnectorName(), t.Tool.Spec().Name)
}

// RequiresApproval 覆盖内嵌类型提升的版本：插件来源的 cli.json 是第三方
// 作者写的，read_only / require_approval 是"自声明"而非信任根——把危险命令
// 标成 read_only 就能免审批直接执行，这是明确的越权缺口。内置目录条目由
// 本仓库审校可以信任；插件条目一律逐次审批。ApprovalSummary 仍由内嵌类型
// 提供（上面方法提升），成对纪律不破。
func (t *cliPluginTool) RequiresApproval() bool { return true }

// ApprovalSummary 在原有描述前补上插件名，审批卡片上能看出是谁带的工具。
//
// ApprovalSummaryWith（拿到参数后的真实命令行）不覆盖：审批层按"首个 token
// 是可执行文件"做风险分类与模式学习，任何前缀都会让它解析错。插件名在审批
// 上下文里由工具名（ap_<plugin>_cli_...）体现。
func (t *cliPluginTool) ApprovalSummary() string {
	return fmt.Sprintf("CLI %s (plugin %s) → %s", t.Tool.ConnectorName(), t.plugin, t.Tool.Spec().Name)
}

// cliPluginRawTool 是插件内 raw 逃生工具的适配器（与 cliPluginTool 同一套
// 前缀与审批摘要纪律；RequiresApproval 由 RawTool 恒真提供）。
type cliPluginRawTool struct {
	*cliconn.RawTool
	plugin string
}

// Name 返回 ap_<plugin>_cli_<connector>_raw。
func (t *cliPluginRawTool) Name() string {
	return PluginCLIToolName(t.plugin, t.RawTool.ConnectorName(), cliconn.RawToolName)
}

// ApprovalSummary 在描述前补上插件名。
func (t *cliPluginRawTool) ApprovalSummary() string {
	return fmt.Sprintf("CLI %s (plugin %s) → arbitrary subcommand (raw)", t.RawTool.ConnectorName(), t.plugin)
}

// cliPluginCompactTool 是插件内 compact 调度工具的适配器。
//
// 调度工具能触发该连接器的任意子命令，所以审批只能取并集、且恒真：第三方作者
// 把整包声明成 read_only 就能免审批，这比单条 read_only 的口子更大。
type cliPluginCompactTool struct {
	*cliconn.CompactTool
	plugin string
}

// Name 返回 ap_<plugin>_cli_<connector>_call。
func (t *cliPluginCompactTool) Name() string {
	return PluginCLIToolName(t.plugin, t.CompactTool.ConnectorName(), cliconn.CompactToolName)
}

// RequiresApproval 恒真：与 cliPluginTool 同一口径（插件声明不是信任根）。
func (t *cliPluginCompactTool) RequiresApproval() bool { return true }

// ApprovalSummary 补上插件名。ApprovalSummaryWith（真实命令行）由内嵌类型提升，
// 不再插入任何前缀——审批层按"首 token 是可执行文件"解析命令。
func (t *cliPluginCompactTool) ApprovalSummary() string {
	return fmt.Sprintf("CLI %s (plugin %s) → all subcommands (compact)", t.CompactTool.ConnectorName(), t.plugin)
}

// pluginCLITool 是三种插件 CLI 适配器的公共视图：注册表要的 tool.Tool 加上
// 超时（tool.Tool 不含 Timeout，而插件必须把连接器的 timeout_seconds 写进
// 注册表，否则执行器会套默认 60s）。
type pluginCLITool interface {
	tool.Tool
	Timeout() time.Duration
}

// wrapPluginCLITool 给 cliconn 产出的工具套上插件命名空间。
//
// 按类型分支而不是套一个"只改 Name"的通用壳：三种工具的审批语义各不相同
// （声明工具恒真、raw 恒真、compact 取并集），通用壳会丢掉 RequiresApproval
// 的覆盖，等于让第三方声明的 read_only 重新生效。
//
// default 打日志而不是静默跳过：新增工具类型时这里必须跟着改，静默跳过会让
// "工具凭空消失"没有任何线索。
func wrapPluginCLITool(t cliconn.RegisteredTool, pluginName string) pluginCLITool {
	switch v := t.(type) {
	case *cliconn.Tool:
		return &cliPluginTool{Tool: v, plugin: pluginName}
	case *cliconn.RawTool:
		return &cliPluginRawTool{RawTool: v, plugin: pluginName}
	case *cliconn.CompactTool:
		return &cliPluginCompactTool{CompactTool: v, plugin: pluginName}
	default:
		log.Warnf("[AgentPlugin] %s: unsupported CLI tool type %T skipped", pluginName, t)
		return nil
	}
}

// PluginCLIToolNames 返回一个插件连接器会注册出的工具全名（已考虑 compact / raw）。
//
// 从 cliconn.ToolsForConnector 反推而不是重写一遍判断：插件详情页的工具清单
// 必须等于实际注册结果，否则开了 compact 之后页面还列着 5 个工具、实际只有 1 个。
func PluginCLIToolNames(pluginName string, conn cliconn.Connector, connectorName string) []string {
	tools := cliconn.ToolsForConnector(connectorName, conn)
	out := make([]string, 0, len(tools))
	prefix := cliconn.ToolPrefix(connectorName)
	for _, t := range tools {
		short := strings.TrimPrefix(t.Name(), prefix)
		if short == "" {
			continue
		}
		out = append(out, PluginCLIToolName(pluginName, connectorName, short))
	}
	return out
}

// timeoutSetter 是 *tool.Registry 上设置单工具超时的方法。
// 不直接依赖 internal/tool 的注册表实现：本包只需要 tool.Tool 这一层命名接口，
// 超时注册也走可选接口，注册表不支持时静默跳过。
type timeoutSetter interface {
	SetTimeout(name string, d time.Duration)
}

// CLITokenSourceFactory 为插件内 CLI 连接器构造执行期 token 源（OAuth 设备码）。
//
// 为什么需要工厂而不是让 cliconn 自己查：token 存在哪、键叫什么、管理器
// 什么时候可用，都是宿主（server）的知识。插件包只负责在注册工具时把源
// 挂到连接器副本上，执行期由 cliconn.Run 直接向源要 token。
//
// 参数带插件名与连接器名：同一个连接器名在不同插件里是两套凭据，键必须
// 能区分（见 CLIOAuthKey）。返回 nil 表示"这个插件不支持 OAuth"，此时
// 带 oauth 声明的连接器会在执行时明确报错，而不是静默不带 token 去跑。
type CLITokenSourceFactory func(pluginName, connectorName string) cliconn.TokenSource

// CLIRegisterOptions 是注册插件 CLI 工具的完整参数集。
type CLIRegisterOptions struct {
	// Creds 按插件名返回用户凭据（key → 值），用于填充 cli.json 里留空的 env 值。
	Creds func(pluginName string) map[string]string
	// TokenSources 为声明了 oauth 的连接器构造执行期 token 源（nil = 不支持）。
	TokenSources CLITokenSourceFactory
	// AllowCommand 是准入校验（policy.CheckCLI）。nil = 不校验。
	//
	// 为什么 CLI 必须在这里校验：插件是第三方内容，cli.json 里的 command
	// 完全由作者决定；不校验就等于"装个插件即可运行本机任意程序"，
	// 而 policy 开篇就把"装插件"列为威胁模型。返回错误时该连接器整体
	// 不注册，并把它记进条目状态（UI 能看见，不是凭空消失）。
	AllowCommand func(connectorName, command string) error
	// Profile 是当前分身名，用于过滤声明了 profiles 的连接器（空串 = 不过滤）。
	//
	// 与全局连接器同一语义：团队把插件连同 cli.json 一起分发时，
	// "这条命令只在某个分身里生效"是合理需求。
	Profile string
}

// RegisterCLITools 把插件声明的 CLI 工具注册到注册表（跳过被禁用的插件与校验失败的条目）。
func RegisterCLITools(plugins map[string]*ManagedPlugin, reg ToolRegistrar, creds func(pluginName string) map[string]string) {
	RegisterCLIToolsWithOptions(plugins, reg, CLIRegisterOptions{Creds: creds})
}

// RegisterCLIToolsWithOAuth 与 RegisterCLITools 相同，额外为声明了 oauth 的
// 连接器挂上 token 源（nil = 不挂，等价于 RegisterCLITools）。
func RegisterCLIToolsWithOAuth(plugins map[string]*ManagedPlugin, reg ToolRegistrar, creds func(pluginName string) map[string]string, tokenSources CLITokenSourceFactory) {
	RegisterCLIToolsWithOptions(plugins, reg, CLIRegisterOptions{Creds: creds, TokenSources: tokenSources})
}

// RegisterCLIToolsWithOptions 是注册插件 CLI 工具的唯一实现。
//
// creds 按插件名返回用户凭据（key → 值），用于填充 cli.json 里留空的 env 值：
// cli.json 中 `"env": ["GH_TOKEN="]` 是"需要用户填写的凭据"占位，与 mcp.json 同一套约定。
// 只覆盖已声明的 key——不把凭据里多出来的键塞进子进程环境，
// 否则用户在凭据表单里写什么就能给子进程注什么。
func RegisterCLIToolsWithOptions(plugins map[string]*ManagedPlugin, reg ToolRegistrar, opts CLIRegisterOptions) {
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
		if opts.Creds != nil {
			pluginCreds = opts.Creds(pluginName)
		}
		for i := range mp.Plugin.CLIConnectors {
			st := mp.Plugin.CLIConnectors[i]
			if st.Error != "" {
				continue
			}
			if opts.AllowCommand != nil {
				if err := opts.AllowCommand(st.Name, st.Connector.Command); err != nil {
					// 写回条目状态，而不是静默跳过：工具"凭空消失"是这类
					// 白名单最常见的可观测性问题（用户以为插件装坏了）。
					mp.Plugin.CLIConnectors[i].Error = "blocked by policy: " + err.Error()
					log.Warnf("[AgentPlugin] %s: cli connector %q blocked: %v", pluginName, st.Name, err)
					continue
				}
			}
			// 与全局连接器同一套生效规则：enabled=false、或不在当前分身里，就整体
			// 跳过。此前这里直接遍历 conn.Tools，导致这两个字段在 cli.json 里
			// "能写、能通过校验、但不生效"，与 compact 是同一类静默失效。
			if !st.Connector.AutoConnect() || !st.Connector.AppliesToProfile(opts.Profile) {
				continue
			}
			conn := withCLICredentials(st.Connector, pluginCreds)
			conn = withCLITokenSource(conn, pluginName, st.Name, opts.TokenSources)
			// 插件连接器同样在注册时固定 command 绝对路径（防 PATH 劫持；
			// 二进制未安装时保留原名，由探测报错）。
			conn = conn.PinCommandPath()
			// 工具清单由 cliconn.ToolsForConnector 统一决定（含 compact / raw）：
			// 插件侧不再自己展开 Tools，否则 compact 在插件里必然又漏一次。
			for _, lt := range cliconn.ToolsForConnector(st.Name, conn) {
				t := wrapPluginCLITool(lt, pluginName)
				if t == nil {
					continue
				}
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
			for _, spec := range st.Connector.Tools {
				u.Unregister(PluginCLIToolName(pluginName, st.Name, spec.Name))
			}
			// 不看 enabled / profiles / compact：注销要覆盖"上次注册时用的是另一套
			// 配置"（刚关掉 compact、刚从启用到禁用），按当前配置推导必然漏掉那批
			// 僵尸工具。注册表注销不存在的名字是空操作，全删一遍最省心。
			u.Unregister(PluginCLIToolName(pluginName, st.Name, cliconn.CompactToolName))
			u.Unregister(PluginCLIToolName(pluginName, st.Name, cliconn.RawToolName))
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

// withCLITokenSource 给声明了 OAuth 的连接器挂上执行期 token 源。
//
// 必须复制 OAuthSpec 再写 Tokens：cli.json 里的 OAuth 是所有工具共享的指针，
// 直接赋值会改到插件定义本身（后续 Unregister/重注册读到被污染的配置），
// 而 Tokens 是 json:"-" 的运行时字段，绝不该出现在任何序列化结果里。
func withCLITokenSource(conn cliconn.Connector, pluginName, connectorName string, factory CLITokenSourceFactory) cliconn.Connector {
	if conn.OAuth == nil || factory == nil {
		return conn
	}
	src := factory(pluginName, connectorName)
	if src == nil {
		return conn
	}
	spec := *conn.OAuth
	if len(conn.OAuth.Scopes) > 0 {
		spec.Scopes = append([]string(nil), conn.OAuth.Scopes...)
	}
	spec.Tokens = src
	conn.OAuth = &spec
	return conn
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
