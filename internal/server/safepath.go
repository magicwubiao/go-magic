package server

import (
	"github.com/magicwubiao/go-magic/pkg/safepath"
)

// SafeJoin 在 root 目录内安全拼接 rel 路径，防止路径穿越。
// 返回绝对路径；若结果路径不在 root 内则返回错误。
//
// Deprecated: 使用 pkg/safepath.SafeJoin。本函数保留用于包内向后兼容。
func SafeJoin(root, rel string) (string, error) {
	return safepath.SafeJoin(root, rel)
}

// IsPathWithin 校验 path 是否在 root 目录内（公开方法）。
//
// Deprecated: 使用 pkg/safepath.IsWithin。
func IsPathWithin(path, root string) bool {
	return safepath.IsWithin(path, root)
}

// SanitizeName 校验名称只包含安全字符（字母、数字、下划线、连字符）。
//
// Deprecated: 使用 pkg/safepath.SanitizeName。
func SanitizeName(name string) error {
	return safepath.SanitizeName(name)
}
