package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

// ============================================================================
// Documentation Generator
// ============================================================================

// DocumentationGenerator 文档生成器
type DocumentationGenerator struct {
	templates map[string]*template.Template
}

// NewDocumentationGenerator 创建文档生成器
func NewDocumentationGenerator() *DocumentationGenerator {
	return &DocumentationGenerator{
		templates: make(map[string]*template.Template),
	}
}

// GenerateMarkdown 生成 Markdown 格式文档
func (dg *DocumentationGenerator) GenerateMarkdown(tools []Tool) string {
	var buf bytes.Buffer
	
	buf.WriteString("# Tool Documentation\n\n")
	buf.WriteString(fmt.Sprintf("Generated at: %s\n\n", "auto-generated"))
	buf.WriteString("## Table of Contents\n\n")
	
	// 生成目录
	for i, tool := range tools {
		buf.WriteString(fmt.Sprintf("%d. [%s](#%s)\n", i+1, tool.Name(), strings.ToLower(tool.Name())))
	}
	buf.WriteString("\n---\n\n")
	
	// 生成每个工具的详细文档
	for _, tool := range tools {
		buf.WriteString(dg.generateToolMarkdown(tool))
		buf.WriteString("\n---\n\n")
	}
	
	return buf.String()
}

func (dg *DocumentationGenerator) generateToolMarkdown(tool Tool) string {
	var buf bytes.Buffer
	
	buf.WriteString(fmt.Sprintf("## %s\n\n", tool.Name()))
	buf.WriteString(fmt.Sprintf("**Description:** %s\n\n", tool.Description()))
	
	schema := tool.Schema()
	if schema != nil {
		buf.WriteString("### Parameters\n\n")
		
		if props, ok := schema["properties"].(map[string]interface{}); ok {
			buf.WriteString("| Name | Type | Required | Description |\n")
			buf.WriteString("|------|------|----------|-------------|\n")
			
			required := []string{}
			if req, ok := schema["required"].([]interface{}); ok {
				for _, r := range req {
					if rStr, ok := r.(string); ok {
						required = append(required, rStr)
					}
				}
			}
			
			for name, prop := range props {
				propMap, ok := prop.(map[string]interface{})
				if !ok {
					continue
				}
				
				propType := ""
				if t, ok := propMap["type"].(string); ok {
					propType = t
				}
				
				desc := ""
				if d, ok := propMap["description"].(string); ok {
					desc = d
				}
				
				isRequired := "No"
				for _, r := range required {
					if r == name {
						isRequired = "Yes"
						break
					}
				}
				
				buf.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", name, propType, isRequired, desc))
			}
			buf.WriteString("\n")
		}
	}
	
	// 添加示例
	buf.WriteString("### Example\n\n")
	buf.WriteString(fmt.Sprintf("```json\n%s\n```\n", dg.generateExampleJSON(tool)))
	
	return buf.String()
}

func (dg *DocumentationGenerator) generateExampleJSON(tool Tool) string {
	schema := tool.Schema()
	if schema == nil {
		return "{}"
	}
	
	example := make(map[string]interface{})
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for name, prop := range props {
			propMap, ok := prop.(map[string]interface{})
			if !ok {
				continue
			}
			
			// 使用默认值
			if def, ok := propMap["default"]; ok {
				example[name] = def
				continue
			}
			
			// 使用枚举的第一个值
			if enum, ok := propMap["enum"].([]interface{}); ok && len(enum) > 0 {
				example[name] = enum[0]
				continue
			}
			
			// 根据类型提供示例值
			switch propMap["type"] {
			case "string":
				example[name] = "example_value"
			case "number", "integer":
				example[name] = 1
			case "boolean":
				example[name] = true
			case "array":
				example[name] = []interface{}{}
			case "object":
				example[name] = map[string]interface{}{}
			}
		}
	}
	
	jsonBytes, _ := json.MarshalIndent(example, "", "  ")
	return string(jsonBytes)
}

