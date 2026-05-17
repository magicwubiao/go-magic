package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Locale 区域设置
type Locale struct {
	code    string
	data    map[string]string
	fallback *Locale
}

// Manager 国际化管理器
type Manager struct {
	mu         sync.RWMutex
	dataDir    string
	locales    map[string]*Locale
	current    string
	fallback   string
}

// NewManager creates internationalization manager
func NewManager(dataDir, defaultLocale string) (*Manager, error) {
	// Use default dir if empty
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/tmp"
		}
		dataDir = filepath.Join(home, ".magic", "i18n")
	}

	m := &Manager{
		dataDir:  dataDir,
		locales:  make(map[string]*Locale),
		current:  defaultLocale,
		fallback: "en",
	}

	// Create directory
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	// Load built-in translations
	m.loadBuiltins()

	// Load user translations
	if err := m.loadUserLocales(); err != nil {
		// Ignore load errors
	}

	return m, nil
}

// SetLocale 设置当前语言
func (m *Manager) SetLocale(code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	code = strings.ToLower(code)
	if _, ok := m.locales[code]; !ok {
		return nil
	}

	m.current = code
	return nil
}

// GetLocale 获取当前语言
func (m *Manager) GetLocale() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// T 翻译文本
func (m *Manager) T(key string, args ...interface{}) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	locale := m.locales[m.current]
	if locale == nil {
		locale = m.locales[m.fallback]
	}

	if locale == nil {
		return key
	}

	// 查找翻译
	text := locale.get(key)

	// 尝试回退
	if text == "" && locale.fallback != nil {
		text = locale.fallback.get(key)
	}

	if text == "" {
		return key
	}

	// 格式化参数
	if len(args) > 0 {
		return format(text, args)
	}

	return text
}

// ListLocales 列出可用语言
func (m *Manager) ListLocales() []LocaleInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]LocaleInfo, 0, len(m.locales))
	for code, locale := range m.locales {
		result = append(result, LocaleInfo{
			Code:     code,
			Name:     locale.get("locale_name"),
			Native:   locale.get("locale_native"),
			Current:  code == m.current,
		})
	}
	return result
}

// get 获取翻译
func (l *Locale) get(key string) string {
	if l == nil {
		return ""
	}
	return l.data[key]
}

