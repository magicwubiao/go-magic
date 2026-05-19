package voice

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
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
	Provider     string                         `json:"provider"`     // openai, elevenlabs, azure, google, whisper
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
}

// DefaultVoiceConfig returns a default voice configuration
func DefaultVoiceConfig() *VoiceConfig {
	return &VoiceConfig{
		Provider:     "openai",
		Model:        "tts-1",
		Voice:        "alloy",
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
	if config != nil && config.Provider != "" {
		m.activeProvider = m.providers[config.Provider]
	}

	return m
}

// registerDefaultProviders registers built-in providers
func (m *Manager) registerDefaultProviders() {
	if m.config == nil {
		m.config = DefaultVoiceConfig()
	}

	// OpenAI TTS/ASR
	openaiConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("openai"),
		Model:    m.config.Model,
		Voice:    m.config.Voice,
		Language: m.config.Language,
		Speed:    m.config.Speed,
		Pitch:    m.config.Pitch,
	}
	m.providers["openai"] = NewOpenAIProvider(openaiConfig)

	// ElevenLabs TTS
	elevenlabsConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("elevenlabs"),
		Model:    m.config.Model,
		Voice:    m.config.Voice,
		Language: m.config.Language,
		Speed:    m.config.Speed,
	}
	m.providers["elevenlabs"] = NewElevenLabsProvider(elevenlabsConfig)

	// Azure TTS/ASR
	azureConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("azure"),
		Region:   m.getProviderRegion("azure"),
		Model:    m.config.Model,
		Voice:    m.config.Voice,
		Language: m.config.Language,
		Speed:    m.config.Speed,
		Pitch:    m.config.Pitch,
	}
	m.providers["azure"] = NewAzureProvider(azureConfig)

	// Google TTS/ASR
	googleConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("google"),
		Model:    m.config.Model,
		Voice:    m.config.Voice,
		Language: m.config.Language,
		Speed:    m.config.Speed,
		Pitch:    m.config.Pitch,
	}
	m.providers["google"] = NewGoogleTTSProvider(googleConfig)

	// Whisper ASR (local or API)
	whisperConfig := &ProviderConfig{
		APIKey:   m.getProviderAPIKey("whisper"),
		Model:    m.config.Model,
		Language: m.config.Language,
	}
	m.providers["whisper"] = NewWhisperProvider(whisperConfig)
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

// VoicePreset represents a voice preset
type VoicePreset struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	VoiceID      string  `json:"voice_id"`
	Speed        float64 `json:"speed"`
	Pitch        float64 `json:"pitch"`
	Instructions string  `json:"instructions"`
	Language     string  `json:"language"`
}

// GetPresets returns built-in voice presets
func (m *Manager) GetPresets() []VoicePreset {
	return []VoicePreset{
		{
			Name:     "Default",
			Provider: "openai",
			Model:    "tts-1",
			VoiceID:  "alloy",
			Speed:    1.0,
			Language: "en",
		},
		{
			Name:     "Professional",
			Provider: "elevenlabs",
			Model:    "eleven_monolingual_v1",
			VoiceID:  "21m00Tcm4TlvDq8ikWAM",
			Speed:    1.0,
			Language: "en",
		},
		{
			Name:     "Friendly",
			Provider: "elevenlabs",
			Model:    "eleven_multilingual_v2",
			VoiceID:  "AZnzlk1XvdvUeBnXmlld",
			Speed:    1.1,
			Language: "en",
		},
		{
			Name:     "Chinese Female",
			Provider: "azure",
			Model:    "",
			VoiceID:  "zh-CN-XiaoxiaoNeural",
			Speed:    1.0,
			Language: "zh-CN",
		},
		{
			Name:     "Chinese Male",
			Provider: "azure",
			Model:    "",
			VoiceID:  "zh-CN-YunxiNeural",
			Speed:    1.0,
			Language: "zh-CN",
		},
		{
			Name:     "Japanese",
			Provider: "azure",
			Model:    "",
			VoiceID:  "ja-JP-NanamiNeural",
			Speed:    1.0,
			Language: "ja-JP",
		},
		{
			Name:     "Google Natural",
			Provider: "google",
			Model:    "",
			VoiceID:  "Wavenet-C",
			Speed:    1.0,
			Language: "en-US",
		},
	}
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
	if config.Provider != "" {
		if provider, ok := m.providers[config.Provider]; ok {
			m.activeProvider = provider
		}
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
