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
	"os"
	"path/filepath"
	"strings"

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
