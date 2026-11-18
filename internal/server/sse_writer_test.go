package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// panicFlusher 模拟客户端断开瞬间底层连接状态竞争导致的 Flush panic。
// 曾经 sseWriter.run 里没有 recover，一次 panic 直接杀死整个后端进程
// （桌面壳重启并重载窗口，用户看到"对话完成时整个页面刷新"）。
type panicFlusher struct{}

func (panicFlusher) Flush() { panic("nil pointer dereference (simulated)") }

// swallowWriter 只吞字节不做事，保证 Fprint 成功、首次 panic 落在 Flush 上。
type swallowWriter struct{ http.ResponseWriter }

func (swallowWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestSSEWriterPanicDoesNotKillProcess(t *testing.T) {
	sw := newSSEWriter(swallowWriter{httptest.NewRecorder()}, panicFlusher{})
	if !sw.Write("data: {\"type\":\"ping\"}\n\n") {
		t.Fatal("first write should be accepted into the buffer")
	}
	// Close 必须在 run() goroutine panic 后仍能正常返回（done 由 defer close）
	done := make(chan struct{})
	go func() {
		sw.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("sseWriter.Close hung: run goroutine did not finish after panic")
	}
	// panic 必须被降级为"流已死"，后续 Write 一律拒绝而不是再次投递
	if sw.Write("data: after\n\n") {
		t.Fatal("writes after a panicked flush must be rejected")
	}
}

func TestSSEWriterCloseIsIdempotent(t *testing.T) {
	sw := newSSEWriter(swallowWriter{httptest.NewRecorder()}, panicFlusher{})
	sw.Close()
	sw.Close() // 第二次 close 曾经会 panic: close of closed channel
	_ = sw.Write("data: x\n\n")
}
