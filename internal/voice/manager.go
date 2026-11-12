package voice

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VoiceProvider defines the interface for TTS/ASR providers
type VoiceProvider interface {
	// Name returns the provider name
	Name() string

	// IsAvailable checks if the provider is available
	IsAvailable() bool

	// TTS converts text to speech
	TTS(ctx context.Context, text string) ([]byte, error)

	// ASR converts speech to text
	ASR(ctx context.Context, audio []byte) (string, error)

	// StreamTTS streams text to speech
	StreamTTS(ctx context.Context, text string) (<-chan []byte, error)

	// StreamASR streams speech to text
	StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error)
}

// VoiceConfig holds voice configuration
type VoiceConfig struct {
	Provider     string                         `json:"provider"`     // openai, elevenlabs, azure, google
	Model        string                         `json:"model"`        // TTS/ASR model
	Voice        string                         `json:"voice"`        // Voice ID
	Language     string                         `json:"language"`     // Language code (en, zh, etc.)
	Speed        float64                        `json:"speed"`        // Speech speed (0.5-2.0)
	Pitch        float64                        `json:"pitch"`        // Voice pitch (-20 to 20)
	APIKey       string                         `json:"api_key"`      // API key
	APIURL       string                         `json:"api_url"`      // Custom API URL
	Region       string                         `json:"region"`       // Azure region
	Latency      string                         `json:"latency"`      // ultra_low, low, medium
	Instructions string                         `json:"instructions"` // Voice instructions
	Providers    map[string]ProviderCredentials `json:"providers"`    // Provider-specific credentials
}

// ProviderCredentials holds credentials for a specific provider
type ProviderCredentials struct {
	APIKey string `json:"api_key"`
	APIURL string `json:"api_url"`
	Region string `json:"region"`
	// GroupID 是 MiniMax 系接口特有的账号组 ID（t2a_v2 必须以 ?GroupId= 查询
	// 参数携带，缺了直接 404/invalid params）。其它厂商忽略此字段。
	GroupID string `json:"group_id,omitempty"`
}

// DefaultVoiceConfig returns a default voice configuration
func DefaultVoiceConfig() *VoiceConfig {
	return &VoiceConfig{
		Provider: "openai",
		// 默认模型 / 音色统一取自 defaults.go（唯一真源，catalog 引用同一张表）。
		Model:        DefaultModelFor("openai"),
		Voice:        DefaultVoiceFor("openai"),
		Language:     "en",
		Speed:        1.0,
		Pitch:        0,
		Latency:      "medium",
		Instructions: "",
		Providers:    make(map[string]ProviderCredentials),
	}
}

// Manager manages voice operations
type Manager struct {
	config         *VoiceConfig
	providers      map[string]VoiceProvider
	activeProvider VoiceProvider
	mu             sync.RWMutex
}

// VoiceResult represents a voice operation result
type VoiceResult struct {
	Audio      []byte  // Audio data
	Text       string  // Text content
	Duration   float64 // Duration in seconds
	Format     string  // Audio format (mp3, wav, ogg)
	SampleRate int     // Sample rate
	Provider   string  // Provider used
}

// NewManager creates a new voice manager
func NewManager(config *VoiceConfig) *Manager {
	m := &Manager{
		config:    config,
		providers: make(map[string]VoiceProvider),
	}

	// Register default providers
	m.registerDefaultProviders()

	// Set active provider
	if m.config != nil {
		m.config.Provider = m.resolveProviderName(m.config.Provider)
		m.activeProvider = m.providers[m.config.Provider]
	}

	return m
}

// resolveProviderName 把配置里的 provider 名归一到一个真正注册过的名字。
//
// 必要性：配置是可能"比代码旧"的。内置的本地 whisper CLI 已经从清单里移除，
// 但用户 config.json 里还留着 "provider": "whisper"。若不归一，activeProvider
// 会是 nil，之后每次朗读都报 provider not available，而用户从这句话里看不出
// 该改哪一项。这里静默回落到默认厂商并留一行日志。
func (m *Manager) resolveProviderName(name string) string {
	if _, ok := m.providers[name]; ok {
		return name
	}
	fallback := DefaultVoiceConfig().Provider
	if name != "" {
		log.Printf("voice: provider %q is no longer available, falling back to %q", name, fallback)
	}
	return fallback
}

