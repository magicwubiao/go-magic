// Package envref 解析配置里的环境变量引用，语法 `${NAME}` / `${NAME:-default}`。
//
// 为什么要它：连接器的凭据过去只能把值直接写进 config.json（UI 层用 *** 掩码）。
// 掩码防的是"旁人看到屏幕"，磁盘上仍然是明文——配置文件一旦被同步、备份、
// 提交到仓库，token 就跟着走了。
//
// 引用形式把"值"换成"取值方式"：配置里只有变量名，真值只存在于进程环境。
// 掩码逻辑因此可以退化成"没什么可掩的"，而不是唯一防线。
//
// 三条约定：
//  1. 只有 `${...}` 才展开，裸 `$NAME` 原样保留——配置里出现 `$` 的场景太多
//     （密码、正则、SQL），不能靠猜。
//  2. 引用不到且没有默认值时返回错误，并列出缺失的变量名：静默展开成空串
//     会让连接器带着空 token 去请求，最后收到一个更难懂的 401。
//  3. 展开结果只在内存里流转，任何 Save 路径都不写回（否则第一次加载就把
//     明文固化了）。
package envref

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// maxDepth 限制默认值的嵌套层数（`${A:-${B:-${C}}}`）。超过就报错，
// 避免构造出循环引用把进程卡死。
const maxDepth = 4

// IsReference 报告 s 是否"整值就是一个引用"（可带默认值）。
//
// 用于掩码判断：`GH_TOKEN=${GH_TOKEN}` 回显成 `***` 会让用户以为配置被改写，
// 而这恰恰是我们想让用户看到的东西——它明确写了"值从哪来"。
func IsReference(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "${") {
		return false
	}
	// 必须整串只有一个引用：`Bearer ${TOKEN}` 这种混合值仍然算密钥，照常掩码。
	end := matchBrace(t, 1)
	return end == len(t)-1
}

// NameOf 在 s 是纯引用时返回它引用的变量名（含默认值时返回变量名，不含默认值）。
func NameOf(s string) (string, bool) {
	if !IsReference(s) {
		return "", false
	}
	t := strings.TrimSpace(s)
	inner := t[2 : len(t)-1]
	name, _, _ := splitDefault(inner)
	return name, true
}

// Expand 展开 s 中的全部 `${...}`。s 不含 `${` 时原样返回。
// 引用了未定义且无默认值的变量时返回 *MissingError。
func Expand(s string) (string, error) {
	missing := missingSet{}
	out, err := expand(s, 0, missing)
	if err != nil {
		return "", err
	}
	if err := missing.err(); err != nil {
		return "", err
	}
	return out, nil
}

// ExpandMap 返回展开后的副本（不修改入参）。nil 入参返回 nil。
func ExpandMap(m map[string]string) (map[string]string, error) {
	if len(m) == 0 {
		return m, nil
	}
	out := make(map[string]string, len(m))
	missing := missingSet{}
	for k, v := range m {
		expanded, err := expand(v, 0, missing)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out[k] = expanded
	}
	if err := missing.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ExpandList 展开 "KEY=VALUE" 形式的环境变量列表，返回新切片。
// 不含 `=` 的条目原样保留（下游会自己忽略）。
func ExpandList(list []string) ([]string, error) {
	if len(list) == 0 {
		return list, nil
	}
	out := make([]string, 0, len(list))
	missing := missingSet{}
	for _, kv := range list {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			out = append(out, kv)
			continue
		}
		expanded, err := expand(value, 0, missing)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", strings.TrimSpace(key), err)
		}
		out = append(out, key+"="+expanded)
	}
	if err := missing.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// MissingError 报告一组未定义且没有默认值的变量。
type MissingError struct {
	Names []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("environment variable(s) not set: %s (define them in the environment, or write ${NAME:-fallback} in the config)",
		strings.Join(e.Names, ", "))
}

// missingSet 累积整份配置里所有缺失的变量，一次性报全。
// 逐个报错会让用户改一次跑一次的来回。
type missingSet map[string]struct{}

func (m missingSet) add(name string) {
	if m != nil {
		m[name] = struct{}{}
	}
}

func (m missingSet) err() error {
	if len(m) == 0 {
		return nil
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return &MissingError{Names: names}
}

func expand(s string, depth int, missing missingSet) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	if depth > maxDepth {
		return "", fmt.Errorf("environment reference nested deeper than %d levels", maxDepth)
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			end := matchBrace(s, i+1)
			if end < 0 {
				// 未闭合：原样保留。配置写错时让用户看到原文，
				// 比悄悄吞掉后半行更容易定位。
				b.WriteString(s[i:])
				break
			}
			name, def, hasDefault := splitDefault(s[i+2 : end])
			value, ok := os.LookupEnv(name)
			if !ok || value == "" {
				if hasDefault {
					expanded, err := expand(def, depth+1, missing)
					if err != nil {
						return "", err
					}
					b.WriteString(expanded)
				} else {
					missing.add(name)
				}
			} else {
				b.WriteString(value)
			}
			i = end + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}

// matchBrace 返回与 s[open] 处的 '{' 配对的 '}' 下标，找不到返回 -1。
// 识别嵌套，因此 `${A:-${B}}` 取到的是最后一个 '}'。
func matchBrace(s string, open int) int {
	if open >= len(s) || s[open] != '{' {
		return -1
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitDefault 把 "NAME" 或 "NAME:-default" 拆开。默认值允许为空串
// （`${TOKEN:-}` 表示"没配就用空"，与"没配就报错"是不同的意图）。
func splitDefault(inner string) (name, def string, hasDefault bool) {
	if idx := strings.Index(inner, ":-"); idx >= 0 {
		return strings.TrimSpace(inner[:idx]), inner[idx+2:], true
	}
	return strings.TrimSpace(inner), "", false
}
