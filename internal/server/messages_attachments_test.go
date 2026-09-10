package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// Regression: reloading a session used to drop every attachment — the API
// mapper ignored ContentParts, so a user message that consisted of an uploaded
// file (no text) came back with empty content and no file list. The chat page
// then showed its empty-content placeholder ("[文件]") instead of the file
// name, making the attachment unrecognizable after a refresh.
func TestConvertDBMessagesKeepsAttachmentNames(t *testing.T) {
	msgs := []types.Message{
		{
			Role: "user",
			ContentParts: []types.ContentPart{
				{
					Type: "file",
					File: &types.FileInfo{
						Name:     "季度报表.xlsx",
						MimeType: "application/vnd.ms-excel",
						URL:      "/api/uploads/sess-1/3f2b.xlsx",
					},
				},
				// Internal hint appended by the streaming path: it must NOT
				// surface as the user's own words.
				{Type: "text", Text: "本次消息的附件已放入工作目录，可直接用文件工具读取：\n- 季度报表.xlsx"},
			},
		},
		{
			Role: "user",
			ContentParts: []types.ContentPart{
				{Type: "image_url", ImageURL: &types.MediaURL{URL: "/api/uploads/sess-1/9c1a.png"}},
			},
		},
		{Role: "assistant", Content: "收到"},
	}

	out := convertDBMessagesToAPI("sess-1", msgs)
	if len(out) != 3 {
		t.Fatalf("got %d messages, want 3", len(out))
	}

	raw, err := json.Marshal(out[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), "季度报表.xlsx") {
		t.Fatalf("file name lost after reload: %s", raw)
	}
	if !strings.Contains(string(raw), `"files"`) {
		t.Fatalf("file list missing from the API payload: %s", raw)
	}
	if strings.Contains(string(raw), "工作目录") {
		t.Fatalf("internal hint text must not be returned as message content: %s", raw)
	}
	if content, _ := out[0]["content"].(string); content != "" {
		t.Fatalf("content should stay empty for an attachment-only message, got %q", content)
	}

	// Image parts must reach the UI through the same `files` list the template
	// renders (thumbnail + name). Legacy rows only carry a uuid reference, so
	// the mime is derived from the extension and the name falls back to a
	// localized placeholder on the client.
	raw, _ = json.Marshal(out[1])
	if !strings.Contains(string(raw), "/api/uploads/sess-1/9c1a.png") {
		t.Fatalf("image reference lost: %s", raw)
	}
	if !strings.Contains(string(raw), `"files"`) {
		t.Fatalf("image attachment missing from the files list: %s", raw)
	}
	if !strings.Contains(string(raw), "image/png") {
		t.Fatalf("image mime not derived from the reference: %s", raw)
	}

	// Plain messages must not grow empty attachments fields.
	if _, ok := out[2]["files"]; ok {
		t.Fatal("assistant message without parts must not carry a files field")
	}
}

