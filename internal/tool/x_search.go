package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// --- X/Twitter Search Tool ---

// XSearchTool searches X (Twitter) for posts, threads, and trends
type XSearchTool struct {
	BaseTool
	apiKey      string
	bearerToken string
}

// XSearchRequest represents an X search request
type XSearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Type       string `json:"type"` // "latest", "top", "users"
}

// XSearchResult represents a single X post
type XSearchResult struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	AuthorID  string `json:"author_id"`
	Text      string `json:"text"`
	Likes     int    `json:"likes"`
	Retweets  int    `json:"retweets"`
	Replies   int    `json:"replies"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
}

// XSearchResponse is the response from X search
type XSearchResponse struct {
	Results    []XSearchResult `json:"results"`
	TotalCount int             `json:"total_count"`
	Query      string          `json:"query"`
}

// NewXSearchTool creates a new X search tool
func NewXSearchTool(apiKey, bearerToken string) *XSearchTool {
	return &XSearchTool{
		BaseTool: *NewBaseTool(
			"x_search",
			"Search X (Twitter) for posts, threads, and specific content. Supports OAuth or API key authentication.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query for X/Twitter",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (default: 10, max: 100)",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Search type: 'latest' (default), 'top', or 'users'",
						"enum":        []string{"latest", "top", "users"},
					},
				},
				"required": []string{"query"},
			},
		),
		apiKey:      apiKey,
		bearerToken: bearerToken,
	}
}

// Execute searches X based on the query
func (t *XSearchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	maxResults := 10
	if v, ok := params["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
		if maxResults > 100 {
			maxResults = 100
		}
	}

	if t.bearerToken == "" && t.apiKey == "" {
		return nil, fmt.Errorf("X API key or bearer token not configured. Set X_API_KEY or X_BEARER_TOKEN environment variable.")
	}

	// Call X API v2
	apiURL := fmt.Sprintf("https://api.x.com/2/tweets/search/recent?query=%s&max_results=%d&tweet.fields=created_at,public_metrics,author_id&expansions=author_id&user.fields=name,username",
		url.QueryEscape(query), maxResults)

	httpReq, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	auth := t.bearerToken
	if auth == "" {
		auth = t.apiKey
	}
	httpReq.Header.Set("Authorization", "Bearer "+auth)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("X API error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("X API error (%d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	response := &XSearchResponse{
		Query: query,
	}

	// Parse tweets
	if data, ok := result["data"].([]interface{}); ok {
		authors := make(map[string]string)
		// handles 单独存 @用户名：拼推文 URL 必须用它，用显示名会得到一个
		// https://x.com/Some%20Display%20Name/... 这种打不开的链接。
		handles := make(map[string]string)
		if includes, ok := result["includes"].(map[string]interface{}); ok {
			if users, ok := includes["users"].([]interface{}); ok {
				for _, u := range users {
					if user, ok := u.(map[string]interface{}); ok {
						id, _ := user["id"].(string)
						name, _ := user["name"].(string)
						username, _ := user["username"].(string)
						authors[id] = name + " (@" + username + ")"
						handles[id] = username
					}
				}
			}
		}

		for _, tweet := range data {
			if t, ok := tweet.(map[string]interface{}); ok {
				id, _ := t["id"].(string)
				text, _ := t["text"].(string)
				authorID, _ := t["author_id"].(string)
				createdAt, _ := t["created_at"].(string)

				likes, retweets, replies := 0, 0, 0
				if metrics, ok := t["public_metrics"].(map[string]interface{}); ok {
					if v, ok := metrics["like_count"].(float64); ok {
						likes = int(v)
					}
					if v, ok := metrics["retweet_count"].(float64); ok {
						retweets = int(v)
					}
					if v, ok := metrics["reply_count"].(float64); ok {
						replies = int(v)
					}
				}

				author := authors[authorID]
				response.Results = append(response.Results, XSearchResult{
					ID:        id,
					Author:    author,
					AuthorID:  authorID,
					Text:      text,
					Likes:     likes,
					Retweets:  retweets,
					Replies:   replies,
					CreatedAt: createdAt,
					URL:       tweetURL(handles[authorID], id),
				})
			}
		}
	}

	if meta, ok := result["meta"].(map[string]interface{}); ok {
		if count, ok := meta["result_count"].(float64); ok {
			response.TotalCount = int(count)
		}
	}

	return response, nil
}

// tweetURL 拼接单条推文的可访问链接。
// 拿不到 @用户名（includes 里没有该作者）时退化成 https://x.com/i/status/<id>，
// X 支持这种不带用户名的永久链接。
func tweetURL(handle, id string) string {
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if handle == "" {
		return "https://x.com/i/status/" + id
	}
	return "https://x.com/" + handle + "/status/" + id
}
