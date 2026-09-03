package tool

import (
	"context"
	"fmt"
	"time"
)

// BrowserClickTool simulates clicking an element using real browser automation
type BrowserClickTool struct {
	bt *BrowserTools
}

func NewBrowserClickTool(bt *BrowserTools) *BrowserClickTool {
	return &BrowserClickTool{bt: bt}
}

func (t *BrowserClickTool) Name() string { return "browser_click" }

func (t *BrowserClickTool) Description() string {
	return "Click an element on the page using CSS selector. Requires browser_navigate to be called first to open a page."
}

func (t *BrowserClickTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for the element to click (e.g., '#submit-button', '.login-btn')",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional, uses default if not provided)",
			},
		},
		"required": []string{"selector"},
	}
}

func (t *BrowserClickTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	selector, ok := args["selector"].(string)
	if !ok || selector == "" {
		return nil, fmt.Errorf("selector is required")
	}

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	// Get browser manager
	bm := GetBrowserManager()

	// Ensure tab exists
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	// Perform click
	if err := bm.Click(tabID, selector); err != nil {
		return nil, fmt.Errorf("failed to click element: %w", err)
	}

	// Get updated page info
	text, _ := bm.GetPageText(tabID)
	title := ""
	if tab, ok := bm.GetTab(tabID); ok {
		title = tab.Title
	}

	return map[string]interface{}{
		"action":     "click",
		"selector":   selector,
		"success":    true,
		"page_title": title,
		"page_text":  truncateString(text, 1000),
	}, nil
}

// BrowserTypeTool simulates typing text into an input field
type BrowserTypeTool struct {
	bt *BrowserTools
}

func NewBrowserTypeTool(bt *BrowserTools) *BrowserTypeTool {
	return &BrowserTypeTool{bt: bt}
}

func (t *BrowserTypeTool) Name() string { return "browser_type" }

func (t *BrowserTypeTool) Description() string {
	return "Type text into an input field or textarea. Requires browser_navigate to be called first."
}

func (t *BrowserTypeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for the input element (e.g., '#username', 'input[name=\"email\"]')",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to type into the element",
			},
			"clear": map[string]interface{}{
				"type":        "boolean",
				"description": "Clear existing content first (default: true)",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
		"required": []string{"selector", "text"},
	}
}

func (t *BrowserTypeTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	selector, ok := args["selector"].(string)
	if !ok || selector == "" {
		return nil, fmt.Errorf("selector is required")
	}

	text, ok := args["text"].(string)
	if !ok {
		return nil, fmt.Errorf("text is required")
	}

	clear := true
	if c, ok := args["clear"].(bool); ok {
		clear = c
	}

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	// Get browser manager
	bm := GetBrowserManager()

	// Ensure tab exists
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	// Clear existing content if requested
	if clear {
		// Select all and delete
		bm.ExecuteJS(tabID, fmt.Sprintf(`
			element = document.querySelector('%s');
			if (element) {
				element.focus();
				element.select();
				element.value = '';
			}
		`, selector))
	}

	// Type text
	if err := bm.Type(tabID, selector, text); err != nil {
		return nil, fmt.Errorf("failed to type text: %w", err)
	}

	return map[string]interface{}{
		"action":   "type",
		"selector": selector,
		"text":     text,
		"cleared":  clear,
		"success":  true,
	}, nil
}

// BrowserScrollTool scrolls the page
type BrowserScrollTool struct {
	bt *BrowserTools
}

func NewBrowserScrollTool(bt *BrowserTools) *BrowserScrollTool {
	return &BrowserScrollTool{bt: bt}
}

func (t *BrowserScrollTool) Name() string { return "browser_scroll" }

func (t *BrowserScrollTool) Description() string {
	return "Scroll the page up, down, or to a specific element."
}

func (t *BrowserScrollTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"direction": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"up", "down", "top", "bottom", "to_element"},
				"description": "Scroll direction or 'to_element' to scroll to a specific element",
			},
			"amount": map[string]interface{}{
				"type":        "number",
				"description": "Number of pixels to scroll for 'up' or 'down' (default: 500)",
			},
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for element to scroll to (required when direction is 'to_element')",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
		"required": []string{"direction"},
	}
}

