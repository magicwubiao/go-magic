package skin

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Renderer renders text with skin styling
type Renderer struct {
	skin     *Config
	mu       sync.RWMutex
	frameIdx int
	stopCh   chan struct{}
}

// NewRenderer creates a new skin renderer
func NewRenderer(skin *Config) *Renderer {
	if skin == nil {
		skin = DefaultSkin
	}
	return &Renderer{
		skin:   skin,
		stopCh: make(chan struct{}),
	}
}

// SetSkin updates the active skin
func (r *Renderer) SetSkin(skin *Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skin = skin
	r.frameIdx = 0
}

// GetSkin returns the current skin
func (r *Renderer) GetSkin() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.skin
}

// Banner renders the banner with skin colors
func (r *Renderer) Banner(title, subtitle string) string {
	r.mu.RLock()
	skin := r.skin
	r.mu.RUnlock()

	var sb strings.Builder

	// Border
	border := "═"
	width := 60
	borderLine := strings.Repeat(border, width)

	sb.WriteString(skin.Colors.BannerBorder)
	sb.WriteString("╔")
	sb.WriteString(borderLine)
	sb.WriteString("╗")
	sb.WriteString("\n")

	// Title
	titleLine := fmt.Sprintf("║  %s%s%s  ║", skin.Colors.BannerTitle, centered(title, width-4), skin.Colors.BannerBorder)
	sb.WriteString(titleLine)
	sb.WriteString("\n")

	// Separator
	sb.WriteString(skin.Colors.BannerBorder)
	sb.WriteString("╠")
	sb.WriteString(borderLine)
	sb.WriteString("╣")
	sb.WriteString("\n")

	// Subtitle
	if subtitle != "" {
		subLine := fmt.Sprintf("║  %s%s%s  ║", skin.Colors.BannerText, centered(subtitle, width-4), skin.Colors.BannerBorder)
		sb.WriteString(subLine)
		sb.WriteString("\n")
	}

	// Bottom border
	sb.WriteString(skin.Colors.BannerBorder)
	sb.WriteString("╚")
	sb.WriteString(borderLine)
	sb.WriteString("╝")
	sb.WriteString(reset())

	return sb.String()
}

// SimpleBanner renders a simple banner without box
func (r *Renderer) SimpleBanner(title string) string {
	r.mu.RLock()
	skin := r.skin
	r.mu.RUnlock()

	return fmt.Sprintf("%s[%s]%s %s",
		skin.Colors.BannerBorder,
		skin.Colors.BannerTitle,
		skin.Colors.BannerBorder,
		skin.Colors.BannerText,
	) + title + reset()
}

// SpinnerFrame returns the next spinner frame
func (r *Renderer) SpinnerFrame() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.skin.Spinner.Frames == nil || len(r.skin.Spinner.Frames) == 0 {
		return "●"
	}

	frame := r.skin.Spinner.Frames[r.frameIdx%len(r.skin.Spinner.Frames)]
	r.frameIdx++
	return frame
}

// ThinkingSpinner returns a thinking spinner with verb
func (r *Renderer) ThinkingSpinner() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.skin.Spinner.Frames == nil || len(r.skin.Spinner.Frames) == 0 {
		return "..."
	}

	frame := r.skin.Spinner.Frames[r.frameIdx%len(r.skin.Spinner.Frames)]
	r.frameIdx++
	return frame
}

// ToolPrefix returns the styled tool prefix
func (r *Renderer) ToolPrefix() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	return skin.Colors.ToolPrefixColor + skin.ToolPrefix + reset()
}

// ToolName returns styled tool name with emoji
func (r *Renderer) ToolName(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	emoji := r.skin.ToolEmojis.GetToolEmoji(name)
	return fmt.Sprintf("%s%s %s%s", r.skin.Colors.ToolPrefixColor, emoji, name, reset())
}

// ToolOutput returns styled tool output
func (r *Renderer) ToolOutput(output string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.skin.Colors.ToolText + output + reset()
}

// Success returns styled success message
func (r *Renderer) Success(msg string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.skin.Colors.Success + "✓ " + msg + reset()
}

// Error returns styled error message
func (r *Renderer) Error(msg string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.skin.Colors.Error + "✗ " + msg + reset()
}

// Warning returns styled warning message
func (r *Renderer) Warning(msg string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.skin.Colors.Warning + "⚠ " + msg + reset()
}

// Info returns styled info message
func (r *Renderer) Info(msg string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.skin.Colors.BannerAccent + "ℹ " + msg + reset()
}

// Prompt returns styled prompt symbol
func (r *Renderer) Prompt() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	symbol := skin.Branding.PromptSymbol
	if symbol == "" {
		symbol = ">"
	}
	return skin.Colors.PromptSymbol + symbol + " " + reset()
}

// AgentName returns the styled agent name
func (r *Renderer) AgentName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	name := skin.Branding.AgentName
	if name == "" {
		name = "magic"
	}
	return skin.Colors.BannerTitle + name + reset()
}

