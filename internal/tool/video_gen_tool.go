package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/util"
)

// video_gen_tool.go — 视频生成工具（video_generate）。
//
// 历史包袱：旧版 video_generate 有三个 provider（Replicate / FAL / Stability），
// 全是坏的（Replicate 少 version 字段且不轮询、FAL 队列端点已下线、Stability
// 必须有输入图），整只工具被删。本文件是重写版，只接**已核实过官方文档**的
// 异步任务型 API，且提交后由工具内部轮询到出片为止，不再出现"只提交不查询"：
//
//   - dashscope   阿里云百炼（通义万相 wan 系列，文生视频 + 图生视频）
//     POST /api/v1/services/aigc/{video-generation|image2video}/video-synthesis
//     （必须带 X-DashScope-Async: enable），随后轮询 GET /api/v1/tasks/{task_id}；
//   - volcengine  火山方舟（Seedance 系列）
//     POST /api/v3/contents/generations/tasks，轮询 GET .../tasks/{id}。
//
// 两个 provider 都是：提交 → 每 10s 轮询 → 成功后下载 MP4 落盘（temp+rename）。
// 视频 URL 有效期均只有 24 小时，所以必须当场下载，不能只把 URL 还给模型。
//
// 视频生成通常耗时 1~5 分钟，工具超时在 RegisterMediaTools 里设为 600s。

// VideoProvider 视频生成提供商类型
type VideoProvider string

const (
	// ProviderDashScope 阿里云百炼（通义万相 wan 系列）
	ProviderDashScope VideoProvider = "dashscope"
	// ProviderVolcengine 火山方舟（Seedance 系列）
	ProviderVolcengine VideoProvider = "volcengine"
)

// VideoGenConfig 视频生成配置（与 ImageGenConfig 同构：config.json video_gen 段 + 环境变量覆盖）
type VideoGenConfig struct {
	Provider          VideoProvider `json:"provider"`
	APIKey            string        `json:"api_key"`
	BaseURL           string        `json:"base_url"`
	Model             string        `json:"model"`
	DefaultDuration   int           `json:"default_duration"`
	DefaultRatio      string        `json:"default_ratio"`
	DefaultResolution string        `json:"default_resolution"`
	OutputDirectory   string        `json:"output_directory"`
	Timeout           time.Duration `json:"timeout"`
}

// DefaultVideoGenConfig 返回默认视频生成配置
func DefaultVideoGenConfig() *VideoGenConfig {
	return &VideoGenConfig{
		Provider:          ProviderDashScope,
		Model:             "wan2.2-t2v-plus",
		DefaultDuration:   5,
		DefaultRatio:      "16:9",
		DefaultResolution: "720p",
		OutputDirectory:   "./generated_videos",
		Timeout:           60 * time.Second,
	}
}

// VideoGenerationTool 视频生成工具
type VideoGenerationTool struct {
	BaseTool
	config *VideoGenConfig
}

