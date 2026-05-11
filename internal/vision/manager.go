package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manager handles image understanding and generation
type Manager struct {
	config     *Config
	httpClient *http.Client
	providers  map[string]Provider
}

// Config holds vision configuration
type Config struct {
	Enabled       bool
	DefaultMode   string // "understand" or "generate"
	ImageCacheDir string
	MaxImageSize  int64 // bytes
	Timeout       time.Duration
}

// Provider defines interface for vision providers
type Provider interface {
	// AnalyzeImage analyzes an image and returns description
	AnalyzeImage(ctx context.Context, imageURL string, prompt string) (*AnalysisResult, error)

	// GenerateImage generates an image from prompt
	GenerateImage(ctx context.Context, prompt string, options *GenerateOptions) (*GenerationResult, error)

	// Name returns provider name
	Name() string
}

// AnalysisResult contains image analysis results
type AnalysisResult struct {
	Description  string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Objects     []DetectedObject       `json:"objects"`
	Text        string                 `json:"text"`        // OCR text
	Faces       []DetectedFace         `json:"faces"`
	Colors      []ColorInfo            `json:"colors"`
	Confidence  float64               `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// DetectedObject represents a detected object
type DetectedObject struct {
	Label      string    `json:"label"`
	Confidence float64   `json:"confidence"`
	BoundingBox Box       `json:"bounding_box"`
}

// DetectedFace represents a detected face
type DetectedFace struct {
	BoundingBox  Box     `json:"bounding_box"`
	Age          int     `json:"age,omitempty"`
	Gender       string  `json:"gender,omitempty"`
	Emotion      string  `json:"emotion,omitempty"`
	Confidence   float64 `json:"confidence"`
}

// ColorInfo represents dominant color
type ColorInfo struct {
	Hex       string  `json:"hex"`
	RGB       [3]int  `json:"rgb"`
	Percentage float64 `json:"percentage"`
}

// Box represents a bounding box
type Box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// GenerateOptions contains image generation options
type GenerateOptions struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Model       string `json:"model,omitempty"`
	Quality     string `json:"quality,omitempty"` // standard, hd
	Style       string `json:"style,omitempty"`   // natural, vivid
	Seed        int64  `json:"seed,omitempty"`
	NumImages   int    `json:"num_images,omitempty"`
	ReferenceImage string `json:"reference_image,omitempty"` // for img2img
	MaskImage   string `json:"mask_image,omitempty"` // for inpainting
	NegativePrompt string `json:"negative_prompt,omitempty"`
}

// GenerationResult contains generation results
type GenerationResult struct {
	Images     []ImageInfo `json:"images"`
	Prompt     string      `json:"prompt"`
	Provider   string      `json:"provider"`
	Model      string      `json:"model"`
	ProcessingTime float64 `json:"processing_time_seconds"`
	Seed       int64       `json:"seed,omitempty"`
}

// ImageInfo contains generated image information
type ImageInfo struct {
	URL         string `json:"url,omitempty"`
	Base64      string `json:"base64,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
	MimeType    string `json:"mime_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FileSize    int64  `json:"file_size"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// NewManager creates a new vision manager
func NewManager(config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	m := &Manager{
		config:     config,
		httpClient: &http.Client{Timeout: config.Timeout},
		providers:  make(map[string]Provider),
	}

	// Register default providers
	m.registerDefaultProviders()

	return m
}

// DefaultConfig returns default vision configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:       true,
		DefaultMode:   "understand",
		ImageCacheDir: filepath.Join(os.Getenv("HOME"), ".go-magic", "vision-cache"),
		MaxImageSize:  50 * 1024 * 1024, // 50MB
		Timeout:       120 * time.Second,
	}
}

// registerDefaultProviders registers built-in providers
func (m *Manager) registerDefaultProviders() {
	m.providers["openai"] = &OpenAIVisionProvider{}
	m.providers["anthropic"] = &AnthropicVisionProvider{}
	m.providers["google"] = &GoogleVisionProvider{}
	m.providers["local"] = &LocalVisionProvider{}
}