// registerDefaultProviders registers built-in providers
func (m *Manager) registerDefaultProviders() {
	if m.config == nil {
		m.config = DefaultVoiceConfig()
	}

	// 每家只吃"属于自己"的模型和音色：配置里的 model/voice 是给当前选中的
	// provider 用的，原样发给别家等于拿 A 的模型打 B 的接口（必然 404）。
	// 以前五家共用 m.config.Model，切换厂商后不重填模型就会带着上一家的模型发请求。

	// OpenAI TTS/ASR
	openaiConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("openai"),
		Model:    m.modelFor("openai"),
		Voice:    m.voiceFor("openai"),
		Language: m.config.Language,
		Speed:    m.config.Speed,
		Pitch:    m.config.Pitch,
	}
	m.providers["openai"] = NewOpenAIProvider(openaiConfig)

	// ElevenLabs TTS
	elevenlabsConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("elevenlabs"),
		Model:    m.modelFor("elevenlabs"),
		Voice:    m.voiceFor("elevenlabs"),
		Language: m.config.Language,
		Speed:    m.config.Speed,
	}
	m.providers["elevenlabs"] = NewElevenLabsProvider(elevenlabsConfig)

	// Azure TTS/ASR
	azureConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("azure"),
		Region:   m.getProviderRegion("azure"),
		Model:    m.modelFor("azure"),
		Voice:    m.voiceFor("azure"),
		Language: m.config.Language,
		Speed:    m.config.Speed,
		Pitch:    m.config.Pitch,
	}
	m.providers["azure"] = NewAzureProvider(azureConfig)

	// Google TTS/ASR
	googleConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("google"),
		Model:    m.modelFor("google"),
		Voice:    m.voiceFor("google"),
		Language: m.config.Language,
		Speed:    m.config.Speed,
		Pitch:    m.config.Pitch,
	}
	m.providers["google"] = NewGoogleTTSProvider(googleConfig)
}

// getProviderAPIKey gets API key for a provider
func (m *Manager) getProviderAPIKey(provider string) string {
	// Check provider-specific credentials first
	if creds, ok := m.config.Providers[provider]; ok && creds.APIKey != "" {
		return creds.APIKey
	}
	// Fall back to global API key
	return m.config.APIKey
}

// getProviderRegion gets region for a provider
func (m *Manager) getProviderRegion(provider string) string {
	// Check provider-specific credentials first
	if creds, ok := m.config.Providers[provider]; ok && creds.Region != "" {
		return creds.Region
	}
	// Fall back to global region
	return m.config.Region
}

// modelFor 返回该 provider 实际该用的模型。
//
// 配置里的 voice.model 只对"当前选中的 provider"成立：用户把 provider 切到
// ElevenLabs、模型栏还留着 gpt-4o-mini-tts 时，最合理的行为是用 ElevenLabs
// 自己的默认值，而不是把 OpenAI 的模型名发过去。没配就回落到 defaults.go。
func (m *Manager) modelFor(provider string) string {
	if strings.EqualFold(strings.TrimSpace(m.config.Provider), provider) {
		if v := strings.TrimSpace(m.config.Model); v != "" {
			return v
		}
	}
	return DefaultModelFor(provider)
}

// voiceFor 同 modelFor，管音色（音色 ID 各家也不通用）。
func (m *Manager) voiceFor(provider string) string {
	if strings.EqualFold(strings.TrimSpace(m.config.Provider), provider) {
		if v := strings.TrimSpace(m.config.Voice); v != "" {
			return v
		}
	}
	return DefaultVoiceFor(provider)
}

// GetProviders returns list of available providers
func (m *Manager) GetProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := make([]string, 0, len(m.providers))
	for name := range m.providers {
		providers = append(providers, name)
	}
	return providers
}

// GetAvailableProviders returns list of providers that are currently available (have API keys)
func (m *Manager) GetAvailableProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := make([]string, 0)
	for name, provider := range m.providers {
		if provider.IsAvailable() {
			providers = append(providers, name)
		}
	}
	return providers
}

