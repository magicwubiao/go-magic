package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/util"
)

// ImageProvider 图片生成提供商类型
type ImageProvider string

const (
	// ProviderDALLE DALL-E 提供商
	ProviderDALLE ImageProvider = "dall-e"
	// ProviderStableDiffusion Stable Diffusion 提供商
	ProviderStableDiffusion ImageProvider = "stable-diffusion"
	// ProviderMidjourney Midjourney 提供商
	ProviderMidjourney ImageProvider = "midjourney"
	// ProviderTogether Together AI 提供商
	ProviderTogether ImageProvider = "together"
)

// ImageGenerationTool 图片生成工具
type ImageGenerationTool struct {
	BaseTool
	config *ImageGenConfig
}

// ImageGenConfig 图片生成配置
type ImageGenConfig struct {
	Provider        ImageProvider `json:"provider"`
	APIKey          string        `json:"api_key"`
	BaseURL         string        `json:"base_url"`
	DefaultSize     string        `json:"default_size"`
	DefaultStyle    string        `json:"default_style"`
	OutputDirectory string        `json:"output_directory"`
	Timeout         time.Duration `json:"timeout"`
}

// DefaultImageGenConfig 返回默认图片生成配置
func DefaultImageGenConfig() *ImageGenConfig {
	return &ImageGenConfig{
		Provider:        ProviderDALLE,
		DefaultSize:     "1024x1024",
		DefaultStyle:    "digital-art",
		OutputDirectory: "./generated_images",
		Timeout:         120 * time.Second,
	}
}

// NewImageGenerationTool 创建图片生成工具
func NewImageGenerationTool(config *ImageGenConfig) *ImageGenerationTool {
	if config == nil {
		config = DefaultImageGenConfig()
	}

	return &ImageGenerationTool{
		BaseTool: *NewBaseTool(
			"image_gen",
			"Generate images from text descriptions using AI. Supports multiple providers (DALL-E, Stable Diffusion, Midjourney). Creates high-quality images based on detailed prompts with various styles and sizes.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Detailed description of the image to generate. Be specific about style, content, colors, and composition.",
					},
					"negative_prompt": map[string]interface{}{
						"type":        "string",
						"description": "Things to avoid in the image (optional)",
					},
					"style": map[string]interface{}{
						"type":        "string",
						"description": "Art style: realistic, anime, cartoon, watercolor, digital-art, photo, etc.",
						"enum":        []string{"realistic", "anime", "cartoon", "watercolor", "digital-art", "photo", "abstract", "impressionist", "cyberpunk", "fantasy", "oil-painting", "sketch"},
						"default":     "digital-art",
					},
					"size": map[string]interface{}{
						"type":        "string",
						"description": "Image size: 256x256, 512x512, 1024x1024, 1024x576, 576x1024, 1792x1024, 1024x1792",
						"enum":        []string{"256x256", "512x512", "1024x1024", "1024x576", "576x1024", "1792x1024", "1024x1792"},
						"default":     "1024x1024",
					},
					"count": map[string]interface{}{
						"type":        "number",
						"description": "Number of images to generate (1-4)",
						"default":     1,
						"minimum":     1,
						"maximum":     4,
					},
					"seed": map[string]interface{}{
						"type":        "number",
						"description": "Random seed for reproducibility (optional)",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Custom output path for the generated image (optional)",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Override default provider: dall-e, stable-diffusion, midjourney, together",
						"enum":        []string{"dall-e", "stable-diffusion", "midjourney", "together"},
					},
				},
				"required": []string{"prompt"},
			},
		),
		config: config,
	}
}

