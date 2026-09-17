package agent

import (
	"context"
)

// toolOpsKey 是携带 ToolOpsObserver 的 context key。
type toolOpsKey struct{}

// ToolOpsObserver 观察单个工具调用的真实执行过程。
//
// 与基于 SSE 文本标记（>>>TOOL_START/TOOL_RESULT）解析的旧方案不同，
// 观察者在 agent 真正执行工具的代码路径上被调用，因此能拿到：
//   - 完整的工具参数（marker 里的 args 被截断到 200 字符且 JSON 已损坏，
//     无法可靠提取 write_file 等长参数工具的路径）；
//   - 真实的执行成败（失败的工具不会产生变更）；
//   - 正确的时序（Starting 发生在 registry.Execute 之前，此时文件尚未
//     被本工具修改，是做"写前快照"的唯一可靠时机）。
//
// 观察者通过 ctx 按"请求"注入，而非挂在共享的 Agent 实例上，避免并发
// 会话互相污染。观察者自身必须并发安全（并行工具组会并发回调）。
type ToolOpsObserver interface {
	// ToolStarting 在工具执行前回调（被审批拒绝的工具不会走到这里）。
	ToolStarting(ctx context.Context, toolName string, args map[string]interface{})
	// ToolFinished 在工具执行完成后回调，err 非空表示执行失败。
	ToolFinished(ctx context.Context, toolName string, args map[string]interface{}, err error)
	// TurnFinished 在一个回合结束时回调（无论成功、出错还是被取消）。
	// 回合已经不再与 SSE 连接一一绑定：客户端可能在回合进行中才接上流，
	// 也可能整个回合都没人监听。此时"本轮改了哪些文件"的收尾信息必须
	// 能够持久化到会话，而不是只随 done 事件推给当时恰好连着的那条连接。
	// 实现（TurnFileOpTracker）需要自行处理重复调用。
	TurnFinished(ctx context.Context)
}

// WithToolOps 把观察者写入 ctx，供 RunConversationStream/executeToolsWithHooks
// 在工具执行路径上读取。nil 观察者等价于不注入。
func WithToolOps(ctx context.Context, obs ToolOpsObserver) context.Context {
	if obs == nil {
		return ctx
	}
	return context.WithValue(ctx, toolOpsKey{}, obs)
}

// toolOpsFromCtx 从 ctx 读取观察者；未注入时返回 nil（静默跳过）。
func toolOpsFromCtx(ctx context.Context) ToolOpsObserver {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(toolOpsKey{}).(ToolOpsObserver); ok {
		return v
	}
	return nil
}

// notifyTurnFinished 在回合结束时调用观察者的收尾钩子。所有退出路径
// （正常结束、出错、被取消）都必须走到这里，否则"本轮变更的文件"会在
// 无人监听 SSE 的回合里丢掉。nil 观察者与 nil ctx 都安全。
func notifyTurnFinished(ctx context.Context) {
	if ctx == nil {
		return
	}
	if obs := toolOpsFromCtx(ctx); obs != nil {
		obs.TurnFinished(ctx)
	}
}