// SetProvider sets the active voice provider
func (m *Manager) SetProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	provider, ok := m.providers[name]
	if !ok {
		return fmt.Errorf("unknown provider: %s", name)
	}

	if !provider.IsAvailable() {
		return fmt.Errorf("provider not available: %s (API key may not be configured)", name)
	}

	m.activeProvider = provider
	m.config.Provider = name
	return nil
}

// GetActiveProvider returns the currently active provider
func (m *Manager) GetActiveProvider() VoiceProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeProvider
}

// GetProvider returns a specific provider by name
func (m *Manager) GetProvider(name string) (VoiceProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	return provider, nil
}

// TTS converts text to speech
func (m *Manager) TTS(ctx context.Context, text string) (*VoiceResult, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("no active voice provider configured")
	}

	if !provider.IsAvailable() {
		return nil, fmt.Errorf("provider %s is not available (API key may not be configured)", provider.Name())
	}

	audio, err := provider.TTS(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("TTS failed: %w", err)
	}

	format := DetectAudioFormat(audio)
	if format == "unknown" {
		format = "mp3"
	}

	return &VoiceResult{
		Audio:    audio,
		Text:     text,
		Duration: estimateDuration(text),
		Format:   format,
		Provider: provider.Name(),
	}, nil
}

// TTSWithProvider converts text to speech using a specific provider
func (m *Manager) TTSWithProvider(ctx context.Context, providerName, text string) (*VoiceResult, error) {
	provider, err := m.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	if !provider.IsAvailable() {
		return nil, fmt.Errorf("provider %s is not available (API key may not be configured)", providerName)
	}

	audio, err := provider.TTS(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("TTS failed: %w", err)
	}

	format := DetectAudioFormat(audio)
	if format == "unknown" {
		format = "mp3"
	}

	return &VoiceResult{
		Audio:    audio,
		Text:     text,
		Duration: estimateDuration(text),
		Format:   format,
		Provider: provider.Name(),
	}, nil
}

// ASR converts speech to text
func (m *Manager) ASR(ctx context.Context, audio []byte) (*VoiceResult, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("no active voice provider configured")
	}

	if !provider.IsAvailable() {
		return nil, fmt.Errorf("provider %s is not available (API key may not be configured)", provider.Name())
	}

	text, err := provider.ASR(ctx, audio)
	if err != nil {
		return nil, fmt.Errorf("ASR failed: %w", err)
	}

	format := DetectAudioFormat(audio)
	if format == "unknown" {
		format = "mp3"
	}

	return &VoiceResult{
		Audio:    audio,
		Text:     text,
		Duration: GetAudioDuration(audio, format),
		Format:   format,
		Provider: provider.Name(),
	}, nil
}

// ASRWithProvider converts speech to text using a specific provider
func (m *Manager) ASRWithProvider(ctx context.Context, providerName string, audio []byte) (*VoiceResult, error) {
	provider, err := m.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	if !provider.IsAvailable() {
		return nil, fmt.Errorf("provider %s is not available (API key may not be configured)", providerName)
	}

	text, err := provider.ASR(ctx, audio)
	if err != nil {
		return nil, fmt.Errorf("ASR failed: %w", err)
	}

	format := DetectAudioFormat(audio)
	if format == "unknown" {
		format = "mp3"
	}

	return &VoiceResult{
		Audio:    audio,
		Text:     text,
		Duration: GetAudioDuration(audio, format),
		Format:   format,
		Provider: provider.Name(),
	}, nil
}

// StreamTTS streams text to speech
func (m *Manager) StreamTTS(ctx context.Context, text string) (<-chan *VoiceResult, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("no active voice provider configured")
	}

	audioCh, err := provider.StreamTTS(ctx, text)
	if err != nil {
		return nil, err
	}

	resultCh := make(chan *VoiceResult)
	go func() {
		defer close(resultCh)
		var fullAudio []byte
		for chunk := range audioCh {
			fullAudio = append(fullAudio, chunk...)
			select {
			case resultCh <- &VoiceResult{
				Audio:    chunk,
				Format:   "mp3",
				Provider: provider.Name(),
			}:
			case <-ctx.Done():
				return
			}
		}
		// Send final result
		select {
		case resultCh <- &VoiceResult{
			Audio:    fullAudio,
			Text:     text,
			Duration: estimateDuration(text),
			Format:   "mp3",
			Provider: provider.Name(),
		}:
		case <-ctx.Done():
		}
	}()

	return resultCh, nil
}

