package tool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Common tool errors
var (
	ErrToolNotFound        = errors.New("tool not found")
	ErrInvalidParameters   = errors.New("invalid parameters")
	ErrToolExecutionFailed = errors.New("tool execution failed")
	ErrToolTimeout         = errors.New("tool execution timeout")
)

// Registry 管理工具注册和执行
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]Tool
	timeouts map[string]time.Duration
	executor ToolExecutor

	// 动态工具
	dynamicTools   map[string]*DynamicTool
	dynamicToolsMu sync.RWMutex

	// 日志
	logger Logger
}

// Logger 工具日志接口
type Logger interface {
	LogToolExecution(name string, params, result map[string]interface{}, duration time.Duration, err error)
}

// defaultLogger 默认日志实现
type defaultLogger struct{}

func (l *defaultLogger) LogToolExecution(name string, params, result map[string]interface{}, duration time.Duration, err error) {
	if err != nil {
		log.Printf("[TOOL] %s failed after %v: %v", name, duration, err)
	} else {
		log.Printf("[TOOL] %s completed in %v", name, duration)
	}
}

// SetTimeout 设置工具的默认超时时间
func (r *Registry) SetTimeout(name string, timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timeouts[name] = timeout
}

// GetTimeout 获取工具的默认超时时间
func (r *Registry) GetTimeout(name string) time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.timeouts[name]
}

// SetLogger 设置日志处理器
func (r *Registry) SetLogger(logger Logger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logger = logger
}

// NewRegistry 创建一个新的工具注册表
func NewRegistry() *Registry {
	return &Registry{
		tools:        make(map[string]Tool),
		timeouts:     make(map[string]time.Duration),
		executor:     NewDefaultToolExecutor(),
		dynamicTools: make(map[string]*DynamicTool),
		logger:       &defaultLogger{},
	}
}

// RegistryWithConfig 带配置的工具注册表
type RegistryWithConfig struct {
	*Registry
	emailConfig *EmailConfig
	smsConfig   *SMSConfig
}

// RegisterAll 注册所有内置工具
func (r *Registry) RegisterAll(workDir string) {
	// File tools
	r.Register(&ReadFileTool{})
	r.Register(&WriteFileTool{})
	r.Register(&FileEditTool{})
	r.Register(&ListFilesTool{})
	r.Register(NewDirectoryTreeTool())
	r.Register(&SearchInFilesTool{})

	// Web tools
	r.Register(&WebSearchTool{})
	// Note: WebExtractTool removed, use browser_navigate instead

	// Command execution with multiple backends (local, docker, ssh)
	r.Register(NewSecureExecuteCommandTool(workDir))

	// Code execution moved to plugins (python-runner, node-runner)
	// Use: magic plugin install python-runner / magic plugin install node-runner

	// Memory tools
	r.Register(&MemoryStoreTool{})
	r.Register(&MemoryRecallTool{})

	// Todo tool
	r.Register(GetTodoTool())

	// Session search tool
	r.Register(GetSessionSearchTool())

	// Clarify tool (with native button support for Telegram/Discord)
	r.Register(NewClarifyTool())

	// LSP diagnostic tool - semantic analysis after file writes
	r.Register(NewLSPDiagnosticTool(workDir))

	// Optional tools - these require external API configuration and are not registered by default
	// Call RegisterOptionalTools() after RegisterAll() to enable them
	// r.Register(NewImageGenerationTool())  // Optional
	// r.Register(NewTTSTool())               // Optional
	// r.Register(NewVideoAnalyzeTool())      // Optional

	// Cron job tool
	r.Register(NewCronJobTool())

	// Interrupt tool - allows agent to interrupt its own execution
	r.Register(NewInterruptTool())

	// Delegation tools (sub-agent tasks) - will be registered when subagent manager is available
	// Call RegisterDelegationTools(manager) to enable them

	// Built-in code execution with tool access
	r.Register(NewExecuteCodeTool())

	// Gitignore tool for generating .gitignore files
	r.Register(NewGitignoreTool())

	// Coding-enhanced tools (useful in coding mode)
	r.Register(NewBatchFileOpsTool())
	r.Register(NewProjectAnalyzeTool())
	r.Register(NewDiffPatchTool())

	// Home Assistant smart home integration
	r.Register(NewHATool())
	r.Register(NewHAGetStateTool())
	r.Register(NewHAListServicesTool())
	r.Register(NewHACallServiceTool())
	r.Register(NewHAEventsTool())
	r.Register(NewHAConfigTool())

	// Skill invocation tool (will be registered when manager is set)
	// r.Register(&SkillInvokeTool{})

	// Browser tools - chromedp-based real browser automation
	bt := NewBrowserTools()
	r.Register(NewBrowserNavigateTool(bt))
	r.Register(NewBrowserClickTool(bt))
	r.Register(NewBrowserTypeTool(bt))
	r.Register(NewBrowserScrollTool(bt))
	r.Register(NewBrowserBackTool())
	r.Register(NewBrowserConsoleTool())
	r.Register(NewBrowserSnapshotTool(bt))
	r.Register(NewBrowserGetImagesTool(bt))

	// Legacy web tools - lightweight goquery-based
	r.Register(NewWebFetchTool())
	r.Register(NewWebSelectTool())

	// Utility tools
	r.Register(NewJSONTool())
	r.Register(NewYAMLTool())
	r.Register(NewStringTool())
	r.Register(NewHashTool())
	r.Register(NewUUIDTool())
	r.Register(NewRandomTool())
	r.Register(NewTimeTool())
	r.Register(NewMathTool())
	r.Register(NewCSVTool())
	r.Register(NewEnvTool())
	r.Register(NewSystemInfoTool())

	// Set default timeouts
	r.SetTimeout("execute_command", 120*time.Second)
	r.SetTimeout("web_search", 30*time.Second)
	r.SetTimeout("web_extract", 60*time.Second)
	r.SetTimeout("web_fetch", 30*time.Second)
	r.SetTimeout("web_select", 30*time.Second)

	// Register plugin tools
	r.registerPluginTools()
}

