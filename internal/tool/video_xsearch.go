package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// VideoGenerateTool generates videos using pluggable provider backends
type VideoGenerateTool struct {
	BaseTool
	config *VideoGenConfig
}

// VideoGenConfig holds configuration for video generation
type VideoGenConfig struct {
	Provider        string `json:"provider"` // "replicate", "fal", "stability", "local"
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`            // e.g., "stable-video-diffusion", "animate-diff"
	DefaultDuration int    `json:"default_duration"` // seconds
	OutputDir       string `json:"output_dir"`
}

// VideoGenRequest represents a video generation request
type VideoGenRequest struct {
	Prompt   string `json:"prompt"`
	ImageURL string `json:"image_url,omitempty"` // optional: image-to-video
	Duration int    `json:"duration,omitempty"`  // seconds
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	FPS      int    `json:"fps,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// VideoGenResult represents the result of video generation
type VideoGenResult struct {
	Success   bool   `json:"success"`
	VideoPath string `json:"video_path,omitempty"`
	VideoURL  string `json:"video_url,omitempty"`
	Duration  int    `json:"duration"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FPS       int    `json:"fps"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Error     string `json:"error,omitempty"`
}

// NewVideoGenerateTool creates a new video generation tool
func NewVideoGenerateTool(config *VideoGenConfig) *VideoGenerateTool {
	if config == nil {
		config = &VideoGenConfig{
			Provider:        "replicate",
			Model:           "stable-video-diffusion",
			DefaultDuration: 4,
			OutputDir:       "./output/videos",
		}
	}
	if config.OutputDir == "" {
		config.OutputDir = "./output/videos"
	}

	return &VideoGenerateTool{
		BaseTool: *NewBaseTool(
			"video_generate",
			"Generate videos from text prompts or images. Supports multiple providers (Replicate, Fal.ai, Stability AI) via pluggable backends.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text description of the video to generate",
					},
					"image_url": map[string]interface{}{
						"type":        "string",
						"description": "Optional: source image URL for image-to-video generation",
					},
					"duration": map[string]interface{}{
						"type":        "integer",
						"description": "Video duration in seconds (default: 4)",
					},
					"width": map[string]interface{}{
						"type":        "integer",
						"description": "Video width in pixels (default: 1024)",
					},
					"height": map[string]interface{}{
						"type":        "integer",
						"description": "Video height in pixels (default: 576)",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Provider to use: replicate, fal, stability",
						"enum":        []string{"replicate", "fal", "stability"},
					},
				},
				"required": []string{"prompt"},
			},
		),
		config: config,
	}
}

// Execute generates a video based on the request parameters
func (t *VideoGenerateTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	req := &VideoGenRequest{
		Prompt:   prompt,
		Duration: t.config.DefaultDuration,
		Width:    1024,
		Height:   576,
		FPS:      24,
		Provider: t.config.Provider,
		Model:    t.config.Model,
	}

	if v, ok := params["image_url"].(string); ok && v != "" {
		req.ImageURL = v
	}
	if v, ok := params["duration"].(float64); ok && v > 0 {
		req.Duration = int(v)
	}
	if v, ok := params["width"].(float64); ok && v > 0 {
		req.Width = int(v)
	}
	if v, ok := params["height"].(float64); ok && v > 0 {
		req.Height = int(v)
	}
	if v, ok := params["provider"].(string); ok && v != "" {
		req.Provider = v
	}

	// Ensure output directory exists
	os.MkdirAll(t.config.OutputDir, 0755)

	switch req.Provider {
	case "replicate":
		return t.generateReplicate(ctx, req)
	case "fal":
		return t.generateFal(ctx, req)
	case "stability":
		return t.generateStability(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", req.Provider)
	}
}

func (t *VideoGenerateTool) generateReplicate(ctx context.Context, req *VideoGenRequest) (*VideoGenResult, error) {
	if t.config.APIKey == "" {
		return nil, fmt.Errorf("Replicate API key not configured")
	}

	// Call Replicate API
	payload := map[string]interface{}{
		"input": map[string]interface{}{
			"prompt":   req.Prompt,
			"width":    req.Width,
			"height":   req.Height,
			"duration": req.Duration,
		},
	}

	body, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", "https://api.replicate.com/v1/predictions", strings.NewReader(string(body)))
	httpReq.Header.Set("Authorization", "Bearer "+t.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Replicate API error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Replicate API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	outputURL := ""
	if output, ok := result["output"].([]interface{}); ok && len(output) > 0 {
		outputURL, _ = output[0].(string)
	} else if output, ok := result["output"].(string); ok {
		outputURL = output
	}

	return &VideoGenResult{
		Success:  outputURL != "",
		VideoURL: outputURL,
		Duration: req.Duration,
		Width:    req.Width,
		Height:   req.Height,
		FPS:      req.FPS,
		Provider: "replicate",
		Model:    t.config.Model,
	}, nil
}

func (t *VideoGenerateTool) generateFal(ctx context.Context, req *VideoGenRequest) (*VideoGenResult, error) {
	if t.config.APIKey == "" {
		return nil, fmt.Errorf("Fal.ai API key not configured")
	}

	payload := map[string]interface{}{
		"prompt":    req.Prompt,
		"image_url": req.ImageURL,
		"duration":  req.Duration,
	}
	body, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", "https://queue.fal.run/fal-ai/fast-animatediff/text-to-video", strings.NewReader(string(body)))
	httpReq.Header.Set("Authorization", "Key "+t.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Fal.ai API error: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	videoURL := ""
	if v, ok := result["video"].(map[string]interface{}); ok {
		videoURL, _ = v["url"].(string)
	}

	return &VideoGenResult{
		Success:  videoURL != "",
		VideoURL: videoURL,
		Duration: req.Duration,
		Width:    req.Width,
		Height:   req.Height,
		FPS:      req.FPS,
		Provider: "fal",
		Model:    "animate-diff",
	}, nil
}

func (t *VideoGenerateTool) generateStability(ctx context.Context, req *VideoGenRequest) (*VideoGenResult, error) {
	if t.config.APIKey == "" {
		return nil, fmt.Errorf("Stability AI API key not configured")
	}

	payload := map[string]interface{}{
		"text_prompts": []map[string]interface{}{
			{"text": req.Prompt, "weight": 1.0},
		},
		"width":  req.Width,
		"height": req.Height,
	}
	body, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", "https://api.stability.ai/v2alpha/generation/image-to-video", strings.NewReader(string(body)))
	httpReq.Header.Set("Authorization", "Bearer "+t.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Stability AI API error: %v", err)
	}
	defer resp.Body.Close()

	return &VideoGenResult{
		Success:  resp.StatusCode == 200,
		Duration: req.Duration,
		Width:    req.Width,
		Height:   req.Height,
		FPS:      req.FPS,
		Provider: "stability",
		Model:    "stable-video-diffusion",
	}, nil
}

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
		if includes, ok := result["includes"].(map[string]interface{}); ok {
			if users, ok := includes["users"].([]interface{}); ok {
				for _, u := range users {
					if user, ok := u.(map[string]interface{}); ok {
						id, _ := user["id"].(string)
						name, _ := user["name"].(string)
						username, _ := user["username"].(string)
						authors[id] = name + " (@" + username + ")"
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
					URL:       fmt.Sprintf("https://x.com/%s/status/%s", strings.TrimPrefix(author, "@"), id),
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
