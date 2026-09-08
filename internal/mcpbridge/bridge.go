// Package mcpbridge bridges tools discovered on standalone MCP servers into
// the agent tool registry.
//
// Background: Agent Plugin MCP tools (ap_* prefix) are injected by
// agentplugin, but tools served by standalone MCP servers configured via
// config.json ("mcp.servers"), `magic mcp connect` or the Web UI used to live
// only inside mcp.Manager — never in *tool.Registry — so the model could not
// see or call them from the agent loop.
//
// This package owns the glue between *mcp.Manager and *tool.Registry so every
// entrypoint (web server, chat/TUI, gateway) can wire MCP servers up the same
// way. Registered tools are named mcp_<server>_<tool>.
package mcpbridge

import (
	"errors"
	"strings"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// SyncToRegistry reconciles the registry with the current MCP topology: every
// stale mcp_* entry (from servers that disconnected or dropped tools) is
// removed, then the tools currently offered by all connected servers are
// (re-)registered. It is idempotent and safe to call repeatedly.
// Returns the number of tools registered.
func SyncToRegistry(mgr *mcp.Manager, reg *tool.Registry) int {
	if mgr == nil || reg == nil {
		return 0
	}

	for _, name := range reg.List() {
		if strings.HasPrefix(name, mcp.NamespacePrefix) {
			reg.Unregister(name)
		}
	}

	count := 0
	for _, serverName := range mgr.ListServers() {
		tools, err := mgr.ListToolsByServer(serverName)
		if err != nil {
			continue
		}
		for _, t := range tools {
			reg.Register(mcp.NewMCPTool(serverName, t, mgr))
			count++
		}
	}
	return count
}

// SyncServerToRegistry refreshes the registry entries of a single MCP server:
// stale mcp_<server>_* tools are removed and the server's current tools are
// registered again. Used after reconnect / tools/refresh.
// Returns the number of tools registered for the server.
func SyncServerToRegistry(mgr *mcp.Manager, reg *tool.Registry, serverName string) int {
	RemoveServerTools(reg, serverName)
	if mgr == nil || reg == nil {
		return 0
	}

	tools, err := mgr.ListToolsByServer(serverName)
	if err != nil {
		return 0
	}
	for _, t := range tools {
		reg.Register(mcp.NewMCPTool(serverName, t, mgr))
	}
	return len(tools)
}

// RemoveServerTools removes every registered mcp_<server>_* tool from the
// registry after that MCP server disconnected.
func RemoveServerTools(reg *tool.Registry, serverName string) {
	if reg == nil {
		return
	}
	prefix := mcp.ToolPrefix(serverName)
	for _, name := range reg.List() {
		if strings.HasPrefix(name, prefix) {
			reg.Unregister(name)
		}
	}
}

// ConnectAndSync connects the configured standalone MCP servers and syncs
// their discovered tools into the registry. Per-server failures never block
// the other servers; any failure is reported in the returned error while the
// healthy servers' tools are still registered.
func ConnectAndSync(mgr *mcp.Manager, reg *tool.Registry, servers map[string]mcp.ServerConfig) error {
	if mgr == nil {
		return errors.New("mcp manager is nil")
	}

	err := mgr.ConnectConfigured(servers)
	if reg != nil {
		SyncToRegistry(mgr, reg)
	}
	return err
}