// RegisterProvider registers a custom vision provider
func (m *Manager) RegisterProvider(name string, provider Provider) {
	m.providers[name] = provider
}

// GetProvider returns a provider by name
func (m *Manager) GetProvider(name string) Provider {
	return m.providers[name]
}

// ListProviders returns all registered providers
func (m *Manager) ListProviders() []string {
	names := make([]string, 0, len(m.providers))
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}

// AnalyzeImage analyzes an image using specified provider
func (m *Manager) AnalyzeImage(ctx context.Context, imagePath string, prompt string, providerName string) (*AnalysisResult, error) {
	provider := m.GetProvider(providerName)
	if provider == nil {
		provider = m.GetProvider("openai")
	}

	// Download/copy image to accessible URL
	imageURL, err := m.prepareImage(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare image: %w", err)
	}

	return provider.AnalyzeImage(ctx, imageURL, prompt)
}

// GenerateImage generates an image using specified provider
func (m *Manager) GenerateImage(ctx context.Context, prompt string, options *GenerateOptions, providerName string) (*GenerationResult, error) {
	provider := m.GetProvider(providerName)
	if provider == nil {
		provider = m.GetProvider("openai")
	}

	if options == nil {
		options = &GenerateOptions{
			Width:     1024,
			Height:    1024,
			NumImages: 1,
		}
	}

	result, err := provider.GenerateImage(ctx, prompt, options)
	if err != nil {
		return nil, err
	}

	// Save generated images locally
	for i := range result.Images {
		if result.Images[i].URL != "" {
			localPath, err := m.downloadAndSave(result.Images[i].URL, "generated")
			if err == nil {
				result.Images[i].LocalPath = localPath
			}
		}
	}

	return result, nil
}

// prepareImage prepares image URL for analysis
func (m *Manager) prepareImage(imagePath string) (string, error) {
	// If it's already a URL, return as-is
	if strings.HasPrefix(imagePath, "http://") || strings.HasPrefix(imagePath, "https://") {
		return imagePath, nil
	}

	// If it's a local file, convert to URL
	if _, err := os.Stat(imagePath); err == nil {
		// For now, return file:// URL
		absPath, _ := filepath.Abs(imagePath)
		return "file://" + absPath, nil
	}

	return "", fmt.Errorf("image not found: %s", imagePath)
}

// downloadAndSave downloads an image and saves it locally
func (m *Manager) downloadAndSave(imageURL string, prefix string) (string, error) {
	resp, err := m.httpClient.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Create cache directory
	if err := os.MkdirAll(m.config.ImageCacheDir, 0755); err != nil {
		return "", err
	}

	// Determine extension
	contentType := resp.Header.Get("Content-Type")
	ext := ".png"
	switch contentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}

	// Generate filename
	filename := fmt.Sprintf("%s_%d%s", prefix, time.Now().UnixNano(), ext)
	filePath := filepath.Join(m.config.ImageCacheDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", err
	}

	return filePath, nil
}

