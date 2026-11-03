package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 这两个用例锁的是同一件事：传输层的时间限制只作用在"等响应头"阶段，
// 不能作用于读 body。
//
// 背景（真实回归）：早期实现把 defaultSSEConnectTimeout 当成
// http.Client.Timeout（整次请求）。服务端先回 200 再慢慢吐 SSE 时，
// 15s 一到就在读 body 的半路掐断，报出
// "failed to read SSE response: context deadline exceeded"。
// deepwiki 的 read_wiki_contents 因此永远失败（实测 79s / 1.3MB）。

// slowBodyServer 立刻回响应头，再延迟 delay 才写 SSE 数据帧。
func slowBodyServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(delay)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n",
			`{"jsonrpc":"2.0","id":"1","result":{"content":[{"type":"text","text":"ok"}]}}`)
	}))
}

// TestSSETransportSlowBodyNotCutByConnectTimeout 验证：body 慢于超时值也能完整读完。
func TestSSETransportSlowBodyNotCutByConnectTimeout(t *testing.T) {
	const connectTimeout = 300 * time.Millisecond
	const bodyDelay = 1500 * time.Millisecond

	srv := slowBodyServer(t, bodyDelay)
	defer srv.Close()

	transport, err := NewSSETransportWithTimeout(srv.URL, connectTimeout)
	if err != nil {
		t.Fatalf("NewSSETransportWithTimeout: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := transport.Send(ctx, &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/call", // 非幂等：不重试，一次成败即结论
		Params:  json.RawMessage(`{"name":"read_wiki_contents"}`),
		ID:      "1",
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Send failed after %v: %v (response headers arrived in time, body delay must not fail the call)", elapsed, err)
	}
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if elapsed < bodyDelay {
		t.Fatalf("returned in %v, expected to wait at least %v for the body", elapsed, bodyDelay)
	}
}

// TestSSETransportHeaderTimeoutStillEnforced 验证：对端迟迟不回响应头时，
// 超时依然生效——放宽 body 不等于放弃"对端是否还活着"的判断。
func TestSSETransportHeaderTimeoutStillEnforced(t *testing.T) {
	const connectTimeout = 150 * time.Millisecond
	const headerDelay = 2 * time.Second

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(headerDelay)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport, err := NewSSETransportWithTimeout(srv.URL, connectTimeout)
	if err != nil {
		t.Fatalf("NewSSETransportWithTimeout: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err = transport.Send(ctx, &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/call",
		Params:  json.RawMessage(`{}`),
		ID:      "1",
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout, got success")
	}
	if elapsed >= headerDelay {
		t.Fatalf("waited %v for headers; connect timeout %v was not enforced", elapsed, connectTimeout)
	}
	// 错误文案要能让用户分辨"对端不说话"，而不是一句裸的 context deadline。
	if !strings.Contains(strings.ToLower(err.Error()), "timed out") &&
		!strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("error should mention a timeout, got: %v", err)
	}
}
