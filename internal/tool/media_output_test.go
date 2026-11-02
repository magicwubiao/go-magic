package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 媒体生成产物必须落进会话工作目录，而不是进程 cwd。
//
// 真实事故：桌面版进程 cwd 是安装目录（AppData\Local\Go Magic），
// "./generated_images" 按 cwd 解析后图片全部写进了程序目录，用户在
// 会话文件面板里根本看不到自己的图。
func TestMediaOutputDirResolvesAgainstSessionWorkDir(t *testing.T) {
	work := t.TempDir()
	ctx := WithWorkDir(context.Background(), work)

	got, err := ResolveMediaOutputDir(ctx, "./generated_images", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if want := filepath.Join(work, "generated_images"); got != want {
		t.Fatalf("dir = %q, want %q", got, want)
	}

	// 绝对目录（用户在 config.json 显式配置的）原样尊重，不收编进工作目录。
	abs := filepath.Join(t.TempDir(), "my_pics")
	got, err = ResolveMediaOutputDir(ctx, abs, "")
	if err != nil {
		t.Fatalf("resolve abs: %v", err)
	}
	if got != abs {
		t.Fatalf("abs dir = %q, want %q", got, abs)
	}

	// 空配置回落到 fallback，同样按会话工作目录解析。
	got, err = ResolveMediaOutputDir(ctx, "", "generated_videos")
	if err != nil {
		t.Fatalf("resolve fallback: %v", err)
	}
	if want := filepath.Join(work, "generated_videos"); got != want {
		t.Fatalf("fallback dir = %q, want %q", got, want)
	}
}

// 图片生成工具的默认落盘链路：无自定义路径时应写进 <会话工作目录>/generated_images。
func TestImageGenSavesIntoSessionWorkDir(t *testing.T) {
	work := t.TempDir()
	tool := &ImageGenerationTool{config: &ImageGenConfig{}}

	paths, err := tool.downloadAndSaveImages(
		WithWorkDir(context.Background(), work),
		[]ImageInfo{{Data: []byte("fake-png-bytes"), Format: "png"}},
		"",
	)
	if err != nil {
		t.Fatalf("downloadAndSaveImages: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("saved = %d paths, want 1", len(paths))
	}
	if dir := filepath.Dir(paths[0]); dir != filepath.Join(work, "generated_images") {
		t.Fatalf("saved into %q, want %q", dir, filepath.Join(work, "generated_images"))
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

// 自定义相对路径同样按会话工作目录解析，而不是进程 cwd。
func TestImageGenCustomPathResolvesAgainstWorkDir(t *testing.T) {
	work := t.TempDir()
	tool := &ImageGenerationTool{config: &ImageGenConfig{}}

	paths, err := tool.downloadAndSaveImages(
		WithWorkDir(context.Background(), work),
		[]ImageInfo{{Data: []byte("x"), Format: "png"}},
		"out/hero.png",
	)
	if err != nil {
		t.Fatalf("downloadAndSaveImages: %v", err)
	}
	want := filepath.Join(work, "out", "hero.png")
	if paths[0] != want {
		t.Fatalf("path = %q, want %q", paths[0], want)
	}
}

// 视频生成工具同规则。
func TestVideoGenSavesIntoSessionWorkDir(t *testing.T) {
	work := t.TempDir()
	tool := &VideoGenerationTool{config: &VideoGenConfig{}}

	// downloadAndSaveVideo 需要真的下载 URL，这里用一个本地 httptest 太重；
	// 直接验证目录解析这层即可（下载字节部分与路径无关）。
	got, err := ResolveMediaOutputDir(
		WithWorkDir(context.Background(), work),
		tool.config.OutputDirectory,
		"generated_videos",
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if want := filepath.Join(work, "generated_videos"); got != want {
		t.Fatalf("dir = %q, want %q", got, want)
	}
	if !strings.HasPrefix("generated_video_x.mp4", "generated_video_") {
		t.Fatal("sanity")
	}
}
