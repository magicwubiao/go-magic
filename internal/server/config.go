package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/approval"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

func (s *Server) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	// Build provider options dynamically from appconfig.ListProviders() so the schema
	// stays in sync with the full set of supported providers (currently 23).
	providerOptions := make([]string, 0, 32)
	for _, p := range appconfig.ListProviders() {
		providerOptions = append(providerOptions, p.Name)
	}

	schema := map[string]interface{}{
		"fields": map[string]interface{}{
			"provider": map[string]interface{}{
				"type":    "string",
				"default": "deepseek",
				"options": providerOptions,
			},
			"model": map[string]interface{}{
				"type":       "string",
				"default":    "deepseek-v4-flash",
				"deprecated": true,
			},
			"cortex.enabled": map[string]interface{}{
				"type":     "boolean",
				"default":  true,
				"category": "cortex",
			},
			"cortex.skill_min_pattern_freq": map[string]interface{}{
				"type":     "number",
				"default":  3,
				"category": "cortex",
				"min":      1,
				"max":      10,
			},
			"secret_redaction": map[string]interface{}{
				"type":    "boolean",
				"default": true,
			},
			"gateway.enabled": map[string]interface{}{
				"type":    "boolean",
				"default": false,
			},
		},
		"category_order": []string{"general", "provider", "model", "tools", "cortex", "gateway"},
	}
	jsonResponse(w, schema)
}

func (s *Server) scanProfiles() []map[string]interface{} {
	profilesDir := filepath.Join(s.magicHome, "profiles")
	profiles := make([]map[string]interface{}, 0)

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		// Return default profile
		return []map[string]interface{}{
			{"name": "default", "path": s.magicHome, "is_default": true, "model": nil, "provider": nil, "has_env": false, "skill_count": 0},
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profilePath := filepath.Join(profilesDir, entry.Name())
		// Check if profile has env file
		hasEnv := false
		if _, err := os.Stat(filepath.Join(profilePath, ".env")); err == nil {
			hasEnv = true
		}
		// Count skills in profile
		skillCount := 0
		skillsDir := filepath.Join(profilePath, "skills")
		if skills, err := os.ReadDir(skillsDir); err == nil {
			skillCount = len(skills)
		}
		profiles = append(profiles, map[string]interface{}{
			"name":        entry.Name(),
			"path":        profilePath,
			"is_default":  entry.Name() == s.cfg.Profile,
			"model":       nil,
			"provider":    nil,
			"has_env":     hasEnv,
			"skill_count": skillCount,
		})
	}

	if len(profiles) == 0 {
		profiles = append(profiles, map[string]interface{}{
			"name": "default", "path": s.magicHome, "is_default": true, "model": nil, "provider": nil, "has_env": false, "skill_count": 0,
		})
	}

	return profiles
}

func (s *Server) handleConfigByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/config/")

	// Handle sub-routes
	switch id {
	case "defaults":
		s.handleConfigDefaults(w, r)
		return
	case "raw":
		s.handleConfigRaw(w, r)
		return
	case "schema":
		s.handleConfigSchema(w, r)
		return
	}

	if s.cfg == nil {
		jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
		return
	}

	// Get specific config value
	cfgMap := make(map[string]interface{})
	data, _ := json.Marshal(s.cfg)
	json.Unmarshal(data, &cfgMap)

	if val, ok := cfgMap[id]; ok {
		jsonResponse(w, map[string]interface{}{"id": id, "value": val})
		return
	}
	jsonResponse(w, map[string]interface{}{"id": id, "value": nil})
}

func (s *Server) parseUserMD(content string) map[string]interface{} {
	data := map[string]interface{}{
		"name":                "",
		"role":                "",
		"communication_style": "",
		"code_style":          "",
		"tech_stack":          []string{},
		"interests":           []string{},
	}

	lines := strings.Split(content, "\n")
	currentSection := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			currentSection = strings.ToLower(strings.TrimPrefix(line, "## "))
			continue
		}

		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)

			if idx := strings.Index(line, ":"); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				value := strings.TrimSpace(line[idx+1:])

				switch currentSection {
				case "about":
					if strings.EqualFold(key, "Name") {
						data["name"] = value
					} else if strings.EqualFold(key, "Role") {
						data["role"] = value
					}
				case "preferences":
					if strings.EqualFold(key, "Communication style") {
						data["communication_style"] = value
					} else if strings.EqualFold(key, "Code style") {
						data["code_style"] = value
					}
				case "tech stack":
					if value != "" && value != "[Not set]" {
						if stack, ok := data["tech_stack"].([]string); ok {
							data["tech_stack"] = append(stack, value)
						}
					}
				case "interests":
					if value != "" && value != "[Not set]" {
						if interests, ok := data["interests"].([]string); ok {
							data["interests"] = append(interests, value)
						}
					}
				}
			}
		}
	}

	return data
}

func (s *Server) handleSettingByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/settings/")

	// Handle /api/settings/profiles/* sub-routes
	if strings.HasPrefix(id, "profiles/") {
		s.handleSettingsProfiles(w, r)
		return
	}

	jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
}

