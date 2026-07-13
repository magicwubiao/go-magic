package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		profiles := s.scanProfiles()
		jsonResponse(w, map[string]interface{}{"profiles": profiles})
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Name             string `json:"name"`
			CloneFromDefault bool   `json:"clone_from_default"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		profileDir := filepath.Join(s.magicHome, "profiles", req.Name)
		os.MkdirAll(profileDir, 0755)

		// Clone from default profile if requested
		if req.CloneFromDefault {
			defaultDir := s.magicHome
			if s.cfg.Profile != "" && s.cfg.Profile != "default" {
				defaultDir = filepath.Join(s.magicHome, "profiles", s.cfg.Profile)
			}

			// Copy .env file if exists
			defaultEnv := filepath.Join(defaultDir, ".env")
			if _, err := os.Stat(defaultEnv); err == nil {
				data, _ := os.ReadFile(defaultEnv)
				os.WriteFile(filepath.Join(profileDir, ".env"), data, 0600)
			}

			// Copy skills directory if exists
			defaultSkills := filepath.Join(defaultDir, "skills")
			if _, err := os.Stat(defaultSkills); err == nil {
				copyDir(defaultSkills, filepath.Join(profileDir, "skills"))
			}

			// Copy soul.md if exists
			defaultSoul := filepath.Join(defaultDir, "soul.md")
			if _, err := os.Stat(defaultSoul); err == nil {
				data, _ := os.ReadFile(defaultSoul)
				os.WriteFile(filepath.Join(profileDir, "soul.md"), data, 0644)
			}
		}

		jsonResponse(w, map[string]interface{}{"ok": true, "name": req.Name, "path": profileDir})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleProfileByName(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/profiles/")

	if strings.HasSuffix(path, "/setup-command") {
		name := strings.TrimSuffix(path, "/setup-command")
		jsonResponse(w, map[string]string{"command": fmt.Sprintf("magic --profile %s chat", name)})
		return
	}
	if strings.HasSuffix(path, "/soul") {
		name := strings.TrimSuffix(path, "/soul")
		soulPath := filepath.Join(s.magicHome, "profiles", name, "soul.md")
		if r.Method == http.MethodGet {
			data, _ := os.ReadFile(soulPath)
			exists := false
			if _, err := os.Stat(soulPath); err == nil {
				exists = true
			}
			// Frontend expects "content" field, not "soul"
			jsonResponse(w, map[string]interface{}{"content": string(data), "exists": exists})
			return
		}
		if r.Method == http.MethodPut {
			var req struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			os.WriteFile(soulPath, []byte(req.Content), 0644)
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}
	}

	// --- User Profile (user.md) ---
	if strings.HasSuffix(path, "/user") {
		name := strings.TrimSuffix(path, "/user")
		userPath := filepath.Join(s.magicHome, "profiles", name, "user.md")
		if r.Method == http.MethodGet {
			data, _ := os.ReadFile(userPath)
			exists := false
			if _, err := os.Stat(userPath); err == nil {
				exists = true
			}
			// Parse user.md content into structured data
			userData := s.parseUserMD(string(data))
			jsonResponse(w, map[string]interface{}{
				"content": string(data),
				"exists":  exists,
				"data":    userData,
			})
			return
		}
		if r.Method == http.MethodPut {
			var req struct {
				Content string                 `json:"content"`
				Data    map[string]interface{} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			// If data is provided, regenerate content
			content := req.Content
			if req.Data != nil {
				content = s.generateUserMD(req.Data)
			}
			os.WriteFile(userPath, []byte(content), 0644)
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}
	}

	// --- User Preferences (from Cortex) ---
	if strings.HasSuffix(path, "/preferences") {
		name := strings.TrimSuffix(path, "/preferences")
		if r.Method == http.MethodGet {
			// Return preferences from Cortex UserProfile
			preferences := s.getUserPreferences(name)
			jsonResponse(w, map[string]interface{}{"preferences": preferences})
			return
		}
	}

	// --- Preference Feedback ---
	if strings.Contains(path, "/preferences/") && strings.HasSuffix(path, "/feedback") {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			name := parts[0]
			key := parts[2]
			if r.Method == http.MethodPost {
				var req struct {
					Accurate bool `json:"accurate"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "Invalid request body", http.StatusBadRequest)
					return
				}
				s.handlePreferenceFeedback(name, key, req.Accurate)
				jsonResponse(w, map[string]bool{"ok": true})
				return
			}
		}
	}
	if strings.HasSuffix(path, "/switch") && r.Method == http.MethodPost {
		name := strings.TrimSuffix(path, "/switch")
		s.mu.Lock()
		s.cfg.Profile = name
		configPath := filepath.Join(s.magicHome, "config.json")
		data, _ := json.MarshalIndent(s.cfg, "", "  ")
		os.WriteFile(configPath, data, 0644)
		s.mu.Unlock()
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}

	if r.Method == http.MethodDelete {
		name := path
		profileDir := filepath.Join(s.magicHome, "profiles", name)
		os.RemoveAll(profileDir)
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}

	if r.Method == http.MethodPatch {
		// Rename profile - frontend sends "new_name", support both "new_name" and "name"
		var req struct {
			Name    string `json:"name"`
			NewName string `json:"new_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		newName := req.NewName
		if newName == "" {
			newName = req.Name
		}
		oldDir := filepath.Join(s.magicHome, "profiles", path)
		newDir := filepath.Join(s.magicHome, "profiles", newName)
		os.Rename(oldDir, newDir)
		jsonResponse(w, map[string]interface{}{"ok": true, "name": newName, "path": newDir})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}
