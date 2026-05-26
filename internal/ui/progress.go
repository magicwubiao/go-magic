package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Color ANSI color codes
type Color struct {
	Reset   string
	Bold    string
	Dim     string
	Black   string
	Red     string
	Green   string
	Yellow  string
	Blue    string
	Magenta string
	Cyan    string
	White   string
}

var ansiColor = &Color{
	Reset:   "\033[0m",
	Bold:    "\033[1m",
	Dim:     "\033[2m",
	Black:   "\033[30m",
	Red:     "\033[31m",
	Green:   "\033[32m",
	Yellow:  "\033[33m",
	Blue:    "\033[34m",
	Magenta: "\033[35m",
	Cyan:    "\033[36m",
	White:   "\033[37m",
}

// ProgressBar 进度条
type ProgressBar struct {
	mu          sync.Mutex
	total       int
	current     int
	width       int
	message     string
	startTime   time.Time
	showTimer   bool
	showPercent bool
	showETA     bool
	colorFg     string
	colorBg     string
	completed   bool
}

// NewProgressBar 创建新进度条
func NewProgressBar(total int) *ProgressBar {
	return &ProgressBar{
		total:       total,
		width:       40,
		showTimer:   true,
		showPercent: true,
		showETA:     true,
		colorFg:     ansiColor.Green,
		startTime:   time.Now(),
	}
}

// SetMessage 设置消息
func (p *ProgressBar) SetMessage(msg string) *ProgressBar {
	p.message = msg
	return p
}

// SetWidth 设置宽度
func (p *ProgressBar) SetWidth(width int) *ProgressBar {
	p.width = width
	return p
}

// SetColors 设置颜色
func (p *ProgressBar) SetColors(fg, bg string) *ProgressBar {
	p.colorFg = fg
	p.colorBg = bg
	return p
}

// SetShowTimer 设置是否显示计时器
func (p *ProgressBar) SetShowTimer(show bool) *ProgressBar {
	p.showTimer = show
	return p
}

// SetShowPercent 设置是否显示百分比
func (p *ProgressBar) SetShowPercent(show bool) *ProgressBar {
	p.showPercent = show
	return p
}

// Increment 增加进度
func (p *ProgressBar) Increment() *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current++
	if p.current >= p.total {
		p.completed = true
	}
	return p
}

// SetProgress 设置进度
func (p *ProgressBar) SetProgress(current int) *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = current
	if p.current >= p.total {
		p.completed = true
	}
	return p
}

// Draw 绘制进度条
func (p *ProgressBar) Draw() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drawLocked()
}

func (p *ProgressBar) drawLocked() {
	// 计算百分比
	percent := 0.0
	if p.total > 0 {
		percent = float64(p.current) / float64(p.total) * 100
	}

	// 计算已填充和未填充的块数
	filled := int(percent / 100 * float64(p.width))
	if filled > p.width {
		filled = p.width
	}
	empty := p.width - filled

	// 构建输出
	var sb strings.Builder
	sb.WriteString("\r")

	// 消息
	if p.message != "" {
		sb.WriteString(fmt.Sprintf("%s: ", p.message))
	}

	// 进度条框架
	sb.WriteString("[")
	sb.WriteString(p.colorFg)
	for i := 0; i < filled; i++ {
		sb.WriteString("█")
	}
	sb.WriteString(ansiColor.Reset)
	for i := 0; i < empty; i++ {
		sb.WriteString("░")
	}
	sb.WriteString("]")

	// 百分比
	if p.showPercent {
		sb.WriteString(fmt.Sprintf(" %5.1f%%", percent))
	}

	// 计数
	sb.WriteString(fmt.Sprintf(" %d/%d", p.current, p.total))

	// 计时器
	if p.showTimer {
		elapsed := time.Since(p.startTime)
		sb.WriteString(fmt.Sprintf(" %s", formatDuration(elapsed)))

		// ETA
		if p.showETA && p.current > 0 && p.current < p.total {
			eta := time.Duration(float64(elapsed) / float64(p.current) * float64(p.total-p.current))
			sb.WriteString(fmt.Sprintf(" ETA:%s", formatDuration(eta)))
		}
	}

	// 清除行尾
	sb.WriteString("   ")
	fmt.Print(sb.String())
}

// Complete 完成并换行
func (p *ProgressBar) Complete() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = p.total
	p.completed = true
	p.drawLocked()
	fmt.Println()
}

// Spinner 旋转指示器
type Spinner struct {
	mu       sync.Mutex
	message  string
	spinner  []string
	index    int
	interval time.Duration
	stopped  bool
	ticker   *time.Ticker
	done     chan bool
}

// NewSpinner 创建新的旋转指示器
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message:  message,
		spinner:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		interval: 100 * time.Millisecond,
		done:     make(chan bool),
	}
}

// SetMessage 设置消息
func (s *Spinner) SetMessage(msg string) *Spinner {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = msg
	return s
}

