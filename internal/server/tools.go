package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	// Flatten every toolset into one list of tool names.
	//
	// buildToolsets() stores each toolset's "tools" value as []string (tool
	// names, for frontend compatibility). This used to assert
	// []map[string]interface{}, which can never succeed against []string — so
	// the endpoint silently answered `[]` no matter how many tools existed.
	names := make([]string, 0)
	for _, ts := range s.buildToolsets() {
		if tsTools, ok := ts["tools"].([]string); ok {
			names = append(names, tsTools...)
		}
	}
	jsonResponse(w, names)
}

func (s *Server) handleGetToolsets() []Toolset {
	// Keep backward compatibility: return Toolset structs from dynamic data
	dynamicToolsets := s.buildToolsets()
	result := make([]Toolset, 0, len(dynamicToolsets))
	for _, ts := range dynamicToolsets {
		name, _ := ts["name"].(string)
		// "tools" is already a []string of tool names; earlier code asserted
		// []map[string]interface{} and therefore always produced an empty list.
		toolNames, _ := ts["tools"].([]string)
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
		s.acquireServerMu("")
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
			_ = s.persistConfig(true)
		}
		s.releaseServerMu()
		jsonResponse(w, map[string]interface{}{"ok": true, "name": id, "enabled": true})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/disable") {
		id = strings.TrimSuffix(id, "/disable")
		s.acquireServerMu("")
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
			_ = s.persistConfig(true)
		}
		s.releaseServerMu()
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
