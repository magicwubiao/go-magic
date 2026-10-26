package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// computer_use.go — 桌面 GUI 控制工具（computer use / CUA）。
//
// 设计要点（与旧实现的关键差异）：
//  1. 平台差异全部下沉到 desktopDriver，Windows 走 Win32 SendInput，
//     不再用 SendKeys 拼字符串（旧实现在 Windows 上既点不动，也能被注入）。
//  2. 坐标统一约定为"截图坐标"，由 driver 提供 截图像素/输入单位 的比例换算。
//     Retina 与 DPI 缩放下不做换算就会点偏。
//  3. 每次动作默认返回一张新截图（observe=true），让模型能确认动作结果，
//     而不是盲操作。
//  4. 包级互斥：物理鼠标键盘是全局资源，两个会话同时操作只会互相破坏。
//  5. 该工具会点击任意位置、向任意窗口输入，属于最高危能力，
//     在 internal/agent/approval.go 里强制逐次确认。

// computerUseMu 串行化所有桌面操作。用包级变量而非结构体字段：
// server 与 bot 各自持有一个 Registry/工具实例，物理设备却是同一套。
var computerUseMu sync.Mutex

// computerUseActions 是允许的动作集合。
var computerUseActions = map[string]bool{
	"screenshot": true,
	"click":      true,
	"move":       true,
	"drag":       true,
	"type":       true,
	"key":        true,
	"scroll":     true,
}

// maxTypeTextLen 限制单次输入文本长度，避免模型把整篇文档灌进键盘事件队列。
const maxTypeTextLen = 20000

// maxScrollAmount 限制单次滚动格数（1 格 ≈ 3 行）。
const maxScrollAmount = 5000

// ComputerUseTool 让 agent 通过鼠标键盘操作桌面 GUI 应用。
type ComputerUseTool struct {
	BaseTool
	screenshotDir string
	driver        desktopDriver
}

// NewComputerUseTool 创建桌面控制工具。screenshotDir 为空时落到
// <magic home>/screenshots（与 browser_vision 一致），再退回系统临时目录。
func NewComputerUseTool(screenshotDir string) *ComputerUseTool {
	return &ComputerUseTool{
		BaseTool: *NewBaseTool(
			"computer_use",
			"Control the desktop GUI of the machine this app runs on with mouse and keyboard. "+
				"Loop: (1) action=screenshot to see the screen; (2) read the image (use vision_analyze "+
				"with the returned path if you need to read it); (3) act with click/move/type/key/scroll "+
				"using coordinates you actually saw. Coordinates are ALWAYS screenshot pixels — never "+
				"guess or reuse coordinates from an old screenshot. A fresh screenshot is returned after "+
				"every action (observe=true by default), so re-read it before the next step. Prefer "+
				"keyboard shortcuts over pixel hunting, and prefer type/key over clicking through menus. "+
				"Text input is keyboard-layout independent and supports Unicode. Every call requires "+
				"explicit user approval because it can affect any application on the desktop.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"description": "Action to perform. screenshot = capture only; move = move pointer; " +
							"click = move then click; drag = press at x/y and release at to_x/to_y; " +
							"type = type text; key = press a key combination; scroll = wheel at x/y",
						"enum": []string{"screenshot", "click", "move", "drag", "type", "key", "scroll"},
					},
					"x": map[string]interface{}{
						"type":        "integer",
						"description": "X coordinate in screenshot pixels. Required for click/move/drag/scroll.",
					},
					"y": map[string]interface{}{
						"type":        "integer",
						"description": "Y coordinate in screenshot pixels. Required for click/move/drag/scroll.",
					},
					"to_x": map[string]interface{}{
						"type":        "integer",
						"description": "Drag destination X in screenshot pixels (drag only).",
					},
					"to_y": map[string]interface{}{
						"type":        "integer",
						"description": "Drag destination Y in screenshot pixels (drag only).",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Literal text to type (type action). Supports Unicode; \\n inserts a newline.",
					},
					"key": map[string]interface{}{
						"type": "string",
						"description": "Key or combination to press: \"enter\", \"tab\", \"esc\", \"f5\", \"ctrl+c\", " +
							"\"ctrl+shift+t\", \"cmd+space\". Use modifier+key syntax with ctrl/alt/shift/cmd.",
					},
					"button": map[string]interface{}{
						"type":        "string",
						"description": "Mouse button for click: left (default), right, middle",
						"enum":        []string{"left", "right", "middle"},
					},
					"count": map[string]interface{}{
						"type":        "integer",
						"description": "Click repeat count, 1-3 (2 = double click). Default 1.",
					},
					"scroll_y": map[string]interface{}{
						"type":        "integer",
						"description": "Scroll steps; positive scrolls down, negative up. One step ≈ 3 lines. Default 3.",
					},
					"observe": map[string]interface{}{
						"type":        "boolean",
						"description": "Capture a fresh screenshot after the action (default true). Set false only when chaining rapid input.",
					},
					"include_base64": map[string]interface{}{
						"type":        "boolean",
						"description": "Also return the PNG as base64 (default false; the file path is usually enough).",
					},
					"output_dir": map[string]interface{}{
						"type":        "string",
						"description": "Directory to save screenshots in (optional).",
					},
				},
				"required": []string{"action"},
			},
		),
		screenshotDir: screenshotDir,
		driver:        platformDriver(),
	}
}

