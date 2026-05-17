package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/magicwubiao/go-magic/internal/util"

	"github.com/PuerkitoBio/goquery"
)

// BrowserTools provides enhanced browser automation tools
type BrowserTools struct {
	defaultTimeout int
}

// NewBrowserTools creates a new browser tools instance
func NewBrowserTools() *BrowserTools {
	return &BrowserTools{
		defaultTimeout: 30,
	}
}

// BrowserNavigateTool navigates to a URL
type BrowserNavigateTool struct {
	bt *BrowserTools
}

func NewBrowserNavigateTool(bt *BrowserTools) *BrowserNavigateTool {
	return &BrowserNavigateTool{bt: bt}
}

func (t *BrowserNavigateTool) Name() string { return "browser_navigate" }

func (t *BrowserNavigateTool) Description() string {
	return "Navigate to a URL and get the page content. Use this to open web pages."
}

func (t *BrowserNavigateTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to navigate to",
			},
			"wait_for": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector or timeout to wait for (optional)",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserNavigateTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	urlStr, ok := args["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url is required")
	}

	// Validate URL
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract title
	title := doc.Find("title").First().Text()

	// Extract main content
	content := doc.Find("body").First().Text()
	content = cleanText(content)

	return map[string]interface{}{
		"url":          urlStr,
		"title":       strings.TrimSpace(title),
		"status":      resp.StatusCode,
		"content":     truncateText(content, 5000),
		"content_type": resp.Header.Get("Content-Type"),
	}, nil
}

// BrowserSnapshotTool takes a snapshot of the current page
type BrowserSnapshotTool struct {
	bt *BrowserTools
}

func NewBrowserSnapshotTool(bt *BrowserTools) *BrowserSnapshotTool {
	return &BrowserSnapshotTool{bt: bt}
}

func (t *BrowserSnapshotTool) Name() string { return "browser_snapshot" }

func (t *BrowserSnapshotTool) Description() string {
	return "Get a snapshot of the current page with all visible elements, links, and structure."
}

func (t *BrowserSnapshotTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to snapshot (optional if session active)",
			},
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector to focus on (optional)",
			},
		},
	}
}

func (t *BrowserSnapshotTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	urlStr, _ := args["url"].(string)
	selector, _ := args["selector"].(string)

	if urlStr == "" {
		return map[string]interface{}{
			"error":    "url is required for snapshot",
			"snapshot": map[string]interface{}{},
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	snapshot := make(map[string]interface{})

	// Title
	snapshot["title"] = doc.Find("title").First().Text()

	// Headings
	var headings []string
	doc.Find("h1, h2, h3").Each(func(i int, s *goquery.Selection) {
		headings = append(headings, strings.TrimSpace(s.Text()))
	})
	snapshot["headings"] = headings

	// Links
	var links []map[string]string
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		text := strings.TrimSpace(s.Text())
		if href != "" && text != "" {
			links = append(links, map[string]string{"text": text, "href": href})
		}
	})
	snapshot["links"] = links

	// Forms
	var forms []map[string]string
	doc.Find("form").Each(func(i int, s *goquery.Selection) {
		method, _ := s.Attr("method")
		action, _ := s.Attr("action")
		forms = append(forms, map[string]string{"method": method, "action": action})
	})
	snapshot["forms"] = forms

	// Selected content
	if selector != "" {
		snapshot["selected"] = doc.Find(selector).First().Text()
	} else {
		snapshot["content"] = truncateText(cleanText(doc.Find("body").First().Text()), 3000)
	}

	return snapshot, nil
}

// BrowserClickTool simulates clicking an element
type BrowserClickTool struct {
	bt *BrowserTools
}

func NewBrowserClickTool(bt *BrowserTools) *BrowserClickTool {
	return &BrowserClickTool{bt: bt}
}

func (t *BrowserClickTool) Name() string { return "browser_click" }

func (t *BrowserClickTool) Description() string {
	return "Click an element on the page. Provide the CSS selector for the element to click."
}

func (t *BrowserClickTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for the element to click",
			},
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL of the page containing the element (optional)",
			},
		},
		"required": []string{"selector"},
	}
}

func (t *BrowserClickTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	selector, ok := args["selector"].(string)
	if !ok {
		return nil, fmt.Errorf("selector is required")
	}

	// Note: This is a simplified implementation. In production, use Playwright or Puppeteer
	return map[string]interface{}{
		"action":   "click",
		"selector": selector,
		"message":  "Click action recorded. For full browser automation, use Playwright integration.",
	}, nil
}

// BrowserTypeTool simulates typing text
type BrowserTypeTool struct {
	bt *BrowserTools
}

func NewBrowserTypeTool(bt *BrowserTools) *BrowserTypeTool {
	return &BrowserTypeTool{bt: bt}
}

func (t *BrowserTypeTool) Name() string { return "browser_type" }

func (t *BrowserTypeTool) Description() string {
	return "Type text into an input field or contentEditable element."
}

func (t *BrowserTypeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for the input element",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to type",
			},
			"clear": map[string]interface{}{
				"type":        "boolean",
				"description": "Clear existing content first (default: false)",
			},
		},
		"required": []string{"selector", "text"},
	}
}

