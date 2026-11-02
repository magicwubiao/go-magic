package tool

// video_backend_kling.go — 可灵（Kling）视频后端。
//
// 契约要点（2026-11 官方文档，https://app.klingai.com/cn/dev/document-api）：
//
//	鉴权    Authorization: Bearer <API_KEY>   ← 新版是控制台直接签发的单一 API Key，
//	                                          不是早期那套 AK/SK 现算 JWT，别照旧博客写
//	文生    POST {base}/text-to-video/{model}
//	        {"prompt": "...", "settings": {"resolution", "aspect_ratio", "duration"}}
//	图生    POST {base}/image-to-video/{model}
//	        {"contents": [{"type":"prompt","text":...},
//	                      {"type":"first_frame","url":...}], "settings": {...}}
//	查询    GET  {base}/tasks?task_ids={id}  → data[0].outputs[].url
//
// 三个容易踩的点：
//  1. 模型名在 **URL 路径**里，不在 body 里。换模型就是换路径。
//  2. 业务错误可能以 HTTP 200 + code!=0 返回，只看状态码会当成成功。
//  3. 新版查询接口是 /tasks?task_ids=（旧的 /v1/videos/{type}/{id} 是上一代路径，
//     两者不通用）。这里只实现新版，旧账号如仍只有老网关，用 base_url 覆盖。
//
// 首帧图只接受公网可达 URL：Kling 的 first_frame.url 不支持 data URI。
// 本地文件在这里明确报错并指向支持 data URI 的后端（volcengine / runway），
// 而不是丢一个本地路径过去换回一句看不懂的 400。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// klingPollInterval 查询间隔。定义成变量而不是常量，是为了测试能把它调到毫秒级
// —— 否则每个用例都要真的等 10 秒，加一个厂商就等于给测试套件加 10 秒。
var klingPollInterval = 10 * time.Second

type klingVideoBackend struct{}

func (klingVideoBackend) Name() string { return "kling" }

func (klingVideoBackend) Available(cfg *VideoGenConfig) bool { return videoBackendHasKey(cfg) }

func (klingVideoBackend) Generate(ctx context.Context, cfg *VideoGenConfig, req VideoRequest) (*VideoResult, error) {
	info, _ := ProviderInfo(MediaKindVideo, "kling")
	base := info.BaseURL(cfg.BaseURL)
	model := info.Model(req.Model)

	resolution := resolutionFor(map[string]string{
		"480p":  "720p", // Kling 没有 480p 档，向上取最近的一档
		"720p":  "720p",
		"1080p": "1080p",
	}, req.Resolution, "720p")
	duration := clampDuration(req.Duration, 3, 15)
	ratio := pickRatio(req.Ratio, info.Ratios, "16:9")

	headers := map[string]string{"Authorization": "Bearer " + cfg.APIKey}

	endpoint := base + "/text-to-video/" + url.PathEscape(model)
	var payload map[string]any

	if req.ImagePath != "" || req.ImageURL != "" {
		// 本地图：Kling 只认公网 URL，这里必须明确报错。
		// 措辞里**不要**劝模型"换一家支持本地文件的"：video_gen 只有一份凭据，
		// 换 provider 必然失败，而模型会一家一家试下去，刷出一屏 404。
		if req.ImageURL == "" {
			return nil, fmt.Errorf(
				"%s can only animate a first-frame image that is publicly reachable: pass image_url (http/https) "+
					"instead of image_path. Local files are not supported by this provider — host the frame "+
					"somewhere public, or generate the video without an image",
				info.Label)
		}
		endpoint = base + "/image-to-video/" + url.PathEscape(model)
		payload = map[string]any{
			"contents": []map[string]any{
				{"type": "prompt", "text": req.Prompt},
				{"type": "first_frame", "url": req.ImageURL},
			},
			"settings": map[string]any{
				"resolution": resolution,
				"duration":   duration,
			},
		}
	} else {
		payload = map[string]any{
			"prompt": req.Prompt,
			"settings": map[string]any{
				"resolution":   resolution,
				"aspect_ratio": ratio,
				"duration":     duration,
			},
		}
	}

	body, err := mediaRequest(ctx, cfg.Timeout, "POST", endpoint, payload, headers)
	if err != nil {
		return nil, withProviderContext(err, MediaKindVideo, "kling", model)
	}

	// HTTP 200 也可能是业务失败（code!=0），必须显式判。
	var submit struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &submit); err != nil {
		return nil, fmt.Errorf("failed to parse %s submit response: %w (body: %s)",
			info.Label, err, truncateRunes(string(body), 300))
	}
	if submit.Code != 0 {
		return nil, &mediaAPIError{
			Provider: "kling",
			Label:    info.Label,
			// 业务码不映射成 HTTP 码：编一份对不上的映射表，比原样透出更误导。
			Status:  400,
			Detail:  fmt.Sprintf("code=%d: %s", submit.Code, submit.Message),
			Model:   model,
			Console: info.ConsoleURL,
		}
	}
	if submit.Data.ID == "" {
		return nil, fmt.Errorf("%s accepted the task but returned no task id (body: %s)",
			info.Label, truncateRunes(string(body), 300))
	}

	videoURL, err := pollKlingTask(ctx, cfg, base, headers, submit.Data.ID, info.Label)
	if err != nil {
		return nil, err
	}
	return &VideoResult{
		URL:        videoURL,
		TaskID:     submit.Data.ID,
		Duration:   duration,
		Ratio:      ratio,
		Resolution: resolution,
		Model:      model,
	}, nil
}