// Welcome returns the welcome message
func (r *Renderer) Welcome() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	return skin.Colors.BannerDim + skin.Branding.Welcome + reset()
}

// ResponseLabel returns the styled response label
func (r *Renderer) ResponseLabel() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	label := skin.Branding.ResponseLabel
	if label == "" {
		label = "Response"
	}
	return skin.Colors.ResponseBorder + "─ " + label + reset()
}

// SectionHeader returns a styled section header
func (r *Renderer) SectionHeader(title string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	return skin.Colors.BannerAccent + "═══ " + title + " ═══" + reset()
}

// BulletItem returns a styled bullet item
func (r *Renderer) BulletItem(text string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	return skin.Colors.BannerAccent + "• " + skin.Colors.BannerText + text + reset()
}

// KeyValue returns a styled key-value pair
func (r *Renderer) KeyValue(key, value string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	return skin.Colors.BannerDim + key + ": " + skin.Colors.BannerText + value + reset()
}

// ProgressBar renders a mini progress bar
func (r *Renderer) ProgressBar(current, total int, width int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skin := r.skin
	filled := (current * width) / total
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	percent := (current * 100) / total

	return fmt.Sprintf("%s[%s%s]%s %d%%",
		skin.Colors.BannerDim,
		skin.Colors.Success,
		bar,
		skin.Colors.BannerDim,
		percent,
	)
}

// WaitingAnimation returns a waiting animation with face
func (r *Renderer) WaitingAnimation() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.skin.Spinner.WaitingFaces == nil || len(r.skin.Spinner.WaitingFaces) == 0 {
		return r.SpinnerFrame()
	}

	face := r.skin.Spinner.WaitingFaces[r.frameIdx%len(r.skin.Spinner.WaitingFaces)]
	r.frameIdx++
	return r.skin.Colors.SpinnerActive + r.SpinnerFrame() + " " + face + reset()
}

// ThinkingAnimation returns a thinking animation with verb
func (r *Renderer) ThinkingAnimation() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.skin.Spinner.ThinkingVerbs == nil || len(r.skin.Spinner.ThinkingVerbs) == 0 {
		return r.SpinnerFrame()
	}

	verb := r.skin.Spinner.ThinkingVerbs[r.frameIdx%len(r.skin.Spinner.ThinkingVerbs)]
	r.frameIdx++
	return r.skin.Colors.SpinnerActive + r.SpinnerFrame() + " " + verb + "..." + reset()
}

// Color returns a color by name
func (r *Renderer) Color(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	switch strings.ToLower(name) {
	case "reset":
		return reset()
	case "bold":
		return "\033[1m"
	case "dim":
		return "\033[2m"
	case "red", "error":
		return r.skin.Colors.Error
	case "green", "success":
		return r.skin.Colors.Success
	case "yellow", "warning":
		return r.skin.Colors.Warning
	case "blue":
		return r.skin.Colors.BannerTitle
	case "cyan":
		return r.skin.Colors.ToolPrefixColor
	case "gold":
		return r.skin.Colors.BannerBorder
	case "white", "default":
		return r.skin.Colors.BannerText
	default:
		return reset()
	}
}

// Stop stops any running animations
func (r *Renderer) Stop() {
	close(r.stopCh)
}

// ResetFrame resets the spinner frame index
func (r *Renderer) ResetFrame() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frameIdx = 0
}

// reset returns ANSI reset code
func reset() string {
	return "\033[0m"
}

// centered returns a centered string
func centered(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	padding := (width - len(s)) / 2
	return strings.Repeat(" ", padding) + s
}

// Waiter provides a spinner for long-running operations
type Waiter struct {
	renderer *Renderer
	message  string
	ticker   *time.Ticker
	stopCh   chan struct{}
	mu       sync.Mutex
	started  bool
}

// NewWaiter creates a new waiter with a message
func NewWaiter(renderer *Renderer, message string) *Waiter {
	return &Waiter{
		renderer: renderer,
		message:  message,
		stopCh:   make(chan struct{}),
	}
}

// Start starts the spinner animation
func (w *Waiter) Start() {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.mu.Unlock()

	w.ticker = time.NewTicker(time.Duration(w.renderer.GetSkin().Spinner.Speed) * time.Millisecond)
	go func() {
		for {
			select {
			case <-w.ticker.C:
				fmt.Printf("\r%s %s", w.renderer.ToolPrefix(), w.renderer.ThinkingAnimation())
			case <-w.stopCh:
				return
			}
		}
	}()
}

// Stop stops the spinner and optionally shows a message
func (w *Waiter) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return
	}

	if w.ticker != nil {
		w.ticker.Stop()
	}
	close(w.stopCh)
	w.started = false

	// Clear the line
	fmt.Print("\r" + strings.Repeat(" ", 80) + "\r")
}

// Succeed shows a success message
func (w *Waiter) Succeed(msg string) {
	w.Stop()
	if msg == "" {
		msg = w.message
	}
	fmt.Println(w.renderer.Success(msg))
}

