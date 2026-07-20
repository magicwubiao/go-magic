package tool

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var safeHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// isPrivateIP 校验 IP 是否为内网/保留地址
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// validateURL 校验 URL 协议和目标地址，防止 SSRF
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// 仅允许 http/https
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	// 解析 host，校验是否为内网地址
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}
	// 解析 DNS
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip.String()) {
			return fmt.Errorf("access to private IP denied: %s", ip)
		}
	}
	return nil
}

// WebSearchTool provides web search capabilities with China-friendly engines
type WebSearchTool struct{}

// WebSearchResult represents a search result
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return "Search the web and return structured results (title, URL, snippet). Supports multiple search engines."
}

func (t *WebSearchTool) Parameters() map[string]interface{} {
	return t.Schema()
}

func (t *WebSearchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query",
			},
			"count": map[string]interface{}{
				"type":        "number",
				"description": "Number of results to return (default: 5, max: 10)",
			},
			"engine": map[string]interface{}{
				"type":        "string",
				"description": "Search engine: 'baidu', 'bing', 'duckduckgo' (default: auto-detect)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query argument is required")
	}

	count := 5
	if c, ok := args["count"].(float64); ok {
		count = int(c)
		if count > 10 {
			count = 10
		}
	}

	engine := "auto"
	if e, ok := args["engine"].(string); ok {
		engine = e
	}

	// Try engines in order of China accessibility
	engines := []string{}
	switch engine {
	case "auto":
		engines = []string{"baidu", "bing", "duckduckgo"}
	case "baidu", "bing", "duckduckgo":
		engines = []string{engine}
	default:
		engines = []string{"baidu", "bing", "duckduckgo"}
	}

	var lastErr error
	for _, eng := range engines {
		var results []WebSearchResult
		var err error

		switch eng {
		case "baidu":
			results, err = t.searchBaidu(ctx, query, count)
		case "bing":
			results, err = t.searchBing(ctx, query, count)
		case "duckduckgo":
			results, err = t.searchDuckDuckGo(ctx, query, count)
		}

		if err != nil {
			lastErr = err
			continue
		}

		if len(results) > 0 {
			return map[string]interface{}{
				"query":   query,
				"engine":  eng,
				"count":   len(results),
				"results": results,
			}, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all search engines failed: %v", lastErr)
	}
	return map[string]interface{}{
		"query":   query,
		"engine":  "none",
		"count":   0,
		"results": []WebSearchResult{},
	}, nil
}

// searchBaidu searches using Baidu (most China-friendly)
func (t *WebSearchTool) searchBaidu(ctx context.Context, query string, count int) ([]WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://www.baidu.com/s?wd=%s&rn=%d", url.QueryEscape(query), count)

	if err := validateURL(searchURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := safeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("baidu returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []WebSearchResult
	doc.Find(".result, .c-container").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if i >= count {
			return false
		}

		// Get title and link
		var title, link string
		s.Find("h3 a, .t a").EachWithBreak(func(j int, a *goquery.Selection) bool {
			title = strings.TrimSpace(a.Text())
			link, _ = a.Attr("href")
			return false // take first
		})

		// Get snippet
		var snippet string
		s.Find(".c-abstract, .content-right_8Zs40, .c-span-last").EachWithBreak(func(j int, span *goquery.Selection) bool {
			snippet = strings.TrimSpace(span.Text())
			if len(snippet) > 10 {
				return false
			}
			return true
		})

		if title != "" && link != "" {
			results = append(results, WebSearchResult{
				Title:   title,
				URL:     link,
				Snippet: snippet,
			})
		}
		return true
	})

	return results, nil
}

// searchBing searches using Bing (China-accessible)
func (t *WebSearchTool) searchBing(ctx context.Context, query string, count int) ([]WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://cn.bing.com/search?q=%s&count=%d", url.QueryEscape(query), count)

	if err := validateURL(searchURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := safeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bing returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []WebSearchResult
	doc.Find(".b_algo").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if i >= count {
			return false
		}

		var title, link, snippet string

		s.Find("h2 a").EachWithBreak(func(j int, a *goquery.Selection) bool {
			title = strings.TrimSpace(a.Text())
			link, _ = a.Attr("href")
			return false
		})

		s.Find(".b_caption p").EachWithBreak(func(j int, p *goquery.Selection) bool {
			snippet = strings.TrimSpace(p.Text())
			return false
		})

		if title != "" && link != "" {
			results = append(results, WebSearchResult{
				Title:   title,
				URL:     link,
				Snippet: snippet,
			})
		}
		return true
	})

	return results, nil
}

// searchDuckDuckGo searches using DuckDuckGo HTML (fallback)
func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, count int) ([]WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	if err := validateURL(searchURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoMagic/1.0)")

	resp, err := safeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("duckduckgo returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []WebSearchResult
	doc.Find(".result").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if i >= count {
			return false
		}

		var title, link, snippet string

		s.Find(".result__a").EachWithBreak(func(j int, a *goquery.Selection) bool {
			title = strings.TrimSpace(a.Text())
			link, _ = a.Attr("href")
			return false
		})

		s.Find(".result__snippet").EachWithBreak(func(j int, p *goquery.Selection) bool {
			snippet = strings.TrimSpace(p.Text())
			return false
		})

		if title != "" && link != "" {
			results = append(results, WebSearchResult{
				Title:   title,
				URL:     link,
				Snippet: snippet,
			})
		}
		return true
	})

	return results, nil
}
