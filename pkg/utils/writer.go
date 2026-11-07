package utils

import "context"

// writerIDKey 是 context 里"谁在写文件"的键。
//
// 用途：并行子代理共享同一个工作区时，文件工具的丢弃更新检测需要区分写者。
// 主 agent 不设置这个值（取到空串，表示"单一主写者"）；subagent 在
// Manager.executeTask 里把 task ID 注入 ctx，于是同一进程内的并发写者
// 天然可区分，不需要额外传参、也不破坏工具签名。
type writerIDKey struct{}

// WithWriterID 把写者标识写入 ctx。空 id 视为取消标识。
func WithWriterID(ctx context.Context, id string) context.Context {
	if id == "" || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, writerIDKey{}, id)
}

// WriterIDFromContext 读取写者标识；未设置时返回空串（主 agent）。
func WriterIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(writerIDKey{}).(string); ok {
		return v
	}
	return ""
}
