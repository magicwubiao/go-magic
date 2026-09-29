package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestRoomLogStripsMemberThinking 盯住群聊广播面：房间日志里不能出现成员的
// <think> 内心独白。
//
// 现场：群聊把成员回合原文（含整段思考）写进房间日志，而 buildRoomPrompt 又
// 把整份日志灌进每个成员的下一轮提示词 —— 实测每个成员每轮多背 ~12KB 他人
// 内心独白，还带着"视觉不可用，去装 OCR"这类已经过期的结论，下一个成员照着
// 又做一遍（pip install 数分钟）。所以这里同时断言两件事：日志干净，且这段
// 思考从未进过任何一个发往模型的请求。
func TestRoomLogStripsMemberThinking(t *testing.T) {
	const secret = "INTERNAL-DELIBERATION-MARKER"
	reply := "<think>" + secret + "：这段思考不该被广播</think>\n普通回复内容"
	mgr, llm, roomID, src := newRoomRig(t, reply, 1)

	if _, err := mgr.SendToRoomWithMedia(context.Background(), roomID, "看看这张图",
		picPersisted(), []RoomUploadItem{{Name: "pic.png", Src: src}}, ""); err != nil {
		t.Fatalf("SendToRoomWithMedia: %v", err)
	}

	// ① 房间日志（人和成员都读的广播面）必须是剥干净的正文。
	msgs, err := mgr.RoomMessages(roomID)
	if err != nil {
		t.Fatalf("RoomMessages: %v", err)
	}
	sawClean := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "<think") || strings.Contains(m.Content, secret) {
			t.Errorf("room log broadcasted raw thinking from %q: %.120q", m.From, m.Content)
		}
		if strings.Contains(m.Content, "普通回复内容") {
			sawClean = true
		}
	}
	if !sawClean {
		t.Fatal("the member's actual reply never reached the room log")
	}

	// ② 任何一次发往模型的请求里都不该出现这段思考（第二个成员的提示词里
	//    尤其不该有）。
	for i, body := range llm.snapshot() {
		if txt := requestText(body); strings.Contains(txt, secret) {
			t.Errorf("request #%d carried another member's raw thinking into the prompt (len=%d)", i+1, len(txt))
		}
	}
}

// TestRoomRoundSendsLiveImageOnlyInFirstRound 盯住"一次用户发图 = 12 次投递"
// 的放大效应：req.Persisted 会被每个成员、每一轮重复使用（4 成员 × 3 轮），
// 每投一次成员就重新识别一次，群聊因此长时间拿不出回复。
//
// 固化语义：活图只跟首轮走；后续轮次成员靠自己的会话历史（轻量引用）与房间
// 日志里的附件记录继续工作。
func TestRoomRoundSendsLiveImageOnlyInFirstRound(t *testing.T) {
	mgr, llm, roomID, src := newRoomRig(t, "普通回复内容", 2)

	if _, err := mgr.SendToRoomWithMedia(context.Background(), roomID, "看看这张图",
		picPersisted(), []RoomUploadItem{{Name: "pic.png", Src: src}}, ""); err != nil {
		t.Fatalf("SendToRoomWithMedia: %v", err)
	}

	bodies := llm.snapshot()
	roundOneImg, roundTwoImg, roundTwoSeen := 0, 0, false
	for _, body := range bodies {
		txt := requestText(body)
		hasImg := requestHasImage(body)
		switch {
		case strings.Contains(txt, "Your turn (round 1/"):
			if hasImg {
				roundOneImg++
			}
		case strings.Contains(txt, "Your turn (round 2/"):
			roundTwoSeen = true
			if hasImg {
				roundTwoImg++
			}
		}
	}

	if roundOneImg == 0 {
		t.Fatalf("first-round member turns did not receive the image (requests=%d)", len(bodies))
	}
	if !roundTwoSeen {
		t.Fatalf("second round never ran; test needs MaxRounds=2 with a non-escalating reply (requests=%d)", len(bodies))
	}
	if roundTwoImg != 0 {
		t.Errorf("later rounds re-delivered the live image %d time(s): every re-delivery re-triggers a fresh image analysis", roundTwoImg)
	}
}

// TestStripInlineImageDataDegradesBase64 盯住落库兜底：replaceLastUserParts 只
// 看最后一条 user 消息，因此更早一条带内联图片的消息（回合被取消后重试留下的
// 重复 user 消息，或旧版本写下的行）会永远留在 bots.db 并被每轮整包重发。
// stripInlineImageData 负责把这些残留的 data: URL 降级掉。
func TestStripInlineImageDataDegradesBase64(t *testing.T) {
	inline := types.ContentPart{
		Type:     "image_url",
		ImageURL: &types.MediaURL{URL: "data:image/png;base64,AAAA"},
	}
	ref := types.ContentPart{
		Type: "file",
		File: &types.FileInfo{Name: "pic.png", URL: "/api/uploads/_shared/pic.png"},
	}
	history := []provider.Message{
		{Role: "user", ContentParts: []types.ContentPart{{Type: "text", Text: "看看这张图"}, inline}},
		{Role: "assistant", Content: "收到"},
		{Role: "user", ContentParts: []types.ContentPart{{Type: "text", Text: "第二张"}, ref}},
	}

	out, changed := stripInlineImageData(history)
	if !changed {
		t.Fatal("inline image data was not detected")
	}
	for i, m := range out {
		for _, p := range m.ContentParts {
			if p.Type == "image_url" && p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:") {
				t.Fatalf("message %d still carries inline base64 after the safety net", i)
			}
		}
	}
	// 轻量引用不受影响，必须原样保留（它才是让历史图片可被重新解析的凭据）。
	found := false
	for _, p := range out[2].ContentParts {
		if p.Type == "file" && p.File != nil && p.File.URL == ref.File.URL {
			found = true
		}
	}
	if !found {
		t.Errorf("ref-form attachment was rewritten: %+v", out[2].ContentParts)
	}
	// 原文不能被就地改写（out[0] 是拷贝）。
	if len(history[0].ContentParts) != 2 || history[0].ContentParts[1].Type != "image_url" {
		t.Error("stripInlineImageData mutated the caller's history in place")
	}
	// 幂等：再跑一次不应再判为"有变化"。
	if _, again := stripInlineImageData(out); again {
		t.Error("stripInlineImageData is not idempotent")
	}
}
