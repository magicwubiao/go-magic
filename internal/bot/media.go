package bot

import (
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Bot 聊天的多模态（图片/附件）支持。
//
// 与会话聊天（internal/server/chatqueue.go）保持同一套持久化语义：
//   - 回合执行时模型看到的是带真实字节的 image_url 部件（data URL）；
//   - 落盘时把 data URL 换成轻量引用（file 部件，URL 指向
//     /api/uploads/<bucket>/<file>），避免几十 MB 的 base64 随 bot.db 膨胀；
//   - 引用按「上传桶 → _shared → 根」的顺序在 <magicHome>/uploads 下解析。
//
// 语义差异（与 sessions 一致的有意取舍）：回合开始后只有**本条消息**的
// 图片会被还原成字节送进模型；历史里的旧图片保持引用形态（provider 侧
// 降级为一句 "File: ..." 文本提及），避免每个回合都把全部历史图片重发。

// uploadsRootForBots resolves the shared uploads root. The bot manager lives
// in the same magicHome as the server (both derive it from config), so refs
// produced by the web dashboard resolve identically here.
func uploadsRootForBots() string {
	return filepath.Join(config.GetMagicHome(), "uploads")
}

// resolveBotUploadRef 解析 "/api/uploads/<bucket>/<file>" 引用为磁盘路径。
// 查找顺序与会话侧 resolveUploadLocalPath 一致（引用桶 → _shared → 根）；
// 所有路径段过 filepath.Base，杜绝 ".." 逃出 uploads 根。
// 解析不到现有文件时返回 ""。
func resolveBotUploadRef(root, ref string) string {
	clean := ref
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	clean = strings.TrimPrefix(clean, "/api/uploads/")
	clean = strings.TrimPrefix(clean, "api/uploads/")
	clean = strings.Trim(clean, "/")
	if clean == "" {
		return ""
	}
	segments := strings.Split(filepath.ToSlash(clean), "/")
	file := filepath.Base(segments[len(segments)-1])
	if file == "" || file == "." || file == ".." {
		return ""
	}
	bucket := ""
	if len(segments) >= 2 {
		bucket = filepath.Base(segments[0])
	}
	candidates := []string{}
	if bucket != "" && bucket != "." && bucket != ".." {
		candidates = append(candidates, filepath.Join(root, bucket, file))
	}
	candidates = append(candidates,
		filepath.Join(root, "_shared", file),
		filepath.Join(root, file),
	)
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// botImageMime 判断（并归一化）file 部件是否应按图片还原：优先看 MIME，
// 退化看扩展名。与 provider 侧 isImage 的判定口径保持宽松一致。
func botImageMime(mime, name string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(m, "image/") && m != "image/svg+xml" {
		return m
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	}
	return ""
}

// rehydrateBotMediaParts 把「本回合」content parts 里的图片引用还原成带
// 真字节的 image_url 部件，让模型真正看到图。
//
// 识别两种引用形态：
//   - file 部件 Contents 以 "ref:" 开头（入队时 stripInlineMediaParts 的
//     队列形态）；
//   - file 部件 Contents 为空、URL 指向 /api/uploads/...（落盘持久形态，
//     见 sessions.go persistedContentParts）。
//
// 文件丢失（被 GC 回收等）时降级为一句明确文字，回合照常进行——与 sessions
// 的 rehydrateMediaRefs 同一策略。非图片引用保持原样（provider 会以文本
// 提及的方式处理）。返回值 changed 标识是否产生了新切片。
func rehydrateBotMediaParts(parts []types.ContentPart) ([]types.ContentPart, bool) {
	root := uploadsRootForBots()
	changed := false
	out := parts
	for i := range out {
		p := out[i]
		if p.Type != "file" || p.File == nil {
			continue
		}
		ref := ""
		if strings.HasPrefix(p.File.Contents, "ref:") {
			ref = strings.TrimPrefix(p.File.Contents, "ref:")
		} else if p.File.Contents == "" && strings.HasPrefix(p.File.URL, "/api/uploads/") {
			ref = p.File.URL
		}
		if ref == "" {
			continue
		}
		mime := botImageMime(p.File.MimeType, p.File.Name)
		if mime == "" {
			continue
		}
		local := resolveBotUploadRef(root, ref)
		if local == "" {
			// 文件已丢失：给模型一句明确的降级文案（绝不静默丢图）。
			label := p.File.Name
			if label == "" {
				label = "未命名图片"
			}
			if !changed {
				out = append([]types.ContentPart(nil), parts...)
				changed = true
			}
			out[i] = types.ContentPart{
				Type: "text",
				Text: "[图片附件 " + label + " 的原始文件已丢失（可能被上传清理任务回收），请告知用户重新发送]",
			}
			continue
		}
		data, err := os.ReadFile(local)
		if err != nil || len(data) == 0 {
			continue
		}
		if !changed {
			out = append([]types.ContentPart(nil), parts...)
			changed = true
		}
		out[i] = types.ContentPart{
			Type:     "image_url",
			ImageURL: &types.MediaURL{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)},
		}
	}
	return out, changed
}

// replaceLastUserParts 返回一份新历史：把最后一条带 ContentParts 的 user
// 消息的部件替换成持久化引用形态。回合与该消息一一对应（bot 队列串行），
// 因此「最后一条带部件的 user 消息」就是本回合追加的那条；找不到时原样
// 返回（例如上下文压缩已把它裁掉——此时磁盘上也不会有这条，无需处理）。
func replaceLastUserParts(history []provider.Message, persisted []types.ContentPart) []provider.Message {
	idx := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" && len(history[i].ContentParts) > 0 {
			idx = i
			break
		}
	}
	if idx < 0 || len(persisted) == 0 {
		return history
	}
	out := append([]provider.Message(nil), history...)
	out[idx] = history[idx]
	out[idx].ContentParts = append([]types.ContentPart(nil), persisted...)
	return out
}

