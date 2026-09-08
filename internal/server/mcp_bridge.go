package server

import (
	"github.com/magicwubiao/go-magic/internal/mcpbridge"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// 独立 MCP server → 工具注册表 的桥接。
//
// 背景:Agent Plugin 的 MCP 工具(ap_* 前缀)已经由 agentplugin 注入
// tool.Registry,但通过 config 的 "mcp.servers"、`magic mcp connect` 或
// Web UI /api/mcp 连接的独立 MCP server,工具此前只停留在 mcp.Manager,
// 从未进入 *tool.Registry —— 模型在 agent 循环中看不到、也调不到 mcp_* 工具。
//
// 这里把 s.mcpMgr 桥接到 s.toolReg,使独立 MCP server 的工具以
// mcp_<server>_<tool> 名称暴露给模型,与内置工具一样可被循环调用。

// initStandaloneMCP 在启动阶段连接 config 里配置的独立 MCP server,并把
// 发现到的工具注册进 Server 的工具注册表。单个 server 连接失败不会阻断
// 启动(与 Agent Plugin MCP 的隔离策略一致)。
func (s *Server) initStandaloneMCP() {
	if s == nil || s.mcpMgr == nil || s.toolReg == nil {
		return
	}
	if s.cfg == nil || s.cfg.MCP == nil || len(s.cfg.MCP.Servers) == 0 {
		return
	}

	if err := mcpbridge.ConnectAndSync(s.mcpMgr, s.toolReg, s.cfg.MCP.Servers); err != nil {
		log.Warnf("[MCP] standalone MCP servers partially failed: %v", err)
	}
	log.Infof("[MCP] standalone MCP servers connected: %d, total tools in registry: %d",
		len(s.mcpMgr.ListServers()), s.toolReg.Count())
}

// syncMCPServerToRegistry 把单个 MCP server 的工具刷新到注册表
// (connect / reconnect / tools refresh 后调用)。
func (s *Server) syncMCPServerToRegistry(serverName string) {
	if s == nil || s.mcpMgr == nil || s.toolReg == nil {
		return
	}
	mcpbridge.SyncServerToRegistry(s.mcpMgr, s.toolReg, serverName)
}

// removeMCPServerTools 从注册表移除某个 server 注册的全部 mcp_* 工具
// (disconnect / delete 后调用,避免模型继续调用已断开的工具)。
func (s *Server) removeMCPServerTools(serverName string) {
	if s == nil || s.toolReg == nil {
		return
	}
	mcpbridge.RemoveServerTools(s.toolReg, serverName)
}
