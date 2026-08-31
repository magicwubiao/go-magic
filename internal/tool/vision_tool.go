package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoder for image.DecodeConfig
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/util"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// VisionAnalyzeTool analyzes an image with AI vision.
// Referenced from hermes-agent's vision_analyze tool. Supports local image paths
// and URLs. When VISION_API_KEY (or OPENAI_API_KEY) is configured, performs a real
// vision analysis through an OpenAI-compatible chat completions endpoint;
// otherwise returns the prepared image path and metadata for the model to use.
type VisionAnalyzeTool struct {
	BaseTool
}

// NewVisionAnalyzeTool creates a new vision analyze tool.
func NewVisionAnalyzeTool() *VisionAnalyzeTool {
	return &VisionAnalyzeTool{
		BaseTool: *NewBaseTool(
			"vision_analyze",
			"Analyze an image with AI vision. Accepts a local file path or an http(s) URL. "+
				"If VISION_API_KEY or OPENAI_API_KEY is set, returns a real AI description of the image. "+
				"Otherwise returns the image path and metadata so a vision-capable model can inspect it.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"image_path": map[string]interface{}{
						"type":        "string",
						"description": "Local path to the image file",
					},
					"image_url": map[string]interface{}{
						"type":        "string",
						"description": "http(s) URL of the image",
					},
					"question": map[string]interface{}{
						"type":        "string",
						"description": "Question about the image content",
						"default":     "Describe this image in detail, including objects, text, and notable details.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Vision model to use (default: gpt-4o-mini, or VISION_MODEL env)",
					},
				},
			},
		),
	}
}

// Execute analyzes the image.
func (t *VisionAnalyzeTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	imagePath, _ := params["image_path"].(string)
	imageURL, _ := params["image_url"].(string)
	if imagePath == "" && imageURL == "" {
		return nil, fmt.Errorf("image_path or image_url is required")
	}

	question := "Describe this image in detail, including objects, text, and notable details."
	if q, ok := params["question"].(string); ok && q != "" {
		question = q
	}
	model, _ := params["model"].(string)

	// Resolve the local file: download URLs into the vision cache.
	localPath := imagePath
	if imageURL != "" {
		if err := validateURL(imageURL); err != nil {
			return nil, fmt.Errorf("invalid image URL: %w", err)
		}
		downloaded, err := downloadImageToCache(ctx, imageURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download image: %w", err)
		}
		localPath = downloaded
	}

	if localPath == "" {
		return nil, fmt.Errorf("could not resolve image location")
	}

	info, err := inspectImage(localPath)
	if err != nil {
		return nil, err
	}

	// Real vision analysis when an API key is available.
	apiKey := os.Getenv("VISION_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey != "" {
		description, usedModel, err := analyzeWithOpenAICompat(ctx, localPath, question, model, apiKey)
		if err != nil {
			// Fall back to returning the prepared image when the API call fails.
			info["note"] = "Vision API call failed: " + err.Error() + ". Use the saved image path with a vision-capable model."
			info["ai_analysis"] = nil
			return info, nil
		}
		info["description"] = description
		info["model"] = usedModel
		info["ai_analysis"] = true
		return info, nil
	}

	info["note"] = "Vision API not configured. Set VISION_API_KEY or OPENAI_API_KEY (optionally VISION_BASE_URL / VISION_MODEL) to enable AI analysis, or use the saved image path with a vision-capable model."
	return info, nil
}

// inspectImage returns metadata about a local image file.
func inspectImage(path string) (map[string]interface{}, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("image file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to access image: %w", err)
	}

	result := map[string]interface{}{
		"image_path": path,
		"size_bytes": info.Size(),
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer f.Close()

	if cfg, format, err := image.DecodeConfig(f); err == nil {
		result["width"] = cfg.Width
		result["height"] = cfg.Height
		result["format"] = format
	} else {
		result["format"] = strings.ToLower(filepath.Ext(path))
		result["width"] = 0
		result["height"] = 0
		result["decode_error"] = err.Error()
	}

	return result, nil
}

// downloadImageToCache downloads a remote image into the vision cache directory.
func downloadImageToCache(ctx context.Context, imageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; go-magic/1.0)")

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Guess an extension from content type or URL.
	ext := guessImageExt(resp.Header.Get("Content-Type"), imageURL)

	cacheDir := filepath.Join(config.GetMagicHome(), "vision-cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("img_%d%s", time.Now().UnixNano(), ext)
	path := filepath.Join(cacheDir, filename)
	if err := os.WriteFile(path, body, 0644); err != nil {
		return "", err
	}
	return path, nil
}

// guessImageExt picks a file extension from content type or URL.
func guessImageExt(contentType, imageURL string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	case strings.Contains(contentType, "gif"):
		return ".gif"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	}

	lower := strings.ToLower(imageURL)
	if idx := strings.LastIndex(lower, "."); idx >= 0 {
		ext := lower[idx:]
		if len(ext) <= 5 {
			switch ext {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
				return ext
			}
		}
	}
	return ".png"
}

// mimeForImage returns the MIME type for a local image based on its extension.
func mimeForImage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/png"
	}
}

// analyzeWithOpenAICompat performs a vision analysis via an OpenAI-compatible
// chat completions endpoint (data-URL image input).
func analyzeWithOpenAICompat(ctx context.Context, imagePath, question, model, apiKey string) (string, string, error) {
	baseURL := os.Getenv("VISION_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = os.Getenv("VISION_MODEL")
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	// Read and base64-encode the image.
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read image: %w", err)
	}
	dataURL := "data:" + mimeForImage(imagePath) + ";base64," + base64.StdEncoding.EncodeToString(data)

	reqBody := map[string]interface{}{
		"model": model,
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": question},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": dataURL,
						},
					},
				},
			},
		},
		"max_tokens": 1024,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(baseURL, "/")+"/chat/completions", bytes.NewReader(reqJSON))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("vision API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("vision API returned %d: %s", resp.StatusCode, truncateStr(string(respBody), 300))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", "", fmt.Errorf("failed to parse vision API response: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", "", fmt.Errorf("vision API returned empty result")
	}

	return strings.TrimSpace(parsed.Choices[0].Message.Content), model, nil
}

// truncateStr truncates a string to max runes.
func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
