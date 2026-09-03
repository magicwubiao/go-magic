package tool

import (
	"context"
	"fmt"
)

// BrowserPressTool presses keyboard keys in the current browser page.
// Referenced from hermes-agent's browser_press tool: submitting forms (Enter),
// navigating (Tab), keyboard shortcuts, etc.
type BrowserPressTool struct {
	bt *BrowserTools
}

// NewBrowserPressTool creates a new browser press tool.
func NewBrowserPressTool(bt *BrowserTools) *BrowserPressTool {
	return &BrowserPressTool{bt: bt}
}

// Name returns the tool name.
func (t *BrowserPressTool) Name() string { return "browser_press" }

// Description returns the tool description.
func (t *BrowserPressTool) Description() string {
	return "Press a keyboard key or type text in the current page. Supports named keys: " +
		"Enter, Tab, Escape, Backspace, Delete, ArrowUp/Down/Left/Right, Home, End, PageUp, " +
		"PageDown, Space, F1-F12, and modifier keys (Control, Shift, Alt, Meta). " +
		"Use for form submission, navigation, and keyboard shortcuts. " +
		"Any other value is typed as literal text."
}

// Schema returns the tool JSON schema.
func (t *BrowserPressTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"key": map[string]interface{}{
				"type":        "string",
				"description": "Key to press (e.g. 'Enter', 'Tab', 'Escape', 'ArrowDown') or text to type",
			},
			"times": map[string]interface{}{
				"type":        "integer",
				"description": "Number of times to press the key (default: 1, max: 100)",
				"default":     1,
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
		"required": []string{"key"},
	}
}

// Execute presses the key.
func (t *BrowserPressTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	key, ok := args["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("key is required")
	}

	times := 1
	if n, ok := args["times"].(float64); ok && n > 0 {
		times = int(n)
	}

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab. Please call browser_navigate first")
	}

	if err := bm.PressKey(tabID, key, times); err != nil {
		return nil, err
	}

	info, _ := bm.GetPageInfo(tabID)

	return map[string]interface{}{
		"key":        key,
		"times":      times,
		"tab_id":     tabID,
		"status":     "pressed",
		"page_url":   info["url"],
		"page_title": info["title"],
	}, nil
}
