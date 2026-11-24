package tool

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// gatedTool 是一个可控制耗时/阻塞的工具，用于验证串行化与超时宽限。
type gatedTool struct {
	BaseTool
	delay time.Duration
	run   func()
}

func newGatedTool(name string, delay time.Duration) *gatedTool {
	return &gatedTool{
		BaseTool: *NewBaseTool(name, "test tool", map[string]interface{}{"type": "object"}),
		delay:    delay,
	}
}

func (g *gatedTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if g.run != nil {
		g.run()
	}
	if g.delay > 0 {
		time.Sleep(g.delay)
	}
	return map[string]interface{}{"ok": true}, nil
}

// 同一 tab 的浏览器工具必须串行；不同 tab 不互相阻塞。
func TestBrowserToolsSerializePerTab(t *testing.T) {
	var mu sync.Mutex
	var order []string
	gate := make(chan struct{})

	slow := newGatedTool("browser_navigate", 0)
	slow.run = func() { order = append(order, "first-start"); <-gate; order = append(order, "first-end") }
	fast := newGatedTool("browser_vision", 0)
	fast.run = func() { mu.Lock(); order = append(order, "second"); mu.Unlock() }

	done := make(chan struct{})
	go func() {
		unlock := serializeBrowserTool("browser_navigate", map[string]interface{}{"tab_id": "t1"})
		slow.Execute(context.Background(), nil)
		unlock()
		close(done)
	}()

	// 等第一个工具真的开始，再请求第二个
	for {
		mu.Lock()
		started := len(order) > 0
		mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}

	second := make(chan struct{})
	go func() {
		unlock := serializeBrowserTool("browser_vision", map[string]interface{}{"tab_id": "t1"})
		fast.Execute(context.Background(), nil)
		unlock()
		close(second)
	}()

	// 第二个必须被挡住
	select {
	case <-second:
		t.Fatal("second browser tool ran while the first held the tab lock")
	case <-time.After(80 * time.Millisecond):
	}

	close(gate)
	<-done
	<-second

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[1] != "first-end" || order[2] != "second" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestBrowserToolsDifferentTabsDoNotBlock(t *testing.T) {
	block := make(chan struct{})
	first := make(chan struct{})
	go func() {
		unlock := serializeBrowserTool("browser_navigate", map[string]interface{}{"tab_id": "a"})
		close(first)
		<-block
		unlock()
	}()
	<-first

	got := make(chan struct{})
	go func() {
		unlock := serializeBrowserTool("browser_navigate", map[string]interface{}{"tab_id": "b"})
		unlock()
		close(got)
	}()

	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("different tabs must not block each other")
	}
	close(block)
}

func TestNonBrowserToolsAreNotSerialized(t *testing.T) {
	unlock := serializeBrowserTool("read_file", nil)
	unlock() // 不应 panic，也不应持有任何锁
}

// 超时判定与工具完成只差一点时，必须把真实结果交出去，而不是报
// "context deadline exceeded"（browser_back 已执行完却报错的根因）。
func TestTimeoutDeliversResultWithinGrace(t *testing.T) {
	exec := &DefaultToolExecutor{}
	tl := newGatedTool("slow_tool", 150*time.Millisecond)

	res := exec.ExecuteWithProtection(context.Background(), tl, nil, 100*time.Millisecond)
	if res.Error != nil {
		t.Fatalf("tool finished inside the grace window, want success, got %v", res.Error)
	}
}

func TestRealTimeoutStillFails(t *testing.T) {
	exec := &DefaultToolExecutor{}
	tl := newGatedTool("very_slow_tool", 3*time.Second)

	res := exec.ExecuteWithProtection(context.Background(), tl, nil, 50*time.Millisecond)
	if res.Error == nil {
		t.Fatal("want timeout error")
	}
	msg := res.Error.Error()
	if !strings.Contains(msg, "may already have completed") || !strings.Contains(msg, "browser_snapshot") {
		t.Fatalf("timeout message should tell the model how to verify state, got %q", msg)
	}
}