// ValidateParams 验证参数
func (t *ImageGenerationTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 执行图片生成
func (t *ImageGenerationTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	// 获取可选参数
	negativePrompt := ""
	if np, ok := params["negative_prompt"].(string); ok {
		negativePrompt = np
	}

	style := t.config.DefaultStyle
	if s, ok := params["style"].(string); ok && s != "" {
		style = s
	}

	size := t.config.DefaultSize
	if sz, ok := params["size"].(string); ok && sz != "" {
		size = sz
	}

	count := 1
	if c, ok := params["count"].(float64); ok {
		count = int(c)
		if count < 1 {
			count = 1
		}
		if count > 4 {
			count = 4
		}
	}

	var seed *int
	if s, ok := params["seed"].(float64); ok {
		seedVal := int(s)
		seed = &seedVal
	}

	outputPath := ""
	if op, ok := params["output_path"].(string); ok {
		outputPath = op
	}

	// 检查是否指定了覆盖的提供商
	provider := t.config.Provider
	if p, ok := params["provider"].(string); ok && p != "" {
		provider = ImageProvider(p)
	}

	// 检查 API 密钥是否配置
	if t.config.APIKey == "" {
		return map[string]interface{}{
			"status":             "not_configured",
			"prompt":             prompt,
			"style":              style,
			"size":               size,
			"count":              count,
			"message":            "Image generation API key not configured. Please set image_gen_api_key in your config.",
			"configuration_help": "Add 'image_gen_api_key' and optionally 'image_gen_provider' to your config.json (the magic home directory)",
		}, nil
	}

	// 根据提供商执行生成
	var result *ImageGenerationResult
	var err error

	switch provider {
	case ProviderDALLE:
		result, err = t.generateWithDALLE(ctx, prompt, negativePrompt, style, size, count, seed)
	case ProviderStableDiffusion:
		result, err = t.generateWithStableDiffusion(ctx, prompt, negativePrompt, style, size, count, seed)
	case ProviderMidjourney:
		result, err = t.generateWithMidjourney(ctx, prompt, negativePrompt, style, size, count, seed)
	case ProviderTogether:
		result, err = t.generateWithTogether(ctx, prompt, negativePrompt, style, size, count, seed)
	default:
		return nil, fmt.Errorf("unsupported image provider: %s", provider)
	}

	if err != nil {
		return nil, fmt.Errorf("image generation failed: %w", err)
	}

	// 下载并保存图片
	savedPaths, err := t.downloadAndSaveImages(ctx, result.Images, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to save images: %w", err)
	}

	return map[string]interface{}{
		"status":          "success",
		"provider":        provider,
		"prompt":          prompt,
		"negative_prompt": negativePrompt,
		"style":           style,
		"size":            size,
		"count":           len(result.Images),
		"images":          savedPaths,
		"revised_prompts": result.RevisedPrompts,
		"seed":            result.Seed,
	}, nil
}

// ImageGenerationResult 图片生成结果
type ImageGenerationResult struct {
	Images         []ImageInfo
	RevisedPrompts []string
	Seed           int
}

// ImageInfo 图片信息
type ImageInfo struct {
	URL        string
	Base64Data string
	Format     string
}

// generateWithDALLE 使用 DALL-E 生成图片
func (t *ImageGenerationTool) generateWithDALLE(ctx context.Context, prompt, negativePrompt, style, size string, count int, seed *int) (*ImageGenerationResult, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// 构建请求体
	reqBody := map[string]interface{}{
		"model":  "dall-e-3",
		"prompt": t.enhancePromptWithStyle(prompt, style),
		"n":      count,
		"size":   size,
	}

	// DALL-E 3 支持 quality 和 style 参数
	if style == "realistic" || style == "photo" {
		reqBody["quality"] = "hd"
	}
	if style == "vivid" || style == "natural" {
		reqBody["style"] = style
	}

	if seed != nil {
		reqBody["seed"] = *seed
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/images/generations", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	client := util.GetHTTPClient()
	if t.config.Timeout > 0 {
		client = &http.Client{Timeout: t.config.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var dalleResp struct {
		Data []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Created int `json:"created"`
	}

	if err := json.Unmarshal(body, &dalleResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := &ImageGenerationResult{
		Images:         make([]ImageInfo, 0, len(dalleResp.Data)),
		RevisedPrompts: make([]string, 0, len(dalleResp.Data)),
	}

	for _, item := range dalleResp.Data {
		img := ImageInfo{
			URL:        item.URL,
			Base64Data: item.B64JSON,
			Format:     "png",
		}
		result.Images = append(result.Images, img)
		if item.RevisedPrompt != "" {
			result.RevisedPrompts = append(result.RevisedPrompts, item.RevisedPrompt)
		}
	}

	return result, nil
}

// generateWithStableDiffusion 使用 Stable Diffusion 生成图片
func (t *ImageGenerationTool) generateWithStableDiffusion(ctx context.Context, prompt, negativePrompt, style, size string, count int, seed *int) (*ImageGenerationResult, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.stability.ai/v2beta"
	}

	// 解析尺寸
	width, height := parseSize(size)

	// 构建请求体
	reqBody := map[string]interface{}{
		"prompt":          t.enhancePromptWithStyle(prompt, style),
		"negative_prompt": negativePrompt,
		"width":           width,
		"height":          height,
		"samples":         count,
		"steps":           30,
		"cfg_scale":       7,
	}

	if seed != nil {
		reqBody["seed"] = *seed
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/stable-image/generate/sd3", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	client := util.GetHTTPClient()
	if t.config.Timeout > 0 {
		client = &http.Client{Timeout: t.config.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	// Stability AI 返回的是 base64 编码的图片
	var sdResp struct {
		Images []string `json:"images"`
		Seed   int      `json:"seed"`
	}

	if err := json.Unmarshal(body, &sdResp); err != nil {
		// 尝试直接解析为单个 base64 图片
		result := &ImageGenerationResult{
			Images: []ImageInfo{{Base64Data: string(body), Format: "png"}},
		}
		if seed != nil {
			result.Seed = *seed
		}
		return result, nil
	}

	result := &ImageGenerationResult{
		Images: make([]ImageInfo, 0, len(sdResp.Images)),
		Seed:   sdResp.Seed,
	}

	for _, imgData := range sdResp.Images {
		result.Images = append(result.Images, ImageInfo{
			Base64Data: imgData,
			Format:     "png",
		})
	}

	return result, nil
}

// generateWithMidjourney 使用 Midjourney API 生成图片
func (t *ImageGenerationTool) generateWithMidjourney(ctx context.Context, prompt, negativePrompt, style, size string, count int, seed *int) (*ImageGenerationResult, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		return nil, fmt.Errorf("Midjourney API base URL must be configured")
	}

	// 构建增强的提示词
	enhancedPrompt := t.enhancePromptWithStyle(prompt, style)
	if negativePrompt != "" {
		enhancedPrompt += " --no " + negativePrompt
	}

	// 添加尺寸参数
	if size == "1024x1024" {
		enhancedPrompt += " --ar 1:1"
	} else if size == "1024x576" {
		enhancedPrompt += " --ar 16:9"
	} else if size == "576x1024" {
		enhancedPrompt += " --ar 9:16"
	}

	if seed != nil {
		enhancedPrompt += fmt.Sprintf(" --seed %d", *seed)
	}

	// 提交生成任务
	reqBody := map[string]interface{}{
		"prompt":      enhancedPrompt,
		"notify_hook": "",
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/trigger/imagine", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	client := util.GetHTTPClient()
	if t.config.Timeout > 0 {
		client = &http.Client{Timeout: t.config.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var mjResp struct {
		Code        int    `json:"code"`
		Description string `json:"description"`
		Result      string `json:"result"`
	}

	if err := json.Unmarshal(body, &mjResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if mjResp.Code != 1 && mjResp.Code != 0 {
		return nil, fmt.Errorf("Midjourney API error: %s", mjResp.Description)
	}

	// Midjourney 是异步的，返回任务信息
	return &ImageGenerationResult{
		Images: []ImageInfo{
			{URL: "", Format: "pending", Base64Data: fmt.Sprintf("task_id:%s", mjResp.Result)},
		},
		RevisedPrompts: []string{enhancedPrompt},
	}, nil
}

// generateWithTogether 使用 Together AI 生成图片
func (t *ImageGenerationTool) generateWithTogether(ctx context.Context, prompt, negativePrompt, style, size string, count int, seed *int) (*ImageGenerationResult, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.together.xyz/v1"
	}

	// 解析尺寸
	width, height := parseSize(size)

	// 选择模型
	model := "stabilityai/stable-diffusion-xl-base-1.0"
	if style == "realistic" || style == "photo" {
		model = "stabilityai/stable-diffusion-xl-base-1.0"
	}

	// 构建请求体
	reqBody := map[string]interface{}{
		"model":           model,
		"prompt":          t.enhancePromptWithStyle(prompt, style),
		"negative_prompt": negativePrompt,
		"width":           width,
		"height":          height,
		"steps":           20,
		"n":               count,
		"response_format": "b64_json",
	}

	if seed != nil {
		reqBody["seed"] = *seed
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/images/generations", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	client := util.GetHTTPClient()
	if t.config.Timeout > 0 {
		client = &http.Client{Timeout: t.config.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var togetherResp struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &togetherResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := &ImageGenerationResult{
		Images: make([]ImageInfo, 0, len(togetherResp.Data)),
	}

	for _, item := range togetherResp.Data {
		img := ImageInfo{
			URL:        item.URL,
			Base64Data: item.B64JSON,
			Format:     "png",
		}
		result.Images = append(result.Images, img)
	}

	return result, nil
}

// enhancePromptWithStyle 根据风格增强提示词
func (t *ImageGenerationTool) enhancePromptWithStyle(prompt, style string) string {
	styleModifiers := map[string]string{
		"realistic":     "photorealistic, highly detailed, 8k resolution, professional photography",
		"anime":         "anime style, manga art, vibrant colors, cel shaded",
		"cartoon":       "cartoon style, colorful, playful, animated",
		"watercolor":    "watercolor painting, soft colors, artistic, flowing",
		"digital-art":   "digital art, highly detailed, vibrant, concept art",
		"photo":         "professional photography, realistic, high quality, DSLR",
		"abstract":      "abstract art, geometric shapes, modern art, colorful",
		"impressionist": "impressionist painting, soft brushstrokes, artistic, 19th century style",
		"cyberpunk":     "cyberpunk style, neon lights, futuristic, high tech, dystopian",
		"fantasy":       "fantasy art, magical, ethereal, detailed, imaginative",
		"oil-painting":  "oil painting, classical art, rich colors, textured",
		"sketch":        "pencil sketch, line art, monochrome, hand drawn",
	}

	if modifier, ok := styleModifiers[style]; ok {
		return prompt + ", " + modifier
	}
	return prompt
}

// parseSize 解析尺寸字符串
func parseSize(size string) (width, height int) {
	switch size {
	case "256x256":
		return 256, 256
	case "512x512":
		return 512, 512
	case "1024x1024":
		return 1024, 1024
	case "1024x576":
		return 1024, 576
	case "576x1024":
		return 576, 1024
	case "1792x1024":
		return 1792, 1024
	case "1024x1792":
		return 1024, 1792
	default:
		return 1024, 1024
	}
}

// downloadAndSaveImages 下载并保存图片
func (t *ImageGenerationTool) downloadAndSaveImages(ctx context.Context, images []ImageInfo, customPath string) ([]string, error) {
	savedPaths := make([]string, 0, len(images))

	// 确保输出目录存在
	outputDir := t.config.OutputDirectory
	if outputDir == "" {
		outputDir = "./generated_images"
	}

	if customPath != "" {
		// 如果指定了自定义路径，使用其目录
		outputDir = filepath.Dir(customPath)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	for i, img := range images {
		var data []byte
		var err error

		if img.Base64Data != "" {
			// 解码 base64 数据
			data, err = base64.StdEncoding.DecodeString(img.Base64Data)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64 image %d: %w", i, err)
			}
		} else if img.URL != "" {
			// 下载图片
			data, err = t.downloadImage(ctx, img.URL)
			if err != nil {
				return nil, fmt.Errorf("failed to download image %d: %w", i, err)
			}
		} else {
			continue
		}

		// 确定文件路径
		var filePath string
		if customPath != "" && i == 0 {
			filePath = customPath
		} else {
			timestamp := time.Now().Format("20060102_150405")
			filename := fmt.Sprintf("generated_%s_%d.%s", timestamp, i, img.Format)
			if img.Format == "" {
				filename = fmt.Sprintf("generated_%s_%d.png", timestamp, i)
			}
			filePath = filepath.Join(outputDir, filename)
		}

		// 保存文件
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return nil, fmt.Errorf("failed to save image %d: %w", i, err)
		}

		savedPaths = append(savedPaths, filePath)
	}

	return savedPaths, nil
}

// downloadImage 下载图片
func (t *ImageGenerationTool) downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// SetConfig 设置图片生成配置
func (t *ImageGenerationTool) SetConfig(config *ImageGenConfig) {
	t.config = config
}

// GetConfig 获取当前配置
func (t *ImageGenerationTool) GetConfig() *ImageGenConfig {
	return t.config
}

// ImageEditTool 图片编辑工具
type ImageEditTool struct {
	BaseTool
	config *ImageGenConfig
}

// NewImageEditTool 创建图片编辑工具
func NewImageEditTool(config *ImageGenConfig) *ImageEditTool {
	if config == nil {
		config = DefaultImageGenConfig()
	}

	return &ImageEditTool{
		BaseTool: *NewBaseTool(
			"image_edit",
			"Edit existing images using AI. Supports inpainting (edit specific areas), outpainting (extend images), and variations. Works with DALL-E and similar models.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"image_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the source image file",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Description of the desired edit or variation",
					},
					"mask_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to mask image for inpainting (optional, white areas will be edited)",
					},
					"edit_type": map[string]interface{}{
						"type":        "string",
						"description": "Type of edit: inpaint, outpaint, variation",
						"enum":        []string{"inpaint", "outpaint", "variation"},
						"default":     "variation",
					},
					"size": map[string]interface{}{
						"type":        "string",
						"description": "Output image size",
						"enum":        []string{"256x256", "512x512", "1024x1024"},
						"default":     "1024x1024",
					},
					"count": map[string]interface{}{
						"type":        "number",
						"description": "Number of variations to generate (1-4)",
						"default":     1,
						"minimum":     1,
						"maximum":     4,
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Custom output path (optional)",
					},
				},
				"required": []string{"image_path", "prompt"},
			},
		),
		config: config,
	}
}

// ValidateParams 验证参数
func (t *ImageEditTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 执行图片编辑
func (t *ImageEditTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	imagePath, _ := params["image_path"].(string)
	if imagePath == "" {
		return nil, fmt.Errorf("image_path is required")
	}

	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	editType := "variation"
	if et, ok := params["edit_type"].(string); ok && et != "" {
		editType = et
	}

	size := "1024x1024"
	if sz, ok := params["size"].(string); ok && sz != "" {
		size = sz
	}

	count := 1
	if c, ok := params["count"].(float64); ok {
		count = int(c)
		if count < 1 {
			count = 1
		}
		if count > 4 {
			count = 4
		}
	}

	maskPath := ""
	if mp, ok := params["mask_path"].(string); ok {
		maskPath = mp
	}

	outputPath := ""
	if op, ok := params["output_path"].(string); ok {
		outputPath = op
	}

	// 检查 API 密钥
	if t.config.APIKey == "" {
		return map[string]interface{}{
			"status":  "not_configured",
			"message": "Image editing API key not configured",
		}, nil
	}

	// 读取源图片
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	var result *ImageGenerationResult

	switch t.config.Provider {
	case ProviderDALLE:
		result, err = t.editWithDALLE(ctx, imageData, prompt, maskPath, size, count, editType)
	default:
		return nil, fmt.Errorf("image editing not supported for provider: %s", t.config.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("image editing failed: %w", err)
	}

	// 保存结果
	savedPaths, err := t.downloadAndSaveImages(ctx, result.Images, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to save edited images: %w", err)
	}

	return map[string]interface{}{
		"status":    "success",
		"edit_type": editType,
		"prompt":    prompt,
		"source":    imagePath,
		"images":    savedPaths,
		"count":     len(savedPaths),
	}, nil
}

// editWithDALLE 使用 DALL-E 编辑图片
func (t *ImageEditTool) editWithDALLE(ctx context.Context, imageData []byte, prompt, maskPath, size string, count int, editType string) (*ImageGenerationResult, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// 根据编辑类型选择端点
	endpoint := "/images/edits"
	if editType == "variation" {
		endpoint = "/images/variations"
	}

	// 构建 multipart 请求
	var body bytes.Buffer
	writer := NewMultipartWriter(&body)

	// 添加图片
	if err := writer.WriteFile("image", "image.png", imageData); err != nil {
		return nil, fmt.Errorf("failed to add image to request: %w", err)
	}

	// 添加遮罩（如果是编辑）
	if maskPath != "" && editType != "variation" {
		maskData, err := os.ReadFile(maskPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read mask file: %w", err)
		}
		if err := writer.WriteFile("mask", "mask.png", maskData); err != nil {
			return nil, fmt.Errorf("failed to add mask to request: %w", err)
		}
	}

	// 添加提示词（如果是编辑）
	if editType != "variation" {
		if err := writer.WriteField("prompt", prompt); err != nil {
			return nil, fmt.Errorf("failed to add prompt: %w", err)
		}
	}

	// 添加其他参数
	if err := writer.WriteField("n", fmt.Sprintf("%d", count)); err != nil {
		return nil, err
	}
	if err := writer.WriteField("size", size); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	client := util.GetHTTPClient()
	if t.config.Timeout > 0 {
		client = &http.Client{Timeout: t.config.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var dalleResp struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &dalleResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := &ImageGenerationResult{
		Images: make([]ImageInfo, 0, len(dalleResp.Data)),
	}

	for _, item := range dalleResp.Data {
		result.Images = append(result.Images, ImageInfo{
			URL:        item.URL,
			Base64Data: item.B64JSON,
			Format:     "png",
		})
	}

	return result, nil
}

// downloadAndSaveImages 下载并保存图片（ImageEditTool 复用）
func (t *ImageEditTool) downloadAndSaveImages(ctx context.Context, images []ImageInfo, customPath string) ([]string, error) {
	tool := &ImageGenerationTool{config: t.config}
	return tool.downloadAndSaveImages(ctx, images, customPath)
}

// MultipartWriter multipart 写入器辅助结构
type MultipartWriter struct {
	boundary string
	writer   *bytes.Buffer
	files    []filePart
	fields   map[string]string
}

type filePart struct {
	fieldname string
	filename  string
	data      []byte
}

// NewMultipartWriter 创建 multipart 写入器
func NewMultipartWriter(buffer *bytes.Buffer) *MultipartWriter {
	return &MultipartWriter{
		boundary: fmt.Sprintf("----FormBoundary%d", time.Now().UnixNano()),
		writer:   buffer,
		fields:   make(map[string]string),
	}
}

// WriteField 写入表单字段
func (m *MultipartWriter) WriteField(fieldname, value string) error {
	m.fields[fieldname] = value
	return nil
}

// WriteFile 写入文件
func (m *MultipartWriter) WriteFile(fieldname, filename string, data []byte) error {
	m.files = append(m.files, filePart{
		fieldname: fieldname,
		filename:  filename,
		data:      data,
	})
	return nil
}

// FormDataContentType 返回 Content-Type
func (m *MultipartWriter) FormDataContentType() string {
	return "multipart/form-data; boundary=" + m.boundary
}

// Close 完成 multipart 写入
func (m *MultipartWriter) Close() error {
	// 写入文件
	for _, file := range m.files {
		fmt.Fprintf(m.writer, "--%s\r\n", m.boundary)
		fmt.Fprintf(m.writer, "Content-Disposition: form-data; name=\"%s\"; filename=\"%s\"\r\n", file.fieldname, file.filename)
		fmt.Fprint(m.writer, "Content-Type: image/png\r\n\r\n")
		m.writer.Write(file.data)
		fmt.Fprint(m.writer, "\r\n")
	}

	// 写入字段
	for name, value := range m.fields {
		fmt.Fprintf(m.writer, "--%s\r\n", m.boundary)
		fmt.Fprintf(m.writer, "Content-Disposition: form-data; name=\"%s\"\r\n\r\n", name)
		fmt.Fprint(m.writer, value)
		fmt.Fprint(m.writer, "\r\n")
	}

	// 结束边界
	fmt.Fprintf(m.writer, "--%s--\r\n", m.boundary)
	return nil
}

// LoadImageGenConfigFromEnv 从环境变量加载图片生成配置
func LoadImageGenConfigFromEnv() *ImageGenConfig {
	config := DefaultImageGenConfig()

	if apiKey := os.Getenv("IMAGE_GEN_API_KEY"); apiKey != "" {
		config.APIKey = apiKey
	}

	if provider := os.Getenv("IMAGE_GEN_PROVIDER"); provider != "" {
		config.Provider = ImageProvider(provider)
	}

	if baseURL := os.Getenv("IMAGE_GEN_BASE_URL"); baseURL != "" {
		config.BaseURL = baseURL
	}

	if outputDir := os.Getenv("IMAGE_GEN_OUTPUT_DIR"); outputDir != "" {
		config.OutputDirectory = outputDir
	}

	return config
}

// LoadImageGenConfigFromMap 从配置映射加载图片生成配置
func LoadImageGenConfigFromMap(cfg map[string]interface{}) *ImageGenConfig {
	config := DefaultImageGenConfig()

	if apiKey, ok := cfg["image_gen_api_key"].(string); ok && apiKey != "" {
		config.APIKey = apiKey
	}

	if provider, ok := cfg["image_gen_provider"].(string); ok && provider != "" {
		config.Provider = ImageProvider(provider)
	}

	if baseURL, ok := cfg["image_gen_base_url"].(string); ok && baseURL != "" {
		config.BaseURL = baseURL
	}

	if outputDir, ok := cfg["image_gen_output_dir"].(string); ok && outputDir != "" {
		config.OutputDirectory = outputDir
	}

	if style, ok := cfg["image_gen_default_style"].(string); ok && style != "" {
		config.DefaultStyle = style
	}

	if size, ok := cfg["image_gen_default_size"].(string); ok && size != "" {
		config.DefaultSize = size
	}

	return config
}

// init 确保在包初始化时检查环境变量
func init() {
	// 这个 init 函数会在包被导入时执行
	// 实际的环境变量加载会在创建工具时进行
}

// EnsureString 确保值是字符串类型
func EnsureString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if s, ok := v.(fmt.Stringer); ok {
		return s.String()
	}
	return ""
}

// StringPtr 返回字符串指针
func StringPtr(s string) *string {
	return &s
}

// IntPtr 返回整数指针
func IntPtr(i int) *int {
	return &i
}

// ContainsString 检查字符串切片是否包含指定字符串
func ContainsString(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}
