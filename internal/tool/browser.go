package tool

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/magicwubiao/go-magic/internal/util"
)

// WebFetchTool fetches and parses web pages using goquery
type WebFetchTool struct{}

// NewWebFetchTool creates a new web fetch tool
func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{}
}

func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

func (t *WebFetchTool) Description() string {
	return "Fetch and parse web pages, extract text and links"
}

func (t *WebFetchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch",
			},
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector to extract specific elements (optional)",
			},
			"extract": map[string]interface{}{
				"type": "string",
				"enum": []string{"text", "html", "links", "all"},
			},
			"max_chars": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters for text/html output; excess is truncated keeping head and tail (default: 15000)",
				"default":     15000,
			},
		},
		"required": []string{"url"},
	}
}

// applyCharBudget 截断超长文本，保留头尾窗口（hermes web_extract 对齐）。
// 返回截断后的文本以及是否发生了截断。
func applyCharBudget(s string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s, false
	}
	// 90% 头部 + 10% 尾部
	headLen := maxChars * 9 / 10
	tailLen := maxChars - headLen
	omitted := len(runes) - maxChars
	truncated := string(runes[:headLen]) +
		fmt.Sprintf("\n\n[... %d characters omitted ...]\n\n", omitted) +
		string(runes[len(runes)-tailLen:])
	return truncated, true
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url, ok := args["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url argument is required")
	}

	// Validate URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; go-magic/1.0)")

	// Execute request
	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d", resp.StatusCode)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	extract := "text"
	if e, ok := args["extract"].(string); ok {
		extract = e
	}

	selector := ""
	if s, ok := args["selector"].(string); ok {
		selector = s
	}

	// 字符预算（hermes web_extract 对齐）：默认 15000，防止大页面撑爆上下文
	maxChars := 15000
	if mc, ok := args["max_chars"].(float64); ok && mc > 0 {
		maxChars = int(mc)
	}

	result := make(map[string]interface{})
	result["url"] = url
	result["status_code"] = resp.StatusCode
	result["content_type"] = resp.Header.Get("Content-Type")

	budget := func(key string) {
		if s, ok := result[key].(string); ok {
			if trimmed, truncated := applyCharBudget(s, maxChars); truncated {
				result[key] = trimmed
				result["truncated"] = true
				result["max_chars"] = maxChars
			}
		}
	}

	switch extract {
	case "links":
		var links []string
		if selector != "" {
			doc.Find(selector).Each(func(i int, s *goquery.Selection) {
				link, _ := s.Attr("href")
				if link != "" {
					links = append(links, link)
				}
			})
		} else {
			doc.Find("a").Each(func(i int, s *goquery.Selection) {
				link, _ := s.Attr("href")
				if link != "" {
					links = append(links, link)
				}
			})
		}
		result["links"] = links

	case "html":
		if selector != "" {
			html, err := doc.Find(selector).First().Html()
			if err != nil {
				return nil, fmt.Errorf("failed to get html: %w", err)
			}
			result["html"] = html
		} else {
			html, err := doc.Find("body").First().Html()
			if err != nil {
				return nil, fmt.Errorf("failed to get html: %w", err)
			}
			result["html"] = html
		}

	case "all":
		if selector != "" {
			result["text"] = strings.TrimSpace(doc.Find(selector).First().Text())
			html, err := doc.Find(selector).First().Html()
			if err != nil {
				return nil, fmt.Errorf("failed to get html: %w", err)
			}
			result["html"] = html
		} else {
			result["text"] = strings.TrimSpace(doc.Find("body").First().Text())
			html, err := doc.Find("body").First().Html()
			if err != nil {
				return nil, fmt.Errorf("failed to get html: %w", err)
			}
			result["html"] = html
		}

		var links []string
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			link, _ := s.Attr("href")
			if link != "" {
				links = append(links, link)
			}
		})
		result["links"] = links

	default: // text
		if selector != "" {
			result["text"] = strings.TrimSpace(doc.Find(selector).First().Text())
		} else {
			result["text"] = strings.TrimSpace(doc.Find("body").First().Text())
		}
	}

	// 对文本类输出应用字符预算
	if extract == "text" || extract == "all" {
		budget("text")
	}
	if extract == "html" || extract == "all" {
		budget("html")
	}

	return result, nil
}

// WebSelectTool extracts specific elements from web pages
type WebSelectTool struct{}

// NewWebSelectTool creates a new web select tool
func NewWebSelectTool() *WebSelectTool {
	return &WebSelectTool{}
}

func (t *WebSelectTool) Name() string {
	return "web_select"
}

func (t *WebSelectTool) Description() string {
	return "Extract structured data from web pages using CSS selectors"
}

func (t *WebSelectTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch",
			},
			"selectors": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type": "string",
						},
						"selector": map[string]interface{}{
							"type": "string",
						},
						"attr": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"description": "Array of selectors to extract",
			},
		},
		"required": []string{"url", "selectors"},
	}
}

func (t *WebSelectTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url, ok := args["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url argument is required")
	}

	selectors, ok := args["selectors"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("selectors must be an array")
	}

	// Validate URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; go-magic/1.0)")

	// Execute request
	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d", resp.StatusCode)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	result := make(map[string]interface{})
	result["url"] = url

	for _, sel := range selectors {
		selMap, ok := sel.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := selMap["name"].(string)
		selector, _ := selMap["selector"].(string)
		attr, _ := selMap["attr"].(string)

		if selector == "" || name == "" {
			continue
		}

		selection := doc.Find(selector)
		if attr != "" {
			// Extract attribute
			val, exists := selection.First().Attr(attr)
			if exists {
				result[name] = val
			} else {
				result[name] = ""
			}
		} else {
			// Extract text
			result[name] = strings.TrimSpace(selection.First().Text())
		}
	}

	return result, nil
}
