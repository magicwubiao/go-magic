//go:build windows

package tool

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// computer_use_windows.go — Windows 桌面驱动。
//
// 全部输入通过 user32!SendInput 注入，不使用 SendKeys：
//   - SendKeys 没有 {CLICK} 这类键名，且只支持有限的转义，会把 "+"/"%"/"^"
//     当修饰键、把单引号当字符串结束（旧实现因此可直接命令注入）。
//   - SendInput 走内核输入队列，并且能返回"实际注入了几条"：被 UIPI 拦截
//     （目标窗口以管理员权限运行）时可以给出可读的失败原因，而不是假装成功。
// 文本输入走 KEYEVENTF_UNICODE，因此中文/emoji 与键盘布局无关。

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC              = modUser32.NewProc("GetDC")
	procReleaseDC          = modUser32.NewProc("ReleaseDC")
	procGetSystemMetrics   = modUser32.NewProc("GetSystemMetrics")
	procSendInput          = modUser32.NewProc("SendInput")
	procGetCursorInfo      = modUser32.NewProc("GetCursorInfo")
	procDrawIconEx         = modUser32.NewProc("DrawIconEx")
	procSetProcessDPIAware = modUser32.NewProc("SetProcessDPIAware")
	procVkKeyScanW         = modUser32.NewProc("VkKeyScanW")

	procCreateCompatibleDC     = modGdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = modGdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = modGdi32.NewProc("SelectObject")
	procBitBlt                 = modGdi32.NewProc("BitBlt")
	procGetDIBits              = modGdi32.NewProc("GetDIBits")
	procDeleteObject           = modGdi32.NewProc("DeleteObject")
	procDeleteDC               = modGdi32.NewProc("DeleteDC")
)

const (
	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000
	diNormal   = 0x0003

	cursorShowing = 0x0001 // CURSORINFO.flags

	// GetSystemMetrics 索引
	smCXScreen        = 0
	smCYScreen        = 1
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	// SendInput 事件类型
	inputMouse    = 0
	inputKeyboard = 1

	// MOUSEEVENTF_*
	mouseMove        = 0x0001
	mouseLeftDown    = 0x0002
	mouseLeftUp      = 0x0004
	mouseRightDown   = 0x0008
	mouseRightUp     = 0x0010
	mouseMiddleDown  = 0x0020
	mouseMiddleUp    = 0x0040
	mouseWheel       = 0x0800
	mouseAbsolute    = 0x8000
	mouseVirtualDesk = 0x4000

	// KEYEVENTF_*
	keyEventKeyUp   = 0x0002
	keyEventUnicode = 0x0004

	wheelDelta   = 120
	dibRGBColors = 0
)

// winPoint 对应 Win32 POINT。
type winPoint struct {
	X int32
	Y int32
}

// cursorInfo 对应 Win32 CURSORINFO。cbSize 由 unsafe.Sizeof 决定
// （amd64 为 24、386 为 20，与 SDK 一致）。
type cursorInfo struct {
	cbSize  uint32
	flags   uint32
	hCursor uintptr
	pt      winPoint
}

