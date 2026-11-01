package tool

// video_backend_minimax.go — MiniMax 海螺（H3）视频后端。
//
// 契约要点（https://platform.minimax.io/docs/guides/video-generation）：
//
//	提交   POST {base}/v2/video_generation
//	       {"model":"MiniMax-H3",
//	        "content":[{"type":"text","text":...}]          // 文生
//	        "content":[{"type":"text",...},
//	                   {"type":"image_url","image_url":{"url":...},"role":"first_frame"}], // 图生
//	        "duration":5, "resolution":"2K", "ratio":"16:9"}
//	查询   GET  {base}/v2/query/video_generation/{task_id}
//	       → task.status = succeeded / failed / cancelled / expired
//	       → task.content.url 就是成片地址（**不需要**再换 file_id）
//
// 两处与 v1 的差别，照着老教程写会全线报错：
//   - v1 是 /v1/video_generation + /v1/query/video_generation?task_id= + /v1/files/retrieve
//     三步换链；v2 一步到位返回 content.url。这里只走 v2。
//   - v1 用 T2V-01 / I2V-01 这类模型名；v2 是 MiniMax-H3 系列。
//
// 图生视频时 ratio 必须是 "adaptive"：成片画幅由输入图决定，传 16:9 反而会 400。
//
// 域名分国内外：默认 api.minimax.io（海外），中国大陆账号用 api.minimaxi.com，
// 通过 base_url 覆盖——两边 API 形状一致，只有域名不同。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// minimaxPollInterval 轮询间隔，测试可调小（官方推荐 10 秒一次）。
var minimaxPollInterval = 10 * time.Second

type minimaxVideoBackend struct{}

func (minimaxVideoBackend) Name() string { return "minimax" }

func (minimaxVideoBackend) Available(cfg *VideoGenConfig) bool { return videoBackendHasKey(cfg) }

func (minimaxVideoBackend) Generate(ctx context.Context, cfg *VideoGenConfig, req VideoRequest) (*VideoResult, error) {
	info, _ := ProviderInfo(MediaKindVideo, "minimax")
	base := info.BaseURL(cfg.BaseURL)
	model := info.Model(req.Model)

	resolution := resolutionFor(map[string]string{
		"480p":  "768P",
		"720p":  "768P",
		"1080p": "2K",
	}, req.Resolution, "768P")
	duration := clampDuration(req.Duration, 5, 10)

	content := []map[string]any{{"type": "text", "text": req.Prompt}}
	ratio := pickRatio(req.Ratio, info.Ratios, "16:9")

	if req.ImagePath != "" || req.ImageURL != "" {
		if req.ImageURL == "" {
			return nil, fmt.Errorf(
				"%s needs a publicly reachable first-frame image: pass image_url instead of image_path, "+
					"or switch provider to volcengine / runway (both accept local files)",
				info.Label)
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]string{"url": req.ImageURL},
			"role":      "first_frame",
		})
		// 图生视频的画幅由首帧图决定，官方要求传 adaptive。
		ratio = "adaptive"
	}

	payload := map[string]any{
		"model":      model,
		"content":    content,
		"duration":   duration,
		"resolution": resolution,
		"ratio":      ratio,
	}

	body, err := mediaRequest(ctx, cfg.Timeout, "POST", base+"/v2/video_generation", payload,
		map[string]string{"Authorization": "Bearer " + cfg.APIKey})
	if err != nil {
		return nil, withProviderContext(err, MediaKindVideo, "minimax", model)
	}

	var submit struct {
		TaskID string `json:"task_id"`
		Task   struct {
			ID string `json:"id"`
		} `json:"task"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(body, &submit); err != nil {
		return nil, fmt.Errorf("failed to parse %s submit response: %w (body: %s)",
			info.Label, err, truncateRunes(string(body), 300))
	}
	if submit.BaseResp.StatusCode != 0 {
		return nil, &mediaAPIError{
			Provider: "minimax", Label: info.Label, Status: 400,
			Detail:  fmt.Sprintf("status_code=%d: %s", submit.BaseResp.StatusCode, submit.BaseResp.StatusMsg),
			Model:   model,
			Console: info.ConsoleURL,
		}
	}

	taskID := submit.TaskID
	if taskID == "" {
		taskID = submit.Task.ID
	}
	if taskID == "" {
		return nil, fmt.Errorf("%s accepted the task but returned no task id (body: %s)",
			info.Label, truncateRunes(string(body), 300))
	}

	videoURL, err := pollMiniMaxTask(ctx, cfg, base, taskID, info.Label)
	if err != nil {
		return nil, err
	}
	return &VideoResult{
		URL:        videoURL,
		TaskID:     taskID,
		Duration:   duration,
		Ratio:      ratio,
		Resolution: strings.ToLower(resolution),
		Model:      model,
	}, nil
}

func pollMiniMaxTask(ctx context.Context, cfg *VideoGenConfig, base, taskID, label string) (string, error) {
	endpoint := base + "/v2/query/video_generation/" + url.PathEscape(taskID)
	headers := map[string]string{"Authorization": "Bearer " + cfg.APIKey}
	// 官方推荐 10 秒一次；比这更快只会打满状态接口，不会更早出片。
	return pollUntilDone(ctx, minimaxPollInterval, func() (bool, string, error) {
		body, err := mediaRequest(ctx, cfg.Timeout, "GET", endpoint, nil, headers)
		if err != nil {
			return true, "", withProviderContext(err, MediaKindVideo, "minimax", "")
		}

		var resp struct {
			Task struct {
				Status  string `json:"status"`
				Content *struct {
					URL string `json:"url"`
				} `json:"content"`
				Error *struct {
					Code    any    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			} `json:"task"`
			BaseResp struct {
				StatusCode int    `json:"status_code"`
				StatusMsg  string `json:"status_msg"`
			} `json:"base_resp"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return true, "", fmt.Errorf("failed to parse %s poll response: %w (body: %s)",
				label, err, truncateRunes(string(body), 300))
		}
		if resp.BaseResp.StatusCode != 0 {
			return true, "", &mediaAPIError{
				Provider: "minimax", Label: label, Status: 400,
				Detail: fmt.Sprintf("status_code=%d: %s", resp.BaseResp.StatusCode, resp.BaseResp.StatusMsg),
			}
		}

		switch strings.ToLower(resp.Task.Status) {
		case "succeeded":
			if resp.Task.Content == nil || resp.Task.Content.URL == "" {
				return true, "", fmt.Errorf("%s task succeeded but returned no video URL", label)
			}
			return true, resp.Task.Content.URL, nil
		case "failed", "cancelled", "expired":
			reason := "no reason provided"
			if resp.Task.Error != nil && strings.TrimSpace(resp.Task.Error.Message) != "" {
				reason = resp.Task.Error.Message
			}
			// 这三个是终态。绝不自动重试：一次重试就是一次真实扣费。
			return true, "", fmt.Errorf("%s task %s: %s", label, strings.ToLower(resp.Task.Status), reason)
		default: // queued / running / preparing
			return false, "", nil
		}
	})
}