// computerUseResult 是工具返回给模型的结构化结果。
type computerUseResult struct {
	Success    bool    `json:"success"`
	Action     string  `json:"action"`
	Screenshot string  `json:"screenshot,omitempty"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	ScaleX     float64 `json:"scale_x,omitempty"`
	ScaleY     float64 `json:"scale_y,omitempty"`
	Cursor     string  `json:"cursor,omitempty"`
	ImageB64   string  `json:"image_base64,omitempty"`
	Hint       string  `json:"hint,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// ValidateParams 在真正触碰桌面之前做参数校验。
//
// 旧实现完全没有校验：负坐标、越界坐标、非法键名都会被原样送进系统调用，
// 模型只能拿到"失败了"这种无从修复的反馈。
func (t *ComputerUseTool) ValidateParams(params map[string]interface{}) error {
	action := strings.ToLower(strings.TrimSpace(strValue(params["action"])))
	if action == "" {
		return fmt.Errorf("action is required (one of: screenshot, click, move, drag, type, key, scroll)")
	}
	if !computerUseActions[action] {
		return fmt.Errorf("unsupported action %q (allowed: screenshot, click, move, drag, type, key, scroll)", action)
	}

	switch action {
	case "click", "move", "drag", "scroll":
		if _, ok := numValue(params["x"]); !ok {
			return fmt.Errorf("%s requires integer x (screenshot pixels)", action)
		}
		if _, ok := numValue(params["y"]); !ok {
			return fmt.Errorf("%s requires integer y (screenshot pixels)", action)
		}
	}
	if action == "drag" {
		if _, ok := numValue(params["to_x"]); !ok {
			return fmt.Errorf("drag requires integer to_x (screenshot pixels)")
		}
		if _, ok := numValue(params["to_y"]); !ok {
			return fmt.Errorf("drag requires integer to_y (screenshot pixels)")
		}
	}
	if action == "type" {
		text := strValue(params["text"])
		if text == "" {
			return fmt.Errorf("type requires non-empty text")
		}
		if len(text) > maxTypeTextLen {
			return fmt.Errorf("text is too long (%d bytes, max %d)", len(text), maxTypeTextLen)
		}
	}
	if action == "key" {
		key := strValue(params["key"])
		if key == "" {
			return fmt.Errorf("key requires a key name (e.g. \"enter\", \"ctrl+c\")")
		}
		// 提前解析一次：非法键名不该等到注入阶段才报错。
		if _, err := parseKeyCombo(key); err != nil {
			return err
		}
	}
	if action == "scroll" {
		if v, ok := numValue(params["scroll_y"]); ok && absInt(v) > maxScrollAmount {
			return fmt.Errorf("scroll_y out of range (%d, max ±%d)", v, maxScrollAmount)
		}
	}
	if action == "click" {
		if v, ok := numValue(params["count"]); ok && (v < 1 || v > 3) {
			return fmt.Errorf("count must be between 1 and 3, got %d", v)
		}
	}
	if b := strValue(params["button"]); b != "" {
		switch strings.ToLower(b) {
		case "left", "right", "middle":
		default:
			return fmt.Errorf("unsupported button %q (allowed: left, right, middle)", b)
		}
	}
	return nil
}

func (t *ComputerUseTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if err := t.ValidateParams(params); err != nil {
		return nil, err
	}
	d := t.driver
	if d == nil {
		return nil, fmt.Errorf("computer_use: no desktop driver for this platform: %w", ErrDesktopUnavailable)
	}
	if err := d.Available(); err != nil {
		return nil, err
	}

	action := strings.ToLower(strings.TrimSpace(strValue(params["action"])))
	// 物理鼠标键盘是全局资源：做不到真正并行，宁可让调用方重试，
	// 也不能让两个会话的输入交错。
	if !computerUseMu.TryLock() {
		return nil, fmt.Errorf("another computer_use action is running — the desktop can only be " +
			"driven by one action at a time; wait for it to finish and retry")
	}
	defer computerUseMu.Unlock()

	sx, sy := d.Scale()
	if sx <= 0 {
		sx = 1
	}
	if sy <= 0 {
		sy = 1
	}

	result := &computerUseResult{Action: action, Success: true, ScaleX: sx, ScaleY: sy}

	// 先执行动作，再统一截图（observe）：动作截图能反映动作结果，
	// 这是 CUA 循环里唯一可靠的反馈来源。
	switch action {
	case "screenshot":
		// 只截图，无动作。
	case "move":
		x, y := t.inputCoords(params, sx, sy)
		if err := d.Move(ctx, x, y); err != nil {
			return nil, err
		}
	case "click":
		x, y := t.inputCoords(params, sx, sy)
		btn := parseButton(strValue(params["button"]))
		count := 1
		if v, ok := numValue(params["count"]); ok && v > 0 {
			count = v
		}
		if err := d.Click(ctx, x, y, btn, count); err != nil {
			return nil, err
		}
	case "drag":
		x, y := t.inputCoords(params, sx, sy)
		tx, ty := t.inputCoordsXY(params, "to_x", "to_y", sx, sy)
		if err := d.Drag(ctx, x, y, tx, ty); err != nil {
			return nil, err
		}
	case "type":
		if err := d.TypeText(ctx, strValue(params["text"])); err != nil {
			return nil, err
		}
	case "key":
		if err := d.Key(ctx, strValue(params["key"])); err != nil {
			return nil, err
		}
	case "scroll":
		x, y := t.inputCoords(params, sx, sy)
		amount := 3
		if v, ok := numValue(params["scroll_y"]); ok {
			amount = v
		}
		if err := d.Scroll(ctx, x, y, amount); err != nil {
			return nil, err
		}
	}

	// 非截图动作默认也抓一张，让模型能验证结果。
	observe := true
	if v, ok := params["observe"].(bool); ok {
		observe = v
	}
	if action != "screenshot" && !observe {
		result.Hint = "Action applied without verification. Call action=screenshot before the next step."
		return result, nil
	}

	cap, err := d.Capture(ctx)
	if err != nil {
		if action == "screenshot" {
			return nil, err
		}
		// 动作已经生效，截图失败不该把整次调用判为失败——否则模型会重做动作。
		result.Success = false
		result.Error = "action applied but the follow-up screenshot failed: " + err.Error()
		return result, nil
	}

	outDir := strValue(params["output_dir"])
	path, err := t.persistScreenshot(outDir, cap.PNG)
	if err != nil {
		return nil, err
	}

	result.Screenshot = path
	result.Width = cap.Width
	result.Height = cap.Height
	if cap.CursorX >= 0 && cap.CursorY >= 0 {
		result.Cursor = fmt.Sprintf("%d,%d", cap.CursorX, cap.CursorY)
	}
	if v, ok := params["include_base64"].(bool); ok && v {
		result.ImageB64 = base64.StdEncoding.EncodeToString(cap.PNG)
	}
	result.Hint = fmt.Sprintf(
		"Screenshot is %dx%d (screenshot coordinates are these pixels). "+
			"Read it with vision_analyze(image_path=%q) before the next action.", cap.Width, cap.Height, path)
	if sx != 1 || sy != 1 {
		result.Hint += fmt.Sprintf(" Coordinates are scaled by %.2fx/%.2fx for this display.", sx, sy)
	}
	return result, nil
}

// inputCoords 把 params 里的 x/y 从截图坐标换算成输入坐标。
func (t *ComputerUseTool) inputCoords(params map[string]interface{}, sx, sy float64) (int, int) {
	return t.inputCoordsXY(params, "x", "y", sx, sy)
}

func (t *ComputerUseTool) inputCoordsXY(params map[string]interface{}, xKey, yKey string, sx, sy float64) (int, int) {
	x, _ := numValue(params[xKey])
	y, _ := numValue(params[yKey])
	return int(math.Round(float64(x) / sx)), int(math.Round(float64(y) / sy))
}

// persistScreenshot 落盘并返回路径。文件名带毫秒时间戳，避免并发会话
// 互相覆盖（旧实现固定写 screenshot.png）。
func (t *ComputerUseTool) persistScreenshot(outputDir string, data []byte) (string, error) {
	dir := t.screenshotDir
	if outputDir != "" {
		dir = outputDir
	}
	dir = resolveScreenshotDir(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create screenshot directory: %w", err)
	}
	name := fmt.Sprintf("desktop_%s.png", time.Now().Format("20060102_150405.000"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("failed to save screenshot: %w", err)
	}
	return path, nil
}

func parseButton(s string) mouseButton {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "right":
		return buttonRight
	case "middle":
		return buttonMiddle
	default:
		return buttonLeft
	}
}

// strValue 安全取出字符串参数。
func strValue(v interface{}) string {
	s, _ := v.(string)
	return s
}

// numValue 安全取出整数参数。JSON 反序列化后数字统一是 float64，
// 但内部调用方可能直接传 int，两种都要认。
func numValue(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int(math.Round(n)), true
	case float32:
		return int(math.Round(float64(n))), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

// 编译期断言：确保实现了参数校验接口。
var _ ParamValidator = (*ComputerUseTool)(nil)
