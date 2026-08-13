// Package safepath 提供路径安全工具，防止路径穿越攻击。
// 供 skills、server 等多个包共用，避免重复实现不一致的安全检查。
package safepath

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeJoin 在 root 目录内安全拼接 rel 路径，防止路径穿越。
// 返回绝对路径；若结果路径不在 root 内则返回错误。
// 拒绝绝对路径、Windows 盘符、含 ".." 的穿越尝试。
func SafeJoin(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("root is empty")
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("invalid root: %w", err)
	}
	cleanedRel := filepath.Clean(rel)
	if filepath.IsAbs(cleanedRel) {
		return "", fmt.Errorf("absolute path not allowed: %s", rel)
	}
	// 拒绝 Windows 盘符（如 C:foo）
	if len(cleanedRel) >= 2 && cleanedRel[1] == ':' {
		return "", fmt.Errorf("drive letter not allowed: %s", rel)
	}
	absPath, err := filepath.Abs(filepath.Join(absRoot, cleanedRel))
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !IsWithin(absPath, absRoot) {
		return "", fmt.Errorf("path traversal detected: %s", rel)
	}
	return absPath, nil
}

// IsWithin 校验 path 是否在 root 目录内（含 root 自身）。
func IsWithin(path, root string) bool {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	if absPath == absRoot {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(absPath, absRoot+sep)
}

// SanitizeName 校验名称只包含安全字符（字母、数字、下划线、连字符），
// 用于 skill/profile 等名称字段，防止注入路径分隔符或 ".."。
// 名称长度上限 64，不允许为空。
func SanitizeName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("name too long")
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return fmt.Errorf("invalid character in name: %q", r)
		}
	}
	// 额外拒绝以连字符开头（避免被某些 shell 解析为选项）
	if name[0] == '-' {
		return fmt.Errorf("name must not start with hyphen")
	}
	return nil
}

// SafeFileName 返回安全的文件/目录名：若 name 通过 SanitizeName 校验则原样返回，
// 否则把非法字符替换为 "_" 并去除前导连字符。
func SafeFileName(name string) string {
	if err := SanitizeName(name); err == nil {
		return name
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.TrimLeft(b.String(), "-")
	if s == "" {
		return "unnamed"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
