package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/voice"
)

var voiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Voice mode for magic Agent",
	Long: `Enable voice interaction with magic Agent.
Supports push-to-talk, speech-to-text, and text-to-speech.`,
}

var voiceListenCmd = &cobra.Command{
	Use:   "listen",
	Short: "Start voice mode (push-to-talk)",
	Run:   runVoiceListen,
}

var voiceSpeakCmd = &cobra.Command{
	Use:   "speak <text>",
	Short: "Convert text to speech",
	Args:  cobra.MinimumNArgs(1),
	Run:   runVoiceSpeak,
}

var voiceTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test voice configuration",
	Run:   runVoiceTest,
}

func init() {
	rootCmd.AddCommand(voiceCmd)
	voiceCmd.AddCommand(voiceListenCmd)
	voiceCmd.AddCommand(voiceSpeakCmd)
	voiceCmd.AddCommand(voiceTestCmd)
}

func runVoiceListen(cmd *cobra.Command, args []string) {
	cfg := voice.DefaultVoiceConfig()
	
	// Override with environment variables
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
	}
	
	mgr := voice.NewManager(cfg)
	
	fmt.Println("Voice mode started. Press the configured key (default: space) to record.")
	fmt.Println("Press Ctrl+C to exit.")

	fmt.Println("\nNote: This is a demo. Full push-to-talk requires terminal integration.")
	fmt.Println("In a full implementation, you would:" +
		"\n  1. Listen for key press" +
		"\n  2. Start recording on key down" +
		"\n  3. Stop recording on key up" +
		"\n  4. Transcribe and send to Agent" +
		"\n  5. Convert Agent response to speech")

	fmt.Println("\nTo test TTS, run: magic voice speak 'Hello, I am magic Agent'")
	_ = mgr
}

func runVoiceSpeak(cmd *cobra.Command, args []string) {
	text := args[0]
	cfg := voice.DefaultVoiceConfig()
	cfg.APIKey = os.Getenv("OPENAI_API_KEY")
	
	if cfg.APIKey == "" {
		fmt.Println("Error: OPENAI_API_KEY not set")
		fmt.Println("Set it with: export OPENAI_API_KEY=your-key")
		os.Exit(1)
	}

	mgr := voice.NewManager(cfg)
	
	fmt.Printf("Speaking: %s\n", text)
	
	// Use TTS directly
	_, err := mgr.TTS(context.Background(), text)
	if err != nil {
		fmt.Printf("Failed to speak: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Done!")
}

func runVoiceTest(cmd *cobra.Command, args []string) {
	cfg := voice.DefaultVoiceConfig()
	
	fmt.Println("Voice Configuration Test")
	fmt.Println("========================")

	fmt.Printf("\nProvider: %s\n", cfg.Provider)
	fmt.Printf("Model: %s\n", cfg.Model)
	fmt.Printf("Voice: %s\n", cfg.Voice)
	fmt.Printf("Language: %s\n", cfg.Language)
	fmt.Printf("Speed: %.1fx\n", cfg.Speed)
	
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		fmt.Println("\nAPI Key: Set")
	} else {
		fmt.Println("\nAPI Key: Not set")
	}
	
	fmt.Println("\nTest available providers:")
	providers := []string{"openai", "elevenlabs", "azure", "coqui", "system"}
	for _, p := range providers {
		fmt.Printf("  - %s\n", p)
	}
}