// mouseInput 对应 Win32 MOUSEINPUT，amd64 下 32 字节。
type mouseInput struct {
	dx        int32
	dy        int32
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

// keyboardInput 对应 Win32 KEYBDINPUT，amd64 下 24 字节。
type keyboardInput struct {
	vk        uint16
	scan      uint16
	flags     uint32
	time      uint32
	extraInfo uintptr
}

// winInput 对应 Win32 INPUT：DWORD type + 4 字节填充 + 32 字节 union。
type winInput struct {
	typ  uint32
	_    uint32
	data [32]byte
}

// 编译期断言：结构体大小必须与 Win32 SDK 完全一致。布局错位会让 SendInput
// 静默注入垃圾事件，比直接报错危险得多。
var (
	_ = [1]struct{}{}[unsafe.Sizeof(winInput{})-40]
	_ = [1]struct{}{}[unsafe.Sizeof(mouseInput{})-32]
	_ = [1]struct{}{}[unsafe.Sizeof(keyboardInput{})-24]
)

// bitmapInfoHeader 对应 Win32 BITMAPINFOHEADER。
type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

// bitmapInfo 对应 Win32 BITMAPINFO（32bpp 时不需要调色板）。
type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

// 虚拟键码（Win32 VK_*）。
const (
	vkBack    = 0x08
	vkTab     = 0x09
	vkReturn  = 0x0D
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkPause   = 0x13
	vkCapital = 0x14
	vkEscape  = 0x1B
	vkSpace   = 0x20
	vkPrior   = 0x21
	vkNext    = 0x22
	vkEnd     = 0x23
	vkHome    = 0x24
	vkLeft    = 0x25
	vkUp      = 0x26
	vkRight   = 0x27
	vkDown    = 0x28
	vkPrint   = 0x2C
	vkInsert  = 0x2D
	vkDelete  = 0x2E
	vkLWin    = 0x5B
	vkApps    = 0x5D
	vkNumLock = 0x90
	vkScroll  = 0x91
	vkF1      = 0x70
)

// vkForName 把归一化键名映射到虚拟键码。
var vkForName = map[string]uint16{
	"backspace": vkBack, "tab": vkTab, "enter": vkReturn, "esc": vkEscape,
	"space": vkSpace, "pageup": vkPrior, "pagedown": vkNext, "end": vkEnd,
	"home": vkHome, "left": vkLeft, "up": vkUp, "right": vkRight, "down": vkDown,
	"printscreen": vkPrint, "insert": vkInsert, "delete": vkDelete,
	"capslock": vkCapital, "numlock": vkNumLock, "scrolllock": vkScroll,
	"pause": vkPause, "menu": vkApps,
}

type winDriver struct {
	dpiOnce sync.Once
}

// platformDriver 返回 Windows 后端。
func platformDriver() desktopDriver { return &winDriver{} }

// dpiAware 让进程成为 system-DPI-aware：截图得到物理像素，SendInput 的绝对
// 坐标也落在物理像素空间。失败不致命——若进程被外部清单固定为 DPI-unaware，
// DWM 会把截图和坐标一起虚拟化，比例仍是 1:1，只是画面略糊。
func (d *winDriver) dpiAware() {
	d.dpiOnce.Do(func() { procSetProcessDPIAware.Call() })
}

// Scale 在 Windows 上恒为 1：无论 DPI-aware 与否，截图像素与输入坐标
// 都处在同一坐标系（见 dpiAware 注释）。
func (d *winDriver) Scale() (float64, float64) { return 1, 1 }

// Available 在 Windows 上恒可用：所需 API 都是系统自带 DLL。
// 唯一的运行时限制（权限、会话、UIPI）会在具体动作时给出明确错误。
func (d *winDriver) Available() error { return nil }

func getSystemMetrics(index int) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(int32(r))
}

// virtualScreen 返回虚拟桌面（所有显示器）的左上角与尺寸。
func virtualScreen() (x, y, w, h int) {
	x = getSystemMetrics(smXVirtualScreen)
	y = getSystemMetrics(smYVirtualScreen)
	w = getSystemMetrics(smCXVirtualScreen)
	h = getSystemMetrics(smCYVirtualScreen)
	if w <= 0 || h <= 0 {
		x, y = 0, 0
		w = getSystemMetrics(smCXScreen)
		h = getSystemMetrics(smCYScreen)
	}
	return
}

