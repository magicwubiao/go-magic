package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
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

	// 先让内存快照跟上磁盘，再进临界区。
	//
	// 这一步绝不能在持 s.mu 时做：syncConfigFromDisk 内部要拿 s.agentsMu，
	// 而反向路径（approval.go 审批广播、sessions.go 工作目录设置）持
	// s.agentsMu 再读 s.cfg/s.provider。两条相反顺序叠加就是 AB-BA 死锁——
	// 表现为"chat 页切完模型、再打开模型供应商页，整个后端卡死到重启"
	// （回归见 model_switch_deadlock_test.go）。
	s.syncConfigFromDisk()

	// s.mu 负责配置内容各字段之间的逻辑串行化；cfgMu 负责 s.cfg 指针/
	// 对象的读写（写入侧 setCfg 由 cfgMu 保护，只拿 s.mu 并不与之互斥）。
	// cfgMu 是叶子锁，持 s.mu 时再拿它安全。
	s.acquireServerMu("")
	defer s.releaseServerMu()

	// If only changing model (not provider), try to use Modeler interface
	if req.Provider == "" && req.Model != "" && s.provider != nil {
		if modeler, ok := provider.GetModeler(s.provider); ok {
			if err := modeler.SetModel(req.Model); err == nil {
				// Update config for persistence: move model to first position in models array
				var respProvider string
				s.lockCfgForWrite()
				// 直接读 s.cfg：已持 cfgMu，走 cfgSnapshot 会自锁
				// （sync.Mutex 不可重入）。该值已是本写点的私有副本。
				if cfg := s.cfg; cfg != nil && cfg.Providers != nil {
					provName := cfg.Provider
					if provCfg, ok := cfg.Providers[provName]; ok {
						// Remove model if exists and add to front
						newModels := []string{req.Model}
						for _, m := range provCfg.Models {
							if m != req.Model {
								newModels = append(newModels, m)
							}
						}
						provCfg.Models = newModels
						cfg.Providers[provName] = provCfg
						// Also update the top-level model field for consistency
						cfg.Model = req.Model
						_ = s.persistConfig(true)
					}
					respProvider = cfg.Provider
				}
				s.unlockCfgForWrite()
				jsonResponse(w, map[string]interface{}{
					"ok":       true,
					"scope":    req.Scope,
					"provider": respProvider,
					"model":    req.Model,
					"message":  "model switched dynamically",
				})
				return
			}
		}
	}

	// Full provider switch (recreate provider)
	if req.Provider != "" && req.Model != "" {
		// 整段原地改写 s.cfg 必须在 cfgMu 内（写入侧 setCfg 由 cfgMu 保护）。
		// 注意 cfgMu 是叶子锁：refreshConvertConfig / clearAgents 会去拿
		// 别的锁，必须在放掉 cfgMu 之后再做。
		s.lockCfgForWrite()
		// lockCfgForWrite 保证 s.cfg 非 nil（且已是本写点私有的副本）。
		cfg := s.cfg
		cfg.Provider = req.Provider
		cfg.Model = req.Model
		// Update provider models array: move selected model to front
		if cfg.Providers != nil {
			if provCfg, ok := cfg.Providers[req.Provider]; ok {
				newModels := []string{req.Model}
				for _, m := range provCfg.Models {
					if m != req.Model {
						newModels = append(newModels, m)
					}
				}
				provCfg.Models = newModels
				cfg.Providers[req.Provider] = provCfg
			}
		}
		// Save config
		_ = s.persistConfig(true)
		// Recreate provider
		s.provider = createProvider(cfg)
		s.unlockCfgForWrite()
		// Install the conversion/vision policy on the fresh instance: without
		// it the new provider runs with a nil ConvertCfg and every image is
		// downgraded to a placeholder.
		s.refreshConvertConfig()
		// Clear all agents to force re-creation. 必须走 agentsMu：map 写入
		// 与 getOrCreateAgent / approval 广播的读并发，裸写会触发 fatal
		// "concurrent map writes"。
		s.clearAgents()
	}

	jsonResponse(w, map[string]interface{}{
		"ok":       true,
		"scope":    req.Scope,
		"provider": req.Provider,
		"model":    req.Model,
	})
}

