package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// HTTPClient is the HTTP client used for API requests
var HTTPClient = &http.Client{
	Timeout: 60 * time.Second,
}

// ProviderConfig holds configuration for voice providers
type ProviderConfig struct {
	APIKey   string
	APIURL   string
	Region   string
	Model    string
	Voice    string
	Language string
	Speed    float64
	Pitch    float64
}

// OpenAIProvider implements OpenAI TTS
// Docs: https://platform.openai.com/docs/guides/text-to-speech
type OpenAIProvider struct {
	config *ProviderConfig
}

// NewOpenAIProvider creates a new OpenAI TTS provider
func NewOpenAIProvider(config *ProviderConfig) *OpenAIProvider {
	if config == nil {
		config = &ProviderConfig{}
	}
	return &OpenAIProvider{config: config}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) IsAvailable() bool {
	return p.config.APIKey != ""
}

func (p *OpenAIProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	if p.config.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key not configured")
	}

	model := p.config.Model
	if model == "" {
		model = "tts-1"
	}

	voice := p.config.Voice
	if voice == "" {
		voice = "alloy"
	}

	speed := p.config.Speed
	if speed == 0 {
		speed = 1.0
	}

	reqBody := map[string]interface{}{
		"model": model,
		"input": text,
		"voice": voice,
		"speed": speed,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/speech", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %s - %s", resp.Status, string(body))
	}

	return io.ReadAll(resp.Body)
}

func (p *OpenAIProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	if p.config.APIKey == "" {
		return "", fmt.Errorf("OpenAI API key not configured")
	}

	model := p.config.Model
	if model == "" {
		model = "whisper-1"
	}

	// Create multipart form data
	var body bytes.Buffer

	// Write audio file
	fmt.Fprintf(&body, "--boundary\r\n")
	fmt.Fprintf(&body, "Content-Disposition: form-data; name=\"file\"; filename=\"audio.mp3\"\r\n")
	fmt.Fprintf(&body, "Content-Type: audio/mpeg\r\n\r\n")
	body.Write(audio)
	fmt.Fprintf(&body, "\r\n")

	// Write model
	fmt.Fprintf(&body, "--boundary\r\n")
	fmt.Fprintf(&body, "Content-Disposition: form-data; name=\"model\"\r\n\r\n")
	fmt.Fprintf(&body, "%s\r\n", model)
	fmt.Fprintf(&body, "--boundary--\r\n")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/transcriptions", &body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenAI API error: %s - %s", resp.Status, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Text, nil
}