// pollKlingTask 轮询 /tasks?task_ids=，直到 succeeded / failed。
//
// 用批查接口（一个 id 也是批量）而不是单任务路径：这是新版文档给出的唯一
// 查询入口，data 是数组，多任务时也是同一个形状。
func pollKlingTask(ctx context.Context, cfg *VideoGenConfig, base string, headers map[string]string, taskID, label string) (string, error) {
	endpoint := base + "/tasks?task_ids=" + url.QueryEscape(taskID)

	return pollUntilDone(ctx, klingPollInterval, func() (bool, string, error) {
		body, err := mediaRequest(ctx, cfg.Timeout, "GET", endpoint, nil, headers)
		if err != nil {
			return true, "", withProviderContext(err, MediaKindVideo, "kling", "")
		}

		var resp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    []struct {
				ID      string `json:"id"`
				Status  string `json:"status"`
				Message string `json:"message"`
				Outputs []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"outputs"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return true, "", fmt.Errorf("failed to parse %s poll response: %w (body: %s)",
				label, err, truncateRunes(string(body), 300))
		}
		if resp.Code != 0 {
			return true, "", &mediaAPIError{
				Provider: "kling", Label: label, Status: 400,
				Detail: fmt.Sprintf("code=%d: %s", resp.Code, resp.Message),
			}
		}
		if len(resp.Data) == 0 {
			// 刚提交的瞬间可能查不到：这不是错误，继续等。
			return false, "", nil
		}

		task := resp.Data[0]
		switch strings.ToLower(task.Status) {
		case "succeeded":
			for _, out := range task.Outputs {
				if out.Type == "video" && out.URL != "" {
					return true, out.URL, nil
				}
			}
			// 状态成功但没有视频产物：不能返回空 URL 让上层去下载一个空地址。
			for _, out := range task.Outputs {
				if out.URL != "" {
					return true, out.URL, nil
				}
			}
			return true, "", fmt.Errorf("%s task succeeded but returned no video URL", label)
		case "failed":
			reason := strings.TrimSpace(task.Message)
			if reason == "" {
				reason = "no reason provided"
			}
			return true, "", fmt.Errorf("%s task failed: %s", label, reason)
		default: // submitted / processing
			return false, "", nil
		}
	})
}

// pickRatio 在厂商支持的画幅集合里挑：命中就用，没命中回落默认值。
//
// 静默回落而不是报错，理由与时长钳制一致——3 分钟的生成任务不该死在
// "4:3 不支持"上；实际生效的值由 VideoResult 回传，工具层如实回显。
func pickRatio(requested string, supported []string, fallback string) string {
	value := strings.TrimSpace(requested)
	if value == "" {
		return fallback
	}
	for _, r := range supported {
		if strings.EqualFold(r, value) {
			return r
		}
	}
	return fallback
}
