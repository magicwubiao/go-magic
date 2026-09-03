package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// newDeadlineTestAgent 构造一个不发起网络请求的最小 Agent。
func newDeadlineTestAgent() *Agent {
	a := NewAIAgent(nil, nil, nil, "test system prompt")
	return a
}

func TestEstimateTurnDuration_Empty(t *testing.T) {
	a := newDeadlineTestAgent()
	if got := a.estimateTurnDuration(); got != defaultTurnDuration {
		t.Fatalf("expected default %v, got %v", defaultTurnDuration, got)
	}
}

func TestEstimateTurnDuration_P90(t *testing.T) {
	a := newDeadlineTestAgent()
	// 最近邻秩 P90：rank = ceil(0.9*N)
	// N=10 样本 [1..9,60] → rank 9 → 第9小 = 9s
	for _, d := range []time.Duration{
		1 * time.Second, 2 * time.Second, 3 * time.Second,
		4 * time.Second, 5 * time.Second, 6 * time.Second,
		7 * time.Second, 8 * time.Second, 9 * time.Second,
		60 * time.Second,
	} {
		a.turnDurations = append(a.turnDurations, d)
	}
	if got := a.estimateTurnDuration(); got != 9*time.Second {
		t.Fatalf("expected P90=9s (rank9/10), got %v", got)
	}

	// N=4 小样本含离群值 [1ms,1ms,1ms,5s] → rank ceil(3.6)=4 → 最大值 5s
	b := newDeadlineTestAgent()
	b.turnDurations = []time.Duration{
		time.Millisecond, time.Millisecond, time.Millisecond, 5 * time.Second,
	}
	if got := b.estimateTurnDuration(); got != 5*time.Second {
		t.Fatalf("expected P90=5s (small-n outlier), got %v", got)
	}
}

func TestEndTurnTiming_Cap32(t *testing.T) {
	a := newDeadlineTestAgent()
	for i := 0; i < 40; i++ {
		a.beginTurnTiming()
		time.Sleep(time.Millisecond)
		a.endTurnTiming()
	}
	if len(a.turnDurations) != 32 {
		t.Fatalf("expected cap of 32 samples, got %d", len(a.turnDurations))
	}
}

func TestCanStartAnotherTurn_NoDeadline(t *testing.T) {
	a := newDeadlineTestAgent()
	ctx := context.Background() // 无 deadline
	if !a.canStartAnotherTurn(ctx) {
		t.Fatal("without deadline the agent must always be allowed to continue")
	}
}

func TestCanStartAnotherTurn_EnoughTime(t *testing.T) {
	a := newDeadlineTestAgent()
	a.turnDurations = append(a.turnDurations, 2*time.Second)
	deadline := time.Now().Add(30 * time.Second) // 远超 2s*1.25
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if !a.canStartAnotherTurn(ctx) {
		t.Fatal("with ample time remaining, another turn should be allowed")
	}
}

func TestCanStartAnotherTurn_TooLittleTime(t *testing.T) {
	a := newDeadlineTestAgent()
	a.turnDurations = append(a.turnDurations, 10*time.Second)
	deadline := time.Now().Add(3 * time.Second) // < 10s*1.25
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if a.canStartAnotherTurn(ctx) {
		t.Fatal("with insufficient remaining time, another turn should be blocked")
	}
}

func TestWriteDeadlineCheckpoint(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", oldHome)

	a := newDeadlineTestAgent()
	a.session = "unit_test_session"
	a.iterationCount = 7
	a.toolCallHistory = []string{"read_file", "write_file", "execute_command"}
	a.history = append(a.history,
		types.Message{Role: "user", Content: strings.Repeat("任务描述", 500)},
		types.Message{Role: "assistant", Content: "step1 done"},
	)

	path := a.writeDeadlineCheckpoint("完成一个大任务", "test reason")
	if path == "" {
		t.Fatal("expected checkpoint path, got empty")
	}
	if !strings.Contains(path, ".magic") || !strings.Contains(path, "checkpoints") {
		t.Fatalf("unexpected checkpoint path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("checkpoint unreadable: %v", err)
	}
	var cp DeadlineCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatalf("checkpoint invalid JSON: %v", err)
	}
	if cp.Session != "unit_test_session" || cp.Completed != 7 || cp.ToolCalls != 3 {
		t.Fatalf("checkpoint fields wrong: %+v", cp)
	}
	if len(cp.LastMessages) != 3 { // system prompt + 2 条测试消息
		t.Fatalf("expected 3 history msgs (system+user+assistant), got %d", len(cp.LastMessages))
	}
	if cp.LastMessages[0].Role != "system" {
		t.Fatalf("first msg should be system prompt, got %q", cp.LastMessages[0].Role)
	}
	if len(cp.Task) > 2100 { // TruncateDetailed(2000) + 指针
		t.Fatalf("task not truncated: %d chars", len(cp.Task))
	}
}

