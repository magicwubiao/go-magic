package tool

import (
	"context"
	"fmt"
)

// ImageGenerationTool 图片生成工具
type ImageGenerationTool struct {
	BaseTool
}

// NewImageGenerationTool 创建图片生成工具
func NewImageGenerationTool() *ImageGenerationTool {
	return &ImageGenerationTool{
		BaseTool: *NewBaseTool(
			"image_gen",
			"Generate images from text descriptions using AI. Creates high-quality images based on detailed prompts.",
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
						"enum":        []string{"realistic", "anime", "cartoon", "watercolor", "digital-art", "photo", "abstract", "impressionist", "cyberpunk", "fantasy"},
					},
					"size": map[string]interface{}{
						"type":        "string",
						"description": "Image size: 256x256, 512x512, 1024x1024, or custom",
						"enum":        []string{"256x256", "512x512", "1024x1024", "1024x576", "576x1024"},
						"default":     "1024x1024",
					},
					"count": map[string]interface{}{
						"type":        "number",
						"description": "Number of images to generate (1-4)",
						"default":     1,
					},
					"seed": map[string]interface{}{
						"type":        "number",
						"description": "Random seed for reproducibility (optional)",
					},
				},
				"required": []string{"prompt"},
			},
		),
	}
}

// ValidateParams 验证参数
func (t *ImageGenerationTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 生成图片
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

	style := ""
	if s, ok := params["style"].(string); ok {
		style = s
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

	// 返回生成参数（实际实现需要调用图像生成 API）
	result := map[string]interface{}{
		"status":          "configured",
		"prompt":          prompt,
		"negative_prompt": negativePrompt,
		"style":           style,
		"size":            size,
		"count":           count,
		"message":         "Image generation configured. Set up image_gen_api_key in config to enable actual generation.",
		"usage_hint":      "Configure your image generation API (e.g., DALL-E, Stable Diffusion) in ~/.magic/config.yaml",
	}

	return result, nil
}
