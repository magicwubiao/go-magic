package cliconn

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseCommand 把"一条完整命令"解析成连接器可用的工具草稿。
//
// 这是降低使用门槛的入口：用户不学参数模板语法，直接粘贴
//
//	gh pr list --repo owner/repo --limit 20 --json
//
// 得到
//
//	Command: gh
//	Tool.Name:    pr_list
//	Tool.Args:    [pr list --repo {{repo}} --limit {{limit}} {{?json}}--json]
//	Tool.Params:  repo(required) / limit(integer) / json(boolean)
//
// 规则（启发式，产物是"草稿"，用户确认后仍可手改）：
//   - 第一个 token 是可执行文件，其余按空格分词（引号内的空格不算分隔）；
//   - `--flag value` / `--flag=value` → 字面量 flag + {{flag}} 占位符，参数类型
//     按示例值推断（20→integer，true→boolean，其余 string）；
//   - 后面没有跟值的 flag（如 --json）→ 条件模板 {{?json}}--json + boolean 参数，
//     模型给 true 时该参数才出现在 argv 里（复用 ExpandArgs 的既有语义）；
//   - <NAME> / {name} / {{name}} / 全大写 token → 位置参数占位符；
//   - 第一个 flag/占位符之前的字面量 token 是子命令，拼成工具名（pr list → pr_list）。
//
// 解析只发生在"生成草稿"这一步，执行路径仍然是 argv 数组直传，
// 这里做的分词不会影响运行时安全模型。
type CommandParse struct {
	// Command 是第一个 token（可执行文件名或路径）。
	Command string `json:"command"`
	// Tool 是生成的工具草稿（name/description/args/params）。
	Tool ToolSpec `json:"tool"`
	// Warnings 是给用户的检查提示（比如"没有识别出任何参数"）。
	Warnings []string `json:"warnings,omitempty"`
}

// ParseCommand 解析一条命令行；输入为空或首 token 无法构成合法工具名时报错。
func ParseCommand(input string) (*CommandParse, error) {
	tokens := tokenizeCommand(input)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty command line")
	}

	p := &CommandParse{
		Command: tokens[0],
		Tool:    ToolSpec{Description: strings.TrimSpace(input)},
	}

	seen := make(map[string]int) // 参数名 → 出现次数，重名时加后缀
	toolWords := make([]string, 0, 4)
	args := make([]string, 0, len(tokens))
	params := make([]ParamSpec, 0, len(tokens)/2)
	pastSeparator := false // 见到裸 "--" 后，后面的 token 都是位置参数

	addParam := func(base string, typ string, required bool) string {
		name := sanitizeParamName(base)
		if name == "" {
			return ""
		}
		seen[name]++
		if seen[name] > 1 {
			name = fmt.Sprintf("%s_%d", name, seen[name])
		}
		// string 是参数类型的默认值，落库时省略。
		if typ == "string" {
			typ = ""
		}
		params = append(params, ParamSpec{Name: name, Type: typ, Required: required})
		return name
	}

	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]

		// 裸 "--"：getopt 约定的"选项结束"，其后一律按位置参数处理。
		if tok == "--" && !pastSeparator {
			pastSeparator = true
			args = append(args, tok)
			continue
		}

		// 选项：--flag / -f / --flag=value。横线数量要保留：
		// `-i` 和 `--i` 对多数 CLI 是两个不同的选项。
		if !pastSeparator && isFlagToken(tok) {
			dashes := len(tok) - len(strings.TrimLeft(tok, "-"))
			name, inlineValue, hasInline := splitFlag(tok)
			name = sanitizeParamName(name)
			prefix := strings.Repeat("-", dashes)
			if name == "" {
				// 形状太怪（比如只是 "-"），原样保留成字面量。
				args = append(args, tok)
				continue
			}

			if hasInline {
				typ := inferValueType(inlineValue)
				if addParam(name, typ, true) == "" {
					args = append(args, tok)
					continue
				}
				args = append(args, prefix+name+"={{"+paramLastName(params)+"}}")
				continue
			}

			// 下一个 token 不是别的选项（负数是值，不是选项）→ 当作它的值。
			if i+1 < len(tokens) && (!isFlagToken(tokens[i+1]) || looksLikeNegativeNumber(tokens[i+1])) {
				i++
				value := tokens[i]
				// 值本身是显式占位符（-n {namespace}）→ 用占位符名当参数名。
				if base, ok := placeholderName(value); ok {
					if pname := addParam(base, "string", true); pname != "" {
						args = append(args, prefix+name, "{{"+pname+"}}")
						continue
					}
				}
				typ := inferValueType(value)
				if addParam(name, typ, true) == "" {
					args = append(args, tok)
					continue
				}
				args = append(args, prefix+name, "{{"+paramLastName(params)+"}}")
				continue
			}

			// 没跟值 → 布尔开关，用条件模板（模型给 true 才带上）。
			if addParam(name, "boolean", false) == "" {
				args = append(args, tok)
				continue
			}
			args = append(args, "{{?"+paramLastName(params)+"}}"+prefix+name)
			continue
		}

		// 位置参数：显式占位符写法 → 参数；普通词 → 字面量（子命令）。
		if base, ok := placeholderName(tok); ok {
			if name := addParam(base, "string", true); name != "" {
				args = append(args, "{{"+name+"}}")
				continue
			}
		}
		args = append(args, tok)
		if !pastSeparator {
			toolWords = append(toolWords, tok)
		}
	}

	p.Tool.Args = args
	p.Tool.Params = params
	p.Tool.Name = toolNameFromWords(toolWords)

	if len(params) == 0 {
		p.Warnings = append(p.Warnings,
			"no parameters detected; edit the tool below to declare what the model may fill in")
	}
	if err := p.Tool.Validate(); err != nil {
		return nil, fmt.Errorf("parsed tool is not valid: %w", err)
	}
	return p, nil
}

