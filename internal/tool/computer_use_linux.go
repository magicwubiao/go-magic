//go:build linux

package tool

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png" // 注册 PNG 解码器，用于读截图尺寸
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// computer_use_linux.go — Linux 桌面驱动（X11）。
//
// 依赖 xdotool（输入）+ 任一截图工具（scrot / ImageMagick import /
// gnome-screenshot）。Wayland 下 xdotool 只能操作 XWayland 应用、无法移动真实
// 光标，因此直接判定不可用并给出可操作的提示，而不是让模型拿到一连串
// "exit status 1"。

type linuxDriver struct {
	availOnce sync.Once
	availErr  error

	shotOnce sync.Once
	shotTool string // 选定的截图工具；空表示没有可用的
	shotCmd  func(path string) []string
}

// platformDriver 返回 Linux 后端。
func platformDriver() desktopDriver { return &linuxDriver{} }

func (d *linuxDriver) Available() error {
	d.availOnce.Do(func() { d.availErr = d.detect() })
	return d.availErr
}

func (d *linuxDriver) detect() error {
	if isWaylandSession() {
		return fmt.Errorf("%w: this is a Wayland session — xdotool cannot move the global cursor "+
			"or drive native Wayland windows. Log in with an X11 session, or run the app with "+
			"XDG_SESSION_TYPE=x11 under XWayland", ErrDesktopUnavailable)
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return fmt.Errorf("%w: xdotool not found — install it (apt install xdotool / "+
			"dnf install xdotool / pacman -S xdotool)", ErrDesktopUnavailable)
	}
	d.shotOnce.Do(d.pickScreenshotTool)
	if d.shotTool == "" {
		return fmt.Errorf("%w: no screenshot tool found — install one of scrot, imagemagick "+
			"(import), or gnome-screenshot", ErrDesktopUnavailable)
	}
	return nil
}

// isWaylandSession 判断当前会话是否为 Wayland。
func isWaylandSession() bool {
	if t := strings.ToLower(os.Getenv("XDG_SESSION_TYPE")); t == "wayland" {
		return true
	}
	// 有些登录器只设 WAYLAND_DISPLAY，不设 XDG_SESSION_TYPE。
	return os.Getenv("XDG_SESSION_TYPE") == "" && os.Getenv("WAYLAND_DISPLAY") != ""
}

// pickScreenshotTool 按可靠性顺序挑一个截图工具。
func (d *linuxDriver) pickScreenshotTool() {
	candidates := []struct {
		name string
		bin  string
		cmd  func(path string) []string
	}{
		{"scrot", "scrot", func(p string) []string { return []string{"-o", p} }},
		{"imagemagick", "import", func(p string) []string { return []string{"-window", "root", p} }},
		{"gnome-screenshot", "gnome-screenshot", func(p string) []string { return []string{"-f", p} }},
		{"grim", "grim", func(p string) []string { return []string{p} }},
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c.bin); err == nil {
			d.shotTool, d.shotCmd = c.name, c.cmd
			return
		}
	}
}

// Scale 在 X11 上恒为 1：截图是 root window 像素，xdotool 的坐标也是
// root window 像素。（HiDPI 缩放由 X server 处理，不改变这两个坐标系的关系。）
func (d *linuxDriver) Scale() (float64, float64) { return 1, 1 }

// Capture 调用截图工具写临时文件，再读回来。
func (d *linuxDriver) Capture(ctx context.Context) (*captureResult, error) {
	if err := d.Available(); err != nil {
		return nil, err
	}
	d.shotOnce.Do(d.pickScreenshotTool)

	tmp, err := os.CreateTemp("", "go-magic-shot-*.png")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	if _, err := runCmd(ctx, d.shotTool, d.shotCmd(path)...); err != nil {
		return nil, fmt.Errorf("screenshot failed (%s): %w", d.shotTool, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read screenshot: %w", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("screenshot is not a valid image: %w", err)
	}

	cx, cy := d.cursorPos(ctx)
	return &captureResult{
		PNG:     data,
		Width:   cfg.Width,
		Height:  cfg.Height,
		CursorX: cx,
		CursorY: cy,
	}, nil
}

// cursorPos 读光标位置，失败返回 -1（截图里没有光标时至少还能给出坐标）。
func (d *linuxDriver) cursorPos(ctx context.Context) (int, int) {
	out, err := runCmd(ctx, "xdotool", "getmouselocation", "--shell")
	if err != nil {
		return -1, -1
	}
	var x, y = -1, -1
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "X="):
			x, _ = strconv.Atoi(strings.TrimPrefix(line, "X="))
		case strings.HasPrefix(line, "Y="):
			y, _ = strconv.Atoi(strings.TrimPrefix(line, "Y="))
		}
	}
	return x, y
}

