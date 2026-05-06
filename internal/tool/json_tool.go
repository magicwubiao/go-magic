package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================================
// JSON Tool - JSON 处理工具
// ============================================================================

type JSONTool struct {
	BaseTool
}

func NewJSONTool() *JSONTool {
	return &JSONTool{
		BaseTool: *NewBaseTool(
			"json",
			"Process JSON data: parse, format, query (JSONPath), transform, validate",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: parse, format, query, transform, validate, minify",
						"enum":        []any{"parse", "format", "query", "transform", "validate", "minify"},
					},
					"data": map[string]any{
						"type":        "string",
						"description": "JSON string to process",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "JSONPath for query operation (e.g., $.store.book[0].title)",
					},
					"indent": map[string]any{
						"type":        "number",
						"description": "Indentation level for format (default: 2)",
						"default":     2,
					},
				},
				"required": []any{"operation", "data"},
			},
		),
	}
}

func (t *JSONTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)
	data, _ := args["data"].(string)
	
	if data == "" {
		return nil, fmt.Errorf("data is required")
	}
	
	switch operation {
	case "parse":
		return t.parse(data)
	case "format":
		indent := 2
		if i, ok := args["indent"].(float64); ok {
			indent = int(i)
		}
		return t.format(data, indent)
	case "query":
		path, _ := args["path"].(string)
		return t.query(data, path)
	case "transform":
		return t.transform(data)
	case "validate":
		return t.validate(data)
	case "minify":
		return t.minify(data)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *JSONTool) parse(data string) (map[string]any, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return map[string]any{"parsed": result, "type": fmt.Sprintf("%T", result)}, nil
}

func (t *JSONTool) format(data string, indent int) (string, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	
	indentStr := strings.Repeat(" ", indent)
	jsonBytes, err := json.MarshalIndent(result, "", indentStr)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

func (t *JSONTool) query(data, path string) (any, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	
	// 简单的 JSONPath 查询实现
	// 支持格式: $.key.subkey 或 $[0].key
	path = strings.TrimPrefix(path, "$")
	parts := strings.Split(path, ".")
	
	current := result
	for _, part := range parts {
		part = strings.TrimPrefix(part, ".")
		if part == "" {
			continue
		}
		
		switch c := current.(type) {
		case map[string]any:
			current = c[part]
		case []any:
			// 支持数组索引
			if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
				idxStr := strings.TrimSuffix(strings.TrimPrefix(part, "["), "]")
				idx := 0
				fmt.Sscanf(idxStr, "%d", &idx)
				if idx >= 0 && idx < len(c) {
					current = c[idx]
				} else {
					return nil, fmt.Errorf("index out of range: %d", idx)
				}
			} else {
				return nil, fmt.Errorf("cannot access key '%s' on array", part)
			}
		default:
			return nil, fmt.Errorf("path not found: %s", part)
		}
	}
	
	return current, nil
}

func (t *JSONTool) transform(data string) (map[string]any, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return map[string]any{"transformed": result, "operations": []string{"format", "sort_keys"}}, nil
}

func (t *JSONTool) validate(data string) (map[string]any, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return map[string]any{
			"valid":   false,
			"error":   err.Error(),
			"message": "JSON syntax error",
		}, nil
	}
	return map[string]any{"valid": true, "message": "Valid JSON"}, nil
}

func (t *JSONTool) minify(data string) (string, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

// ============================================================================
// YAML Tool - YAML 处理工具
// ============================================================================

type YAMLTool struct {
	BaseTool
}

func NewYAMLTool() *YAMLTool {
	return &YAMLTool{
		BaseTool: *NewBaseTool(
			"yaml",
			"Process YAML data: parse, format, convert to/from JSON",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: parse, format, to_json, from_json",
						"enum":        []any{"parse", "format", "to_json", "from_json"},
					},
					"data": map[string]any{
						"type":        "string",
						"description": "YAML string to process",
					},
				},
				"required": []any{"operation", "data"},
			},
		),
	}
}

func (t *YAMLTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)
	data, _ := args["data"].(string)
	
	if data == "" {
		return nil, fmt.Errorf("data is required")
	}
	
	switch operation {
	case "parse":
		return t.parse(data)
	case "format":
		return t.format(data)
	case "to_json":
		return t.toJSON(data)
	case "from_json":
		return t.fromJSON(data)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *YAMLTool) parse(data string) (map[string]any, error) {
	// 简单 YAML 解析
	result := make(map[string]any)
	lines := strings.Split(data, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"")
			result[key] = value
		}
	}
	
	return result, nil
}

func (t *YAMLTool) format(data string) (string, error) {
	parsed, err := t.parse(data)
	if err != nil {
		return "", err
	}
	
	// 简单格式化输出
	var buf strings.Builder
	for k, v := range parsed {
		buf.WriteString(fmt.Sprintf("%s: %v\n", k, v))
	}
	return buf.String(), nil
}

func (t *YAMLTool) toJSON(data string) (string, error) {
	parsed, err := t.parse(data)
	if err != nil {
		return "", err
	}
	
	jsonBytes, err := json.Marshal(parsed)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

func (t *YAMLTool) fromJSON(data string) (string, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	
	// 简单 JSON 到 YAML 转换
	if m, ok := result.(map[string]any); ok {
		var buf strings.Builder
		for k, v := range m {
			buf.WriteString(fmt.Sprintf("%s: %v\n", k, v))
		}
		return buf.String(), nil
	}
	
	return "", fmt.Errorf("unsupported type for YAML conversion")
}
