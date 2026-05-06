package tool

import (
	"context"
	"time"
)

// ToolContext 工具执行上下文，扩展标准 context.Context
type ToolContext struct {
	context.Context
	SessionID   string            // 会话ID
	UserID      string            // 用户ID
	RequestID   string            // 请求ID
	ToolName    string            // 当前工具名称
	Metadata    map[string]any    // 元数据
	Logger      Logger            // 日志器
	Metrics     MetricsRecorder   // 指标记录器
	StartTime   time.Time         // 开始时间
}

// NewToolContext 创建工具上下文
func NewToolContext(parent context.Context) *ToolContext {
	return &ToolContext{
		Context:   parent,
		Metadata:  make(map[string]any),
		StartTime: time.Now(),
	}
}

// WithSession 设置会话ID
func (tc *ToolContext) WithSession(sessionID string) *ToolContext {
	tc.SessionID = sessionID
	return tc
}

// WithUser 设置用户ID
func (tc *ToolContext) WithUser(userID string) *ToolContext {
	tc.UserID = userID
	return tc
}

// WithRequest 设置请求ID
func (tc *ToolContext) WithRequest(requestID string) *ToolContext {
	tc.RequestID = requestID
	return tc
}

// WithTool 设置工具名称
func (tc *ToolContext) WithTool(toolName string) *ToolContext {
	tc.ToolName = toolName
	return tc
}

// WithLogger 设置日志器
func (tc *ToolContext) WithLogger(logger Logger) *ToolContext {
	tc.Logger = logger
	return tc
}

// WithMetrics 设置指标记录器
func (tc *ToolContext) WithMetrics(metrics MetricsRecorder) *ToolContext {
	tc.Metrics = metrics
	return tc
}

// SetMetadata 设置元数据
func (tc *ToolContext) SetMetadata(key string, value any) {
	tc.Metadata[key] = value
}

// GetMetadata 获取元数据
func (tc *ToolContext) GetMetadata(key string) (any, bool) {
	val, ok := tc.Metadata[key]
	return val, ok
}

// Elapsed 返回已用时间
func (tc *ToolContext) Elapsed() time.Duration {
	return time.Since(tc.StartTime)
}

// MetricsRecorder 指标记录器接口
type MetricsRecorder interface {
	RecordToolExecution(toolName string, duration time.Duration, success bool, errorMsg string)
	RecordToolMetric(toolName string, metricName string, value float64)
}

// DefaultMetricsRecorder 默认指标记录器（空实现）
type DefaultMetricsRecorder struct{}

func (r *DefaultMetricsRecorder) RecordToolExecution(toolName string, duration time.Duration, success bool, errorMsg string) {
	// 空实现，可扩展为 Prometheus/StatsD 等
}

func (r *DefaultMetricsRecorder) RecordToolMetric(toolName string, metricName string, value float64) {
	// 空实现
}

// ToolContextKey 用于 context 存储的 key 类型
type toolContextKey string

const toolContextKeyVal toolContextKey = "tool_context"

// ToContext 将 ToolContext 转为标准 context.Context
func (tc *ToolContext) ToContext() context.Context {
	return context.WithValue(tc.Context, toolContextKeyVal, tc)
}

// FromContext 从 context.Context 获取 ToolContext
func FromContext(ctx context.Context) *ToolContext {
	if tc, ok := ctx.Value(toolContextKeyVal).(*ToolContext); ok {
		return tc
	}
	return NewToolContext(ctx)
}
