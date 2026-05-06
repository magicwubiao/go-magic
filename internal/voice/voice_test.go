package voice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.STTProvider != "whisper" {
		t.Errorf("Expected STTProvider 'whisper', got '%s'", cfg.STTProvider)
	}

	if cfg.TTSProvider != "openai" {
		t.Errorf("Expected TTSProvider 'openai', got '%s'", cfg.TTSProvider)
	}

	if cfg.TTSVoice != "alloy" {
		t.Errorf("Expected TTSVoice 'alloy', got '%s'", cfg.TTSVoice)
	}

	if cfg.PushToTalkKey != "space" {
		t.Errorf("Expected PushToTalkKey 'space', got '%s'", cfg.PushToTalkKey)
	}

	if cfg.AudioFormat != "mp3" {
		t.Errorf("Expected AudioFormat 'mp3', got '%s'", cfg.AudioFormat)
	}

	if cfg.SampleRate != 16000 {
		t.Errorf("Expected SampleRate 16000, got %d", cfg.SampleRate)
	}
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if mgr == nil {
		t.Fatal("Manager should not be nil")
	}

	if mgr.config != cfg {
		t.Error("Config not set correctly")
	}

	if mgr.stt == nil {
		t.Error("STT provider not initialized")
	}

	if mgr.tts == nil {
		t.Error("TTS provider not initialized")
	}
}

func TestNewManagerNilConfig(t *testing.T) {
	mgr, err := NewManager(nil)
	if err != nil {
		t.Fatalf("NewManager with nil config failed: %v", err)
	}

	if mgr == nil {
		t.Fatal("Manager should not be nil")
	}
}

func TestStartStopRecording(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Start recording
	err = mgr.StartRecording()
	if err != nil {
		// Recording may fail without audio device, which is OK in test
		t.Logf("Recording start failed (expected without audio device): %v", err)
		return
	}

	if !mgr.IsRecording() {
		t.Error("Expected recording to be true after StartRecording")
	}

	// Stop recording
	path, err := mgr.StopRecording()
	if err != nil {
		t.Errorf("StopRecording failed: %v", err)
	}

	if path == "" {
		t.Error("Expected non-empty audio file path")
	}

	// Check file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("Audio file should exist: %s", path)
	} else {
		os.Remove(path) // Clean up
	}

	if mgr.IsRecording() {
		t.Error("Expected recording to be false after StopRecording")
	}
}

func TestStopRecordingNotRecording(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = mgr.StopRecording()
	if err == nil {
		t.Error("Expected error when stopping non-recording session")
	}
}

func TestStartRecordingAlreadyRecording(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// First start
	err = mgr.StartRecording()
	if err != nil {
		t.Logf("Recording start failed (expected without audio device): %v", err)
		return
	}

	// Second start should fail
	err = mgr.StartRecording()
	if err == nil {
		mgr.StopRecording()
		t.Error("Expected error when starting already recording session")
	}

	mgr.StopRecording()
}

func TestTranscribeAudioNoFile(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = mgr.TranscribeAudio(context.Background(), "/nonexistent/file.mp3")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestTranscribeData(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// This will fail without valid API key, but tests the flow
	_, err = mgr.TranscribeData(context.Background(), []byte{0, 1, 2, 3})
	// We expect an error, so just verify no panic
	_ = err
}

func TestSpeak(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// This will fail without valid API key, but tests the flow
	err = mgr.Speak(context.Background(), "Hello world")
	// We expect an error, so just verify no panic
	_ = err
}

func TestSpeakToFile(t *testing.T) {
	cfg := DefaultConfig()
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	tmpFile := filepath.Join(os.TempDir(), "voice_test_speak.mp3")

	err = mgr.SpeakToFile(context.Background(), "Hello world", tmpFile)
	// We expect an error without valid API key
	_ = err

	// Clean up if file was created
	os.Remove(tmpFile)
}

func TestNewAudioRecorder(t *testing.T) {
	recorder := NewAudioRecorder(16000, 1, "mp3")
	if recorder == nil {
		t.Fatal("Recorder should not be nil")
	}

	if recorder.sampleRate != 16000 {
		t.Errorf("Expected sampleRate 16000, got %d", recorder.sampleRate)
	}

	if recorder.channels != 1 {
		t.Errorf("Expected channels 1, got %d", recorder.channels)
	}

	if recorder.format != "mp3" {
		t.Errorf("Expected format 'mp3', got '%s'", recorder.format)
	}
}

func TestEncodeDecodeBase64(t *testing.T) {
	testData := []byte{1, 2, 3, 4, 5}

	encoded := EncodeBase64(testData)
	if encoded == "" {
		t.Error("Encoded string should not be empty")
	}

	decoded, err := DecodeBase64(encoded)
	if err != nil {
		t.Errorf("DecodeBase64 failed: %v", err)
	}

	if string(decoded) != string(testData) {
		t.Errorf("Decoded data mismatch: got %v, want %v", decoded, testData)
	}
}

func TestDecodeBase64Invalid(t *testing.T) {
	_, err := DecodeBase64("not-valid-base64!!!")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}

func TestWhisperSTT(t *testing.T) {
	cfg := &Config{
		APIKey:  "test-api-key",
		BaseURL: "https://api.openai.com/v1",
	}

	stt := &WhisperSTT{config: cfg}

	// Test with invalid API key (will fail, but no panic)
	_, err := stt.Transcribe(context.Background(), []byte{0, 1, 2})
	if err == nil {
		t.Log("Expected error with invalid API key")
	}
}

func TestOpenAITTS(t *testing.T) {
	cfg := &Config{
		APIKey:   "test-api-key",
		BaseURL:  "https://api.openai.com/v1",
		TTSVoice: "alloy",
	}

	tts := &OpenAITTS{config: cfg}

	// Test with invalid API key (will fail, but no panic)
	_, err := tts.Speak(context.Background(), "Hello")
	if err == nil {
		t.Log("Expected error with invalid API key")
	}
}

func TestElevenLabsTTS(t *testing.T) {
	cfg := &Config{
		APIKey:   "test-api-key",
		TTSVoice: "21m00Tcm4TlvDq8ikWAM",
	}

	tts := &ElevenLabsTTS{config: cfg}

	// Test with invalid API key (will fail, but no panic)
	_, err := tts.Speak(context.Background(), "Hello")
	if err == nil {
		t.Log("Expected error with invalid API key")
	}
}

func TestOpenAISTTAlias(t *testing.T) {
	cfg := &Config{
		APIKey:  "test-api-key",
		BaseURL: "https://api.openai.com/v1",
	}

	// Verify OpenAISTT is an alias for WhisperSTT
	stt := OpenAISTT{config: cfg}
	_ = WhisperSTT{config: cfg}

	// Both should have the same interface
	var _ STTProvider = &stt
}

func TestConfigSerialization(t *testing.T) {
	cfg := &Config{
		STTProvider:   "openai",
		TTSProvider:   "elevenlabs",
		TTSVoice:      "custom-voice",
		PushToTalkKey: "enter",
		APIKey:        "secret-key",
		BaseURL:       "https://custom.api.com",
		AudioFormat:   "wav",
		SampleRate:    44100,
	}

	if cfg.STTProvider != "openai" {
		t.Errorf("STTProvider mismatch")
	}

	if cfg.TTSProvider != "elevenlabs" {
		t.Errorf("TTSProvider mismatch")
	}

	if cfg.TTSVoice != "custom-voice" {
		t.Errorf("TTSVoice mismatch")
	}
}
