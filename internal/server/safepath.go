package server

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeJoin 在 root 目录内安全拼接 rel 路径，防止路径穿越。
// 返回绝对路径；若结果路径不在 root 内则返回错误。
func SafeJoin(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("root is empty")
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("invalid root: %w", err)
	}
	// 拒绝 rel 中的绝对路径和盘符
	cleanedRel := filepath.Clean(rel)
	if filepath.IsAbs(cleanedRel) {
		return "", fmt.Errorf("absolute path not allowed: %s", rel)
	}
	// 拒绝 Windows 盘符
	if len(cleanedRel) >= 2 && cleanedRel[1] == ':' {
		return "", fmt.Errorf("drive letter not allowed: %s", rel)
	}
	absPath, err := filepath.Abs(filepath.Join(absRoot, cleanedRel))
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	// 严格前缀校验，加 separator 防止 /tmp/foo 匹配 /tmp/foobar
	if !isWithinPath(absPath, absRoot) {
		return "", fmt.Errorf("path traversal detected: %s", rel)
	}
	return absPath, nil
}

// isWithinPath 校验 path 是否在 root 目录内（含 root 自身）。
func isWithinPath(path, root string) bool {
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(path, root+sep)
}

// IsPathWithin 校验 path 是否在 root 目录内（公开方法）。
func IsPathWithin(path, root string) bool {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	return isWithinPath(absPath, absRoot)
}

// SanitizeName 校验名称只包含安全字符（字母、数字、下划线、连字符），用于 profile/kanban 等名称字段。
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
	return nil
}
