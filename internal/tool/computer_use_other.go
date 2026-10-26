//go:build !windows && !darwin && !linux

package tool

// computer_use_other.go — 其它平台（*BSD、plan9 等）没有桌面驱动。
// 返回 ErrDesktopUnavailable，配合 ComputerUseTool.Available() 让该工具
// 不出现在模型可见的工具列表里。

import "fmt"

// platformDriver 返回一个始终不可用的后端。
func platformDriver() desktopDriver {
	return &unsupportedDriver{
		reason: fmt.Errorf("%w: computer_use is only implemented for Windows, macOS and Linux/X11",
			ErrDesktopUnavailable),
	}
}
