package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAgent 是一个跑在管道另一端的可编程 ACP agent。
//
// 它的存在意义是：客户端代码必须对着**真实的报文**验证，而不是对着自己的
// 假设。initialize / session/new / session/update / request_permission /
// fs 全部按规范拼 JSON，字段名写错就立刻失败。
type fakeAgent struct {
	t  *testing.T
	br *bufio.Reader
	w  io.Writer

	writeMu sync.Mutex

	mu       sync.Mutex
	handlers map[string]func(json.RawMessage) (interface{}, error)

	notices chan rpcEnvelope
	pending map[string]chan *rpcEnvelope
	seq     int64

	permMu    sync.Mutex
	permTrace []PermissionOutcome

	out io.Closer // agent → client 方向的写端，用来模拟 agent 死亡
}

func newFakeAgent(t *testing.T) (*fakeAgent, Transport) {
	t.Helper()

	clientToAgentR, clientToAgentW := io.Pipe()
	agentToClientR, agentToClientW := io.Pipe()

	tr := newStreamTransport(agentToClientR, clientToAgentW, clientToAgentW, func() string { return "" })

	fa := &fakeAgent{
		t:        t,
		br:       bufio.NewReaderSize(clientToAgentR, 64<<10),
		w:        agentToClientW,
		out:      agentToClientW,
		handlers: map[string]func(json.RawMessage) (interface{}, error){},
		notices:  make(chan rpcEnvelope, 64),
		pending:  map[string]chan *rpcEnvelope{},
	}

	fa.on("initialize", func(json.RawMessage) (interface{}, error) {
		return InitializeResult{
			ProtocolVersion: ProtocolVersion,
			AgentInfo:       &Implementation{Name: "fake", Version: "1.0"},
		}, nil
	})
	fa.on("session/new", func(json.RawMessage) (interface{}, error) {
		return NewSessionResult{SessionID: "sess-1"}, nil
	})

	go fa.loop()

	t.Cleanup(func() {
		_ = clientToAgentW.Close()
		_ = agentToClientW.Close()
		_ = clientToAgentR.Close()
		_ = agentToClientR.Close()
	})
	return fa, tr
}

func (f *fakeAgent) on(method string, h func(json.RawMessage) (interface{}, error)) {
	f.mu.Lock()
	f.handlers[method] = h
	f.mu.Unlock()
}

func (f *fakeAgent) loop() {
	for {
		line, err := f.br.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var env rpcEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}

		switch {
		case env.Method == "" && len(env.ID) > 0:
			key := idKey(env.ID)
			f.mu.Lock()
			ch := f.pending[key]
			delete(f.pending, key)
			f.mu.Unlock()
			if ch != nil {
				ch <- &env
			}
		case env.Method != "" && len(env.ID) > 0:
			f.mu.Lock()
			h := f.handlers[env.Method]
			f.mu.Unlock()

			go func(env rpcEnvelope, h func(json.RawMessage) (interface{}, error)) {
				if h == nil {
					f.respond(env.ID, nil, &RPCError{Code: codeMethodNotFound, Message: "method not found: " + env.Method})
					return
				}
				res, err := h(env.Params)
				if err != nil {
					f.respond(env.ID, nil, &RPCError{Code: codeInternalError, Message: err.Error()})
					return
				}
				f.respond(env.ID, res, nil)
			}(env, h)
		case env.Method != "":
			select {
			case f.notices <- env:
			default:
			}
		}
	}
}

func (f *fakeAgent) write(data []byte) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	_, _ = f.w.Write(append(data, '\n'))
}

func (f *fakeAgent) respond(id json.RawMessage, result interface{}, rpcErr *RPCError) {
	resp := map[string]interface{}{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else if result == nil {
		resp["result"] = map[string]interface{}{}
	} else {
		resp["result"] = result
	}
	data, _ := json.Marshal(resp)
	f.write(data)
}

func (f *fakeAgent) notify(method string, params interface{}) {
	env := rpcEnvelope{JSONRPC: "2.0", Method: method, Params: marshalParams(params)}
	data, _ := json.Marshal(env)
	f.write(data)
}

// request 模拟 agent 向 client 发起请求（例如要权限、读文件）。
func (f *fakeAgent) request(method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&f.seq, 1)
	key := strconv.FormatInt(id, 10)

	ch := make(chan *rpcEnvelope, 1)
	f.mu.Lock()
	f.pending[key] = ch
	f.mu.Unlock()

	env := rpcEnvelope{
		JSONRPC: "2.0",
		ID:      json.RawMessage(key),
		Method:  method,
		Params:  marshalParams(params),
	}
	data, _ := json.Marshal(env)
	f.write(data)

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-time.After(10 * time.Second):
		return nil, errors.New("fake agent: client did not answer " + method)
	}
}

