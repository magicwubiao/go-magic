// Package configtypes provides unified configuration type definitions for go-magic.
// All configuration structures should be defined here to avoid duplication across packages.
package configtypes

// Config is the main configuration structure for go-magic application.
type Config struct {
	// Basic settings
	Profile    string `json:"profile"`
	MagicHome  string `json:"magic_home"`
	WorkingDir string `json:"working_dir,omitempty"`

	// Provider settings
	Provider  string                 `json:"provider"`
	Model     string                 `json:"model"`
	Providers map[string]ProviderCfg `json:"providers"`

	// Feature toggles
	Tools        ToolsConfig     `json:"tools,omitempty"`
	Gateway      GatewayConfig   `json:"gateway,omitempty"`
	Agent        AgentConfig     `json:"agent,omitempty"`
	Memory       MemoryConfig    `json:"memory,omitempty"`
	Kanban       KanbanConfig    `json:"kanban,omitempty"`
	Notification NotificationCfg `json:"notification,omitempty"`
	ImageGen     ImageGenConfig  `json:"image_gen,omitempty"`
	Security     SecurityConfig  `json:"security,omitempty"`

	// Advanced features
	Cortex    CortexConfig    `json:"cortex,omitempty"`
	Plugin    PluginConfig    `json:"plugin,omitempty"`
	Execution ExecutionConfig `json:"execution,omitempty"`
	Log       LogConfig       `json:"log,omitempty"`
	Server    ServerConfig    `json:"server,omitempty"`
	MCP       MCPConfig       `json:"mcp,omitempty"`
	SubAgent  SubAgentConfig  `json:"subagent,omitempty"`
	Voice     VoiceConfig     `json:"voice,omitempty"`
	Privacy   PrivacyConfig   `json:"privacy,omitempty"`
	Display   DisplayConfig   `json:"display,omitempty"`
}

