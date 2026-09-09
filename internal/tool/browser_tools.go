package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
				"description": "CSS selector to wait for after the page loads (optional, up to 10s)",
			},
		},
		"required": []string{"url"},
	}
}

// normalizeURL keeps URLs that already carry a scheme (http, https, file,
// data, about, ...) untouched and picks a sensible default scheme for bare
// host names like "example.com/page". Local hosts (localhost, 127.0.0.1, any
// bare IP) default to http:// since they are almost always plain-HTTP dev
// servers; everything else defaults to https://.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		// A real scheme:// URL parses with no Opaque part. Opaque schemes
		// (about:, data:, ...) are only kept when explicitly known —
		// otherwise "localhost:8080" would be misread as scheme "localhost".
		if u.Opaque == "" || opaqueSchemeOK(u.Scheme) {
			return raw
		}
	}
	if looksLikeLocalHost(raw) {
		return "http://" + raw
	}
	return "https://" + raw
}

// opaqueSchemeOK lists schemes whose URLs carry no "//" authority (about:,
// data:, ...) and should therefore be preserved as-is.
func opaqueSchemeOK(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "about", "data", "mailto", "javascript", "tel", "sms", "blob":
		return true
	}
	return false
}

// looksLikeLocalHost reports whether a scheme-less raw URL points at a local
// host (localhost or a bare IP address, with optional :port). IPv6 literals
// (::1 or [::1]:8080) are handled.
func looksLikeLocalHost(raw string) bool {
	host := raw
	// strip path / query / fragment for host extraction
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if strings.HasPrefix(host, "[") {
		// bracketed IPv6 literal, e.g. [::1]:8080
		if i := strings.Index(host, "]"); i >= 0 {
			host = host[1:i]
		}
	} else if i := strings.LastIndex(host, ":"); i >= 0 && strings.Count(host, ":") == 1 {
		// host:port — strip the numeric port
		if _, err := strconv.Atoi(host[i+1:]); err == nil {
			host = host[:i]
		}
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	return net.ParseIP(host) != nil
}

func (t *BrowserNavigateTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	urlStr, ok := args["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url is required")
	}

	urlStr = normalizeURL(urlStr)

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}
	waitFor, _ := args["wait_for"].(string)

	// Browser automation first
	title, text, err := t.tryBrowserAutomation(tabID, urlStr)
	if err == nil {
		waitStatus := "not_requested"
		if waitFor != "" {
			if werr := GetBrowserManager().WaitForElement(tabID, waitFor, 10*time.Second); werr != nil {
				waitStatus = fmt.Sprintf("element not found within 10s: %v", werr)
			} else {
				waitStatus = "found"
			}
		}
		return map[string]interface{}{
			"url":      urlStr,
			"title":    title,
			"tab_id":   tabID,
			"content":  utils.Truncate(text, 5000),
			"success":  true,
			"method":   "browser",
			"wait_for": waitStatus,
		}, nil
	}

	// Fallback to HTTP fetch if browser automation fails
	result, ferr := t.fetchWithHTTP(urlStr, tabID)
	if ferr != nil {
		return nil, fmt.Errorf("both browser automation and HTTP fetch failed (browser: %v; http: %w)", err, ferr)
	}

	if m, ok := result.(map[string]interface{}); ok {
		m["browser_error"] = err.Error()
		m["note"] = "browser automation unavailable, so content was fetched over plain HTTP and may miss JavaScript-rendered parts; interactive tools (browser_click etc.) still need a working Chrome"
	}
	return result, nil
}

