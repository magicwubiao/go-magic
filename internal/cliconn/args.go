package cliconn

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 参数模板展开。
//
// 只做"填槽"，不做分词：模板被原样当成一个 argv 元素，占位符替换成字符串后
// 仍然是一个元素。因此模型给出的值里带空格/分号/管道符都不会变成额外的命令
// 或参数——这一点是整个模块的安全底座，改动前请先读 config.go 顶部的注释。
var (
	// 任意位置的 {{name}} 或 {{?name}}
	placeholderRe = regexp.MustCompile(`\{\{(\??)([A-Za-z_][A-Za-z0-9_]*)\}\}`)
	// 整条 arg 就是一个 {{name}}：值为空时整条 arg 省略（如 "--limit {{limit}}"）
	singlePlaceholderRe = regexp.MustCompile(`^\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}$`)
	// 条件前缀 {{?name}}：为真才保留其后的字面量
	conditionPrefixRe = regexp.MustCompile(`^\{\{\?([A-Za-z_][A-Za-z0-9_]*)\}\}`)
)

// PlaceholderSet 是一条模板里出现过的占位符，按用法分类。
type PlaceholderSet struct {
	Values     []string // {{name}}
	Conditions []string // {{?name}}
}

// Placeholders 解析一条模板里的占位符（保留出现顺序，去重）。
func Placeholders(tmpl string) PlaceholderSet {
	var set PlaceholderSet
	seenVal := map[string]bool{}
	seenCond := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(tmpl, -1) {
		name := m[2]
		if m[1] == "?" {
			if !seenCond[name] {
				seenCond[name] = true
				set.Conditions = append(set.Conditions, name)
			}
			continue
		}
		if !seenVal[name] {
			seenVal[name] = true
			set.Values = append(set.Values, name)
		}
	}
	return set
}

// resolved 是"参数名 → 字符串值 / 真值判定"的中间结果。
type resolved struct {
	str    map[string]string
	truthy map[string]bool
}

// ResolveParams 把模型给的参数按声明校验并转成字符串。
//
// 声明之外的多余键被忽略而不是报错：模型经常顺手多塞一个它以为有用的字段，
// 为此让整次调用失败没有收益。
func ResolveParams(spec ToolSpec, in map[string]any) (resolved, error) {
	out := resolved{str: make(map[string]string, len(spec.Params)), truthy: make(map[string]bool, len(spec.Params))}
	for _, p := range spec.Params {
		raw, ok := in[p.Name]
		if !ok || raw == nil {
			if p.Default != nil {
				raw, ok = p.Default, true
			}
		}
		if !ok || raw == nil {
			if p.Required {
				return out, fmt.Errorf("missing required parameter %q", p.Name)
			}
			continue
		}

		s, err := stringifyParam(p, raw)
		if err != nil {
			return out, err
		}
		if len(p.Enum) > 0 && !containsFold(p.Enum, s) {
			return out, fmt.Errorf("parameter %q must be one of %v (got %q)", p.Name, p.Enum, s)
		}
		out.str[p.Name] = s
		out.truthy[p.Name] = isTruthy(raw, s)
	}
	return out, nil
}

// stringifyParam 把一个 JSON 参数转成命令行里的字符串。
func stringifyParam(p ParamSpec, raw any) (string, error) {
	switch p.Type {
	case "integer", "number":
		switch v := raw.(type) {
		case float64:
			if v == float64(int64(v)) {
				return strconv.FormatInt(int64(v), 10), nil
			}
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		case int:
			return strconv.Itoa(v), nil
		case int64:
			return strconv.FormatInt(v, 10), nil
		case json.Number:
			return v.String(), nil
		case string:
			return strings.TrimSpace(v), nil
		default:
			return "", fmt.Errorf("parameter %q must be a number", p.Name)
		}
	case "boolean":
		switch v := raw.(type) {
		case bool:
			return strconv.FormatBool(v), nil
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			if s == "true" || s == "false" {
				return s, nil
			}
			return "", fmt.Errorf("parameter %q must be true or false", p.Name)
		default:
			return "", fmt.Errorf("parameter %q must be true or false", p.Name)
		}
	case "array", "object":
		buf, err := json.Marshal(raw)
		if err != nil {
			return "", fmt.Errorf("parameter %q could not be serialized as JSON: %w", p.Name, err)
		}
		return string(buf), nil
	default:
		switch v := raw.(type) {
		case string:
			return v, nil
		case bool:
			return strconv.FormatBool(v), nil
		case float64:
			if v == float64(int64(v)) {
				return strconv.FormatInt(int64(v), 10), nil
			}
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		default:
			buf, err := json.Marshal(raw)
			if err != nil {
				return "", fmt.Errorf("parameter %q: %w", p.Name, err)
			}
			return string(buf), nil
		}
	}
}

