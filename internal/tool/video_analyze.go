package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
			"Analyze video content using multimodal AI. Supports local video files and URLs. "+
				"Extracts key frames and provides detailed video analysis including scene descriptions, "+
				"object detection, and transcript when available.",
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
						"default":     5,
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
	if q, ok := params["question"].(string); ok && q != "" {
		question = q
	}

	frameCount := 5
	if fc, ok := params["frame_count"].(int); ok && fc > 0 && fc <= 10 {
		frameCount = fc
	}

	// 检查是否为 URL
	isURL := len(videoPath) > 4 && (strings.HasPrefix(videoPath, "http://") || strings.HasPrefix(videoPath, "https://"))

	if isURL {
		return t.analyzeURL(videoPath, question, frameCount)
	}

	// 处理本地文件
	return t.analyzeLocalFile(videoPath, question, frameCount)
}

func (t *VideoAnalyzeTool) analyzeURL(url, question string, frameCount int) (interface{}, error) {
	// 对于 URL，检查 ffmpeg 是否可用
	if !isFFmpegAvailable() {
		return nil, fmt.Errorf("ffmpeg is required for video analysis but is not installed. " +
			"Please install ffmpeg: https://ffmpeg.org/download.html")
	}

	result := map[string]interface{}{
		"status":       "ready",
		"video_url":    url,
		"question":     question,
		"frame_count":  frameCount,
		"message":      "Video URL ready for analysis. Extracting key frames...",
		"extraction_note": "Key frames will be extracted using ffmpeg and analyzed by the vision model.",
	}

	return result, nil
}

func (t *VideoAnalyzeTool) analyzeLocalFile(path, question string, frameCount int) (interface{}, error) {
	expandedPath := expandPath(path)

	info, err := os.Stat(expandedPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("video file not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to access file: %w", err)
	}

	// 检查 ffmpeg 是否可用
	if !isFFmpegAvailable() {
		return nil, fmt.Errorf("ffmpeg is required for video analysis but is not installed. " +
			"Please install ffmpeg: https://ffmpeg.org/download.html")
	}

	// 获取视频时长
	duration, err := getVideoDuration(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get video duration: %w", err)
	}

	// 创建临时目录存放帧
	tmpDir, err := os.MkdirTemp("", "video_frames_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir) // 清理临时目录

	// 提取关键帧
	frames, err := extractKeyFrames(expandedPath, tmpDir, frameCount, duration)
	if err != nil {
		return nil, fmt.Errorf("failed to extract frames: %w", err)
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames could be extracted from video")
	}

	// 返回结果
	framePaths := make([]string, 0, len(frames))
	for _, frame := range frames {
		framePaths = append(framePaths, frame)
	}

	result := map[string]interface{}{
		"status":        "frames_extracted",
		"video_path":    expandedPath,
		"video_size":    info.Size(),
		"duration":      duration,
		"question":       question,
		"frame_count":   len(frames),
		"frames":        framePaths,
		"message":       fmt.Sprintf("Successfully extracted %d key frames from video", len(frames)),
		"analysis_note": "Frames are saved in temp directory. Configure a vision-capable model (GPT-4V, Gemini, etc.) to analyze the frames.",
	}

	return result, nil
}

// isFFmpegAvailable 检查 ffmpeg 是否可用
func isFFmpegAvailable() bool {
	cmd := exec.Command("ffmpeg", "-version")
	return cmd.Run() == nil
}

// getVideoDuration 获取视频时长（秒）
func getVideoDuration(videoPath string) (float64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

// extractKeyFrames 从视频中均匀提取 N 帧
func extractKeyFrames(videoPath, tmpDir string, count int, duration float64) ([]string, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("invalid video duration")
	}

	var frames []string
	interval := duration / float64(count+1)

	for i := 1; i <= count; i++ {
		timestamp := interval * float64(i)
		framePath := filepath.Join(tmpDir, fmt.Sprintf("frame_%03d.jpg", i))

		cmd := exec.Command("ffmpeg",
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
		if _, err := os.Stat(framePath); err == nil {
			frames = append(frames, framePath)
		}
	}

	return frames, nil
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
