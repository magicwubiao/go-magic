package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/subagent"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// stubToolCallProvider 模拟支持工具调用的 provider。
// 按 calls 序列依次返回响应，用来驱动运行器的工具循环。
type stubToolCallProvider struct {
	calls      []*provider.ChatResponse
	idx        int
	seenTools  [][]map[string]interface{} // 记录每次请求传入的 tools，验证 schema 真的传下去了
	seenMsgLen []int
}

func (p *stubToolCallProvider) Name() string { return "stub" }

func (p *stubToolCallProvider) Chat(_ context.Context, _ []provider.Message) (*provider.ChatResponse, error) {
	return nil, errors.New("Chat should not be used when tools are available")
}

func (p *stubToolCallProvider) ChatWithTools(_ context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error) {
	p.seenTools = append(p.seenTools, tools)
	p.seenMsgLen = append(p.seenMsgLen, len(messages))
	if p.idx >= len(p.calls) {
		return nil, errors.New("stub provider ran out of canned responses")
	}
	resp := p.calls[p.idx]
	p.idx++
	return resp, nil
}

// echoTool 记录自身被调用时的参数，便于断言工具确实被执行了。
type echoTool struct {
	calls []map[string]interface{}
}

func (t *echoTool) Name() string        { return "echo" }
func (t *echoTool) Description() string { return "echoes input" }
func (t *echoTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"msg": map[string]interface{}{"type": "string"}},
	}
}

func (t *echoTool) Execute(_ context.Context, params map[string]interface{}) (interface{}, error) {
	t.calls = append(t.calls, params)
	if msg, ok := params["msg"].(string); ok {
		return "echo:" + msg, nil
	}
	return "echo:(empty)", nil
}

// failingTool 始终返回错误，用于验证工具报错不会中断整个子代理。
type failingTool struct{}

func (t *failingTool) Name() string        { return "boom" }
func (t *failingTool) Description() string { return "always fails" }
func (t *failingTool) Schema() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (t *failingTool) Execute(context.Context, map[string]interface{}) (interface{}, error) {
	return nil, errors.New("tool exploded")
}

// testAdapter 把 *tool.Registry 包成 subagent 侧可直接取用的适配器。
// 与 cmd/magic 的 toolRegistryAdapter 语义一致（含 Execute 方法集）。
type testAdapter struct {
	registry *tool.Registry
}

func (a *testAdapter) List() []string { return a.registry.List() }

func (a *testAdapter) Get(name string) (subagent.Tool, error) {
	t, err := a.registry.Get(name)
	if err != nil {
		return nil, err
	}
	return &testToolAdapter{tool: t}, nil
}

type testToolAdapter struct{ tool tool.Tool }

func (a *testToolAdapter) Name() string                   { return a.tool.Name() }
func (a *testToolAdapter) Description() string            { return a.tool.Description() }
func (a *testToolAdapter) Schema() map[string]interface{} { return a.tool.Schema() }
func (a *testToolAdapter) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return a.tool.Execute(ctx, params)
}

func newTestRunner(prov provider.Provider, reg subagent.ToolRegistry, schemas []map[string]interface{}) *simpleAgentRunner {
	return &simpleAgentRunner{
		provider:     prov,
		registry:     reg,
		toolsSchema:  schemas,
		systemPrompt: "you are a test subagent",
	}
}

// TestRunConversationExecutesToolLoop 是本文件的核心回归。
//
// 旧实现只调用一次 provider.Chat 并直接返回 content，既没把 toolsSchema 传给
// 模型，也没有工具执行循环。结果是子代理声称"能自主完成子任务"，实际退化成
// 一次纯文本问答——工具调用请求被静默丢弃，且不会有任何报错。这个测试锁死
// 修复后的行为：工具必须被真正执行，结果必须回喂，循环必须能收敛到最终答复。
func TestRunConversationExecutesToolLoop(t *testing.T) {
	registry := tool.NewRegistry()
	echo := &echoTool{}
	registry.Register(echo)

	schemas := registry.ListWithSchemas()
	if len(schemas) == 0 {
		t.Fatal("expected echo tool schema to be available")
	}

	prov := &stubToolCallProvider{
		calls: []*provider.ChatResponse{
			// 第一轮：模型请求调用 echo。
			{
				Content: "let me check",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "echo", Arguments: map[string]interface{}{"msg": "hello"}},
				},
			},
			// 第二轮：拿到工具结果后给出最终答复。
			{Content: "final answer"},
		},
	}

	runner := newTestRunner(prov, &testAdapter{registry: registry}, schemas)

	out, err := runner.RunConversation(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("RunConversation returned error: %v", err)
	}
	if out != "final answer" {
		t.Fatalf("expected final answer, got %q", out)
	}

	// 工具必须被真的执行，且收到正确参数。
	if len(echo.calls) != 1 {
		t.Fatalf("expected echo tool to be called exactly once, got %d", len(echo.calls))
	}
	if got := echo.calls[0]["msg"]; got != "hello" {
		t.Fatalf("expected msg=hello, got %v", got)
	}

	// toolsSchema 必须传给了 provider——这正是旧实现丢掉的环节。
	if len(prov.seenTools) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(prov.seenTools))
	}
	for i, sent := range prov.seenTools {
		if len(sent) == 0 {
			t.Fatalf("provider call %d received empty tools schema", i)
		}
	}

	// 第二轮请求应当带上 assistant(tool_calls) + tool 消息，共 4 条
	// （system / user / assistant / tool）。少了 tool 消息链，provider 会 400。
	if prov.seenMsgLen[len(prov.seenMsgLen)-1] != 4 {
		t.Fatalf("expected 4 messages on final provider call, got %d", prov.seenMsgLen[len(prov.seenMsgLen)-1])
	}
}

