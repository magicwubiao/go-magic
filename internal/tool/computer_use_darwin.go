//go:build darwin

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
	"time"
)

// computer_use_darwin.go — macOS 桌面驱动。
//
// 输入用 cliclick（CGEvent 封装，需 brew install cliclick），截图用系统自带
// screencapture。两者都需要用户在"系统设置 → 隐私与安全性"里授予权限：
//   - 屏幕录制：screencapture 抓屏
//   - 辅助功能：cliclick 注入鼠标键盘事件
// 缺权限时命令会失败但不报明确原因，所以 Available() 之外还会在错误信息里
// 提示用户去检查这两项。
//
// Retina 是这里最关键的坑：screencapture 输出物理像素（2x），cliclick 接受
// 逻辑 point。不换算就会点偏一半。

type darwinDriver struct {
	availOnce sync.Once
	availErr  error

	scaleOnce sync.Once
	scaleX    float64
	scaleY    float64
}

// platformDriver 返回 macOS 后端。
func platformDriver() desktopDriver { return &darwinDriver{} }

func (d *darwinDriver) Available() error {
	d.availOnce.Do(func() {
		if _, err := exec.LookPath("cliclick"); err != nil {
			d.availErr = fmt.Errorf("%w: cliclick not found — install it with "+
				"`brew install cliclick`, then grant Accessibility permission in "+
				"System Settings → Privacy & Security → Accessibility", ErrDesktopUnavailable)
		}
	})
	return d.availErr
}

func screencaptureBin() string {
	if p, err := exec.LookPath("screencapture"); err == nil {
		return p
	}
	return "/usr/sbin/screencapture"
}

func (d *darwinDriver) Scale() (float64, float64) {
	d.scaleOnce.Do(d.detectScale)
	if d.scaleX <= 0 {
		return 1, 1
	}
	return d.scaleX, d.scaleY
}

// detectScale 用 截图物理像素 / 逻辑分辨率 求缩放比。
//
// 逻辑分辨率从 Finder 的 desktop bounds 拿（osascript，失败则退回 1:1）。
// 两者都拿不到时按 1:1 处理——点偏的风险仍然存在，但至少不会除零。
func (d *darwinDriver) detectScale() {
	d.scaleX, d.scaleY = 1, 1

	logicalW, logicalH := desktopBounds()
	if logicalW <= 0 || logicalH <= 0 {
		return
	}
	pw, ph, err := d.probePixelSize()
	if err != nil || pw <= 0 || ph <= 0 {
		return
	}
	sx := float64(pw) / float64(logicalW)
	sy := float64(ph) / float64(logicalH)
	// 误差小于 1% 视为 1:1，避免把浮点噪声传给上层。
	if absFloat(sx-1) < 0.01 {
		sx = 1
	}
	if absFloat(sy-1) < 0.01 {
		sy = 1
	}
	d.scaleX, d.scaleY = sx, sy
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// desktopBounds 通过 Finder 拿主屏逻辑分辨率。需要 Automation 权限，
// 用户拒绝时返回 0,0（上层退回 1:1）。
func desktopBounds() (int, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "osascript", "-e",
		`tell application "Finder" to get bounds of window of desktop`)
	if err != nil {
		return 0, 0
	}
	// 输出形如 "0, 0, 1512, 982"
	fields := strings.Split(strings.TrimSpace(out), ",")
	if len(fields) != 4 {
		return 0, 0
	}
	right, err1 := strconv.Atoi(strings.TrimSpace(fields[2]))
	bottom, err2 := strconv.Atoi(strings.TrimSpace(fields[3]))
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return right, bottom
}