// Capture 用 GDI BitBlt 抓取整个虚拟桌面，并把当前光标绘制到图上。
// 没有光标的截图无法回答"鼠标现在在哪"，而坐标驱动正靠这个判断。
func (d *winDriver) Capture(ctx context.Context) (*captureResult, error) {
	d.dpiAware()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	vx, vy, vw, vh := virtualScreen()
	if vw <= 0 || vh <= 0 {
		return nil, fmt.Errorf("failed to query screen size")
	}
	// 上限保护：避免异常配置下分配上百 MB。双 8K 约 6600 万像素。
	if int64(vw)*int64(vh) > 80_000_000 {
		return nil, fmt.Errorf("screen too large to capture (%dx%d)", vw, vh)
	}

	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("GetDC failed: no interactive desktop session " +
			"(screen capture needs a logged-in desktop, not a service/session-0 context)")
	}
	defer procReleaseDC.Call(0, hdcScreen)

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(hdcMem)

	// 位图必须基于 screen DC 创建，否则色彩深度可能对不上。
	hbm, _, _ := procCreateCompatibleBitmap.Call(hdcScreen, uintptr(vw), uintptr(vh))
	if hbm == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(hbm)

	old, _, _ := procSelectObject.Call(hdcMem, hbm)
	// 负坐标（左侧/上方还有显示器）按 int32 截断传入，与 BitBlt 的 int 参数一致。
	ok, _, _ := procBitBlt.Call(hdcMem, 0, 0, uintptr(vw), uintptr(vh),
		hdcScreen, uintptr(int32(vx)), uintptr(int32(vy)), uintptr(srcCopy|captureBlt))
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt failed: the desktop may be locked or on the secure desktop")
	}

	// 光标：GetCursorInfo 给全局坐标，换算到截图坐标后绘制。
	cursorX, cursorY := -1, -1
	ci := cursorInfo{cbSize: uint32(unsafe.Sizeof(cursorInfo{}))}
	if r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r != 0 {
		cx, cy := int(ci.pt.X)-vx, int(ci.pt.Y)-vy
		cursorX, cursorY = cx, cy
		if ci.flags&cursorShowing != 0 && cx >= 0 && cy >= 0 && cx < vw && cy < vh {
			procDrawIconEx.Call(hdcMem, uintptr(cx), uintptr(cy), ci.hCursor,
				0, 0, 0, 0, uintptr(diNormal))
		}
	}

	// GetDIBits 要求位图未被任何 DC 选中。
	procSelectObject.Call(hdcMem, old)

	// 用正高度（自下而上的 DIB）而非负高度：部分显示驱动对负高度的支持
	// 不一致，一旦被忽略整张图会上下翻转，而这种错误很难从结果里看出来。
	bmi := bitmapInfo{header: bitmapInfoHeader{
		size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		width:       int32(vw),
		height:      int32(vh),
		planes:      1,
		bitCount:    32,
		compression: 0, // BI_RGB
	}}
	buf := make([]byte, vw*vh*4)
	lines, _, _ := procGetDIBits.Call(hdcMem, hbm, 0, uintptr(vh),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmi)), uintptr(dibRGBColors))
	if lines == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}

	// BGRA（自下而上）-> RGBA（自上而下），并补满 alpha
	// （GetDIBits 返回的 alpha 通道未定义，直接透传会得到全透明图像）。
	img := image.NewRGBA(image.Rect(0, 0, vw, vh))
	rowBytes := vw * 4
	for y := 0; y < vh; y++ {
		src := (vh - 1 - y) * rowBytes
		dst := y * rowBytes
		for x := 0; x < rowBytes; x += 4 {
			img.Pix[dst+x+0] = buf[src+x+2]
			img.Pix[dst+x+1] = buf[src+x+1]
			img.Pix[dst+x+2] = buf[src+x+0]
			img.Pix[dst+x+3] = 0xFF
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("png encode failed: %w", err)
	}

	return &captureResult{
		PNG:     out.Bytes(),
		Width:   vw,
		Height:  vh,
		CursorX: cursorX,
		CursorY: cursorY,
	}, nil
}

func newMouseInput(flags uint32, dx, dy int32, data uint32) winInput {
	in := winInput{typ: inputMouse}
	*(*mouseInput)(unsafe.Pointer(&in.data[0])) = mouseInput{
		dx: dx, dy: dy, flags: flags, mouseData: data,
	}
	return in
}

func newKeyboardInput(vk, scan uint16, flags uint32) winInput {
	in := winInput{typ: inputKeyboard}
	*(*keyboardInput)(unsafe.Pointer(&in.data[0])) = keyboardInput{
		vk: vk, scan: scan, flags: flags,
	}
	return in
}

// sendInput 注入一批事件，并校验实际注入数量。
//
// SendInput 在目标窗口完整性级别更高（以管理员身份运行）时只注入一部分
// 事件而不报错。把"部分注入"暴露出来，模型才不会以为动作成功了。
func sendInput(events []winInput) error {
	if len(events) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]),
	)
	if int(n) == len(events) {
		return nil
	}
	detail := ""
	// proc.Call 总是把 GetLastError 的结果装箱返回（0 也是非 nil 接口），
	// 所以必须显式比较 Errno 值，不能用 err != nil 判断。
	if e, ok := err.(syscall.Errno); ok && e != 0 {
		detail = ": " + e.Error()
	}
	return fmt.Errorf("SendInput injected %d of %d events%s — the target window may be "+
		"running elevated; run this app at the same privilege level", n, len(events), detail)
}