// NewVideoGenerationTool 创建视频生成工具
func NewVideoGenerationTool(config *VideoGenConfig) *VideoGenerationTool {
	if config == nil {
		config = DefaultVideoGenConfig()
	}

	return &VideoGenerationTool{
		BaseTool: *NewBaseTool(
			"video_generate",
			"Generate short videos from a text prompt (and optionally a first-frame image) using AI. "+
				"Providers: dashscope (Alibaba wan, text-to-video & image-to-video) and volcengine (Seedance). "+
				"Generation typically takes 1-5 minutes; the tool polls the provider and saves the MP4 locally.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Detailed description of the video: subject, motion, camera movement, lighting. Be concrete (e.g. 'slow dolly-in on a kitten running through moonlit grass') instead of grandiose.",
					},
					"image_url": map[string]interface{}{
						"type":        "string",
						"description": "Public http(s) URL of a first-frame image to animate (image-to-video). Optional; omit for text-to-video.",
					},
					"image_path": map[string]interface{}{
						"type":        "string",
						"description": "Local path of a first-frame image for image-to-video. Sent as a base64 data URI (supported by volcengine). For dashscope use image_url with a publicly reachable address instead.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Provider model override, e.g. wan2.2-t2v-plus / wan2.2-i2v-plus (dashscope) or doubao-seedance-1-0-pro-250528 (volcengine). Default comes from config.",
					},
					"duration": map[string]interface{}{
						"type":        "number",
						"description": "Video length in seconds (3-15). Default 5.",
					},
					"ratio": map[string]interface{}{
						"type":        "string",
						"description": "Aspect ratio. dashscope supports 16:9, 9:16 (480p-1080p) and 1:1 (720p only); volcengine supports 16:9, 9:16, 1:1, 4:3, 3:4, 21:9.",
						"enum":        []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
						"default":     "16:9",
					},
					"resolution": map[string]interface{}{
						"type":        "string",
						"description": "Resolution tier: 480p, 720p (default) or 1080p.",
						"enum":        []string{"480p", "720p", "1080p"},
						"default":     "720p",
					},
					"negative_prompt": map[string]interface{}{
						"type":        "string",
						"description": "Content to avoid (dashscope only, optional)",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Custom output file path for the downloaded MP4 (optional)",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Override default provider: dashscope or volcengine",
						"enum":        []string{"dashscope", "volcengine"},
					},
				},
				"required": []string{"prompt"},
			},
		),
		config: config,
	}
}

