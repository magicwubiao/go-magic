package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/voice"
	"github.com/magicwubiao/go-magic/pkg/config"
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
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	voiceCfg := voice.DefaultConfig()
	if cfg.Voice != nil {
		voiceCfg.APIKey = os.Getenv("OPENAI_API_KEY")
		voiceCfg.TTSVoice = cfg.Voice.TTSVoice
		voiceCfg.STTProvider = cfg.Voice.STTProvider
		voiceCfg.TTSProvider = cfg.Voice.TTSProvider
	}

	// Override with environment variable if available
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		voiceCfg.APIKey = apiKey
	}

	mgr, err := voice.NewManager(voiceCfg)
	_ = mgr
	if err != nil {
		fmt.Printf("Failed to create voice manager: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Voice mode started. Press the configured key (default: space) to record.")
	fmt.Println("Press Ctrl+C to exit.")

	fmt.Println("\nNote: This is a demo. Full push-to-talk requires terminal integration.")
	fmt.Println("In a full implementation, you would:" +
		"\n  1. Listen for key press\n" +
		"  2. Start recording on key down\n" +
		"  3. Stop recording on key up\n" +
		"  4. Transcribe and send to Agent\n" +
		"  5. Convert Agent response to speech")

	fmt.Println("\nTo test TTS, run: magic voice speak 'Hello, I am magic Agent'")
}

func runVoiceSpeak(cmd *cobra.Command, args []string) {
	text := args[0]
	var mgr *voice.Manager

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	voiceCfg := voice.DefaultConfig()
	voiceCfg.APIKey = os.Getenv("OPENAI_API_KEY")

	if cfg.Voice != nil {
		voiceCfg.TTSVoice = cfg.Voice.TTSVoice
		voiceCfg.TTSProvider = cfg.Voice.TTSProvider
	}

	if voiceCfg.APIKey == "" {
		fmt.Println("Error: OPENAI_API_KEY not set")
		fmt.Println("Set it with: export OPENAI_API_KEY=your-key")
		os.Exit(1)
	}

	mgr, err = voice.NewManager(voiceCfg)
	if err != nil {
		fmt.Printf("Failed to create voice manager: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Speaking: %s\n", text)

	if err := mgr.Speak(cmd.Context(), text); err != nil {
		fmt.Printf("Failed to speak: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Done!")
}

func runVoiceTest(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Voice Configuration Test")
	fmt.Println("========================")

	voiceCfg := voice.DefaultConfig()
	if cfg.Voice != nil {
		voiceCfg = cfg.Voice
	}

	fmt.Printf("\nSTT Provider: %s\n", voiceCfg.STTProvider)
	fmt.Printf("TTS Provider: %s\n", voiceCfg.TTSProvider)
	fmt.Printf("TTS Voice: %s\n", voiceCfg.TTSVoice)
	fmt.Printf("Push-to-talk Key: %s\n", voiceCfg.PushToTalkKey)
	fmt.Printf("Audio Format: %s\n", voiceCfg.AudioFormat)
	fmt.Printf("Sample Rate: %d Hz\n", voiceCfg.SampleRate)

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		fmt.Println("\nAPI Key: ✓ Set")
	} else {
		fmt.Println("\nAPI Key: ✗ Not set (required for STT/TTS)")
	}

	fmt.Println("\nNote: To test actual voice functionality, run:")
	fmt.Println("  magic voice speak 'Hello world'")
}
