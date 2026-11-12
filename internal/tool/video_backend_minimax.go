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
// 首帧图有两条路，且**都受支持**：
//   - 公网地址（image_url）：要求外部可访问，适合已经有图床/OSS 的场景；
//   - Base64 Data URL（image_path）：v2 的 image_url.url 明确接受
//     "data:image/jpeg;base64,..."。格式 JPG/JPEG/PNG/WEBP/HEIC/HEIF，单图 ≤30MB，
//     整个请求体 ≤64MB（base64 膨胀 4/3，所以本地图上限按 30MB 卡）。
//     这条让"本地图 → 视频"不必先上传图床再来一遍。
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

// minimaxMaxImageBytes 单张首帧图上限（官方 30MB）。变量而非常量：测试用
// 小值覆盖，避免为了测上限真去写一个 30MB 的临时文件。
var minimaxMaxImageBytes int64 = 30 << 20

type minimaxVideoBackend struct{}

func (minimaxVideoBackend) Name() string { return "minimax" }

func (minimaxVideoBackend) Available(cfg *VideoGenConfig) bool { return videoBackendHasKey(cfg) }

func (minimaxVideoBackend) Generate(ctx context.Context, cfg *VideoGenConfig, req VideoRequest) (*VideoResult, error) {
	info, _ := ProviderInfo(MediaKindVideo, "minimax")
	base := info.BaseURL(cfg.BaseURL)
	model := info.Model(req.Model)

	// 同时接受通用档位（480p/720p/1080p，工具 schema 面向模型就是这套）和
	// MiniMax 原生档位（768p/2k，设置页下拉直接展示这套）。
	resolution := resolutionFor(map[string]string{
		"480p":  "768P",
		"720p":  "768P",
		"1080p": "2K",
		"768p":  "768P",
		"2k":    "2K",
	}, req.Resolution, "768P")
	// H3 是 4-15 秒（上一代 Hailuo 才是 5-10）：按 5-10 钳会把 4 秒和
	// 11-15 秒整段砍掉。H3-Max 下限 5 秒由 durationForModel 提前处理。
	duration := clampDuration(req.Duration, 4, 15)

	content := []map[string]any{{"type": "text", "text": req.Prompt}}
	ratio := pickRatio(req.Ratio, info.Ratios, "16:9")

	if req.ImagePath != "" || req.ImageURL != "" {
		ref, err := minimaxImageRef(ctx, req.ImagePath, req.ImageURL)
		if err != nil {
			return nil, err
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]string{"url": ref},
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

// minimaxImageRef 把首帧图变成 v2 能吃的引用。
//
// 优先用调用方给的公网地址；没有就地把本地图内联成 Base64 Data URL——
// v2 的 image_url.url 官方支持 data:image/...;base64,（单图 ≤30MB、请求体 ≤64MB），
// 所以图生视频不必先把图传到图床。
func minimaxImageRef(ctx context.Context, imagePath, imageURL string) (string, error) {
	if imageURL != "" {
		return imageURL, nil
	}
	data, mime, err := readInlineImage(ctx, imagePath, minimaxMaxImageBytes, minimaxImageExts, "MiniMax")
	if err != nil {
		return "", err
	}
	return imageDataURI(mime, data), nil
}

// minimaxImageExts 官方支持的图片格式（比其它厂商多 HEIC/HEIF）。
var minimaxImageExts = []string{".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif"}

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
