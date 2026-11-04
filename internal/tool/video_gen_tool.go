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
				"Providers: dashscope (Alibaba wan), volcengine (Seedance), kling (Kuaishou Kling), "+
				"runway (Runway Gen-4), minimax (Hailuo H3). Pick the provider by what the user's account "+
				"has, and by capability: Runway only renders 5 or 10 second clips; Kling and MiniMax need a "+
				"public URL for image-to-video while volcengine and runway accept local files. "+
				"Only the provider configured in Settings has credentials, so do not switch provider "+
				"on your own to work around a capability limit — report the limit instead. "+
				"Generation typically takes 1-5 minutes; the tool polls the provider and saves the MP4 locally. "+
				"Every call is billed per rendered second and needs an explicit user confirmation, so never "+
				"retry with a reworded prompt to explore variations — ask the user first.",
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
						"description": "Local path of a first-frame image for image-to-video. All built-in providers accept local files (base64 inline; kling sends the raw string, the rest send a data URI); providers without an inline path reject it with a hint to pass image_url instead.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Provider model override, e.g. wan2.2-t2v-plus (dashscope), doubao-seedance-1-0-pro-250528 (volcengine), kling-3.0 (kling), gen4.5 (runway), MiniMax-H3 (minimax). Default comes from config.",
					},
					"duration": map[string]interface{}{
						"type":        "number",
						"description": "Video length in seconds (3-15). Runway snaps to 5 or 10. Default comes from config (video_gen.default_duration).",
					},
					"ratio": map[string]interface{}{
						"type":        "string",
						"description": "Aspect ratio. dashscope supports 16:9, 9:16 and 1:1 (720p only); volcengine also supports 4:3, 3:4 and 21:9; kling, runway and minimax support 16:9, 9:16 and 1:1. Omit to use the configured default ratio (Settings > Media). Unsupported values fall back to 16:9 and the actual value is reported back.",
						"enum":        []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
					},
					"resolution": map[string]interface{}{
						"type":        "string",
						"description": "Resolution tier: 480p, 720p or 1080p. Omit to use the configured default resolution (Settings > Media). Providers map this to their own tiers (minimax uses 768P/2K) and the actual value is reported back.",
						"enum":        []string{"480p", "720p", "1080p"},
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
						"description": "Override the configured provider. Only useful for plugin backends that carry their own credentials; built-in providers share one credential set, so overriding to a different built-in provider is rejected.",
						// 这个 enum 是**内置后端**的快照，故意不跟注册表联动：工具 schema 一旦
						// 随插件注册变化，模型看到的能力面就不可复现了（同一个 prompt 在装插件
						// 前后表现不一致，排查成本极高）。插件贡献的 provider 走
						// config.json 的 video_gen.provider 指定，不通过这个参数选择。
						"enum": []string{"dashscope", "volcengine", "kling", "runway", "minimax"},
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

	// 配置里的 provider 才是"这份凭据属于谁"。模型可以通过 provider 参数改选，
	// 但内置后端的 key / base_url / model 是全局单值，换名等于让凭据与接口错配。
	configuredProvider := effectiveVideoProvider(t.config)
	provider := configuredProvider
	overridden := false
	if p, ok := params["provider"].(string); ok {
		if p = strings.TrimSpace(p); p != "" {
			provider = VideoProvider(p)
			overridden = CanonicalProviderName(MediaKindVideo, p) !=
				CanonicalProviderName(MediaKindVideo, string(configuredProvider))
		}
	}
	if overridden {
		// 配置里的模型名属于配置里那一家（runway + MiniMax-H3 就是一个必 404 的组合）。
		// 模型显式传了 model 的仍然尊重，那是调用方明确的意图。
		if m, ok := params["model"].(string); !ok || strings.TrimSpace(m) == "" {
			model = ""
		}
	}

	// 先查后端：provider 拼错 / 插件已卸载要立刻报"不支持"，而不是误导性的"未配置"。
	// 首帧图的处理（公网 URL 直用 / 本地文件转 data URI / 不支持就报错）属于
	// **后端能力差异**，已下沉到各 backend；这里不再按厂商名分支。
	backend, ok := LookupVideoBackend(string(provider))
	if !ok {
		return nil, fmt.Errorf("unsupported video provider: %s (available: %s)",
			provider, strings.Join(RegisteredVideoProviders(), ", "))
	}

	// 换到另一家**内置**后端时直接拒绝，不发请求。
	//
	// video_gen 段只有一份 key：硬发就是拿 minimax 的密钥去打 runway，再拼出
	// https://api.minimax.cn/v1/videos/text2video 这种不存在的路径，回来一个
	// nginx 404 页；模型读不懂 404，会继续换下一家，一次对话刷出一屏互相矛盾的
	// 错误——用户看到的现象就是"明明配好了，怎么全在报错"。这里把链路断在源头。
	if overridden && isBuiltinVideoBackend(string(provider)) {
		return map[string]interface{}{
			"status":   "not_configured",
			"provider": provider,
			"prompt":   prompt,
			"message": fmt.Sprintf(
				"Only %q is configured with credentials, so this call cannot switch to %q at runtime: "+
					"it would send the %q key to another vendor's endpoint. Retry with provider %q, "+
					"or ask the user to change the provider in Settings > Media.",
				configuredProvider, provider, configuredProvider, configuredProvider),
			"configuration_help": ConfigHelp(MediaKindVideo, string(provider)),
		}, nil
	}

	// 插件后端自带凭据，可以换；但 base_url / model 属于配置里那一家，不能顺手
	// 带过去——否则插件后端会拿着别家的域名和模型名发请求。
	genCfg := t.config
	if overridden {
		clone := *t.config
		clone.BaseURL = ""
		clone.Model = ""
		genCfg = &clone
	}

	// 可用性由后端判定（内置后端要求 key，插件后端可能不需要），不在工具层硬判断。
	if !backend.Available(genCfg) {
		return map[string]interface{}{
			"status":             "not_configured",
			"provider":           provider,
			"prompt":             prompt,
			"message":            fmt.Sprintf("Video generation is not configured for provider %q.", provider),
			"configuration_help": ConfigHelp(MediaKindVideo, string(provider)),
		}, nil
	}

	genResult, err := backend.Generate(ctx, genCfg, VideoRequest{
		Prompt:         prompt,
		NegativePrompt: negativePrompt,
		ImageURL:       imageURL,
		ImagePath:      imagePath,
		Model:          model,
		Duration:       duration,
		Ratio:          ratio,
		Resolution:     resolution,
	})
	if err != nil {
		return nil, fmt.Errorf("video generation failed: %w", err)
	}
	videoURL, taskID := genResult.URL, genResult.TaskID

	// 回显**实际生效**的参数。各厂商会做映射与钳制（Runway 只有 5/10 秒、
	// Kling 没有 480p、MiniMax 用 768P/2K 而不是 720p/1080p），继续回显用户
	// 原始输入等于撒谎：用户看到 "duration: 8" 但拿到 5 秒的片子，只会以为是 bug。
	if genResult.Duration > 0 {
		duration = genResult.Duration
	}
	if genResult.Ratio != "" {
		ratio = genResult.Ratio
	}
	if genResult.Resolution != "" {
		resolution = genResult.Resolution
	}
	if genResult.Model != "" {
		model = genResult.Model
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
	return pollUntilDone(ctx, dashscopePollInterval, func() (done bool, videoURL string, err error) {
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
// maxVideoDownloadBytes 单个视频的下载上限。
// 同 maxImageDownloadBytes 的理由：URL 来自后端（可能是插件），不能无限信任。
// 512MB 足够覆盖 1 分钟 1080p 的成片。
const maxVideoDownloadBytes = 512 << 20

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

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoDownloadBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read video data: %w", err)
	}
	if int64(len(data)) > maxVideoDownloadBytes {
		return "", fmt.Errorf("video exceeds %d bytes", maxVideoDownloadBytes)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("downloaded video is empty")
	}

	// 相对输出目录按会话工作目录解析（与 image_gen 同一套规则，别按进程
	// cwd 落盘——桌面版会把视频写进安装目录）；自定义路径走 resolvePath 沙箱。
	outputDir, err := ResolveMediaOutputDir(ctx, t.config.OutputDirectory, "generated_videos")
	if err != nil {
		return "", fmt.Errorf("failed to resolve output directory: %w", err)
	}
	filePath := customPath
	if filePath != "" {
		abs, perr := resolvePath(ctx, filePath)
		if perr != nil {
			return "", fmt.Errorf("invalid output path: %w", perr)
		}
		filePath = abs
	} else {
		filename := fmt.Sprintf("generated_video_%s.mp4", time.Now().Format("20060102_150405"))
		filePath = filepath.Join(outputDir, filename)
	}
	outputDir = filepath.Dir(filePath)

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

// RequiresApproval 实现 agent 侧的 approvalAware：视频生成**每次**都要人工确认。
//
// 判定依据是钱，不是破坏性：内置 provider 都按渲染秒数计费（10 秒 1080p 通常在
// 几元到十几元），单次调用还要跑 1~5 分钟。模型最典型的失败模式是"不满意就换个
// prompt 再生成一次"，一次对话能叠出好几倍账单，而用户根本没机会拦。所以这里
// 用恒真声明，不参与任何风险推断。
//
// 注意：RequiresApproval 与 ApprovalSummary 必须成对实现，agent 的 approvalAware
// 是两方法接口，少一个断言就失败 → 静默变成"不用确认"。
func (t *VideoGenerationTool) RequiresApproval() bool { return true }

// ApprovalSummary 给审批卡片一句人类可读的动作描述。
// 计费相关的参数（时长/分辨率/模型/prompt）由 agent 侧的审批钩子从调用参数里
// 摊开，这里只负责"这是什么工具"。
func (t *VideoGenerationTool) ApprovalSummary() string {
	return "video_generate (paid video generation)"
}
