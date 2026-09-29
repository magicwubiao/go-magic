package bot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// recordingLLM 是一个记录完整请求体的 OpenAI 兼容 mock：用来断言转换层真的
// 把图片当作 image_url 部件发给了模型，而不是降级成占位文本。
type recordingLLM struct {
	mu     sync.Mutex
	bodies []map[string]interface{}
	// reply 是每次调用固定返回的 assistant 文本（留空则回 "收到"）。
	reply string
}

func (r *recordingLLM) handler(w http.ResponseWriter, req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	var body map[string]interface{}
	_ = json.Unmarshal(raw, &body)

	r.mu.Lock()
	r.bodies = append(r.bodies, body)
	reply := r.reply
	r.mu.Unlock()

	if reply == "" {
		reply = "收到"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(textResponse(reply))
}

// snapshot 返回按顺序记录的请求体副本。
func (r *recordingLLM) snapshot() []map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]interface{}, len(r.bodies))
	copy(out, r.bodies)
	return out
}

// requestText 把一次请求体里所有文本内容拼起来（含 parts 形态），便于断言
// "某段内容有没有进过发给模型的提示词"。
func requestText(body map[string]interface{}) string {
	var sb strings.Builder
	msgs, _ := body["messages"].([]interface{})
	for _, m := range msgs {
		msg, _ := m.(map[string]interface{})
		switch c := msg["content"].(type) {
		case string:
			sb.WriteString(c)
			sb.WriteString("\n")
		case []interface{}:
			for _, p := range c {
				pm, _ := p.(map[string]interface{})
				if s, _ := pm["text"].(string); s != "" {
					sb.WriteString(s)
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}

// requestHasImage reports whether any message in this request carries a real
// inline image part.
func requestHasImage(body map[string]interface{}) bool {
	msgs, _ := body["messages"].([]interface{})
	for _, m := range msgs {
		msg, _ := m.(map[string]interface{})
		arr, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, p := range arr {
			pm, _ := p.(map[string]interface{})
			if pm["type"] != "image_url" {
				continue
			}
			iu, _ := pm["image_url"].(map[string]interface{})
			if u, _ := iu["url"].(string); strings.HasPrefix(u, "data:image/") {
				return true
			}
		}
	}
	return false
}

// allParts flattens every content part of every message across all recorded
// requests (content is an array whenever the turn carried media).
func (r *recordingLLM) allParts() []map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]interface{}
	for _, body := range r.bodies {
		msgs, _ := body["messages"].([]interface{})
		for _, m := range msgs {
			msg, _ := m.(map[string]interface{})
			arr, ok := msg["content"].([]interface{})
			if !ok {
				continue
			}
			for _, p := range arr {
				if pm, ok := p.(map[string]interface{}); ok {
					out = append(out, pm)
				}
			}
		}
	}
	return out
}

// newRoomRig 搭一套"两个成员 + 一张真图"的群聊测试环境：mock LLM（固定回
// reply）+ 临时 magic home + 房间。返回的 pngPath 是上传桶里的原图路径，
// 可直接当 RoomUploadItem.Src。
func newRoomRig(t *testing.T, reply string, maxRounds int) (mgr *Manager, llm *recordingLLM, roomID, pngPath string) {
	t.Helper()

	llm = &recordingLLM{reply: reply}
	server := httptest.NewServer(http.HandlerFunc(llm.handler))
	t.Cleanup(server.Close)

	home := t.TempDir()
	cfgData := map[string]interface{}{
		"provider": "custom",
		"providers": map[string]interface{}{
			"custom": map[string]interface{}{
				"api_key":  "test-key",
				"base_url": server.URL,
				// 名字判定为视觉模型：这条链路以前不发图，正是因为策略没装上。
				"models": []string{"gpt-4o-mini"},
			},
		},
		"bot_mode":    map[string]interface{}{"enabled": true},
		"working_dir": t.TempDir(),
	}
	raw, _ := json.Marshal(cfgData)
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_MAGIC_HOME", home)

	// rehydrateBotMediaParts 只认 <magicHome>/uploads 下的真实文件（引用桶 →
	// _shared → 根），所以这里要放一张真图，否则会走"附件已丢失"的降级文案。
	sharedDir := filepath.Join(home, "uploads", "_shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8AAAwAB/AL+2gAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	pngPath = filepath.Join(sharedDir, "pic.png")
	if err := os.WriteFile(pngPath, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"alice", "bob"} {
		if err := store.Save(&Config{Name: n, Title: n}); err != nil {
			t.Fatal(err)
		}
	}

	mgr, err = NewManager(mustLoadConfig(t))
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v (nil manager = bot mode off?)", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("manager start: %v", err)
	}
	t.Cleanup(mgr.Stop)

	room := &RoomConfig{Name: "r", Members: []string{"alice", "bob"}, MaxRounds: maxRounds}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	return mgr, llm, room.ID, pngPath
}

// picPersisted / picItem 是同一张测试图的两种形态：落库引用与工作目录副本源。
func picPersisted() []types.ContentPart {
	return []types.ContentPart{{
		Type: "file",
		File: &types.FileInfo{
			Name:     "pic.png",
			MimeType: "image/png",
			URL:      "/api/uploads/_shared/pic.png",
		},
	}}
}

// TestRoomRoundDeliversImageAndPersistsRef 覆盖群聊发图的两条端到端不变量：
//
//  1. 成员 bot 的请求里必须带真实的 image_url 部件。修复前 bot 链路没装转换
//     策略，图片在这里被替换成 "(image attachment omitted: ...)" 占位文本 ——
//     模型因此看不到图，回一句"当前模型不支持视觉识别"，再转去装 OCR 自救。
//  2. 落库（bots.db / 内存历史）必须是轻量上传引用，不能是 base64 字节。
//     修复前 sendRoomTurn 没传 persistedParts，几十 MB 的 base64 会进库并被
//     之后每一轮重放。
func TestRoomRoundDeliversImageAndPersistsRef(t *testing.T) {
	mgr, llm, roomID, src := newRoomRig(t, "", 1)

	persisted := picPersisted()
	items := []RoomUploadItem{{Name: "pic.png", Src: src}}

	res, err := mgr.SendToRoomWithMedia(context.Background(), roomID, "看看这张图", persisted, items, "")
	if err != nil {
		t.Fatalf("SendToRoomWithMedia: %v", err)
	}
	if res == nil {
		t.Fatal("nil room result")
	}

	// ① 模型侧：必须真的收到图片字节。
	sawImage, sawPlaceholder, placeholderText := false, false, ""
	for _, p := range llm.allParts() {
		switch p["type"] {
		case "image_url":
			if iu, ok := p["image_url"].(map[string]interface{}); ok {
				if u, _ := iu["url"].(string); strings.HasPrefix(u, "data:image/png;base64,") {
					sawImage = true
				}
			}
		case "text":
			if s, _ := p["text"].(string); strings.Contains(s, "image attachment omitted") {
				sawPlaceholder, placeholderText = true, s
			}
		}
	}
	if sawPlaceholder {
		t.Errorf("image degraded to a placeholder instead of reaching the model: %q", placeholderText)
	}
	if !sawImage {
		t.Fatal("no image_url part reached the model for a group-chat attachment")
	}

	// ② 落库侧：轻量引用，不是 base64。
	ctx := context.Background()
	sess, err := mgr.Sessions().LoadSession(ctx, RoomSessionID("alice", roomID))
	if err != nil {
		t.Fatalf("load alice's room session: %v", err)
	}
	var lastParts []types.ContentPart
	for _, m := range sess.Messages {
		if m.Role == "user" && len(m.ContentParts) > 0 {
			lastParts = m.ContentParts
		}
	}
	if len(lastParts) == 0 {
		t.Fatal("room turn persisted no content parts for the user message")
	}
	var ref *types.FileInfo
	for _, p := range lastParts {
		if p.Type == "image_url" {
			t.Fatalf("base64 image leaked into bots.db (history will replay it every turn)")
		}
		if p.Type == "file" && p.File != nil {
			ref = p.File
		}
	}
	if ref == nil || !strings.HasPrefix(ref.URL, "/api/uploads/") {
		t.Errorf("attachment not persisted as an upload ref: %+v", lastParts)
	}

	// ②b 整个会话里都不能残留内联图片字节（不只是最后一条 user 消息）：
	// stripInlineImageData 是落库前的兜底，专门清掉"更早那条带图消息"——
	// 例如回合被取消后重试追加的重复 user 消息，或旧版本写下的行。
	for i, m := range sess.Messages {
		for _, p := range m.ContentParts {
			if p.Type == "image_url" && p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:") {
				t.Fatalf("message %d still carries inline base64 in bots.db", i)
			}
		}
	}
}
