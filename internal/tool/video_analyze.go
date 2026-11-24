package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// 视频理解默认/上限帧数。抽帧是逐个 fork ffmpeg 跑出来的，帧数越多越慢，
// 而且多帧一次性塞给视觉模型时 token 也线性增长。
const (
	defaultVideoFrameCount = 5
	maxVideoFrameCount     = 10
	// videoFrameRetention 抽出的帧保留时长：结果里会把帧路径返回给模型，
	// 若立刻删掉临时目录，模型随后 vision_analyze 这些路径必然 file not found。
	videoFrameRetention = 7 * 24 * time.Hour
)

// VideoAnalyzeTool 视频理解工具
type VideoAnalyzeTool struct {
	BaseTool
}

// NewVideoAnalyzeTool 创建视频理解工具
func NewVideoAnalyzeTool() *VideoAnalyzeTool {
	return &VideoAnalyzeTool{
		BaseTool: *NewBaseTool(
			"video_analyze",
			"Analyze video content with the session's vision model. Supports local video files and http(s) URLs. "+
				"Extracts key frames with ffmpeg, sends them to the vision model in one request, and returns a written "+
				"analysis answering `question`.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"video_path": map[string]interface{}{
						"type":        "string",
						"description": "Path or URL to the video file (local path or http/https URL)",
					},
					"question": map[string]interface{}{
						"type":        "string",
						"description": "Question about the video content",
						"default":     "Describe what happens in this video in detail. Identify key scenes, objects, people, and actions.",
					},
					"frame_count": map[string]interface{}{
						"type":        "integer",
						"description": "Number of key frames to extract for analysis (1-10)",
						"default":     defaultVideoFrameCount,
					},
				},
				"required": []string{"video_path"},
			},
		),
	}
}

// Execute 分析视频内容
func (t *VideoAnalyzeTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	videoPath, ok := params["video_path"].(string)
	if !ok || videoPath == "" {
		return nil, fmt.Errorf("video_path is required")
	}

	question := "Describe what happens in this video in detail. Identify key scenes, objects, people, and actions."
	if q, ok := params["question"].(string); ok && strings.TrimSpace(q) != "" {
		question = q
	}

	// 关键：参数来自 JSON 解码，数字是 float64；用 `params["frame_count"].(int)`
	// 断言永远失败，于是无论请求几帧都按默认 5 帧处理（"请求 3 帧返回 5 帧"）。
	frameCount := defaultVideoFrameCount
	if fc := paramInt(params, "frame_count"); fc > 0 {
		frameCount = fc
	}
	if frameCount > maxVideoFrameCount {
		frameCount = maxVideoFrameCount
	}

	if !isFFmpegAvailable() {
		return nil, fmt.Errorf("ffmpeg is required for video analysis but is not installed. " +
			"Please install ffmpeg: https://ffmpeg.org/download.html")
	}

	isURL := strings.HasPrefix(videoPath, "http://") || strings.HasPrefix(videoPath, "https://")
	if strings.HasPrefix(videoPath, "file://") {
		return nil, fmt.Errorf("file:// URLs are not supported; pass the local path instead")
	}

	source := videoPath
	var videoSize int64
	if isURL {
		// ffmpeg 自己会拉流，不必先整段下载到磁盘（视频动辄上百 MB）。
		// 仍要过 SSRF 校验：否则等于给模型开了一个内网探测/任意 URL 抓取通道。
		if err := validateURL(videoPath); err != nil {
			return nil, fmt.Errorf("invalid video URL: %w", err)
		}
	} else {
		expandedPath := expandPath(videoPath)
		// 相对路径按会话工作目录解析：附件被物化到 <workDir>/.magic-uploads/，
		// 模型拿到的就是这个相对路径。
		resolved, rerr := resolveLocalMedia(ctx, expandedPath)
		if rerr != nil {
			return nil, rerr
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("failed to access file: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("video_path points to a directory: %s", resolved)
		}
		source = resolved
		videoSize = info.Size()
	}

	duration, err := getVideoDuration(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to get video duration: %w", err)
	}

	frames, err := extractKeyFrames(ctx, source, frameCount, duration)
	if err != nil {
		return nil, fmt.Errorf("failed to extract frames: %w", err)
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames could be extracted from the video (duration %.3fs); the file may be corrupt or use an unsupported codec", duration)
	}

	result := map[string]interface{}{
		"video_path":       source,
		"duration_seconds": duration,
		"frames_requested": frameCount,
		"frame_count":      len(frames),
		"frames":           frames,
		"question":         question,
	}
	if isURL {
		result["video_url"] = videoPath
		result["video_path"] = ""
	} else {
		result["video_size"] = videoSize
	}

	// 真的做分析。以前这里只把帧落盘就返回，并提示"Configure a vision-capable
	// model"——同会话的 vision_analyze 明明可用，等于把活儿甩回给模型。
	analysis, model, analyzedBy, note := analyzeFrames(ctx, frames, question)
	if analyzedBy != "" {
		result["status"] = "analyzed"
		result["analysis"] = analysis
		result["model"] = model
		result["analyzed_by"] = analyzedBy
		return result, nil
	}

	// 没有可用的视觉后端：如实说明，并给出下一步可操作的方向。
	result["status"] = "frames_extracted"
	result["analysis_note"] = fmt.Sprintf(
		"Frames were extracted but NOT analyzed: %s. The frames are saved at %s — "+
			"call vision_analyze on them if a vision-capable model becomes available, "+
			"or set VISION_API_KEY / VISION_BASE_URL / VISION_MODEL to use a dedicated vision endpoint.",
		note, filepath.Dir(frames[0]))
	return result, nil
}

// analyzeFrames 依次尝试独立视觉端点与会话模型，返回描述、模型名、来源与失败原因。
// analyzedBy 为空表示两条路径都不可用，note 说明原因。
func analyzeFrames(ctx context.Context, frames []string, question string) (analysis, model, analyzedBy, note string) {
	if len(frames) > 1 {
		question = fmt.Sprintf("%s\n\nThe %d images are key frames sampled evenly across the video, in chronological order. "+
			"Use them together: describe the progression over time, not each frame in isolation.", question, len(frames))
	} else {
		question = fmt.Sprintf("%s\n\nThe image is a key frame sampled from a video.", question)
	}

	apiKey := os.Getenv("VISION_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey != "" {
		desc, usedModel, err := analyzeImagesWithOpenAICompat(ctx, frames, question, os.Getenv("VISION_MODEL"), apiKey)
		if err == nil {
			return desc, usedModel, "vision_api", ""
		}
		note = "the dedicated vision endpoint failed: " + err.Error()
	} else {
		note = "no VISION_API_KEY/OPENAI_API_KEY is configured"
	}

	if desc, usedModel, ok := analyzeImagesWithSessionModel(ctx, frames, question); ok {
		return desc, usedModel, "session_model", ""
	}
	if note != "" {
		note += "; "
	}
	note += "the current chat model is not configured or does not accept image input"
	return "", "", "", note
}

// isFFmpegAvailable 检查 ffmpeg 是否可用
func isFFmpegAvailable() bool {
	cmd := exec.Command("ffmpeg", "-version")
	return cmd.Run() == nil
}

// getVideoDuration 获取视频时长（秒）
func getVideoDuration(ctx context.Context, videoPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	raw := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration %q: %w", raw, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("video reports a non-positive duration (%.3fs)", duration)
	}

	return duration, nil
}

