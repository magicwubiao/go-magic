package tool

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoder for image.DecodeConfig
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"os"
	"path/filepath"
	"time"
)

// BrowserVisionTool takes a screenshot of the current page and saves it to disk.
// Referenced from hermes-agent's browser_vision tool: useful for CAPTCHAs, visual
// verification challenges, complex layouts, or when the text snapshot is insufficient.
type BrowserVisionTool struct {
	bt *BrowserTools
}

// NewBrowserVisionTool creates a new browser vision tool.
func NewBrowserVisionTool(bt *BrowserTools) *BrowserVisionTool {
	return &BrowserVisionTool{bt: bt}
}

// Name returns the tool name.
func (t *BrowserVisionTool) Name() string { return "browser_vision" }

// Description returns the tool description.
func (t *BrowserVisionTool) Description() string {
	return "Take a screenshot of the current page and save it to disk. " +
		"Useful for CAPTCHAs, visual verification, layout checks, or anything the text " +
		"snapshot cannot capture. Returns the local file path so a vision-capable model " +
		"can analyze the image."
}

// Schema returns the tool JSON schema.
func (t *BrowserVisionTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
			"output_dir": map[string]interface{}{
				"type":        "string",
				"description": "Directory to save the screenshot in (optional, defaults to screenshots dir under the magic home or temp)",
			},
		},
	}
}

// Execute takes and saves the screenshot.
func (t *BrowserVisionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	outputDir := ""
	if dir, ok := args["output_dir"].(string); ok && dir != "" {
		outputDir = dir
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab. Please call browser_navigate first")
	}

	buf, err := bm.Screenshot(tabID)
	if err != nil {
		return nil, fmt.Errorf("failed to take screenshot: %w", err)
	}

	// Resolve the save directory
	if outputDir == "" {
		if envDir := os.Getenv("GO_MAGIC_SCREENSHOT_DIR"); envDir != "" {
			outputDir = envDir
		} else {
			outputDir = filepath.Join(os.TempDir(), "go-magic", "screenshots")
		}
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create screenshot directory: %w", err)
	}

	filename := fmt.Sprintf("browser_%s_%s.png", tabID, time.Now().Format("20060102_150405"))
	savePath := filepath.Join(outputDir, filename)
	if err := os.WriteFile(savePath, buf, 0644); err != nil {
		return nil, fmt.Errorf("failed to save screenshot: %w", err)
	}

	// Decode image dimensions for metadata
	var width, height int
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(buf)); err == nil {
		width, height = cfg.Width, cfg.Height
	}

	info, _ := bm.GetPageInfo(tabID)
	url, _ := info["url"].(string)
	title, _ := info["title"].(string)

	return map[string]interface{}{
		"status":     "saved",
		"path":       savePath,
		"tab_id":     tabID,
		"url":        url,
		"title":      title,
		"format":     "png",
		"width":      width,
		"height":     height,
		"size_bytes": len(buf),
		"timestamp":  time.Now().Format(time.RFC3339),
		"note": "The screenshot has been saved to disk. Analyze it with a vision-capable model " +
			"if you need to understand visual content such as CAPTCHAs or layout issues.",
	}, nil
}