// RegisterOptionalTools registers tools that require external API configuration.
// These are not registered by default to avoid confusing the LLM with non-functional placeholder tools.
func (r *Registry) RegisterOptionalTools() {
	r.Register(NewImageGenerationTool(nil))
	r.Register(NewImageEditTool(nil))
	r.Register(NewTTSTool())
	r.Register(NewASRTool())
	r.Register(NewASRAvailableProvidersTool())
	r.Register(NewTTSAvailableVoicesTool())
	r.Register(NewAudioPlayTool())
	r.Register(NewAudioDownloadTool())
	r.Register(NewVideoAnalyzeTool())

	// Video generation (pluggable backends: Replicate, Fal.ai, Stability AI)
	r.Register(NewVideoGenerateTool(nil))

	// X/Twitter search
	r.Register(NewXSearchTool("", ""))

	// Computer use (GUI control via mouse/keyboard)
	r.Register(NewComputerUseTool(""))

	// Send message - cross-platform messaging (requires gateway)
	// Call SetGateway on the tool after gateway is initialized
	r.Register(NewSendMessageTool(nil))
}

// RegisterImageGenTool 注册图片生成工具（带配置）
func (r *Registry) RegisterImageGenTool(config *ImageGenConfig) {
	if config != nil && config.APIKey != "" {
		r.Register(NewImageGenerationTool(config))
		r.SetTimeout("image_gen", 180*time.Second)
		r.Register(NewImageEditTool(config))
		r.SetTimeout("image_edit", 180*time.Second)
	}
}

// RegisterOptionalToolsWithConfig 使用配置注册可选工具
func (r *Registry) RegisterOptionalToolsWithConfig(imageGenConfig *ImageGenConfig) {
	r.RegisterImageGenTool(imageGenConfig)
	r.Register(NewTTSTool())
	r.Register(NewVideoAnalyzeTool())
}

// RegisterEmailTool 注册邮件发送工具（需要配置）
func (r *Registry) RegisterEmailTool(config *EmailConfig) {
	if config != nil && config.SMTPHost != "" {
		r.Register(NewEmailTool(config))
		r.SetTimeout("send_email", 30*time.Second)
	}
}

// RegisterSMSTool 注册短信发送工具（需要配置）
func (r *Registry) RegisterSMSTool(config *SMSConfig) {
	if config != nil && config.Provider != "" {
		r.Register(NewSMSTool(config))
		r.SetTimeout("send_sms", 30*time.Second)
	}
}

// RegisterWithNotificationConfig 注册所有工具，包括邮件和短信工具
func (r *Registry) RegisterWithNotificationConfig(workDir string, emailConfig *EmailConfig, smsConfig *SMSConfig) {
	r.RegisterAll(workDir)
	r.RegisterEmailTool(emailConfig)
	r.RegisterSMSTool(smsConfig)
}

// GetAllTools 返回所有内置工具实例
func GetAllTools() []Tool {
	return []Tool{
		&ReadFileTool{},
		&WriteFileTool{},
		&WebSearchTool{},
		// Note: WebExtractTool removed, use browser_navigate instead
		NewSecureExecuteCommandTool(""),
		&ListFilesTool{},
		&SearchInFilesTool{},
		&MemoryStoreTool{},
		&MemoryRecallTool{},
		// Browser tools - lightweight goquery-based implementation
		NewWebFetchTool(),
		NewWebSelectTool(),
	}
}

// Register 注册一个工具
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Unregister 注销一个工具
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	delete(r.timeouts, name)
}

// Get 获取工具实例
func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %s not found", name)
	}
	return t, nil
}

// List 返回所有已注册的工具名称
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// ListWithSchemas 返回所有工具及其 Schema
func (r *Registry) ListWithSchemas() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]map[string]interface{}, 0, len(r.tools))
	toolSchema := &ToolSchema{}
	for _, t := range r.tools {
		schemas = append(schemas, toolSchema.ToOpenAISchema(t))
	}
	return schemas
}

