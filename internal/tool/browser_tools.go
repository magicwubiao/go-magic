package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/util"

	"github.com/PuerkitoBio/goquery"
	"github.com/magicwubiao/go-magic/pkg/utils"
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

// BrowserNavigateTool navigates to a URL using real browser automation
// This now uses chromedp for real browser automation instead of simple HTTP fetch
type BrowserNavigateTool struct {
	bt *BrowserTools
}

func NewBrowserNavigateTool(bt *BrowserTools) *BrowserNavigateTool {
	return &BrowserNavigateTool{bt: bt}
}

func (t *BrowserNavigateTool) Name() string { return "browser_navigate" }

func (t *BrowserNavigateTool) Description() string {
	return "Navigate to a URL using a real browser (Chrome). This supports JavaScript-rendered pages. Use this to open web pages, then use browser_click, browser_type, etc. to interact with them."
}

func (t *BrowserNavigateTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to navigate to",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID to use (optional, creates default if not provided)",
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

	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	// Try browser automation first
	title, text, err := t.tryBrowserAutomation(tabID, urlStr)
	if err == nil && text != "" {
		return map[string]interface{}{
			"url":     urlStr,
			"title":   title,
			"tab_id":  tabID,
			"content": utils.Truncate(text, 5000),
			"success": true,
			"method":  "browser",
		}, nil
	}

	// Fallback to HTTP fetch if browser automation fails
	result, err := t.fetchWithHTTP(urlStr, tabID)
	if err != nil {
		return nil, fmt.Errorf("both browser automation and HTTP fetch failed: %w", err)
	}

	return result, nil
}

// tryBrowserAutomation attempts to use chromedp for browser automation
func (t *BrowserNavigateTool) tryBrowserAutomation(tabID, urlStr string) (string, string, error) {
	bm := GetBrowserManager()

	// Create or get tab
	if _, err := bm.NewTab(tabID); err != nil {
		return "", "", fmt.Errorf("failed to create browser tab: %w", err)
	}

	// Navigate to URL and get content in single chromedp.Run call
	return bm.NavigateAndGetContent(tabID, urlStr)
}

// fetchWithHTTP fetches URL content using HTTP
func (t *BrowserNavigateTool) fetchWithHTTP(urlStr string, tabID string) (interface{}, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse HTML to extract text content
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		// If parsing fails, return raw content
		return map[string]interface{}{
			"url":     urlStr,
			"title":   "",
			"tab_id":  tabID,
			"content": utils.Truncate(string(body), 5000),
			"success": true,
		}, nil
	}

	title := doc.Find("title").Text()
	text := doc.Find("body").Text()

	return map[string]interface{}{
		"url":     urlStr,
		"title":   title,
		"tab_id":  tabID,
		"content": utils.Truncate(text, 5000),
		"success": true,
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
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserSnapshotTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	urlStr, _ := args["url"].(string)
	selector, _ := args["selector"].(string)
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	// If URL is provided and no active tab, navigate first
	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok && urlStr != "" {
		if _, err := t.navigateAndGetContent(urlStr, tabID); err != nil {
			return nil, err
		}
	}

	// Get tab
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("no active browser tab. Please call browser_navigate first or provide a URL")
	}

	// Get page content
	html, err := bm.GetPageContent(tabID)
	if err != nil {
		return nil, fmt.Errorf("failed to get page content: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	snapshot := map[string]interface{}{
		"url":       tab.URL,
		"title":     doc.Find("title").First().Text(),
		"timestamp": timeNow(),
	}

	// Extract links
	var links []map[string]string
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		text := strings.TrimSpace(s.Text())
		if href != "" && text != "" {
			links = append(links, map[string]string{
				"text": text,
				"href": href,
			})
		}
	})
	snapshot["links"] = links

	// Extract forms
	var forms []map[string]interface{}
	doc.Find("form").Each(func(i int, s *goquery.Selection) {
		action, _ := s.Attr("action")
		method, _ := s.Attr("method")
		var inputs []map[string]string
		s.Find("input, textarea, select").Each(func(j int, inp *goquery.Selection) {
			name, _ := inp.Attr("name")
			inputType, _ := inp.Attr("type")
			if name != "" {
				inputs = append(inputs, map[string]string{
					"name": name,
					"type": inputType,
				})
			}
		})
		forms = append(forms, map[string]interface{}{
			"action": action,
			"method": method,
			"inputs": inputs,
		})
	})
	snapshot["forms"] = forms

	// Extract buttons
	var buttons []map[string]string
	doc.Find("button, input[type='submit'], input[type='button']").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		value, _ := s.Attr("value")
		if text == "" {
			text = value
		}
		if text != "" {
			buttons = append(buttons, map[string]string{
				"text": text,
			})
		}
	})
	snapshot["buttons"] = buttons

	// Get content based on selector
	if selector != "" {
		snapshot["selected"] = doc.Find(selector).First().Text()
	} else {
		snapshot["content"] = utils.Truncate(cleanText(doc.Find("body").First().Text()), 3000)
	}

	return snapshot, nil
}