// TestRunConversationNoToolCallReturnsImmediately 验证模型直接答复时不多绕一轮。
func TestRunConversationNoToolCallReturnsImmediately(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&echoTool{})

	prov := &stubToolCallProvider{
		calls: []*provider.ChatResponse{{Content: "direct answer"}},
	}
	runner := newTestRunner(prov, &testAdapter{registry: registry}, registry.ListWithSchemas())

	out, err := runner.RunConversation(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "direct answer" {
		t.Fatalf("expected direct answer, got %q", out)
	}
	if prov.idx != 1 {
		t.Fatalf("expected exactly 1 provider call, got %d", prov.idx)
	}
}

// TestRunConversationToolErrorDoesNotAbortSubagent 验证单个工具报错不会让整个
// 子代理失败——错误信息对模型同样有价值，应回喂给它换一种做法重试。
func TestRunConversationToolErrorDoesNotAbortSubagent(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&failingTool{})

	prov := &stubToolCallProvider{
		calls: []*provider.ChatResponse{
			{ToolCalls: []types.ToolCall{{ID: "c1", Name: "boom", Arguments: map[string]interface{}{}}}},
			{Content: "recovered"},
		},
	}
	runner := newTestRunner(prov, &testAdapter{registry: registry}, registry.ListWithSchemas())

	out, err := runner.RunConversation(context.Background(), "try the failing tool")
	if err != nil {
		t.Fatalf("tool error should not abort the subagent, got: %v", err)
	}
	if out != "recovered" {
		t.Fatalf("expected recovered, got %q", out)
	}
}

// TestRunConversationUnknownToolReportsBack 验证调用不存在的工具时回喂错误，
// 而不是 panic 或静默忽略。
func TestRunConversationUnknownToolReportsBack(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&echoTool{})

	prov := &stubToolCallProvider{
		calls: []*provider.ChatResponse{
			{ToolCalls: []types.ToolCall{{ID: "c1", Name: "no_such_tool", Arguments: map[string]interface{}{}}}},
			{Content: "switched approach"},
		},
	}
	runner := newTestRunner(prov, &testAdapter{registry: registry}, registry.ListWithSchemas())

	out, err := runner.RunConversation(context.Background(), "call a missing tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "switched approach" {
		t.Fatalf("expected switched approach, got %q", out)
	}
}

// TestRunConversationExhaustedTurnsKeepsPartialOutput 验证触达回合上限时
// 保留最后可见内容，避免调用方只拿到一句错误、丢失子代理已产出的进度。
func TestRunConversationExhaustedTurnsKeepsPartialOutput(t *testing.T) {
	registry := tool.NewRegistry()
	echo := &echoTool{}
	registry.Register(echo)

	// 每次都请求工具，永远不收敛 —— 必然撞上 subAgentMaxTurns。
	calls := make([]*provider.ChatResponse, subAgentMaxTurns+2)
	for i := range calls {
		calls[i] = &provider.ChatResponse{
			Content: "working",
			ToolCalls: []types.ToolCall{
				{ID: "c", Name: "echo", Arguments: map[string]interface{}{"msg": "again"}},
			},
		}
	}
	prov := &stubToolCallProvider{calls: calls}
	runner := newTestRunner(prov, &testAdapter{registry: registry}, registry.ListWithSchemas())

	out, err := runner.RunConversation(context.Background(), "never finishes")
	if err == nil {
		t.Fatal("expected an error when turns are exhausted")
	}
	if !strings.Contains(err.Error(), "exceeded maximum turns") {
		t.Fatalf("expected 'exceeded maximum turns' in error, got: %v", err)
	}
	if out != "working" {
		t.Fatalf("expected partial output to be preserved, got %q", out)
	}
	if len(echo.calls) != subAgentMaxTurns {
		t.Fatalf("expected %d tool executions, got %d", subAgentMaxTurns, len(echo.calls))
	}
}

// TestRunConversationContextCancel 验证 ctx 取消能及时中断循环。
func TestRunConversationContextCancel(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&echoTool{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进入循环前即已取消

	calls := make([]*provider.ChatResponse, subAgentMaxTurns+2)
	for i := range calls {
		calls[i] = &provider.ChatResponse{
			ToolCalls: []types.ToolCall{{ID: "c", Name: "echo", Arguments: map[string]interface{}{"msg": "x"}}},
		}
	}
	prov := &stubToolCallProvider{calls: calls}
	runner := newTestRunner(prov, &testAdapter{registry: registry}, registry.ListWithSchemas())

	_, err := runner.RunConversation(ctx, "cancelled")
	if err == nil {
		t.Fatal("expected an error on cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to be wrapped, got: %v", err)
	}
}

// TestRunConversationNonToolProviderFallsBack 验证不支持工具调用的 provider
// 退化为单次对话，而不是报错或产生"我调用了工具"的错觉。
func TestRunConversationNonToolProviderFallsBack(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&echoTool{})

	prov := &plainProvider{content: "plain reply"}
	runner := newTestRunner(prov, &testAdapter{registry: registry}, registry.ListWithSchemas())

	out, err := runner.RunConversation(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "plain reply" {
		t.Fatalf("expected plain reply, got %q", out)
	}
}

// plainProvider 只实现 provider.Provider，不实现 ToolCaller。
type plainProvider struct{ content string }

func (p *plainProvider) Name() string { return "plain" }

func (p *plainProvider) Chat(_ context.Context, _ []provider.Message) (*provider.ChatResponse, error) {
	return &provider.ChatResponse{Content: p.content}, nil
}
