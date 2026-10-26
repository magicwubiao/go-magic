//go:build windows

package tool

import (
	"bytes"
	"context"
	"image"
	_ "image/png" // 注册 PNG 解码器
	"os"
	"testing"
	"time"
)

// TestWindowsDesktopSmoke 验证 GDI 截图与 SendInput 坐标链路在真机上对得上。
//
// 默认跳过：它会真的移动鼠标光标（虽然会复位），不适合放进常规测试。
// 手动执行：
//
//	COMPUTER_USE_SMOKE=1 go test ./internal/tool/ -run TestWindowsDesktopSmoke -v
//
// 它验证四件事：
//  1. BitBlt 截图成功，且像素尺寸与 GetSystemMetrics 报告的虚拟屏尺寸一致
//     （不一致说明 DPI 虚拟化导致截图与输入坐标不在同一空间）。
//  2. 截图里报告的坐标原点与虚拟屏左上角一致。
//  3. 用截图坐标发出移动指令后，光标真的落在那个位置（±2px 舍入误差）。
//  4. 复位光标，不留副作用。
func TestWindowsDesktopSmoke(t *testing.T) {
	if os.Getenv("COMPUTER_USE_SMOKE") != "1" {
		t.Skip("set COMPUTER_USE_SMOKE=1 to run (moves the real mouse cursor briefly)")
	}

	d := &winDriver{}
	ctx := context.Background()

	first, err := d.Capture(ctx)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if len(first.PNG) == 0 {
		t.Fatal("capture returned an empty image")
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(first.PNG))
	if err != nil {
		t.Fatalf("capture is not valid PNG: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want png", format)
	}
	if cfg.Width != first.Width || cfg.Height != first.Height {
		t.Errorf("metadata says %dx%d but decoded PNG is %dx%d",
			first.Width, first.Height, cfg.Width, cfg.Height)
	}

	vx, vy, vw, vh := virtualScreen()
	t.Logf("virtual screen origin=(%d,%d) size=%dx%d, capture=%dx%d, png=%d bytes",
		vx, vy, vw, vh, first.Width, first.Height, len(first.PNG))

	// 1. 截图像素尺寸必须等于虚拟屏尺寸（缩放因子 1:1，驱动声明如此）。
	if first.Width != vw || first.Height != vh {
		t.Fatalf("capture size %dx%d != virtual screen %dx%d — coordinate scaling is wrong, "+
			"clicks would land off-target", first.Width, first.Height, vw, vh)
	}

	// 2. 光标坐标必须落在截图范围内（原点对齐）。
	if first.CursorX < 0 || first.CursorY < 0 ||
		first.CursorX >= vw || first.CursorY >= vh {
		t.Errorf("cursor (%d,%d) outside capture bounds %dx%d — likely an origin mismatch",
			first.CursorX, first.CursorY, vw, vh)
	}
	origX, origY := first.CursorX, first.CursorY
	defer func() {
		if err := d.Move(ctx, origX, origY); err != nil {
			t.Logf("failed to restore cursor: %v", err)
		}
	}()

	// 3. 移动后回读光标位置，验证绝对坐标归一化是对的。
	targets := [][2]int{
		{vw / 4, vh / 4},
		{vw * 3 / 4, vh * 3 / 4},
	}
	for _, target := range targets {
		if err := d.Move(ctx, target[0], target[1]); err != nil {
			t.Fatalf("Move to %v failed: %v", target, err)
		}
		// SendInput 是异步注入，给它一点时间被输入队列处理。
		time.Sleep(80 * time.Millisecond)

		shot, err := d.Capture(ctx)
		if err != nil {
			t.Fatalf("Capture after move failed: %v", err)
		}
		dx := absInt(shot.CursorX - target[0])
		dy := absInt(shot.CursorY - target[1])
		if dx > 2 || dy > 2 {
			t.Errorf("moved to %v but cursor is at (%d,%d): off by (%d,%d)",
				target, shot.CursorX, shot.CursorY, dx, dy)
		} else {
			t.Logf("move %v -> cursor (%d,%d) ok", target, shot.CursorX, shot.CursorY)
		}
	}
}