// SetSpinner 设置旋转字符
func (s *Spinner) SetSpinner(chars []string) *Spinner {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spinner = chars
	return s
}

// Start 开始显示
func (s *Spinner) Start() *Spinner {
	s.ticker = time.NewTicker(s.interval)
	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.mu.Lock()
				if s.stopped {
					s.ticker.Stop()
					s.mu.Unlock()
					return
				}
				s.index = (s.index + 1) % len(s.spinner)
				s.drawLocked()
				s.mu.Unlock()
			case <-s.done:
				s.ticker.Stop()
				return
			}
		}
	}()
	return s
}

// Stop 停止显示
func (s *Spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.done <- true
	// 清除行
	fmt.Print("\r")
	fmt.Print(strings.Repeat(" ", 50))
	fmt.Print("\r")
}

// Success 显示成功
func (s *Spinner) Success(msg string) {
	s.Stop()
	fmt.Printf("\r%s✓%s %s\n", ansiColor.Green, ansiColor.Reset, msg)
}

// Error 显示错误
func (s *Spinner) Error(msg string) {
	s.Stop()
	fmt.Printf("\r%s✗%s %s\n", ansiColor.Red, ansiColor.Reset, msg)
}

// Warning 显示警告
func (s *Spinner) Warning(msg string) {
	s.Stop()
	fmt.Printf("\r%s⚠%s %s\n", ansiColor.Yellow, ansiColor.Reset, msg)
}

func (s *Spinner) drawLocked() {
	fmt.Printf("\r%s%s%s %s", ansiColor.Cyan, s.spinner[s.index], ansiColor.Reset, s.message)
}

// StatusMessage 状态消息
func StatusMessage(msg string) {
	fmt.Printf("\r%s%s%s\n", ansiColor.Dim, msg, ansiColor.Reset)
}

// SuccessMessage 成功消息
func SuccessMessage(msg string) {
	fmt.Printf("%s✓%s %s\n", ansiColor.Green, ansiColor.Reset, msg)
}

// ErrorMessage 错误消息
func ErrorMessage(msg string) {
	fmt.Printf("%s✗%s %s\n", ansiColor.Red, ansiColor.Reset, msg)
}

// WarningMessage 警告消息
func WarningMessage(msg string) {
	fmt.Printf("%s⚠%s %s\n", ansiColor.Yellow, ansiColor.Reset, msg)
}

// InfoMessage 信息消息
func InfoMessage(msg string) {
	fmt.Printf("%sℹ%s %s\n", ansiColor.Blue, ansiColor.Reset, msg)
}

// formatDuration 格式化时长
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// Table 表格绘制
type Table struct {
	headers []string
	rows    [][]string
	widths  []int
}

// NewTable 创建新表格
func NewTable(headers ...string) *Table {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	return &Table{
		headers: headers,
		widths:  widths,
	}
}

// AddRow 添加行
func (t *Table) AddRow(row ...string) *Table {
	for i, cell := range row {
		if i >= len(t.widths) {
			break
		}
		if len(cell) > t.widths[i] {
			t.widths[i] = len(cell)
		}
	}
	t.rows = append(t.rows, row)
	return t
}

// Render 渲染表格
func (t *Table) Render() string {
	var sb strings.Builder

	// 分隔线
	separator := "+"
	for _, w := range t.widths {
		separator += strings.Repeat("-", w+2) + "+"
	}
	sb.WriteString(separator + "\n")

	// 表头
	sb.WriteString("|")
	for i, h := range t.headers {
		sb.WriteString(fmt.Sprintf(" %-*s |", t.widths[i], h))
	}
	sb.WriteString("\n")
	sb.WriteString(separator + "\n")

	// 数据行
	for _, row := range t.rows {
		sb.WriteString("|")
		for i := range t.headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			sb.WriteString(fmt.Sprintf(" %-*s |", t.widths[i], cell))
		}
		sb.WriteString("\n")
	}

	// 底部
	sb.WriteString(separator)
	return sb.String()
}

// Print 打印表格
func (t *Table) Print() {
	fmt.Print(t.Render())
}

// Box 绘制方框
func Box(title, content string, width int) string {
	border := strings.Repeat("─", width-4)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("┌─%s─┐\n", border))
	if title != "" {
		sb.WriteString(fmt.Sprintf("│ %-*s │\n", width-4, title))
		sb.WriteString(fmt.Sprintf("├─%s─┤\n", border))
	}
	lines := wrapText(content, width-4)
	for _, line := range lines {
		sb.WriteString(fmt.Sprintf("│ %-*s │\n", width-4, line))
	}
	sb.WriteString(fmt.Sprintf("└─%s─┘", border))
	return sb.String()
}

// wrapText 包装文本
func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	var lines []string
	var current strings.Builder

	for _, word := range words {
		if current.Len()+len(word)+1 > width {
			lines = append(lines, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}

	return lines
}
