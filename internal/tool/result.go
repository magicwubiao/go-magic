package tool

import (
	"time"
)

// ToolResult 标准化工具执行结果
type ToolResult struct {
	Success   bool                   // 是否成功
	Data      any                    // 结果数据
	Error     string                 // 错误信息
	ErrorCode string                 // 错误码
	Metadata  map[string]any         // 执行元数据
	Duration  time.Duration          // 执行耗时
	Warnings  []string               // 警告信息
}

// NewSuccessResult 创建成功结果
func NewSuccessResult(data any) *ToolResult {
	return &ToolResult{
		Success:  true,
		Data:     data,
		Metadata: make(map[string]any),
	}
}

// NewErrorResult 创建错误结果
func NewErrorResult(err string) *ToolResult {
	return &ToolResult{
		Success: false,
		Error:   err,
		Metadata: make(map[string]any),
	}
}

// NewErrorResultWithCode 创建带错误码的结果
func NewErrorResultWithCode(err string, code string) *ToolResult {
	return &ToolResult{
		Success:   false,
		Error:     err,
		ErrorCode: code,
		Metadata:  make(map[string]any),
	}
}

// WithMetadata 添加元数据
func (r *ToolResult) WithMetadata(key string, value any) *ToolResult {
	r.Metadata[key] = value
	return r
}

// WithDuration 设置执行耗时
func (r *ToolResult) WithDuration(d time.Duration) *ToolResult {
	r.Duration = d
	return r
}

// AddWarning 添加警告
func (r *ToolResult) AddWarning(warning string) *ToolResult {
	r.Warnings = append(r.Warnings, warning)
	return r
}

// IsSuccess 检查是否成功
func (r *ToolResult) IsSuccess() bool {
	return r.Success
}

// ToMap 转换为 map
func (r *ToolResult) ToMap() map[string]any {
	result := map[string]any{
		"success": r.Success,
	}
	if r.Success {
		result["data"] = r.Data
	} else {
		result["error"] = r.Error
		if r.ErrorCode != "" {
			result["error_code"] = r.ErrorCode
		}
	}
	if len(r.Metadata) > 0 {
		result["metadata"] = r.Metadata
	}
	if len(r.Warnings) > 0 {
		result["warnings"] = r.Warnings
	}
	return result
}
