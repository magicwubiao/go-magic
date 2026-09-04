package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/vision"
	"github.com/magicwubiao/go-magic/internal/voice"
)

var voiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Voice mode for Magic Agent",
	Long: `Enable voice interaction with Magic Agent.
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

// Voice flags
var (
	voiceProvider string
	voiceLanguage string
	voiceVoiceID  string
	voiceModel    string
)

// Image attachment flags for message commands
var (
	attachImage string
	attachAudio string
	attachVideo string
	attachFile  string
)

func init() {
	rootCmd.Flags().StringVar(&attachImage, "image", "", "Attach an image file")
	rootCmd.Flags().StringVar(&attachAudio, "audio", "", "Attach an audio file")
	rootCmd.Flags().StringVar(&attachVideo, "video", "", "Attach a video file")
	rootCmd.Flags().StringVar(&attachFile, "file", "", "Attach a generic file")

	rootCmd.Flags().StringVar(&voiceProvider, "provider", "", "Voice provider (openai, elevenlabs, azure, coqui)")
	rootCmd.Flags().StringVar(&voiceLanguage, "lang", "en", "Voice language code")
	rootCmd.Flags().StringVar(&voiceVoiceID, "voice", "", "Voice ID for TTS")
	rootCmd.Flags().StringVar(&voiceModel, "model", "tts-1", "TTS model")

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
		cfg.Provider = "openai"
	}

	// Override with flags
	if voiceProvider != "" {
		cfg.Provider = voiceProvider
	}
	if voiceLanguage != "" {
		cfg.Language = voiceLanguage
	}
	if voiceVoiceID != "" {
		cfg.Voice = voiceVoiceID
	}
	if voiceModel != "" {
		cfg.Model = voiceModel
	}

	// Create voice manager
	_ = voice.NewManager(cfg)

	fmt.Println("Starting voice mode...")
	fmt.Println("Press Ctrl+B to start recording, speak, then stop speaking to auto-detect")
	fmt.Println("Press Ctrl+C to exit")

	// Start listening (simplified - just show message)
	fmt.Println("\nVoice listening is ready.")
	fmt.Println("Note: Full push-to-talk requires terminal/audio setup.")
}

func runVoiceSpeak(cmd *cobra.Command, args []string) {
	cfg := voice.DefaultVoiceConfig()

	// Override with environment variables
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
		cfg.Provider = "openai"
	}

	// Override with flags
	if voiceProvider != "" {
		cfg.Provider = voiceProvider
	}
	if voiceLanguage != "" {
		cfg.Language = voiceLanguage
	}
	if voiceVoiceID != "" {
		cfg.Voice = voiceVoiceID
	}
	if voiceModel != "" {
		cfg.Model = voiceModel
	}

	text := strings.Join(args, " ")

	// Create voice manager
	manager := voice.NewManager(cfg)

	fmt.Printf("Speaking: %s\n", text)

	// Convert to speech
	result, err := manager.TTS(context.Background(), text)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Save to file
	outputFile := "output.mp3"
	if err := os.WriteFile(outputFile, result.Audio, 0644); err != nil {
		fmt.Printf("Error saving audio: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Audio saved to: %s (%s format)\n", outputFile, result.Format)
}

func runVoiceTest(cmd *cobra.Command, args []string) {
	cfg := voice.DefaultVoiceConfig()

	// Check environment variables
	fmt.Println("Voice Configuration Test")
	fmt.Println("=========================")

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		fmt.Println("✓ OPENAI_API_KEY is set")
		cfg.APIKey = apiKey
		cfg.Provider = "openai"
	} else {
		fmt.Println("✗ OPENAI_API_KEY is not set")
	}

	if azureKey := os.Getenv("AZURE_SPEECH_KEY"); azureKey != "" {
		fmt.Println("✓ AZURE_SPEECH_KEY is set")
	} else {
		fmt.Println("✗ AZURE_SPEECH_KEY is not set")
	}

	if elevenlabsKey := os.Getenv("ELEVENLABS_API_KEY"); elevenlabsKey != "" {
		fmt.Println("✓ ELEVENLABS_API_KEY is set")
	} else {
		fmt.Println("✗ ELEVENLABS_API_KEY is not set")
	}

	fmt.Printf("\nDefault Provider: %s\n", cfg.Provider)
	fmt.Printf("Default Model: %s\n", cfg.Model)
	fmt.Printf("Default Language: %s\n", cfg.Language)
	fmt.Printf("Default Voice: %s\n", cfg.Voice)

	// Create manager and test
	manager := voice.NewManager(cfg)

	fmt.Println("\nTesting TTS...")
	testText := "Hello, this is a test of the voice system."
	result, err := manager.TTS(context.Background(), testText)
	if err != nil {
		fmt.Printf("✗ TTS test failed: %v\n", err)
	} else {
		fmt.Printf("✓ TTS test passed (generated %d bytes, format: %s)\n", len(result.Audio), result.Format)
	}
}

// =============================================================================
// Vision Image Understanding
// =============================================================================

var visionCmd = &cobra.Command{
	Use:   "vision",
	Short: "Vision/image understanding commands",
	Long:  `Commands for image understanding and analysis.`,
}

var visionAnalyzeCmd = &cobra.Command{
	Use:   "analyze <image_path_or_url>",
	Short: "Analyze an image using vision model",
	Args:  cobra.MinimumNArgs(1),
	Run:   runVisionAnalyze,
}

var visionCompareCmd = &cobra.Command{
	Use:   "compare <image1> <image2>",
	Short: "Compare two images",
	Args:  cobra.ExactArgs(2),
	Run:   runVisionCompare,
}

func init() {
	rootCmd.AddCommand(visionCmd)
	visionCmd.AddCommand(visionAnalyzeCmd)
	visionCmd.AddCommand(visionCompareCmd)
}

func runVisionAnalyze(cmd *cobra.Command, args []string) {
	imagePath := args[0]

	// Validate image exists or is URL
	if !strings.HasPrefix(imagePath, "http://") && !strings.HasPrefix(imagePath, "https://") {
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			fmt.Printf("Error: image file not found: %s\n", imagePath)
			os.Exit(1)
		}
	}

	// Create vision manager
	manager := vision.NewManager(nil)

	fmt.Printf("Analyzing image: %s\n", imagePath)
	fmt.Println("Please wait...")

	// Analyze
	result, err := manager.AnalyzeImage(context.Background(), imagePath, "", "")
	if err != nil {
		fmt.Printf("Error analyzing image: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nAnalysis Result:")
	fmt.Printf("Description: %s\n", result.Description)
	if len(result.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(result.Tags, ", "))
	}
	if result.Confidence > 0 {
		fmt.Printf("Confidence: %.1f%%\n", result.Confidence*100)
	}
}

func runVisionCompare(cmd *cobra.Command, args []string) {
	image1Path := args[0]
	image2Path := args[1]

	// Create vision manager
	manager := vision.NewManager(nil)

	fmt.Println("Comparing images...")
	fmt.Println("Please wait...")

	// Analyze both images
	result1, err := manager.AnalyzeImage(context.Background(), image1Path, "", "")
	if err != nil {
		fmt.Printf("Error analyzing image 1: %v\n", err)
		os.Exit(1)
	}

	result2, err := manager.AnalyzeImage(context.Background(), image2Path, "", "")
	if err != nil {
		fmt.Printf("Error analyzing image 2: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nComparison Result:")
	fmt.Printf("Image 1: %s\n", result1.Description)
	fmt.Printf("Image 2: %s\n", result2.Description)
	fmt.Printf("Image 1 tags: %s\n", strings.Join(result1.Tags, ", "))
	fmt.Printf("Image 2 tags: %s\n", strings.Join(result2.Tags, ", "))
}
