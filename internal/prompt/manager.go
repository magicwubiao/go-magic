package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// Template Prompt 模板
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Content     string            `json:"content"`
	Variables   []Variable        `json:"variables"`
	Tags        []string          `json:"tags"`
	Version     int               `json:"version"`
	Author      string            `json:"author"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	UsageCount  int               `json:"usage_count"`
	Metadata    map[string]string `json:"metadata"`
}

// Variable 模板变量
type Variable struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // string, int, bool, select
	Default     string   `json:"default"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Options     []string `json:"options,omitempty"` // for select type
}

// RenderResult 渲染结果
type RenderResult struct {
	Content   string
	Variables map[string]string
	Warnings  []string
}

// Manager Prompt 模板管理器
type Manager struct {
	dataDir   string
	templates map[string]*Template
	funcMap   template.FuncMap
}

// NewManager 创建新的 Prompt 管理器
func NewManager(dataDir string) (*Manager, error) {
	m := &Manager{
		dataDir:   dataDir,
		templates: make(map[string]*Template),
		funcMap:   template.FuncMap{},
	}

	// 注册函数
	m.registerFunctions()

	// 创建目录
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	// 加载模板
	if err := m.load(); err != nil {
		// 加载默认模板
		m.loadDefaults()
	}

	return m, nil
}

// registerFunctions 注册模板函数
func (m *Manager) registerFunctions() {
	m.funcMap = template.FuncMap{
		// 字符串函数
		"upper":    strings.ToUpper,
		"lower":    strings.ToLower,
		"title":    strings.Title,
		"trim":     strings.TrimSpace,
		"truncate": m.truncate,
		"replace":  strings.ReplaceAll,

		// 格式化函数
		"date":      m.formatDate,
		"time":      m.formatTime,
		"timestamp": m.timestamp,

		// 逻辑函数
		"default": m.defaultValue,
		"env":     os.Getenv,

		// 列表函数
		"join":  strings.Join,
		"split": strings.Split,

		// 数字函数
		"add":          m.add,
		"sub":          m.sub,
		"mult":         m.mult,
		"formatNumber": m.formatNumber,
	}

	// 注册内置模板
	m.loadDefaults()
}

// truncate 截断字符串
func (m *Manager) truncate(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length-3] + "..."
}

// formatDate 格式化日期
func (m *Manager) formatDate(format string) string {
	return time.Now().Format(format)
}

// formatTime 格式化时间
func (m *Manager) formatTime() string {
	return time.Now().Format("15:04:05")
}

// timestamp 时间戳
func (m *Manager) timestamp() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

// defaultValue 默认值
func (m *Manager) defaultValue(value, defaultVal string) string {
	if value == "" {
		return defaultVal
	}
	return value
}

// add 加法
func (m *Manager) add(a, b int) int {
	return a + b
}

// sub 减法
func (m *Manager) sub(a, b int) int {
	return a - b
}

// mult 乘法
func (m *Manager) mult(a, b int) int {
	return a * b
}

// formatNumber 格式化数字
func (m *Manager) formatNumber(n int) string {
	return fmt.Sprintf("%d", n)
}

