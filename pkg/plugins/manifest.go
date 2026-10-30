// Package plugins 定义"插件清单"：一次安装把 skills、CLI 连接器、MCP server
// 一起写进配置。
//
// 为什么是清单而不是运行时：skills / MCP / CLI 三套能力都已经各有实现，
// 缺的从来不是新概念，而是"一个名字 + 一份声明 + 一次安装"。所以这里只做
// 三件事——解析声明、校验、展开成配置片段；真正落盘仍由调用方走各自的
// 持久化路径（server 的 persistMCPServers / persistCLIConnectors），
// 避免出现第二条写配置的通道。
//
// 明确不做的：远端 marketplace、版本解析、签名校验。没有分发基建时做这些
// 只会是半成品，当前的形态是"本地目录里的 plugin.json"。
package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
)

// ManifestFile 是插件清单的文件名（放在插件目录下）。
const ManifestFile = "plugin.json"

var (
	pluginNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	semverishRe  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-.+][0-9A-Za-z.-]+)?$`)
)

// Manifest 是一份插件声明。
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	// Requires 声明依赖的其他插件（安装时不递归，只检查是否已装）。
	Requires []string `json:"requires,omitempty"`
	// Skills 是技能名列表。技能文件本身不在插件里——插件只声明"需要哪些"，
	// 免得同一份 skill 被复制到每个插件目录里各存一份。
	Skills []string `json:"skills,omitempty"`
	// Connectors 是 CLI 连接器（写入 config.json 的 cli.connectors）。
	Connectors map[string]cliconn.Connector `json:"connectors,omitempty"`
	// MCPServers 是 MCP 连接器（写入 mcp.servers）。
	MCPServers map[string]mcp.ServerConfig `json:"mcp_servers,omitempty"`
	// RequiresEnv 声明安装后必须存在于环境里的变量（凭据用 ${ENV} 引用）。
	// 安装时只检查、不写入——值永远不落进配置。
	RequiresEnv []string `json:"requires_env,omitempty"`
}

// Parse 解析并校验一份插件声明。
func Parse(data []byte) (Manifest, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return Manifest{}, fmt.Errorf("plugin manifest is empty")
	}
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(trimmed))
	// 未知字段直接报错：插件作者写错键名时，静默忽略会变成"装上了但没生效"。
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("invalid plugin manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// LoadFile 读取并校验一个 plugin.json。
func LoadFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	m, err := Parse(data)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Discover 扫描 dir 下的子目录，找出所有 <dir>/<name>/plugin.json。
//
// 单层扫描而不是递归：插件目录是用户手工放的，递归会把 node_modules 之类
// 里的示例清单也扫进来，安装列表随即变成噪声。
func Discover(dir string) ([]Manifest, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}

	var out []Manifest
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), ManifestFile)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		m, err := LoadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errs
}

// Validate 检查声明是否自洽。
func (m Manifest) Validate() error {
	if err := ValidateName(m.Name); err != nil {
		return err
	}
	if m.Version != "" && !semverishRe.MatchString(m.Version) {
		return fmt.Errorf("plugin %q: version %q is not semver-ish (want x.y.z)", m.Name, m.Version)
	}
	if len(m.Connectors) == 0 && len(m.MCPServers) == 0 && len(m.Skills) == 0 {
		return fmt.Errorf("plugin %q declares nothing: add connectors, mcp_servers or skills", m.Name)
	}
	for _, dep := range m.Requires {
		if err := ValidateName(dep); err != nil {
			return fmt.Errorf("plugin %q: requires: %w", m.Name, err)
		}
	}
	for name, c := range m.Connectors {
		if err := cliconn.ValidateConnectorName(name); err != nil {
			return fmt.Errorf("plugin %q: connector %q: %w", m.Name, name, err)
		}
		if err := c.Validate(); err != nil {
			return fmt.Errorf("plugin %q: connector %q: %w", m.Name, name, err)
		}
	}
	for name, s := range m.MCPServers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("plugin %q: mcp server has an empty name", m.Name)
		}
		// 与 mcp 连接器的既有约定一致：stdio 要 command，远程要 url。
		if s.Transport == mcp.TransportSSE || s.Transport == mcp.TransportHTTP {
			if strings.TrimSpace(s.URL) == "" {
				return fmt.Errorf("plugin %q: mcp server %q uses %s transport but has no url", m.Name, name, s.Transport)
			}
		} else if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("plugin %q: mcp server %q has no command", m.Name, name)
		}
	}
	for _, skill := range m.Skills {
		if strings.TrimSpace(skill) == "" {
			return fmt.Errorf("plugin %q: skill name is empty", m.Name)
		}
	}
	for _, env := range m.RequiresEnv {
		if strings.TrimSpace(env) == "" {
			return fmt.Errorf("plugin %q: requires_env has an empty entry", m.Name)
		}
	}
	return nil
}

// ValidateName 校验插件名（同时用作目录名与安装记录键）。
func ValidateName(name string) error {
	if !pluginNameRe.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("invalid plugin name %q: use letters, digits, '-', '_' or '.' (max 64 chars)", name)
	}
	return nil
}

// Conflicts 报告与现有配置重名的部分：默认不覆盖用户已有的连接器。
func (m Manifest) Conflicts(connectors map[string]cliconn.Connector, servers map[string]mcp.ServerConfig) []string {
	var out []string
	for name := range m.Connectors {
		if _, ok := connectors[name]; ok {
			out = append(out, "cli:"+name)
		}
	}
	for name := range m.MCPServers {
		if _, ok := servers[name]; ok {
			out = append(out, "mcp:"+name)
		}
	}
	sort.Strings(out)
	return out
}

// MissingEnv 返回声明了但环境里没有的变量。
func (m Manifest) MissingEnv() []string {
	var out []string
	for _, name := range m.RequiresEnv {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
