package server

import (
	"encoding/json"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// 回归：历史消息接口必须把落库的 TimelineSnapshot 还原成 streaming_timeline_snapshot。
//
// 背景（10-11）：前端只有内存态 streaming_timeline_snapshot，后端从不返回它，
// 于是"重开页面"后执行过程（思考 ↔ 工具的穿插展示）整个消失，只剩正文。
// 修法是后端把时间线随消息落库并在历史接口带回。这里锁住接口字段名与结构，
// 防止再次丢失导致刷新后执行过程不见了。
func TestConvertDBMessagesExposesTimelineSnapshot(t *testing.T) {
	msgs := []types.Message{{
		ID:      "assistant_turn1",
		Role:    "assistant",
		Content: "<think>plan</think>step one",
		ToolCallsSnapshot: []types.ToolExecutionSnapshot{
			{ID: "turn1_tool_0", Name: "terminal", Args: `{"command":"ls"}`, Status: "completed", Success: true},
		},
		TimelineSnapshot: []types.TimelineSegment{
			{Kind: "text", End: 17},
			{Kind: "tool", ToolCallID: "turn1_tool_0"},
		},
	}}

	got := convertDBMessagesToAPI("s1", msgs)
	if len(got) != 1 {
		t.Fatalf("messages=%d, want 1", len(got))
	}

	raw, ok := got[0]["streaming_timeline_snapshot"]
	if !ok {
		t.Fatal("streaming_timeline_snapshot missing from history API response")
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var segs []map[string]interface{}
	if err := json.Unmarshal(b, &segs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(segs) != 2 {
		t.Fatalf("segments=%d, want 2 (%s)", len(segs), string(b))
	}
	if segs[0]["kind"] != "text" {
		t.Fatalf("seg[0].kind=%v, want text", segs[0]["kind"])
	}
	// 前端按 obj.end 做切片，字段必须叫 end
	if _, ok := segs[0]["end"]; !ok {
		t.Fatalf("text segment lost its end offset: %s", string(b))
	}
	if segs[1]["kind"] != "tool" {
		t.Fatalf("seg[1].kind=%v, want tool", segs[1]["kind"])
	}
	// 前端读的是 toolCallId（驼峰），名字写错会让工具段找不到对应卡片
	if segs[1]["toolCallId"] != "turn1_tool_0" {
		t.Fatalf("tool segment toolCallId=%v, want turn1_tool_0", segs[1]["toolCallId"])
	}
}

// 没有工具调用（纯文本回合）时不写时间线字段，避免历史接口里多一份空数组。
func TestConvertDBMessagesOmitsEmptyTimeline(t *testing.T) {
	got := convertDBMessagesToAPI("s1", []types.Message{{
		ID: "assistant_plain", Role: "assistant", Content: "hello",
	}})
	if _, ok := got[0]["streaming_timeline_snapshot"]; ok {
		t.Fatal("empty timeline should be omitted")
	}
}
