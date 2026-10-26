package tool

import (
	"fmt"
	"strings"
	"unicode"
)

// keyCombo 是解析并归一化后的按键组合，例如 "ctrl+shift+t"。
//
// 归一化后的修饰键只有 5 种：ctrl / alt / shift / cmd / win。
// cmd 与 win 在语义上是同一个键（macOS Command / Windows Win），由各平台
// driver 决定是否映射到本平台的等价键（Linux 上两者都映射到 Super）。
type keyCombo struct {
	Modifiers []string
	Key       string
}

// String 还原成可读形式，用于日志与错误信息。
func (k keyCombo) String() string {
	if len(k.Modifiers) == 0 {
		return k.Key
	}
	return strings.Join(k.Modifiers, "+") + "+" + k.Key
}

// modifierAliases 把常见写法归一化成 5 个标准名。
var modifierAliases = map[string]string{
	"ctrl":    "ctrl",
	"control": "ctrl",
	"ctl":     "ctrl",
	"alt":     "alt",
	"opt":     "alt",
	"option":  "alt",
	"shift":   "shift",
	"cmd":     "cmd",
	"command": "cmd",
	"super":   "win",
	"meta":    "win",
	"win":     "win",
	"windows": "win",
}

// keyAliases 把各种常见叫法归一化成 standardKeyNames 里的名字。
var keyAliases = map[string]string{
	"return":     "enter",
	"escape":     "esc",
	"del":        "delete",
	"ins":        "insert",
	"pgup":       "pageup",
	"pgdn":       "pagedown",
	"pagedn":     "pagedown",
	"page_up":    "pageup",
	"page_down":  "pagedown",
	"spacebar":   "space",
	"arrowup":    "up",
	"arrowdown":  "down",
	"arrowleft":  "left",
	"arrowright": "right",
	"prtsc":      "printscreen",
	"print":      "printscreen",
	"caps":       "capslock",
	"bksp":       "backspace",
	"back":       "backspace",
}

// standardKeyNames 是允许使用名字（而非单个字符）表示的特殊键。
var standardKeyNames = map[string]bool{
	"enter": true, "tab": true, "esc": true, "backspace": true, "delete": true,
	"space": true, "insert": true, "home": true, "end": true,
	"pageup": true, "pagedown": true,
	"up": true, "down": true, "left": true, "right": true,
	"printscreen": true, "capslock": true, "numlock": true, "scrolllock": true,
	"pause": true, "menu": true,
	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true, "f6": true,
	"f7": true, "f8": true, "f9": true, "f10": true, "f11": true, "f12": true,
	"f13": true, "f14": true, "f15": true, "f16": true, "f17": true, "f18": true,
	"f19": true, "f20": true, "f21": true, "f22": true, "f23": true, "f24": true,
}

// parseKeyCombo 解析 "ctrl+c" / "enter" / "f5" 这类写法。
//
// 校验是刻意严格的：主键必须是单个可打印 ASCII 字符或 standardKeyNames 中的
// 名字。未经校验的字符串会被直接传给 xdotool/cliclick，既是注入面，也会让
// 模型拿到"命令执行失败"这种无从修复的报错。
func parseKeyCombo(s string) (*keyCombo, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return nil, fmt.Errorf("key is required (e.g. \"enter\", \"ctrl+c\", \"f5\")")
	}

	tokens := strings.Split(raw, "+")
	// "ctrl++" 表示"ctrl 加字面加号"，而 "ctrl+" 是漏写了主键——两者
	// 以结尾是否有两个加号区分，不能都当成字面加号（否则模型写错的组合键
	// 会静默变成一个奇怪的按键）。
	if strings.HasSuffix(raw, "++") {
		tokens = append(tokens[:len(tokens)-2], "+")
	}

	combo := &keyCombo{}
	seen := make(map[string]bool, 4)
	for i, tok := range tokens {
		name := strings.ToLower(strings.TrimSpace(tok))
		if name == "" {
			return nil, fmt.Errorf("invalid key combination %q: empty segment", s)
		}
		last := i == len(tokens)-1
		if !last {
			mod, ok := modifierAliases[name]
			if !ok {
				return nil, fmt.Errorf("unknown modifier %q in %q (allowed: ctrl, alt, shift, cmd/win)", tok, s)
			}
			if seen[mod] {
				continue // "ctrl+ctrl+c" 这类重复修饰键直接忽略，不报错
			}
			seen[mod] = true
			combo.Modifiers = append(combo.Modifiers, mod)
			continue
		}
		key, err := normalizeKeyToken(name, s)
		if err != nil {
			return nil, err
		}
		combo.Key = key
	}

	if combo.Key == "" {
		return nil, fmt.Errorf("invalid key combination %q: missing key", s)
	}
	return combo, nil
}

// normalizeKeyToken 归一化单个主键名，并拒绝不认识的键。
func normalizeKeyToken(name, original string) (string, error) {
	if alias, ok := keyAliases[name]; ok {
		name = alias
	}
	if standardKeyNames[name] {
		return name, nil
	}

	runes := []rune(name)
	if len(runes) == 1 {
		r := runes[0]
		// 只接受可打印 ASCII（英文布局下的单个按键）。中文等非 ASCII 字符
		// 应该走 type 动作输入文本，而不是当按键。
		if r >= 0x20 && r < 0x7f && !unicode.IsSpace(r) {
			return name, nil
		}
	}
	return "", fmt.Errorf("unsupported key %q in %q: use a single character, "+
		"one of enter/tab/esc/backspace/delete/space/home/end/pageup/pagedown/"+
		"up/down/left/right/f1-f24, or type literal text with the \"type\" action", name, original)
}

// isModifier 判断归一化后的名字是否为修饰键。
func isModifier(name string) bool {
	switch name {
	case "ctrl", "alt", "shift", "cmd", "win":
		return true
	}
	return false
}

// hasModifier 判断组合里是否包含某个归一化修饰键。
func hasModifier(mods []string, name string) bool {
	for _, m := range mods {
		if m == name {
			return true
		}
	}
	return false
}

// parseFnKey 解析 "f1".."f24"，返回 1..24。
func parseFnKey(key string) (int, bool) {
	if len(key) < 2 || key[0] != 'f' {
		return 0, false
	}
	n := 0
	for _, c := range key[1:] {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 24 {
		return 0, false
	}
	return n, true
}
