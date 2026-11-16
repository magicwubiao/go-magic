package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// DefaultProfileName 是内置的人设名。
//
// 它**没有**单独的目录：default 就是正在运行的这套数据（<magicHome>/cortex）。
// 面板编辑 default 时改的就是当前生效的 SOUL.md / USER.md，所见即所得。
const DefaultProfileName = "default"

// ProfileEnvVar 是运行期覆盖"当前人设"的环境变量。
//
// 语义已随降级调整：人设是**内容模板**，`magic --profile work chat` 表示
// "启动时把 work 预设应用一次"，而不是"换一套目录身份"。之所以仍用环境变量
// 而不是直接改 cfg.Profile：一次性选择不该被后续任何一次 persistConfig
// 固化下来。持久化的选择走面板 / `magic config set profile`。
const ProfileEnvVar = "GO_MAGIC_PROFILE"

// ActiveProfile 返回当前记录的人设名（不代表磁盘目录，见 ProfileDir）。
// 优先级：GO_MAGIC_PROFILE（--profile 翻译而来）> config.json 的 profile > default。
//
// 名字非法（手改 config.json 写进 "../.." 之类）时回退 default：这个值仍会
// 拼进模板文件路径，绝不能让它带着分隔符到处跑。

func (c *Config) ActiveProfile() string {
	if p := strings.TrimSpace(os.Getenv(ProfileEnvVar)); p != "" {
		if ValidProfileName(p) {
			return p
		}
		return DefaultProfileName
	}
	if c != nil {
		if p := strings.TrimSpace(c.Profile); p != "" {
			if ValidProfileName(p) {
				return p
			}
			return DefaultProfileName
		}
	}
	return DefaultProfileName
}

// windowsReservedNames 是 Windows 上不能作为目录名的设备名。
// 分身名会直接变成目录名，命中它就等于把文件写进黑洞。
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidProfileName 判断 name 能否作为分身名。
//
// 分身名会被拼进文件路径（<magicHome>/profiles/<name>），所以必须保证它是
// **单个、安全的路径段**：拒绝空串、"." / ".."、路径分隔符（含 Windows
// 的反斜杠）、盘符冒号、NUL、首尾空白，以及 Windows 设备名。
// 允许中文等 Unicode 字母（面板是中文的，用户会直接打"工作"）。
func ValidProfileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if len([]rune(name)) > 64 {
		return false
	}
	if strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == 0:
			return false
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return !windowsReservedNames[strings.ToLower(name)]
}

// ProfileDir 返回人设目录（模板文件 SOUL.md / USER.md / USER.json 所在地）。
//
//   - "" 与 "default" → <magicHome>/cortex，即当前正在使用的那份人格本身；
//   - 其它合法名字   → <magicHome>/profiles/<name>。
//
// 注意：运行期**不再**按这个目录读取人设。人设只是模板，应用预设时由
// server 把内容写进活动 cortex 目录；这个函数只用于面板的读写与克隆。
//
// 名字非法时返回 ""：调用方必须把它当成参数错误（400）处理，绝不能拿去
// 拼路径 —— 这是防目录穿越的最后一道闸，filepath.Join 自己会老老实实
// 按 ".." 往上跳。
func ProfileDir(magicHome, profile string) string {
	if profile == "" || profile == DefaultProfileName {
		return filepath.Join(magicHome, "cortex")
	}
	if !ValidProfileName(profile) {
		return ""
	}
	return filepath.Join(magicHome, "profiles", profile)
}

// SoulPath 返回分身 SOUL.md 的路径；名字非法时返回 ""。
func SoulPath(magicHome, profile string) string {
	dir := ProfileDir(magicHome, profile)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "SOUL.md")
}

// UserMDPath 返回分身 USER.md（人类可读视图）的路径；名字非法时返回 ""。
func UserMDPath(magicHome, profile string) string {
	dir := ProfileDir(magicHome, profile)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "USER.md")
}

// UserJSONPath 返回分身 USER.json（权威存储）的路径；名字非法时返回 ""。
func UserJSONPath(magicHome, profile string) string {
	dir := ProfileDir(magicHome, profile)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "USER.json")
}

// ApplyPersonaToActive 把预设 src 的人设与画像复制到活动目录（覆盖）。
//
// 只在显式 `--profile <name>` 启动时调用：预设是模板，点名即"用这套人格启动"。
// 面板上的应用走 server 的 live 写入路径（带 SoulManager 的内存状态与大小闸），
// 这里是进程刚起来、还没有任何运行时状态时的文件级等价实现。
//
// 不复制 .env / skills：一套部署一份凭据，技能是全局的。
// 返回实际被覆盖的文件名，便于调用方打印日志。
func ApplyPersonaToActive(magicHome, name string) ([]string, error) {
	if name == "" || name == DefaultProfileName {
		return nil, nil
	}
	srcDir := ProfileDir(magicHome, name)
	dstDir := ProfileDir(magicHome, DefaultProfileName)
	if srcDir == "" || dstDir == "" {
		return nil, fmt.Errorf("invalid profile name: %q", name)
	}
	if _, err := os.Stat(srcDir); err != nil {
		return nil, fmt.Errorf("profile %q not found: %w", name, err)
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return nil, err
	}

	pairs := [][2]string{
		{readableSoulPath(filepath.Join(srcDir, "SOUL.md")), filepath.Join(dstDir, "SOUL.md")},
		{filepath.Join(srcDir, "USER.md"), filepath.Join(dstDir, "USER.md")},
		{filepath.Join(srcDir, "USER.json"), filepath.Join(dstDir, "USER.json")},
	}

	var copied []string
	for _, p := range pairs {
		data, err := os.ReadFile(p[0])
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return copied, err
		}
		if err := writeFileAtomic0644(p[1], data); err != nil {
			return copied, err
		}
		copied = append(copied, filepath.Base(p[1]))
	}
	return copied, nil
}

// readableSoulPath 返回实际存在的 SOUL.md 路径（优先大写，其次旧的小写）。
func readableSoulPath(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	lower := filepath.Join(filepath.Dir(path), strings.ToLower(filepath.Base(path)))
	if _, err := os.Stat(lower); err == nil {
		return lower
	}
	return path
}

// writeFileAtomic0644 先写同目录临时文件再 rename：启动期应用预设时若进程
// 被杀，不会留下半截 SOUL.md（那会让 AI 人格变成一段残句）。
func writeFileAtomic0644(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".persona.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