// tokenizeCommand 把命令行拆成 token。只做两件事：空白分隔、引号聚组
// （单/双引号内的空格不分词）。不解释任何 shell 语义——反引号、$、;
// 都按普通字符保留，因为这一步的结果只用于"生成草稿"，不用于执行。
func tokenizeCommand(input string) []string {
	var tokens []string
	var cur strings.Builder
	inToken := false
	quote := byte(0)

	flush := func() {
		if inToken {
			tokens = append(tokens, cur.String())
			cur.Reset()
			inToken = false
		}
	}

	for i := 0; i < len(input); i++ {
		c := input[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inToken = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			cur.WriteByte(c)
			inToken = true
		}
	}
	flush()
	return tokens
}

func isFlagToken(tok string) bool {
	return len(tok) > 1 && tok[0] == '-'
}

// splitFlag 拆出 --flag=value 的两半；hasInline 表示写成了 = 形式。
func splitFlag(tok string) (name, value string, hasInline bool) {
	body := strings.TrimLeft(tok, "-")
	if idx := strings.IndexByte(body, '='); idx >= 0 {
		return body[:idx], body[idx+1:], true
	}
	return body, "", false
}

func looksLikeNegativeNumber(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' {
		return false
	}
	_, err := strconv.ParseFloat(tok, 64)
	return err == nil
}

// inferValueType 按示例值猜参数类型：20→integer，1.5→number，
// true/false→boolean，其余 string（string 是默认值，落库时省略）。
func inferValueType(v string) string {
	if v == "" {
		return "string"
	}
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return "integer"
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return "number"
	}
	switch strings.ToLower(v) {
	case "true", "false":
		return "boolean"
	}
	return "string"
}

// placeholderName 识别显式占位符写法：<FILE>、{name}、{{name}}、全大写 WORD。
// 返回的 base 还没做合法化，调用方要过 sanitizeParamName。
func placeholderName(tok string) (string, bool) {
	if n := len(tok); n > 4 && strings.HasPrefix(tok, "{{") && strings.HasSuffix(tok, "}}") {
		return tok[2 : n-2], true
	}
	if n := len(tok); n > 2 && strings.HasPrefix(tok, "<") && strings.HasSuffix(tok, ">") {
		return tok[1 : n-1], true
	}
	if n := len(tok); n > 2 && strings.HasPrefix(tok, "{") && strings.HasSuffix(tok, "}") {
		return tok[1 : n-1], true
	}
	// 全大写约定：--repo OWNER/REPO 里手写的 OWNER、OUT_FILE 等。
	if len(tok) >= 2 {
		upper := true
		for _, r := range tok {
			if (r < 'A' || r > 'Z') && r != '_' && (r < '0' || r > '9') {
				upper = false
				break
			}
		}
		if upper && strings.ContainsFunc(tok, func(r rune) bool { return r >= 'A' && r <= 'Z' }) {
			return strings.ToLower(tok), true
		}
	}
	return "", false
}

// sanitizeParamName 把任意来源的名字整理成符合 paramNameRe 的形式。
func sanitizeParamName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteByte('_') // 不能以数字开头
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 48 {
		name = name[:48]
	}
	return name
}

// toolNameFromWords 用子命令词拼工具名：pr list → pr_list。
// 无任何子命令词时回退成 "run"。
func toolNameFromWords(words []string) string {
	if len(words) == 0 {
		return "run"
	}
	name := sanitizeParamName(strings.ToLower(strings.Join(words, "_")))
	if name == "" || name == "_" {
		return "run"
	}
	// 只取前三个词，避免超长命令把工具名撑爆。
	parts := strings.Split(name, "_")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	name = strings.Join(parts, "_")
	if len(name) > 48 {
		name = name[:48]
	}
	return name
}

// paramLastName 取刚追加的参数名（addParam 可能因重名加了后缀）。
func paramLastName(params []ParamSpec) string {
	if len(params) == 0 {
		return ""
	}
	return params[len(params)-1].Name
}