// extractKeyFrames 从视频中均匀提取 count 帧，返回实际落盘的帧路径。
func extractKeyFrames(ctx context.Context, videoPath string, count int, duration float64) ([]string, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("invalid video duration")
	}

	dir, err := videoFrameDir()
	if err != nil {
		return nil, err
	}

	var frames []string
	interval := duration / float64(count+1)

	for i := 1; i <= count; i++ {
		timestamp := interval * float64(i)
		framePath := filepath.Join(dir, fmt.Sprintf("frame_%03d.jpg", i))

		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-ss", fmt.Sprintf("%.3f", timestamp),
			"-i", videoPath,
			"-frames:v", "1",
			"-q:v", "2",
			"-y", // 覆盖已存在的文件
			framePath)

		if err := cmd.Run(); err != nil {
			// 单帧提取失败不影响其他帧
			continue
		}

		// 检查文件是否真的生成了
		if info, statErr := os.Stat(framePath); statErr == nil && info.Size() > 0 {
			frames = append(frames, framePath)
		}
	}

	return frames, nil
}

// videoFrameDir 返回本次抽帧的持久化目录。
//
// 以前用 os.MkdirTemp + defer RemoveAll：结果里返回的 frames 路径在被模型读到
// 之前就已经删掉了。现在落到 <magic home>/video-frames/<纳秒>/，并按保留期
// 顺手清理旧目录。
func videoFrameDir() (string, error) {
	base := filepath.Join(config.GetMagicHome(), "video-frames")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("failed to create video frame directory: %w", err)
	}
	pruneOldVideoFrames(base)

	dir := filepath.Join(base, strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create video frame directory: %w", err)
	}
	return dir, nil
}

// pruneOldVideoFrames 删除超过保留期的抽帧目录；失败不影响本次调用。
func pruneOldVideoFrames(base string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-videoFrameRetention)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(base, entry.Name()))
	}
}

// expandPath expands ~ and environment variables in a path
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home := os.Getenv("HOME"); home != "" {
			path = filepath.Join(home, path[2:])
		}
	}
	return filepath.Clean(path)
}