// loadDefaults 加载默认模板
func (m *Manager) loadDefaults() {
	defaults := []*Template{
		{
			ID:          "default-chat",
			Name:        "Default Chat",
			Description: "Default chat prompt with basic context",
			Content: `You are {{.agent_name}}, {{.agent_description}}.
{{if .context}}Context:
{{.context}}
{{end}}
{{if .system_prompt}}
System Instructions:
{{.system_prompt}}
{{end}}
Current time: {{.current_time}}
{{if .tools}}Available tools:
{{.tools}}
{{end}}
Remember to be helpful, harmless, and honest.`,
			Variables: []Variable{
				{Name: "agent_name", Type: "string", Default: "magic", Description: "Agent name"},
				{Name: "agent_description", Type: "string", Default: "a helpful AI assistant", Description: "Agent description"},
				{Name: "context", Type: "string", Default: "", Description: "Additional context"},
				{Name: "system_prompt", Type: "string", Default: "", Description: "Additional system instructions"},
				{Name: "tools", Type: "string", Default: "", Description: "Available tools description"},
			},
			Tags:      []string{"chat", "default"},
			Author:    "system",
			CreatedAt: time.Now(),
		},
		{
			ID:          "code-review",
			Name:        "Code Review",
			Description: "Prompt for reviewing code changes",
			Content: `You are a code reviewer examining the following changes:

Repository: {{.repo_name}}
Branch: {{.branch_name}}
Commit: {{.commit_sha}}

Files changed:
{{.files_changed}}

Diff:
{{.diff}}

Please review this code and provide feedback on:
1. Potential bugs or security issues
2. Code quality and style
3. Performance concerns
4. Suggestions for improvement

Be specific and cite line numbers where applicable.`,
			Variables: []Variable{
				{Name: "repo_name", Type: "string", Required: true, Description: "Repository name"},
				{Name: "branch_name", Type: "string", Required: true, Description: "Branch name"},
				{Name: "commit_sha", Type: "string", Required: true, Description: "Commit SHA"},
				{Name: "files_changed", Type: "string", Required: true, Description: "List of changed files"},
				{Name: "diff", Type: "string", Required: true, Description: "Code diff"},
			},
			Tags:      []string{"code", "review", "development"},
			Author:    "system",
			CreatedAt: time.Now(),
		},
		{
			ID:          "summarize",
			Name:        "Summarize",
			Description: "Summarize long content into key points",
			Content: `Please summarize the following content:

---

{{.content}}

---

Provide a concise summary with:
- Main topic/theme
- Key points ({{.num_points | default "3"}} points maximum)
- Important details
- Any actionable conclusions or recommendations`,
			Variables: []Variable{
				{Name: "content", Type: "string", Required: true, Description: "Content to summarize"},
				{Name: "num_points", Type: "int", Default: "3", Description: "Maximum number of key points"},
			},
			Tags:      []string{"summarize", "analysis"},
			Author:    "system",
			CreatedAt: time.Now(),
		},
		{
			ID:          "debug",
			Name:        "Debug Assistant",
			Description: "Help debug code issues",
			Content: `You are debugging the following code issue:

Issue Description:
{{.issue_description}}

Error Message:
{{.error_message}}

Code:
{{.code}}

Language: {{.language}}
Framework: {{.framework | default "none"}}

Please help identify:
1. The root cause of the issue
2. Suggested fix
3. Prevention tips`,
			Variables: []Variable{
				{Name: "issue_description", Type: "string", Required: true, Description: "Describe the issue"},
				{Name: "error_message", Type: "string", Description: "Error message if any"},
				{Name: "code", Type: "string", Required: true, Description: "Code with the issue"},
				{Name: "language", Type: "string", Default: "unknown", Description: "Programming language"},
				{Name: "framework", Type: "string", Default: "", Description: "Framework used"},
			},
			Tags:      []string{"debug", "code", "development"},
			Author:    "system",
			CreatedAt: time.Now(),
		},
	}

	for _, t := range defaults {
		m.templates[t.ID] = t
	}
}

// Render 渲染模板
func (m *Manager) Render(id string, variables map[string]string) (*RenderResult, error) {
	tpl, ok := m.templates[id]
	if !ok {
		return nil, fmt.Errorf("template '%s' not found", id)
	}

	// 验证必需变量
	result := &RenderResult{
		Variables: make(map[string]string),
		Warnings:  make([]string, 0),
	}

	// 设置默认值并检查必需变量
	for _, v := range tpl.Variables {
		if val, ok := variables[v.Name]; ok && val != "" {
			result.Variables[v.Name] = val
		} else if v.Default != "" {
			result.Variables[v.Name] = v.Default
		} else if v.Required {
			return nil, fmt.Errorf("required variable '%s' is missing", v.Name)
		} else {
			result.Variables[v.Name] = ""
		}
	}

	// 添加内置变量
	result.Variables["current_time"] = time.Now().Format("2006-01-02 15:04:05")
	result.Variables["current_date"] = time.Now().Format("2006-01-02")
	result.Variables["current_year"] = fmt.Sprintf("%d", time.Now().Year())

	// 创建模板
	tmpl, err := template.New(id).Funcs(m.funcMap).Parse(tpl.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	// 渲染
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, result.Variables); err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	result.Content = buf.String()

	// 更新使用计数
	tpl.UsageCount++
	tpl.UpdatedAt = time.Now()

	return result, nil
}

// RenderString 渲染字符串模板
func (m *Manager) RenderString(content string, variables map[string]string) (string, error) {
	tmpl, err := template.New("custom").Funcs(m.funcMap).Parse(content)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, variables); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Create 创建新模板
func (m *Manager) Create(name, description, content string, tags []string, variables []Variable) (*Template, error) {
	id := generateID(name)

	// 检查是否已存在
	if _, ok := m.templates[id]; ok {
		return nil, fmt.Errorf("template '%s' already exists", id)
	}

	tpl := &Template{
		ID:          id,
		Name:        name,
		Description: description,
		Content:     content,
		Variables:   variables,
		Tags:        tags,
		Version:     1,
		Author:      "user",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    make(map[string]string),
	}

	m.templates[id] = tpl
	if err := m.save(); err != nil {
		delete(m.templates, id)
		return nil, err
	}

	return tpl, nil
}

