package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeBackend 记录调用并返回可预测的结果，让协议层可以脱离模型单独测。
type fakeBackend struct {
	mu       sync.Mutex
	threads  map[string]bool
	allow    bool
	lastText string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{threads: map[string]bool{}}
}

func (f *fakeBackend) NewThread(_ context.Context, opts ThreadOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("th_%d", len(f.threads)+1)
	f.threads[id] = true
	f.allow = opts.AllowApproval
	return id, nil
}

func (f *fakeBackend) Reply(_ context.Context, threadID, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.threads[threadID] {
		return "", fmt.Errorf("thread %q not found", threadID)
	}
	f.lastText = prompt
	return "answer:" + prompt, nil
}

func (f *fakeBackend) Close() {}

// serve 跑一次服务端，返回按 id 索引的响应。
//
// 请求是并发处理的，响应顺序不保证，因此断言必须按 id 取而不是按位置取。
func serve(t *testing.T, lines ...string) map[string]rpcResponse {
	t.Helper()
	return serveWith(t, newFakeBackend(), lines...)
}

// serveWith 复用同一个后端跑一批消息（会话状态要跨 Serve 调用保留时用）。
func serveWith(t *testing.T, backend Backend, lines ...string) map[string]rpcResponse {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer
	srv := New(backend, in, &out)
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	resps := map[string]rpcResponse{}
	dec := json.NewDecoder(&out)
	for {
		var r rpcResponse
		if err := dec.Decode(&r); err != nil {
			break
		}
		key := string(r.ID)
		if key == "" {
			key = "null"
		}
		if _, dup := resps[key]; dup {
			t.Fatalf("duplicate response id %s (raw=%s)", key, out.String())
		}
		resps[key] = r
	}
	return resps
}

func resultOf(t *testing.T, resps map[string]rpcResponse, id string) map[string]any {
	t.Helper()
	r, ok := resps[id]
	if !ok {
		t.Fatalf("no response for id %s (got %d responses)", id, len(resps))
	}
	if r.Error != nil {
		t.Fatalf("id %s returned error: %+v", id, r.Error)
	}
	m, _ := r.Result.(map[string]any)
	return m
}

func TestInitializeAndToolsList(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`, // 通知：不应产生响应
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("want 2 responses (notification must be silent), got %d", len(resps))
	}

	init := resultOf(t, resps, "1")
	if init["protocolVersion"] != ProtocolVersion {
		t.Fatalf("protocolVersion = %v", init["protocolVersion"])
	}
	if _, ok := init["capabilities"]; !ok {
		t.Fatal("initialize must advertise capabilities")
	}

	list := resultOf(t, resps, "2")
	tools, _ := list["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("want 2 tools (magic, magic_reply), got %d", len(tools))
	}
	names := []string{}
	for _, raw := range tools {
		m := raw.(map[string]any)
		names = append(names, m["name"].(string))
		if _, ok := m["inputSchema"]; !ok {
			t.Fatalf("tool %v has no inputSchema", m["name"])
		}
	}
	if strings.Join(names, ",") != "magic,magic_reply" {
		t.Fatalf("unexpected tools: %v", names)
	}
}

func TestToolCallCreatesThreadAndReplies(t *testing.T) {
	// 依赖前一条结果的调用按客户端真实行为分两批发：收到 thread_id 才会追问。
	// 同一批里的请求是并发处理的，串行假设只对"无依赖"的调用成立。
	backend := newFakeBackend()
	first := serveWith(t, backend,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"magic","arguments":{"prompt":"hello"}}}`,
	)

	res := resultOf(t, first, "1")
	if res["isError"] != false {
		t.Fatalf("unexpected error result: %v", res)
	}
	meta, _ := res["_meta"].(map[string]any)
	if meta["thread_id"] != "th_1" {
		t.Fatalf("thread id missing from _meta: %v", res)
	}
	content := res["content"].([]any)[0].(map[string]any)
	if content["text"] != "answer:hello" {
		t.Fatalf("unexpected content: %v", content)
	}

	second := serveWith(t, backend,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"magic_reply","arguments":{"thread_id":"th_1","prompt":"again"}}}`,
	)
	content = resultOf(t, second, "2")["content"].([]any)[0].(map[string]any)
	if content["text"] != "answer:again" {
		t.Fatalf("reply did not reuse thread: %v", content)
	}
}

func TestToolCallUnknownThreadIsToolError(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"magic_reply","arguments":{"thread_id":"nope","prompt":"x"}}}`,
	)
	// 会话不存在属于"工具结果失败"，不是协议错误：客户端要把原因交回给它的模型。
	r := resps["1"]
	if r.Error != nil {
		t.Fatalf("must not be a JSON-RPC error: %v", r.Error)
	}
	if r.Result.(map[string]any)["isError"] != true {
		t.Fatalf("want isError=true, got %v", r.Result)
	}
}

func TestMissingPromptIsToolError(t *testing.T) {
	resps := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"magic","arguments":{}}}`)
	r := resps["1"]
	if r.Result == nil || r.Result.(map[string]any)["isError"] != true {
		t.Fatalf("empty prompt must be reported as a tool error: %+v", r)
	}
}

func TestUnknownMethodAndBrokenJSON(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"no/such"}`,
		`{not json`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(resps) != 3 {
		t.Fatalf("want 3 responses, got %d", len(resps))
	}
	if r := resps["1"]; r.Error == nil || r.Error.Code != -32601 {
		t.Fatalf("unknown method must be -32601, got %+v", r.Error)
	}
	if r := resps["null"]; r.Error == nil || r.Error.Code != -32700 {
		t.Fatalf("broken json must be -32700, got %+v", r.Error)
	}
	// 坏报文不能让服务退出：后面那条 ping 仍要有响应。
	if r := resps["2"]; r.Error != nil {
		t.Fatalf("ping failed: %+v", r.Error)
	}
}

func TestApprovalFlagParsed(t *testing.T) {
	f := newFakeBackend()
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"magic","arguments":{"prompt":"x","approval":"allow"}}}` + "\n")
	var out bytes.Buffer
	srv := New(f, in, &out)
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.allow {
		t.Fatal("approval=allow must reach the backend")
	}
}
