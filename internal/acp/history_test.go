package acp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 调用历史环形缓冲的语义。这里守的是三条：
//  1. 新的在前（管理页直接按顺序渲染，不二次排序）；
//  2. 容量封顶（不落盘的诊断缓冲，绝不能无限增长）；
//  3. 非文本块绝不进预览（base64 图片/音频数据进日志既脏又泄体积）。

// resetHistory 清空包级历史，避免用例之间互相污染。
func resetHistory(t *testing.T) {
	t.Helper()
	historyMu.Lock()
	history = nil
	historyID = 0
	historyMu.Unlock()
}

func TestRecentCallsNewestFirstAndFilter(t *testing.T) {
	resetHistory(t)

	appendCall(CallRecord{Agent: "a", Kind: CallKindConnect, Status: CallStatusOK})
	time.Sleep(2 * time.Millisecond)
	appendCall(CallRecord{Agent: "b", Kind: CallKindPrompt, Status: CallStatusOK})
	time.Sleep(2 * time.Millisecond)
	appendCall(CallRecord{Agent: "a", Kind: CallKindPrompt, Status: CallStatusError, Error: "boom"})

	all := RecentCalls("", 0)
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	if all[0].Agent != "a" || all[0].Kind != CallKindPrompt {
		t.Fatalf("newest record = %+v, want agent a prompt", all[0])
	}
	if all[0].ID <= all[1].ID {
		t.Fatalf("IDs not ascending backwards: %d then %d", all[0].ID, all[1].ID)
	}

	onlyA := RecentCalls("a", 0)
	if len(onlyA) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(onlyA))
	}
	for _, rec := range onlyA {
		if rec.Agent != "a" {
			t.Fatalf("filter leaked agent %q", rec.Agent)
		}
	}

	limited := RecentCalls("", 2)
	if len(limited) != 2 {
		t.Fatalf("limited len = %d, want 2", len(limited))
	}
}

func TestHistoryRingCapacity(t *testing.T) {
	resetHistory(t)

	for i := 0; i < historyCapacity+30; i++ {
		appendCall(CallRecord{Agent: "a", Kind: CallKindPrompt, Status: CallStatusOK})
	}

	all := RecentCalls("", 0)
	if len(all) != historyCapacity {
		t.Fatalf("len = %d, want %d (ring must cap)", len(all), historyCapacity)
	}
	// 最旧的被挤掉：剩下的是最后 historyCapacity 条。
	if all[len(all)-1].ID != int64(30+1) {
		t.Fatalf("oldest kept ID = %d, want %d", all[len(all)-1].ID, 31)
	}
	if all[0].ID != int64(historyCapacity+30) {
		t.Fatalf("newest ID = %d, want %d", all[0].ID, historyCapacity+30)
	}
}

func TestPromptPreviewSkipsBinaryBlocks(t *testing.T) {
	blocks := []ContentBlock{
		TextBlock("第一段"),
		ImageBlock(strings.Repeat("A", 4096), "image/png"),
		TextBlock("第二段"),
		{Type: ContentTypeResource, Resource: &EmbeddedResource{URI: "file:///x.txt", Text: "secret"}},
	}

	got := promptPreview(blocks, previewRunes)
	if strings.Contains(got, "AAAA") || strings.Contains(got, "secret") {
		t.Fatalf("preview leaked binary data: %q", got)
	}
	want := "第一段\n[image]\n第二段\n[resource:file:///x.txt]"
	if got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestPreviewTextTruncates(t *testing.T) {
	got := previewText(strings.Repeat("长", previewRunes+10), previewRunes)
	if got == "" || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated preview should end with ellipsis, got %q", got)
	}
	if len([]rune(got)) != previewRunes+1 {
		t.Fatalf("preview length = %d, want %d", len([]rune(got)), previewRunes+1)
	}
}

func TestBuildPromptRecordStatuses(t *testing.T) {
	blocks := []ContentBlock{TextBlock("hi")}

	okRec := buildPromptRecord("a", blocks, &PromptOutcome{
		StopReason: StopReasonEndTurn,
		Text:       "done",
		ToolCalls:  []ToolCallInfo{{ID: "1"}},
		Updates:    3,
	}, nil, 1500*time.Millisecond, "  stderr lines  ")
	if okRec.Status != CallStatusOK {
		t.Fatalf("status = %q, want ok", okRec.Status)
	}
	if okRec.StopReason != StopReasonEndTurn || okRec.ToolCalls != 1 || okRec.Updates != 3 {
		t.Fatalf("outcome fields lost: %+v", okRec)
	}
	if okRec.PromptPreview != "hi" || okRec.ReplyPreview != "done" {
		t.Fatalf("previews wrong: %q / %q", okRec.PromptPreview, okRec.ReplyPreview)
	}
	if okRec.Stderr != "stderr lines" {
		t.Fatalf("stderr not trimmed: %q", okRec.Stderr)
	}
	if okRec.DurationMs != 1500 {
		t.Fatalf("duration = %d, want 1500", okRec.DurationMs)
	}
	if okRec.Start.IsZero() {
		t.Fatalf("start time missing")
	}

	cancelRec := buildPromptRecord("a", blocks, &PromptOutcome{Cancelled: true}, nil, time.Second, "")
	if cancelRec.Status != CallStatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelRec.Status)
	}

	errRec := buildPromptRecord("a", blocks, nil, errors.New("agent died"), time.Second, "")
	if errRec.Status != CallStatusError || errRec.Error != "agent died" {
		t.Fatalf("error record wrong: %+v", errRec)
	}
}

func TestSnipStderrKeepsTail(t *testing.T) {
	long := strings.Repeat("x", stderrRecordRunes*2)
	got := snipStderr(long)
	if len([]rune(got)) != stderrRecordRunes {
		t.Fatalf("tail length = %d, want %d", len([]rune(got)), stderrRecordRunes)
	}
	if snipStderr("  \n ") != "" {
		t.Fatalf("whitespace-only stderr should collapse to empty")
	}
}
