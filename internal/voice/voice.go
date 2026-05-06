package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config holds voice configuration
type Config struct {
	STTProvider   string `json:"stt_provider"`     // "whisper", "faster-whisper", "google"
	TTSProvider   string `json:"tts_provider"`     // "openai", "elevenlabs", "google"
	TTSVoice      string `json:"tts_voice"`        // TTS voice ID
	PushToTalkKey string `json:"push_to_talk_key"` // Key for push-to-talk
	APIKey        string `json:"api_key"`          // API key for STT/TTS
	BaseURL       string `json:"base_url"`         // Base URL for API
	AudioFormat   string `json:"audio_format"`     // "mp3", "wav", "ogg"
	SampleRate    int    `json:"sample_rate"`      // Sample rate in Hz
}

// DefaultConfig returns default voice configuration
func DefaultConfig() *Config {
	return &Config{
		STTProvider:   "whisper",
		TTSProvider:   "openai",
		TTSVoice:      "alloy",
		PushToTalkKey: "space",
		AudioFormat:   "mp3",
		SampleRate:    16000,
	}
}

// STTProvider interface for speech-to-text
type STTProvider interface {
	Transcribe(ctx context.Context, audioData []byte) (string, error)
}

// TTSProvider interface for text-to-speech
type TTSProvider interface {
	Speak(ctx context.Context, text string) ([]byte, error)
}

// Manager manages voice input/output
type Manager struct {
	config    *Config
	stt       STTProvider
	tts       TTSProvider
	recording bool
	mu        sync.Mutex
	audioFile string
}

// NewManager creates a new voice manager
func NewManager(cfg *Config) (*Manager, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	mgr := &Manager{
		config: cfg,
	}

	// Initialize STT provider
	switch cfg.STTProvider {
	case "whisper", "faster-whisper":
		mgr.stt = &WhisperSTT{config: cfg}
	case "openai":
		mgr.stt = &OpenAISTT{config: cfg}
	default:
		mgr.stt = &WhisperSTT{config: cfg}
	}

	// Initialize TTS provider
	switch cfg.TTSProvider {
	case "openai":
		mgr.tts = &OpenAITTS{config: cfg}
	case "elevenlabs":
		mgr.tts = &ElevenLabsTTS{config: cfg}
	default:
		mgr.tts = &OpenAITTS{config: cfg}
	}

	return mgr, nil
}

// StartRecording starts audio recording
func (m *Manager) StartRecording() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.recording {
		return fmt.Errorf("already recording")
	}

	// Create temp file for recording
	m.audioFile = fmt.Sprintf("/tmp/voice_%s.%s", uuid.New().String()[:8], m.config.AudioFormat)

	// Start recording using arecord or sox
	cmd := exec.Command("rec",
		"-r", fmt.Sprintf("%d", m.config.SampleRate),
		"-c", "1",
		"-b", "16",
		m.audioFile,
	)
	if err := cmd.Start(); err != nil {
		// Try alternative: ffmpeg
		cmd = exec.Command("ffmpeg", "-f", "alsa", "-i", "default",
			"-ar", fmt.Sprintf("%d", m.config.SampleRate),
			"-ac", "1",
			"-acodec", "pcm_s16le",
			m.audioFile,
		)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start recording: %w", err)
		}
	}

	m.recording = true
	return nil
}

// StopRecording stops audio recording and returns the audio file path
func (m *Manager) StopRecording() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.recording {
		return "", fmt.Errorf("not recording")
	}

	// Stop recording by sending signal
	exec.Command("pkill", "-f", "rec").Run()
	exec.Command("pkill", "-f", "ffmpeg.*"+m.audioFile).Run()

	m.recording = false
	return m.audioFile, nil
}

// TranscribeAudio transcribes audio file to text
func (m *Manager) TranscribeAudio(ctx context.Context, audioPath string) (string, error) {
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}

	return m.stt.Transcribe(ctx, data)
}