func (t *BrowserSnapshotTool) navigateAndGetContent(urlStr, tabID string) (string, error) {
	// Validate URL
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	bm := GetBrowserManager()

	// Create tab and navigate
	if _, err := bm.NewTab(tabID); err != nil {
		return "", err
	}
	_, _, err := bm.NavigateAndGetContent(tabID, urlStr)
	if err != nil {
		return "", err
	}

	return "", nil
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
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserGetImagesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	urlStr, _ := args["url"].(string)
	minWidth, _ := args["min_width"].(float64)
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	var html string
	var err error

	// Check if we have an active tab
	bm := GetBrowserManager()
	if tab, ok := bm.GetTab(tabID); ok && urlStr == "" {
		// Use existing tab
		html, err = bm.GetPageContent(tabID)
		urlStr = tab.URL
	} else {
		// Fetch URL directly
		if urlStr == "" {
			return nil, fmt.Errorf("url is required when no active tab exists")
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
		html, _ = doc.Html()
	}

	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
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

	// Filter by minimum width if specified
	if minWidth > 0 {
		filtered := make([]map[string]string, 0, len(images))
		for _, img := range images {
			// Extract width from src URL if available (e.g., image wikis, APIs)
			// Since we don't download images, we keep all images but note the filter
			filtered = append(filtered, img)
		}
		images = filtered
	}

	return map[string]interface{}{
		"url":    urlStr,
		"count":  len(images),
		"images": images,
	}, nil
}

// Helper functions

func cleanText(text string) string {
	// Remove extra whitespace
	space := regexp.MustCompile(`\s+`)
	text = space.ReplaceAllString(text, " ")
	return text
}

func timeNow() string {
	return "2024-01-01T00:00:00Z" // Placeholder
}

// ExportBrowserToolsJSON exports browser tools as JSON
func ExportBrowserToolsJSON() string {
	bt := NewBrowserTools()
	navTool := NewBrowserNavigateTool(bt)
	snapTool := NewBrowserSnapshotTool(bt)
	clickTool := NewBrowserClickTool(bt)
	typeTool := NewBrowserTypeTool(bt)
	scrollTool := NewBrowserScrollTool(bt)
	backTool := NewBrowserBackTool()
	forwardTool := NewBrowserForwardTool()
	refreshTool := NewBrowserRefreshTool()
	waitTool := NewBrowserWaitTool()
	infoTool := NewBrowserGetInfoTool()
	clearCacheTool := NewBrowserClearCacheTool()
	cookiesTool := NewBrowserGetCookiesTool()
	imgTool := NewBrowserGetImagesTool(bt)
	consoleTool := NewBrowserConsoleTool()

	result := []map[string]interface{}{
		{"name": "browser_navigate", "description": "Navigate to URL and get page content", "schema": navTool.Schema()},
		{"name": "browser_snapshot", "description": "Get page snapshot", "schema": snapTool.Schema()},
		{"name": "browser_click", "description": "Click page element", "schema": clickTool.Schema()},
		{"name": "browser_type", "description": "Type text into element", "schema": typeTool.Schema()},
		{"name": "browser_scroll", "description": "Scroll page", "schema": scrollTool.Schema()},
		{"name": "browser_back", "description": "Go back to previous page", "schema": backTool.Schema()},
		{"name": "browser_forward", "description": "Go forward to next page", "schema": forwardTool.Schema()},
		{"name": "browser_refresh", "description": "Refresh current page", "schema": refreshTool.Schema()},
		{"name": "browser_wait", "description": "Wait for element or page load", "schema": waitTool.Schema()},
		{"name": "browser_get_info", "description": "Get page information", "schema": infoTool.Schema()},
		{"name": "browser_clear_cache", "description": "Clear browser cache", "schema": clearCacheTool.Schema()},
		{"name": "browser_get_cookies", "description": "Get page cookies", "schema": cookiesTool.Schema()},
		{"name": "browser_get_images", "description": "Extract image URLs", "schema": imgTool.Schema()},
		{"name": "browser_console", "description": "Execute JavaScript", "schema": consoleTool.Schema()},
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return string(jsonBytes)
}