// loadBuiltins 加载内置语言
func (m *Manager) loadBuiltins() {
	// 英语
	m.locales["en"] = &Locale{
		code: "en",
		data: map[string]string{
			"locale_name":  "English",
			"locale_native": "English",

			// 通用
			"ok":              "OK",
			"cancel":          "Cancel",
			"save":            "Save",
			"delete":          "Delete",
			"edit":            "Edit",
			"create":          "Create",
			"close":           "Close",
			"back":            "Back",
			"next":            "Next",
			"yes":             "Yes",
			"no":              "No",
			"error":           "Error",
			"warning":         "Warning",
			"success":         "Success",
			"info":            "Info",
			"loading":         "Loading...",
			"done":            "Done",
			"retry":           "Retry",
			"skip":            "Skip",
			"help":            "Help",
			"search":          "Search",
			"settings":        "Settings",
			"exit":            "Exit",

			// 命令行
			"cmd.help.title":      "Magic Agent - Help",
			"cmd.help.usage":      "Usage",
			"cmd.help.examples":   "Examples",
			"cmd.help.options":    "Options",

			// 错误信息
			"err.not_found":        "Not found",
			"err.invalid_input":    "Invalid input",
			"err.permission_denied":"Permission denied",
			"err.network_error":    "Network error",
			"err.timeout":          "Request timeout",
			"err.internal":         "Internal error",

			// 欢迎信息
			"welcome.title":   "Welcome to Magic Agent",
			"welcome.subtitle": "A high-performance AI Agent in Go",
			"welcome.tip":     "Type /help for available commands",
		},
	}

	// 中文
	m.locales["zh"] = &Locale{
		code: "zh",
		data: map[string]string{
			"locale_name":  "Chinese",
			"locale_native": "中文",

			// 通用
			"ok":              "确定",
			"cancel":          "取消",
			"save":            "保存",
			"delete":          "删除",
			"edit":            "编辑",
			"create":          "创建",
			"close":           "关闭",
			"back":            "返回",
			"next":            "下一步",
			"yes":             "是",
			"no":              "否",
			"error":           "错误",
			"warning":         "警告",
			"success":         "成功",
			"info":            "信息",
			"loading":         "加载中...",
			"done":            "完成",
			"retry":           "重试",
			"skip":            "跳过",
			"help":            "帮助",
			"search":          "搜索",
			"settings":        "设置",
			"exit":            "退出",

			// 命令行
			"cmd.help.title":      "Magic Agent - 帮助",
			"cmd.help.usage":      "用法",
			"cmd.help.examples":   "示例",
			"cmd.help.options":    "选项",

			// 错误信息
			"err.not_found":        "未找到",
			"err.invalid_input":   "无效输入",
			"err.permission_denied":"权限被拒绝",
			"err.network_error":   "网络错误",
			"err.timeout":         "请求超时",
			"err.internal":        "内部错误",

			// 欢迎信息
			"welcome.title":   "欢迎使用 Magic Agent",
			"welcome.subtitle": "高性能 Go 语言 AI 助手",
			"welcome.tip":     "输入 /help 查看可用命令",
		},
	}

	// 日语
	m.locales["ja"] = &Locale{
		code: "ja",
		data: map[string]string{
			"locale_name":  "Japanese",
			"locale_native": "日本語",

			// 通用
			"ok":              "OK",
			"cancel":          "キャンセル",
			"save":            "保存",
			"delete":          "削除",
			"edit":            "編集",
			"create":          "作成",
			"close":           "閉じる",
			"back":            "戻る",
			"next":            "次へ",
			"yes":             "はい",
			"no":              "いいえ",
			"error":           "エラー",
			"warning":         "警告",
			"success":         "成功",
			"info":            "情報",
			"loading":         "読み込み中...",
			"done":            "完了",
			"retry":           "再試行",
			"skip":            "スキップ",
			"help":            "ヘルプ",
			"search":          "検索",
			"settings":        "設定",
			"exit":            "終了",

			// 命令行
			"cmd.help.title":      "Magic Agent - ヘルプ",
			"cmd.help.usage":      "使用方法",
			"cmd.help.examples":   "例",
			"cmd.help.options":    "オプション",

			// 错误信息
			"err.not_found":        "見つかりません",
			"err.invalid_input":   "無効な入力",
			"err.permission_denied":"権限がありません",
			"err.network_error":   "ネットワークエラー",
			"err.timeout":         "タイムアウト",
			"err.internal":        "内部エラー",

			// 欢迎信息
			"welcome.title":   "Magic Agent へようこそ",
			"welcome.subtitle": "高性能 Go 言語 AI アシスタント",
			"welcome.tip":     "/help でコマンド一覧を表示",
		},
	}

	// 设置回退
	for _, locale := range m.locales {
		if fallback, ok := m.locales[m.fallback]; ok {
			locale.fallback = fallback
		}
	}
}

// loadUserLocales 加载用户翻译
func (m *Manager) loadUserLocales() error {
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			localePath := filepath.Join(m.dataDir, entry.Name(), "messages.json")
			if data, err := os.ReadFile(localePath); err == nil {
				var translations map[string]string
				if err := json.Unmarshal(data, &translations); err == nil {
					if locale, ok := m.locales[entry.Name()]; ok {
						// 合并翻译
						for k, v := range translations {
							locale.data[k] = v
						}
					}
				}
			}
		}
	}

	return nil
}

// AddTranslation 添加翻译
func (m *Manager) AddTranslation(locale, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	l, ok := m.locales[locale]
	if !ok {
		l = &Locale{
			code: locale,
			data: make(map[string]string),
		}
		m.locales[locale] = l
	}

	l.data[key] = value

	// 保存到文件
	localeDir := filepath.Join(m.dataDir, locale)
	os.MkdirAll(localeDir, 0755)

	path := filepath.Join(localeDir, "messages.json")
	data, err := json.MarshalIndent(l.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// LocaleInfo 语言信息
type LocaleInfo struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Native  string `json:"native"`
	Current bool   `json:"current"`
}

// format 格式化字符串
func format(template string, args []interface{}) string {
	result := template
	for i, arg := range args {
		placeholder := "{" + string(rune('0'+i)) + "}"
		result = strings.Replace(result, placeholder, formatValue(arg), -1)
	}
	return result
}

// formatValue 格式化值
func formatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return string(rune(val))
	default:
		return ""
	}
}
