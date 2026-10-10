package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 本文件钉住「回合起点随 stream_started 下发」这一不变量。
//
// 用户报告的故障：chat 页面的"当前回合已执行 X 分 Y 秒"在**切换会话后
// 从头开始计**。
//
// 根因在前端侧（由本测试所钉住的字段堵住）：本端发起的回合，服务端只在
// queue_changed（队列增删改）时下发 active_started_at，stream_started 里
// 没有，于是前端会话状态里的 activeTurnStartedAt 长期为 0，显示只能退回
// "组件本地、从 streaming 起算"的计数器——而那个计数器以 streaming
// false→true 为重置条件，切换会话必然经历这两跳，于是切回来就归零。
//
// 修复：stream_started 直接带上服务端认领时刻（sessionQueue.turnStartedAt），
// 前端据此把起点写进**会话状态**；会话切换、keep-alive 停用都不会再清零。

// parseStartedAt 从一帧 SSE data 里取出 stream_started 的 active_started_at。
func parseStartedAt(t *testing.T, frame string) int64 {
	t.Helper()
	body := strings.TrimSpace(strings.TrimPrefix(frame, "data: "))
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v (frame=%q)", err, frame)
	}
	if payload["type"] != "stream_started" {
		t.Fatalf("期望 stream_started，实际 %v", payload["type"])
	}
	v, _ := payload["active_started_at"].(float64)
	return int64(v)
}

func TestSSETurnStartedCarriesActiveStartedAt(t *testing.T) {
	item := &queuedTurn{id: "t1", content: "hello"}
	started := time.Now().Add(-90 * time.Second).Truncate(time.Second)

	if got := parseStartedAt(t, sseTurnStarted(item, started)); got != started.Unix() {
		t.Fatalf("stream_started 应带上回合认领时刻：got=%d want=%d", got, started.Unix())
	}

	// 零值（空闲 / 看门狗已强制解锁）时下发 0，而不是留下上一个回合的残值。
	if got := parseStartedAt(t, sseTurnStarted(item, time.Time{})); got != 0 {
		t.Fatalf("认领时刻为空时应下发 0：got=%d", got)
	}
}

func TestTurnStartedAtOfFollowsTurnLifecycle(t *testing.T) {
	q := newSessionQueue()
	if v := q.turnStartedAtOf(); !v.IsZero() {
		t.Fatalf("空闲队列的认领时刻应为零值，got=%v", v)
	}

	now := time.Now()
	q.mu.Lock()
	q.turnStartedAt = now
	q.mu.Unlock()
	if v := q.turnStartedAtOf(); !v.Equal(now) {
		t.Fatalf("认领时刻应可读回：got=%v want=%v", v, now)
	}
}