// Update 更新模板
func (m *Manager) Update(id string, content string) error {
	tpl, ok := m.templates[id]
	if !ok {
		return fmt.Errorf("template '%s' not found", id)
	}

	tpl.Content = content
	tpl.Version++
	tpl.UpdatedAt = time.Now()

	return m.save()
}

// Delete 删除模板
func (m *Manager) Delete(id string) error {
	if _, ok := m.templates[id]; !ok {
		return fmt.Errorf("template '%s' not found", id)
	}

	delete(m.templates, id)
	return m.save()
}

// Get 获取模板
func (m *Manager) Get(id string) *Template {
	return m.templates[id]
}

// List 列出所有模板
func (m *Manager) List() []*Template {
	result := make([]*Template, 0, len(m.templates))
	for _, t := range m.templates {
		result = append(result, t)
	}
	return result
}

// ListByTag 按标签筛选
func (m *Manager) ListByTag(tag string) []*Template {
	result := make([]*Template, 0)
	for _, tpl := range m.templates {
		for _, t := range tpl.Tags {
			if t == tag {
				result = append(result, tpl)
				break
			}
		}
	}
	return result
}

// Search 搜索模板
func (m *Manager) Search(query string) []*Template {
	query = strings.ToLower(query)
	result := make([]*Template, 0)

	for _, t := range m.templates {
		if strings.Contains(strings.ToLower(t.Name), query) ||
			strings.Contains(strings.ToLower(t.Description), query) {
			result = append(result, t)
			continue
		}
		for _, tag := range t.Tags {
			if strings.Contains(strings.ToLower(tag), query) {
				result = append(result, t)
				break
			}
		}
	}

	return result
}

// Duplicate 复制模板
func (m *Manager) Duplicate(id, newName string) (*Template, error) {
	original, ok := m.templates[id]
	if !ok {
		return nil, fmt.Errorf("template '%s' not found", id)
	}

	newID := generateID(newName)
	duplicate := &Template{
		ID:          newID,
		Name:        newName,
		Description: original.Description,
		Content:     original.Content,
		Variables:   make([]Variable, len(original.Variables)),
		Tags:        original.Tags,
		Version:     1,
		Author:      "user",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    make(map[string]string),
	}

	copy(duplicate.Variables, original.Variables)

	m.templates[newID] = duplicate
	if err := m.save(); err != nil {
		delete(m.templates, newID)
		return nil, err
	}

	return duplicate, nil
}

// Import 导入模板
func (m *Manager) Import(data []byte) (*Template, error) {
	var tpl Template
	if err := json.Unmarshal(data, &tpl); err != nil {
		return nil, err
	}

	// 生成新 ID 避免冲突
	tpl.ID = generateID(tpl.Name)
	tpl.Author = "imported"
	tpl.CreatedAt = time.Now()
	tpl.UpdatedAt = time.Now()

	m.templates[tpl.ID] = &tpl
	if err := m.save(); err != nil {
		delete(m.templates, tpl.ID)
		return nil, err
	}

	return &tpl, nil
}

// Export 导出模板
func (m *Manager) Export(id string) ([]byte, error) {
	tpl := m.templates[id]
	if tpl == nil {
		return nil, fmt.Errorf("template '%s' not found", id)
	}
	return json.MarshalIndent(tpl, "", "  ")
}

// ExportAll 导出所有模板
func (m *Manager) ExportAll() ([]byte, error) {
	return json.MarshalIndent(m.templates, "", "  ")
}

// save 保存到文件
func (m *Manager) save() error {
	path := filepath.Join(m.dataDir, "templates.json")
	data, err := json.MarshalIndent(m.templates, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// load 加载模板
func (m *Manager) load() error {
	path := filepath.Join(m.dataDir, "templates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &m.templates)
}

// generateID 生成唯一 ID
func generateID(name string) string {
	// 转换为小写并替换空格
	id := strings.ToLower(name)
	id = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	return id
}

// ValidateTemplate 验证模板语法
func ValidateTemplate(content string) error {
	_, err := template.New("validate").Parse(content)
	return err
}

// ExtractVariables 提取模板变量
func ExtractVariables(content string) []string {
	re := regexp.MustCompile(`\{\{(\.[a-zA-Z_][a-zA-Z0-9_]*)\}\}`)
	matches := re.FindAllStringSubmatch(content, -1)

	vars := make([]string, 0)
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) > 1 {
			name := match[1][1:] // 去掉前面的点
			if !seen[name] {
				vars = append(vars, name)
				seen[name] = true
			}
		}
	}

	return vars
}