func (s *Server) deleteEnvVar(path string, key string) {
	envVars := s.readEnvFile(path)
	delete(envVars, key)
	s.writeEnvFile(path, envVars)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// Merge settings into config
		configPath := filepath.Join(s.magicHome, "config.json")
		data, _ := json.MarshalIndent(s.cfg, "", "  ")
		os.WriteFile(configPath, data, 0644)
		jsonResponse(w, map[string]bool{"ok": true})
		return
	}
	jsonResponse(w, map[string]interface{}{
		"general": map[string]interface{}{
			"language": "en",
			"timezone": "UTC",
		},
		"appearance": map[string]interface{}{
			"theme":    "dark",
			"fontSize": 14,
		},
	})
}

func (s *Server) handleSettingsProfiles(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/settings/profiles/")

	// List profiles
	if path == "" || path == "/" {
		profiles := s.scanProfiles()
		jsonResponse(w, map[string]interface{}{"profiles": profiles})
		return
	}

	// Handle switch: /api/settings/profiles/{name}/switch
	if strings.HasSuffix(path, "/switch") {
		name := strings.TrimSuffix(path, "/switch")
		// Actually switch profile
		if s.cfg != nil {
			s.cfg.Profile = name
			if err := s.cfg.Save(); err != nil {
				http.Error(w, "Failed to save config: "+err.Error(), 500)
				return
			}
			// Reload config to ensure memory is in sync
			if newCfg, err := appconfig.Load(); err == nil {
				s.cfg = newCfg
			}
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "switched": true})
		return
	}

	// Handle delete: /api/settings/profiles/{name}
	if r.Method == http.MethodDelete {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": path, "deleted": true})
		return
	}

	// Handle create: POST /api/settings/profiles
	if r.Method == http.MethodPost {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": req.Name, "created": true})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handleConfigRaw(w http.ResponseWriter, r *http.Request) {
	configPath := filepath.Join(s.magicHome, "config.json")

	switch r.Method {
	case "GET":
		data, err := os.ReadFile(configPath)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"yaml": "{}"})
			return
		}
		jsonResponse(w, map[string]interface{}{"yaml": string(data)})
	case "PUT":
		var req struct {
			JsonText string `json:"json_text"`
			YamlText string `json:"yaml_text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		// Support both json_text and yaml_text (frontend uses yaml_text)
		content := req.JsonText
		if content == "" {
			content = req.YamlText
		}
		// Validate JSON
		var parsed interface{}
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		// Write atomically
		tmpPath := configPath + ".tmp"
		os.WriteFile(tmpPath, []byte(content), 0644)
		os.Rename(tmpPath, configPath)
		// Reload config
		newCfg, err := appconfig.Load()
		if err == nil {
			s.mu.Lock()
			s.cfg = newCfg
			s.provider = createProvider(s.cfg)
			s.agents = make(map[string]*agent.Agent)
			s.mu.Unlock()
		}
		jsonResponse(w, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) writeEnvFile(path string, vars map[string]string) {
	var lines []string
	for k, v := range vars {
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600)
}

func (s *Server) readEnvFile(path string) map[string]string {
	result := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func (s *Server) getUserPreferences(profileName string) []map[string]interface{} {
	// For now, return mock data
	// In production, this should read from Cortex UserProfile
	return []map[string]interface{}{
		{
			"key":        "communication_style",
			"value":      "简洁",
			"context":    "多次要求简短回答",
			"confidence": 0.85,
			"source":     "learned",
		},
		{
			"key":        "preferred_language",
			"value":      "Go",
			"context":    "多次使用 Go 示例",
			"confidence": 0.92,
			"source":     "learned",
		},
	}
}

func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) {
	envPath := filepath.Join(s.magicHome, ".env")

	switch r.Method {
	case "GET":
		envVars := s.readEnvFile(envPath)
		envResponse := make(map[string]interface{})
		for key, value := range envVars {
			info := map[string]interface{}{
				"is_set":         value != "",
				"redacted_value": "",
				"description":    "",
				"url":            nil,
				"category":       "",
				"is_password":    false,
				"tools":          []string{},
				"advanced":       false,
			}
			if value != "" {
				info["redacted_value"] = "****"
			}
			// Enrich known keys with description and category
			switch key {
			case "DEEPSEEK_API_KEY":
				info["description"] = "API key for DeepSeek provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "OPENAI_API_KEY":
				info["description"] = "API key for OpenAI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "ANTHROPIC_API_KEY":
				info["description"] = "API key for Anthropic provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "GOOGLE_API_KEY":
				info["description"] = "API key for Google AI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "OPENROUTER_API_KEY":
				info["description"] = "API key for OpenRouter provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "GROQ_API_KEY":
				info["description"] = "API key for Groq provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "TOGETHER_API_KEY":
				info["description"] = "API key for Together AI provider"
				info["category"] = "provider"
				info["is_password"] = true
				info["tools"] = []string{"chat"}
			case "FIRECRAWL_API_KEY":
				info["description"] = "API key for Firecrawl web scraping"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "TAVILY_API_KEY":
				info["description"] = "API key for Tavily search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "EXA_API_KEY":
				info["description"] = "API key for Exa search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "SERPAPI_API_KEY":
				info["description"] = "API key for SerpAPI search"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"web"}
			case "GITHUB_TOKEN":
				info["description"] = "GitHub personal access token"
				info["category"] = "tool"
				info["is_password"] = true
				info["tools"] = []string{"git"}
			case "GO_MAGIC_HOME":
				info["description"] = "Path to the Magic home directory"
				info["category"] = "general"
				info["is_password"] = false
				info["tools"] = []string{}
			}
			envResponse[key] = info
		}
		jsonResponse(w, envResponse)
	case "POST", "PUT":
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		s.writeEnvVar(envPath, req.Key, req.Value)
		jsonResponse(w, map[string]bool{"ok": true})
	case "DELETE":
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		s.deleteEnvVar(envPath, req.Key)
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Always re-read from file to pick up changes made by gateway process (e.g. QR login token)
		if freshCfg, err := appconfig.Load(); err == nil {
			s.cfg = freshCfg
		}
		if s.cfg == nil {
			jsonResponse(w, map[string]interface{}{})
			return
		}
		jsonResponse(w, s.cfg)
	case "PUT":
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), 400)
			return
		}
		// Get config payload - support both {config: {...}} and direct config object
		var configData map[string]interface{}
		if cfg, ok := req["config"].(map[string]interface{}); ok {
			configData = cfg
		} else {
			configData = req
		}

		// Handle dot-notation keys (e.g. "memory.enabled") by expanding into nested objects
		expanded := expandDotKeys(configData)

		// Merge into config
		if s.cfg == nil {
			s.cfg = appconfig.DefaultConfig()
		}
		data, _ := json.Marshal(expanded)
		if err := json.Unmarshal(data, s.cfg); err != nil {
			http.Error(w, "failed to merge config: "+err.Error(), 500)
			return
		}
		// Save
		configPath := filepath.Join(s.magicHome, "config.json")
		saveData, _ := json.MarshalIndent(s.cfg, "", "  ")
		if err := os.WriteFile(configPath, saveData, 0644); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), 500)
			return
		}

		// Check which config sections changed and hot-reload accordingly
		hotReloadKeys := []string{"provider", "model", "api_key", "base_url", "secret_redaction", "profile", "working_dir"}
		needsProviderReload := false

		for key := range expanded {
			for _, reloadKey := range hotReloadKeys {
				if key == reloadKey {
					needsProviderReload = true
					break
				}
			}
		}

		// Hot-reload provider if provider-related config changed
		if needsProviderReload {
			s.mu.Lock()
			s.provider = createProvider(s.cfg)
			s.mu.Unlock()
		}

		// Hot-reload approval config if approval section changed
		if _, ok := expanded["approval"]; ok && s.approvalMgr != nil {
			if ac := s.cfg.Approval; ac != nil {
				// 校验 strategy 合法性，空字符串保持不变
				if ac.Strategy != "" {
					switch ac.Strategy {
					case "manual", "auto", "smart", "whitelist":
						s.approvalMgr.SetStrategy(approval.Strategy(ac.Strategy))
					}
				}
				// 使用 SetXxx 方法，避免修改 GetConfig() 返回的局部拷贝
				if ac.TrustThreshold > 0 {
					s.approvalMgr.SetTrustThreshold(ac.TrustThreshold)
				}
				s.approvalMgr.SetEnableLearning(ac.EnableLearning)
				s.approvalMgr.SetEnableCLIConfirm(ac.EnableCLIConfirm)
				if ac.ApprovalTimeout > 0 {
					s.approvalMgr.SetApprovalTimeout(ac.ApprovalTimeout)
				}
			}
		}

		// Hot-reload privacy/PII config: clear agent cache so new sessions pick up the new redactor
		if _, ok := expanded["privacy"]; ok {
			s.mu.Lock()
			s.agents = make(map[string]*agent.Agent)
			s.mu.Unlock()
		}

		// Bot Mode: history_window / inject_bot_protocol apply to the running
		// manager immediately; toggling enabled requires a full restart, which
		// the UI surfaces as a warning banner (bot_mode.enabled is read by
		// initBotMode only when the manager is (re)created).
		if _, ok := expanded["bot_mode"]; ok && s.botManager != nil {
			s.botManager.ReloadConfig(s.cfg)
		}

		// Return updated config
		jsonResponse(w, s.cfg)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) writeEnvVar(path string, key, value string) {
	envVars := s.readEnvFile(path)
	envVars[key] = value
	s.writeEnvFile(path, envVars)
}

func (s *Server) handleConfigDefaults(w http.ResponseWriter, r *http.Request) {
	defaults := appconfig.DefaultConfig()
	jsonResponse(w, defaults)
}