func (t *BrowserTypeTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	selector, _ := args["selector"].(string)
	text, _ := args["text"].(string)
	clear, _ := args["clear"].(bool)

	return map[string]interface{}{
		"action":   "type",
		"selector": selector,
		"text":     text,
		"cleared":  clear,
		"message":  "Type action recorded. For full browser automation, use Playwright integration.",
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
				"enum":        []string{"up", "down", "top", "bottom"},
				"description": "Scroll direction",
			},
			"amount": map[string]interface{}{
				"type":        "number",
				"description": "Number of pixels to scroll (default: 500)",
			},
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "Scroll to specific element selector (optional)",
			},
		},
		"required": []string{"direction"},
	}
}

func (t *BrowserScrollTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	direction, _ := args["direction"].(string)
	amount, _ := args["amount"].(float64)
	selector, _ := args["selector"].(string)

	if amount == 0 {
		amount = 500
	}

	return map[string]interface{}{
		"action":    "scroll",
		"direction": direction,
		"amount":    int(amount),
		"selector":  selector,
		"message":   "Scroll action recorded. For full browser automation, use Playwright integration.",
	}, nil
}

// BrowserBackTool navigates back
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
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *BrowserBackTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"action":  "back",
		"message": "Back navigation recorded. For full browser automation, use Playwright integration.",
	}, nil
}

// BrowserGetImagesTool extracts image URLs
type BrowserGetImagesTool struct {
	bt *BrowserTools
}

func NewBrowserGetImagesTool(bt *BrowserTools) *BrowserGetImagesTool {
	return &BrowserGetImagesTool{bt: bt}
}

func (t *BrowserGetImagesTool) Name() string { return "browser_get_images" }

func (t *BrowserGetImagesTool) Description() string {
	return "Get all image URLs from the current page or a specific URL."
}

func (t *BrowserGetImagesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to extract images from",
			},
			"min_width": map[string]interface{}{
				"type":        "number",
				"description": "Minimum image width in pixels (optional)",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserGetImagesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	urlStr, _ := args["url"].(string)
	minWidth, _ := args["min_width"].(float64)

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	baseURL, _ := url.Parse(urlStr)
	var images []map[string]string

	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		alt, _ := s.Attr("alt")

		// Resolve relative URLs
		if src != "" {
			if !strings.HasPrefix(src, "http") {
				absURL := baseURL.ResolveReference(&url.URL{Path: src})
				src = absURL.String()
			}
			images = append(images, map[string]string{
				"src": src,
				"alt": alt,
			})
		}
	})

	_ = minWidth // TODO: implement min_width filtering

	return map[string]interface{}{
		"url":    urlStr,
		"count":  len(images),
		"images": images,
	}, nil
}

// BrowserConsoleTool extracts console errors (placeholder)
type BrowserConsoleTool struct{}

func NewBrowserConsoleTool() *BrowserConsoleTool {
	return &BrowserConsoleTool{}
}

func (t *BrowserConsoleTool) Name() string { return "browser_console" }

func (t *BrowserConsoleTool) Description() string {
	return "Get console messages and errors from the browser. Requires JavaScript execution."
}

func (t *BrowserConsoleTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL of the page to check console",
			},
			"level": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"all", "error", "warning", "info"},
				"description": "Console level to fetch",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserConsoleTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"message": "Console extraction requires Playwright integration",
		"hint":    "Use web_search or web_fetch for content extraction without JS",
	}, nil
}

// Helper functions

func cleanText(text string) string {
	// Remove extra whitespace
	space := regexp.MustCompile(`\s+`)
	text = space.ReplaceAllString(text, " ")
	return text
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// ExportBrowserToolsJSON exports browser tools as JSON
func ExportBrowserToolsJSON() string {
	navTool := &BrowserNavigateTool{}
	snapTool := &BrowserSnapshotTool{}
	clickTool := &BrowserClickTool{}
	typeTool := &BrowserTypeTool{}
	scrollTool := &BrowserScrollTool{}
	backTool := &BrowserBackTool{}
	imgTool := &BrowserGetImagesTool{}
	consoleTool := &BrowserConsoleTool{}

	result := []map[string]interface{}{
		{"name": "browser_navigate", "description": "Navigate to URL and get page content", "schema": navTool.Schema()},
		{"name": "browser_snapshot", "description": "Get page snapshot", "schema": snapTool.Schema()},
		{"name": "browser_click", "description": "Click page element", "schema": clickTool.Schema()},
		{"name": "browser_type", "description": "Type text into element", "schema": typeTool.Schema()},
		{"name": "browser_scroll", "description": "Scroll page", "schema": scrollTool.Schema()},
		{"name": "browser_back", "description": "Go back to previous page", "schema": backTool.Schema()},
		{"name": "browser_get_images", "description": "Extract image URLs", "schema": imgTool.Schema()},
		{"name": "browser_console", "description": "Get console messages", "schema": consoleTool.Schema()},
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return string(jsonBytes)
}
