package tool

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrDesktopUnavailable 表示当前平台或运行环境无法控制桌面：无图形会话的
// 容器、Wayland 下的 X11 工具、缺少 xdotool/cliclick 等依赖、macOS 未授予
// 辅助功能/屏幕录制权限。错误的 Message 面向用户，要能直接指出缺什么。
var ErrDesktopUnavailable = errors.New("desktop control is unavailable")

// mouseButton 是平台无关的鼠标按键枚举。
type mouseButton int

const (
	buttonLeft mouseButton = iota
	buttonRight
	buttonMiddle
)

// String 用于日志与错误信息。
func (b mouseButton) String() string {
	switch b {
	case buttonRight:
		return "right"
	case buttonMiddle:
		return "middle"
	default:
		return "left"
	}
}

// captureResult 是一次整屏抓取的结果。
//
// 坐标系约定（整个 computer_use 模块都遵守）：
//   - "截图坐标系" = 截图像素坐标，原点在左上角。模型看到的就是这个坐标系，
//     schema 里的 x/y 也一律是截图坐标。
//   - "输入坐标系" = 平台原生输入 API 使用的单位。Windows 是物理像素、
//     macOS 是逻辑 point、X11 是 root window 像素。
//   - Scale() 返回 截图像素 / 输入单位。工具层用 x/scale 把模型坐标换算成
//     输入坐标，driver 只认输入坐标。
//
// Retina（macOS 2x）与 DPI 缩放（Windows 150%）下这两个坐标系不相等，
// 不做换算就会出现"点偏一半屏幕"的经典故障。
type captureResult struct {
	PNG     []byte
	Width   int
	Height  int
	CursorX int // 截图坐标系下的光标位置；-1 表示该平台未提供
	CursorY int
}

// desktopDriver 是一个平台的桌面控制后端。
//
// 所有坐标参数都是输入坐标系（见 captureResult 注释），由上层完成换算。
// 实现必须并发安全：上层虽然串行化调用，但 Scale/Capture 可能被并发读取。
type desktopDriver interface {
	// Capture 抓取整个虚拟桌面并返回 PNG 编码后的图像。
	Capture(ctx context.Context) (*captureResult, error)
	// Scale 返回 截图像素 / 输入单位。可能在首次调用时做一次探测并缓存。
	Scale() (sx, sy float64)
	// Move 把光标移到 (x,y)。
	Move(ctx context.Context, x, y int) error
	// Click 在 (x,y) 点击 count 次。
	Click(ctx context.Context, x, y int, btn mouseButton, count int) error
	// Drag 从 (fromX,fromY) 拖到 (toX,toY)，等同按住左键移动后释放。
	Drag(ctx context.Context, fromX, fromY, toX, toY int) error
	// TypeText 输入一段文本（键盘布局无关，尽量走 Unicode 注入）。
	TypeText(ctx context.Context, text string) error
	// Key 按下组合键，combo 形如 "enter"、"ctrl+c"、"ctrl+shift+t"。
	Key(ctx context.Context, combo string) error
	// Scroll 把光标放到 (x,y) 后滚动 amount 格，正数向下、负数向上，
	// 一格为 120（WHEEL_DELTA）等价量。
	Scroll(ctx context.Context, x, y, amount int) error
	// Available 返回 nil 表示依赖齐备可用，否则返回面向用户的原因。
	Available() error
}

// platformDriver 由各平台文件实现，返回该平台的 driver。
// （定义在 computer_use_windows.go / _darwin.go / _linux.go / _other.go）

// absInt 返回绝对值。栏位运算比 math.Abs 少一次类型转换，滚动计算里用得最多。
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// runCmd 执行外部命令并把 stderr 合并进错误信息。
// 桌面驱动的 Unix 实现（xdotool / cliclick / screencapture）都靠它，
// 错误里必须带上工具自己的输出——否则用户只能看到 "exit status 1"。
func runCmd(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return msg, fmt.Errorf("%s failed: %s", filepath.Base(bin), msg)
	}
	return string(out), nil
}

// unsupportedDriver 用于不支持的平台：所有操作都返回 ErrDesktopUnavailable，
// 配合 ComputerUseTool.Available() 让它不出现在模型可见的工具列表里。
type unsupportedDriver struct {
	reason error
}

func (d *unsupportedDriver) Capture(context.Context) (*captureResult, error) {
	return nil, d.err()
}

func (d *unsupportedDriver) Scale() (float64, float64) { return 1, 1 }

func (d *unsupportedDriver) Move(context.Context, int, int) error { return d.err() }

func (d *unsupportedDriver) Click(context.Context, int, int, mouseButton, int) error {
	return d.err()
}

func (d *unsupportedDriver) Drag(context.Context, int, int, int, int) error { return d.err() }

func (d *unsupportedDriver) TypeText(context.Context, string) error { return d.err() }

func (d *unsupportedDriver) Key(context.Context, string) error { return d.err() }

func (d *unsupportedDriver) Scroll(context.Context, int, int, int) error { return d.err() }

func (d *unsupportedDriver) Available() error { return d.err() }

func (d *unsupportedDriver) err() error {
	if d.reason != nil {
		return d.reason
	}
	return ErrDesktopUnavailable
}