// tryBrowserAutomation attempts to use chromedp for browser automation
func (t *BrowserNavigateTool) tryBrowserAutomation(tabID, urlStr string) (string, string, error) {
	bm := GetBrowserManager()

	tab, err := bm.NewTab(tabID)
	if err != nil {
		// Only recycle the browser when no other tab depends on it — a dead
		// Chrome left over from a previous run otherwise poisons every future
		// attempt until the process restarts.
		if bm.TabCount() == 0 {
			bm.Close()
			tab, err = bm.NewTab(tabID)
			if err != nil {
				return "", "", fmt.Errorf("failed to create browser tab: %w", err)
			}
		} else {
			return "", "", fmt.Errorf("failed to create browser tab: %w", err)
		}
	}

	// A tab whose context is already canceled means the underlying Chrome
	// session is gone. Recycle the whole browser and retry once so navigation
	// self-heals instead of failing forever with "context canceled".
	if tab.Ctx.Err() != nil {
		bm.Close()
		tab, err = bm.NewTab(tabID)
		if err != nil {
			return "", "", fmt.Errorf("failed to restart browser: %w", err)
		}
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
			"method":  "http",
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
		"method":  "http",
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
	urlStr = normalizeURL(urlStr)

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
	return "Get image URLs from the current page or a specific URL. When a live browser tab exists the rendered DOM is used (JavaScript-added images included) and min_width filtering works; otherwise the page is fetched over HTTP and only static HTML images are returned."
}

func (t *BrowserGetImagesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to extract images from (optional when an active tab exists)",
			},
			"min_width": map[string]interface{}{
				"type":        "number",
				"description": "Minimum natural image width in pixels; only honored in live-tab mode (optional)",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

func (t *BrowserGetImagesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	urlStr, _ := args["url"].(string)
	minWidth, _ := args["min_width"].(float64)
	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()

	// Live-tab mode: reflect the rendered DOM (includes JS-added images and
	// real natural widths).
	if tab, ok := bm.GetTab(tabID); ok && (urlStr == "" || urlStr == tab.URL) {
		imgs, err := bm.GetImagesWithInfo(tabID)
		if err != nil {
			return nil, err
		}
		images := make([]map[string]string, 0, len(imgs))
		for _, im := range imgs {
			if minWidth > 0 && float64(im.Width) < minWidth {
				continue
			}
			if im.Src == "" {
				continue
			}
			images = append(images, map[string]string{"src": im.Src, "alt": im.Alt})
		}
		result := map[string]interface{}{
			"url":    tab.URL,
			"count":  len(images),
			"images": images,
			"mode":   "live_tab",
		}
		if minWidth > 0 {
			result["filter"] = fmt.Sprintf("natural_width >= %d", int(minWidth))
		}
		return result, nil
	}

	// Static fetch mode: only used when there is no matching live tab.
	if urlStr == "" {
		return nil, fmt.Errorf("url is required when no active tab exists")
	}
	urlStr = normalizeURL(urlStr)

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
	images := make([]map[string]string, 0)
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		alt, _ := s.Attr("alt")
		src = resolveImgURL(baseURL, src)
		if src != "" {
			images = append(images, map[string]string{
				"src": src,
				"alt": alt,
			})
		}
	})

	result := map[string]interface{}{
		"url":    urlStr,
		"count":  len(images),
		"images": images,
		"mode":   "static_fetch",
	}
	if minWidth > 0 {
		result["note"] = "min_width cannot be applied over a static HTTP fetch (image widths are unknown); use a live browser tab to filter by width"
	}
	return result, nil
}

// resolveImgURL resolves a possibly-relative src against the page base URL,
// preserving query strings, absolute paths and protocol-relative (//host)
// URLs. Falls back to the raw src on parse errors.
func resolveImgURL(base *url.URL, src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	ref, err := url.Parse(src)
	if err != nil {
		return src
	}
	if base == nil {
		return src
	}
	return base.ResolveReference(ref).String()
}

// Helper functions

func cleanText(text string) string {
	// Remove extra whitespace
	space := regexp.MustCompile(`\s+`)
	text = space.ReplaceAllString(text, " ")
	return text
}

func timeNow() string {
	return time.Now().UTC().Format(time.RFC3339)
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
	pressTool := NewBrowserPressTool(bt)
	visionTool := NewBrowserVisionTool(bt)
	dialogTool := NewBrowserDialogTool(bt)

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
		{"name": "browser_press", "description": "Press keyboard key or type text", "schema": pressTool.Schema()},
		{"name": "browser_vision", "description": "Screenshot current page", "schema": visionTool.Schema()},
		{"name": "browser_dialog", "description": "List or respond to JS dialogs", "schema": dialogTool.Schema()},
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return string(jsonBytes)
}
