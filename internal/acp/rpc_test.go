package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestConcurrentCallsKeepTheirOwnResponses(t *testing.T) {
	fa, tr := newFakeAgent(t)
	p := newPeer(tr.WriteMessage, tr.Close)
	go func() {
		for {
			line, err := tr.ReadMessage()
			if err != nil {
				return
			}
			_ = p.dispatch(line)
		}
	}()
	defer p.Close()

	// 让每个响应在服务端乱序返回：先到的请求反而后回。
	fa.on("echo", func(params json.RawMessage) (interface{}, error) {
		var in struct {
			N int `json:"n"`
		}
		_ = json.Unmarshal(params, &in)
		time.Sleep(time.Duration(40-in.N%40) * time.Millisecond)
		return map[string]interface{}{"n": in.N}, nil
	})

	const calls = 40
	var wg sync.WaitGroup
	errs := make(chan error, calls)

	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			raw, err := p.call(context.Background(), "echo", map[string]int{"n": n})
			if err != nil {
				errs <- fmt.Errorf("call %d: %w", n, err)
				return
			}
			var out struct {
				N int `json:"n"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				errs <- fmt.Errorf("call %d: bad json: %w", n, err)
				return
			}
			if out.N != n {
				errs <- fmt.Errorf("call %d got response for %d (responses crossed)", n, out.N)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCallCancelledDropsLateResponse(t *testing.T) {
	fa, tr := newFakeAgent(t)
	p := newPeer(tr.WriteMessage, tr.Close)
	go func() {
		for {
			line, err := tr.ReadMessage()
			if err != nil {
				return
			}
			_ = p.dispatch(line)
		}
	}()
	defer p.Close()

	fa.on("slow", func(json.RawMessage) (interface{}, error) {
		time.Sleep(200 * time.Millisecond)
		return map[string]bool{"ok": true}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := p.call(ctx, "slow", nil); err == nil {
		t.Fatal("expected ctx error")
	}

	// 迟到的响应必须被安静丢弃（不能 panic、不能塞给下一次调用）。
	time.Sleep(300 * time.Millisecond)
	fa.on("fast", func(json.RawMessage) (interface{}, error) {
		return map[string]bool{"ok": true}, nil
	})
	if _, err := p.call(context.Background(), "fast", nil); err != nil {
		t.Fatalf("follow-up call failed: %v", err)
	}
}

func TestUnknownMethodGetsStandardError(t *testing.T) {
	fa, tr := newFakeAgent(t)
	_ = fa
	p := newPeer(tr.WriteMessage, tr.Close)
	go func() {
		for {
			line, err := tr.ReadMessage()
			if err != nil {
				return
			}
			_ = p.dispatch(line)
		}
	}()
	defer p.Close()

	_, err := p.call(context.Background(), "does/not/exist", nil)
	if err == nil {
		t.Fatal("expected method-not-found error")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("err type = %T, want *RPCError", err)
	}
	if !rpcErr.MethodNotFound() {
		t.Errorf("code = %d, want -32601", rpcErr.Code)
	}
}

func TestIDKeyNormalizesStringsAndNumbers(t *testing.T) {
	cases := map[string]string{
		`1`:    "1",
		`"1"`:  "1",
		` "a"`: "a",
		`7`:    "7",
	}
	for raw, want := range cases {
		if got := idKey(json.RawMessage(raw)); got != want {
			t.Errorf("idKey(%s) = %q, want %q", raw, got, want)
		}
	}
}
