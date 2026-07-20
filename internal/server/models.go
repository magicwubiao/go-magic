package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

func (s *Server) handleModelSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope    string `json:"scope"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Task     string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// If only changing model (not provider), try to use Modeler interface
	if req.Provider == "" && req.Model != "" && s.provider != nil {
		if modeler, ok := provider.GetModeler(s.provider); ok {
			if err := modeler.SetModel(req.Model); err == nil {
				// Update config for persistence: move model to first position in models array
				if s.cfg != nil && s.cfg.Providers != nil {
					provName := s.cfg.Provider
					if provCfg, ok := s.cfg.Providers[provName]; ok {
						// Remove model if exists and add to front
						newModels := []string{req.Model}
						for _, m := range provCfg.Models {
							if m != req.Model {
								newModels = append(newModels, m)
							}
						}
						provCfg.Models = newModels
						s.cfg.Providers[provName] = provCfg
						// Also update the top-level model field for consistency
						s.cfg.Model = req.Model
						configPath := filepath.Join(s.magicHome, "config.json")
						data, _ := json.MarshalIndent(s.cfg, "", "  ")
						os.WriteFile(configPath, data, 0644)
					}
				}
				jsonResponse(w, map[string]interface{}{
					"ok":       true,
					"scope":    req.Scope,
					"provider": s.cfg.Provider,
					"model":    req.Model,
					"message":  "model switched dynamically",
				})
				return
			}
		}
	}

	// Full provider switch (recreate provider)
	if req.Provider != "" && req.Model != "" {
		s.cfg.Provider = req.Provider
		s.cfg.Model = req.Model
		// Update provider models array: move selected model to front
		if s.cfg.Providers != nil {
			if provCfg, ok := s.cfg.Providers[req.Provider]; ok {
				newModels := []string{req.Model}
				for _, m := range provCfg.Models {
					if m != req.Model {
						newModels = append(newModels, m)
					}
				}
				provCfg.Models = newModels
				s.cfg.Providers[req.Provider] = provCfg
			}
		}
		// Save config
		configPath := filepath.Join(s.magicHome, "config.json")
		data, _ := json.MarshalIndent(s.cfg, "", "  ")
		os.WriteFile(configPath, data, 0644)
		// Recreate provider
		s.provider = createProvider(s.cfg)
		// Clear all agents to force re-creation
		s.agents = make(map[string]*agent.Agent)
	}

	jsonResponse(w, map[string]interface{}{
		"ok":       true,
		"scope":    req.Scope,
		"provider": req.Provider,
		"model":    req.Model,
	})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	providers := make([]map[string]interface{}, 0)
	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			providers = append(providers, map[string]interface{}{
				"id":       name,
				"name":     name,
				"enabled":  true,
				"api_key":  maskAPIKey(provCfg.APIKey),
				"base_url": provCfg.BaseURL,
				"model":    provCfg.GetCurrentModel(),
				"models":   provCfg.Models,
			})
		}
	}
	jsonResponse(w, providers)
}

func (s *Server) handleCircuitReset(w http.ResponseWriter, r *http.Request) {
	if s.provider == nil {
		http.Error(w, "no provider configured", 400)
		return
	}

	// Try to reset circuit breaker using type switch for embedded BaseProvider
	switch p := s.provider.(type) {
	case *provider.DeepSeekProvider:
		if p.OpenAICompatibleProvider != nil && p.OpenAICompatibleProvider.BaseProvider != nil {
			p.OpenAICompatibleProvider.BaseProvider.ResetCircuitBreaker()
		}
	case *provider.OpenAICompatibleProvider:
		if p.BaseProvider != nil {
			p.BaseProvider.ResetCircuitBreaker()
		}
	}

	jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "circuit breaker reset",
	})
}

func (s *Server) handleModelByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/models/")
	jsonResponse(w, map[string]interface{}{
		"id":         id,
		"name":       id,
		"contextLen": 128000,
	})
}

func (s *Server) handleModelInfo(w http.ResponseWriter, r *http.Request) {
	providerName := s.cfg.Provider
	modelName := s.cfg.GetCurrentModel()

	// Try to get current model from Modeler interface
	if s.provider != nil {
		if modeler, ok := provider.GetModeler(s.provider); ok {
			modelName = modeler.GetModel()
		}
	}

	if modelName == "" {
		modelName = "default"
	}

	// Infer context length and capabilities from model name
	contextLen := 128000
	maxOutput := 4096
	supportsVision := false
	supportsReasoning := false
	modelFamily := providerName
	modelDisplayName := modelName

	// Try to get accurate model info from Modeler interface
	if s.provider != nil {
		if modeler, ok := provider.GetModeler(s.provider); ok {
			for _, m := range modeler.ListModels() {
				if m.ID == modelName {
					if m.ContextLen > 0 {
						contextLen = m.ContextLen
					}
					if m.Name != "" {
						modelDisplayName = m.Name
					}
					break
				}
			}
		}
	}

	modelLower := strings.ToLower(modelName)
	switch {
	case strings.Contains(modelLower, "gpt-4o"):
		contextLen = 128000
		maxOutput = 16384
		supportsVision = true
		modelFamily = "openai"
	case strings.Contains(modelLower, "gpt-4-turbo") || strings.Contains(modelLower, "gpt-4-1106"):
		contextLen = 128000
		maxOutput = 4096
		supportsVision = true
		modelFamily = "openai"
	case strings.Contains(modelLower, "gpt-4"):
		contextLen = 8192
		maxOutput = 8192
		modelFamily = "openai"
	case strings.Contains(modelLower, "gpt-3.5"):
		contextLen = 16385
		maxOutput = 4096
		modelFamily = "openai"
	case strings.Contains(modelLower, "claude-3-5") || strings.Contains(modelLower, "claude-3.5") || strings.Contains(modelLower, "claude-4"):
		contextLen = 200000
		maxOutput = 8192
		supportsVision = true
		modelFamily = "anthropic"
	case strings.Contains(modelLower, "claude-3"):
		contextLen = 200000
		maxOutput = 4096
		supportsVision = true
		modelFamily = "anthropic"
	case strings.Contains(modelLower, "claude"):
		contextLen = 100000
		maxOutput = 4096
		modelFamily = "anthropic"
	case strings.Contains(modelLower, "deepseek"):
		contextLen = 64000
		maxOutput = 8192
		modelFamily = "deepseek"
	case strings.Contains(modelLower, "gemini"):
		contextLen = 1000000
		maxOutput = 8192
		supportsVision = true
		modelFamily = "google"
	case strings.Contains(modelLower, "llama"):
		contextLen = 128000
		maxOutput = 4096
		modelFamily = "meta"
	case strings.Contains(modelLower, "qwen"):
		contextLen = 128000
		maxOutput = 8192
		modelFamily = "alibaba"
	case strings.Contains(modelLower, "glm") || strings.Contains(modelLower, "chatglm"):
		contextLen = 128000
		maxOutput = 4096
		modelFamily = "zhipu"
	case strings.Contains(modelLower, "o1") || strings.Contains(modelLower, "o3"):
		contextLen = 200000
		maxOutput = 100000
		supportsReasoning = true
		supportsVision = true
		modelFamily = "openai"
	}

	// Try to get capabilities from provider
	supportsTools := true
	if s.provider != nil {
		caps := provider.GetCapabilities(s.provider)
		if caps != nil {
			supportsTools = caps.ToolCalling
			if caps.Vision {
				supportsVision = true
			}
		}
	}

	jsonResponse(w, map[string]interface{}{
		"model":                    fmt.Sprintf("%s/%s", providerName, modelName),
		"model_display_name":       modelDisplayName,
		"provider":                 providerName,
		"auto_context_length":      contextLen,
		"config_context_length":    0,
		"effective_context_length": contextLen,
		"capabilities": map[string]interface{}{
			"supports_tools":     supportsTools,
			"supports_vision":    supportsVision,
			"supports_reasoning": supportsReasoning,
			"context_window":     contextLen,
			"max_output_tokens":  maxOutput,
			"model_family":       modelFamily,
		},
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := make([]map[string]interface{}, 0)
	seen := make(map[string]bool)

	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			// Collect all models from the provider's Models array and Model field
			modelSet := make(map[string]bool)

			// Add models from Models array (multi-model support)
			for _, m := range provCfg.Models {
				modelSet[m] = true
			}

			// If no models configured, use "default"
			if len(modelSet) == 0 {
				modelSet["default"] = true
			}

			// Generate model entries
			for modelName := range modelSet {
				id := fmt.Sprintf("%s/%s", name, modelName)
				if !seen[id] {
					seen[id] = true
					models = append(models, map[string]interface{}{
						"id":         id,
						"name":       modelName,
						"provider":   name,
						"contextLen": 128000,
					})
				}
			}
		}
	}

	// Always include current provider's models
	if s.cfg != nil && s.cfg.Provider != "" {
		modelSet := make(map[string]bool)

		// Add from Models array
		if s.cfg.Providers != nil {
			if provCfg, ok := s.cfg.Providers[s.cfg.Provider]; ok {
				for _, m := range provCfg.Models {
					modelSet[m] = true
				}
			}
		}

		for modelName := range modelSet {
			id := fmt.Sprintf("%s/%s", s.cfg.Provider, modelName)
			if !seen[id] {
				models = append(models, map[string]interface{}{
					"id":         id,
					"name":       modelName,
					"provider":   s.cfg.Provider,
					"contextLen": 128000,
				})
			}
		}
	}

	if len(models) == 0 {
		models = append(models, map[string]interface{}{
			"id":         "default/default",
			"name":       "default",
			"provider":   "default",
			"contextLen": 128000,
		})
	}

	jsonResponse(w, models)
}

func (s *Server) handleModelAuxiliary(w http.ResponseWriter, r *http.Request) {
	auxiliaryModels := make([]map[string]interface{}, 0)

	// Try to read auxiliary models from config providers
	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			if name == s.cfg.Provider {
				continue // skip primary model
			}
			auxiliaryModels = append(auxiliaryModels, map[string]interface{}{
				"id":         name,
				"name":       name,
				"model":      provCfg.GetCurrentModel(),
				"contextLen": 128000,
			})
		}
	}

	// If no auxiliary models found from config, return reasonable defaults
	if len(auxiliaryModels) == 0 {
		auxiliaryModels = []map[string]interface{}{
			{"id": "auto", "name": "Auto", "model": "", "contextLen": 128000},
		}
	}

	jsonResponse(w, auxiliaryModels)
}

func (s *Server) handleProvidersSubRoutes(w http.ResponseWriter, r *http.Request) {
	// Support both /api/providers/{name}/* and /api/platforms/{name}/*
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/api/providers/")
	path = strings.TrimPrefix(path, "/api/platforms/")

	// Extract provider name and sub-route
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]
	subRoute := ""
	if len(parts) > 1 {
		subRoute = parts[1]
	}

	// Handle GET /{name} - get single provider
	if r.Method == http.MethodGet && subRoute == "" {
		if s.cfg != nil && s.cfg.Providers != nil {
			if provCfg, ok := s.cfg.Providers[name]; ok {
				jsonResponse(w, ProviderInfo{
					Name:    name,
					Label:   name,
					BaseURL: provCfg.BaseURL,
					Models:  provCfg.Models,
					APIKey:  maskAPIKey(provCfg.APIKey),
				})
				return
			}
		}
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	// Handle PUT /{name} - update provider
	if r.Method == http.MethodPut && subRoute == "" {
		var req struct {
			BaseURL string   `json:"base_url"`
			Model   string   `json:"model"`
			APIKey  string   `json:"api_key"`
			Models  []string `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		// Update provider config
		if s.cfg != nil {
			if s.cfg.Providers == nil {
				s.cfg.Providers = make(map[string]appconfig.ProviderConfig)
			}
			provCfg := s.cfg.Providers[name]
			if req.BaseURL != "" {
				provCfg.BaseURL = req.BaseURL
			}
			if req.APIKey != "" {
				provCfg.APIKey = req.APIKey
			}
			// Models array: first element is current model
			if req.Models != nil {
				provCfg.Models = req.Models
				// If this is the current provider, also update top-level model
				if s.cfg.Provider == name && len(req.Models) > 0 {
					s.cfg.Model = req.Models[0]
				}
			}
			s.cfg.Providers[name] = provCfg
			s.cfg.Save()
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name})
		return
	}

	// Handle POST /{name} - create provider (alias for PUT to create new)
	if r.Method == http.MethodPost && subRoute == "" {
		var req struct {
			Name    string   `json:"name"`
			BaseURL string   `json:"base_url"`
			APIKey  string   `json:"api_key"`
			Models  []string `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		// Use provided name or fallback to URL parameter
		providerName := req.Name
		if providerName == "" {
			providerName = name
		}
		// Create/update provider config
		if s.cfg != nil {
			if s.cfg.Providers == nil {
				s.cfg.Providers = make(map[string]appconfig.ProviderConfig)
			}
			provCfg := s.cfg.Providers[providerName]
			if req.BaseURL != "" {
				provCfg.BaseURL = req.BaseURL
			}
			if req.APIKey != "" {
				provCfg.APIKey = req.APIKey
			}
			// Models array: first element is current model
			if req.Models != nil {
				provCfg.Models = req.Models
				// If this is the current provider, also update top-level model
				if s.cfg.Provider == providerName && len(req.Models) > 0 {
					s.cfg.Model = req.Models[0]
				}
			}
			s.cfg.Providers[providerName] = provCfg
			s.cfg.Save()
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": providerName, "created": true})
		return
	}

	// Handle DELETE /{name} - delete provider
	if r.Method == http.MethodDelete && subRoute == "" {
		if s.cfg != nil && s.cfg.Providers != nil {
			if _, exists := s.cfg.Providers[name]; exists {
				delete(s.cfg.Providers, name)
				// If deleted provider was current, clear top-level fields
				if s.cfg.Provider == name {
					s.cfg.Provider = ""
					s.cfg.Model = ""
				}
				s.cfg.Save()
				jsonResponse(w, map[string]interface{}{"ok": true, "name": name})
				return
			}
		}
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	// Handle POST /{name}/enable - enable provider
	if r.Method == http.MethodPost && subRoute == "enable" {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": true})
		return
	}

	// Handle POST /{name}/disable - disable provider
	if r.Method == http.MethodPost && subRoute == "disable" {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "enabled": false})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handleModelOptions(w http.ResponseWriter, r *http.Request) {
	providerList := make([]map[string]interface{}, 0)
	providerNames := make(map[string]bool) // Track which providers are already added

	// First: Add all configured providers from config
	if s.cfg != nil && s.cfg.Providers != nil {
		for name, provCfg := range s.cfg.Providers {
			isCurrent := name == s.cfg.Provider
			providerNames[name] = true

			// Get models from config first (user-configured models take priority)
			models := []string{}
			if len(provCfg.Models) > 0 {
				// Use user-configured models from config file
				models = provCfg.Models
			}

			// Only use Modeler interface as fallback when no config is available
			if len(models) == 0 && isCurrent && s.provider != nil {
				if modeler, ok := provider.GetModeler(s.provider); ok {
					modelInfos := modeler.ListModels()
					models = make([]string, len(modelInfos))
					for i, m := range modelInfos {
						models[i] = m.ID
					}
				}
			}

			providerList = append(providerList, map[string]interface{}{
				"name":         name,
				"slug":         name,
				"models":       models,
				"total_models": len(models),
				"is_current":   isCurrent,
			})
		}
	}

	// Second: Add built-in providers that are not yet in the list
	builtinProviders := appconfig.ListProviders()
	for _, bp := range builtinProviders {
		if !providerNames[bp.Name] {
			isCurrent := s.cfg != nil && bp.Name == s.cfg.Provider
			providerList = append(providerList, map[string]interface{}{
				"name":         bp.Name,
				"slug":         bp.Name,
				"display_name": bp.DisplayName,
				"models":       bp.Models,
				"total_models": len(bp.Models),
				"is_current":   isCurrent,
			})
		}
	}

	model := ""
	currentProviderName := ""
	if s.cfg != nil {
		model = s.cfg.GetCurrentModel()
		currentProviderName = s.cfg.Provider
		// Try to get current model from Modeler interface
		if s.provider != nil {
			if modeler, ok := provider.GetModeler(s.provider); ok {
				model = modeler.GetModel()
			}
		}
	}
	jsonResponse(w, map[string]interface{}{
		"model":     model,
		"provider":  currentProviderName,
		"providers": providerList,
	})
}
