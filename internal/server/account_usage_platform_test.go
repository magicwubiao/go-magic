package server

// 回合结束的 token 记账（accountTurnUsage）不得改动会话的 platform 列。
//
// 回归背景：该函数曾把 `s.cfgProvider()`（**LLM 供应商名**，如 mimo / huoshan）
// 当作 platform 实参传给 SaveSessionDataFromMap，而后者把它写进
// `UPDATE sessions SET platform = ?`。于是每跑完一个回合，web 会话的 platform
// 就被改名成供应商名；前端侧栏/搜索/目录分组都按 `source === 'web'` 过滤
// （chat.ts::loadSessions、ChatView.vue::isWebSession），会话随即从侧栏里
// 消失、残留条目的标题也不再刷新（用户报"chat 页面对话标题名称不更新了"）。
//
// 这里端到端钉住：会话 platform 是 web，跑完一回合记账后必须仍是 web，
// 且 token 必须真的累加进去（别为了不改 platform 把记账一起丢了）。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
)

func TestAccountTurnUsageKeepsSessionPlatform(t *testing.T) {
	store, err := session.NewStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now()
	if err := store.SaveSession(ctx, &session.Session{
		ID:        "sess-web",
		Profile:   "magic",
		Platform:  "web",
		Messages:  []types.Message{{ID: "m1", Role: "user", Content: "你好"}},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// cfg.Provider 故意设成供应商名：旧实现会把它写进 platform。
	s := &Server{
		cfg:           &config.Config{Provider: "mimo"},
		agents:        make(map[string]*agent.Agent),
		sessionTokens: make(map[string][3]int),
		sessionStore:  store,
	}

	prov := &stubUsageProvider{}
	a := agent.NewAIAgent(prov, nil, nil, "")
	if _, err := a.RunConversation(ctx, "hi"); err != nil {
		t.Fatalf("RunConversation: %v", err)
	}
	s.agents["sess-web"] = a

	s.accountTurnUsage("sess-web")

	got, err := store.LoadSession(ctx, "sess-web")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got.Platform != "web" {
		t.Fatalf("记账后 platform = %q，期望 \"web\"：web 会话会被前端侧栏/目录分组过滤掉，"+
			"标题也不再刷新", got.Platform)
	}
	if got.InputTokens <= 0 && got.OutputTokens <= 0 {
		t.Fatalf("token 没有被记账（in=%d, out=%d）：/usage 页面会永远为空",
			got.InputTokens, got.OutputTokens)
	}
}