// isTruthy 判定参数是否为"真"（用于 {{?name}} 条件标记）。
func isTruthy(raw any, str string) bool {
	if b, ok := raw.(bool); ok {
		return b
	}
	switch strings.ToLower(strings.TrimSpace(str)) {
	case "", "false", "0", "no", "off", "null", "nil":
		return false
	}
	return true
}

func containsFold(list []string, v string) bool {
	for _, item := range list {
		if strings.EqualFold(item, v) {
			return true
		}
	}
	return false
}

// ExpandArgs 把连接器公共参数 + 工具参数展开成最终 argv（不含可执行文件本身）。
//
// 规则：
//   - "{{name}}"      → 值替换。整条 arg 只有这一个占位符且值为空时，该 arg 被省略
//     （"--limit {{limit}}" 因此能表达"可选参数"）。
//   - "{{?name}}"     开头 → 条件：name 为真时去掉标记保留其后字面量，否则整条丢弃。
//   - 其余情况按字面量处理，绝不分词。
//
// 未声明的占位符不会走到这里（Validate 已拦截）；真漏了也只会替换成空串，
// 不会退化成"多一个参数"。
func ExpandArgs(connargs []string, spec ToolSpec, r resolved) []string {
	tmpls := make([]string, 0, len(connargs)+len(spec.Args))
	tmpls = append(tmpls, connargs...)
	tmpls = append(tmpls, spec.Args...)

	out := make([]string, 0, len(tmpls))
	for _, tmpl := range tmpls {
		if m := conditionPrefixRe.FindStringSubmatch(tmpl); m != nil {
			if !r.truthy[m[1]] {
				continue
			}
			rest := tmpl[len(m[0]):]
			if rest == "" {
				continue
			}
			out = append(out, interpolate(rest, r))
			continue
		}

		if m := singlePlaceholderRe.FindStringSubmatch(tmpl); m != nil {
			val, ok := r.str[m[1]]
			if !ok || val == "" {
				// 可选参数没给值：整条 arg 省略，而不是塞一个空字符串
				// （空字符串对多数 CLI 是"给了一个空参数"，语义完全不同）。
				//
				// 同时回退紧邻的选项名：`--limit {{limit}}` 是最常见的写法，
				// 只丢掉占位符会留下一个悬空的 --limit，多数 CLI 会当场报
				// "flag needs an argument"。仅当前一个 arg 是纯选项名
				// （以 - 开头且不含模板）时才回退，避免误删真正的开关。
				if n := len(out); n > 0 && isBareOption(out[n-1]) {
					out = out[:n-1]
				}
				continue
			}
			out = append(out, val)
			continue
		}

		out = append(out, interpolate(tmpl, r))
	}
	return out
}

// interpolate 替换模板里所有 {{name}}（不做省略判断）。
func interpolate(tmpl string, r resolved) string {
	return placeholderRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		sub := placeholderRe.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		return r.str[sub[2]]
	})
}

// isBareOption 判断一个 argv 元素是不是"光秃秃的选项名"（--limit、-n），
// 用于省略可选值参数时把它的选项名一起收回。
func isBareOption(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	if strings.Contains(arg, "=") || strings.Contains(arg, "{{") {
		return false
	}
	return true
}