// isMaskedAPIKey 报告前端回传的是不是脱敏占位值（GET /api/providers / GET
// /api/providers/{name} 返回的是 maskAPIKey 形式，如 "sk-1234****cdef"）。
//
// 模型设置页的编辑弹窗用列表里的值预填 key 输入框（ModelsProvidersView
// openEditProviderModal），用户不动 key 直接点保存时，脱敏串就会被当成真 key
// 落盘 —— 之后所有请求都 401「无效的 API Key」，而且从配置里看不出来（长度、
// 前缀都像真的）。这种"回写脱敏值"的保存视为「保持原 key 不变」。
func isMaskedAPIKey(key string) bool {
	return strings.Contains(key, "****")
}

// applyLiveProviderCredentials 把刚保存的 provider 配置装到**正在运行**的
// provider 实例上。缓存 agent 与 server 共享同一实例，只写 config.json 不会让
// 新 key 生效：聊天继续用旧 key 请求 → 一直 401（而设置页的"测试连接"是通的，
// 因为它用新配置新建临时 provider，掩盖了这个问题）。
//
// 只有被编辑的 provider 正是当前使用中的那个才需要处理；就地更新不支持时
// （凭据存在私有字段里的实现，见 provider.ApplyCredentials）回退为重建 provider
// 并清空缓存 agent —— 与切换供应商（handleModelSet）走同一条路径。
func (s *Server) applyLiveProviderCredentials(name string, provCfg appconfig.ProviderConfig) {
	// 直接读 s.cfg，**不能**走 cfgSnapshot：本函数的调用方
	// （handleProvidersSubRoutes）已持 cfgMu，再拿一次必自我死锁。
	// 调用方持 cfgMu 同时也保证了这里读到的是本写点的私有副本。
	cfg := s.cfg
	if cfg == nil || cfg.Provider != name {
		return
	}
	// 快路径：provider 支持就地更新凭据时直接改，不碰任何锁。
	if s.provider != nil && provider.ApplyCredentials(s.provider, provCfg.APIKey, provCfg.BaseURL) {
		return
	}
	// 慢路径要重建 provider + 清 agent 缓存，会去拿别的锁 —— 此刻
	// cfgMu 还被调用方持着，这里不能动。打标记，由调用方在放锁后调
	// rebuildLiveProvider 完成。
	s.rebuildLiveProvider = true
}

