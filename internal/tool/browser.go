package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/magicwubiao/go-magic/internal/util"
	"golang.org/x/text/encoding/htmlindex"
)

// maxWebFetchBytes 限制单次网页抓取的响应体大小（10MB），防止超大响应耗尽内存
const maxWebFetchBytes = 10 << 20

// defaultWebUserAgent 是普通桌面浏览器的 UA。
//
// 以前用的是 "Mozilla/5.0 (compatible; go-magic/1.0)"：这个串会命中不少站点的
// 爬虫/降级策略，返回精简版页面（表现为 web_fetch 只拿到一两段文字，而浏览器
// 里整页内容都在），让人误判成解析 bug。
const defaultWebUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var metaCharsetRe = regexp.MustCompile(`(?i)<meta[^>]+charset\s*=\s*["']?([a-z0-9_\-]+)`)

// sniffCharset 从 Content-Type 或 <meta charset> 判断页面编码。
func sniffCharset(body []byte, contentType string) string {
	if contentType != "" {
		if _, params, err := mime.ParseMediaType(contentType); err == nil {
			if cs := strings.TrimSpace(params["charset"]); cs != "" {
				return cs
			}
		}
	}
	head := body
	if len(head) > 4096 {
		head = head[:4096]
	}
	if m := metaCharsetRe.FindSubmatch(head); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// decodeHTMLBody 按页面声明的字符集解码。
//
// 不做这一步时，GBK/Big5/Shift_JIS 页面会被当成 UTF-8 解析，正文变成乱码——
// 这是"web_fetch 结果和浏览器不一致"的另一个来源（与是否执行 JS 无关）。
func decodeHTMLBody(body []byte, contentType string) []byte {
	cs := sniffCharset(body, contentType)
	if cs == "" {
		return body
	}
	switch strings.ToLower(cs) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return body
	}
	enc, err := htmlindex.Get(cs)
	if err != nil || enc == nil {
		return body
	}
	decoded, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return body
	}
	return decoded
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
	return "Fetch a web page over plain HTTP and extract its text/html/links (no JavaScript is executed). " +
		"For JS-rendered pages, content behind a login, or pages that look empty here, use browser_navigate instead."
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
				"type":        "string",
				"enum":        []string{"text", "html", "links", "all"},
				"description": "What to return (default: text)",
			},
			"max_chars": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters for text/html output; excess is truncated keeping head and tail (default: 15000)",
				"default":     15000,
			},
			"user_agent": map[string]interface{}{
				"type":        "string",
				"description": "Override the User-Agent header (optional; defaults to a desktop Chrome UA)",
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

	// Validate URL: keep any explicit scheme (file://, data://, ...) and only
	// default bare host names to https://
	url = normalizeURL(url)
	if strings.HasPrefix(url, "file://") {
		return nil, fmt.Errorf("cannot read local files (file:// URL) via HTTP; use the read_file tool instead")
	}
	// SSRF 防护：拒绝非 http/https 协议与内网地址（与 web_tools.validateURL 对齐）
	if err := validateURL(url); err != nil {
		return nil, err
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	ua := defaultWebUserAgent
	if custom, ok := args["user_agent"].(string); ok && strings.TrimSpace(custom) != "" {
		ua = custom
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

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

	// 先整体读入（受 10MB 上限保护）再解码：判定编码需要看 Content-Type 与
	// <meta charset>，而流式解析无法回头纠正已解析的字节。
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxWebFetchBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	body := decodeHTMLBody(raw, resp.Header.Get("Content-Type"))

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
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
	if mc := paramInt(args, "max_chars"); mc > 0 {
		maxChars = mc
	}

	result := make(map[string]interface{})
	result["url"] = url
	result["status_code"] = resp.StatusCode
	result["content_type"] = resp.Header.Get("Content-Type")
	if cs := sniffCharset(raw, resp.Header.Get("Content-Type")); cs != "" {
		result["charset"] = cs
	}

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

	// 静态抓取几乎什么都没拿到时，直接点明下一步，避免被当成"页面只有这么多内容"。
	if text, ok := result["text"].(string); ok && len([]rune(text)) < 200 {
		result["note"] = "Extracted very little text over plain HTTP. The page is probably rendered by JavaScript " +
			"or served differently to non-browser clients; use browser_navigate to get the rendered DOM."
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
	return "Extract specific elements from a web page using CSS selectors (plain HTTP, no JavaScript). " +
		"Returns the element text by default; set attr for an attribute value, all=true to collect every match " +
		"(e.g. a list of links), and check `matched` to distinguish 'no match' from 'empty value'."
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
							"type":        "string",
							"description": "Key under which the extracted value is returned",
						},
						"selector": map[string]interface{}{
							"type":        "string",
							"description": "CSS selector",
						},
						"attr": map[string]interface{}{
							"type": "string",
							"description": "Attribute to read (e.g. 'href', 'src'). " +
								"Omit it, or use 'text', to get the element's text",
						},
						"all": map[string]interface{}{
							"type":        "boolean",
							"description": "Return every match as an array instead of only the first one",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of values to return when all=true (default: unlimited)",
						},
					},
					"required": []interface{}{"name", "selector"},
				},
				"description": "Array of selectors to extract",
			},
		},
		"required": []interface{}{"url", "selectors"},
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

	// Validate URL: keep any explicit scheme (file://, data://, ...) and only
	// default bare host names to https://
	url = normalizeURL(url)
	if strings.HasPrefix(url, "file://") {
		return nil, fmt.Errorf("cannot read local files (file:// URL) via HTTP; use the read_file tool instead")
	}
	// SSRF 防护：拒绝非 http/https 协议与内网地址（与 web_tools.validateURL 对齐）
	if err := validateURL(url); err != nil {
		return nil, err
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", defaultWebUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

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

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxWebFetchBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(decodeHTMLBody(raw, resp.Header.Get("Content-Type"))))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	result := make(map[string]interface{})
	result["url"] = url
	for k, v := range applySelectors(doc, selectors) {
		result[k] = v
	}
	return result, nil
}