// probePixelSize 抓一张临时截图只为读尺寸，之后丢弃。
func (d *darwinDriver) probePixelSize() (int, int, error) {
	tmp, err := os.CreateTemp("", "go-magic-probe-*.png")
	if err != nil {
		return 0, 0, err
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runCmd(ctx, screencaptureBin(), "-x", path); err != nil {
		return 0, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func (d *darwinDriver) Capture(ctx context.Context) (*captureResult, error) {
	if err := d.Available(); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "go-magic-shot-*.png")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	// -x 静音，-C 把光标一并截进去（CUA 需要知道指针在哪）。
	if _, err := runCmd(ctx, screencaptureBin(), "-x", "-C", path); err != nil {
		return nil, fmt.Errorf("screenshot failed: %w "+
			"(check Screen Recording permission in System Settings → Privacy & Security)", err)
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
		PNG: data, Width: cfg.Width, Height: cfg.Height, CursorX: cx, CursorY: cy,
	}, nil
}

func (d *darwinDriver) cursorPos(ctx context.Context) (int, int) {
	out, err := runCmd(ctx, "cliclick", "p:")
	if err != nil {
		return -1, -1
	}
	fields := strings.Split(strings.TrimSpace(out), ",")
	if len(fields) != 2 {
		return -1, -1
	}
	x, err1 := strconv.Atoi(strings.TrimSpace(fields[0]))
	y, err2 := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err1 != nil || err2 != nil {
		return -1, -1
	}
	return x, y
}

// runCliclick 执行一组 cliclick 命令（一次进程启动内按序执行，
// 这样 kd:/ku: 之间的按键状态不会因为进程退出而丢失）。
func runCliclick(ctx context.Context, commands ...string) error {
	args := append([]string{"-w", "20"}, commands...)
	_, err := runCmd(ctx, "cliclick", args...)
	if err != nil {
		return fmt.Errorf("%w (check Accessibility permission for the app running this "+
			"process in System Settings → Privacy & Security → Accessibility)", err)
	}
	return nil
}

func (d *darwinDriver) Move(ctx context.Context, x, y int) error {
	return runCliclick(ctx, fmt.Sprintf("m:%d,%d", x, y))
}

func (d *darwinDriver) Click(ctx context.Context, x, y int, btn mouseButton, count int) error {
	if count < 1 {
		count = 1
	}
	var verb string
	switch btn {
	case buttonRight:
		verb = "rc"
	case buttonMiddle:
		verb = "mc"
	default:
		switch count {
		case 2:
			verb = "dc"
		case 3:
			verb = "tc"
		default:
			verb = "c"
		}
	}
	return runCliclick(ctx, fmt.Sprintf("%s:%d,%d", verb, x, y))
}

func (d *darwinDriver) Drag(ctx context.Context, fromX, fromY, toX, toY int) error {
	return runCliclick(ctx,
		fmt.Sprintf("dd:%d,%d", fromX, fromY), // drag down
		fmt.Sprintf("dm:%d,%d", toX, toY),     // drag move
		fmt.Sprintf("du:%d,%d", toX, toY),     // drag up
	)
}

// cliclickKeyName 把归一化键名映射到 cliclick 的 kp: 名称。
var cliclickKeyName = map[string]string{
	"enter": "enter", "tab": "tab", "esc": "esc", "space": "space",
	"delete": "delete", "backspace": "backspace",
	"home": "home", "end": "end", "pageup": "page-up", "pagedown": "page-down",
	"up": "arrow-up", "down": "arrow-down", "left": "arrow-left", "right": "arrow-right",
}

// cliclickModifier 把归一化修饰键映射到 cliclick 的 kd:/ku: 名称。
func cliclickModifier(name string) (string, bool) {
	switch name {
	case "ctrl":
		return "ctrl", true
	case "alt":
		return "alt", true
	case "shift":
		return "shift", true
	case "cmd", "win":
		return "cmd", true
	}
	return "", false
}

func (d *darwinDriver) TypeText(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	// t: 会原样输入后续字符；换行需要显式按 Return。
	parts := strings.Split(text, "\n")
	commands := make([]string, 0, len(parts)*2)
	for i, p := range parts {
		if i > 0 {
			commands = append(commands, "kp:enter")
		}
		if p != "" {
			commands = append(commands, "t:"+strings.TrimSuffix(p, "\r"))
		}
	}
	return runCliclick(ctx, commands...)
}

func (d *darwinDriver) Key(ctx context.Context, combo string) error {
	kc, err := parseKeyCombo(combo)
	if err != nil {
		return err
	}

	commands := make([]string, 0, len(kc.Modifiers)*2+2)
	mods := make([]string, 0, len(kc.Modifiers))
	for _, m := range kc.Modifiers {
		name, ok := cliclickModifier(m)
		if !ok {
			return fmt.Errorf("unsupported modifier %q", m)
		}
		mods = append(mods, name)
		commands = append(commands, "kd:"+name)
	}

	if name, ok := cliclickKeyName[kc.Key]; ok {
		commands = append(commands, "kp:"+name)
	} else if strings.HasPrefix(kc.Key, "f") {
		// f1..f16 是 cliclick 支持的范围。
		if n, ok := parseFnKey(kc.Key); ok && n <= 16 {
			commands = append(commands, "kp:f"+strconv.Itoa(n))
		} else {
			return fmt.Errorf("key %q is not supported on macOS (f1-f16 only)", kc.Key)
		}
	} else if len([]rune(kc.Key)) == 1 {
		// 单字符：t: 在当前按下的修饰键状态下输入，等效于按键。
		commands = append(commands, "t:"+kc.Key)
	} else {
		return fmt.Errorf("key %q is not supported on macOS", kc.Key)
	}

	// 修饰键逆序释放，避免中间态触发别的快捷键。
	for i := len(mods) - 1; i >= 0; i-- {
		commands = append(commands, "ku:"+mods[i])
	}
	return runCliclick(ctx, commands...)
}

func (d *darwinDriver) Scroll(ctx context.Context, x, y, amount int) error {
	if amount == 0 {
		return nil
	}
	if err := d.Move(ctx, x, y); err != nil {
		return err
	}
	dir := "w:down"
	if amount < 0 {
		dir = "w:up"
	}
	steps := absInt(amount)
	if steps > 100 {
		steps = 100
	}
	commands := make([]string, steps)
	for i := range commands {
		commands[i] = dir
	}
	return runCliclick(ctx, commands...)
}
