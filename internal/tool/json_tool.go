package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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

// jsonPathTokens 把 JSONPath 拆成 token 序列。
// 支持写法：$.key.sub、$.key[0]、$[0].key、$.key[0][1]、$.key[*]。
// 实现思路：把 "[" 前面补一个 "." 再按 "." 分段，"[0]" 段即为索引 token。
func jsonPathTokens(path string) ([]string, error) {
	if !strings.HasPrefix(path, "$") {
		return nil, fmt.Errorf("path must start with '$'")
	}
	normalized := strings.ReplaceAll(path[1:], "[", ".[")
	tokens := strings.Split(normalized, ".")
	var out []string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, "[") {
			if !strings.HasSuffix(tok, "]") {
				return nil, fmt.Errorf("invalid path segment: %s", tok)
			}
			inner := tok[1 : len(tok)-1]
			if inner != "*" {
				var idx int
				if _, err := fmt.Sscanf(inner, "%d", &idx); err != nil || fmt.Sprintf("%d", idx) != inner {
					return nil, fmt.Errorf("invalid array index: %s", inner)
				}
			}
		}
		out = append(out, tok)
	}
	return out, nil
}

func (t *JSONTool) query(data, path string) (any, error) {
	var result any
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	tokens, err := jsonPathTokens(path)
	if err != nil {
		return nil, err
	}

	// current 是当前游标；wildcards 收集 [*] 产生的多结果
	currents := []any{result}
	for _, tok := range tokens {
		if !strings.HasPrefix(tok, "[") {
			// key 段：对每个游标取字段
			next := make([]any, 0, len(currents))
			for _, cur := range currents {
				m, ok := cur.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("cannot access key '%s' on non-object", tok)
				}
				v, exists := m[tok]
				if !exists {
					return nil, fmt.Errorf("path not found: %s", tok)
				}
				next = append(next, v)
			}
			currents = next
			continue
		}

		inner := tok[1 : len(tok)-1]
		next := make([]any, 0, len(currents))
		for _, cur := range currents {
			arr, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("cannot index non-array with '%s'", tok)
			}
			if inner == "*" {
				next = append(next, arr...)
				continue
			}
			var idx int
			fmt.Sscanf(inner, "%d", &idx)
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("index out of range: %d (array length %d)", idx, len(arr))
			}
			next = append(next, arr[idx])
		}
		currents = next
	}

	// 单结果直接返回值；[*] 多结果包装成数组
	if len(currents) == 1 {
		return currents[0], nil
	}
	return currents, nil
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

func (t *YAMLTool) parse(data string) (any, error) {
	// 真正的 YAML 解析（yaml.v3），列表/嵌套 map/多行字符串均正确映射
	var result any
	if err := yaml.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	return result, nil
}

func (t *YAMLTool) format(data string) (string, error) {
	parsed, err := t.parse(data)
	if err != nil {
		return "", err
	}

	out, err := yaml.Marshal(parsed)
	if err != nil {
		return "", err
	}
	return string(out), nil
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

	out, err := yaml.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