// TranscribeData transcribes raw audio data to text
func (m *Manager) TranscribeData(ctx context.Context, audioData []byte) (string, error) {
	return m.stt.Transcribe(ctx, audioData)
}

// Speak converts text to speech and plays it
func (m *Manager) Speak(ctx context.Context, text string) error {
	audioData, err := m.tts.Speak(ctx, text)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	// Save to temp file and play
	tmpFile := fmt.Sprintf("/tmp/voice_reply_%s.mp3", uuid.New().String()[:8])
	if err := os.WriteFile(tmpFile, audioData, 0644); err != nil {
		return fmt.Errorf("failed to write audio file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Play audio
	cmd := exec.Command("mpg123", "-q", tmpFile)
	if err := cmd.Run(); err != nil {
		// Try alternative: aplay or paplay
		cmd = exec.Command("ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", tmpFile)
		cmd.Run()
	}

	return nil
}

// SpeakToFile converts text to speech and saves to file
func (m *Manager) SpeakToFile(ctx context.Context, text, outputPath string) error {
	audioData, err := m.tts.Speak(ctx, text)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	return os.WriteFile(outputPath, audioData, 0644)
}

// IsRecording returns whether currently recording
func (m *Manager) IsRecording() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recording
}

// WhisperSTT implements STT using OpenAI Whisper API
type WhisperSTT struct {
	config *Config
}

func (s *WhisperSTT) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	baseURL := s.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// Create multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file
	part, err := writer.CreateFormFile("file", "audio.mp3")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/audio/transcriptions", body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s", string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Text, nil
}

// OpenAISTT is an alias for WhisperSTT (uses OpenAI Whisper API)
type OpenAISTT = WhisperSTT

// OpenAITTS implements TTS using OpenAI API
type OpenAITTS struct {
	config *Config
}

func (t *OpenAITTS) Speak(ctx context.Context, text string) ([]byte, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	voice := t.config.TTSVoice
	if voice == "" {
		voice = "alloy"
	}

	reqBody := map[string]interface{}{
		"model": "tts-1",
		"voice": voice,
		"input": text,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/audio/speech", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", string(audioData))
	}

	return audioData, nil
}

// ElevenLabsTTS implements TTS using ElevenLabs API
type ElevenLabsTTS struct {
	config *Config
}

func (t *ElevenLabsTTS) Speak(ctx context.Context, text string) ([]byte, error) {
	voice := t.config.TTSVoice
	if voice == "" {
		voice = "21m00Tcm4TlvDq8ikWAM" // Default voice
	}

	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", voice)

	reqBody := map[string]interface{}{
		"text": text,
		"voice_settings": map[string]interface{}{
			"stability":        0.5,
			"similarity_boost": 0.75,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", t.config.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", string(audioData))
	}

	return audioData, nil
}

// AudioRecorder provides low-level audio recording
type AudioRecorder struct {
	sampleRate int
	channels   int
	format     string
}

// NewAudioRecorder creates a new audio recorder
func NewAudioRecorder(sampleRate, channels int, format string) *AudioRecorder {
	return &AudioRecorder{
		sampleRate: sampleRate,
		channels:   channels,
		format:     format,
	}
}

// RecordToFile records audio to a file
func (r *AudioRecorder) RecordToFile(ctx context.Context, duration time.Duration, outputPath string) error {
	args := []string{
		"-f", "alsa",
		"-i", "default",
		"-ar", fmt.Sprintf("%d", r.sampleRate),
		"-ac", fmt.Sprintf("%d", r.channels),
	}

	switch r.format {
	case "wav":
		args = append(args, "-acodec", "pcm_s16le", outputPath)
	case "mp3":
		args = append(args, "-acodec", "libmp3lame", "-q:a", "2", outputPath)
	case "ogg":
		args = append(args, "-acodec", "libvorbis", outputPath)
	default:
		args = append(args, outputPath)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd.Run()
}

// EncodeBase64 encodes audio data as base64
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeBase64 decodes base64 audio data
func DecodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