// rebuildLiveProviderNow 在**不持 cfgMu** 的前提下重建 provider 并清空
// agent 缓存。由调用方在释放 cfgMu 之后**仅当** `s.rebuildLiveProvider`
// 为真时调用 —— 标记的读取与清零都在调用方的 cfgMu 临界区内完成，
// 本函数自己完全不碰那个字段（否则会与其它请求在 cfgMu 下的写入竞态）。
func (s *Server) rebuildLiveProviderNow() {
	cfg := s.cfgSnapshot()
	if cfg == nil {
		return
	}
	s.provider = createProvider(cfg)
	s.refreshConvertConfig()
	s.clearAgents()
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	s.syncConfigFromDisk()
	// 走 cfgSnapshot 而非"用 s.mu 包住裸读"：s.cfg 的写入侧（setCfg）由
	// cfgMu 保护，两把锁互不相干，拿 s.mu 并不能与 setCfg 互斥
	// （CI `-race` 实测报 DATA RACE）。
	cfg := s.cfgSnapshot()
	providers := make([]map[string]interface{}, 0)
	if cfg != nil && cfg.Providers != nil {
		for name, provCfg := range cfg.Providers {
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
	s.syncConfigFromDisk()
	// 取一次不可变快照即可：写入侧 setCfg 由 cfgMu 保护，与 s.mu 无关，
	// 所以这里不能用 s.mu 来"保护"裸读（那样两把锁并不互斥）。
	cfg := s.cfgSnapshot()
	providerName := ""
	if cfg != nil {
		providerName = cfg.Provider
	}
	modelName := ""
	if cfg != nil {
		modelName = cfg.GetCurrentModel()
	}

	// Try to get current model from Modeler interface
	if s.provider != nil {
		if modeler, ok := provider.GetModeler(s.provider); ok {
			modelName = modeler.GetModel()
		}
	}

	if modelName == "" {
		modelName = "default"
	}

	// Infer context length and capabilities from model name.
	// Vision support now comes from the SINGLE source of truth
	// (provider.ModelSupportsVision: runtime learning → per-model registry
	// → negative/positive name heuristics) so the capabilities reported here
	// match what the request path actually does with image parts. The old
	// duplicated per-family switch drifted from the conversion logic.
	contextLen := provider.DefaultModelContextLen
	maxOutput := provider.DefaultModelMaxOutput
	supportsVision := provider.ModelSupportsVision(modelName)
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

	// 窗口：单一数据源是 pkg/catalog（provider.ModelContextLen 查的就是它），
	// 与 agent 的压缩阈值共用同一份数据。命中才覆盖上一步 Modeler 给出的值 ——
	// provider 的模型列表可能来自用户配置或在线拉取（带不上窗口），而目录里
	// 有登记时以目录为准。
	//
	// max_output_tokens / model_family / supports_reasoning 原先来自一份按名称
	// 推断的规则表，该表已删除（与 catalog 重复维护、必然漂移）。catalog 不登记
	// 这两项，故：max_output_tokens 退回 DefaultModelMaxOutput（仅展示，请求路径
	// 不受约束）；model_family 用配置的 provider 名；supports_reasoning 恒为 false
	// （无数据源；前端亦未消费 /model/info，字段保留仅为兼容）。
	if n := provider.ModelContextLen(modelName); n > 0 {
		contextLen = n
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

	// Explicit per-provider "vision" declaration (config Providers[].vision,
	// edited via the UI dropdown) has the same precedence as the request
	// path in server.go: it beats both name detection and provider-level
	// capabilities.
	// 复用本函数开头取的 cfg 快照（不可变），无需再加锁。
	if cfg != nil && cfg.Providers != nil {
		if provCfg, ok := cfg.Providers[cfg.Provider]; ok && provCfg.Vision != nil {
			supportsVision = *provCfg.Vision
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
	s.syncConfigFromDisk()
	// 取不可变快照：s.cfg 的写入侧由 cfgMu 保护，与 s.mu 无关
	// （拿 s.mu 包裸读并不能与 setCfg 互斥，CI `-race` 会报）。
	cfg := s.cfgSnapshot()
	models := make([]map[string]interface{}, 0)
	seen := make(map[string]bool)

	if cfg != nil && cfg.Providers != nil {
		for name, provCfg := range cfg.Providers {
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
	if cfg != nil && cfg.Provider != "" {
		modelSet := make(map[string]bool)

		// Add from Models array
		if cfg.Providers != nil {
			if provCfg, ok := cfg.Providers[cfg.Provider]; ok {
				for _, m := range provCfg.Models {
					modelSet[m] = true
				}
			}
		}

		for modelName := range modelSet {
			id := fmt.Sprintf("%s/%s", cfg.Provider, modelName)
			if !seen[id] {
				models = append(models, map[string]interface{}{
					"id":         id,
					"name":       modelName,
					"provider":   cfg.Provider,
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
	cfg := s.cfgSnapshot()
	if cfg != nil && cfg.Providers != nil {
		for name, provCfg := range cfg.Providers {
			if name == cfg.Provider {
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
	// 整个 handler 都是 provider 配置的读写（GET/PUT/POST/DELETE 子路由），
	// 全程在 s.mu + cfgMu 内完成：s.cfg 与其 Providers map 会被 handleModelSet /
	// syncConfigFromDisk 并发整体替换，此前无锁直读直写是真实数据竞态。
	//
	// **两把锁都要**：s.mu 负责配置内容各字段的逻辑串行化，cfgMu 负责
	// s.cfg 指针/对象与其它写入方（setCfg）互斥 —— 只拿 s.mu 并不能与
	// setCfg 互斥（CI `-race` 实测）。cfgMu 是叶子锁，可安全地持 s.mu 再拿它。
	//
	// 注意：这里**不**先 reloadConfig —— 那会把内存快照整体换成磁盘内容，
	// 而 PUT 语义本就是"在内存快照上改一处再落盘"。无条件重载反而会丢掉
	// 本次请求前的内存态（测试也依赖这一点：直接构造 Server 并预置 cfg 后
	// 调 PUT，期望改的是那份 cfg）。网关/CLI 的外部写入由 persistConfig
	// 的 preserveGateway 与 reloadConfig 的其它调用点兜底。
	// 用 cfgWriteGuard 同时持有 s.mu + cfgMu，且可幂等地提前释放
	// （本 handler 里有两次网络请求，绝不能持锁）。
	g := s.lockCfgWriteGuard()
	defer g.Release()
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
		// 直接读 s.cfg：本 handler 入口已持 s.mu + cfgMu（g 未释放），
		// 走 cfgSnapshot 会再拿一次 cfgMu ⇒ 不可重入的自我死锁（前端
		// "保存供应商"第一步就 GET 探存在性，一点保存整个后端当场挂死）。
		// 这里只读不改，读的是 lockCfgForWrite 换出的写点私有副本。
		if cfg := s.cfg; cfg != nil && cfg.Providers != nil {
			if provCfg, ok := cfg.Providers[name]; ok {
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
			BaseURL string          `json:"base_url"`
			Model   string          `json:"model"`
			APIKey  string          `json:"api_key"`
			Models  []string        `json:"models"`
			Vision  json.RawMessage `json:"vision,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		// Update provider config
		// 直接读 s.cfg（**不能**用 cfgSnapshot：此处已持 cfgMu，再拿一次
		// 就是不可重入的自我死锁）。lockCfgForWrite 已把它换成私有副本。
		if cfg := s.cfg; cfg != nil {
			if cfg.Providers == nil {
				cfg.Providers = make(map[string]appconfig.ProviderConfig)
			}
			provCfg := cfg.Providers[name]
			if req.BaseURL != "" {
				provCfg.BaseURL = req.BaseURL
			}
			if req.APIKey != "" && !isMaskedAPIKey(req.APIKey) {
				provCfg.APIKey = req.APIKey
			}
			// Models array: first element is current model
			if req.Models != nil {
				provCfg.Models = req.Models
				// If this is the current provider, also update top-level model
				if cfg.Provider == name && len(req.Models) > 0 {
					cfg.Model = req.Models[0]
				}
			}
			// Vision: key present with true/false sets the declaration, with
			// null clears it (back to name-based auto-detection), key absent
			// (nil slice) leaves the stored value untouched. A value-type
			// RawMessage is required here: decoding null into a *RawMessage
			// nils the pointer, making it indistinguishable from an absent key.
			if len(req.Vision) > 0 {
				var vb *bool
				if err := json.Unmarshal(req.Vision, &vb); err == nil {
					provCfg.Vision = vb
				}
			}
			cfg.Providers[name] = provCfg
			_ = s.persistConfig(true)
			// 凭据/地址改动必须落到正在运行的 provider 实例上（缓存 agent 共享
			// 同一实例，只写 config 的话聊天仍用旧 key 请求，一直 401）。
			// 只做快路径（就地更新凭据）；需要重建时它会打标记。
			s.applyLiveProviderCredentials(name, provCfg)
		}
		// 重建 provider / 刷新视觉策略都会去拿别的锁（clearAgents 拿
		// agentsMu、refreshConvertConfig 要读配置快照），必须在释放
		// cfgMu 之后做 —— Release 幂等，defer 里再调一次安全。
		// 在持锁期间读出并清零标记：出锁后本函数不再碰它（避免竞态）。
		rebuild := s.rebuildLiveProvider
		s.rebuildLiveProvider = false
		g.Release()
		if rebuild {
			s.rebuildLiveProviderNow()
		}
		// The vision declaration is only useful if the running provider
		// sees it: cached agents share this provider instance, so refresh
		// its convert config instead of waiting for a restart.
		s.refreshConvertConfig()
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name})
		return
	}

	// Handle POST /{name} - create provider (alias for PUT to create new)
	if r.Method == http.MethodPost && subRoute == "" {
		var req struct {
			Name    string          `json:"name"`
			BaseURL string          `json:"base_url"`
			APIKey  string          `json:"api_key"`
			Models  []string        `json:"models"`
			Vision  json.RawMessage `json:"vision,omitempty"`
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
		// 直接读 s.cfg（**不能**用 cfgSnapshot：此处已持 cfgMu，再拿一次
		// 就是不可重入的自我死锁）。lockCfgForWrite 已把它换成私有副本，
		// 下面的原地改写只发生在这份副本上。
		if cfg := s.cfg; cfg != nil {
			if cfg.Providers == nil {
				cfg.Providers = make(map[string]appconfig.ProviderConfig)
			}
			provCfg := cfg.Providers[providerName]
			if req.BaseURL != "" {
				provCfg.BaseURL = req.BaseURL
			}
			if req.APIKey != "" && !isMaskedAPIKey(req.APIKey) {
				provCfg.APIKey = req.APIKey
			}
			// Models array: first element is current model
			if req.Models != nil {
				provCfg.Models = req.Models
				// If this is the current provider, also update top-level model
				if cfg.Provider == providerName && len(req.Models) > 0 {
					cfg.Model = req.Models[0]
				}
			}
			// Vision declaration: true/false sets, null clears (auto),
			// key absent leaves it untouched (see the PUT branch note).
			if len(req.Vision) > 0 {
				var vb *bool
				if err := json.Unmarshal(req.Vision, &vb); err == nil {
					provCfg.Vision = vb
				}
			}
			cfg.Providers[providerName] = provCfg
			_ = s.persistConfig(true)
			// See the PUT branch: credentials must reach the live instance.
			s.applyLiveProviderCredentials(providerName, provCfg)
		}
		// See the PUT branch: 释放 cfgMu 之后再重建 / 刷新视觉策略。
		// 持锁期间读出并清零标记，出锁后不再碰它。
		rebuild := s.rebuildLiveProvider
		s.rebuildLiveProvider = false
		g.Release()
		if rebuild {
			s.rebuildLiveProviderNow()
		}
		// See the PUT branch: keep the live provider's vision policy in
		// sync with the just-saved declaration.
		s.refreshConvertConfig()
		jsonResponse(w, map[string]interface{}{"ok": true, "name": providerName, "created": true})
		return
	}

	// Handle DELETE /{name} - delete provider
	if r.Method == http.MethodDelete && subRoute == "" {
		// 直接读 s.cfg：此处仍持 cfgMu，走 cfgSnapshot 会自锁；且本分支
		// 要**原地改** Providers（删条目），必须改在写点私有副本上
		// （lockCfgForWrite 已做过 COW），改快照会污染别人手上的旧快照。
		if cfg := s.cfg; cfg != nil && cfg.Providers != nil {
			if _, exists := cfg.Providers[name]; exists {
				delete(cfg.Providers, name)
				// If deleted provider was current, clear top-level fields
				if cfg.Provider == name {
					cfg.Provider = ""
					cfg.Model = ""
				}
				_ = s.persistConfig(true)
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

	// Handle POST /{name}/test - verify provider connectivity with a real
	// lightweight chat round-trip. Body may carry unsaved form values
	// (api_key/base_url/model); they win over the stored config so the UI
	// can test a key before saving it.
	if r.Method == http.MethodPost && subRoute == "test" {
		var req struct {
			APIKey  string `json:"api_key"`
			BaseURL string `json:"base_url"`
			Model   string `json:"model"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req) // body is optional
		}

		// 直接读 s.cfg：此处仍持 cfgMu（g 未释放），走 cfgSnapshot 会自锁。
		// 只读它的字段值、且下面马上 Release，不改动它。
		cfg := s.cfg
		if cfg == nil {
			http.Error(w, "config unavailable", http.StatusInternalServerError)
			return
		}
		provCfg := appconfig.ProviderConfig{}
		if cfg.Providers != nil {
			if saved, ok := cfg.Providers[name]; ok {
				provCfg = saved
			}
		}
		if req.APIKey != "" {
			provCfg.APIKey = req.APIKey
		}
		if req.BaseURL != "" {
			provCfg.BaseURL = req.BaseURL
		}
		if req.Model != "" {
			provCfg.Models = []string{req.Model}
		}
		model := provCfg.GetCurrentModel()
		if model == "" {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": "no model configured"})
			return
		}
		// 配置已快照成 provCfg：测试连接要打真实网络请求（最多 20s），
		// 绝不能持 s.mu / cfgMu —— 否则一个卡住的端点会让整个后端的
		// 配置读全部排队。Release 幂等，defer 里再调一次也安全。
		g.Release()

		prov, err := appconfig.CreateProviderFor(name, provCfg)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		start := time.Now()
		resp, err := prov.Chat(ctx, []types.Message{{Role: "user", Content: "ping"}})
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			jsonResponse(w, map[string]interface{}{
				"ok":        false,
				"error":     truncateRunes(err.Error(), 300),
				"latencyMs": latencyMs,
			})
			return
		}
		replyChars := 0
		if resp != nil {
			replyChars = len([]rune(resp.Content))
		}
		jsonResponse(w, map[string]interface{}{
			"ok":         true,
			"model":      model,
			"latencyMs":  latencyMs,
			"replyChars": replyChars,
		})
		return
	}

	// Handle POST /{name}/fetch-models - pull the provider's live model list
	// from its /models API endpoint. Body may carry unsaved form values
	// (api_key/base_url); they win over the stored config, same as /test, so
	// the UI can fetch with a key typed into the edit modal before saving.
	if r.Method == http.MethodPost && subRoute == "fetch-models" {
		var req struct {
			APIKey  string `json:"api_key"`
			BaseURL string `json:"base_url"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req) // body is optional
		}

		// 直接读 s.cfg：此处仍持 cfgMu（g 未释放），走 cfgSnapshot 会自锁。
		cfg := s.cfg
		if cfg == nil {
			http.Error(w, "config unavailable", http.StatusInternalServerError)
			return
		}
		provCfg := appconfig.ProviderConfig{}
		if cfg.Providers != nil {
			if saved, ok := cfg.Providers[name]; ok {
				provCfg = saved
			}
		}
		if req.APIKey != "" {
			provCfg.APIKey = req.APIKey
		}
		if req.BaseURL != "" {
			provCfg.BaseURL = req.BaseURL
		}
		// baseURL 兜底：内置目录里的官方端点（与构造函数 fallback 同源）
		if provCfg.BaseURL == "" {
			for _, bp := range appconfig.ListProviders() {
				if bp.Name == name {
					provCfg.BaseURL = bp.BaseURL
					break
				}
			}
		}
		if provCfg.BaseURL == "" {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": "no base URL configured for this provider"})
			return
		}
		// 凭据已快照到 provCfg：立刻放锁，网络请求（FetchModels）绝不能
		// 持 s.mu / cfgMu —— 那是全 server 的配置读锁，一个慢请求会拖死
		// 整个后端。Release 幂等，defer 里再调一次也安全。
		g.Release()

		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		models, err := provider.FetchModels(ctx, name, provCfg.APIKey, provCfg.BaseURL)
		if err != nil {
			jsonResponse(w, map[string]interface{}{
				"ok":    false,
				"error": truncateRunes(err.Error(), 300),
			})
			return
		}
		jsonResponse(w, map[string]interface{}{
			"ok":     true,
			"models": models,
			"count":  len(models),
		})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handleModelOptions(w http.ResponseWriter, r *http.Request) {
	s.syncConfigFromDisk()
	// 模型供应商页的主接口。
	//
	// **必须走 cfgSnapshot 而不是裸读 s.cfg**：s.cfg 的写入侧（setCfg）由
	// cfgMu 保护，而 cfgMu 与 s.mu 是两把不同的锁 —— 用 s.mu 包住裸读
	// **不能**和 setCfg 互斥（CI `-race` 实测报 DATA RACE）。这里先取一次
	// 不可变快照，后续全程只读快照字段。
	cfg := s.cfgSnapshot()
	providerList := make([]map[string]interface{}, 0)
	providerNames := make(map[string]bool) // Track which providers are already added

	// First: Add all configured providers from config
	if cfg != nil && cfg.Providers != nil {
		for name, provCfg := range cfg.Providers {
			isCurrent := name == cfg.Provider
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
			isCurrent := cfg != nil && bp.Name == cfg.Provider
			providerList = append(providerList, map[string]interface{}{
				"name":         bp.Name,
				"slug":         bp.Name,
				"display_name": bp.DisplayName,
				"models":       bp.Models,
				"total_models": len(bp.Models),
				"base_url":     bp.BaseURL,
				"is_current":   isCurrent,
			})
		}
	}

	model := ""
	currentProviderName := ""
	if cfg != nil {
		model = cfg.GetCurrentModel()
		currentProviderName = cfg.Provider
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
