package tool

import (
	"context"
	"fmt"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"io"
	"net/http"
	"strings"
)

type WebSearchTool struct{}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return "Search the web and return structured results (title, URL, snippet)"
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
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok {
		return nil, fmt.Errorf("query argument is required")
	}

	count := 5
	if c, ok := args["count"].(float64); ok {
		count = int(c)
		if count > 10 {
			count = 10
		}
	}

	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", strings.ReplaceAll(query, " ", "+"))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoMagic/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	results := parseDuckDuckGoResults(doc, count)

	return map[string]interface{}{
		"query":   query,
		"count":   len(results),
		"results": results,
	}, nil
}

// WebSearchResult represents a web search result
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func parseDuckDuckGoResults(n *html.Node, maxResults int) []WebSearchResult {
	var results []WebSearchResult

	// Find all result containers with class "result"
	var findResults func(*html.Node)
	findResults = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			// Check if this link is inside a result container
			if isInsideResult(n) {
				result := extractWebResultFromLink(n)
				if result.URL != "" && len(results) < maxResults {
					results = append(results, result)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findResults(c)
		}
	}
	findResults(n)

	// Fallback: simple extraction if the above didn't work
	if len(results) == 0 {
		results = simpleExtractWebResults(n, maxResults)
	}

	return results
}

func isInsideResult(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode {
			for _, a := range p.Attr {
				if a.Key == "class" && strings.Contains(a.Val, "result") {
					return true
				}
			}
		}
	}
	return false
}

func extractWebResultFromLink(n *html.Node) WebSearchResult {
	var result WebSearchResult

	// Get URL
	for _, a := range n.Attr {
		if a.Key == "href" {
			result.URL = a.Val
			break
		}
	}

	// Get title (link text)
	result.Title = getTextContent(n)

	// Try to find snippet (usually in a div with class "snippet")
	parent := n.Parent
	for p := parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode {
			for _, a := range p.Attr {
				if a.Key == "class" && strings.Contains(a.Val, "snippet") {
					result.Snippet = getTextContent(p)
					return result
				}
			}
		}
	}

	return result
}

func simpleExtractWebResults(n *html.Node, maxResults int) []WebSearchResult {
	var results []WebSearchResult
	var links []*html.Node

	// Collect all links
	var collectLinks func(*html.Node)
	collectLinks = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			links = append(links, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectLinks(c)
		}
	}
	collectLinks(n)

	for i, link := range links {
		if i >= maxResults {
			break
		}
		result := extractWebResultFromLink(link)
		if result.URL != "" && strings.HasPrefix(result.URL, "http") {
			results = append(results, result)
		}
	}

	return results
}

func getTextContent(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return strings.TrimSpace(sb.String())
}

// ============================================================================
// Web Search Tool (standalone, no conflicts with file_tools)
// ============================================================================