// absoluteMouse 把截图坐标换算成 SendInput 要求的 0..65535 归一化坐标。
// 带 VIRTUALDESK，多显示器时也能落到正确的屏幕上。
func absoluteMouse(x, y int) winInput {
	vx, vy, vw, vh := virtualScreen()
	nx, ny := int32(0), int32(0)
	if vw > 1 {
		nx = int32(int64(x-vx) * 65535 / int64(vw-1))
	}
	if vh > 1 {
		ny = int32(int64(y-vy) * 65535 / int64(vh-1))
	}
	if nx < 0 {
		nx = 0
	} else if nx > 65535 {
		nx = 65535
	}
	if ny < 0 {
		ny = 0
	} else if ny > 65535 {
		ny = 65535
	}
	return newMouseInput(mouseMove|mouseAbsolute|mouseVirtualDesk, nx, ny, 0)
}

func (d *winDriver) Move(ctx context.Context, x, y int) error {
	d.dpiAware()
	if err := ctx.Err(); err != nil {
		return err
	}
	return sendInput([]winInput{absoluteMouse(x, y)})
}

func (d *winDriver) Click(ctx context.Context, x, y int, btn mouseButton, count int) error {
	d.dpiAware()
	if err := ctx.Err(); err != nil {
		return err
	}
	if count < 1 {
		count = 1
	}

	var down, up uint32
	switch btn {
	case buttonRight:
		down, up = mouseRightDown, mouseRightUp
	case buttonMiddle:
		down, up = mouseMiddleDown, mouseMiddleUp
	default:
		down, up = mouseLeftDown, mouseLeftUp
	}

	if err := sendInput([]winInput{absoluteMouse(x, y)}); err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		if err := sendInput([]winInput{
			newMouseInput(down, 0, 0, 0),
			newMouseInput(up, 0, 0, 0),
		}); err != nil {
			return err
		}
		// 双击需要落在系统的双击时限内，但两次之间仍要留出极小间隔，
		// 否则部分应用会把两次 press 合并成一次。
		if i < count-1 {
			time.Sleep(30 * time.Millisecond)
		}
	}
	return nil
}

