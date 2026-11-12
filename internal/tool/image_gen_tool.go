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
	Provider ImageProvider `json:"provider"`
	APIKey   string        `json:"api_key"`
	BaseURL  string        `json:"base_url"`
	// Model 可选，覆盖后端默认模型。为空时各后端用 catalog 里的推荐值。
	Model           string        `json:"model"`
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
						"description": "Art style: realistic, anime, cartoon, watercolor, digital-art, photo, etc. Omit to use the configured default style (Settings > Media).",
						"enum":        []string{"realistic", "anime", "cartoon", "watercolor", "digital-art", "photo", "abstract", "impressionist", "cyberpunk", "fantasy", "oil-painting", "sketch"},
					},
					"size": map[string]interface{}{
						"type":        "string",
						"description": "Image size (aspect ratio: 1:1, 3:2, 2:3). Omit to use the configured default size (Settings > Media).",
						// enum 收敛到默认模型 GPT Image 实际接受的三个值。SD / Together
						// 等后端支持任意像素，但走 config 的 default_size（不过 enum），
						// 这里列别的值只会诱导模型传一个默认 provider 直接拒的尺寸。
						"enum": []string{"1024x1024", "1536x1024", "1024x1536"},
					},
					"count": map[string]interface{}{
						"type":        "number",
						"description": "Number of images to generate (1-4).",
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
						"description": "Override default provider: dall-e, stable-diffusion, midjourney, together, minimax",
						// 注意：这个 enum 是**内置后端**的快照，故意不跟注册表联动 ——
						// 工具 schema 一旦随插件注册变化，模型看到的能力面就不可复现了。
						// 插件贡献的 provider 走 config.json 的 image_gen.provider 指定
						// （见 media_provider.go 的边界说明），不通过这个参数选择。
						"enum": []string{"dall-e", "stable-diffusion", "midjourney", "together", "minimax"},
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

	// 检查是否指定了覆盖的提供商。未指定时用 effectiveImageProvider 兜零值，
	// 保证 &ImageGenConfig{APIKey: ...} 这种漏填 Provider 的配置也能正常工作。
	// 换到另一家内置后端时，凭据与默认域名都属于配置里那一家，见下方说明。
	configuredProvider := effectiveImageProvider(t.config)
	provider := configuredProvider
	overridden := false
	if p, ok := params["provider"].(string); ok {
		if p = strings.TrimSpace(p); p != "" {
			provider = ImageProvider(p)
			overridden = CanonicalProviderName(MediaKindImage, p) !=
				CanonicalProviderName(MediaKindImage, string(configuredProvider))
		}
	}

	// 先查后端：provider 拼错 / 插件已卸载要立刻报"不支持"，而不是误导性的"未配置"。
	// 内置 4 个在 media_backend_builtin.go 里注册；插件贡献的后端实现同一接口后
	// 就能在这里被查到，工具名、schema、落盘链路全都不动。
	backend, ok := LookupImageBackend(string(provider))
	if !ok {
		return nil, fmt.Errorf("unsupported image provider: %s (available: %s)",
			provider, strings.Join(RegisteredImageProviders(), ", "))
	}

	// 内置后端共用 image_gen 段那一份 key / base_url：运行期换名等于拿 A 的密钥
	// 打 B 的接口（再拼出别家的域名 + 路径，回来一个网关 404 页），所以直接拒绝。
	// 插件后端自带凭据，换过去可以，但要把配置里那家的 base_url / model 清掉。
	if overridden && isBuiltinImageBackend(string(provider)) {
		return map[string]interface{}{
			"status":   "not_configured",
			"provider": provider,
			"prompt":   prompt,
			"style":    style,
			"size":     size,
			"count":    count,
			"message": fmt.Sprintf(
				"Only %q is configured with credentials, so this call cannot switch to %q at runtime: "+
					"it would send the %q key to another vendor's endpoint. Retry with provider %q, "+
					"or ask the user to change the provider in Settings > Media.",
				configuredProvider, provider, configuredProvider, configuredProvider),
			"configuration_help": ConfigHelp(MediaKindImage, string(provider)),
		}, nil
	}

	genCfg := t.config
	if overridden {
		clone := *t.config
		clone.BaseURL = ""
		clone.Model = ""
		genCfg = &clone
	}

	// 可用性由后端判定，不在工具层硬判断 APIKey：内置后端要求 key，插件后端可能
	// 不需要（如本地 ComfyUI）。
	if !backend.Available(genCfg) {
		return map[string]interface{}{
			"status":             "not_configured",
			"provider":           provider,
			"prompt":             prompt,
			"style":              style,
			"size":               size,
			"count":              count,
			"message":            fmt.Sprintf("Image generation is not configured for provider %q.", provider),
			"configuration_help": ConfigHelp(MediaKindImage, string(provider)),
		}, nil
	}

	result, err := backend.Generate(ctx, genCfg, ImageRequest{
		Prompt:         prompt,
		NegativePrompt: negativePrompt,
		Style:          style,
		Size:           size,
		Count:          count,
		Seed:           seed,
	})
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
//
// 三个取数通道优先级：Data > Base64Data > URL。内置后端沿用 URL/Base64Data，
// 插件后端若已经把字节取回来，直接填 Data 即可，内核不再多一次下载。
type ImageInfo struct {
	URL        string
	Base64Data string
	Format     string
	// Data 内联字节（插件后端直接回字节时用）。
	Data []byte
	// Mime 可选 MIME 类型（image/png 等），Format 为空时用于推断扩展名。
	Mime string
}

// generateWithDALLE 使用 OpenAI GPT Image 生成图片。
//
// 函数名和 provider 标识保留 "dall-e" 是历史原因（config.json 里存的就是这个
// 值，改名会让存量配置失效），但请求契约是 GPT Image 的 —— DALL·E 2/3 已于
// 2026-05-12 停用，再用 dall-e-3 只会拿回 404。两者有三处不兼容，改动前先看：
//   - size 只认 1024x1024 / 1536x1024 / 1024x1536（见 gptImageSize）
//   - quality 是 low/medium/high，没有 hd
//   - 没有 style / seed 参数，传了会被拒
func (t *ImageGenerationTool) generateWithDALLE(ctx context.Context, prompt, negativePrompt, style, size string, count int, seed *int) (*ImageGenerationResult, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	model := infoOrDefaultModel(MediaKindImage, string(ProviderDALLE), t.config.Model)
	reqBody := map[string]interface{}{
		"model":  model,
		"prompt": t.enhancePromptWithStyle(prompt, style),
		"n":      count,
		"size":   gptImageSize(size),
	}

	// 风格已经由 enhancePromptWithStyle 写进 prompt，这里只把"要写实/摄影"
	// 翻译成高保真档位。GPT Image 没有 style 参数，旧代码传 style=vivid 会 400。
	if style == "realistic" || style == "photo" {
		reqBody["quality"] = "high"
	}

	// seed 不再传递：随机种子是 DALL·E 3 的遗留参数，GPT Image 不支持。
	_ = seed

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

	// 选择模型：配置优先。原来是"两个分支赋同一个值"的死代码（都指向 SDXL），
	// 现在默认改成 Together 主推的 FLUX.1-schnell（更快更便宜），想用 SDXL 或
	// 其他模型在设置页选即可——这也是把模型开放成配置项的实际理由。
	model := infoOrDefaultModel(MediaKindImage, string(ProviderTogether), t.config.Model)

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

// gptImageSize 把工具层/配置里的尺寸压到 GPT Image 实际接受的三档。
//
// DALL·E 时代的 1792x1024 / 1024x1792 都不再合法：GPT Image 只认
// 1024x1024、1536x1024、1024x1536。这里做"就近吸附"而不是报错——尺寸表达的
// 是画幅意图，要 16:9 就给 1536x1024 依然满足意图，而一次 400 只会让人以为
// 工具坏了。这与 media_catalog.go 里 dashscope 的时长吸附是同一种取舍。
func gptImageSize(size string) string {
	s := strings.ToLower(strings.TrimSpace(size))
	switch s {
	case "1536x1024", "1792x1024", "16:9", "3:2":
		return "1536x1024"
	case "1024x1536", "1024x1792", "9:16", "2:3":
		return "1024x1536"
	case "", "1024x1024", "1:1":
		return "1024x1024"
	}
	// 其它像素串按宽高关系判断；解析不出来就走正方形（GPT Image 的默认档）。
	var w, h int
	if n, _ := fmt.Sscanf(s, "%dx%d", &w, &h); n == 2 && w > 0 && h > 0 {
		switch {
		case w > h:
			return "1536x1024"
		case h > w:
			return "1024x1536"
		}
	}
	return "1024x1024"
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
	}
	// 白名单之外再尝试按 "宽x高" 解析：GPT Image 的 1536x1024 / 1024x1536 不在
	// 上面那组历史档位里，但 SD / Together 这类接受任意像素的后端要用它 ——
	// 一律压回 1024x1024 会让"设置页能选"变成"选了不生效"。
	var w, h int
	if n, _ := fmt.Sscanf(strings.ToLower(strings.TrimSpace(size)), "%dx%d", &w, &h); n == 2 && w > 0 && h > 0 {
		return w, h
	}
	return 1024, 1024
}

// downloadAndSaveImages 下载并保存图片
func (t *ImageGenerationTool) downloadAndSaveImages(ctx context.Context, images []ImageInfo, customPath string) ([]string, error) {
	savedPaths := make([]string, 0, len(images))

	// 解析落盘位置。相对输出目录按会话工作目录解析——桌面版进程 cwd 是
	// 安装目录，按 cwd 落盘会把图片写进 AppData 的程序目录里（真实事故）；
	// 模型给的自定义路径走 resolvePath，与 write_file 同一套沙箱规则。
	outputDir, err := ResolveMediaOutputDir(ctx, t.config.OutputDirectory, "generated_images")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve output directory: %w", err)
	}

	if customPath != "" {
		abs, perr := resolvePath(ctx, customPath)
		if perr != nil {
			return nil, fmt.Errorf("invalid output path: %w", perr)
		}
		customPath = abs
		outputDir = filepath.Dir(abs)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	for i, img := range images {
		var data []byte
		var err error

		switch {
		case len(img.Data) > 0:
			// 插件后端已经取回字节，内核只管落盘
			data = img.Data
		case img.Base64Data != "":
			// 解码 base64 数据
			data, err = base64.StdEncoding.DecodeString(img.Base64Data)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64 image %d: %w", i, err)
			}
		case img.URL != "":
			// 下载图片
			data, err = t.downloadImage(ctx, img.URL)
			if err != nil {
				return nil, fmt.Errorf("failed to download image %d: %w", i, err)
			}
		default:
			continue
		}

		// 确定文件路径
		var filePath string
		if customPath != "" && i == 0 {
			filePath = customPath
		} else {
			ext := img.Format
			if ext == "" {
				ext = extensionFromMime(img.Mime)
			}
			if ext == "" {
				ext = "png"
			}
			timestamp := time.Now().Format("20060102_150405")
			filename := fmt.Sprintf("generated_%s_%d.%s", timestamp, i, ext)
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

// maxImageDownloadBytes 单张图片的下载上限。
//
// 后端把 URL 交给内核下载，等于让第三方/插件决定我们要拉多大的东西。
// 没有上限时一个坏地址（指向大文件、或 Content-Length 撒谎）就能把内存打满。
// 32MB 对 4K PNG 都绰绰有余，超过基本可以判定不是图片。
const maxImageDownloadBytes = 32 << 20

// downloadImage 下载图片
func (t *ImageGenerationTool) downloadImage(ctx context.Context, url string) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("empty image url")
	}
	return DownloadMediaBytes(ctx, url, maxImageDownloadBytes)
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
						"description": "Output image size (1:1, 3:2, 2:3). Omit to use the backend default.",
						// 与 image_gen 用同一组值：编辑链路最终也发给 GPT Image，而它
						// 只认这三档。旧枚举里的 256x256 / 512x512 会被静默放大到
						// 1024x1024，留着只会让调用方以为尺寸生效了。
						"enum": []string{"1024x1024", "1536x1024", "1024x1536"},
					},
					"count": map[string]interface{}{
						"type":        "number",
						"description": "Number of variations to generate (1-4)",
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

// SetConfig 设置图片编辑配置（与 ImageGenerationTool 共用 image_gen 段）。
// 设置页保存后由服务端注入，避免"改了配置但编辑还在用旧 provider/key"。
func (t *ImageEditTool) SetConfig(config *ImageGenConfig) {
	if config == nil {
		return
	}
	t.config = config
}

// GetConfig 获取当前配置（与 ImageGenerationTool 对称，便于排查"改了没生效"）。
func (t *ImageEditTool) GetConfig() *ImageGenConfig { return t.config }

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

	// 编辑能力已归后端：先在注册表里找"当前 provider"，再判断它是否实现
	// ImageEditor。两步顺序不能反——provider 拼错时要报“不支持这个 provider”，
	// 而不是把用户带去查 key（与 image_gen 同一条教训）。
	provider := effectiveImageProvider(t.config)
	backend, ok := LookupImageBackend(string(provider))
	if !ok {
		return nil, fmt.Errorf("unsupported image provider: %s (available: %s)",
			provider, strings.Join(RegisteredImageProviders(), ", "))
	}

	// 先判断"能不能编辑"，再判断"配没配好"：顺序反了会把
	// "这家不支持编辑" 显示成 "未配置"，用户会去翻 key，越查越远。
	editor, ok := backend.(ImageEditor)
	if !ok {
		return nil, fmt.Errorf(
			"image editing not supported for provider: %s (editing providers: %s)",
			provider, strings.Join(RegisteredImageEditors(), ", "))
	}

	if !backend.Available(t.config) {
		return map[string]interface{}{
			"status":             "not_configured",
			"provider":           provider,
			"message":            fmt.Sprintf("Image editing is not configured for provider %q.", provider),
			"configuration_help": ConfigHelp(MediaKindImage, string(provider)),
		}, nil
	}

	// 读取源图片（相对路径按会话工作目录解析，与文件工具一致——附件被
	// 物化到 <workDir>/.magic-uploads/，模型传的就是该相对路径）
	resolvedImage, rerr := resolveLocalMedia(ctx, imagePath)
	if rerr != nil {
		return nil, rerr
	}
	imageData, err := os.ReadFile(resolvedImage)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	// 遮罩同样按会话工作目录解析：模型给的相对路径不会因为进程 cwd 变化而失效。
	if maskPath != "" {
		if resolvedMask, merr := resolveLocalMedia(ctx, maskPath); merr == nil {
			maskPath = resolvedMask
		}
	}

	result, err := editor.Edit(ctx, t.config, ImageEditRequest{
		Prompt:   prompt,
		EditType: editType,
		Size:     size,
		Count:    count,
		Image:    imageData,
		MaskPath: maskPath,
	})
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

// editImageWithDALLE 使用 DALL-E 编辑图片（包级函数：后端适配器与工具层共用，
// 不再挂在 ImageEditTool 上——编辑能力已归后端，工具只负责参数搬运与落盘）。
func editImageWithDALLE(ctx context.Context, cfg *ImageGenConfig, imageData []byte, prompt, maskPath, size string, count int, editType string) (*ImageGenerationResult, error) {
	baseURL := ""
	if cfg != nil {
		baseURL = cfg.BaseURL
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// GPT Image 只有 /images/edits 一个编辑端点：
	//   - /images/variations 是 DALL·E 2 的专属能力，GPT Image 没有，调用必 404
	//   - 变体因此改写成"同图 + 一句指令"的编辑请求，语义最接近且仍然出图
	endpoint := "/images/edits"
	if editType == "variation" && strings.TrimSpace(prompt) == "" {
		prompt = "Generate a variation of this image."
	}

	// 构建 multipart 请求
	var body bytes.Buffer
	writer := NewMultipartWriter(&body)

	// 每次编辑都必须带 model。不传时 OpenAI 会回落到已停用的 dall-e-2，
	// 请求直接失败——这是换掉 DALL·E 之后最容易漏的一处。
	configuredModel := ""
	if cfg != nil {
		configuredModel = cfg.Model
	}
	model := infoOrDefaultModel(MediaKindImage, string(ProviderDALLE), configuredModel)
	if err := writer.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("failed to add model: %w", err)
	}

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

	// 添加提示词（变体也带，见上面把空 prompt 改写成一句话）
	if err := writer.WriteField("prompt", prompt); err != nil {
		return nil, fmt.Errorf("failed to add prompt: %w", err)
	}

	// 添加其他参数
	if err := writer.WriteField("n", fmt.Sprintf("%d", count)); err != nil {
		return nil, err
	}
	if err := writer.WriteField("size", gptImageSize(size)); err != nil {
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
	apiKey := ""
	if cfg != nil {
		apiKey = cfg.APIKey
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := util.GetHTTPClient()
	if cfg != nil && cfg.Timeout > 0 {
		client = &http.Client{Timeout: cfg.Timeout}
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
