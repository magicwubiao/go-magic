package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

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

// abortingProvider 按序返回若干响应，随后让下一次请求失败，模拟上游断流。
type abortingProvider struct {
	responses []*provider.ChatResponse
	idx       int
	err       error
}

func (p *abortingProvider) Name() string { return "aborting" }

func (p *abortingProvider) Chat(_ context.Context, _ []provider.Message) (*provider.ChatResponse, error) {
	return nil, errors.New("Chat should not be used when tools are available")
}

func (p *abortingProvider) ChatWithTools(_ context.Context, _ []provider.Message, _ []map[string]interface{}) (*provider.ChatResponse, error) {
	if p.idx >= len(p.responses) {
		return nil, p.err
	}
	resp := p.responses[p.idx]
	p.idx++
	return resp, nil
}

// TestRunConversationStreamAbortKeepsWorkEvidence 复现真实故障：子代理已经改完
// 文件、跑完测试，只在生成最终回传摘要时上游断流（stream operation aborted:
// context deadline exceeded）。此时任务状态是 failed，但工作成果完整落盘。
//
// 若中断时只返回空字符串，poll_task 的 partial_output 会是空的，调用方只看
// status 就会重派一遍——白烧 token，还可能覆盖掉已经正确的实现。这个测试锁死：
// 中断返回的 output 必须带上"做过了什么"的证据。
func TestRunConversationStreamAbortKeepsWorkEvidence(t *testing.T) {
	registry := tool.NewRegistry()
	echo := &echoTool{}
	registry.Register(echo)

	prov := &abortingProvider{
		responses: []*provider.ChatResponse{
			{
				Content:   "改 price.go：Total 补上 quantity 相乘",
				ToolCalls: []types.ToolCall{{ID: "c1", Name: "echo", Arguments: map[string]interface{}{"msg": "go test ./cart/..."}}},
			},
			{
				Content:   "补充表驱动测试",
				ToolCalls: []types.ToolCall{{ID: "c2", Name: "echo", Arguments: map[string]interface{}{"msg": "git diff"}}},
			},
		},
		err: errors.New("stream operation aborted: context deadline exceeded"),
	}

	runner := newTestRunner(prov, &testAdapter{registry: registry}, registry.ListWithSchemas())
	out, err := runner.RunConversation(context.Background(), "修 cart 的 Total bug")

	if err == nil {
		t.Fatal("expected an error when the upstream stream aborts")
	}
	if !strings.Contains(err.Error(), "stream operation aborted") {
		t.Fatalf("expected upstream error to be preserved, got: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("中断时必须回传抢救内容，否则调用方无法判断改动是否已落盘")
	}

	for _, want := range []string{
		"aborted run salvage",
		"tool_calls=2",
		"does NOT mean nothing happened", // 明确提示"失败≠没干活"
		"改 price.go",                     // 中断时最后一轮助手的说明文字被保留
		"补充表驱动测试",                        // 且是"最后"那段，不是更早那段
		"echo:go test ./cart/...",        // 最近工具输出（已执行过的动作）
		"echo:git diff",
		"echo", // 工具名标注
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("抢救内容缺少 %q，实际输出：\n%s", want, out)
		}
	}

	if len(echo.calls) != 2 {
		t.Fatalf("两个工具调用都应已执行，实际 %d", len(echo.calls))
	}
}

// TestSalvageAbortedRunTruncatesOnRuneBoundary 保证截断按 rune 进行：
// 按字节切会把中文劈成乱码，回传给主 agent 后无法阅读。
func TestSalvageAbortedRunTruncatesOnRuneBoundary(t *testing.T) {
	longCN := strings.Repeat("多件商品少收钱，必须乘上数量。", 200)
	messages := []provider.Message{
		{Role: "assistant", Content: "写测试", ToolCalls: []types.ToolCall{{ID: "x", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "x", Content: longCN},
	}

	out := salvageAbortedRun(messages, map[string]string{"x": "write_file"}, "总结一下", 3, 1)
	if !utf8.ValidString(out) {
		t.Fatal("截断后产生了非法 UTF-8（按字节截断的典型症状）")
	}
	if strings.ContainsRune(out, '\uFFFD') {
		t.Fatal("截断产生了替换字符，说明切在了多字节字符中间")
	}
	if !strings.Contains(out, "[truncated, original") {
		t.Fatalf("超长工具输出应被截断并标注，实际输出：\n%s", out)
	}
	if len([]rune(out)) > salvageMaxNotes*salvageNoteRunes+salvageMaxToolResults*salvageToolResultRunes+800 {
		t.Fatalf("抢救内容超出预算：%d runes", len([]rune(out)))
	}
}

// TestSalvageAbortedRunEmptyWhenNothingHappened 保证"确实什么都没做"时返回空串，
// 不伪造一段看似有产出的文本。
func TestSalvageAbortedRunEmptyWhenNothingHappened(t *testing.T) {
	if out := salvageAbortedRun(nil, nil, "", 0, 0); out != "" {
		t.Fatalf("无任何内容时应返回空串，实际：%q", out)
	}
}