func (t *BrowserScrollTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	direction, ok := args["direction"].(string)
	if !ok || direction == "" {
		return nil, fmt.Errorf("direction is required")
	}

	amount := 500.0
	if a, ok := args["amount"].(float64); ok && a > 0 {
		amount = a
	}

	selector, _ := args["selector"].(string)

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	// Get browser manager
	bm := GetBrowserManager()

	// Ensure tab exists
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	var err error
	switch direction {
	case "up":
		err = bm.Scroll(tabID, 0, -int64(amount))
	case "down":
		err = bm.Scroll(tabID, 0, int64(amount))
	case "top":
		err = bm.Scroll(tabID, 0, 0)
	case "bottom":
		_, err = bm.ExecuteJS(tabID, "window.scrollTo(0, document.body.scrollHeight);")
	case "to_element":
		if selector == "" {
			return nil, fmt.Errorf("selector is required when direction is 'to_element'")
		}
		err = bm.ScrollToElement(tabID, selector)
	default:
		return nil, fmt.Errorf("invalid direction: %s", direction)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to scroll: %w", err)
	}

	return map[string]interface{}{
		"action":    "scroll",
		"direction": direction,
		"amount":    int(amount),
		"selector":  selector,
		"success":   true,
	}, nil
}

// BrowserBackTool navigates back in browser history
type BrowserBackTool struct{}

func NewBrowserBackTool() *BrowserBackTool {
	return &BrowserBackTool{}
}

func (t *BrowserBackTool) Name() string { return "browser_back" }

func (t *BrowserBackTool) Description() string {
	return "Navigate back to the previous page in browser history."
}

func (t *BrowserBackTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserBackTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	// Get browser manager
	bm := GetBrowserManager()

	// Ensure tab exists
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	// Go back
	if err := bm.Back(tabID); err != nil {
		return nil, fmt.Errorf("failed to go back: %w", err)
	}

	// Get updated page info
	text, _ := bm.GetPageText(tabID)
	title := ""
	url := ""
	if tab, ok := bm.GetTab(tabID); ok {
		title = tab.Title
		url = tab.URL
	}

	return map[string]interface{}{
		"action":     "back",
		"success":    true,
		"page_title": title,
		"page_url":   url,
		"page_text":  truncateString(text, 1000),
	}, nil
}

// BrowserConsoleTool executes JavaScript in the browser
type BrowserConsoleTool struct{}

func NewBrowserConsoleTool() *BrowserConsoleTool {
	return &BrowserConsoleTool{}
}

func (t *BrowserConsoleTool) Name() string { return "browser_console" }

func (t *BrowserConsoleTool) Description() string {
	return "Execute JavaScript code in the browser console and return the result."
}

func (t *BrowserConsoleTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"script": map[string]interface{}{
				"type":        "string",
				"description": "JavaScript code to execute",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
		"required": []string{"script"},
	}
}

func (t *BrowserConsoleTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	script, ok := args["script"].(string)
	if !ok || script == "" {
		return nil, fmt.Errorf("script is required")
	}

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	// Get browser manager
	bm := GetBrowserManager()

	// Ensure tab exists
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	// Execute script
	result, err := bm.ExecuteJS(tabID, script)
	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	return map[string]interface{}{
		"action":  "execute_js",
		"script":  script,
		"result":  result,
		"success": true,
	}, nil
}

// BrowserForwardTool navigates forward in browser history
type BrowserForwardTool struct{}

func NewBrowserForwardTool() *BrowserForwardTool {
	return &BrowserForwardTool{}
}

func (t *BrowserForwardTool) Name() string { return "browser_forward" }

func (t *BrowserForwardTool) Description() string {
	return "Navigate forward to the next page in browser history."
}

