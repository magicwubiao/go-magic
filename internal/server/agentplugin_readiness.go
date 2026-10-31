package server

// 插件"就绪度"：这个插件装上之后能不能真的用起来。
//
// 两类阻塞是必现且可预期的：
//   - MCP 条目缺凭据（mcp.json 里留空的 env、args/headers 里的 ${CRED:NAME}
//     还没填）——启动时必然 auth 失败；
//   - CLI 连接器的命令本机没有（插件装了，命令没装）——每次调用必然 exec 失败。
//
// 以前这两种状态下开关照样能打开，"启用"成功但工具一个都用不了，用户只能
// 从报错里自己猜原因。现在把它们算成 blockers：列表里标"未就绪"，
// 启用时先弹引导（去授权 / 去安装 / 仍然启用），把原因摆在动作之前。

import (
	"strings"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
)

// pluginBlocker 描述一条"为什么还不能用"。
type pluginBlocker struct {
	// Kind: auth（缺凭据）/ oauth（需要网页授权）/ connect（连接失败）/
	// missing_binary（命令没装）/ rejected（清单致命）。
	Kind string `json:"kind"`
	// Scope: mcp / cli / plugin。
	Scope string `json:"scope"`
	// Name 是 MCP 条目名或 CLI 连接器名。
	Name string `json:"name,omitempty"`
	// Keys 是 auth 类缺失的凭据 key。
	Keys []string `json:"keys,omitempty"`
	// Command 是 missing_binary 类缺失的命令。
	Command string `json:"command,omitempty"`
	// CatalogID 是目录条目 id：有值时前端可以一键安装。
	CatalogID string `json:"catalog_id,omitempty"`
	// Detail 是 connect/oauth 类失败的原因（透传条目 error）。
	Detail string `json:"detail,omitempty"`
}

// pluginBlockers 计算一个插件的阻塞项（空表示就绪）。
//
// 已连接的 MCP 条目不再报缺凭据：那说明凭据是从别处（比如 server 自己读到的
// 环境变量）拿到的，required_env 只是"声明了位置"，不代表一定要用户填。
func (s *Server) pluginBlockers(name string, mp *agentplugin.ManagedPlugin) []pluginBlocker {
	out := []pluginBlocker{}
	if mp == nil || mp.Plugin == nil {
		return out
	}
	p := mp.Plugin
	if p.IsRejected() {
		return append(out, pluginBlocker{Kind: "rejected", Scope: "plugin"})
	}

	creds := s.agentPluginCredentials(name)
	// 条目状态在后台连接期间会变化，走锁内快照。
	statuses := p.MCPServers
	if mp.Runtime != nil {
		statuses = mp.Runtime.EntryStatuses()
	}
	for _, ms := range statuses {
		if ms.Connected {
			continue // 已连上说明凭据与远端都到位。
		}
		var missing []string
		for _, k := range agentplugin.SpecCredentialKeys(ms.Spec) {
			if strings.TrimSpace(creds[k]) == "" {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			out = append(out, pluginBlocker{Kind: "auth", Scope: "mcp", Name: ms.Name, Keys: missing})
			continue
		}
		// 支持 OAuth 但还没授权：这才是"去点一下登录就好"，不要报成连接失败。
		if ms.OAuthAvailable && ms.OAuthState != "authorized" {
			out = append(out, pluginBlocker{Kind: "oauth", Scope: "mcp", Name: ms.Name, Detail: ms.OAuthError})
			continue
		}
		// 没缺凭据但也没连上（远端超时/网络/静态校验失败）：同样是"装了
		// 却用不了"，必须在头部就亮出来，不能等用户展开详情才发现。
		// 注意 lark 这类 auth 失败的条目也带 Error——上面 missing 分支
		// 已经兜住，不会走到这里重复报。
		if ms.Error != "" {
			out = append(out, pluginBlocker{Kind: "connect", Scope: "mcp", Name: ms.Name, Detail: ms.Error})
		}
	}

	catKind, catID, fromCatalog := agentplugin.CatalogSource(p)
	for _, cs := range p.CLIConnectors {
		if cs.Error != "" {
			continue
		}
		if agentplugin.CommandAvailable(cs.Connector.Command) {
			continue
		}
		b := pluginBlocker{
			Kind:    "missing_binary",
			Scope:   "cli",
			Name:    cs.Name,
			Command: cs.Connector.Command,
		}
		if fromCatalog && catKind == agentplugin.CatalogKindCLI {
			b.CatalogID = catID
		}
		out = append(out, b)
	}
	return out
}

// decoratePluginSummaries 给 Summary 的每一项补上 ready / blockers。
//
// 放在 server 层而不是 agentplugin.Summary：就绪度需要用户凭据（存在配置里）
// 与插件名索引，这两样只有 server 有。
func (s *Server) decoratePluginSummaries(list []map[string]any, plugins map[string]*agentplugin.ManagedPlugin) {
	for _, item := range list {
		name, _ := item["name"].(string)
		blockers := s.pluginBlockers(name, plugins[name])
		item["blockers"] = blockers
		item["ready"] = len(blockers) == 0
	}
}
