package server

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/usage"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// stubUsageProvider 每轮返回固定 usage 的假 provider。
type stubUsageProvider struct {
	calls int
}

func (p *stubUsageProvider) Name() string { return "stub" }

func (p *stubUsageProvider) Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	p.calls++
	return &provider.ChatResponse{
		Content: "hello",
		Usage: &provider.Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}, nil
}

// TestAccountTurnUsageRecordsOnce 钉死「回合结束后记账」这条链：
//   - 有增量时必须写进 usage 统计（/usage 页面的数据源）；
//   - 没有新消耗时重复调用不得重复记账（delta 基线只能消费一次）。
func TestAccountTurnUsageRecordsOnce(t *testing.T) {
	mgr, err := usage.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	s := &Server{
		usageMgr:      mgr,
		cfg:           &config.Config{Provider: "stub"},
		agents:        make(map[string]*agent.Agent),
		sessionTokens: make(map[string][3]int),
	}

	prov := &stubUsageProvider{}
	a := agent.NewAIAgent(prov, nil, nil, "")
	if _, err := a.RunConversation(context.Background(), "hi"); err != nil {
		t.Fatalf("RunConversation: %v", err)
	}
	in, out, _ := a.GetTokenStats()
	if in == 0 && out == 0 {
		t.Fatalf("agent 没有累计 token，测试前置条件不成立")
	}
	s.agents["sess-1"] = a

	s.accountTurnUsage("sess-1")

	today, err := mgr.GetTodayStats()
	if err != nil {
		t.Fatalf("GetTodayStats: %v", err)
	}
	if today.TotalInput != in || today.TotalOutput != out {
		t.Fatalf("记账后今日统计 = (%d,%d)，期望 (%d,%d)", today.TotalInput, today.TotalOutput, in, out)
	}

	// 同一回合再记一次：基线已推进，delta 为 0，不得重复累加。
	s.accountTurnUsage("sess-1")
	today2, err := mgr.GetTodayStats()
	if err != nil {
		t.Fatalf("GetTodayStats: %v", err)
	}
	if today2.TotalInput != today.TotalInput || today2.TotalOutput != today.TotalOutput {
		t.Fatalf("重复记账：今日统计从 (%d,%d) 变成 (%d,%d)",
			today.TotalInput, today.TotalOutput, today2.TotalInput, today2.TotalOutput)
	}
}

// TestRunQueuedTurnAccountsUsage 是结构断言：队列路径的回合必须在收尾处
// 调用 accountTurnUsage。队列改造后回合跑在 worker 里，不再经过旧 /api/chat
// 的收尾，漏掉这一行 /usage 页面就永远没有新数据（用户报「用量统计不到」）。
func TestRunQueuedTurnAccountsUsage(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "chatqueue.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse chatqueue.go: %v", err)
	}
	var found bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runQueuedTurn" {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "accountTurnUsage" {
				found = true
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatalf("runQueuedTurn 未调用 accountTurnUsage：队列路径的用量不会被记账，/usage 页面将永远为空")
	}
}

// TestTurnTokenDeltaHandlesAgentReset agent 被重建后累计值归零再增长时，
// delta 不得为负（负增量会把历史统计冲减掉）。
func TestTurnTokenDeltaHandlesAgentReset(t *testing.T) {
	s := &Server{
		cfg:           &config.Config{Provider: "stub"},
		agents:        make(map[string]*agent.Agent),
		sessionTokens: make(map[string][3]int),
	}
	// 基线：上一回合已累计到 (1000, 500, 10)
	s.sessionTokens["sess-x"] = [3]int{1000, 500, 10}

	a := agent.NewAIAgent(&stubUsageProvider{}, nil, nil, "")
	if _, err := a.RunConversation(context.Background(), "hi"); err != nil {
		t.Fatalf("RunConversation: %v", err)
	}
	s.agents["sess-x"] = a

	in, out, cache := s.turnTokenDelta("sess-x")
	if in < 0 || out < 0 || cache < 0 {
		t.Fatalf("agent 重建后 delta 出现负值: (%d,%d,%d)", in, out, cache)
	}
	if in == 0 && out == 0 {
		t.Fatalf("agent 重建后 delta 应为当前全量，却为 0")
	}
	_ = time.Now
}
