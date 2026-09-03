package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleDashboardPluginsSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/dashboard/plugins/")

	// Handle enable
	if strings.HasSuffix(path, "/enable") {
		name := strings.TrimSuffix(path, "/enable")
		if r.Method == http.MethodPost {
			// Add to enabled list
			if s.cfg != nil {
				found := false
				for _, e := range s.cfg.Plugins.Enabled {
					if e == name {
						found = true
						break
					}
				}
				if !found {
					s.cfg.Plugins.Enabled = append(s.cfg.Plugins.Enabled, name)
					// Remove from disabled list if present
					newDisabled := []string{}
					for _, d := range s.cfg.Plugins.Disabled {
						if d != name {
							newDisabled = append(newDisabled, d)
						}
					}
					s.cfg.Plugins.Disabled = newDisabled
					s.cfg.Save()
				}
			}
			jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": true})
			return
		}
	}

	// Handle disable
	if strings.HasSuffix(path, "/disable") {
		name := strings.TrimSuffix(path, "/disable")
		if r.Method == http.MethodPost {
			// Add to disabled list
			if s.cfg != nil {
				found := false
				for _, d := range s.cfg.Plugins.Disabled {
					if d == name {
						found = true
						break
					}
				}
				if !found {
					s.cfg.Plugins.Disabled = append(s.cfg.Plugins.Disabled, name)
					// Remove from enabled list if present
					newEnabled := []string{}
					for _, e := range s.cfg.Plugins.Enabled {
						if e != name && e != "all" {
							newEnabled = append(newEnabled, e)
						}
					}
					s.cfg.Plugins.Enabled = newEnabled
					s.cfg.Save()
				}
			}
			jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": false})
			return
		}
	}

	// Handle delete (DELETE /api/dashboard/plugins/{name})
	if r.Method == http.MethodDelete {
		name := path
		pluginsDir := filepath.Join(s.magicHome, "plugins")
		pluginPath, err := SafeJoin(pluginsDir, name)
		if err != nil {
			http.Error(w, "Invalid plugin path", http.StatusBadRequest)
			return
		}

		// Check if it's a file or directory
		info, err := os.Stat(pluginPath)
		if err != nil {
			http.Error(w, "Plugin not found", http.StatusNotFound)
			return
		}

		// Remove plugin
		if info.IsDir() {
			err = os.RemoveAll(pluginPath)
		} else {
			err = os.Remove(pluginPath)
		}

		if err != nil {
			http.Error(w, "Failed to delete plugin", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "deleted": true})
		return
	}

	// Handle visibility (legacy)
	if strings.HasSuffix(path, "/visibility") {
		name := strings.TrimSuffix(path, "/visibility")
		if r.Method == http.MethodPost {
			var req struct {
				Hidden bool `json:"hidden"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "hidden": req.Hidden})
			return
		}
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handlePluginProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req struct {
			MemoryProvider string `json:"memory_provider"`
			ContextEngine  string `json:"context_engine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"ok":              true,
			"memory_provider": req.MemoryProvider,
			"context_engine":  req.ContextEngine,
		})
		return
	}
	// GET
	jsonResponse(w, map[string]interface{}{
		"memory_provider": "",
		"memory_options":  []map[string]interface{}{},
		"context_engine":  "",
		"context_options": []map[string]interface{}{},
	})
}

func (s *Server) handleDashboardPlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Use unified scanPluginsDir logic
		plugins := s.scanPluginsDir()
		jsonResponse(w, plugins)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	plugins := s.scanPluginsDir()
	jsonResponse(w, plugins)
}

func (s *Server) handlePluginsStatistics(w http.ResponseWriter, r *http.Request) {
	// Return empty statistics for now (would need effectiveness manager integration)
	stats := []map[string]interface{}{}
	jsonResponse(w, stats)
}

func (s *Server) handleDashboardPluginsRescan(w http.ResponseWriter, r *http.Request) {
	// Frontend uses POST, support both GET and POST
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	plugins := s.scanPluginsDir()
	// Return the plugins list so frontend can update
	jsonResponse(w, plugins)
}