// ValidateParams 验证参数
func (t *VideoGenerationTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 执行视频生成：提交异步任务 → 轮询 → 下载落盘
func (t *VideoGenerationTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	if t.config.APIKey == "" {
		return map[string]interface{}{
			"status":             "not_configured",
			"prompt":             prompt,
			"message":            "Video generation API key not configured.",
			"configuration_help": "Add a 'video_gen' section to config.json (api_key + provider: dashscope or volcengine), or set VIDEO_GEN_API_KEY.",
		}, nil
	}

	// 图生视频输入：优先 image_url（公网地址），其次本地 image_path。
	imageURL := ""
	if v, ok := params["image_url"].(string); ok {
		imageURL = strings.TrimSpace(v)
	}
	imagePath := ""
	if v, ok := params["image_path"].(string); ok {
		imagePath = strings.TrimSpace(v)
	}

	// 可选参数
	duration := t.config.DefaultDuration
	if d, ok := params["duration"].(float64); ok && d > 0 {
		duration = int(d)
	}
	if duration < 3 {
		duration = 3
	}
	if duration > 15 {
		duration = 15
	}

	ratio := t.config.DefaultRatio
	if r, ok := params["ratio"].(string); ok && r != "" {
		ratio = r
	}
	resolution := t.config.DefaultResolution
	if r, ok := params["resolution"].(string); ok && r != "" {
		resolution = r
	}
	negativePrompt := ""
	if np, ok := params["negative_prompt"].(string); ok {
		negativePrompt = np
	}
	model := t.config.Model
	if m, ok := params["model"].(string); ok && m != "" {
		model = m
	}
	outputPath := ""
	if op, ok := params["output_path"].(string); ok {
		outputPath = op
	}

	provider := t.config.Provider
	if p, ok := params["provider"].(string); ok && p != "" {
		provider = VideoProvider(p)
	}

	// 本地图片按 provider 能力处理：volcengine 支持 base64 data URI；
	// dashscope 的 img_url 只收公网地址/OSS，本地文件直接给出明确报错。
	if imagePath != "" {
		resolved, rerr := resolveLocalMedia(ctx, imagePath)
		if rerr != nil {
			return nil, rerr
		}
		if provider == ProviderVolcengine {
			dataURI, derr := localImageToDataURI(resolved)
			if derr != nil {
				return nil, derr
			}
			imageURL = dataURI
		} else {
			return nil, fmt.Errorf(
				"provider %s requires a publicly reachable image_url for image-to-video; "+
					"upload the file somewhere accessible or switch provider to volcengine, which accepts local images",
				provider)
		}
	}

	var videoURL, taskID string
	var err error

	switch provider {
	case ProviderDashScope:
		videoURL, taskID, err = t.runDashScope(ctx, prompt, negativePrompt, imageURL, model, duration, ratio, resolution)
	case ProviderVolcengine:
		videoURL, taskID, err = t.runVolcengine(ctx, prompt, imageURL, model, duration, ratio, resolution)
	default:
		return nil, fmt.Errorf("unsupported video provider: %s", provider)
	}
	if err != nil {
		return nil, fmt.Errorf("video generation failed: %w", err)
	}

	// 视频 URL 24 小时过期，必须立刻下载保存
	savedPath, err := t.downloadAndSaveVideo(ctx, videoURL, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to save video: %w", err)
	}

	result := map[string]interface{}{
		"status":     "success",
		"provider":   provider,
		"model":      model,
		"task_id":    taskID,
		"prompt":     prompt,
		"duration":   duration,
		"ratio":      ratio,
		"resolution": resolution,
		"video_path": savedPath,
		// 原始 URL 一并返回，方便人工复核；注意 24h 过期
		"video_url": videoURL,
	}
	if imageURL != "" && !strings.HasPrefix(imageURL, "data:") {
		result["image_url"] = imageURL
	}
	return result, nil
}

// ---- 阿里云百炼（DashScope / wan 系列）----

// runDashScope 提交百炼视频生成任务并轮询到出片。
// 文生视频走 video-generation 端点，带首帧图时走 image2video 端点。
func (t *VideoGenerationTool) runDashScope(ctx context.Context, prompt, negativePrompt, imageURL, model string, duration int, ratio, resolution string) (videoURL, taskID string, err error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/api/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// 有首帧图时切 i2v 端点；若用户没显式指定 i2v 模型，把默认 t2v 模型名替换过去
	endpoint := "/services/aigc/video-generation/video-synthesis"
	if imageURL != "" && strings.Contains(model, "-t2v-") {
		model = strings.Replace(model, "-t2v-", "-i2v-", 1)
	}
	if imageURL != "" {
		endpoint = "/services/aigc/image2video/video-synthesis"
	}

	size, serr := dashScopeSize(ratio, resolution)
	if serr != nil {
		return "", "", serr
	}

	input := map[string]interface{}{"prompt": prompt}
	if negativePrompt != "" {
		input["negative_prompt"] = negativePrompt
	}
	if imageURL != "" {
		input["img_url"] = imageURL
	}

	reqBody := map[string]interface{}{
		"model":      model,
		"input":      input,
		"parameters": map[string]interface{}{"size": size},
	}

	taskID, err = t.postDashScopeTask(ctx, baseURL+endpoint, reqBody)
	if err != nil {
		return "", "", err
	}

	videoURL, err = t.pollDashScopeTask(ctx, baseURL, taskID)
	return videoURL, taskID, err
}

// dashScopeSize 把 ratio+resolution 映射成百炼要求的 "宽*高" 字符串。
// 百炼只认具体像素（如 1280*720），不认 16:9 或 "720P" 这类写法。
func dashScopeSize(ratio, resolution string) (string, error) {
	switch {
	case ratio == "16:9" && resolution == "480p":
		return "832*480", nil
	case ratio == "16:9" && resolution == "720p":
		return "1280*720", nil
	case ratio == "16:9" && resolution == "1080p":
		return "1920*1080", nil
	case ratio == "9:16" && resolution == "480p":
		return "480*832", nil
	case ratio == "9:16" && resolution == "720p":
		return "720*1280", nil
	case ratio == "9:16" && resolution == "1080p":
		return "1080*1920", nil
	case ratio == "1:1" && resolution == "720p":
		return "960*960", nil
	}
	return "", fmt.Errorf(
		"unsupported ratio/resolution for dashscope: %s/%s (supported: 16:9 and 9:16 at 480p/720p/1080p, 1:1 at 720p)",
		ratio, resolution)
}

// postDashScopeTask 提交任务，返回 task_id。必须带 X-DashScope-Async: enable，
// 否则报 "current user api does not support synchronous calls"。
func (t *VideoGenerationTool) postDashScopeTask(ctx context.Context, url string, body map[string]interface{}) (string, error) {
	reqJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)
	req.Header.Set("X-DashScope-Async", "enable")

	respBody, err := t.doHTTP(ctx, req)
	if err != nil {
		return "", err
	}

	var resp struct {
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("failed to parse submit response: %w (body: %s)", err, truncateForLog(respBody))
	}
	if resp.Output.TaskID == "" {
		return "", fmt.Errorf("task submit rejected (code=%s): %s", resp.Code, resp.Message)
	}
	return resp.Output.TaskID, nil
}

