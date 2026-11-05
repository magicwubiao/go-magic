package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
)

// 目录插件包接口：把内置的 MCP 连接器目录 / CLI 应用目录里的条目，
// 物化成真正的 Agent Plugin（plugin.json + mcp.json / cli.json）。
//
//	GET  /api/plugins/catalog?kind=mcp|cli  目录列表（含 installed 状态）
//	POST /api/plugins/catalog/install       body {"kind":"mcp","id":"lark","args":[...]}
//
// 与"官方技能包"的区别只是内容来源：官方包内嵌在二进制里，目录包由
// pkg/connectors / pkg/clicatalog 的模板现场生成。装上之后两者完全同构——
// 同一个扫描目录、同一套启用/禁用/卸载/授权。
func (s *Server) listAgentPluginCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && kind != agentplugin.CatalogKindMCP && kind != agentplugin.CatalogKindCLI {
		http.Error(w, "unknown kind (allowed: mcp, cli)", http.StatusBadRequest)
		return
	}
	jsonResponse(w, agentplugin.CatalogPacks(kind))
}

// installAgentPluginFromCatalog 一键把目录条目装成插件。
//
// args 只对 stdio 类 MCP 条目生效：postgres / sqlite / filesystem 这类条目的
// 参数里有必须由用户填的路径或连接串，目录只能给示例值。
func (s *Server) installAgentPluginFromCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Kind string   `json:"kind"`
		ID   string   `json:"id"`
		Args []string `json:"args"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid json body: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.Kind = strings.TrimSpace(body.Kind)
	body.ID = strings.TrimSpace(body.ID)
	if body.Kind == "" || body.ID == "" {
		http.Error(w, "missing 'kind' or 'id'", http.StatusBadRequest)
		return
	}

	// 生命周期串行：两条并发安装（或安装与卸载交错）会在同一目录上打架。
	// 与 zip 安装同一条锁，但**不做全量 swapAgentPlugins**——那会把其它插件
	// 的 MCP 子进程全部杀掉（装一个插件不该打断别人的会话）。
	s.agentPluginsMu.Lock()
	defer s.agentPluginsMu.Unlock()

	dir, name, required, err := agentplugin.InstallCatalogPack(body.Kind, body.ID, body.Args)
	if err != nil {
		if errors.Is(err, agentplugin.ErrPluginExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "install failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 只激活新插件：解析 → 换入条目 → 注入 skills/CLI/MCP 工具。
	reloaded := agentplugin.LoadDir(dir)
	if s.isPluginDisabled(reloaded.Name()) {
		reloaded.Disabled = true
		reloaded.Runtime = nil
	}
	s.putPluginEntry(reloaded.Name(), reloaded)
	s.activatePlugin(reloaded)

	jsonResponse(w, map[string]any{
		"ok":           true,
		"name":         name,
		"dir":          dir,
		"required_env": required,
	})
}