func TestGracefulDeadlineFinish_MessageAndEvent(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", oldHome)

	a := newDeadlineTestAgent()
	events := a.bus.Subscribe(8)
	a.session = "graceful_sess"

	msg := a.gracefulDeadlineFinish("some task")
	if !strings.Contains(msg, "[Deadline approaching]") ||
		!strings.Contains(msg, "checkpoints") {
		t.Fatalf("unexpected graceful message: %s", msg)
	}
	select {
	case ev := <-events.C:
		m, ok := ev.Data.(map[string]interface{})
		if !ok || m["reason"] != "deadline_graceful_finish" {
			t.Fatalf("unexpected warning event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no warning event emitted")
	}
}

func TestSanitizeAgentSlug(t *testing.T) {
	if got := SanitizeAgentSlug(""); got != "session" {
		t.Fatalf("empty slug handling wrong: %q", got)
	}
	got := SanitizeAgentSlug("../evil sess/name")
	if strings.ContainsAny(got, "/.") {
		t.Fatalf("unsafe slug: %q", got)
	}
	long := SanitizeAgentSlug(strings.Repeat("x", 100))
	if len(long) > 48 {
		t.Fatalf("slug not capped: %d", len(long))
	}
}

func TestCheckpointFilePermissions(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", oldHome)

	a := newDeadlineTestAgent()
	a.session = "perm_check"
	path := a.writeDeadlineCheckpoint("t", "r")
	if path == "" {
		t.Fatal("checkpoint not written")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("checkpoint perms too open: %v", info.Mode().Perm())
	}
	_ = filepath.Base(path)
}

// capTrackingRegistry 模拟带耗时的工具执行，追踪峰值在飞并发数。
type capTrackingRegistry struct {
	mu       sync.Mutex
	inFlight int
	peak     int
}

func (r *capTrackingRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	r.mu.Lock()
	r.inFlight++
	if r.inFlight > r.peak {
		r.peak = r.inFlight
	}
	r.mu.Unlock()

	time.Sleep(50 * time.Millisecond) // 模拟真实工具开销

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()
	return "ok", nil
}

// TestMaxParallelTools_ConcurrencyCap 验证并行工具组受全局信号量约束：
// 上限 2、6 个并行调用时峰值在飞数量不得超 2。
func TestMaxParallelTools_ConcurrencyCap(t *testing.T) {
	a := NewAIAgent(nil, nil, nil, "sys")
	a.ApplyOption(WithMaxParallelTools(2))

	tracker := &capTrackingRegistry{}
	a.registry = tracker

	calls := []types.ToolCall{
		{ID: "1", Function: types.Function{Name: "tool_read_a", Arguments: "{}"}},
		{ID: "2", Function: types.Function{Name: "tool_read_b", Arguments: "{}"}},
		{ID: "3", Function: types.Function{Name: "tool_read_c", Arguments: "{}"}},
		{ID: "4", Function: types.Function{Name: "tool_read_d", Arguments: "{}"}},
		{ID: "5", Function: types.Function{Name: "tool_read_e", Arguments: "{}"}},
		{ID: "6", Function: types.Function{Name: "tool_read_f", Arguments: "{}"}},
	}

	if _, err := a.executeToolsWithHooks(context.Background(), calls); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if tracker.peak > 2 {
		t.Fatalf("concurrency cap violated: peak in-flight = %d (want <= 2)", tracker.peak)
	}
}