// waitNotice 等一条指定方法的上行通知，返回是否收到。
func (f *fakeAgent) waitNotice(method string) bool {
	deadline := time.After(5 * time.Second)
	for {
		select {
		case n := <-f.notices:
			if n.Method == method {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// die 模拟 agent 进程突然消失（输出端关闭）。
func (f *fakeAgent) die() { _ = f.out.Close() }

func newTestClient(t *testing.T, dir string, mutate func(*ClientOptions)) (*Client, *fakeAgent) {
	t.Helper()

	fa, tr := newFakeAgent(t)
	opts := ClientOptions{
		Name:    "fake",
		Cwd:     dir,
		Command: "unused", // 传输已注入，不会真起进程
		Timeout: 5 * time.Second,
	}
	if mutate != nil {
		mutate(&opts)
	}

	client, err := newClientWithTransport(context.Background(), opts, tr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, fa
}

func TestClientPromptStreamsUpdates(t *testing.T) {
	dir := t.TempDir()
	client, fa := newTestClient(t, dir, nil)

	fa.on("session/prompt", func(json.RawMessage) (interface{}, error) {
		fa.notify("session/update", map[string]interface{}{
			"sessionId": "sess-1",
			"update": map[string]interface{}{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]interface{}{"type": "text", "text": "Hello "},
			},
		})
		fa.notify("session/update", map[string]interface{}{
			"sessionId": "sess-1",
			"update": map[string]interface{}{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]interface{}{"type": "text", "text": "world"},
			},
		})
		fa.notify("session/update", map[string]interface{}{
			"sessionId": "sess-1",
			"update": map[string]interface{}{
				"sessionUpdate": "tool_call",
				"toolCallId":    "call_1",
				"title":         "Reading file",
				"kind":          "read",
				"status":        "in_progress",
			},
		})
		fa.notify("session/update", map[string]interface{}{
			"sessionId": "sess-1",
			"update": map[string]interface{}{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    "call_1",
				"status":        "completed",
			},
		})
		return PromptResult{StopReason: StopReasonEndTurn}, nil
	})

	var seen int
	out, err := client.Prompt(context.Background(), []ContentBlock{TextBlock("hi")}, func(Update) { seen++ })
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if out.Text != "Hello world" {
		t.Errorf("text = %q, want %q", out.Text, "Hello world")
	}
	if out.StopReason != StopReasonEndTurn {
		t.Errorf("stopReason = %q", out.StopReason)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(out.ToolCalls))
	}
	if out.ToolCalls[0].Title != "Reading file" {
		t.Errorf("tool title lost after incremental update: %+v", out.ToolCalls[0])
	}
	if out.ToolCalls[0].Status != ToolStatusCompleted {
		t.Errorf("tool status = %q, want completed", out.ToolCalls[0].Status)
	}
	if seen != 4 {
		t.Errorf("callback count = %d, want 4", seen)
	}
}

func TestInitializeRejectsVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	fa, tr := newFakeAgent(t)
	fa.on("initialize", func(json.RawMessage) (interface{}, error) {
		return InitializeResult{ProtocolVersion: 2}, nil
	})

	_, err := newClientWithTransport(context.Background(), ClientOptions{
		Name: "fake", Cwd: dir, Command: "unused", Timeout: time.Second,
	}, tr)
	if err == nil || !strings.Contains(err.Error(), "protocol version") {
		t.Fatalf("err = %v, want protocol version error", err)
	}
}

func TestPermissionPolicyDefaultsToReadOnly(t *testing.T) {
	dir := t.TempDir()
	client, fa := newTestClient(t, dir, nil)

	fa.on("session/prompt", func(json.RawMessage) (interface{}, error) {
		for _, kind := range []string{ToolKindRead, ToolKindExecute} {
			raw, err := fa.request("session/request_permission", RequestPermissionParams{
				SessionID: "sess-1",
				ToolCall:  ToolCall{ToolCallID: "c-" + kind, Kind: &kind},
				Options: []PermissionOption{
					{OptionID: "allow-once", Name: "Allow", Kind: PermissionAllowOnce},
					{OptionID: "reject-once", Name: "Reject", Kind: PermissionRejectOnce},
				},
			})
			if err != nil {
				return nil, err
			}
			var res RequestPermissionResult
			_ = json.Unmarshal(raw, &res)
			fa.permMu.Lock()
			fa.permTrace = append(fa.permTrace, res.Outcome)
			fa.permMu.Unlock()
		}
		return PromptResult{StopReason: StopReasonEndTurn}, nil
	})

	if _, err := client.Prompt(context.Background(), []ContentBlock{TextBlock("x")}, nil); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	fa.permMu.Lock()
	defer fa.permMu.Unlock()
	if len(fa.permTrace) != 2 {
		t.Fatalf("permission trace = %d entries", len(fa.permTrace))
	}
	if fa.permTrace[0].OptionID != "allow-once" {
		t.Errorf("read outcome = %+v, want allow-once", fa.permTrace[0])
	}
	if fa.permTrace[1].OptionID != "reject-once" {
		t.Errorf("execute outcome = %+v, want reject-once", fa.permTrace[1])
	}
}

func TestFSScopedToSessionRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	inside := filepath.Join(root, "a.txt")
	if err := os.WriteFile(inside, []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	client, fa := newTestClient(t, root, nil)

	fa.on("session/prompt", func(json.RawMessage) (interface{}, error) {
		line, limit := 2, 1
		raw, err := fa.request("fs/read_text_file", ReadTextFileParams{
			SessionID: "sess-1", Path: inside, Line: &line, Limit: &limit,
		})
		if err != nil {
			return nil, err
		}
		var res ReadTextFileResult
		_ = json.Unmarshal(raw, &res)
		if res.Content != "line2" {
			return nil, errors.New("unexpected content: " + res.Content)
		}

		if _, err := fa.request("fs/read_text_file", ReadTextFileParams{
			SessionID: "sess-1", Path: secret,
		}); err == nil {
			return nil, errors.New("read outside roots was allowed")
		}

		if _, err := fa.request("fs/write_text_file", WriteTextFileParams{
			SessionID: "sess-1", Path: filepath.Join(root, "b.txt"), Content: "x",
		}); err == nil {
			return nil, errors.New("write was allowed without capability")
		}

		return PromptResult{StopReason: StopReasonEndTurn}, nil
	})

	if _, err := client.Prompt(context.Background(), []ContentBlock{TextBlock("x")}, nil); err != nil {
		t.Fatalf("prompt: %v", err)
	}
}

func TestWriteEnabledWhenConfigured(t *testing.T) {
	root := t.TempDir()
	client, fa := newTestClient(t, root, func(o *ClientOptions) { o.AllowWrite = true })

	target := filepath.Join(root, "out.txt")
	fa.on("session/prompt", func(json.RawMessage) (interface{}, error) {
		if _, err := fa.request("fs/write_text_file", WriteTextFileParams{
			SessionID: "sess-1", Path: target, Content: "written",
		}); err != nil {
			return nil, err
		}
		return PromptResult{StopReason: StopReasonEndTurn}, nil
	})

	if _, err := client.Prompt(context.Background(), []ContentBlock{TextBlock("x")}, nil); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "written" {
		t.Errorf("content = %q", string(data))
	}
}

func TestPromptCancelSendsNotification(t *testing.T) {
	dir := t.TempDir()
	client, fa := newTestClient(t, dir, nil)

	fa.on("session/prompt", func(json.RawMessage) (interface{}, error) {
		// 一直等客户端取消，再按规范回 cancelled。
		if !fa.waitNotice("session/cancel") {
			return nil, errors.New("no cancel notification")
		}
		return PromptResult{StopReason: StopReasonCancelled}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	out, err := client.Prompt(ctx, []ContentBlock{TextBlock("long task")}, nil)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !out.Cancelled {
		t.Error("outcome should be marked cancelled")
	}
	if out.StopReason != StopReasonCancelled {
		t.Errorf("stopReason = %q, want cancelled", out.StopReason)
	}
}

func TestPromptRejectsConcurrentTurns(t *testing.T) {
	dir := t.TempDir()
	client, fa := newTestClient(t, dir, nil)

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	fa.on("session/prompt", func(json.RawMessage) (interface{}, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return PromptResult{StopReason: StopReasonEndTurn}, nil
	})

	go func() { _, _ = client.Prompt(context.Background(), []ContentBlock{TextBlock("first")}, nil) }()
	<-started

	_, err := client.Prompt(context.Background(), []ContentBlock{TextBlock("second")}, nil)
	close(release)
	if !errors.Is(err, ErrPromptInFlight) {
		t.Fatalf("second prompt err = %v, want ErrPromptInFlight", err)
	}
}

func TestAgentExitFailsPendingCall(t *testing.T) {
	dir := t.TempDir()
	client, fa := newTestClient(t, dir, nil)

	fa.on("session/prompt", func(json.RawMessage) (interface{}, error) {
		fa.die() // 输出端关闭 = agent 死了
		return PromptResult{StopReason: StopReasonEndTurn}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	_, err := client.Prompt(ctx, []ContentBlock{TextBlock("x")}, nil)
	if err == nil {
		t.Fatal("expected error when agent stream closes")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("failure took %s, pending call should fail immediately", time.Since(start))
	}
}
