package agentplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 已知顶层字段白名单(schema 闭包):不在表中的顶层字段视为 unknown,报告并忽略。
var knownManifestFields = map[string]bool{
	"$schema": true, "name": true, "version": true, "description": true,
	"author": true, "homepage": true, "repository": true, "license": true,
	"keywords": true, "extensions": true,
}

// loadManifest 从 pluginRoot/plugin.json 加载并校验清单。
//
// 返回 (manifest, warnings, fatalErr):
//   - fatalErr 非 nil 时插件被拒绝(manifest 缺失、$schema 缺失/不支持、name 非法等致命违反)。
//   - warnings 收集 unknown 顶层字段、非 object extensions 等非致命报告。
//   - 规范:不可在加载时联网拉取 schema,$schema 仅用于选择本地校验规则。
func loadManifest(pluginRoot string) (*Manifest, []string, error) {
	manifestPath := filepath.Join(pluginRoot, manifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", manifestName, err)
	}

	// 第一阶段:解析为原始 map,检测 unknown 顶层字段并捕获 extensions 原始类型。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", manifestName, err)
	}

	var warnings []string

	// $schema 必须存在;缺失/不支持为致命。
	schemaRaw, ok := raw["$schema"]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing required $schema", manifestName)
	}
	var schemaVal string
	if err := json.Unmarshal(schemaRaw, &schemaVal); err != nil || schemaVal == "" {
		return nil, nil, fmt.Errorf("%s: $schema must be a non-empty string", manifestName)
	}
	if !isSupportedSchema(schemaVal) {
		return nil, nil, fmt.Errorf("%s: unsupported $schema %q (supported: %s)", manifestName, schemaVal, SpecVersion)
	}

	// 报告 unknown 顶层字段(非致命)。
	for k := range raw {
		if !knownManifestFields[k] {
			warnings = append(warnings, fmt.Sprintf("unknown top-level field %q ignored", k))
		}
	}

	// 第二阶段:解析为结构体。extensions 用 json.RawMessage 容错,
	// 避免非 object 的 extensions 导致整个解析失败(规范要求非致命)。
	type manifestRaw struct {
		Schema      string          `json:"$schema"`
		Name        string          `json:"name"`
		Version     string          `json:"version,omitempty"`
		Description string          `json:"description,omitempty"`
		Author      *Author         `json:"author,omitempty"`
		Homepage    string          `json:"homepage,omitempty"`
		Repository  string          `json:"repository,omitempty"`
		License     string          `json:"license,omitempty"`
		Keywords    []string        `json:"keywords,omitempty"`
		Extensions  json.RawMessage `json:"extensions,omitempty"`
	}
	var mr manifestRaw
	if err := json.Unmarshal(data, &mr); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", manifestName, err)
	}

	m := &Manifest{
		Schema:      schemaVal,
		Name:        mr.Name,
		Version:     mr.Version,
		Description: mr.Description,
		Author:      mr.Author,
		Homepage:    mr.Homepage,
		Repository:  mr.Repository,
		License:     mr.License,
		Keywords:    mr.Keywords,
	}

	// name 必须存在且合法(致命)。
	if err := validatePluginName(m.Name); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", manifestName, err)
	}

	// extensions 必须是 object,否则报告并忽略(非致命)。
	// 注意:extensions 内每个命名空间的值不做校验,由客户端按实现情况消费/忽略。
	if len(mr.Extensions) > 0 {
		var extVal map[string]any
		if err := json.Unmarshal(mr.Extensions, &extVal); err != nil {
			warnings = append(warnings, "extensions is not an object, ignored")
		} else {
			m.Extensions = extVal
		}
	}

	return m, warnings, nil
}

// isSupportedSchema 判断 $schema 是否指向本实现支持的规范版本。
// 规范要求版本匹配:plugin.json 与 mcp.json 的 Agent Plugins 版本须一致。
func isSupportedSchema(schema string) bool {
	// 接受规范的 schema URL,或裸版本号。
	return schema == PluginSchemaURL || schema == SpecVersion ||
		strings.HasSuffix(schema, "/"+SpecVersion+"/plugin.schema.json")
}

// schemaVersion 从 $schema URL 提取规范版本号(如 "1.0.0")。
func schemaVersion(schema string) string {
	for _, prefix := range []string{PluginSchemaURL, MCPSchemaURL} {
		if schema == prefix {
			return SpecVersion
		}
	}
	// 兼容形如 .../schemas/<ver>/plugin.schema.json
	if idx := strings.Index(schema, "/schemas/"); idx >= 0 {
		rest := schema[idx+len("/schemas/"):]
		if slash := strings.Index(rest, "/"); slash > 0 {
			return rest[:slash]
		}
	}
	if schema == SpecVersion {
		return SpecVersion
	}
	return ""
}

// validatePluginName 校验 name 规则:
//   - 1-64 字符
//   - 仅小写 ASCII 字母、数字、连字符、点
//   - 首尾为字母数字
//   - 不含 "--" 或 ".."
func validatePluginName(name string) error {
	if l := len(name); l < 1 || l > 64 {
		return fmt.Errorf("name must be 1-64 characters, got %d", l)
	}
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.'
		if !ok {
			return fmt.Errorf("name %q contains illegal character %q (allowed: lowercase ascii letters, digits, hyphen, period)", name, r)
		}
		if i == 0 || i == len(name)-1 {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				return fmt.Errorf("name %q must begin and end with an alphanumeric character", name)
			}
		}
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("name %q must not contain %q", name, "--")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name %q must not contain %q", name, "..")
	}
	return nil
}
