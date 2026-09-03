package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
)

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		servers := []map[string]interface{}{}
		if s.mcpMgr != nil {
			for _, name := range s.mcpMgr.ListServers() {
				info, _ := s.mcpMgr.GetServerInfo(name)
				connected := info != nil && info["connected"] == true
				toolCount := 0
				if info != nil {
					if tc, ok := info["tool_count"].(int); ok {
						toolCount = tc
					}
				}
				lastCheck := ""
				if info != nil {
					if lc, ok := info["last_health_check"].(time.Time); ok {
						lastCheck = lc.Format(time.RFC3339)
					}
				}
				servers = append(servers, map[string]interface{}{
					"name":              name,
					"connected":         connected,
					"tool_count":        toolCount,
					"last_health_check": lastCheck,
				})
			}
		}
		jsonResponse(w, servers)
	case http.MethodPost:
		var req struct {
			Name      string   `json:"name"`
			Transport string   `json:"transport"`
			Command   string   `json:"command"`
			Args      []string `json:"args"`
			Env       []string `json:"env"`
			URL       string   `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.mcpMgr == nil {
			http.Error(w, "MCP manager not initialized", http.StatusInternalServerError)
			return
		}
		cfg := mcp.ServerConfig{
			Command:   req.Command,
			Args:      req.Args,
			Env:       req.Env,
			Transport: req.Transport,
			URL:       req.URL,
		}
		var err error
		switch req.Transport {
		case "sse":
			err = s.mcpMgr.ConnectSSE(req.Name, cfg)
		default:
			err = s.mcpMgr.ConnectStdio(req.Name, cfg)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]interface{}{"name": req.Name, "success": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPServerByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp/servers/")
	parts := strings.Split(path, "/")
	name := parts[0]

	if s.mcpMgr == nil {
		http.Error(w, "MCP manager not initialized", http.StatusInternalServerError)
		return
	}

	if len(parts) >= 2 {
		switch parts[1] {
		case "connect":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := s.mcpMgr.Reconnect(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, map[string]bool{"success": true})
			return
		case "disconnect":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := s.mcpMgr.Disconnect(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, map[string]bool{"success": true})
			return
		case "reconnect":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := s.mcpMgr.Reconnect(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, map[string]bool{"success": true})
			return
		case "health":
			if r.Method == http.MethodPost {
				err := s.mcpMgr.HealthCheck(name)
				healthy := err == nil
				result := map[string]interface{}{
					"name":    name,
					"healthy": healthy,
				}
				if err != nil {
					result["error"] = err.Error()
				}
				jsonResponse(w, []map[string]interface{}{result})
				return
			}
			info, _ := s.mcpMgr.GetServerInfo(name)
			connected := info != nil && info["connected"] == true
			result := map[string]interface{}{
				"name":    name,
				"healthy": connected,
			}
			jsonResponse(w, []map[string]interface{}{result})
			return
		case "tools":
			if r.Method == http.MethodPost && len(parts) >= 3 && parts[2] == "refresh" {
				if err := s.mcpMgr.RefreshTools(name); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				tools, _ := s.mcpMgr.ListToolsByServer(name)
				jsonResponse(w, tools)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			tools, err := s.mcpMgr.ListToolsByServer(name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, tools)
			return
		}
	}

	if r.Method == http.MethodGet {
		info, err := s.mcpMgr.GetServerInfo(name)
		if err != nil || info == nil {
			http.Error(w, "Server not found", http.StatusNotFound)
			return
		}
		jsonResponse(w, info)
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.mcpMgr.Disconnect(name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]string{"status": "deleted"})
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	// Return all tools from all toolsets
	tools := make([]map[string]interface{}, 0)
	toolsets := s.buildToolsets()
	for _, ts := range toolsets {
		if tsTools, ok := ts["tools"].([]map[string]interface{}); ok {
			tools = append(tools, tsTools...)
		}
	}
	jsonResponse(w, tools)
}

func (s *Server) handleMCPHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	results := []map[string]interface{}{}
	if s.mcpMgr != nil {
		for _, name := range s.mcpMgr.ListServers() {
			err := s.mcpMgr.HealthCheck(name)
			healthy := err == nil
			result := map[string]interface{}{
				"name":    name,
				"healthy": healthy,
			}
			if err != nil {
				result["error"] = err.Error()
			}
			results = append(results, result)
		}
	}
	jsonResponse(w, results)
}

func (s *Server) handleGetToolsets() []Toolset {
	// Keep backward compatibility: return Toolset structs from dynamic data
	dynamicToolsets := s.buildToolsets()
	result := make([]Toolset, 0, len(dynamicToolsets))
	for _, ts := range dynamicToolsets {
		name, _ := ts["name"].(string)
		// Convert tool objects to tool names for backward compatibility
		var toolNames []string
		if tsTools, ok := ts["tools"].([]map[string]interface{}); ok {
			for _, t := range tsTools {
				if toolName, ok := t["name"].(string); ok {
					toolNames = append(toolNames, toolName)
				}
			}
		}
		result = append(result, Toolset{
			ID:      strings.ToLower(strings.ReplaceAll(name, " ", "_")),
			Name:    name,
			Tools:   toolNames,
			Enabled: true,
		})
	}
	return result
}

func (s *Server) handleToolsets(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, s.buildToolsets())
}

func (s *Server) handleToolsetsStatistics(w http.ResponseWriter, r *http.Request) {
	stats := []map[string]interface{}{}
	toolsets := s.buildToolsets()

	for _, ts := range toolsets {
		name, _ := ts["name"].(string)
		tools, _ := ts["tools"].([]string)

		// Calculate total calls from all tools in this toolset
		totalCalls := 0
		toolStats := make(map[string]int)
		if s.toolReg != nil {
			allStats := s.toolReg.GetStats()
			for _, toolName := range tools {
				if stat, ok := allStats[toolName]; ok {
					totalCalls += stat.TotalCalls
					toolStats[toolName] = stat.TotalCalls
				}
			}
		}

		// Find last used time
		lastUsed := time.Time{}
		if s.toolReg != nil {
			allStats := s.toolReg.GetStats()
			for _, toolName := range tools {
				if stat, ok := allStats[toolName]; ok && stat.LastUsed.After(lastUsed) {
					lastUsed = stat.LastUsed
				}
			}
		}

		stats = append(stats, map[string]interface{}{
			"toolset_name": name,
			"total_calls":  totalCalls,
			"tool_stats":   toolStats,
			"last_used":    lastUsed.Format(time.RFC3339),
		})
	}

	jsonResponse(w, stats)
}

func (s *Server) handleToolsStatistics(w http.ResponseWriter, r *http.Request) {
	stats := []map[string]interface{}{}
	if s.toolReg != nil {
		for name, stat := range s.toolReg.GetStats() {
			trend := "stable"
			if stat.SuccessRate >= 0.9 && stat.TotalCalls > 10 {
				trend = "improving"
			} else if stat.SuccessRate < 0.5 && stat.TotalCalls > 5 {
				trend = "declining"
			}
			stats = append(stats, map[string]interface{}{
				"tool_name":     name,
				"total_calls":   stat.TotalCalls,
				"success_calls": stat.SuccessCalls,
				"failed_calls":  stat.FailedCalls,
				"success_rate":  stat.SuccessRate,
				"avg_duration":  stat.AvgDuration.Milliseconds(),
				"last_used":     stat.LastUsed.Format(time.RFC3339),
				"trend":         trend,
			})
		}
	}
	jsonResponse(w, stats)
}

func (s *Server) handleToolCategories(w http.ResponseWriter, r *http.Request) {
	cats := map[string][]map[string]interface{}{}
	for _, ts := range s.buildToolsets() {
		name, _ := ts["name"].(string)
		cats[name] = []map[string]interface{}{ts}
	}
	jsonResponse(w, cats)
}

func (s *Server) handleToolByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tools/")
	tool := map[string]interface{}{
		"id":          id,
		"name":        id,
		"description": fmt.Sprintf("Tool: %s", id),
		"parameters":  map[string]interface{}{},
	}
	jsonResponse(w, tool)
}

func (s *Server) handleToolsetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tools/toolsets/")
	id = strings.TrimPrefix(id, "/api/toolsets/")

	// Handle enable/disable
	if strings.HasSuffix(r.URL.Path, "/enable") {
		id = strings.TrimSuffix(id, "/enable")
		s.mu.Lock()
		if s.cfg != nil {
			// Add to enabled list if not present
			found := false
			for _, e := range s.cfg.Tools.Enabled {
				if e == id || e == "all" {
					found = true
					break
				}
			}
			if !found {
				s.cfg.Tools.Enabled = append(s.cfg.Tools.Enabled, id)
			}
			// Remove from disabled list
			newDisabled := make([]string, 0)
			for _, d := range s.cfg.Tools.Disabled {
				if d != id {
					newDisabled = append(newDisabled, d)
				}
			}
			s.cfg.Tools.Disabled = newDisabled
			_ = s.cfg.Save()
		}
		s.mu.Unlock()
		jsonResponse(w, map[string]interface{}{"ok": true, "name": id, "enabled": true})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/disable") {
		id = strings.TrimSuffix(id, "/disable")
		s.mu.Lock()
		if s.cfg != nil {
			// Add to disabled list
			found := false
			for _, d := range s.cfg.Tools.Disabled {
				if d == id {
					found = true
					break
				}
			}
			if !found {
				s.cfg.Tools.Disabled = append(s.cfg.Tools.Disabled, id)
			}
			// Remove from enabled list (unless "all")
			newEnabled := make([]string, 0)
			for _, e := range s.cfg.Tools.Enabled {
				if e != id {
					newEnabled = append(newEnabled, e)
				}
			}
			s.cfg.Tools.Enabled = newEnabled
			_ = s.cfg.Save()
		}
		s.mu.Unlock()
		jsonResponse(w, map[string]interface{}{"ok": true, "name": id, "enabled": false})
		return
	}

	for _, ts := range s.buildToolsets() {
		if ts["name"] == id || ts["id"] == id {
			jsonResponse(w, ts)
			return
		}
	}
	http.Error(w, "not found", 404)
}
