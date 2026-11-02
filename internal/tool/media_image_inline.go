package tool

// media_image_inline.go — 本地首帧图内联成厂商可接受的引用。
//
// 背景：图生视频有两类入参形态，各家只能吃其中一种：
//   - Base64 Data URL（带 data: 前缀）：dashscope、minimax、volcengine、runway；
//   - 裸 Base64 字符串（**不能带** data: 前缀）：kling —— 官方明确写了带前缀会解析失败。
//
// 两种都是 base64 的编码方式，差别只在外壳，所以读取/校验/大小卡口共用一套，
// 由各后端决定最后怎么包。这里不掺厂商判断，只负责"把本地文件安全地读成字节"。
//
// 为什么值得统一：此前 dashscope / kling / minimax 三个后端各自硬编码了
// "本地文件不支持，请上传到公网"，但三家官方文档都写了支持 base64——用户被迫
// 先把图传到图床才能做图生视频，这是纯粹的实现缺口，不是厂商限制。

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// imageMimeByExt 扩展名 → MIME。jpg 必须报 image/jpeg：报 image/jpg 会被当成未知格式。
var imageMimeByExt = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".heic": "image/heic",
	".heif": "image/heif",
}

// readInlineImage 读取本地首帧图，按调用方给的格式白名单与大小上限校验。
//
// allowed 为空表示接受 imageMimeByExt 里的全部格式；label 用于错误文案里点出厂商，
// 让模型一眼看出"是格式/大小不对"而不是"网络炸了"。
func readInlineImage(
	ctx context.Context,
	imagePath string,
	maxBytes int64,
	allowed []string,
	label string,
) ([]byte, string, error) {
	resolved, err := resolveLocalMedia(ctx, imagePath)
	if err != nil {
		return nil, "", err
	}

	ext := strings.ToLower(filepath.Ext(resolved))
	mime, known := imageMimeByExt[ext]
	if !known || (len(allowed) > 0 && !extAllowed(allowed, ext)) {
		return nil, "", fmt.Errorf(
			"%s accepts %s first frames, got %q; convert the image first, or pass image_url "+
				"with a publicly reachable address",
			label, strings.Join(allowed, "/"), ext)
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read first-frame image: %w", err)
	}
	if maxBytes > 0 && stat.Size() > maxBytes {
		return nil, "", fmt.Errorf(
			"first-frame image is %.1f MB, but %s accepts at most %d MB inline; "+
				"resize it first, or pass image_url with a publicly reachable address",
			float64(stat.Size())/(1<<20), label, maxBytes>>20)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read first-frame image: %w", err)
	}
	return data, mime, nil
}

// imageDataURI 组装成 data:<mime>;base64,<data>，dashscope / minimax 等直接吃这个。
func imageDataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func extAllowed(allowed []string, ext string) bool {
	for _, a := range allowed {
		if a == ext {
			return true
		}
	}
	return false
}
