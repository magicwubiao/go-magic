// Package cliconnbridge 把 CLI 连接器的工具注册进 agent 工具注册表。
//
// 单独成包的理由与 mcpbridge 相同：internal/cliconn 只描述"怎么跑一个命令"，
// 不依赖 tool 包（pkg/config 要 import cliconn，而 internal/tool 又 import
// pkg/config，直接在 cliconn 里 import tool 会成环）。注册表这层胶水放在这里，
// 每个入口（web server / TUI / gateway）都能用同一份逻辑。
package cliconnbridge

import (
	"strings"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// BuildAndSync 用一份定义重建 Manager 并把工具同步进注册表。
//
// 这是各入口（web server / chat / TUI）共用的唯一入口：调用方不需要知道
// "先 New 再 Sync"的顺序，也不会出现某个入口忘了同步的情况——CLI 连接器
// 曾经只接在 web server 里，`magic` 启动的会话看不到任何 cli_* 工具。
//
// profile 用于过滤 profiles 限定的连接器（空串 = 不过滤）。
// 返回新 Manager（没有子进程需要清理，仅用于查询与试跑）与注册的工具数。
func BuildAndSync(defs map[string]cliconn.Connector, reg *tool.Registry, profile string) (*cliconn.Manager, int) {
	mgr := cliconn.New(defs)
	return mgr, SyncToRegistry(mgr, reg, profile)
}

// SyncToRegistry 让注册表与 CLI 连接器定义保持一致：先清掉全部 cli_* 工具，
// 再按当前定义重新注册。幂等，可反复调用。
//
// profile 用于过滤 profiles 限定的连接器（空串 = 不过滤）。
// 返回注册的工具数。
func SyncToRegistry(mgr *cliconn.Manager, reg *tool.Registry, profile string) int {
	if mgr == nil || reg == nil {
		return 0
	}

	// cli_ 前缀是 CLI 连接器独占的（内置工具没有以此为名的），
	// 因此整体清空是安全的，也能把"被删除的连接器"留下的僵尸工具一起收走。
	for _, name := range reg.List() {
		if strings.HasPrefix(name, cliconn.NamespacePrefix) {
			reg.Unregister(name)
		}
	}

	count := 0
	for _, t := range mgr.ToolsForProfile(profile) {
		reg.Register(t)
		// 按工具设置超时：注册表没有条目时执行器会套默认的 60s，
		// 连接器里配的 timeout_seconds 就白写了（构建/同步类命令常超过 60s）。
		reg.SetTimeout(t.Name(), t.Timeout())
		count++
	}
	return count
}

// RemoveConnectorTools 移除某个连接器注册的全部工具（改配置/删除后调用）。
func RemoveConnectorTools(reg *tool.Registry, connector string) {
	if reg == nil {
		return
	}
	prefix := cliconn.ToolPrefix(connector)
	for _, name := range reg.List() {
		if strings.HasPrefix(name, prefix) {
			reg.Unregister(name)
		}
	}
}