// applySelectors 把 selectors 描述应用到文档上，返回 name → 值（可能是字符串
// 或字符串数组），外加 matched 计数与未命中提示。与抓取解耦，便于单测。
func applySelectors(doc *goquery.Document, selectors []interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	// matched 与取值分开返回：取到空字符串到底是"没匹配到元素"还是"元素内容为空"，
	// 光看值是分不出来的。
	matched := make(map[string]int)
	var unmatched []string

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

		// attr:"text" 曾经被当成"读取名为 text 的属性"，静默返回空串；这里把
		// 文本语义的几种常见写法都归一化到"取文本"。
		wantText := false
		switch strings.ToLower(strings.TrimSpace(attr)) {
		case "", "text", "inner_text", "innertext", "textcontent":
			wantText = true
		}

		selection := doc.Find(selector)
		count := selection.Length()
		matched[name] = count
		if count == 0 {
			unmatched = append(unmatched, name)
		}

		valueOf := func(s *goquery.Selection) string {
			if wantText {
				return strings.TrimSpace(s.Text())
			}
			v, _ := s.Attr(attr)
			return strings.TrimSpace(v)
		}

		all, _ := selMap["all"].(bool)
		limit := paramInt(selMap, "limit")

		if !all && limit <= 1 {
			// 单值模式：保持历史行为（取第一个），但把匹配数告诉调用方。
			result[name] = valueOf(selection.First())
			continue
		}

		values := make([]string, 0, count)
		selection.EachWithBreak(func(_ int, s *goquery.Selection) bool {
			values = append(values, valueOf(s))
			if limit > 0 && len(values) >= limit {
				return false
			}
			return true
		})
		result[name] = values
	}

	if len(matched) > 0 {
		result["matched"] = matched
	}
	if len(unmatched) > 0 {
		result["note"] = fmt.Sprintf("selectors with no match: %s", strings.Join(unmatched, ", "))
	}
	return result
}
