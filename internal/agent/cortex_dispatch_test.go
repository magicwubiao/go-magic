package agent

import (
	"context"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// TestRunConversationWithDisabledCortexManager 锁死一个 fatal 级回归。
//
// 旧实现：RunConversation / RunConversationWithMedia 只判 `a.cortexManager != nil`
// 就分流转给 RunWithCortex，而 RunWithCortex 在 `!IsEnabled()` 时又回落到
// RunConversation —— 两边互相调用，结局是 **不可 recover 的 fatal stack overflow，
// 整个进程直接死掉**（连测试二进制一起崩，所以这个用例本身就是探针：如果哪天
// 有人把 IsEnabled() 判定改回去，这里不是断言失败，而是 runner 报 fatal error）。
//
// "非 nil 但 IsEnabled()==false" 不是假想形态：NewManagerWithProfileAndConfig 在
// config.Enabled=false 时返回的就是这样一个空壳，而 internal/server/server.go 会把它
// 交给每个会话 agent（CLI 的几条路径都有 cfg.Cortex.Enabled 兜着，server 侧没有）。
func TestRunConversationWithDisabledCortexManager(t *testing.T) {
	prov := &mockToolProvider{responses: []*provider.ChatResponse{
		{Content: "ok"},   // RunConversation
		{Content: "done"}, // RunConversationWithMedia
	}}
	registry := &mockRegistry{tools: map[string]func(map[string]interface{}) (string, error){}}

	mgr := cortex.NewManagerWithProfileAndConfig(t.TempDir(), prov, "",
		&cortex.ManagerConfig{Enabled: false})
	if mgr == nil {
		t.Fatal("precondition failed: expected a non-nil manager shell")
	}
	if mgr.IsEnabled() {
		t.Fatal("precondition failed: manager must report disabled")
	}
	// 回合收尾会把沉淀抽取丢到后台（哪怕这里因 disabled 直接返回）：
	// 等它落定再让 t.TempDir() 清理，避免写盘 goroutine 与目录删除竞态。
	t.Cleanup(mgr.WaitPendingWrites)

	ag := NewEnhancedAgent(prov, registry, nil, "You are a helpful assistant.", WithCortex(mgr))

	resp, err := ag.RunConversation(context.Background(), "hello")
	if err != nil {
		t.Fatalf("RunConversation should fall through to the local path, got error: %v", err)
	}
	if resp == "" {
		t.Fatal("expected a non-empty response from the local path")
	}

	// 同一判定在带媒体的入口上，走同一条分流（历史上的两个坏点之一）。
	resp, err = ag.RunConversationWithMedia(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("RunConversationWithMedia should fall through to the local path, got error: %v", err)
	}
	if resp == "" {
		t.Fatal("expected a non-empty response from the media path")
	}
}
