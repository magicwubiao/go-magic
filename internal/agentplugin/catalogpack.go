package agentplugin

// 目录插件包（Catalog Packs）：把内置的 MCP 连接器目录（pkg/connectors）与
// CLI 应用目录（pkg/clicatalog）里的每一条，物化成一个**真正的 Agent Plugin**。
//
// 为什么不再"目录只做预填表单"：
//   - 用户需要的是"装上就能用"，而不是"先选条目、再理解命令/参数/凭据该填哪"；
//   - 目录条目装上之后应该和手写插件一样可启用/禁用/卸载/授权，走进同一套
//     生命周期，页面也只需要一套卡片与一套状态；
//   - 物化后的包是普通文件，用户/团队可以拷走、改参数、二次分发。
//
// 生成规则（保持"目录是唯一事实来源"）：
//   - 插件名 = "<kind>-<id>"（如 mcp-lark / cli-gh），与官方技能包名不冲突；
//   - MCP 条目 → plugin.json + mcp.json（单 server）；需要凭据的位置写成占位：
//     env 值为空、args/headers 里写 ${CRED:NAME}（见 credref.go）；
//   - CLI 条目 → plugin.json + cli.json（单连接器）；Requires 写成空 env 占位；
//   - 写盘后立刻用 Load 走一遍完整校验，坏包当场删除并报错，不留在扫描目录里。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/clicatalog"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/connectors"
)

// 目录种类。
const (
	CatalogKindMCP = "mcp"
	CatalogKindCLI = "cli"
)

// catalogPackExtKey 是 plugin.json 里记录来源的扩展命名空间。
// 纯元数据：客户端不消费也不影响加载；留着是为了回答"这个插件是从哪来的"。
const catalogPackExtKey = "io.github.magicwubiao.catalog"