func (d *linuxDriver) Move(ctx context.Context, x, y int) error {
	_, err := runCmd(ctx, "xdotool", "mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y))
	return err
}

func (d *linuxDriver) Click(ctx context.Context, x, y int, btn mouseButton, count int) error {
	if count < 1 {
		count = 1
	}
	button := "1"
	switch btn {
	case buttonRight:
		button = "3"
	case buttonMiddle:
		button = "2"
	}
	_, err := runCmd(ctx, "xdotool",
		"mousemove", strconv.Itoa(x), strconv.Itoa(y),
		"click", "--repeat", strconv.Itoa(count), "--delay", "40", button)
	return err
}

func (d *linuxDriver) Drag(ctx context.Context, fromX, fromY, toX, toY int) error {
	// 一次调用串起来：xdotool 支持在同一次执行内按顺序执行多个子命令，
	// 分开调用会在进程启动之间丢掉按键状态。
	_, err := runCmd(ctx, "xdotool",
		"mousemove", "--sync", strconv.Itoa(fromX), strconv.Itoa(fromY),
		"mousedown", "1",
		"mousemove", "--sync", strconv.Itoa(toX), strconv.Itoa(toY),
		"mouseup", "1")
	return err
}

func (d *linuxDriver) TypeText(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	// "--" 结束选项解析：以 "-" 开头的文本不会被当成参数。
	_, err := runCmd(ctx, "xdotool", "type", "--clearmodifiers", "--delay", "12", "--", text)
	return err
}

// xdotoolKeysym 把归一化键名映射到 X keysym 名。
var xdotoolKeysym = map[string]string{
	"enter": "Return", "tab": "Tab", "esc": "Escape", "backspace": "BackSpace",
	"delete": "Delete", "space": "space", "insert": "Insert",
	"home": "Home", "end": "End", "pageup": "Prior", "pagedown": "Next",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"printscreen": "Print", "capslock": "Caps_Lock", "numlock": "Num_Lock",
	"scrolllock": "Scroll_Lock", "pause": "Pause", "menu": "Menu",
}

func (d *linuxDriver) Key(ctx context.Context, combo string) error {
	kc, err := parseKeyCombo(combo)
	if err != nil {
		return err
	}

	parts := make([]string, 0, len(kc.Modifiers)+1)
	for _, m := range kc.Modifiers {
		switch m {
		case "ctrl":
			parts = append(parts, "ctrl")
		case "alt":
			parts = append(parts, "alt")
		case "shift":
			parts = append(parts, "shift")
		case "cmd", "win":
			parts = append(parts, "super")
		}
	}
	if name, ok := xdotoolKeysym[kc.Key]; ok {
		parts = append(parts, name)
	} else if n, ok := parseFnKey(kc.Key); ok {
		parts = append(parts, "F"+strconv.Itoa(n))
	} else {
		parts = append(parts, kc.Key)
	}

	_, err = runCmd(ctx, "xdotool", "key", "--clearmodifiers", strings.Join(parts, "+"))
	return err
}

func (d *linuxDriver) Scroll(ctx context.Context, x, y, amount int) error {
	if amount == 0 {
		return nil
	}
	if err := d.Move(ctx, x, y); err != nil {
		return err
	}
	// X11 的滚轮是按钮 4（上）/ 5（下），一次点击 = 一格。
	button := "5" // amount > 0 向下
	if amount < 0 {
		button = "4"
	}
	steps := absInt(amount)
	if steps > 200 {
		steps = 200
	}
	_, err := runCmd(ctx, "xdotool", "click", "--repeat", strconv.Itoa(steps),
		"--delay", "12", button)
	return err
}