// Fail shows an error message
func (w *Waiter) Fail(msg string) {
	w.Stop()
	if msg == "" {
		msg = w.message
	}
	fmt.Println(w.renderer.Error(msg))
}

// MarkdownRenderer renders markdown content
type MarkdownRenderer struct {
	noColor bool
}

// NewMarkdownRenderer creates a new markdown renderer
func NewMarkdownRenderer(noColor bool) *MarkdownRenderer {
	return &MarkdownRenderer{
		noColor: noColor,
	}
}

// Render renders markdown to styled text
func (r *MarkdownRenderer) Render(text string) string {
	// For now, just return the text (basic implementation)
	// In a real implementation, this would convert markdown to styled terminal output
	return text
}

// RenderStreaming renders markdown content streamingly
func (r *MarkdownRenderer) RenderStreaming(text string) string {
	// For now, just return the text (basic implementation)
	return text
}

// ToolCallDisplay holds tool call information for display
type ToolCallDisplay struct {
	Name      string
	Arguments string
	Started   time.Time
	Ended     time.Time
	Success   bool
	Error     string
}

// ToolCallRenderer renders tool call information
type ToolCallRenderer struct {
	noColor bool
}

// NewToolCallRenderer creates a new tool call renderer
func NewToolCallRenderer(noColor bool) *ToolCallRenderer {
	return &ToolCallRenderer{
		noColor: noColor,
	}
}

// RenderStart renders the start of a tool call
func (r *ToolCallRenderer) RenderStart(display *ToolCallDisplay) {
	if r.noColor {
		fmt.Printf("Calling tool: %s with args: %s\n", display.Name, display.Arguments)
	} else {
		fmt.Printf("\033[36mCalling tool:\033[0m \033[32m%s\033[0m \033[36mwith args:\033[0m %s\n", display.Name, display.Arguments)
	}
}

// RenderResult renders the result of a tool call
func (r *ToolCallRenderer) RenderResult(display *ToolCallDisplay) {
	duration := display.Ended.Sub(display.Started)
	if r.noColor {
		if display.Success {
			fmt.Printf("Tool %s completed in %v\n", display.Name, duration)
		} else {
			fmt.Printf("Tool %s failed: %s\n", display.Name, display.Error)
		}
	} else {
		if display.Success {
			fmt.Printf("\033[32m✓\033[0m Tool \033[32m%s\033[0m completed in %v\n", display.Name, duration)
		} else {
			fmt.Printf("\033[31m✗\033[0m Tool \033[31m%s\033[0m failed: %s\n", display.Name, display.Error)
		}
	}
}

// CostInfo holds cost information for display
type CostInfo struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	TotalCost    float64
	Currency     string
}

// CostRenderer renders cost information
type CostRenderer struct {
	noColor bool
}

// NewCostRenderer creates a new cost renderer
func NewCostRenderer(noColor bool) *CostRenderer {
	return &CostRenderer{
		noColor: noColor,
	}
}

// RenderCost renders cost information
func (r *CostRenderer) RenderCost(info *CostInfo) {
	if r.noColor {
		fmt.Println()
		fmt.Println("╔═══════════════════════════════════════╗")
		fmt.Println("║           Usage Statistics            ║")
		fmt.Println("╠═══════════════════════════════════════╣")
		fmt.Printf("║  Input Tokens:   %-18d ║\n", info.InputTokens)
		fmt.Printf("║  Output Tokens:  %-18d ║\n", info.OutputTokens)
		fmt.Printf("║  Total Tokens:   %-18d ║\n", info.TotalTokens)
		fmt.Printf("║  Est. Cost:      %-18.4f %s ║\n", info.TotalCost, info.Currency)
		fmt.Println("╚═══════════════════════════════════════╝")
		fmt.Println()
	} else {
		fmt.Println()
		fmt.Printf("\033[36m╔═══════════════════════════════════════╗\033[0m\n")
		fmt.Printf("\033[36m║%s           📊 Usage Statistics%s            \033[36m║\033[0m\n", "\033[0m", "\033[0m")
		fmt.Printf("\033[36m╠═══════════════════════════════════════╣\033[0m\n")
		fmt.Printf("\033[36m║%s  Input Tokens:   \033[33m%-18d\033[0m \033[36m║\033[0m\n", "\033[0m", info.InputTokens)
		fmt.Printf("\033[36m║%s  Output Tokens:  \033[33m%-18d\033[0m \033[36m║\033[0m\n", "\033[0m", info.OutputTokens)
		fmt.Printf("\033[36m║%s  Total Tokens:   \033[33m%-18d\033[0m \033[36m║\033[0m\n", "\033[0m", info.TotalTokens)
		fmt.Printf("\033[36m║%s  Est. Cost:      \033[32m%-18.4f %s\033[0m \033[36m║\033[0m\n", "\033[0m", info.TotalCost, info.Currency)
		fmt.Printf("\033[36m╚═══════════════════════════════════════╝\033[0m\n")
		fmt.Println()
	}
}