// Execute 执行工具
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	t, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	// 检查动态工具是否过期
	r.dynamicToolsMu.RLock()
	if dt, ok := r.dynamicTools[name]; ok {
		if dt.IsExpired() {
			r.dynamicToolsMu.RUnlock()
			r.dynamicToolsMu.Lock()
			delete(r.dynamicTools, name)
			r.dynamicToolsMu.Unlock()
			return nil, fmt.Errorf("dynamic tool %s has expired", name)
		}
	}
	r.dynamicToolsMu.RUnlock()

	// 获取超时时间
	timeout := r.GetTimeout(name)
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	// 使用执行器执行
	result := r.executor.ExecuteWithProtection(ctx, t, args, timeout)

	// 记录日志
	r.mu.RLock()
	logger := r.logger
	r.mu.RUnlock()
	if logger != nil {
		resultMap := make(map[string]interface{})
		if result.Result != nil {
			resultMap["result"] = result.Result
		}
		logger.LogToolExecution(name, args, resultMap, result.Duration, result.Error)
	}

	return result.Result, result.Error
}

// ExecuteWithTimeout 执行工具并指定超时时间
func (r *Registry) ExecuteWithTimeout(ctx context.Context, name string, args map[string]interface{}, timeout time.Duration) (interface{}, error) {
	t, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	result := r.executor.ExecuteWithProtection(ctx, t, args, timeout)
	return result.Result, result.Error
}

// ExecuteSafe 安全执行工具，捕获所有错误
func (r *Registry) ExecuteSafe(ctx context.Context, name string, args map[string]interface{}) ToolExecutionResult {
	t, err := r.Get(name)
	if err != nil {
		return ToolExecutionResult{
			ToolName: name,
			Params:   args,
			Error:    err,
		}
	}

	timeout := r.GetTimeout(name)
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	return r.executor.ExecuteWithProtection(ctx, t, args, timeout)
}

// HasTool 检查工具是否已注册
func (r *Registry) HasTool(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// Count 返回已注册工具数量
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// RegisterDynamic 注册一个动态工具（带 TTL）
func (r *Registry) RegisterDynamic(tool Tool, ttl time.Duration) {
	r.dynamicToolsMu.Lock()
	defer r.dynamicToolsMu.Unlock()
	r.dynamicTools[tool.Name()] = NewDynamicTool(tool, ttl)
}

// CleanupDynamic 清理过期的动态工具
func (r *Registry) CleanupDynamic() {
	r.dynamicToolsMu.Lock()
	defer r.dynamicToolsMu.Unlock()

	for name, dt := range r.dynamicTools {
		if dt.IsExpired() {
			delete(r.dynamicTools, name)
		}
	}
}

// GetToolInfo 获取工具信息
func (r *Registry) GetToolInfo(name string) (*ToolInfo, error) {
	t, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	return &ToolInfo{
		Name:        t.Name(),
		Description: t.Description(),
		Schema:      t.Schema(),
	}, nil
}

// FilterToolsByPrefix 按前缀过滤工具
func (r *Registry) FilterToolsByPrefix(prefix string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, 0)
	for _, t := range r.tools {
		if len(t.Name()) >= len(prefix) && t.Name()[:len(prefix)] == prefix {
			tools = append(tools, t)
		}
	}
	return tools
}

// FilterToolsByKeyword 按关键词过滤工具
func (r *Registry) FilterToolsByKeyword(keyword string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	keyword = toLower(keyword)
	tools := make([]Tool, 0)
	for _, t := range r.tools {
		name := toLower(t.Name())
		desc := toLower(t.Description())
		if contains(name, keyword) || contains(desc, keyword) {
			tools = append(tools, t)
		}
	}
	return tools
}

// 辅助函数 - 使用标准库实现
func toLower(s string) string {
	return strings.ToLower(s)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// RegisterSkillTool 注册技能工具（带 SkillInfoProvider）
func (r *Registry) RegisterSkillTool(provider SkillInfoProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools["skill"] = NewSkillInvokeTool(provider)
}

// RegisterWithSkillManager 注册所有工具，包括技能工具
func (r *Registry) RegisterWithSkillManager(skillManager SkillInfoProvider, workDir string) {
	r.RegisterAll(workDir)
	r.RegisterSkillTool(skillManager)
}

// registerPluginTools discovers and registers plugin commands as agent tools.
// This bridges the plugin system with the tool system, so when a plugin
// declares commands in its manifest, the agent can call them via tool calling.
func (r *Registry) registerPluginTools() {
	loader := newPluginToolLoader()
	if loader == nil {
		return
	}

	tools := loader.DiscoverTools()
	for _, t := range tools {
		r.Register(t)
		r.SetTimeout(t.Name(), 30*time.Second)
	}
}