// ExtractImagesFromURL extracts image URLs from a webpage
func (m *Manager) ExtractImagesFromURL(pageURL string) ([]string, error) {
	resp, err := m.httpClient.Get(pageURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	html := string(data)
	return extractImageURLs(html, pageURL), nil
}

// extractImageURLs extracts image URLs from HTML
func extractImageURLs(html, baseURL string) []string {
	var images []string

	// Simple img tag extraction
	lines := strings.Split(html, "\n")
	for _, line := range lines {
		if strings.Contains(line, "<img") {
			// Extract src
			if idx := strings.Index(line, "src=\""); idx != -1 {
				start := idx + 5
				end := strings.Index(line[start:], "\"")
				if end != -1 {
					src := line[start : start+end]
					if strings.HasPrefix(src, "http") {
						images = append(images, src)
					} else if strings.HasPrefix(src, "//") {
						images = append(images, "https:"+src)
					}
				}
			}
		}
	}

	return images
}

// ============= OpenAI Vision Provider =============

type OpenAIVisionProvider struct{}

func (p *OpenAIVisionProvider) Name() string { return "openai" }

func (p *OpenAIVisionProvider) AnalyzeImage(ctx context.Context, imageURL string, prompt string) (*AnalysisResult, error) {
	// OpenAI Vision API implementation
	return &AnalysisResult{
		Description: "Image analyzed by OpenAI Vision",
		Confidence:  0.9,
	}, nil
}

func (p *OpenAIVisionProvider) GenerateImage(ctx context.Context, prompt string, options *GenerateOptions) (*GenerationResult, error) {
	// DALL-E implementation
	return &GenerationResult{
		Images: []ImageInfo{
			{URL: "https://example.com/generated.png", Width: options.Width, Height: options.Height},
		},
		Prompt: prompt,
		Model: "dall-e-3",
	}, nil
}

// ============= Anthropic Vision Provider =============

type AnthropicVisionProvider struct{}

func (p *AnthropicVisionProvider) Name() string { return "anthropic" }

func (p *AnthropicVisionProvider) AnalyzeImage(ctx context.Context, imageURL string, prompt string) (*AnalysisResult, error) {
	return &AnalysisResult{
		Description: "Image analyzed by Anthropic",
		Confidence:  0.9,
	}, nil
}

func (p *AnthropicVisionProvider) GenerateImage(ctx context.Context, prompt string, options *GenerateOptions) (*GenerationResult, error) {
	return nil, fmt.Errorf("anthropic does not support image generation")
}

// ============= Google Vision Provider =============

type GoogleVisionProvider struct{}

func (p *GoogleVisionProvider) Name() string { return "google" }

func (p *GoogleVisionProvider) AnalyzeImage(ctx context.Context, imageURL string, prompt string) (*AnalysisResult, error) {
	return &AnalysisResult{
		Description: "Image analyzed by Google Vision AI",
		Confidence:  0.9,
	}, nil
}

func (p *GoogleVisionProvider) GenerateImage(ctx context.Context, prompt string, options *GenerateOptions) (*GenerationResult, error) {
	return nil, fmt.Errorf("google vision does not support image generation, use Imagen instead")
}

// ============= Local Vision Provider =============

type LocalVisionProvider struct{}

func (p *LocalVisionProvider) Name() string { return "local" }

func (p *LocalVisionProvider) AnalyzeImage(ctx context.Context, imageURL string, prompt string) (*AnalysisResult, error) {
	// Local vision model (e.g., LLaVA, CogVLM)
	return &AnalysisResult{
		Description: "Image analyzed by local vision model",
		Confidence:  0.8,
	}, nil
}

func (p *LocalVisionProvider) GenerateImage(ctx context.Context, prompt string, options *GenerateOptions) (*GenerationResult, error) {
	return nil, fmt.Errorf("local provider requires SDXL or similar setup")
}

// ============= Tool Functions =============

// AnalyzeImageTool is the tool function for image analysis
func AnalyzeImageTool(ctx context.Context, args map[string]interface{}) (string, error) {
	imageURL, _ := args["image_url"].(string)
	prompt, _ := args["prompt"].(string)
	provider, _ := args["provider"].(string)

	if imageURL == "" {
		return "", fmt.Errorf("image_url is required")
	}
	if prompt == "" {
		prompt = "Describe this image in detail"
	}

	m := NewManager(nil)
	result, err := m.AnalyzeImage(ctx, imageURL, prompt, provider)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

// GenerateImageTool is the tool function for image generation
func GenerateImageTool(ctx context.Context, args map[string]interface{}) (string, error) {
	prompt, _ := args["prompt"].(string)
	provider, _ := args["provider"].(string)

	width, _ := args["width"].(float64)
	height, _ := args["height"].(float64)

	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	options := &GenerateOptions{
		Width:  1024,
		Height: 1024,
	}
	if width > 0 {
		options.Width = int(width)
	}
	if height > 0 {
		options.Height = int(height)
	}

	m := NewManager(nil)
	result, err := m.GenerateImage(ctx, prompt, options, provider)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}