func (p *OpenAIProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte, 10)

	go func() {
		defer close(ch)

		audio, err := p.TTS(ctx, text)
		if err != nil {
			return
		}

		// Send in chunks
		chunkSize := 4096
		for i := 0; i < len(audio); i += chunkSize {
			end := i + chunkSize
			if end > len(audio) {
				end = len(audio)
			}
			select {
			case ch <- audio[i:end]:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string, 10)

	go func() {
		defer close(ch)

		// Collect audio chunks
		var fullAudio []byte
		for chunk := range audio {
			fullAudio = append(fullAudio, chunk...)
		}

		if len(fullAudio) == 0 {
			return
		}

		text, err := p.ASR(ctx, fullAudio)
		if err != nil {
			return
		}

		select {
		case ch <- text:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// ElevenLabsProvider implements ElevenLabs TTS
// Docs: https://elevenlabs.io/docs/api-reference/text-to-speech
type ElevenLabsProvider struct {
	config *ProviderConfig
}

// NewElevenLabsProvider creates a new ElevenLabs TTS provider
func NewElevenLabsProvider(config *ProviderConfig) *ElevenLabsProvider {
	if config == nil {
		config = &ProviderConfig{}
	}
	return &ElevenLabsProvider{config: config}
}

func (p *ElevenLabsProvider) Name() string { return "elevenlabs" }

func (p *ElevenLabsProvider) IsAvailable() bool {
	return p.config.APIKey != ""
}

func (p *ElevenLabsProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	if p.config.APIKey == "" {
		return nil, fmt.Errorf("ElevenLabs API key not configured")
	}

	voiceID := p.config.Voice
	if voiceID == "" {
		voiceID = "21m00Tcm4TlvDq8ikWAM" // Default voice (Rachel)
	}

	model := p.config.Model
	if model == "" {
		model = "eleven_multilingual_v2"
	}

	speed := p.config.Speed
	if speed == 0 {
		speed = 1.0
	}

	reqBody := map[string]interface{}{
		"text":     text,
		"model_id": model,
		"voice_settings": map[string]interface{}{
			"stability":         0.5,
			"similarity_boost":  0.75,
			"style":             0.0,
			"use_speaker_boost": true,
			"speed":             speed,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", voiceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("xi-api-key", p.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ElevenLabs API error: %s - %s", resp.Status, string(body))
	}

	return io.ReadAll(resp.Body)
}

func (p *ElevenLabsProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	return "", fmt.Errorf("ElevenLabs does not support ASR")
}

func (p *ElevenLabsProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte, 10)

	go func() {
		defer close(ch)

		audio, err := p.TTS(ctx, text)
		if err != nil {
			return
		}

		chunkSize := 4096
		for i := 0; i < len(audio); i += chunkSize {
			end := i + chunkSize
			if end > len(audio) {
				end = len(audio)
			}
			select {
			case ch <- audio[i:end]:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (p *ElevenLabsProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	return nil, fmt.Errorf("ElevenLabs does not support ASR")
}

// AzureProvider implements Azure Speech Services
// Docs: https://docs.microsoft.com/en-us/azure/cognitive-services/speech-service/
type AzureProvider struct {
	config *ProviderConfig
}

// NewAzureProvider creates a new Azure Speech provider
func NewAzureProvider(config *ProviderConfig) *AzureProvider {
	if config == nil {
		config = &ProviderConfig{}
	}
	return &AzureProvider{config: config}
}

func (p *AzureProvider) Name() string { return "azure" }

func (p *AzureProvider) IsAvailable() bool {
	return p.config.APIKey != "" && p.config.Region != ""
}

func (p *AzureProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	if p.config.APIKey == "" || p.config.Region == "" {
		return nil, fmt.Errorf("Azure Speech API key and region not configured")
	}

	language := p.config.Language
	if language == "" {
		language = "en-US"
	}

	voiceName := p.config.Voice
	if voiceName == "" {
		// Default voices by language
		voiceMap := map[string]string{
			"en-US": "en-US-JennyNeural",
			"en-GB": "en-GB-SoniaNeural",
			"zh-CN": "zh-CN-XiaoxiaoNeural",
			"zh-HK": "zh-HK-HiuMaanNeural",
			"ja-JP": "ja-JP-NanamiNeural",
			"ko-KR": "ko-KR-SunHiNeural",
			"de-DE": "de-DE-KatjaNeural",
			"fr-FR": "fr-FR-DeniseNeural",
			"es-ES": "es-ES-ElviraNeural",
		}
		if v, ok := voiceMap[language]; ok {
			voiceName = v
		} else {
			voiceName = "en-US-JennyNeural"
		}
	}

	speed := p.config.Speed
	if speed == 0 {
		speed = 1.0
	}

	pitch := p.config.Pitch

	// SSML for more control
	ssml := fmt.Sprintf(`<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="%s">
		<voice name="%s">
			<prosody rate="%.1f" pitch="%+.1f%%">%s</prosody>
		</voice>
	</speak>`, language, voiceName, (speed-1)*100, pitch*10, escapeXML(text))

	url := fmt.Sprintf("https://%s.tts.speech.microsoft.com/cognitiveservices/v1", p.config.Region)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(ssml))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Ocp-Apim-Subscription-Key", p.config.APIKey)
	req.Header.Set("Content-Type", "application/ssml+xml")
	req.Header.Set("X-Microsoft-OutputFormat", "audio-16khz-128kbitrate-mono-mp3")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Azure API error: %s - %s", resp.Status, string(body))
	}

	return io.ReadAll(resp.Body)
}

func (p *AzureProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	if p.config.APIKey == "" || p.config.Region == "" {
		return "", fmt.Errorf("Azure Speech API key and region not configured")
	}

	language := p.config.Language
	if language == "" {
		language = "en-US"
	}

	url := fmt.Sprintf("https://%s.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1?language=%s", p.config.Region, language)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(audio))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Ocp-Apim-Subscription-Key", p.config.APIKey)
	req.Header.Set("Content-Type", "audio/wav")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Azure API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		RecognitionStatus string `json:"RecognitionStatus"`
		DisplayText       string `json:"DisplayText"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if result.RecognitionStatus != "Success" {
		return "", fmt.Errorf("recognition failed: %s", result.RecognitionStatus)
	}

	return result.DisplayText, nil
}

func (p *AzureProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte, 10)

	go func() {
		defer close(ch)

		audio, err := p.TTS(ctx, text)
		if err != nil {
			return
		}

		chunkSize := 4096
		for i := 0; i < len(audio); i += chunkSize {
			end := i + chunkSize
			if end > len(audio) {
				end = len(audio)
			}
			select {
			case ch <- audio[i:end]:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (p *AzureProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string, 10)

	go func() {
		defer close(ch)

		var fullAudio []byte
		for chunk := range audio {
			fullAudio = append(fullAudio, chunk...)
		}

		if len(fullAudio) == 0 {
			return
		}

		text, err := p.ASR(ctx, fullAudio)
		if err != nil {
			return
		}

		select {
		case ch <- text:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// GoogleTTSProvider implements Google Cloud Text-to-Speech
// Docs: https://cloud.google.com/text-to-speech/docs
type GoogleTTSProvider struct {
	config *ProviderConfig
}

// NewGoogleTTSProvider creates a new Google TTS provider
func NewGoogleTTSProvider(config *ProviderConfig) *GoogleTTSProvider {
	if config == nil {
		config = &ProviderConfig{}
	}
	return &GoogleTTSProvider{config: config}
}

func (p *GoogleTTSProvider) Name() string { return "google" }

func (p *GoogleTTSProvider) IsAvailable() bool {
	return p.config.APIKey != ""
}

func (p *GoogleTTSProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	if p.config.APIKey == "" {
		return nil, fmt.Errorf("Google API key not configured")
	}

	language := p.config.Language
	if language == "" {
		language = "en-US"
	}

	voiceName := p.config.Voice
	if voiceName == "" {
		voiceName = fmt.Sprintf("%s-Standard-C", language)
	}

	speed := p.config.Speed
	if speed == 0 {
		speed = 1.0
	}

	pitch := p.config.Pitch

	reqBody := map[string]interface{}{
		"input": map[string]interface{}{
			"text": text,
		},
		"voice": map[string]interface{}{
			"languageCode": language,
			"name":         voiceName,
		},
		"audioConfig": map[string]interface{}{
			"audioEncoding": "MP3",
			"speakingRate":  speed,
			"pitch":         pitch,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := "https://texttospeech.googleapis.com/v1/text:synthesize"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.config.APIKey)

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Google API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		AudioContent string `json:"audioContent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Decode base64 audio
	return base64DecodeString(result.AudioContent)
}

func (p *GoogleTTSProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	if p.config.APIKey == "" {
		return "", fmt.Errorf("Google API key not configured")
	}

	language := p.config.Language
	if language == "" {
		language = "en-US"
	}

	// Encode audio to base64
	audioB64 := base64EncodeData(audio)

	reqBody := map[string]interface{}{
		"config": map[string]interface{}{
			"encoding":        "MP3",
			"sampleRateHertz": 16000,
			"languageCode":    language,
		},
		"audio": map[string]interface{}{
			"content": audioB64,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := "https://speech.googleapis.com/v1/speech:recognize"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.config.APIKey)

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Google API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Results []struct {
			Alternatives []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
			} `json:"alternatives"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Results) == 0 || len(result.Results[0].Alternatives) == 0 {
		return "", fmt.Errorf("no speech recognized")
	}

	return result.Results[0].Alternatives[0].Transcript, nil
}

func (p *GoogleTTSProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	ch := make(chan []byte, 10)

	go func() {
		defer close(ch)

		audio, err := p.TTS(ctx, text)
		if err != nil {
			return
		}

		chunkSize := 4096
		for i := 0; i < len(audio); i += chunkSize {
			end := i + chunkSize
			if end > len(audio) {
				end = len(audio)
			}
			select {
			case ch <- audio[i:end]:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (p *GoogleTTSProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string, 10)

	go func() {
		defer close(ch)

		var fullAudio []byte
		for chunk := range audio {
			fullAudio = append(fullAudio, chunk...)
		}

		if len(fullAudio) == 0 {
			return
		}

		text, err := p.ASR(ctx, fullAudio)
		if err != nil {
			return
		}

		select {
		case ch <- text:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// WhisperProvider implements OpenAI Whisper for ASR only
// This is a specialized provider for local Whisper installations
type WhisperProvider struct {
	config *ProviderConfig
}

// NewWhisperProvider creates a new Whisper ASR provider
func NewWhisperProvider(config *ProviderConfig) *WhisperProvider {
	if config == nil {
		config = &ProviderConfig{}
	}
	return &WhisperProvider{config: config}
}

func (p *WhisperProvider) Name() string { return "whisper" }

func (p *WhisperProvider) IsAvailable() bool {
	// Check if whisper command is available
	_, err := exec.LookPath("whisper")
	return err == nil
}

func (p *WhisperProvider) TTS(ctx context.Context, text string) ([]byte, error) {
	return nil, fmt.Errorf("whisper is ASR only")
}

func (p *WhisperProvider) ASR(ctx context.Context, audio []byte) (string, error) {
	// Save audio to temp file
	tmpDir, err := os.MkdirTemp("", "whisper-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	audioPath := filepath.Join(tmpDir, "input.mp3")
	if err := os.WriteFile(audioPath, audio, 0644); err != nil {
		return "", fmt.Errorf("failed to write audio file: %w", err)
	}

	language := p.config.Language
	if language == "" {
		language = "en"
	}

	model := p.config.Model
	if model == "" {
		model = "base"
	}

	// Run whisper command
	cmd := exec.CommandContext(ctx, "whisper", audioPath,
		"--model", model,
		"--language", language,
		"--output_format", "txt",
		"--output_dir", tmpDir,
	)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("whisper command failed: %w", err)
	}

	// Read output
	outputPath := filepath.Join(tmpDir, "input.txt")
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to read output: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

func (p *WhisperProvider) StreamTTS(ctx context.Context, text string) (<-chan []byte, error) {
	return nil, fmt.Errorf("whisper is ASR only")
}

func (p *WhisperProvider) StreamASR(ctx context.Context, audio <-chan []byte) (<-chan string, error) {
	ch := make(chan string, 10)

	go func() {
		defer close(ch)

		var fullAudio []byte
		for chunk := range audio {
			fullAudio = append(fullAudio, chunk...)
		}

		if len(fullAudio) == 0 {
			return
		}

		text, err := p.ASR(ctx, fullAudio)
		if err != nil {
			return
		}

		select {
		case ch <- text:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// Helper functions

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// base64EncodeData encodes bytes to base64 string
func base64EncodeData(data []byte) string {
	const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	if len(data) == 0 {
		return ""
	}

	var result strings.Builder
	result.Grow((len(data) + 2) / 3 * 4)

	for i := 0; i < len(data); i += 3 {
		b1 := data[i]
		b2 := byte(0)
		b3 := byte(0)

		if i+1 < len(data) {
			b2 = data[i+1]
		}
		if i+2 < len(data) {
			b3 = data[i+2]
		}

		idx1 := b1 >> 2
		idx2 := ((b1 & 0x03) << 4) | (b2 >> 4)
		idx3 := ((b2 & 0x0F) << 2) | (b3 >> 6)
		idx4 := b3 & 0x3F

		result.WriteByte(base64Chars[idx1])
		result.WriteByte(base64Chars[idx2])

		if i+1 < len(data) {
			result.WriteByte(base64Chars[idx3])
		} else {
			result.WriteByte('=')
		}

		if i+2 < len(data) {
			result.WriteByte(base64Chars[idx4])
		} else {
			result.WriteByte('=')
		}
	}

	return result.String()
}

// base64DecodeString decodes base64 string to bytes
func base64DecodeString(s string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}

	// Remove padding
	s = strings.TrimRight(s, "=")

	var result strings.Builder
	result.Grow(len(s) * 3 / 4)

	decodeChar := func(c byte) byte {
		if c >= 'A' && c <= 'Z' {
			return c - 'A'
		}
		if c >= 'a' && c <= 'z' {
			return c - 'a' + 26
		}
		if c >= '0' && c <= '9' {
			return c - '0' + 52
		}
		if c == '+' {
			return 62
		}
		if c == '/' {
			return 63
		}
		return 0
	}

	for i := 0; i < len(s); i += 4 {
		var buf [3]byte
		n := 0

		c1 := decodeChar(s[i])
		c2 := decodeChar(s[i+1])
		buf[0] = (c1 << 2) | (c2 >> 4)
		n = 1

		var c3 byte
		if i+2 < len(s) {
			c3 = decodeChar(s[i+2])
			buf[1] = ((c2 & 0x0F) << 4) | (c3 >> 2)
			n = 2
		}

		if i+3 < len(s) {
			c4 := decodeChar(s[i+3])
			buf[2] = ((c3 & 0x03) << 6) | c4
			n = 3
		}

		result.Write(buf[:n])
	}

	return []byte(result.String()), nil
}

// GetAvailableVoices returns available voices for a provider and language
func GetAvailableVoices(provider, language string) []string {
	switch provider {
	case "openai":
		return []string{"alloy", "echo", "fable", "onyx", "nova", "shimmer"}
	case "elevenlabs":
		return []string{
			"21m00Tcm4TlvDq8ikWAM", // Rachel
			"AZnzlk1XvdvUeBnXmlld", // Domi
			"EXAVITQu4vr4xnSDxMaL", // Bella
			"ErXwobaYiN019PkySvjV", // Antoni
			"MF3mGyEYCl7XYWbV9V6O", // Elli
			"TxGEqnHWrfWFTfGW9XjX", // Josh
			"VR6AewLTigWG4xSOukaG", // Arnold
			"pNInz6obpgDQGcFmaJgB", // Adam
			"yoZ06aMxZJJ28mfd3POQ", // Sam
		}
	case "azure":
		voices := map[string][]string{
			"en": {"en-US-JennyNeural", "en-US-GuyNeural", "en-GB-SoniaNeural", "en-AU-NatashaNeural"},
			"zh": {"zh-CN-XiaoxiaoNeural", "zh-CN-YunxiNeural", "zh-CN-YunjianNeural", "zh-HK-HiuMaanNeural"},
			"ja": {"ja-JP-NanamiNeural", "ja-JP-KeitaNeural"},
			"ko": {"ko-KR-SunHiNeural", "ko-KR-InJoonNeural"},
			"de": {"de-DE-KatjaNeural", "de-DE-ConradNeural"},
			"fr": {"fr-FR-DeniseNeural", "fr-FR-HenriNeural"},
			"es": {"es-ES-ElviraNeural", "es-ES-AlvaroNeural"},
		}
		if v, ok := voices[language]; ok {
			return v
		}
		return voices["en"]
	case "google":
		return []string{
			"Standard-A", "Standard-B", "Standard-C", "Standard-D",
			"Wavenet-A", "Wavenet-B", "Wavenet-C", "Wavenet-D",
		}
	default:
		return []string{"default"}
	}
}

// GetSupportedLanguages returns supported languages for a provider
func GetSupportedLanguages(provider string) []string {
	switch provider {
	case "openai":
		return []string{"en", "zh", "ja", "ko", "de", "fr", "es", "it", "pt", "ru", "ar", "hi"}
	case "elevenlabs":
		return []string{"en", "zh", "ja", "ko", "de", "fr", "es", "it", "pt", "pl", "hi", "ar"}
	case "azure":
		return []string{"en-US", "en-GB", "zh-CN", "zh-HK", "ja-JP", "ko-KR", "de-DE", "fr-FR", "es-ES", "it-IT", "pt-BR", "ru-RU", "ar-SA", "hi-IN"}
	case "google":
		return []string{"en-US", "en-GB", "zh-CN", "ja-JP", "ko-KR", "de-DE", "fr-FR", "es-ES", "it-IT", "pt-BR", "ru-RU", "ar-XA", "hi-IN"}
	default:
		return []string{"en"}
	}
}

// PlayAudio plays audio using system audio player
func PlayAudio(audio []byte) error {
	// Save to temp file
	tmpFile, err := os.CreateTemp("", "magic-audio-*.mp3")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(audio); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write audio: %w", err)
	}
	tmpFile.Close()

	// Play based on OS
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("afplay", tmpFile.Name())
	case "linux":
		// Try different players
		if _, err := exec.LookPath("mpg123"); err == nil {
			cmd = exec.Command("mpg123", "-q", tmpFile.Name())
		} else if _, err := exec.LookPath("mpg321"); err == nil {
			cmd = exec.Command("mpg321", "-q", tmpFile.Name())
		} else if _, err := exec.LookPath("ffplay"); err == nil {
			cmd = exec.Command("ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", tmpFile.Name())
		} else if _, err := exec.LookPath("paplay"); err == nil {
			cmd = exec.Command("paplay", tmpFile.Name())
		} else {
			return fmt.Errorf("no audio player found (tried mpg123, mpg321, ffplay, paplay)")
		}
	case "windows":
		// Use PowerShell to play audio
		psCmd := fmt.Sprintf(`Add-Type -AssemblyName presentationCore; $player = New-Object System.Windows.Media.MediaPlayer; $player.Open("%s"); $player.Play(); Start-Sleep -Seconds 10`, tmpFile.Name())
		cmd = exec.Command("powershell", "-c", psCmd)
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	return cmd.Run()
}

// DownloadAudio downloads audio from URL
func DownloadAudio(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download audio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// DetectAudioFormat detects audio format from data
func DetectAudioFormat(data []byte) string {
	if len(data) < 4 {
		return "unknown"
	}

	// MP3: ID3 or MPEG sync word
	if string(data[:3]) == "ID3" || (data[0] == 0xFF && (data[1]&0xE0) == 0xE0) {
		return "mp3"
	}

	// WAV: RIFF header
	if string(data[:4]) == "RIFF" {
		return "wav"
	}

	// OGG
	if string(data[:4]) == "OggS" {
		return "ogg"
	}

	// FLAC
	if string(data[:4]) == "fLaC" {
		return "flac"
	}

	// M4A/AAC
	if string(data[:4]) == "ftyp" || string(data[4:8]) == "ftyp" {
		return "m4a"
	}

	return "unknown"
}

// ConvertAudioFormat converts audio to different format using ffmpeg
func ConvertAudioFormat(input []byte, outputFormat string) ([]byte, error) {
	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found, please install ffmpeg")
	}

	// Create temp files
	tmpDir, err := os.MkdirTemp("", "audio-convert-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input.audio")
	outputPath := filepath.Join(tmpDir, "output."+outputFormat)

	if err := os.WriteFile(inputPath, input, 0644); err != nil {
		return nil, fmt.Errorf("failed to write input file: %w", err)
	}

	// Run ffmpeg
	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-y",
		"-loglevel", "error",
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	return os.ReadFile(outputPath)
}

// GetAudioDuration estimates audio duration in seconds
func GetAudioDuration(audio []byte, format string) float64 {
	switch format {
	case "mp3":
		// Rough estimate: 128 kbps = 16 KB/s
		return float64(len(audio)) / 16000.0
	case "wav":
		// Assume 16-bit stereo 44.1kHz = 176.4 KB/s
		return float64(len(audio)) / 176400.0
	default:
		return float64(len(audio)) / 16000.0
	}
}

// ValidateAudio validates audio data
func ValidateAudio(audio []byte, maxSize int) error {
	if len(audio) == 0 {
		return fmt.Errorf("audio data is empty")
	}

	if maxSize > 0 && len(audio) > maxSize {
		return fmt.Errorf("audio data too large: %d bytes (max %d)", len(audio), maxSize)
	}

	format := DetectAudioFormat(audio)
	if format == "unknown" {
		return fmt.Errorf("unknown audio format")
	}

	return nil
}

// URL-encoded path handling for file paths
func filePathToURL(path string) string {
	u := url.URL{Path: path}
	return u.String()
}