// CatalogPack 是目录中的一条可安装插件（列表用，不含命令细节之外的敏感信息）。
type CatalogPack struct {
	Kind string `json:"kind"` // mcp | cli
	ID   string `json:"id"`
	// Name 是物化后的插件名，也是目录名（前端据此判断"已安装"）。
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"`
	// CategoryLabel 是分类的展示文案（目录里是英文，前端不再维护一份映射）。
	CategoryLabel string   `json:"category_label,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	// Transport 仅 MCP 条目有值（stdio / streamable-http / sse），纯展示。
	Transport string `json:"transport,omitempty"`
	// RequiredEnv 是安装后需要用户填写的凭据 key（MCP: env 空值 + ${CRED:}；CLI: Requires）。
	RequiredEnv []string `json:"required_env,omitempty"`
	// OAuth 表示该条目走 OAuth 授权（安装后点"登录"即可，无需填凭据）。
	OAuth bool `json:"oauth,omitempty"`
	// OAuthScopes 是请求的权限范围（仅展示）。
	OAuthScopes []string `json:"oauth_scopes,omitempty"`
	// Args 是 stdio 模板参数（仅供"调整参数"预览与覆盖安装）。
	Args []string `json:"args,omitempty"`
	// Tools 是 CLI 条目的工具名（MCP 工具要连上才知道，这里为空）。
	Tools []string `json:"tools,omitempty"`
	// CLI 专属：命令名、本机是否已安装、安装/登录命令（仅展示）。
	Binary          string   `json:"binary,omitempty"`
	BinaryFound     bool     `json:"binary_found"`
	InstallCommands []string `json:"install_commands,omitempty"`
	LoginCommands   []string `json:"login_commands,omitempty"`
	// LoginExec 非空表示登录可由服务端代跑（目录已登记非交互 argv），
	// 前端据此显示"一键登录"而不是只给一条要复制的命令。
	LoginExec []string `json:"login_exec,omitempty"`
	DocsURL   string   `json:"docs_url,omitempty"`
	// Installed 报告扫描目录下是否已有同名插件。
	Installed bool `json:"installed"`
}

// CatalogPacks 返回目录里的可安装插件列表。kind 为空表示两类都返回。
func CatalogPacks(kind string) []CatalogPack {
	var out []CatalogPack
	if kind == "" || kind == CatalogKindMCP {
		out = append(out, mcpCatalogPacks()...)
	}
	if kind == "" || kind == CatalogKindCLI {
		out = append(out, cliCatalogPacks()...)
	}
	return out
}

// mcpCatalogPacks 把 MCP 连接器目录转成插件列表。
func mcpCatalogPacks() []CatalogPack {
	labels := map[string]string{}
	for _, c := range connectors.Categories() {
		labels[c.ID] = c.Label
	}
	entries := connectors.All()
	out := make([]CatalogPack, 0, len(entries))
	for _, c := range entries {
		spec := buildMCPSpec(c, nil)
		pack := CatalogPack{
			Kind:          CatalogKindMCP,
			ID:            c.ID,
			Name:          CatalogPackName(CatalogKindMCP, c.ID),
			DisplayName:   c.Name,
			Description:   c.Description,
			Category:      c.Category,
			CategoryLabel: labels[c.Category],
			Tags:          c.Tags,
			Transport:     spec.Type,
			RequiredEnv:   SpecCredentialKeys(mcpOutToSpec(spec)),
			Args:          spec.Args,
			DocsURL:       c.DocsURL,
			Installed:     catalogPackInstalled(CatalogKindMCP, c.ID),
		}
		if spec.OAuth != nil {
			// OAuth 条目不需要用户填任何凭据；凭据 key 列表留给前端决定
			// 是否展示"授权"而不是"填表"。
			pack.OAuth = true
			pack.OAuthScopes = append([]string(nil), spec.OAuth.Scopes...)
			pack.RequiredEnv = nil
		}
		out = append(out, pack)
	}
	return out
}

// cliCatalogPacks 把 CLI 应用目录转成插件列表。
func cliCatalogPacks() []CatalogPack {
	entries := clicatalog.All()
	labels := map[string]string{}
	for _, c := range clicatalog.Categories() {
		labels[c.ID] = c.Label
	}
	// LookPath 在 Windows 上要扫完整条 PATH（找不到的命令最慢，每条几百毫秒），
	// 串行跑 14 条就是好几秒——前端"CLI 应用渲染慢"就是它。并行探测取最慢一条。
	found := make([]bool, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		wg.Add(1)
		go func(i int, bin string) {
			defer wg.Done()
			_, err := exec.LookPath(bin)
			found[i] = err == nil
		}(i, e.Connector.Command)
	}
	wg.Wait()

	out := make([]CatalogPack, 0, len(entries))
	for i, e := range entries {
		conn := buildCLIConnector(e)
		tools := make([]string, 0, len(conn.Tools))
		for _, t := range conn.Tools {
			tools = append(tools, t.Name)
		}
		bin := e.Connector.Command
		out = append(out, CatalogPack{
			Kind:          CatalogKindCLI,
			ID:            e.ID,
			Name:          CatalogPackName(CatalogKindCLI, e.ID),
			DisplayName:   e.Name,
			Description:   e.Description,
			Category:      e.Category,
			CategoryLabel: labels[e.Category],
			Tags:          e.Tags,
			RequiredEnv:   envPlaceholderKeys(conn.Env),
			Tools:         tools,

			Binary:          bin,
			BinaryFound:     found[i],
			InstallCommands: e.Install.CommandsFor(runtime.GOOS),
			LoginCommands:   e.Login.CommandsFor(runtime.GOOS),
			LoginExec:       append([]string(nil), e.LoginExec...),
			DocsURL:         e.DocsURL,
			Installed:       catalogPackInstalled(CatalogKindCLI, e.ID),
		})
	}
	return out
}

// CatalogSource 返回插件是从哪个目录条目物化出来的（非目录插件 ok=false）。
//
// 目录装出来的插件在 plugin.json 的 extensions 里留了来源标记
// （见 catalogPackExtKey），UI 据此把"命令未安装"的 CLI 连接器接回目录里的
// 安装命令——用户不需要自己去找 winget / brew 的包名。
func CatalogSource(p *Plugin) (kind, id string, ok bool) {
	if p == nil || p.Extensions == nil {
		return "", "", false
	}
	raw, exists := p.Extensions[catalogPackExtKey]
	if !exists {
		return "", "", false
	}
	m, isMap := raw.(map[string]any)
	if !isMap {
		return "", "", false
	}
	kind, _ = m["kind"].(string)
	id, _ = m["id"].(string)
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(id) == "" {
		return "", "", false
	}
	return kind, id, true
}

// CatalogPackName 返回目录条目物化后的插件名（kind-id）。
// 前缀把两类目录分开，避免与手工创建的插件或连接器撞名。
func CatalogPackName(kind, id string) string {
	return kind + "-" + sanitizePackSegment(id)
}

// sanitizePackSegment 只保留小写字母、数字、点与连字符：
// plugin 名规则比目录 ID 严格（不能有大写、下划线），这里统一归一化。
func sanitizePackSegment(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		out = "pack"
	}
	return out
}

// catalogPackInstalled 判断插件是否已物化到扫描目录。
func catalogPackInstalled(kind, id string) bool {
	_, ok := InstalledDir(CatalogPackName(kind, id))
	return ok
}

// InstallCatalogPack 把目录条目物化成一个插件包并返回插件目录。
//
// argsOverride 仅对 stdio 类 MCP 条目生效（空表示用目录模板）：postgres、
// sqlite、filesystem 这类条目的参数里有必须由用户填的路径/连接串，
// 目录里只能给示例值，安装时需要允许覆盖。
//
// 安装完成后会用 Load 做一次完整静态校验；校验不通过的包立即删除并返回错误，
// 绝不把一个坏插件留在扫描目录里（它会在下次 reload 时变成一条错误卡片）。
func InstallCatalogPack(kind, id string, argsOverride []string) (dir, name string, required []string, err error) {
	kind = strings.TrimSpace(kind)
	id = strings.TrimSpace(id)
	name = CatalogPackName(kind, id)
	if err := validatePluginName(name); err != nil {
		return "", "", nil, fmt.Errorf("invalid pack name %q: %w", name, err)
	}

	files, required, err := buildCatalogPackFiles(kind, id, argsOverride)
	if err != nil {
		return "", "", nil, err
	}

	scanDir := DefaultScanDir()
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		return "", "", nil, fmt.Errorf("ensure scan dir: %w", err)
	}
	dir = filepath.Join(scanDir, name)
	if _, statErr := os.Stat(dir); statErr == nil {
		return "", "", nil, fmt.Errorf("plugin %q already exists, uninstall first", name)
	}

	if err := writePackFiles(dir, files); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", nil, err
	}

	// 立刻走一遍真实加载：清单/组件/条目级校验都在这里现形。
	p := Load(dir, filepath.Join(config.GetMagicHome(), "agent-plugins-data", name))
	if err := verifyLoadedPack(p); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", nil, err
	}
	return dir, name, required, nil
}

// verifyLoadedPack 检查加载结果是否真的可用（而不是"没崩"）。
func verifyLoadedPack(p *Plugin) error {
	if p == nil {
		return fmt.Errorf("plugin did not load")
	}
	if p.FatalError != "" {
		return fmt.Errorf("generated plugin is invalid: %s", p.FatalError)
	}
	if p.MCPDisabled {
		return fmt.Errorf("generated mcp.json is invalid: %s", strings.Join(p.Warnings, "; "))
	}
	if p.CLIDisabled {
		return fmt.Errorf("generated cli.json is invalid: %s", strings.Join(p.Warnings, "; "))
	}
	for _, ms := range p.MCPServers {
		if ms.Error != "" {
			return fmt.Errorf("generated MCP entry %q is invalid: %s", ms.Name, ms.Error)
		}
	}
	for _, cs := range p.CLIConnectors {
		if cs.Error != "" {
			return fmt.Errorf("generated CLI connector %q is invalid: %s", cs.Name, cs.Error)
		}
	}
	return nil
}

// buildCatalogPackFiles 生成一个包的全部文件（路径 → 内容）。
func buildCatalogPackFiles(kind, id string, argsOverride []string) (map[string][]byte, []string, error) {
	switch kind {
	case CatalogKindMCP:
		c, ok := connectors.Get(id)
		if !ok {
			return nil, nil, fmt.Errorf("unknown MCP catalog entry %q", id)
		}
		return buildMCPPackFiles(c, argsOverride)
	case CatalogKindCLI:
		e, ok := clicatalog.Get(id)
		if !ok {
			return nil, nil, fmt.Errorf("unknown CLI catalog entry %q", id)
		}
		return buildCLIPackFiles(e)
	default:
		return nil, nil, fmt.Errorf("unknown catalog kind %q (allowed: mcp, cli)", kind)
	}
}

// mcpServerOut 是生成的 mcp.json 里的条目结构（与 MCPServerSpec 同构，
// 单独定义是为了让字段顺序/省略规则稳定，输出更易读）。
type mcpServerOut struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	OAuth   *mcp.OAuthConfig  `json:"oauth,omitempty"`
}

// buildMCPSpec 把目录条目转成 mcp.json 的 server 条目。
//
// 凭据位置三态：env → 值为空；args → {{KEY}} 换成 ${CRED:KEY}；
// headers → 值写 ${CRED:KEY}（当前目录无此形态，保留能力以备扩条目）。
func buildMCPSpec(c connectors.Connector, argsOverride []string) mcpServerOut {
	spec := mcpServerOut{Type: mapCatalogTransport(c.Transport)}
	args := c.Args
	if len(argsOverride) > 0 {
		args = argsOverride
	}
	switch spec.Type {
	case TransportStdio:
		spec.Command = c.Command
		if len(args) > 0 {
			converted := make([]string, len(args))
			for i, a := range args {
				converted[i] = catalogParamToCredRef(a)
			}
			spec.Args = converted
		}
	default:
		spec.URL = c.URL
		// OAuth 只对远程条目有意义；写进 mcp.json 后由运行时在连接前取 token。
		if c.OAuth != nil {
			spec.OAuth = &mcp.OAuthConfig{
				ClientID:     c.OAuth.ClientID,
				ClientSecret: c.OAuth.ClientSecret,
				Scopes:       append([]string(nil), c.OAuth.Scopes...),
				RedirectPort: c.OAuth.RedirectPort,
				Resource:     c.OAuth.Resource,
			}
		}
	}

	if spec.OAuth != nil {
		// OAuth 条目不应再要求用户填 API key：两套凭据并存只会让人困惑，
		// 而且会把本来能"点一下登录"的流程退化成手抄 token。
		return spec
	}

	env := map[string]string{}
	headers := map[string]string{}
	for _, req := range c.Requires {
		switch req.Where {
		case connectors.WhereEnv:
			env[req.Key] = ""
		case connectors.WhereHeader:
			headers[req.Key] = "${CRED:" + req.Key + "}"
		}
	}
	if len(env) > 0 {
		spec.Env = env
	}
	if len(headers) > 0 {
		spec.Headers = headers
	}
	return spec
}

// catalogParamToCredRef 把目录模板里的 {{KEY}} 换成插件凭据引用 ${CRED:KEY}。
func catalogParamToCredRef(arg string) string {
	for {
		start := strings.Index(arg, "{{")
		if start < 0 {
			return arg
		}
		end := strings.Index(arg[start:], "}}")
		if end < 0 {
			return arg
		}
		end += start
		key := strings.TrimSpace(arg[start+2 : end])
		if !paramNameOK(key) {
			// 不是参数占位符：原样保留，避免把正常文本改坏。
			return arg
		}
		arg = arg[:start] + "${CRED:" + key + "}" + arg[end+2:]
	}
}

// paramNameOK 校验 {{KEY}} 里的名字（与插件凭据 key 同字符集）。
func paramNameOK(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9' && i > 0)
		if !ok {
			return false
		}
	}
	return true
}

// mapCatalogTransport 把目录的传输名映射成插件规范的传输名。
func mapCatalogTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "stdio":
		return TransportStdio
	case "http", "streamable-http", "streamablehttp":
		return TransportStreamableHTTP
	case "sse":
		return TransportSSE
	default:
		return TransportStdio
	}
}

// mcpServerName 决定 mcp.json 里的条目名：优先目录的 DefaultName，
// 但它要拼进工具名 ap_<plugin>_<server>_<tool>，必须符合条目名字符集。
func mcpServerName(c connectors.Connector) string {
	for _, cand := range []string{c.DefaultName, c.ID} {
		if cand != "" && validateEntryName(cand) == nil {
			return cand
		}
	}
	return "server"
}

// buildMCPPackFiles 生成 MCP 目录条目的插件文件。
func buildMCPPackFiles(c connectors.Connector, argsOverride []string) (map[string][]byte, []string, error) {
	spec := buildMCPSpec(c, argsOverride)
	if spec.Type != TransportStdio && spec.URL == "" {
		return nil, nil, fmt.Errorf("catalog entry %q has no url for transport %q", c.ID, spec.Type)
	}
	if spec.Type == TransportStdio && spec.Command == "" {
		return nil, nil, fmt.Errorf("catalog entry %q has no command", c.ID)
	}

	name := CatalogPackName(CatalogKindMCP, c.ID)
	manifest := pluginManifestOut{
		Schema:      PluginSchemaURL,
		Name:        name,
		Version:     "1.0.0",
		Description: c.Description,
		Keywords:    append([]string{"mcp"}, c.Tags...),
		Extensions: map[string]any{
			catalogPackExtKey: map[string]any{"kind": CatalogKindMCP, "id": c.ID},
		},
	}
	mcpDoc := map[string]any{
		"$schema": MCPSchemaURL,
		"mcpServers": map[string]mcpServerOut{
			mcpServerName(c): spec,
		},
	}

	files := map[string][]byte{}
	if err := putJSON(files, manifestName, manifest); err != nil {
		return nil, nil, err
	}
	if err := putJSON(files, mcpConfigName, mcpDoc); err != nil {
		return nil, nil, err
	}
	return files, SpecCredentialKeys(mcpOutToSpec(spec)), nil
}

// mcpOutToSpec 把输出结构转回加载器使用的主结构（计算凭据 key 用）。
func mcpOutToSpec(s mcpServerOut) MCPServerSpec {
	return MCPServerSpec{
		Type:    s.Type,
		Command: s.Command,
		Args:    s.Args,
		Env:     s.Env,
		URL:     s.URL,
		Headers: s.Headers,
		OAuth:   s.OAuth,
	}
}

// pluginManifestOut 是生成的 plugin.json。
type pluginManifestOut struct {
	Schema      string         `json:"$schema"`
	Name        string         `json:"name"`
	Version     string         `json:"version,omitempty"`
	Description string         `json:"description,omitempty"`
	Keywords    []string       `json:"keywords,omitempty"`
	Extensions  map[string]any `json:"extensions,omitempty"`
}

// buildCLIConnector 把 CLI 目录条目转成连接器声明：
// Requires 落成空 env 占位（与 cli.json 既有约定一致），其余原样。
func buildCLIConnector(e clicatalog.Entry) cliconn.Connector {
	conn := e.Connector
	env := make([]string, 0, len(e.Requires))
	for _, req := range e.Requires {
		env = append(env, req.Key+"=")
	}
	conn.Env = env
	conn.Enabled = nil // 默认启用：装完即出现在工具列表
	return conn
}

// buildCLIPackFiles 生成 CLI 目录条目的插件文件。
func buildCLIPackFiles(e clicatalog.Entry) (map[string][]byte, []string, error) {
	if strings.TrimSpace(e.Connector.Command) == "" {
		return nil, nil, fmt.Errorf("catalog entry %q has no command", e.ID)
	}
	name := CatalogPackName(CatalogKindCLI, e.ID)
	connName := e.DefaultName
	if connName == "" {
		connName = e.ID
	}
	if err := cliconn.ValidateConnectorName(connName); err != nil {
		return nil, nil, fmt.Errorf("catalog entry %q has invalid connector name %q: %w", e.ID, connName, err)
	}

	manifest := pluginManifestOut{
		Schema:      PluginSchemaURL,
		Name:        name,
		Version:     "1.0.0",
		Description: e.Description,
		Keywords:    append([]string{"cli"}, e.Tags...),
		Extensions: map[string]any{
			catalogPackExtKey: map[string]any{"kind": CatalogKindCLI, "id": e.ID},
		},
	}
	cliDoc := CLIConfig{Connectors: map[string]cliconn.Connector{connName: buildCLIConnector(e)}}

	files := map[string][]byte{}
	if err := putJSON(files, manifestName, manifest); err != nil {
		return nil, nil, err
	}
	if err := putJSON(files, cliConfigName, cliDoc); err != nil {
		return nil, nil, err
	}
	return files, envPlaceholderKeys(cliDoc.Connectors[connName].Env), nil
}

// putJSON 把 v 序列化成缩进 JSON 写入 files（落盘可读，便于用户手动微调）。
func putJSON(files map[string][]byte, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	files[name] = append(data, '\n')
	return nil
}

// writePackFiles 把文件写进插件目录（目录必须不存在或为空）。
func writePackFiles(dir string, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		target := filepath.Join(dir, n)
		if !within(target, dir) {
			return fmt.Errorf("plugin file %q escapes destination", n)
		}
		if err := os.WriteFile(target, files[n], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", n, err)
		}
	}
	return nil
}