func (d *winDriver) Drag(ctx context.Context, fromX, fromY, toX, toY int) error {
	d.dpiAware()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sendInput([]winInput{
		absoluteMouse(fromX, fromY),
		newMouseInput(mouseLeftDown, 0, 0, 0),
	}); err != nil {
		return err
	}
	// 分步移动：一次性跳到终点时，画布/拖拽排序类程序收不到 dragover，
	// 会表现为"拖了但没动"。
	const steps = 8
	for i := 1; i <= steps; i++ {
		x := fromX + (toX-fromX)*i/steps
		y := fromY + (toY-fromY)*i/steps
		if err := sendInput([]winInput{absoluteMouse(x, y)}); err != nil {
			// 松开按键，避免把鼠标留在按下状态。
			sendInput([]winInput{newMouseInput(mouseLeftUp, 0, 0, 0)})
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sendInput([]winInput{newMouseInput(mouseLeftUp, 0, 0, 0)})
}

// inputChunk 限制单次 SendInput 的事件数，避免过长数组被系统丢弃。
const inputChunk = 128

func (d *winDriver) TypeText(ctx context.Context, text string) error {
	d.dpiAware()
	if text == "" {
		return nil
	}

	events := make([]winInput, 0, inputChunk)
	flush := func() error {
		if len(events) == 0 {
			return nil
		}
		err := sendInput(events)
		events = events[:0]
		return err
	}

	for _, r := range text {
		if err := ctx.Err(); err != nil {
			return err
		}
		// 控制字符必须走真实按键：Unicode 注入对 \n / \t 无效。
		switch r {
		case '\n', '\r':
			events = append(events,
				newKeyboardInput(vkReturn, 0, 0),
				newKeyboardInput(vkReturn, 0, keyEventKeyUp))
		case '\t':
			events = append(events,
				newKeyboardInput(vkTab, 0, 0),
				newKeyboardInput(vkTab, 0, keyEventKeyUp))
		default:
			// 逐 UTF-16 code unit 注入，emoji/生僻字（代理对）也能正确输入。
			for _, unit := range utf16.Encode([]rune{r}) {
				events = append(events,
					newKeyboardInput(0, unit, keyEventUnicode),
					newKeyboardInput(0, unit, keyEventUnicode|keyEventKeyUp))
			}
		}
		if len(events) >= inputChunk {
			if err := flush(); err != nil {
				return err
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return flush()
}

func vkForModifier(name string) (uint16, bool) {
	switch name {
	case "ctrl":
		return vkControl, true
	case "alt":
		return vkMenu, true
	case "shift":
		return vkShift, true
	case "cmd", "win":
		return vkLWin, true
	}
	return 0, false
}

func (d *winDriver) Key(ctx context.Context, combo string) error {
	d.dpiAware()
	if err := ctx.Err(); err != nil {
		return err
	}
	kc, err := parseKeyCombo(combo)
	if err != nil {
		return err
	}

	// 无修饰键的单字符：直接走 Unicode 注入，与键盘布局无关
	// （德语键盘 y/z 互换、法语 AZERTY 都能输入正确的字符）。
	if len(kc.Modifiers) == 0 && len([]rune(kc.Key)) == 1 {
		return d.TypeText(ctx, kc.Key)
	}

	var vk uint16
	needShift := false
	if code, ok := vkForName[kc.Key]; ok {
		vk = code
	} else if n, ok := parseFnKey(kc.Key); ok {
		vk = uint16(vkF1 + n - 1)
	} else {
		// 单字符或需要 shift 的符号：交给系统按当前布局翻译。
		r := []rune(kc.Key)
		if len(r) != 1 {
			return fmt.Errorf("unsupported key %q", kc.Key)
		}
		ch := uint16(r[0])
		if r[0] >= 'a' && r[0] <= 'z' {
			// VkKeyScanW 用小写字母查表时可能返回需要 shift 的结果，
			// 统一换成大写字母以避免多余的 shift。
			ch = uint16(r[0] - 'a' + 'A')
		}
		res, _, _ := procVkKeyScanW.Call(uintptr(ch))
		if int16(res) == -1 {
			return fmt.Errorf("key %q cannot be produced by the current keyboard layout", kc.Key)
		}
		vk = uint16(byte(res))
		needShift = byte(res>>8)&1 != 0
	}

	downs := make([]uint16, 0, len(kc.Modifiers)+2)
	for _, m := range kc.Modifiers {
		code, ok := vkForModifier(m)
		if !ok {
			return fmt.Errorf("unsupported modifier %q", m)
		}
		downs = append(downs, code)
	}
	if needShift && !hasModifier(kc.Modifiers, "shift") {
		downs = append(downs, vkShift)
	}

	events := make([]winInput, 0, len(downs)*2+2)
	for _, code := range downs {
		events = append(events, newKeyboardInput(code, 0, 0))
	}
	events = append(events,
		newKeyboardInput(vk, 0, 0),
		newKeyboardInput(vk, 0, keyEventKeyUp))
	// 修饰键逆序释放：先按的 ctrl 最后松，避免中间态触发其它快捷键。
	for i := len(downs) - 1; i >= 0; i-- {
		events = append(events, newKeyboardInput(downs[i], 0, keyEventKeyUp))
	}
	return sendInput(events)
}

func (d *winDriver) Scroll(ctx context.Context, x, y, amount int) error {
	d.dpiAware()
	if err := ctx.Err(); err != nil {
		return err
	}
	if amount == 0 {
		return nil
	}
	// 先把光标落到目标位置：滚轮事件作用于光标下的窗口，
	// 不先移动就会滚到别的窗口上。
	if err := sendInput([]winInput{absoluteMouse(x, y)}); err != nil {
		return err
	}

	// 正数向下 = 内容上移 = 负的 wheel delta（Win32 里正 delta 是向上）。
	delta := -amount * wheelDelta
	steps := (absInt(amount) + 2) / 3 // 每步 3 格
	if steps < 1 {
		steps = 1
	}
	perStep := int32(delta / steps)
	acc := delta
	for i := 0; i < steps; i++ {
		d := perStep
		if i == steps-1 {
			d = int32(acc) // 最后一步补齐余数，保证总量精确
		}
		acc -= int(d)
		if err := sendInput([]winInput{newMouseInput(mouseWheel, 0, 0, uint32(d))}); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}
