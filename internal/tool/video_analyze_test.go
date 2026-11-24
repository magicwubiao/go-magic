package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// isolateMediaEnv 隔离配置与密钥：否则 analyzeFrames 会拿本机真实 provider
// 去发一次多图请求，测试在无外网环境下会挂到超时。
func isolateMediaEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GO_MAGIC_HOME", t.TempDir())
	t.Setenv("VISION_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
}

// makeTestVideo 用 ffmpeg 生成一段 3 秒的纯色测试视频；没有 ffmpeg 时跳过。
func makeTestVideo(t *testing.T) string {
	t.Helper()
	if !isFFmpegAvailable() {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "sample.mp4")
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=160x120:rate=10",
		"-pix_fmt", "yuv420p", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot generate a test video here: %v (%s)", err, out)
	}
	return path
}

// frame_count 来自 JSON 是 float64，旧实现用 .(int) 断言导致请求 3 帧返回 5 帧。
func TestVideoAnalyzeHonorsFrameCountFromJSON(t *testing.T) {
	isolateMediaEnv(t)
	video := makeTestVideo(t)

	res, err := NewVideoAnalyzeTool().Execute(WithWorkDir(context.Background(), filepath.Dir(video)), map[string]any{
		"video_path":  video,
		"frame_count": float64(3), // 与 JSON 解码后的类型一致
		"question":    "What is shown?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]any)

	if m["frames_requested"] != 3 {
		t.Fatalf("frames_requested: got %v, want 3", m["frames_requested"])
	}
	if m["frame_count"] != 3 {
		t.Fatalf("frame_count: got %v, want 3", m["frame_count"])
	}
	if m["question"] != "What is shown?" {
		t.Fatalf("question not honored: got %v", m["question"])
	}

	// 返回的帧路径必须真实存在（旧实现先删临时目录再返回路径）。
	frames, _ := m["frames"].([]string)
	for _, f := range frames {
		if info, err := os.Stat(f); err != nil || info.Size() == 0 {
			t.Fatalf("returned frame %s does not exist: %v", f, err)
		}
	}
}

func TestVideoAnalyzeClampsFrameCount(t *testing.T) {
	isolateMediaEnv(t)
	video := makeTestVideo(t)

	res, err := NewVideoAnalyzeTool().Execute(context.Background(), map[string]any{
		"video_path":  video,
		"frame_count": 99,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := res.(map[string]any)["frames_requested"]; got != maxVideoFrameCount {
		t.Fatalf("frames_requested: got %v, want %d", got, maxVideoFrameCount)
	}
}

func TestVideoAnalyzeMissingFile(t *testing.T) {
	isolateMediaEnv(t)
	_, err := NewVideoAnalyzeTool().Execute(context.Background(), map[string]any{
		"video_path": filepath.Join(t.TempDir(), "nope.mp4"),
	})
	if err == nil {
		t.Fatal("want error for missing video")
	}
}

// 没有视觉后端时必须明确说明"抽了帧但没分析"，而不是谎称分析完成。
func TestVideoAnalyzeWithoutVisionBackendIsHonest(t *testing.T) {
	isolateMediaEnv(t)
	video := makeTestVideo(t)

	res, err := NewVideoAnalyzeTool().Execute(context.Background(), map[string]any{
		"video_path":  video,
		"frame_count": 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]any)
	if m["status"] != "analyzed" {
		// 会话模型可用时直接分析成功，这也是预期行为。
		if m["status"] != "frames_extracted" {
			t.Fatalf("unexpected status: %v", m["status"])
		}
		note, _ := m["analysis_note"].(string)
		if note == "" || m["analysis"] != nil {
			t.Fatalf("frames_extracted must carry an honest note and no analysis, got %v", m)
		}
	}
}