// StreamASR streams speech to text
func (m *Manager) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("no active voice provider configured")
	}

	return provider.StreamASR(ctx, audio)
}

// SaveAudio saves audio data to file
func (m *Manager) SaveAudio(audio []byte, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}
	return os.WriteFile(path, audio, 0644)
}

// LoadAudio loads audio data from file
func (m *Manager) LoadAudio(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Base64Encode encodes audio to base64
func (m *Manager) Base64Encode(audio []byte) string {
	return base64.StdEncoding.EncodeToString(audio)
}

// Base64Decode decodes base64 to audio
func (m *Manager) Base64Decode(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(data)
}

// PlayAudio plays audio using system audio player
func (m *Manager) PlayAudio(audio []byte) error {
	return PlayAudio(audio)
}

// DownloadAudio downloads audio from URL
func (m *Manager) DownloadAudio(ctx context.Context, url string) ([]byte, error) {
	return DownloadAudio(ctx, url)
}

// ConvertAudioFormat converts audio to different format
func (m *Manager) ConvertAudioFormat(audio []byte, format string) ([]byte, error) {
	return ConvertAudioFormat(audio, format)
}

// GetAvailableVoices returns available voices for a provider and language
func (m *Manager) GetAvailableVoices(provider, language string) []string {
	return GetAvailableVoices(provider, language)
}

// GetSupportedLanguages returns supported languages for a provider
func (m *Manager) GetSupportedLanguages(provider string) []string {
	return GetSupportedLanguages(provider)
}

// UpdateConfig updates the voice configuration
func (m *Manager) UpdateConfig(config *VoiceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = config
	m.registerDefaultProviders()

	// Update active provider
	if m.config != nil {
		m.config.Provider = m.resolveProviderName(m.config.Provider)
		m.activeProvider = m.providers[m.config.Provider]
	}

	return nil
}

// GetConfig returns the current voice configuration
func (m *Manager) GetConfig() *VoiceConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// estimateDuration estimates speech duration based on text
func estimateDuration(text string) float64 {
	// Rough estimate: ~150 words per minute
	words := len(text) / 5 // rough word count
	return float64(words) / 150.0 * 60.0
}

// ConversationSession represents a voice conversation session
type ConversationSession struct {
	ID        string
	StartTime time.Time
	Messages  []VoiceMessage
	Language  string
	IsActive  bool
}

// VoiceMessage represents a voice message in a conversation
type VoiceMessage struct {
	Role      string // user, assistant
	Text      string // transcribed text
	Audio     []byte // audio data
	Timestamp time.Time
}

// StartSession starts a new voice conversation
func (m *Manager) StartSession(ctx context.Context, language string) (*ConversationSession, error) {
	session := &ConversationSession{
		ID:        fmt.Sprintf("voice_%d", time.Now().Unix()),
		StartTime: time.Now(),
		Language:  language,
		IsActive:  true,
		Messages:  []VoiceMessage{},
	}
	return session, nil
}

// ProcessVoiceInput processes voice input and returns text
func (m *Manager) ProcessVoiceInput(ctx context.Context, session *ConversationSession, audio []byte) (string, error) {
	result, err := m.ASR(ctx, audio)
	if err != nil {
		return "", err
	}

	session.Messages = append(session.Messages, VoiceMessage{
		Role:      "user",
		Text:      result.Text,
		Audio:     audio,
		Timestamp: time.Now(),
	})

	return result.Text, nil
}

// GenerateVoiceResponse generates voice response for text
func (m *Manager) GenerateVoiceResponse(ctx context.Context, session *ConversationSession, text string) ([]byte, error) {
	result, err := m.TTS(ctx, text)
	if err != nil {
		return nil, err
	}

	session.Messages = append(session.Messages, VoiceMessage{
		Role:      "assistant",
		Text:      text,
		Audio:     result.Audio,
		Timestamp: time.Now(),
	})

	return result.Audio, nil
}
