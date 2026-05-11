package voice

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
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
	Provider       string  // openai, elevenlabs, azure, coqui
	Model          string  // TTS/ASR model
	Voice          string  // Voice ID
	Language       string  // Language code (en, zh, etc.)
	Speed          float64 // Speech speed (0.5-2.0)
	Pitch          float64 // Voice pitch (-20 to 20)
	APIKey         string  // API key
	Endpoint       string  // Custom endpoint
	Latency        string  // ultra_low, low, medium
	Instructions   string  // Voice instructions
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
		Endpoint:     "",
		Latency:      "medium",
		Instructions: "",
	}
}

// Manager manages voice operations
type Manager struct {
	config     *VoiceConfig
	providers  map[string]VoiceProvider
	activeProvider VoiceProvider
	mu         sync.RWMutex
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
	// OpenAI TTS
	m.providers["openai"] = &OpenAIProvider{}
	
	// ElevenLabs TTS
	m.providers["elevenlabs"] = &ElevenLabsProvider{}
	
	// Coqui TTS (open source)
	m.providers["coqui"] = &CoquiProvider{}
	
	// Azure TTS
	m.providers["azure"] = &AzureProvider{}
	
	// Local/whisper ASR
	m.providers["whisper"] = &WhisperProvider{}
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

// SetProvider sets the active voice provider
func (m *Manager) SetProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	provider, ok := m.providers[name]
	if !ok {
		return fmt.Errorf("unknown provider: %s", name)
	}
	
	if !provider.IsAvailable() {
		return fmt.Errorf("provider not available: %s", name)
	}
	
	m.activeProvider = provider
	m.config.Provider = name
	return nil
}

// TTS converts text to speech
func (m *Manager) TTS(ctx context.Context, text string) (*VoiceResult, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()
	
	if provider == nil {
		return nil, fmt.Errorf("no active voice provider")
	}
	
	audio, err := provider.TTS(ctx, text)
	if err != nil {
		return nil, err
	}
	
	return &VoiceResult{
		Audio:    audio,
		Text:     text,
		Duration: estimateDuration(text),
		Format:   "mp3",
		Provider: provider.Name(),
	}, nil
}

// ASR converts speech to text
func (m *Manager) ASR(ctx context.Context, audio []byte) (*VoiceResult, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()
	
	if provider == nil {
		return nil, fmt.Errorf("no active voice provider")
	}
	
	text, err := provider.ASR(ctx, audio)
	if err != nil {
		return nil, err
	}
	
	return &VoiceResult{
		Audio:    audio,
		Text:     text,
		Duration: estimateDuration(text),
		Provider: provider.Name(),
	}, nil
}

// StreamTTS streams text to speech
func (m *Manager) StreamTTS(ctx context.Context, text string) (<-chan *VoiceResult, error) {
	m.mu.RLock()
	provider := m.activeProvider
	m.mu.RUnlock()
	
	if provider == nil {
		return nil, fmt.Errorf("no active voice provider")
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
		return nil, fmt.Errorf("no active voice provider")
	}
	
	return provider.StreamASR(ctx, audio)
}

// SaveAudio saves audio data to file
func (m *Manager) SaveAudio(audio []byte, path string) error {
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

// VoicePreset represents a voice preset
type VoicePreset struct {
	Name         string
	Provider     string
	Model        string
	VoiceID      string
	Speed        float64
	Pitch        float64
	Instructions string
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
		},
		{
			Name:     "Professional",
			Provider: "elevenlabs",
			Model:    "eleven_monolingual_v1",
			VoiceID:  "cj40qdj02ou0fbxa0n7y",
			Speed:    1.0,
		},
		{
			Name:     "Friendly",
			Provider: "elevenlabs",
			Model:    "eleven_multilingual_v2",
			VoiceID:  "cgSgspJ2z5r8hbh5y6vx",
			Speed:    1.1,
		},
		{
			Name:     "Chinese",
			Provider: "coqui",
			Model:    "tts_models/zh/baker/tacotron2-DDCG",
			Speed:    1.0,
		},
	}
}

// estimateDuration estimates speech duration based on text
func estimateDuration(text string) float64 {
	// Rough estimate: ~150 words per minute
	words := len(text) / 5 // rough word count
	return float64(words) / 150.0 * 60.0
}

// OpenAIProvider implements OpenAI TTS
type OpenAIProvider struct {
	apiKey   string
	endpoint string
	model    string
	voice    string
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) IsAvailable() bool { return true }

func (p *OpenAIProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	// Placeholder - would need OpenAI API integration
	return []byte{}, nil
}

func (p *OpenAIProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	return "", nil
}

func (p *OpenAIProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
		// Placeholder
	}()
	return ch, nil
}

func (p *OpenAIProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
		// Placeholder
	}()
	return ch, nil
}

// ElevenLabsProvider implements ElevenLabs TTS
type ElevenLabsProvider struct {
	apiKey string
	model  string
	voice  string
}

func (p *ElevenLabsProvider) Name() string { return "elevenlabs" }

func (p *ElevenLabsProvider) IsAvailable() bool { return true }

func (p *ElevenLabsProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	return []byte{}, nil
}

func (p *ElevenLabsProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	return "", nil
}

func (p *ElevenLabsProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *ElevenLabsProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

// CoquiProvider implements Coqui TTS (open source)
type CoquiProvider struct {
	model string
}

func (p *CoquiProvider) Name() string { return "coqui" }

func (p *CoquiProvider) IsAvailable() bool { return true }

func (p *CoquiProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	return []byte{}, nil
}

func (p *CoquiProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	return "", nil
}

func (p *CoquiProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *CoquiProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

// AzureProvider implements Azure TTS
type AzureProvider struct {
	apiKey   string
	region   string
	endpoint string
}

func (p *AzureProvider) Name() string { return "azure" }

func (p *AzureProvider) IsAvailable() bool { return true }

func (p *AzureProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	return []byte{}, nil
}

func (p *AzureProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	return "", nil
}

func (p *AzureProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *AzureProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

// WhisperProvider implements Whisper ASR
type WhisperProvider struct {
	model    string
	endpoint string
}

func (p *WhisperProvider) Name() string { return "whisper" }

func (p *WhisperProvider) IsAvailable() bool { return true }

func (p *WhisperProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	return nil, fmt.Errorf("whisper is ASR only")
}

func (p *WhisperProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	return "", nil
}

func (p *WhisperProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	return nil, fmt.Errorf("whisper is ASR only")
}

func (p *WhisperProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

// ConversationSession represents a voice conversation session
type ConversationSession struct {
	ID          string
	StartTime   time.Time
	Messages    []VoiceMessage
	Language    string
	IsActive    bool
}

// VoiceMessage represents a voice message in a conversation
type VoiceMessage struct {
	Role      string    // user, assistant
	Text      string    // transcribed text
	Audio     []byte    // audio data
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
