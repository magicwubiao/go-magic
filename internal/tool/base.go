package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Tool 定义所有工具的统一接口（参考 PicoClaw 设计）
type Tool interface {
	// Name 返回工具名称
	Name() string
	// Description 返回工具描述
	Description() string
	// Schema 返回 OpenAI function calling 格式的 JSON Schema
	Schema() map[string]interface{}
	// Execute 执行工具，返回结果或错误
	Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// BaseTool 提供工具的默认实现基础类
type BaseTool struct {
	name        string
	description string
	schema      map[string]interface{}
}

// NewBaseTool 创建一个基础工具
func NewBaseTool(name, description string, schema map[string]interface{}) *BaseTool {
	return &BaseTool{
		name:        name,
		description: description,
		schema:      schema,
	}
}

func (t *BaseTool) Name() string                   { return t.name }
func (t *BaseTool) Description() string            { return t.description }
func (t *BaseTool) Schema() map[string]interface{} { return t.schema }

// DefaultTimeout 默认工具执行超时时间
const DefaultTimeout = 60 * time.Second

// MaxTimeout 最大超时时间
const MaxTimeout = 300 * time.Second

// ToolExecutionResult 工具执行结果
type ToolExecutionResult struct {
	ToolName  string
	Params    map[string]interface{}
	Result    interface{}
	Error     error
	Duration  time.Duration
	Recovered bool // 是否从 panic 中恢复
}

// ToolExecutor 工具执行器接口
type ToolExecutor interface {
	ExecuteWithProtection(ctx context.Context, tool Tool, params map[string]interface{}, timeout time.Duration) ToolExecutionResult
}

// DefaultToolExecutor 默认工具执行器
type DefaultToolExecutor struct{}

// NewDefaultToolExecutor 创建默认执行器
func NewDefaultToolExecutor() *DefaultToolExecutor {
	return &DefaultToolExecutor{}
}

// ExecuteWithProtection 执行工具，带有 panic 保护和超时控制
func (e *DefaultToolExecutor) ExecuteWithProtection(ctx context.Context, tool Tool, params map[string]interface{}, timeout time.Duration) ToolExecutionResult {
	start := time.Now()
	result := ToolExecutionResult{
		ToolName: tool.Name(),
		Params:   params,
	}

	// 设置超时
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxTimeout {
		timeout = MaxTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Panic 保护
	defer func() {
		if r := recover(); r != nil {
			result.Recovered = true
			result.Error = fmt.Errorf("panic recovered: %v", r)
		}
	}()

	// 参数验证（如果工具实现了参数验证器）
	if validator, ok := tool.(ParamValidator); ok {
		if err := validator.ValidateParams(params); err != nil {
			result.Error = fmt.Errorf("parameter validation failed: %w", err)
			result.Duration = time.Since(start)
			return result
		}
	}

	// 执行工具
	res, err := tool.Execute(ctx, params)
	result.Result = res
	result.Error = err
	result.Duration = time.Since(start)

	return result
}

// ParamValidator 参数验证器接口
type ParamValidator interface {
	ValidateParams(params map[string]interface{}) error
}

// ValidateParams 验证参数是否符合 Schema
func ValidateParams(schema map[string]interface{}, params map[string]interface{}) error {
	if schema == nil {
		return nil
	}

	required, _ := schema["required"].([]interface{})
	properties, _ := schema["properties"].(map[string]interface{})

	// 检查必填参数
	for _, req := range required {
		reqName, ok := req.(string)
		if !ok {
			continue
		}
		if _, exists := params[reqName]; !exists {
			return fmt.Errorf("missing required parameter: %s", reqName)
		}
	}

	// 检查参数类型
	for name, value := range params {
		if prop, ok := properties[name]; ok {
			propMap, ok := prop.(map[string]interface{})
			if !ok {
				continue
			}
			expectedType, _ := propMap["type"].(string)
			if err := validateType(name, value, expectedType); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateType(name string, value interface{}, expectedType string) error {
	if expectedType == "" {
		return nil
	}

	var actualType string
	switch value.(type) {
	case string:
		actualType = "string"
	case float64:
		actualType = "number"
	case int:
		actualType = "integer"
	case bool:
		actualType = "boolean"
	case []interface{}:
		actualType = "array"
	case map[string]interface{}:
		actualType = "object"
	case nil:
		actualType = "null"
	default:
		actualType = "unknown"
	}

	// 类型兼容检查
	if expectedType == "number" && actualType == "integer" {
		return nil
	}
	if expectedType != actualType {
		return fmt.Errorf("parameter '%s' expected type '%s', got '%s'", name, expectedType, actualType)
	}

	return nil
}

// DynamicTool 动态工具，支持 TTL 过期
type DynamicTool struct {
	Tool
	TTL     time.Duration
	Expires time.Time
}

// NewDynamicTool 创建一个动态工具
func NewDynamicTool(tool Tool, ttl time.Duration) *DynamicTool {
	return &DynamicTool{
		Tool:    tool,
		TTL:     ttl,
		Expires: time.Now().Add(ttl),
	}
}

// IsExpired 检查工具是否已过期
func (d *DynamicTool) IsExpired() bool {
	return time.Now().After(d.Expires)
}

// ToolSchema 工具 Schema 转换工具
type ToolSchema struct{}

// ToOpenAISchema 将工具转换为 OpenAI function calling 格式
func (ts *ToolSchema) ToOpenAISchema(tool Tool) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        tool.Name(),
			"description": tool.Description(),
			"parameters":  tool.Schema(),
		},
	}
}

// ToOpenAISchemas 批量转换工具为 OpenAI 格式
func (ts *ToolSchema) ToOpenAISchemas(tools []Tool) []map[string]interface{} {
	schemas := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		schemas = append(schemas, ts.ToOpenAISchema(tool))
	}
	return schemas
}

// ToolInfo 工具信息
type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
}

// ToJSON 将工具信息转为 JSON 字符串
func (ti *ToolInfo) ToJSON() string {
	data, err := json.MarshalIndent(ti, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"name": %q, "error": "failed to marshal"}`, ti.Name)
	}
	return string(data)
}

// FromTool 从 Tool 创建 ToolInfo
func (ti *ToolInfo) FromTool(tool Tool) *ToolInfo {
	return &ToolInfo{
		Name:        tool.Name(),
		Description: tool.Description(),
		Schema:      tool.Schema(),
	}
}