// 图片附件落库：必须转成 file 部件并带上原始名 + MIME + 引用路径。以前只存
// image_url —— MediaURL 没有名字字段，图片回放时前端认不出它，气泡里就只剩
// [文件] 占位，用户完全不知道那是哪张图。
func TestPersistedContentPartsKeepsImageName(t *testing.T) {
	const ref = "/api/uploads/sess-1/9c1a.png?token=abc"
	parts := []types.ContentPart{
		{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64," + strings.Repeat("A", 4096)}},
		{Type: "text", Text: "看看这张图"},
		{Type: "file", File: &types.FileInfo{
			Name:     "notes.txt",
			MimeType: "text/plain",
			URL:      ref,
			Contents: "data:text/plain;base64,aGk=",
		}},
	}

	out := persistedContentParts(parts, []string{ref}, []string{"截图 2026-09-10 15.20.03.png"}, nil)
	if len(out) != 3 {
		t.Fatalf("got %d parts, want 3", len(out))
	}

	img := out[0]
	if img.Type != "file" || img.File == nil {
		t.Fatalf("image part must persist as a file part, got %+v", img)
	}
	if img.File.Name != "截图 2026-09-10 15.20.03.png" {
		t.Errorf("original name lost: %q", img.File.Name)
	}
	if img.File.URL != ref {
		t.Errorf("reference url = %q, want %q", img.File.URL, ref)
	}
	if img.File.MimeType != "image/png" {
		t.Errorf("mime = %q, want image/png (from the data URL prefix)", img.File.MimeType)
	}
	if strings.HasPrefix(img.File.URL, "data:") {
		t.Errorf("data URL leaked into the stored reference: %q", img.File.URL)
	}
	if img.File.Contents != "" {
		t.Errorf("inline base64 must not be persisted, got %d bytes", len(img.File.Contents))
	}

	// Non-image parts are sanitized the same way; text parts ride along so the
	// workdir hint still survives a reload.
	if f := out[2].File; f == nil || f.Name != "notes.txt" || f.MimeType != "text/plain" || f.Contents != "" {
		t.Errorf("file part not sanitized: %+v", out[2].File)
	}
	if out[1].Type != "text" || out[1].Text != "看看这张图" {
		t.Errorf("text part altered: %+v", out[1])
	}
}

// 图片部件没有引用（既没上传路径也没名字）时不能整块丢掉：留着一条无名附件，
// 前端至少能显示本地化的「图片」；丢掉就会退回 [文件] 占位。
func TestPersistedContentPartsKeepsImagelessRefPart(t *testing.T) {
	out := persistedContentParts(
		[]types.ContentPart{{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,QUJD"}}},
		[]string{""}, nil, nil,
	)
	if len(out) != 1 || out[0].File == nil {
		t.Fatalf("image part dropped without a reference: %+v", out)
	}
	if out[0].File.URL != "" || out[0].File.Name != "" {
		t.Errorf("unexpected metadata: %+v", out[0].File)
	}
	if out[0].File.MimeType != "image/png" {
		t.Errorf("mime = %q, want image/png", out[0].File.MimeType)
	}
}

// 客户端没给图片名时回查 uploads 元数据兜底（老数据 / 恢复草稿路径）。
func TestPersistedContentPartsResolvesImageName(t *testing.T) {
	const ref = "/api/uploads/sess-1/9c1a.png"
	called := false
	out := persistedContentParts(
		[]types.ContentPart{{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,QUJD"}}},
		[]string{ref}, nil,
		func(got string) string {
			called = true
			if got != ref {
				t.Errorf("resolver got %q, want %q", got, ref)
			}
			return "猫.png"
		},
	)
	if !called {
		t.Fatal("name resolver was not consulted when the client sent no name")
	}
	if len(out) != 1 || out[0].File == nil || out[0].File.Name != "猫.png" {
		t.Fatalf("resolver result not applied: %+v", out)
	}
	if out[0].File.MimeType != "image/png" {
		t.Errorf("mime = %q, want image/png", out[0].File.MimeType)
	}
}

// 图片 MIME 兜底：优先 data URL 前缀，其次引用扩展名，都判不出时给 image/*，
// 前端只靠 startsWith("image/") 决定要不要渲染缩略图。
func TestImageMimeForRef(t *testing.T) {
	cases := []struct{ dataURL, ref, want string }{
		{"data:image/jpeg;base64,QUJD", "/api/uploads/s/x.png", "image/jpeg"},
		{"", "/api/uploads/s/x.webp", "image/webp"},
		{"data:application/octet-stream;base64,QUJD", "/api/uploads/s/x.gif", "image/gif"},
		{"", "/api/uploads/s/9c1a", "image/*"},
	}
	for _, c := range cases {
		if got := imageMimeForRef(c.dataURL, c.ref); got != c.want {
			t.Errorf("imageMimeForRef(%q, %q) = %q, want %q", c.dataURL, c.ref, got, c.want)
		}
	}
}

// 端到端（除 HTTP 外）：一轮流式请求里的图片 → 落库形态 → 会话库序列化
// （JSON blob，和 store 里存的形式一致）→ 回放映射成 API 载荷。任何一环把
// 名字/缩略图引用丢掉，气泡里就会退回 [文件] 占位。
func TestImageAttachmentRoundTrip(t *testing.T) {
	const ref = "/api/uploads/sess-1/9c1a.png?token=old"
	live := []types.ContentPart{
		{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64," + strings.Repeat("B", 512)}},
		{Type: "text", Text: "附件已放入工作目录"},
	}

	persisted := persistedContentParts(live, []string{ref}, []string{"设计稿 v2.png"}, nil)
	blob, err := json.Marshal(types.Message{Role: "user", ContentParts: persisted})
	if err != nil {
		t.Fatalf("marshal stored message: %v", err)
	}
	var stored types.Message
	if err := json.Unmarshal(blob, &stored); err != nil {
		t.Fatalf("unmarshal stored message: %v", err)
	}

	out := convertDBMessagesToAPI("sess-1", []types.Message{stored})
	files, ok := out[0]["files"].([]map[string]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("replayed files = %#v, want exactly one attachment", out[0]["files"])
	}
	if files[0]["name"] != "设计稿 v2.png" {
		t.Errorf("name after round trip = %v, want 设计稿 v2.png", files[0]["name"])
	}
	if files[0]["url"] != ref {
		t.Errorf("url after round trip = %v, want %v", files[0]["url"], ref)
	}
	if mime, _ := files[0]["mime"].(string); !strings.HasPrefix(mime, "image/") {
		t.Errorf("mime after round trip = %v, want an image/* type", files[0]["mime"])
	}
	// 内联 base64 必须留在本轮请求里，绝不能进会话库。
	if strings.Contains(string(blob), "base64") {
		t.Fatalf("inline payload leaked into the stored blob: %s", blob)
	}
}

// 上传引用解析：带 token 查询串、共享桶、裸文件名、空串都要能正确还原。
func TestParseUploadRef(t *testing.T) {
	cases := []struct {
		ref               string
		wantSID, wantDisk string
	}{
		{"/api/uploads/sess-1/9c1a.png", "sess-1", "9c1a.png"},
		{"/api/uploads/sess-1/9c1a.png?token=abc", "sess-1", "9c1a.png"},
		{"/api/uploads/_shared/9c1a.png", "", "9c1a.png"},
		{"https://example.com/uploads/sess-2/9c1a.png", "sess-2", "9c1a.png"},
		{"9c1a.png", "", "9c1a.png"},
		{"", "", ""},
	}
	for _, c := range cases {
		sid, disk := parseUploadRef(c.ref)
		if sid != c.wantSID || disk != c.wantDisk {
			t.Errorf("parseUploadRef(%q) = (%q, %q), want (%q, %q)", c.ref, sid, disk, c.wantSID, c.wantDisk)
		}
	}
}