// --- 群聊（rooms）附件支持 ---

// botWorkdirAttachmentsDir 与 server 侧 workdirAttachmentsDir 同名：成员 bot
// 的工作目录里都用 <workDir>/.magic-uploads/ 收纳本回合附件副本。
const botWorkdirAttachmentsDir = ".magic-uploads"

// roomAttachmentsFromParts 从 ref-form file 部件（persistedContentParts 产出：
// Name/URL/Mime 齐全，Contents 为空）提取群聊日志用的附件列表。
func roomAttachmentsFromParts(parts []types.ContentPart) []RoomAttachment {
	if len(parts) == 0 {
		return nil
	}
	atts := make([]RoomAttachment, 0, len(parts))
	for _, p := range parts {
		if p.Type != "file" || p.File == nil || p.File.URL == "" {
			continue
		}
		atts = append(atts, RoomAttachment{
			Name: p.File.Name,
			URL:  p.File.URL,
			Mime: p.File.MimeType,
		})
	}
	if len(atts) == 0 {
		return nil
	}
	return atts
}

// roomPartsFromAttachments 是 roomAttachmentsFromParts 的逆变换：把群聊日志
// 消息里的附件编码回 ref-form file 部件，借用 session store 的 ContentParts
// 字段持久化（不改动 store 结构）。
func roomPartsFromAttachments(atts []RoomAttachment) []types.ContentPart {
	if len(atts) == 0 {
		return nil
	}
	parts := make([]types.ContentPart, 0, len(atts))
	for _, a := range atts {
		if a.URL == "" {
			continue
		}
		parts = append(parts, types.ContentPart{
			Type: "file",
			File: &types.FileInfo{
				Name:     a.Name,
				MimeType: a.Mime,
				URL:      a.URL,
			},
		})
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

// materializeRoomUploads 把一轮群聊附件的规范副本拷进成员 bot 的工作目录
// （与单 bot 聊天的 materializeUploads 同语义），返回给模型的摘要文字；
// 没有可拷贝项或工作目录不可用时返回 ""。Src 是 server 侧上传落盘的规范
// 路径，成员拿到的副本互不影响。
func (m *Manager) materializeRoomUploads(items []RoomUploadItem, botName string) string {
	if len(items) == 0 {
		return ""
	}
	workDir := m.BotWorkDir(botName)
	if workDir == "" {
		return ""
	}
	dstDir := filepath.Join(workDir, botWorkdirAttachmentsDir)
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return ""
	}
	var lines []string
	for _, it := range items {
		if it.Src == "" || it.Name == "" {
			continue
		}
		dst := filepath.Join(dstDir, filepath.Base(it.Src))
		if err := copyRoomUpload(it.Src, dst); err != nil {
			continue
		}
		lines = append(lines, "- "+it.Name+" → "+botWorkdirAttachmentsDir+"/"+filepath.Base(dst)+"（工作目录内）")
	}
	if len(lines) == 0 {
		return ""
	}
	return "本次消息的附件已放入工作目录，可直接用文件工具读取：\n" + strings.Join(lines, "\n")
}

// copyRoomUpload copies src to dst (temp + rename, 0600) — same shape as the
// server-side copyUploadFile.
func copyRoomUpload(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
