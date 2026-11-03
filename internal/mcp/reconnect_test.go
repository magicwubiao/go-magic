package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestReconnectFailureKeepsEntry 锁定"重连失败不丢条目"。
//
// 旧实现是 Disconnect → connect：一旦续接的 connect 失败，clients 里的条目
// 就永久消失，后续每一次工具调用都报 "MCP server 'x' not found"。用户的
// 体感是"一次网络抖动之后，这个连接器就再也用不了了"，而错误文案还指向
// 完全错误的方向（说没配过，其实是连不上）。
func TestReconnectFailureKeepsEntry(t *testing.T) {
	srv := slowMockServer(t, 0)
	mgr := NewManager()
	defer mgr.DisconnectAll()

	if err := mgr.ConnectSSE("mock", ServerConfig{Transport: "sse", URL: srv.URL}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// 服务端消失：下一次重连必然失败。
	srv.Close()

	if err := mgr.Reconnect("mock"); err == nil {
		t.Fatal("expected reconnect to fail after the server went away")
	}

	found := false
	for _, n := range mgr.ListServers() {
		if n == "mock" {
			found = true
		}
	}
	if !found {
		t.Fatalf("entry dropped after a failed reconnect; ListServers=%v", mgr.ListServers())
	}

	// 工具清单仍可读：注册表靠它维持影子工具，条目掉出去会让模型手里的
	// 工具凭空消失，直到用户手动重连。
	if _, err := mgr.ListToolsByServer("mock"); err != nil {
		t.Fatalf("listing tools after a failed reconnect must not error, got: %v", err)
	}

	// 调用要报真实的连接错误，而不是 not found。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, callErr := mgr.CallTool(ctx, "mock", "mock_echo", map[string]any{})
	if callErr == nil {
		t.Fatal("expected the call to fail while the server is down")
	}
	if strings.Contains(callErr.Error(), "not found") {
		t.Fatalf("a down server must not be reported as not found: %v", callErr)
	}
}
