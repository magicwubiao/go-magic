package tool

// video_backend_runway.go — Runway 视频后端。
//
// 契约要点（https://docs.dev.runwayml.com）：
//
//	鉴权   Authorization: Bearer <API_SECRET>
//	       X-Runway-Version: 2024-11-06   ← 必需，且是**日期**不是版本号。
//	                                       缺这个头会直接 400，而不是 401，很容易误判成参数错。
//	文生   POST {base}/v1/text_to_video   {"model","promptText","ratio","duration"}
//	图生   POST {base}/v1/image_to_video  {"model","promptImage","promptText","ratio","duration"}
//	查询   GET  {base}/v1/tasks/{id}      → output[0] 是签名的 MP4 地址
//
// 两处与其它厂商不同的语义，必须在这里吸收掉，不能漏给上层：
//   - ratio 是**像素比**（"1280:720"），不是 "16:9"；
//   - duration 只接受 5 或 10 秒，传 8 会被拒绝，所以用 snap 吸附到最近的档位。
//
// 首帧图走 data URI（Runway 支持，上限 5MB），所以 image_path 可用——这一点
// 比 Kling / dashscope 友好，值得在 catalog 的 Notes 里写清楚。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// runwayAPIVersion 是 Runway 要求钉死的 API 版本日期。
// 官方只发布过这一个值；换值前必须先去文档确认，写错等于全部请求 400。
const runwayAPIVersion = "2024-11-06"

// runwayMaxDataURIBytes data URI 输入上限（官方 5MB）。
// 注意是**编码前**的原始字节：base64 会膨胀 4/3，但限制针对的是文件本身。
const runwayMaxDataURIBytes = 5 << 20

// runwayPollInterval 轮询间隔，测试可调小（官方要求不低于 5 秒）。
var runwayPollInterval = 5 * time.Second

type runwayVideoBackend struct{}

func (runwayVideoBackend) Name() string { return "runway" }

func (runwayVideoBackend) Available(cfg *VideoGenConfig) bool { return videoBackendHasKey(cfg) }

func (runwayVideoBackend) Generate(ctx context.Context, cfg *VideoGenConfig, req VideoRequest) (*VideoResult, error) {
	info, _ := ProviderInfo(MediaKindVideo, "runway")
	base := info.BaseURL(cfg.BaseURL)
	model := info.Model(req.Model)

	// 时长只有 5 / 10 两档，吸附到最近的。
	duration := snapDuration(clampDuration(req.Duration, 5, 10), 5, 10)
	ratio, err := runwayRatio(req.Ratio, req.Resolution)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"Authorization":    "Bearer " + cfg.APIKey,
		"X-Runway-Version": runwayAPIVersion,
	}

	payload := map[string]any{
		"model":      model,
		"promptText": req.Prompt,
		"ratio":      ratio,
		"duration":   duration,
	}

	endpoint := base + "/v1/text_to_video"
	if req.ImagePath != "" || req.ImageURL != "" {
		imageRef := req.ImageURL
		if imageRef == "" {
			dataURI, derr := runwayFirstFrameDataURI(req.ImagePath, info.Label)
			if derr != nil {
				return nil, derr
			}
			imageRef = dataURI
		}
		payload["promptImage"] = imageRef
		endpoint = base + "/v1/image_to_video"
	}

	body, err := mediaRequest(ctx, cfg.Timeout, "POST", endpoint, payload, headers)
	if err != nil {
		return nil, withProviderContext(err, MediaKindVideo, "runway", model)
	}

	var submit struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &submit); err != nil {
		return nil, fmt.Errorf("failed to parse %s submit response: %w (body: %s)",
			info.Label, err, truncateRunes(string(body), 300))
	}
	if submit.ID == "" {
		return nil, fmt.Errorf("%s accepted the task but returned no task id (body: %s)",
			info.Label, truncateRunes(string(body), 300))
	}

	videoURL, err := pollRunwayTask(ctx, cfg, base, headers, submit.ID, info.Label)
	if err != nil {
		return nil, err
	}
	return &VideoResult{
		URL:        videoURL,
		TaskID:     submit.ID,
		Duration:   duration,
		Ratio:      ratio,
		Resolution: runwayResolutionLabel(req.Resolution),
		Model:      model,
	}, nil
}

// runwayRatio 把统一的画幅 + 分辨率映射成 Runway 的像素比。
func runwayRatio(ratio, resolution string) (string, error) {
	value := pickRatio(ratio, []string{"16:9", "9:16", "1:1"}, "16:9")
	high := strings.EqualFold(strings.TrimSpace(resolution), "1080p")

	switch value {
	case "16:9":
		if high {
			return "1920:1080", nil
		}
		return "1280:720", nil
	case "9:16":
		if high {
			return "1080:1920", nil
		}
		return "720:1280", nil
	case "1:1":
		return "960:960", nil
	}
	return "", fmt.Errorf("unsupported aspect ratio for Runway: %s", ratio)
}

// runwayResolutionLabel 回显实际生效的分辨率档位（Runway 由像素比决定，没有独立档位）。
func runwayResolutionLabel(resolution string) string {
	if strings.EqualFold(strings.TrimSpace(resolution), "1080p") {
		return "1080p"
	}
	return "720p"
}

// runwayFirstFrameDataURI 读本地图并转 data URI，超过 5MB 时给出可操作的建议。
func runwayFirstFrameDataURI(path, label string) (string, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to read first-frame image: %w", err)
	}
	if stat.Size() > runwayMaxDataURIBytes {
		return "", fmt.Errorf(
			"first-frame image is %.1f MB, but %s accepts at most %d MB as data URI; "+
				"resize it first, or upload it somewhere reachable and pass image_url",
			float64(stat.Size())/(1<<20), label, runwayMaxDataURIBytes>>20)
	}
	return localImageToDataURI(path)
}

func pollRunwayTask(ctx context.Context, cfg *VideoGenConfig, base string, headers map[string]string, taskID, label string) (string, error) {
	// Runway 明确要求同一个 task 的轮询间隔不低于 5 秒。
	endpoint := base + "/v1/tasks/" + taskID

	return pollUntilDone(ctx, runwayPollInterval, func() (bool, string, error) {
		body, err := mediaRequest(ctx, cfg.Timeout, "GET", endpoint, nil, headers)
		if err != nil {
			return true, "", withProviderContext(err, MediaKindVideo, "runway", "")
		}

		var resp struct {
			Status      string   `json:"status"`
			Output      []string `json:"output"`
			Failure     string   `json:"failure"`
			FailureCode string   `json:"failureCode"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return true, "", fmt.Errorf("failed to parse %s poll response: %w (body: %s)",
				label, err, truncateRunes(string(body), 300))
		}

		switch strings.ToUpper(resp.Status) {
		case "SUCCEEDED":
			for _, out := range resp.Output {
				if out != "" {
					return true, out, nil
				}
			}
			return true, "", fmt.Errorf("%s task succeeded but returned no output URL", label)
		case "FAILED", "CANCELLED":
			detail := strings.TrimSpace(resp.Failure)
			if detail == "" {
				detail = "no reason provided"
			}
			if resp.FailureCode != "" {
				detail = fmt.Sprintf("%s (code=%s)", detail, resp.FailureCode)
			}
			return true, "", fmt.Errorf("%s task %s: %s", label, strings.ToLower(resp.Status), detail)
		default: // PENDING / THROTTLED / RUNNING
			return false, "", nil
		}
	})
}