// ProviderCfg defines provider-specific configuration.
type ProviderCfg struct {
	Provider  string            `json:"provider"`
	APIKey    string            `json:"api_key"`
	BaseURL   string            `json:"base_url,omitempty"`
	Proxy     string            `json:"proxy,omitempty"`
	Fallback  []string          `json:"fallback,omitempty"`
	ExtraBody map[string]string `json:"extra_body,omitempty"`

	// Extended fields for advanced configuration
	APIVersion string            `json:"api_version,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
	Models     []string          `json:"models,omitempty"` // List of supported models, first element is current model
	MaxRPM     int               `json:"max_rpm,omitempty"`
	MaxTPM     int               `json:"max_tpm,omitempty"`
}

// GetCurrentModel returns the current model (first element of Models array)
func (p *ProviderCfg) GetCurrentModel() string {
	if len(p.Models) > 0 {
		return p.Models[0]
	}
	return ""
}

// MemoryConfig defines memory system configuration.
// This unified structure combines all memory-related settings.
type MemoryConfig struct {
	// Basic settings
	Enabled     bool   `json:"enabled"`
	DBPath      string `json:"db_path,omitempty"`
	AutoRecall  bool   `json:"auto_recall,omitempty"`
	RecallLimit int    `json:"recall_limit,omitempty"`

	// Storage settings
	StorageDir     string `json:"storage_dir,omitempty"`
	MaxSizeMB      int    `json:"max_size_mb,omitempty"`
	MaxContentLen  int    `json:"max_content_length,omitempty"`
	MaxAgentMemLen int    `json:"max_agent_mem_length,omitempty"`
	MaxUserMemLen  int    `json:"max_user_mem_length,omitempty"`

	// FTS settings
	FTSEnabled     bool `json:"fts_enabled,omitempty"`
	FTSMaxResults  int  `json:"fts_max_results,omitempty"`
	FTSBoostRecent bool `json:"fts_boost_recent,omitempty"`

	// Decay settings
	EnableDecay bool    `json:"enable_decay,omitempty"`
	DecayRate   float64 `json:"decay_rate,omitempty"`
	EnableDedup bool    `json:"enable_dedup,omitempty"`

	// Cleanup settings
	CleanupEnabled   bool   `json:"cleanup_enabled,omitempty"`
	CleanupInterval  string `json:"cleanup_interval,omitempty"`
	CleanupOlderThan string `json:"cleanup_older_than,omitempty"`

	// Summarization
	AutoSummarize      bool   `json:"auto_summarize,omitempty"`
	SummarizeThreshold int    `json:"summarize_threshold,omitempty"`
	LLMProvider        string `json:"llm_provider,omitempty"`
}

// GatewayConfig defines gateway/platform integration configuration.
type GatewayConfig struct {
	Enabled   bool                      `json:"enabled"`
	Platforms map[string]PlatformConfig `json:"platforms,omitempty"`

	// Runtime settings
	MaxSessions     int    `json:"max_sessions,omitempty"`
	SessionTimeout  string `json:"session_timeout,omitempty"`
	EnableSlashCmd  bool   `json:"enable_slash_cmd,omitempty"`
	PlatformTimeout string `json:"platform_timeout,omitempty"`
	APIPort         int    `json:"api_port,omitempty"`
	EnableAPI       bool   `json:"enable_api,omitempty"`
}

// PlatformConfig defines platform-specific configuration.
type PlatformConfig struct {
	// Authentication
	Token     string `json:"token,omitempty"`
	Secret    string `json:"secret,omitempty"`
	AppKey    string `json:"app_key,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	CorpID    string `json:"corp_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`

	// Settings
	Enabled         bool     `json:"enabled"`
	ProxyURL        string   `json:"proxy_url,omitempty"`
	AllowedChannels []string `json:"allowed_channels,omitempty"`
	BlockedChannels []string `json:"blocked_channels,omitempty"`

	// Advanced
	APIURL        string `json:"api_url,omitempty"`
	Region        string `json:"region,omitempty"`
	MessageFormat string `json:"message_format,omitempty"`
}

// AgentConfig defines agent behavior configuration.
type AgentConfig struct {
	MaxIterations     int     `json:"max_iterations,omitempty"`
	MaxTurns          int     `json:"max_turns,omitempty"`
	CompressionRatio  float64 `json:"compression_ratio,omitempty"`
	GoalMaxTurns      int     `json:"goal_max_turns,omitempty"`
	AutoApprove       bool    `json:"auto_approve,omitempty"`
	EnableSubtasks    bool    `json:"enable_subtasks,omitempty"`
	EnableHooks       bool    `json:"enable_hooks,omitempty"`
	TruncateThreshold int     `json:"truncate_threshold,omitempty"`
}

// ToolsConfig defines tools configuration.
type ToolsConfig struct {
	Enabled  []string `json:"enabled,omitempty"`
	Disabled []string `json:"disabled,omitempty"`
}

// KanbanConfig defines kanban system configuration.
type KanbanConfig struct {
	Enabled bool   `json:"enabled"`
	DBPath  string `json:"db_path,omitempty"`
}

// NotificationCfg defines notification configuration.
type NotificationCfg struct {
	Email EmailConfig `json:"email,omitempty"`
	SMS   SMSConfig   `json:"sms,omitempty"`
}

// EmailConfig defines email notification settings.
type EmailConfig struct {
	Enabled  bool   `json:"enabled"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPUser string `json:"smtp_user"`
	SMTPPass string `json:"smtp_pass"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// SMSConfig defines SMS notification settings.
type SMSConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Phone    string `json:"phone"`
}

// ImageGenConfig defines image generation configuration.
type ImageGenConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// SecurityConfig defines security configuration.
type SecurityConfig struct {
	PII  PIIConfig    `json:"pii,omitempty"`
	File FileSecurity `json:"file,omitempty"`
}

// FileSecurity defines file operation security configuration.
type FileSecurity struct {
	Enabled          bool     `json:"enabled,omitempty"`
	AllowedPaths     []string `json:"allowed_paths,omitempty"`
	BlockedPaths     []string `json:"blocked_paths,omitempty"`
	SessionIsolation bool     `json:"session_isolation,omitempty"`
	DefaultFileMode  string   `json:"default_file_mode,omitempty"`
	DefaultDirMode   string   `json:"default_dir_mode,omitempty"`
	MaxFileSizeKB    int      `json:"max_file_size_kb,omitempty"`
	AllowSymlinks    bool     `json:"allow_symlinks,omitempty"`
}

// PIIConfig defines PII redaction configuration.
type PIIConfig struct {
	Enabled      bool `json:"enabled"`
	RedactPhone  bool `json:"redact_phone"`
	RedactEmail  bool `json:"redact_email"`
	RedactName   bool `json:"redact_name"`
	RedactCredit bool `json:"redact_credit"`
	RedactSSN    bool `json:"redact_ssn"`
}

// CortexConfig defines cortex/cognition system configuration.
type CortexConfig struct {
	Enabled         bool   `json:"enabled"`
	PerceptionModel string `json:"perception_model,omitempty"`
	DecisionModel   string `json:"decision_model,omitempty"`
}

// PluginConfig defines plugin system configuration.
type PluginConfig struct {
	Enabled  bool     `json:"enabled"`
	Dir      string   `json:"dir,omitempty"`
	AutoLoad bool     `json:"auto_load,omitempty"`
	Allowed  []string `json:"allowed,omitempty"`
}

// ExecutionConfig defines execution system configuration.
type ExecutionConfig struct {
	MaxParallel      int    `json:"max_parallel,omitempty"`
	CheckpointDir    string `json:"checkpoint_dir,omitempty"`
	EnableCheckpoint bool   `json:"enable_checkpoint,omitempty"`
}

// LogConfig defines logging configuration.
type LogConfig struct {
	Level  string `json:"level,omitempty"`
	File   string `json:"file,omitempty"`
	Format string `json:"format,omitempty"`
}

// ServerConfig defines server configuration.
type ServerConfig struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port,omitempty"`
	Host    string `json:"host,omitempty"`
}