func (t *BrowserForwardTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserForwardTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	if err := bm.Forward(tabID); err != nil {
		return nil, fmt.Errorf("failed to go forward: %w", err)
	}

	text, _ := bm.GetPageText(tabID)
	title := ""
	url := ""
	if tab, ok := bm.GetTab(tabID); ok {
		title = tab.Title
		url = tab.URL
	}

	return map[string]interface{}{
		"action":     "forward",
		"success":    true,
		"page_title": title,
		"page_url":   url,
		"page_text":  truncateString(text, 1000),
	}, nil
}

// BrowserRefreshTool refreshes the current page
type BrowserRefreshTool struct{}

func NewBrowserRefreshTool() *BrowserRefreshTool {
	return &BrowserRefreshTool{}
}

func (t *BrowserRefreshTool) Name() string { return "browser_refresh" }

func (t *BrowserRefreshTool) Description() string {
	return "Refresh/reload the current page."
}

func (t *BrowserRefreshTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserRefreshTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	if err := bm.Refresh(tabID); err != nil {
		return nil, fmt.Errorf("failed to refresh page: %w", err)
	}

	return map[string]interface{}{
		"action":  "refresh",
		"success": true,
	}, nil
}

// BrowserWaitTool waits for an element or page load
type BrowserWaitTool struct{}

func NewBrowserWaitTool() *BrowserWaitTool {
	return &BrowserWaitTool{}
}

func (t *BrowserWaitTool) Name() string { return "browser_wait" }

func (t *BrowserWaitTool) Description() string {
	return "Wait for an element to appear or for the page to finish loading."
}

func (t *BrowserWaitTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector to wait for (optional, waits for page load if not provided)",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserWaitTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	selector, _ := args["selector"].(string)
	timeout := 30.0
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = t
	}

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	var err error
	if selector != "" {
		err = bm.WaitForElement(tabID, selector, time.Duration(timeout)*time.Second)
	} else {
		err = bm.WaitForLoad(tabID, time.Duration(timeout)*time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("wait timed out: %w", err)
	}

	return map[string]interface{}{
		"action":   "wait",
		"selector": selector,
		"success":  true,
	}, nil
}

// BrowserGetInfoTool gets page information
type BrowserGetInfoTool struct{}

func NewBrowserGetInfoTool() *BrowserGetInfoTool {
	return &BrowserGetInfoTool{}
}

func (t *BrowserGetInfoTool) Name() string { return "browser_get_info" }

func (t *BrowserGetInfoTool) Description() string {
	return "Get current page information including title, URL, and load state."
}

func (t *BrowserGetInfoTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserGetInfoTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	info, err := bm.GetPageInfo(tabID)
	if err != nil {
		return nil, fmt.Errorf("failed to get page info: %w", err)
	}

	return info, nil
}

// BrowserClearCacheTool clears browser cache
type BrowserClearCacheTool struct{}

func NewBrowserClearCacheTool() *BrowserClearCacheTool {
	return &BrowserClearCacheTool{}
}

func (t *BrowserClearCacheTool) Name() string { return "browser_clear_cache" }

func (t *BrowserClearCacheTool) Description() string {
	return "Clear browser localStorage and sessionStorage for the current page."
}

func (t *BrowserClearCacheTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserClearCacheTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	if err := bm.ClearCache(tabID); err != nil {
		return nil, fmt.Errorf("failed to clear cache: %w", err)
	}

	return map[string]interface{}{
		"action":  "clear_cache",
		"success": true,
	}, nil
}

// BrowserGetCookiesTool gets cookies
type BrowserGetCookiesTool struct{}

func NewBrowserGetCookiesTool() *BrowserGetCookiesTool {
	return &BrowserGetCookiesTool{}
}

func (t *BrowserGetCookiesTool) Name() string { return "browser_get_cookies" }

func (t *BrowserGetCookiesTool) Description() string {
	return "Get all cookies for the current page."
}

func (t *BrowserGetCookiesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserGetCookiesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab found. Please call browser_navigate first")
	}

	cookies, err := bm.GetCookies(tabID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cookies: %w", err)
	}

	return map[string]interface{}{
		"action":  "get_cookies",
		"count":   len(cookies),
		"cookies": cookies,
		"success": true,
	}, nil
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