// pollDashScopeTask 轮询百炼任务直到 SUCCEEDED/FAILED（PENDING → RUNNING → SUCCEEDED/FAILED）
func (t *VideoGenerationTool) pollDashScopeTask(ctx context.Context, baseURL, taskID string) (string, error) {
	url := baseURL + "/tasks/" + taskID
	return pollUntilDone(ctx, 10*time.Second, func() (done bool, videoURL string, err error) {
		req, rerr := http.NewRequestWithContext(ctx, "GET", url, nil)
		if rerr != nil {
			return true, "", fmt.Errorf("failed to create poll request: %w", rerr)
		}
		req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

		respBody, rerr := t.doHTTP(ctx, req)
		if rerr != nil {
			return true, "", rerr
		}

		var resp struct {
			Output struct {
				TaskStatus string `json:"task_status"`
				VideoURL   string `json:"video_url"`
				Message    string `json:"message"`
				Code       string `json:"code"`
			} `json:"output"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return true, "", fmt.Errorf("failed to parse poll response: %w (body: %s)", err, truncateForLog(respBody))
		}

		switch resp.Output.TaskStatus {
		case "SUCCEEDED":
			if resp.Output.VideoURL == "" {
				return true, "", fmt.Errorf("task succeeded but no video_url returned")
			}
			return true, resp.Output.VideoURL, nil
		case "FAILED", "CANCELED", "UNKNOWN":
			return true, "", fmt.Errorf("task ended with status %s (code=%s): %s",
				resp.Output.TaskStatus, resp.Output.Code, resp.Output.Message)
		default: // PENDING / RUNNING：继续轮询
			return false, "", nil
		}
	})
}

// ---- 火山方舟（Volcengine Ark / Seedance 系列）----

// runVolcengine 提交方舟视频生成任务并轮询到出片。
// prompt 里的时长/画幅/分辨率按官方约定以 "--flag" 形式附加在 text 里。
func (t *VideoGenerationTool) runVolcengine(ctx context.Context, prompt, imageURL, model string, duration int, ratio, resolution string) (videoURL, taskID string, err error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	text := fmt.Sprintf("%s --duration %d --ratio %s --resolution %s --watermark false", prompt, duration, ratio, resolution)
	content := []map[string]interface{}{
		{"type": "text", "text": text},
	}
	if imageURL != "" {
		content = append(content, map[string]interface{}{
			"type":      "image_url",
			"image_url": map[string]string{"url": imageURL},
		})
	}

	reqBody := map[string]interface{}{
		"model":   model,
		"content": content,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/contents/generations/tasks", bytes.NewReader(reqJSON))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	respBody, err := t.doHTTP(ctx, req)
	if err != nil {
		return "", "", err
	}

	var submitResp struct {
		ID    string `json:"id"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &submitResp); err != nil {
		return "", "", fmt.Errorf("failed to parse submit response: %w (body: %s)", err, truncateForLog(respBody))
	}
	if submitResp.ID == "" {
		msg := ""
		if submitResp.Error != nil {
			msg = fmt.Sprintf(" (code=%s): %s", submitResp.Error.Code, submitResp.Error.Message)
		}
		return "", "", fmt.Errorf("task submit rejected%s", msg)
	}
	taskID = submitResp.ID

	videoURL, err = t.pollVolcengineTask(ctx, baseURL, taskID)
	return videoURL, taskID, err
}

// pollVolcengineTask 轮询方舟任务：queued/running → succeeded/failed/expired/cancelled
func (t *VideoGenerationTool) pollVolcengineTask(ctx context.Context, baseURL, taskID string) (string, error) {
	url := baseURL + "/contents/generations/tasks/" + taskID
	return pollUntilDone(ctx, 10*time.Second, func() (done bool, videoURL string, err error) {
		req, rerr := http.NewRequestWithContext(ctx, "GET", url, nil)
		if rerr != nil {
			return true, "", fmt.Errorf("failed to create poll request: %w", rerr)
		}
		req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

		respBody, rerr := t.doHTTP(ctx, req)
		if rerr != nil {
			return true, "", rerr
		}

		var resp struct {
			Status  string `json:"status"`
			Content *struct {
				VideoURL string `json:"video_url"`
			} `json:"content"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return true, "", fmt.Errorf("failed to parse poll response: %w (body: %s)", err, truncateForLog(respBody))
		}

		switch resp.Status {
		case "succeeded":
			if resp.Content == nil || resp.Content.VideoURL == "" {
				return true, "", fmt.Errorf("task succeeded but no video_url returned")
			}
			return true, resp.Content.VideoURL, nil
		case "failed", "expired", "cancelled":
			msg := ""
			if resp.Error != nil {
				msg = fmt.Sprintf(" (code=%s): %s", resp.Error.Code, resp.Error.Message)
			}
			return true, "", fmt.Errorf("task ended with status %s%s", resp.Status, msg)
		default: // queued / running：继续轮询
			return false, "", nil
		}
	})
}

// ---- 公共辅助 ----

// pollUntilDone 每 interval 调一次 check，直到 done 或 ctx 取消/超时。
// 首次查询前先等一个 interval：任务刚提交必然是排队态，立刻查没有意义。
func pollUntilDone(ctx context.Context, interval time.Duration, check func() (done bool, videoURL string, err error)) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("video generation polling aborted (context: %w)", ctx.Err())
		case <-time.After(interval):
		}
		done, videoURL, err := check()
		if done {
			return videoURL, err
		}
	}
}

// doHTTP 执行请求并校验状态码，返回响应体
func (t *VideoGenerationTool) doHTTP(ctx context.Context, req *http.Request) ([]byte, error) {
	client := util.GetHTTPClient()
	if t.config.Timeout > 0 {
		client = &http.Client{Timeout: t.config.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, truncateForLog(body))
	}
	return body, nil
}

// downloadAndSaveVideo 下载生成的 MP4 到输出目录（temp+rename 原子写）
func (t *VideoGenerationTool) downloadAndSaveVideo(ctx context.Context, videoURL, customPath string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", videoURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read video data: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("downloaded video is empty")
	}

	outputDir := t.config.OutputDirectory
	if outputDir == "" {
		outputDir = "./generated_videos"
	}
	filePath := customPath
	if filePath == "" {
		filename := fmt.Sprintf("generated_video_%s.mp4", time.Now().Format("20060102_150405"))
		filePath = filepath.Join(outputDir, filename)
	} else if dir := filepath.Dir(filePath); dir != "." {
		outputDir = dir
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// temp+rename 原子写，避免半截 MP4 被当成完整文件
	tmpFile, err := os.CreateTemp(outputDir, ".video_tmp_*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write video data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to flush video data: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to finalize video file: %w", err)
	}

	return filePath, nil
}

// localImageToDataURI 读取本地图片并转成 base64 data URI（volcengine 支持）
func localImageToDataURI(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read image file: %w", err)
	}
	mime := "image/png"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// truncateForLog 截断响应体，避免错误信息里塞进整个大 JSON
func truncateForLog(body []byte) string {
	s := string(body)
	if len(s) > 512 {
		return s[:512] + "...(truncated)"
	}
	return s
}

// SetConfig 设置视频生成配置
func (t *VideoGenerationTool) SetConfig(config *VideoGenConfig) { t.config = config }

// GetConfig 获取当前配置
func (t *VideoGenerationTool) GetConfig() *VideoGenConfig { return t.config }