// GenerateOpenAPISpec 生成 OpenAPI 格式的工具规范
func (dg *DocumentationGenerator) GenerateOpenAPISpec(tools []Tool) []byte {
	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "Tool API",
			"version":     "1.0.0",
			"description": "Auto-generated tool API specification",
		},
		"paths": make(map[string]interface{}),
	}
	
	paths := spec["paths"].(map[string]interface{})
	for _, tool := range tools {
		path := fmt.Sprintf("/tools/%s", tool.Name())
		paths[path] = map[string]interface{}{
			"post": map[string]interface{}{
				"summary":     tool.Description(),
				"operationId": tool.Name(),
				"requestBody": map[string]interface{}{
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": tool.Schema(),
						},
					},
				},
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Successful response",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"success": map[string]interface{}{"type": "boolean"},
										"data":    map[string]interface{}{"type": "object"},
										"error":   map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
				},
			},
		}
	}
	
	jsonBytes, _ := json.MarshalIndent(spec, "", "  ")
	return jsonBytes
}

// GenerateToolHelp 生成单个工具的帮助信息
func (dg *DocumentationGenerator) GenerateToolHelp(toolName string, tools []Tool) string {
	for _, tool := range tools {
		if tool.Name() == toolName {
			return dg.generateToolMarkdown(tool)
		}
	}
	return fmt.Sprintf("Tool '%s' not found\n", toolName)
}

// GenerateIndex 生成工具索引
func (dg *DocumentationGenerator) GenerateIndex(tools []Tool) map[string]interface{} {
	index := map[string]interface{}{
		"total": len(tools),
		"tools": make([]map[string]interface{}, 0, len(tools)),
	}
	
	toolList := index["tools"].([]map[string]interface{})
	for _, tool := range tools {
		toolList = append(toolList, map[string]interface{}{
			"name":        tool.Name(),
			"description": tool.Description(),
		})
	}
	
	return index
}

// ============================================================================
// Tool Help Command Generator
// ============================================================================

// HelpGenerator 帮助信息生成器
type HelpGenerator struct{}

// NewHelpGenerator 创建帮助生成器
func NewHelpGenerator() *HelpGenerator {
	return &HelpGenerator{}
}

// GenerateHelp 生成帮助文本
func (hg *HelpGenerator) GenerateHelp(tool Tool) string {
	var buf bytes.Buffer
	
	buf.WriteString(fmt.Sprintf("Tool: %s\n", tool.Name()))
	buf.WriteString(fmt.Sprintf("Description: %s\n\n", tool.Description()))
	
	schema := tool.Schema()
	if schema != nil {
		buf.WriteString("Usage:\n")
		buf.WriteString(fmt.Sprintf("  /tool %s", tool.Name()))
		
		if props, ok := schema["properties"].(map[string]interface{}); ok {
			for name, prop := range props {
				propMap, ok := prop.(map[string]interface{})
				if !ok {
					continue
				}
				
				// 获取参数类型
				propType := "any"
				if t, ok := propMap["type"].(string); ok {
					propType = t
				}
				
				required := false
				if req, ok := schema["required"].([]interface{}); ok {
					for _, r := range req {
						if r == name {
							required = true
							break
						}
					}
				}
				
				prefix := ""
				if required {
					prefix = "<"
				} else {
					prefix = "["
				}
				suffix := ""
				if required {
					suffix = ">"
				} else {
					suffix = "]"
				}
				
				buf.WriteString(fmt.Sprintf(" %s%s:%s=%s%s", prefix, name, propType, name, suffix))
			}
		}
		buf.WriteString("\n\n")
	}
	
	return buf.String()
}

// GenerateAllHelp 生成所有工具的帮助
func (hg *HelpGenerator) GenerateAllHelp(registry *Registry) string {
	var buf bytes.Buffer
	
	buf.WriteString("Available Tools:\n")
	buf.WriteString("================\n\n")
	
	for _, name := range registry.List() {
		tool, _ := registry.Get(name)
		if tool != nil {
			buf.WriteString(fmt.Sprintf("- **%s**: %s\n", tool.Name(), tool.Description()))
		}
	}
	
	buf.WriteString("\nUse '/help <tool_name>' for detailed information about a specific tool.\n")
	
	return buf.String()
}
