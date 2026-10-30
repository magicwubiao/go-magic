package agentplugin

// 官方技能包(Official Packs):编译期内嵌进二进制的"技能 + CLI 连接器"插件。
//
// 设计动机:CLI 连接器的手写配置(参数模板、参数声明 JSON)对普通用户门槛太高。
// 把"怎么用某个 CLI"的知识固化成随版本分发的技能包——用户在插件页看到的是
// "我能干什么"(示例问法 + 技能说明),需要凭据的包只有一个"授权"按钮,
// 全程没有"添加/编辑连接器"这一步。与外部产品的"技能 + 连接器"形态对齐。
//
// 约束:
//   - 包内容只随 go-magic 版本更新,不做热更新;用户装到扫描目录后就是
//     一个普通插件,可以禁用/卸载/填凭据,与手工安装的插件无任何区别。
//   - 物化(materialize)只写常规文件,不做任何模板渲染;安装后立即由
//     Manager.LoadAll 走完整校验,坏包在这里就现形。
//   - 目录名即包名,复用 validatePluginName 校验,防止路径逃逸。

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed all:official
var officialFS embed.FS

// officialRootDir 是 embed FS 里技能包的根目录。
const officialRootDir = "official"

// OfficialPack 是一个官方技能包的展示元信息。
type OfficialPack struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	// Examples 是 manifest 声明的示例问法,前端直接展示。
	Examples []string `json:"examples,omitempty"`
	// Skills 是包内 skill 目录名列表。
	Skills []string `json:"skills,omitempty"`
	// RequiredEnv 是 cli.json 里值为空的 env key,即安装后需要用户填写的凭据。
	// 非空 = 前端把按钮渲染成"授权"而不是直接可用。
	RequiredEnv []string `json:"required_env,omitempty"`
	// Installed 报告该包是否已在扫描目录中。
	Installed bool `json:"installed"`
}

// officialManifest 只取 plugin.json 里展示需要的字段。
type officialManifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// OfficialPackNames 返回内嵌技能包名列表(升序)。
func OfficialPackNames() []string {
	entries, err := officialFS.ReadDir(officialRootDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && hasManifestFS(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// hasManifestFS 判断内嵌包目录是否含 plugin.json。
// 注意:embed FS 的路径分隔符永远是 '/',必须用 path.Join 而不是 filepath.Join。
func hasManifestFS(name string) bool {
	_, err := officialFS.ReadFile(path.Join(officialRootDir, name, manifestName))
	return err == nil
}

// OfficialPacks 返回全部官方技能包元信息(按名字升序)。
//
// 元信息直接从内嵌的 plugin.json / cli.json 解析;解析失败的包直接跳过——
// 内嵌包随二进制发布,坏包说明构建流程有问题,不应在运行时半展示。
func OfficialPacks() []OfficialPack {
	var out []OfficialPack
	for _, name := range OfficialPackNames() {
		pack, err := loadOfficialPack(name)
		if err != nil {
			continue
		}
		out = append(out, pack)
	}
	return out
}

// loadOfficialPack 解析单个内嵌包的元信息。
func loadOfficialPack(name string) (OfficialPack, error) {
	dir := path.Join(officialRootDir, name)

	var m officialManifest
	if data, err := officialFS.ReadFile(path.Join(dir, manifestName)); err != nil {
		return OfficialPack{}, fmt.Errorf("read %s: %w", manifestName, err)
	} else if err := json.Unmarshal(data, &m); err != nil {
		return OfficialPack{}, fmt.Errorf("parse %s: %w", manifestName, err)
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = name
	}

	_, installed := InstalledDir(name)
	pack := OfficialPack{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Examples:    m.Examples,
		Installed:   installed,
	}

	// skills/ 直接子目录名。
	if sks, err := officialFS.ReadDir(path.Join(dir, skillsDirName)); err == nil {
		for _, e := range sks {
			if e.IsDir() {
				pack.Skills = append(pack.Skills, e.Name())
			}
		}
		sort.Strings(pack.Skills)
	}

	// cli.json 里值为空的 env key = 待填凭据。
	if data, err := officialFS.ReadFile(path.Join(dir, cliConfigName)); err == nil {
		var cfg CLIConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			for _, cn := range cfg.Connectors {
				pack.RequiredEnv = append(pack.RequiredEnv, envPlaceholderKeys(cn.Env)...)
			}
			sort.Strings(pack.RequiredEnv)
			pack.RequiredEnv = dedupStrings(pack.RequiredEnv)
		}
	}
	return pack, nil
}

// InstallOfficial 把一个官方技能包物化到插件扫描目录,返回插件目录路径。
//
// 已存在同名插件目录时返回错误(调用方可提示先卸载);目录名经 validatePluginName
// 校验,且必须真实存在于内嵌包列表——两道检查合起来杜绝把任意路径拼进目标位置。
func InstallOfficial(name string) (string, error) {
	if err := validatePluginName(name); err != nil {
		return "", fmt.Errorf("invalid pack name: %w", err)
	}
	found := false
	for _, n := range OfficialPackNames() {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("official pack %q not found", name)
	}

	scanDir := DefaultScanDir()
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure scan dir: %w", err)
	}
	dest := filepath.Join(scanDir, name)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("plugin %q already exists, uninstall first", name)
	}

	if err := materializeOfficial(name, dest); err != nil {
		_ = os.RemoveAll(dest) // 半成品目录必须清掉,否则重装会撞 "already exists"
		return "", err
	}
	return dest, nil
}

// materializeOfficial 把内嵌包目录逐文件写出(dest 必须不存在)。
// 文件名全部来自 embed FS(编译期固定),不存在运行时注入的路径成分。
func materializeOfficial(name, dest string) error {
	// root 用 path.Join:fs.WalkDir 的参数是 embed FS 路径('/'分隔符),
	// rel 之后拼 target 才换回 filepath.Join(磁盘路径)。
	root := path.Join(officialRootDir, name)
	return fs.WalkDir(officialFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if !within(target, dest) {
			return fmt.Errorf("embedded entry %q escapes destination", rel)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := officialFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", rel, err)
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// dedupStrings 去重并保持原有(已排序)顺序。
func dedupStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
