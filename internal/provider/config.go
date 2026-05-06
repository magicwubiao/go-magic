package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// ProviderConfig represents configuration for a provider
type ProviderConfig struct {
	// Provider identification
	Name    string `json:"name"`
	Display string `json:"display"`
	
	// API configuration
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
	
	// Optional configuration
	APIVersion string            `json:"api_version,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
	
	// Defaults
	DefaultModel string   `json:"default_model,omitempty"`
	Models      []string `json:"models,omitempty"`
	
	// Rate limiting
	MaxRPM int `json:"max_rpm,omitempty"` // requests per minute
	MaxTPM int `json:"max_tpm,omitempty"` // tokens per minute
	
	// Validation state
	Validated bool `json:"-"`
	
	// File path if loaded from file
	FilePath string `json:"-"`
}

// Validate checks if the provider config is valid
func (pc *ProviderConfig) Validate() error {
	if pc.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	
	// APIKey validation depends on provider type
	// Some providers (like Ollama) don't need API keys
	if !isNoKeyProvider(pc.Name) && pc.APIKey == "" {
		return fmt.Errorf("API key is required for provider %s", pc.Name)
	}
	
	if pc.BaseURL == "" {
		pc.BaseURL = getDefaultBaseURL(pc.Name)
	}
	
	if pc.Model == "" && pc.DefaultModel != "" {
		pc.Model = pc.DefaultModel
	}
	
	if pc.Model == "" {
		pc.Model = getDefaultModel(pc.Name)
	}
	
	pc.Validated = true
	return nil
}

// isNoKeyProvider returns true if the provider doesn't require an API key
func isNoKeyProvider(name string) bool {
	noKeyProviders := []string{"ollama", "vllm", "local"}
	for _, p := range noKeyProviders {
		if strings.Contains(strings.ToLower(name), p) {
			return true
		}
	}
	return false
}

// getDefaultBaseURL returns the default base URL for a provider
func getDefaultBaseURL(name string) string {
	defaults := map[string]string{
		"openai":      "https://api.openai.com/v1",
		"anthropic":   "https://api.anthropic.com",
		"deepseek":    "https://api.deepseek.com",
		"gemini":      "https://generativelanguage.googleapis.com/v1beta",
		"groq":        "https://api.groq.com/openai/v1",
		"kimi":        "https://api.moonshot.cn/v1",
		"moonshot":    "https://api.moonshot.cn/v1",
		"openrouter":  "https://openrouter.ai/api/v1",
		"ollama":      "http://localhost:11434",
		"vllm":        "http://localhost:8000",
		"cohere":      "https://api.cohere.ai/v2",
		"mistral":     "https://api.mistral.ai/v1",
		"perplexity":  "https://api.perplexity.ai",
		"together":    "https://api.together.xyz/v1",
		"dashscope":   "https://dashscope.aliyuncs.com/compatible-mode/v1",
		"zhipu":       "https://open.bigmodel.cn/api/paas/v4",
		"wenxin":      "https://aip.baidubce.com/rpc/2/0/ai_qianfan_200/v1",
		"doubao":      "https://ark.cn-beijing.volces.com/api/v3",
		"hunyuan":     "https://hunyuan.cloud.tencent.com/v1",
		"huoshan":     "https://ark.cn-beijing.volces.com/api/v3",
		"minimax":     "https://api.minimax.chat/v1",
		"mimo":        "https://api.mymimo.ai/v1",
	}
	
	if url, ok := defaults[strings.ToLower(name)]; ok {
		return url
	}
	
	return ""
}

// getDefaultModel returns the default model for a provider
func getDefaultModel(name string) string {
	defaults := map[string]string{
		"openai":      "gpt-4",
		"anthropic":   "claude-3-5-haiku-20241022",
		"deepseek":    "deepseek-chat",
		"gemini":      "gemini-1.5-flash",
		"groq":        "mixtral-8x7b-32768",
		"kimi":        "moonshot-v1-8k",
		"moonshot":    "moonshot-v1-8k",
		"openrouter":  "openai/gpt-4",
		"ollama":      "llama3",
		"vllm":        "llama3",
		"cohere":      "command-r-plus",
		"mistral":     "mistral-large-latest",
		"perplexity":  "llama-3.1-sonar-small-128k-online",
		"together":    "meta-llama/Llama-3-70b-chat-hf",
		"dashscope":   "qwen-plus",
		"zhipu":       "glm-4",
		"wenxin":      "ernie-4.0-8k",
		"doubao":      "doubao-pro-32k",
		"hunyuan":     "hunyuan-pro",
		"huoshan":     "doubao-pro-32k",
		"minimax":     "abab6.5s-chat",
		"mimo":        "mimo-3-7b",
	}
	
	if model, ok := defaults[strings.ToLower(name)]; ok {
		return model
	}
	
	return ""
}

// ConfigManager manages provider configurations with hot reload support
type ConfigManager struct {
	providers map[string]*ProviderConfig
	mu        sync.RWMutex
	filePath  string
	onChange  func(string) // callback when config changes
	
	// Hot reload
	watchEnabled bool
	watcher     *fileWatcher
}

// NewConfigManager creates a new configuration manager
func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		providers: make(map[string]*ProviderConfig),
	}
}

// LoadFromFile loads provider configurations from a JSON file
func (cm *ConfigManager) LoadFromFile(path string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	cm.filePath = path
	
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	
	return cm.parseAndLoad(data)
}

// parseAndLoad parses configuration data
func (cm *ConfigManager) parseAndLoad(data []byte) error {
	var config struct {
		Providers []ProviderConfig `json:"providers"`
	}
	
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	
	for i := range config.Providers {
		pc := &config.Providers[i]
		pc.FilePath = cm.filePath
		
		if err := pc.Validate(); err != nil {
			log.Warnf("Invalid provider config %s: %v", pc.Name, err)
			continue
		}
		
		cm.providers[pc.Name] = pc
	}
	
	return nil
}

// EnableHotReload enables watching the config file for changes
func (cm *ConfigManager) EnableHotReload(onChange func(string)) error {
	if cm.filePath == "" {
		return fmt.Errorf("no config file path set")
	}
	
	cm.onChange = onChange
	cm.watchEnabled = true
	
	// Start watching
	w, err := newFileWatcher(cm.filePath)
	if err != nil {
		return err
	}
	cm.watcher = w
	
	go cm.watchLoop()
	
	return nil
}

// watchLoop watches for file changes
func (cm *ConfigManager) watchLoop() {
	for {
		if !cm.watchEnabled {
			break
		}
		
		if cm.watcher.waitForChange() {
			cm.reload()
		}
	}
}

// reload reloads the configuration from file
func (cm *ConfigManager) reload() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	log.Info("Reloading provider configuration...")
	
	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		log.Errorf("Failed to read config file: %v", err)
		return
	}
	
	// Create new map
	newProviders := make(map[string]*ProviderConfig)
	
	var config struct {
		Providers []ProviderConfig `json:"providers"`
	}
	
	if err := json.Unmarshal(data, &config); err != nil {
		log.Errorf("Failed to parse config: %v", err)
		return
	}
	
	for i := range config.Providers {
		pc := &config.Providers[i]
		pc.FilePath = cm.filePath
		
		if err := pc.Validate(); err != nil {
			log.Warnf("Invalid provider config %s: %v", pc.Name, err)
			continue
		}
		
		newProviders[pc.Name] = pc
	}
	
	// Replace old config
	cm.providers = newProviders
	
	log.Infof("Reloaded %d provider configurations", len(cm.providers))
	
	// Notify callback
	if cm.onChange != nil {
		cm.onChange(cm.filePath)
	}
}

// Get returns a provider configuration
func (cm *ConfigManager) Get(name string) (*ProviderConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	pc, ok := cm.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	
	return pc, nil
}

// List returns all provider configurations
func (cm *ConfigManager) List() []*ProviderConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	result := make([]*ProviderConfig, 0, len(cm.providers))
	for _, pc := range cm.providers {
		result = append(result, pc)
	}
	
	return result
}

// Set sets a provider configuration
func (cm *ConfigManager) Set(pc *ProviderConfig) error {
	if err := pc.Validate(); err != nil {
		return err
	}
	
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	cm.providers[pc.Name] = pc
	return nil
}

// Delete removes a provider configuration
func (cm *ConfigManager) Delete(name string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	delete(cm.providers, name)
}

// Save saves the configuration to file
func (cm *ConfigManager) Save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	if cm.filePath == "" {
		return fmt.Errorf("no file path set")
	}
	
	providers := make([]ProviderConfig, 0, len(cm.providers))
	for _, pc := range cm.providers {
		providers = append(providers, *pc)
	}
	
	data, err := json.MarshalIndent(struct {
		Providers []ProviderConfig `json:"providers"`
	}{Providers: providers}, "", "  ")
	if err != nil {
		return err
	}
	
	// Ensure directory exists
	dir := filepath.Dir(cm.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	
	return os.WriteFile(cm.filePath, data, 0644)
}

// Close stops watching and releases resources
func (cm *ConfigManager) Close() {
	cm.watchEnabled = false
	if cm.watcher != nil {
		cm.watcher.close()
	}
}

// fileWatcher watches a file for changes
type fileWatcher struct {
	path    string
	modTime int64
	events  chan bool
	closed  bool
}

func newFileWatcher(path string) (*fileWatcher, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	
	w := &fileWatcher{
		path:    path,
		modTime: info.ModTime().UnixNano(),
		events:  make(chan bool, 1),
	}
	
	return w, nil
}

func (w *fileWatcher) waitForChange() bool {
	// Simple polling implementation
	// In production, would use fsnotify
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for !w.closed {
		select {
		case <-ticker.C:
			info, err := os.Stat(w.path)
			if err != nil {
				continue
			}
			
			newModTime := info.ModTime().UnixNano()
			if newModTime != w.modTime {
				w.modTime = newModTime
				return true
			}
		}
	}
	
	return false
}

func (w *fileWatcher) close() {
	w.closed = true
	close(w.events)
}
