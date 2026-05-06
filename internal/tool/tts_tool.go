package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TTSTool 文字转语音工具
type TTSTool struct {
	BaseTool
}

// NewTTSTool 创建 TTS 工具
func NewTTSTool() *TTSTool {
	return &TTSTool{
		BaseTool: *NewBaseTool(
			"tts",
			"Convert text to speech. Generate audio from text with customizable voice and language.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to convert to speech",
					},
					"voice": map[string]interface{}{
						"type":        "string",
						"description": "Voice name or ID",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "Language code (e.g., en, zh, ja)",
						"default":     "en",
					},
					"speed": map[string]interface{}{
						"type":        "number",
						"description": "Speech speed (0.5 - 2.0)",
						"default":     1.0,
					},
					"pitch": map[string]interface{}{
						"type":        "number",
						"description": "Speech pitch adjustment (-10 to 10)",
						"default":     0,
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Output file path (default: ~/tts_output.mp3)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Audio format: mp3, wav, ogg",
						"enum":        []string{"mp3", "wav", "ogg"},
						"default":     "mp3",
					},
				},
				"required": []string{"text"},
			},
		),
	}
}

// ValidateParams 验证参数
func (t *TTSTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 执行 TTS
func (t *TTSTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	text, ok := params["text"].(string)
	if !ok || text == "" {
		return nil, fmt.Errorf("text is required")
	}

	// 获取可选参数
	voice := "default"
	if v, ok := params["voice"].(string); ok && v != "" {
		voice = v
	}

	language := "en"
	if l, ok := params["language"].(string); ok && l != "" {
		language = l
	}

	speed := 1.0
	if s, ok := params["speed"].(float64); ok {
		speed = s
		if speed < 0.5 {
			speed = 0.5
		}
		if speed > 2.0 {
			speed = 2.0
		}
	}

	pitch := 0
	if p, ok := params["pitch"].(float64); ok {
		pitch = int(p)
		if pitch < -10 {
			pitch = -10
		}
		if pitch > 10 {
			pitch = 10
		}
	}

	outputPath := ""
	if op, ok := params["output_path"].(string); ok && op != "" {
		outputPath = op
	} else {
		// 默认输出路径
		home, _ := os.UserHomeDir()
		timestamp := fmt.Sprintf("%d", len(text)*1000) // rough hash
		outputPath = filepath.Join(home, "tts_output_"+timestamp+".mp3")
	}

	format := "mp3"
	if f, ok := params["format"].(string); ok && f != "" {
		format = f
	}

	// 验证输出路径
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, fmt.Errorf("invalid output path: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// 返回配置信息（实际实现需要 TTS API）
	result := map[string]interface{}{
		"status":      "configured",
		"text_length": len(text),
		"word_count":  len(strings.Fields(text)),
		"voice":       voice,
		"language":    language,
		"speed":       speed,
		"pitch":       pitch,
		"output_path": absPath,
		"format":      format,
		"message":     "TTS configured. Set up tts_api_key in config to enable actual speech generation.",
		"available_voices": map[string][]string{
			"en": {"default", "male_1", "female_1", "professional", "friendly"},
			"zh": {"default", "male_zh", "female_zh"},
			"ja": {"default", "male_ja", "female_ja"},
		},
		"usage_hint": "Configure your TTS API (e.g., Google TTS, Azure Speech) in ~/.magic/config.yaml",
	}

	return result, nil
}
