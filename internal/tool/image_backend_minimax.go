package tool

// image_backend_minimax.go — MiniMax 图像生成后端（image-01）。
//
// 契约要点（https://platform.minimax.io/docs/api-reference/image-generation-t2i）：
//
//	POST {base}/v1/image_generation
//	{"model":"image-01","prompt":"...","aspect_ratio":"1:1","n":1,
//	 "response_format":"url","prompt_optimizer":true}
//	→ {"data":{"image_urls":["https://..."]},"base_resp":{"status_code":0}}
//
// 两个与别家不同的地方：
//   - 画幅用 aspect_ratio（"16:9"），不接受 1024x1024 这种像素写法。工具层传进来
//     的 size 是像素，必须在这里换成语义画幅；像素由模型自己定（image-01 出 1024 级）。
//   - 失败可能是 HTTP 200 + base_resp.status_code != 0，只看状态码会当成成功。
//
// 与视频后端一致：只把 URL 交回内核，下载与落盘由 downloadAndSaveImages 完成。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type minimaxImageBackend struct{}

func (minimaxImageBackend) Name() string { return "minimax" }

func (minimaxImageBackend) Available(cfg *ImageGenConfig) bool { return imageBackendHasKey(cfg) }

func (minimaxImageBackend) Generate(ctx context.Context, cfg *ImageGenConfig, req ImageRequest) (*ImageGenerationResult, error) {
	info, _ := ProviderInfo(MediaKindImage, "minimax")
	base := info.BaseURL(cfg.BaseURL)
	model := info.Model(cfg.Model)

	count := req.Count
	if count <= 0 {
		count = 1
	}
	if count > 9 {
		// image-01 单次最多 9 张。钳到上限而不是报错：用户要 12 张，给 9 张
		// 比整单失败有用，工具层会把实际张数写进结果。
		count = 9
	}

	payload := map[string]any{
		"model":            model,
		"prompt":           req.Prompt,
		"aspect_ratio":     minimaxAspectRatio(req.Size, info.Ratios),
		"n":                count,
		"response_format":  "url",
		"prompt_optimizer": true,
	}
	if req.Seed != nil {
		payload["seed"] = *req.Seed
	}

	body, err := mediaRequest(ctx, cfg.Timeout, http.MethodPost, base+"/v1/image_generation", payload,
		map[string]string{"Authorization": "Bearer " + cfg.APIKey})
	if err != nil {
		return nil, withProviderContext(err, MediaKindImage, "minimax", model)
	}

	var resp struct {
		Data struct {
			ImageURLs []string `json:"image_urls"`
		} `json:"data"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w (body: %s)",
			info.Label, err, truncateRunes(string(body), 300))
	}
	if resp.BaseResp.StatusCode != 0 {
		return nil, &mediaAPIError{
			Provider: "minimax", Label: info.Label, Status: 400,
			Detail:  fmt.Sprintf("status_code=%d: %s", resp.BaseResp.StatusCode, resp.BaseResp.StatusMsg),
			Model:   model,
			Console: info.ConsoleURL,
		}
	}
	if len(resp.Data.ImageURLs) == 0 {
		// 返回 0 张图 + nil error 会让模型以为生成成功，用户拿不到任何东西。
		return nil, fmt.Errorf("%s returned no images (body: %s)", info.Label, truncateRunes(string(body), 300))
	}

	result := &ImageGenerationResult{Images: make([]ImageInfo, 0, len(resp.Data.ImageURLs))}
	for _, u := range resp.Data.ImageURLs {
		if u == "" {
			continue
		}
		result.Images = append(result.Images, ImageInfo{URL: u, Format: "png"})
	}
	if len(result.Images) == 0 {
		return nil, fmt.Errorf("%s returned only empty image entries", info.Label)
	}
	return result, nil
}

// minimaxAspectRatio 把像素尺寸换成 MiniMax 的语义画幅。
//
// 只按长宽比判断，不看具体像素：1024x1024 与 512x512 都是 1:1，没必要区分。
func minimaxAspectRatio(size string, supported []string) string {
	fallback := "1:1"
	width, height := parsePixelSize(size)
	if width <= 0 || height <= 0 {
		return fallback
	}

	var ratio string
	switch {
	case width == height:
		ratio = "1:1"
	case width*9 == height*16:
		ratio = "16:9"
	case width*16 == height*9:
		ratio = "9:16"
	case width*3 == height*4:
		ratio = "4:3"
	case width*4 == height*3:
		ratio = "3:4"
	default:
		// 非标准尺寸按"横还是竖"归到最接近的画幅，而不是硬塞 1:1。
		if width > height {
			ratio = "16:9"
		} else {
			ratio = "9:16"
		}
	}
	return pickRatio(ratio, supported, fallback)
}

// parsePixelSize 解析 "1024x1024" / "1792*1024" 这类写法。
func parsePixelSize(size string) (int, int) {
	raw := strings.ToLower(strings.TrimSpace(size))
	if raw == "" {
		return 0, 0
	}
	sep := "x"
	if !strings.Contains(raw, "x") && strings.Contains(raw, "*") {
		sep = "*"
	}
	parts := strings.SplitN(raw, sep, 2)
	if len(parts) != 2 {
		return 0, 0
	}
	var w, h int
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &w); err != nil {
		return 0, 0
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &h); err != nil {
		return 0, 0
	}
	return w, h
}
