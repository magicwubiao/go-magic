package agentplugin

// 凭据字段元数据：把"要填什么"和"去哪里拿"一起送到前端。
//
// 之前 /credentials 只回一串 key（`GH_TOKEN`、`JIRA_API_TOKEN`…），前端只能渲染
// 一排没有上下文的密码框——用户得自己去搜"这个 token 在哪生成"。目录里其实
// 早就登记了 Label 与 HelpURL（connectors.Requirement、clicatalog.Requirement），
// 只是没透出来。
//
// 现在的取舍很明确：**能跳转网站拿到的凭据，优先给一条直链**。前端把
// HelpURL 渲染成"去获取"按钮，用户点一下就到厂商的 token 页面，复制、粘贴、
// 保存，不用在站内翻菜单。没有 HelpURL 的（手写插件、目录没登记）退回纯输入框。

import (
	"sort"
	"strings"

	"github.com/magicwubiao/go-magic/pkg/clicatalog"
	"github.com/magicwubiao/go-magic/pkg/connectors"
)

// CredentialField 是一个"要用户自己填"的凭据项。
type CredentialField struct {
	Key string `json:"key"`
	// Label 是给人看的名字（目录登记，如 "API token"）；空时前端回退显示 Key。
	Label string `json:"label,omitempty"`
	// Secret 表示值应当隐藏显示（token / secret）。
	Secret bool `json:"secret,omitempty"`
	// Placeholder 是格式提示（如 "https://your.atlassian.net"）。
	Placeholder string `json:"placeholder,omitempty"`
	// HelpURL 是厂商页面（生成/复制凭据的地方）。空 = 目录没登记。
	HelpURL string `json:"help_url,omitempty"`
}

// CLICredentialGroup 返回 CLI 连接器在凭据表单里的分组键。
//
// 与 MCP 条目名共存于同一个 map，前缀把两类分开（同名时不会互相覆盖）。
func CLICredentialGroup(connector string) string { return "cli:" + connector }

// CredentialFields 返回插件每个凭据分组的字段元数据。
//
// 分组键与 /credentials 的 required_env 完全一致：MCP 条目名、CLI 的
// "cli:<连接器名>"。空 map 表示这个插件不需要用户填任何东西。
func CredentialFields(p *Plugin) map[string][]CredentialField {
	if p == nil {
		return nil
	}
	mcpMeta, cliMeta := catalogCredentialMeta(p)
	out := make(map[string][]CredentialField, len(p.MCPServers)+len(p.CLIConnectors))

	for _, ms := range p.MCPServers {
		keys := SpecCredentialKeys(ms.Spec)
		if len(keys) == 0 {
			continue
		}
		out[ms.Name] = fieldsForKeys(keys, mcpMeta)
	}
	for _, cs := range p.CLIConnectors {
		keys := RequiredCLIEnv(cs.Connector)
		if len(keys) == 0 {
			continue
		}
		out[CLICredentialGroup(cs.Name)] = fieldsForKeys(keys, cliMeta)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CredentialHelpURLs 汇总一组字段里出现过的获取地址（去重、保持首次出现顺序）。
//
// 前端用它决定要不要在表单顶部放一个"去 XX 获取"的主按钮：多数字段共享
// 同一个页面（如飞书的 App ID / App Secret），一个入口就够。
func CredentialHelpURLs(fields []CredentialField) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range fields {
		u := strings.TrimSpace(f.HelpURL)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// catalogCredentialMeta 取目录里登记过的字段元数据（key → 字段）。
//
// 只按目录来源取，不做跨类混用：同一个 key 在 MCP 目录与 CLI 目录里
// 可能指向不同页面（比如都叫 API_TOKEN）。
func catalogCredentialMeta(p *Plugin) (mcpMeta, cliMeta map[string]CredentialField) {
	kind, id, ok := CatalogSource(p)
	if !ok {
		return nil, nil
	}
	switch kind {
	case CatalogKindMCP:
		c, found := connectors.Get(id)
		if !found {
			return nil, nil
		}
		mcpMeta = make(map[string]CredentialField, len(c.Requires))
		for _, r := range c.Requires {
			mcpMeta[r.Key] = CredentialField{
				Key:         r.Key,
				Label:       r.Label,
				Secret:      r.Secret,
				Placeholder: r.Placeholder,
				HelpURL:     r.HelpURL,
			}
		}
	case CatalogKindCLI:
		e, found := clicatalog.Get(id)
		if !found {
			return nil, nil
		}
		cliMeta = make(map[string]CredentialField, len(e.Requires))
		for _, r := range e.Requires {
			cliMeta[r.Key] = CredentialField{
				Key:         r.Key,
				Label:       r.Label,
				Secret:      r.Secret,
				Placeholder: r.Placeholder,
				HelpURL:     r.HelpURL,
			}
		}
	}
	return mcpMeta, cliMeta
}

// fieldsForKeys 按 key 顺序产出字段；目录没登记的 key 只保留 key 本身。
func fieldsForKeys(keys []string, meta map[string]CredentialField) []CredentialField {
	out := make([]CredentialField, 0, len(keys))
	for _, k := range keys {
		if f, ok := meta[k]; ok {
			out = append(out, f)
			continue
		}
		out = append(out, CredentialField{Key: k})
	}
	// keys 已由调用方排序（envPlaceholderKeys / SpecCredentialKeys 都排过），
	// 这里再兜一次，避免 map 迭代顺序渗进来。
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
