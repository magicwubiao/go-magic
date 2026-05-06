package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VisionTool 图片理解工具
type VisionTool struct {
	BaseTool
	// 可以在此添加 vision API client
}

// NewVisionTool 创建图片理解工具
func NewVisionTool() *VisionTool {
	return &VisionTool{
		BaseTool: *NewBaseTool(
			"vision_analyze",
			"Analyze images using AI vision. Supports local files, URLs, and base64 encoded images.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"image_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the image file (local) or URL",
					},
					"question": map[string]interface{}{
						"type":        "string",
						"description": "Specific question to ask about the image",
						"default":     "Describe this image in detail.",
					},
					"mode": map[string]interface{}{
						"type":        "string",
						"description": "Analysis mode: general, ocr, faces, objects, text",
						"enum":        []string{"general", "ocr", "faces", "objects", "text"},
						"default":     "general",
					},
					"detail": map[string]interface{}{
						"type":        "string",
						"description": "Detail level: low, high",
						"enum":        []string{"low", "high"},
						"default":     "high",
					},
				},
				"required": []string{"image_path"},
			},
		),
	}
}

// ValidateParams 验证参数
func (t *VisionTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 分析图片
func (t *VisionTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	imagePath, ok := params["image_path"].(string)
	if !ok || imagePath == "" {
		return nil, fmt.Errorf("image_path is required")
	}

	question := "Describe this image in detail."
	if q, ok := params["question"].(string); ok && q != "" {
		question = q
	}

	mode := "general"
	if m, ok := params["mode"].(string); ok && m != "" {
		mode = m
	}

	detail := "high"
	if d, ok := params["detail"].(string); ok && d != "" {
		detail = d
	}

	// 检查是否为 URL
	isURL := len(imagePath) > 4 && (strings.HasPrefix(imagePath, "http://") || strings.HasPrefix(imagePath, "https://"))

	if isURL {
		return t.analyzeURL(imagePath, question, mode, detail)
	}

	// 处理本地文件
	return t.analyzeLocalFile(imagePath, question, mode, detail)
}

func (t *VisionTool) analyzeURL(url, question, mode, detail string) (interface{}, error) {
	// 返回配置信息，实际调用需要 vision API
	result := map[string]interface{}{
		"status":    "configured",
		"image_url": url,
		"question":  question,
		"mode":      mode,
		"detail":    detail,
		"message":   "Vision analysis configured for URL.",
		"note":      "Configure a vision-capable model (GPT-4V, Claude 3, etc.) in config to enable analysis.",
		"supported_modes": []string{
			"general - General image description",
			"ocr - Extract text from images",
			"faces - Detect and describe faces",
			"objects - Identify objects in image",
			"text - Read and transcribe text",
		},
	}

	return result, nil
}

func (t *VisionTool) analyzeLocalFile(path, question, mode, detail string) (interface{}, error) {
	expandedPath := expandPath(path)

	info, err := os.Stat(expandedPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("image file not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to access file: %w", err)
	}

	// 检查文件大小
	if info.Size() > 20*1024*1024 { // 20MB limit
		return nil, fmt.Errorf("file too large (max 20MB)")
	}

	// 检查文件类型
	ext := strings.ToLower(filepath.Ext(path))
	validExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true,
		".gif": true, ".webp": true, ".bmp": true,
		".tiff": true, ".tif": true,
	}

	if !validExts[ext] {
		return nil, fmt.Errorf("unsupported image format: %s", ext)
	}

	// 返回配置信息
	result := map[string]interface{}{
		"status":     "configured",
		"image_path": expandedPath,
		"image_size": info.Size(),
		"question":   question,
		"mode":       mode,
		"detail":     detail,
		"message":    "Vision analysis configured for local file.",
		"note":       "Configure a vision-capable model in config to enable analysis.",
		"file_info": map[string]interface{}{
			"name": filepath.Base(path),
			"size": info.Size(),
			"ext":  ext,
		},
	}

	return result, nil
}

func expandPath(p string) string {
	if len(p) > 0 && p[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[1:])
	}
	// Handle relative paths
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		return abs
	}
	return p
}
