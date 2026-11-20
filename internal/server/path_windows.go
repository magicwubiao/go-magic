//go:build windows

package server

// Windows 上 PATH 的进程内刷新。
//
// 问题：winget / 各类安装器改的是注册表里的 PATH（HKLM 的系统 PATH +
// HKCU 的用户 PATH），而正在运行的进程拿到的是启动时的快照。装完
// Node/Python/Git 后 exec.LookPath 依然找不到，运行时页面一直显示
// "未安装"，重启应用才恢复。
//
// 解决：安装完成后从注册表重读两份 PATH，与进程现有 PATH 做并集合并，
// 再 os.Setenv 写回。之后所有 LookPath（运行时探测、CLI 目录探测、
// job 的 binary 确认）无需重启即可看到新装的命令。
//
// 合并顺序刻意安排为：注册表系统 PATH → 注册表用户 PATH → 进程独有条目。
// 这与"新开一个终端"的解析顺序一致，还能顺带压过 Windows 商店的 python
// stub（stub 在用户 PATH 的 WindowsApps 里，真 Python 由 winget 装进系统
// PATH，排在前面即胜出）。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// refreshProcessPath 从注册表重读系统+用户 PATH 并合并进当前进程环境。
// 注册表读取失败时静默保持现状：刷新失败不该比"重启应用"更糟。
func refreshProcessPath() {
	machine := readRegistryPath(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	user := readRegistryPath(registry.CURRENT_USER, `Environment`)
	current := filepath.SplitList(os.Getenv("PATH"))
	os.Setenv("PATH", mergePathList(current, machine, user))
}

// readRegistryPath 读一个环境变量键下的 Path 值；键不存在或读失败返回 nil。
// GetStringValue 会自动展开 REG_EXPAND_SZ（%SystemRoot% 之类）。
func readRegistryPath(root registry.Key, key string) []string {
	k, err := registry.OpenKey(root, key, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}
	return filepath.SplitList(v)
}

// addUserPathEntry 把 dir 插到 HKCU\Environment 的 Path **最前面**（已存在
// 则不动，返回 nil）。
//
// 为什么插前面而不是追加：用户 PATH 里的 WindowsApps 有商店 python stub，
// 追加会让 stub 抢先（LookPath 顺序在 PATH 前面）。我们管理的 runtimes 目录
// 排前面，探测才能稳定命中真 Python。
//
// 用 registry API 而不是 reg.exe：读改写一气呵成，还躲开了 reg add 对
// REG_EXPAND_SZ 的引号转义问题。回写统一用 REG_EXPAND_SZ，保住 Path 里
// 已有的 %VAR% 条目。
func addUserPathEntry(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("empty path entry")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`,
		registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open HKCU Environment: %w", err)
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("read user Path: %w", err)
	}
	entries := filepath.SplitList(v)
	for _, e := range entries {
		if strings.EqualFold(strings.TrimSpace(e), dir) {
			return nil // 已经在了
		}
	}
	out := append([]string{dir}, entries...)
	return k.SetExpandStringValue("Path", strings.Join(out, ";"))
}

// notifyEnvChanged 广播 WM_SETTINGCHANGE，让资源管理器刷新环境变量，
// 之后新开的终端才能看到我们写入的 PATH。失败静默：广播只是锦上添花，
// 应用自身与子进程的 PATH 我们已经通过 refreshProcessPath 解决了。
func notifyEnvChanged() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	name, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var res uintptr
	// HWND_BROADCAST(0xFFFF) + WM_SETTINGCHANGE(0x001A) + SMTO_ABORTIFHUNG(0x0002)
	proc.Call(0xFFFF, 0x001A, 0, uintptr(unsafe.Pointer(name)), 0x0002, 2000,
		uintptr(unsafe.Pointer(&res)))
}

// mergePathList 把三段 PATH 条目按顺序做并集（大小写不敏感去重，Windows
// 路径不区分大小写）：进程现有条目里注册表没有的，垫在合并结果最后——
// 它们是启动时父进程额外注入的，排在注册表内容之后语义最稳。
func mergePathList(current, machine, user []string) string {
	seen := make(map[string]bool)
	var out []string
	add := func(entries []string) {
		for _, p := range entries {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			key := strings.ToLower(p)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	add(machine)
	add(user)
	add(current)
	return strings.Join(out, string(os.PathListSeparator))
}
