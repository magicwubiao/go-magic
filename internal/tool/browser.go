package tool

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
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
		},
		"required": []string{"url"},
	}
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
	client := &http.Client{}
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

	result := make(map[string]interface{})
	result["url"] = url
	result["status_code"] = resp.StatusCode
	result["content_type"] = resp.Header.Get("Content-Type")

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
	client := &http.Client{}
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
