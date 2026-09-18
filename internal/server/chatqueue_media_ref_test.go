package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// 本文件钉住「粘贴图片 → 排队 → 模型看到图」这条链路的两个端点：
//
//   stripInlineMediaParts（入队时剥离 base64，换成 ref 引用）
//   rehydrateMediaRefs（回合开跑时从 uploads 磁盘把字节读回来）
//
// 血债：排队重构时 strip 把图片直接换成了一句占位文字
// 「[图片附件已随消息接收…]」，而执行路径没人还原图片 ——
// 用户粘贴的截图模型从头到尾没看见过，agent 只会复读"有新图片附件"。
// 这组测试保证：剥离后一定能还原；还原失败必须降级成明确的文字，
// 而不是悄悄变成看不见的空洞。

const testPNGBytes = "\x89PNG\r\n\x1a\n fake-image-bytes-for-test"

func mediaTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{magicHome: t.TempDir()}
}

func imagePart(dataURL string) types.ContentPart {
	return types.ContentPart{Type: "image_url", ImageURL: &types.MediaURL{URL: dataURL}}
}

// TestStripReplacesImageDataWithRef：有上传引用的图片 → ref 引用部件，
// 且 mime/name 从原部件与 refs 里带出来。
func TestStripReplacesImageDataWithRef(t *testing.T) {
	parts := []types.ContentPart{
		{Type: "text", Text: "看看这张图"},
		imagePart("data:image/png;base64,AAAA"),
	}
	refs := []string{"/api/uploads/sess-1/img.png"}
	names := []string{"截图-20260917.png"}

	got := stripInlineMediaParts(parts, refs, names)
	if got == nil {
		t.Fatal("含 data: 图片的 parts 应该被改写，实际返回 nil")
	}
	if len(got) != 2 {
		t.Fatalf("应仍为 2 个部件，实际 %d", len(got))
	}
	if got[0].Text != "看看这张图" {
		t.Fatalf("文本部件被改动: %q", got[0].Text)
	}
	fp := got[1].File
	if fp == nil || got[1].Type != "file" {
		t.Fatalf("图片应替换为 file 引用部件，实际 %+v", got[1])
	}
	if fp.Contents != "ref:/api/uploads/sess-1/img.png" {
		t.Fatalf("引用内容错误: %q", fp.Contents)
	}
	if fp.MimeType != "image/png" || fp.Name != "截图-20260917.png" {
		t.Fatalf("mime/name 未带出: mime=%q name=%q", fp.MimeType, fp.Name)
	}
	// 原 slices 不许被就地修改（persistedContentParts 还要用原始 parts）
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Fatal("strip 就地修改了原始 parts —— persistedParts 会跟着坏掉")
	}
}

// TestStripKeepsImageWithoutRef：图片从未上传（没有 ref 可回捞）时必须
// 保留原始 data URL —— 宁可占内存，也不能让模型看不见图。
func TestStripKeepsImageWithoutRef(t *testing.T) {
	parts := []types.ContentPart{imagePart("data:image/jpeg;base64,BBBB")}
	got := stripInlineMediaParts(parts, []string{""}, []string{"x.png"})
	if got == nil || len(got) != 1 {
		t.Fatalf("应返回 1 个部件，实际 %+v", got)
	}
	if got[0].ImageURL == nil || got[0].ImageURL.URL != "data:image/jpeg;base64,BBBB" {
		t.Fatalf("无 ref 的图片被丢弃/替换: %+v", got[0])
	}
}

// TestStripPlainTextNoop：纯文本消息不应触发改写（返回 nil 即"无需处理"）。
func TestStripPlainTextNoop(t *testing.T) {
	parts := []types.ContentPart{{Type: "text", Text: "hello"}}
	if got := stripInlineMediaParts(parts, nil, nil); got != nil {
		t.Fatalf("纯文本不应改写，实际 %+v", got)
	}
}

// TestRehydrateRoundTrip：strip → 落盘 → rehydrate，模型最终拿到的必须是
// 与磁盘字节一致的 image_url 部件。这是「模型能看到粘贴的图」的判定性断言。
func TestRehydrateRoundTrip(t *testing.T) {
	s := mediaTestServer(t)
	imgPath := filepath.Join(s.uploadsRoot(), "sess-1")
	if err := os.MkdirAll(imgPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgPath, "shot.png"), []byte(testPNGBytes), 0o644); err != nil {
		t.Fatal(err)
	}

	stripped := stripInlineMediaParts(
		[]types.ContentPart{imagePart("data:image/png;base64,AAAA")},
		[]string{"/api/uploads/sess-1/shot.png"},
		[]string{"shot.png"},
	)
	rehydrated := s.rehydrateMediaRefs(stripped)
	if len(rehydrated) != 1 {
		t.Fatalf("应为 1 个部件，实际 %d", len(rehydrated))
	}
	p := rehydrated[0]
	if p.ImageURL == nil || p.Type != "image_url" {
		t.Fatalf("还原后应是 image_url 部件，实际 %+v", p)
	}
	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(p.ImageURL.URL, wantPrefix) {
		t.Fatalf("data URL 前缀错误: %q", p.ImageURL.URL)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(p.ImageURL.URL, wantPrefix))
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if string(raw) != testPNGBytes {
		t.Fatalf("还原字节与磁盘不一致: got %d bytes", len(raw))
	}
}

// TestRehydrateFallbackOrder：引用桶不存在时应依次尝试 _shared 与根目录。
func TestRehydrateFallbackOrder(t *testing.T) {
	s := mediaTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.uploadsRoot(), "_shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.uploadsRoot(), "_shared", "b.png"), []byte(testPNGBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	stripped := stripInlineMediaParts(
		[]types.ContentPart{imagePart("data:image/png;base64,AAAA")},
		[]string{"/api/uploads/sess-x/b.png?token=abc"},
		[]string{"b.png"},
	)
	rehydrated := s.rehydrateMediaRefs(stripped)
	if rehydrated[0].ImageURL == nil {
		t.Fatalf("应从 _shared 桶还原成功，实际 %+v", rehydrated[0])
	}
}

// TestRehydrateMissingFileDegradesToText：文件被清理时必须给模型一句
// 明确的文字（而不是 ref 空部件或 panic），回合照常进行。
func TestRehydrateMissingFileDegradesToText(t *testing.T) {
	s := mediaTestServer(t)
	stripped := stripInlineMediaParts(
		[]types.ContentPart{imagePart("data:image/png;base64,AAAA")},
		[]string{"/api/uploads/sess-gone/gone.png"},
		[]string{"gone.png"},
	)
	rehydrated := s.rehydrateMediaRefs(stripped)
	if len(rehydrated) != 1 || rehydrated[0].Type != "text" {
		t.Fatalf("应降级为 text 部件，实际 %+v", rehydrated)
	}
	if !strings.Contains(rehydrated[0].Text, "重新发送") {
		t.Fatalf("降级文案应提示用户重新发送: %q", rehydrated[0].Text)
	}
}

// TestResolveUploadLocalPathTraversal：引用里的 .. 必须逃不出 uploads 根。
func TestResolveUploadLocalPathTraversal(t *testing.T) {
	s := mediaTestServer(t)
	secret := filepath.Join(s.magicHome, "secret.txt")
	if err := os.WriteFile(secret, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		"/api/uploads/../secret.txt",
		"/api/uploads/sess/../../secret.txt",
		"../../secret.txt",
		"/api/uploads/",
	} {
		if p := resolveUploadLocalPath(s.uploadsRoot(), ref); p != "" {
			t.Fatalf("引用 %q 不应解析出路径，实际 %q", ref, p)
		}
	}
}