// MCPConfig defines MCP (Model Context Protocol) configuration.
type MCPConfig struct {
	Enabled bool                 `json:"enabled"`
	Servers map[string]MCPServer `json:"servers,omitempty"`
}

// MCPServer defines MCP server configuration.
type MCPServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// SubAgentConfig defines sub-agent configuration.
type SubAgentConfig struct {
	Enabled  bool   `json:"enabled"`
	MaxDepth int    `json:"max_depth,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
}

// VoiceConfig defines voice system configuration.
type VoiceConfig struct {
	Enabled bool      `json:"enabled"`
	ASR     ASRConfig `json:"asr,omitempty"`
	TTS     TTSConfig `json:"tts,omitempty"`
}

// ASRConfig defines ASR (speech recognition) configuration.
type ASRConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// TTSConfig defines TTS (text-to-speech) configuration.
type TTSConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	Voice    string `json:"voice"`
}

// PrivacyConfig defines privacy settings.
type PrivacyConfig struct {
	Enabled       bool `json:"enabled"`
	RedactPII     bool `json:"redact_pii"`
	AnonymizeUser bool `json:"anonymize_user"`
}

// DisplayConfig defines display/UI settings.
type DisplayConfig struct {
	Theme    string `json:"theme,omitempty"`
	Language string `json:"language,omitempty"`
}

// DefaultConfig returns the default configuration values.
func DefaultConfig() *Config {
	return &Config{
		Profile:   "default",
		MagicHome: "~/.magic",
		Provider:  "openai",
		Model:     "gpt-4",
		Agent: AgentConfig{
			MaxIterations:     50,
			MaxTurns:          80,
			CompressionRatio:  0.3,
			GoalMaxTurns:      10,
			AutoApprove:       false,
			EnableSubtasks:    true,
			EnableHooks:       true,
			TruncateThreshold: 4000,
		},
		Memory: MemoryConfig{
			Enabled:       true,
			DBPath:        "~/.magic/memory.db",
			AutoRecall:    true,
			RecallLimit:   5,
			FTSEnabled:    true,
			FTSMaxResults: 10,
		},
		Gateway: GatewayConfig{
			Enabled:        false,
			MaxSessions:    100,
			SessionTimeout: "30m",
			EnableSlashCmd: true,
		},
	}
}
