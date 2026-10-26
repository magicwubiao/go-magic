package tool

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDriver 记录收到的调用，用来验证工具层的坐标换算、互斥与错误处理。
// 它不触碰真实桌面，因此这些测试可以在任何 CI 上跑。
type fakeDriver struct {
	mu sync.Mutex

	availErr error
	scaleX   float64
	scaleY   float64

	captureErr error

	moves    [][2]int
	clicks   []fakeClick
	drags    [][4]int
	types    []string
	keys     []string
	scrolls  []fakeScroll
	captures int

	// onClick 在 Click 内被调用，测试可借此阻塞以验证互斥。
	onClick func()
}

type fakeClick struct {
	X, Y  int
	Btn   mouseButton
	Count int
}

type fakeScroll struct {
	X, Y, Amount int
}

func (f *fakeDriver) Available() error { return f.availErr }

func (f *fakeDriver) Scale() (float64, float64) {
	if f.scaleX == 0 {
		return 1, 1
	}
	return f.scaleX, f.scaleY
}

func (f *fakeDriver) Capture(ctx context.Context) (*captureResult, error) {
	f.mu.Lock()
	f.captures++
	f.mu.Unlock()
	if f.captureErr != nil {
		return nil, f.captureErr
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &captureResult{PNG: buf.Bytes(), Width: 1600, Height: 900, CursorX: 10, CursorY: 20}, nil
}

func (f *fakeDriver) Move(ctx context.Context, x, y int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, [2]int{x, y})
	return nil
}

func (f *fakeDriver) Click(ctx context.Context, x, y int, btn mouseButton, count int) error {
	f.mu.Lock()
	f.clicks = append(f.clicks, fakeClick{X: x, Y: y, Btn: btn, Count: count})
	hook := f.onClick
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (f *fakeDriver) Drag(ctx context.Context, x1, y1, x2, y2 int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drags = append(f.drags, [4]int{x1, y1, x2, y2})
	return nil
}

func (f *fakeDriver) TypeText(ctx context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.types = append(f.types, text)
	return nil
}

func (f *fakeDriver) Key(ctx context.Context, combo string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, combo)
	return nil
}

func (f *fakeDriver) Scroll(ctx context.Context, x, y, amount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scrolls = append(f.scrolls, fakeScroll{X: x, Y: y, Amount: amount})
	return nil
}

func newTestTool(t *testing.T, d *fakeDriver) *ComputerUseTool {
	t.Helper()
	tool := NewComputerUseTool(t.TempDir())
	tool.driver = d
	return tool
}

// --- 按键解析 ---

func TestParseKeyCombo(t *testing.T) {
	cases := []struct {
		in      string
		wantKey string
		wantMod []string
		wantErr bool
	}{
		{in: "enter", wantKey: "enter"},
		{in: "Return", wantKey: "enter"},
		{in: "ESC", wantKey: "esc"},
		{in: "ctrl+c", wantKey: "c", wantMod: []string{"ctrl"}},
		{in: "Ctrl+Shift+T", wantKey: "t", wantMod: []string{"ctrl", "shift"}},
		{in: "cmd+space", wantKey: "space", wantMod: []string{"cmd"}},
		{in: "option+tab", wantKey: "tab", wantMod: []string{"alt"}},
		{in: "f5", wantKey: "f5"},
		{in: "f24", wantKey: "f24"},
		{in: "pgdn", wantKey: "pagedown"},
		{in: "ctrl+ctrl+a", wantKey: "a", wantMod: []string{"ctrl"}}, // 重复修饰键忽略
		{in: "", wantErr: true},
		{in: "ctrl+", wantErr: true},
		{in: "ctrl+nosuchkey", wantErr: true},
		{in: "hyper+a", wantErr: true},
		{in: "f99", wantErr: true},
		{in: "中文", wantErr: true},
	}
	for _, c := range cases {
		got, err := parseKeyCombo(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseKeyCombo(%q) = %v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseKeyCombo(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got.Key != c.wantKey {
			t.Errorf("parseKeyCombo(%q).Key = %q, want %q", c.in, got.Key, c.wantKey)
		}
		if strings.Join(got.Modifiers, ",") != strings.Join(c.wantMod, ",") {
			t.Errorf("parseKeyCombo(%q).Modifiers = %v, want %v", c.in, got.Modifiers, c.wantMod)
		}
	}
}

// --- 参数校验 ---

func TestValidateParams(t *testing.T) {
	tool := newTestTool(t, &fakeDriver{})
	cases := []struct {
		name    string
		params  map[string]interface{}
		wantErr bool
	}{
		{"screenshot ok", map[string]interface{}{"action": "screenshot"}, false},
		{"missing action", map[string]interface{}{}, true},
		{"unknown action", map[string]interface{}{"action": "explode"}, true},
		{"click ok", map[string]interface{}{"action": "click", "x": float64(10), "y": float64(20)}, false},
		{"click at origin ok", map[string]interface{}{"action": "click", "x": float64(0), "y": float64(0)}, false},
		{"click missing y", map[string]interface{}{"action": "click", "x": float64(10)}, true},
		{"move ok", map[string]interface{}{"action": "move", "x": 5, "y": 6}, false},
		{"drag ok", map[string]interface{}{
			"action": "drag", "x": 1, "y": 2, "to_x": 3, "to_y": 4}, false},
		{"drag missing to", map[string]interface{}{
			"action": "drag", "x": 1, "y": 2}, true},
		{"type ok", map[string]interface{}{"action": "type", "text": "hello"}, false},
		{"type empty", map[string]interface{}{"action": "type", "text": ""}, true},
		{"key ok", map[string]interface{}{"action": "key", "key": "ctrl+c"}, false},
		{"key invalid", map[string]interface{}{"action": "key", "key": "ctrl+!!"}, true},
		{"scroll too far", map[string]interface{}{
			"action": "scroll", "x": 1, "y": 1, "scroll_y": float64(999999)}, true},
		{"click count too big", map[string]interface{}{
			"action": "click", "x": 1, "y": 1, "count": float64(9)}, true},
		{"bad button", map[string]interface{}{
			"action": "click", "x": 1, "y": 1, "button": "thumb"}, true},
		{"type too long", map[string]interface{}{
			"action": "type", "text": strings.Repeat("x", maxTypeTextLen+1)}, true},
	}
	for _, c := range cases {
		err := tool.ValidateParams(c.params)
		if c.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
	}
}

// --- 坐标换算 ---

func TestCoordinateScaling(t *testing.T) {
	// 模拟 Retina：截图是输入坐标的 2 倍。
	d := &fakeDriver{scaleX: 2, scaleY: 2}
	tool := newTestTool(t, d)

	if _, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "click", "x": float64(400), "y": float64(300),
		"observe": false,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(d.clicks) != 1 {
		t.Fatalf("expected 1 click, got %d", len(d.clicks))
	}
	if got := d.clicks[0]; got.X != 200 || got.Y != 150 {
		t.Errorf("click coords = (%d,%d), want (200,150) after 2x scaling", got.X, got.Y)
	}
}

func TestCoordinateScalingDragAndScroll(t *testing.T) {
	d := &fakeDriver{scaleX: 1.5, scaleY: 1.5}
	tool := newTestTool(t, d)

	if _, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "drag", "x": float64(150), "y": float64(150),
		"to_x": float64(300), "to_y": float64(600), "observe": false,
	}); err != nil {
		t.Fatalf("drag: %v", err)
	}
	want := [4]int{100, 100, 200, 400}
	if len(d.drags) != 1 || d.drags[0] != want {
		t.Errorf("drag = %v, want %v", d.drags, want)
	}

	if _, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "scroll", "x": float64(150), "y": float64(150),
		"scroll_y": float64(3), "observe": false,
	}); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if len(d.scrolls) != 1 || d.scrolls[0].Amount != 3 || d.scrolls[0].X != 100 {
		t.Errorf("scroll = %+v, want x=100 amount=3", d.scrolls)
	}
}

// --- 可用性 ---

func TestExecuteRejectsUnavailableDriver(t *testing.T) {
	d := &fakeDriver{availErr: ErrDesktopUnavailable}
	tool := newTestTool(t, d)

	_, err := tool.Execute(context.Background(), map[string]interface{}{"action": "screenshot"})
	if err == nil {
		t.Fatal("expected error when driver is unavailable")
	}
	if !errors.Is(err, ErrDesktopUnavailable) {
		t.Errorf("expected ErrDesktopUnavailable, got %v", err)
	}
	if tool.Available() {
		t.Error("Available() should be false when the driver reports unavailable")
	}
}

func TestAvailableWhenDriverOK(t *testing.T) {
	tool := newTestTool(t, &fakeDriver{})
	if !tool.Available() {
		t.Error("Available() should be true when the driver has no error")
	}
}

// --- 截图落盘 ---

func TestScreenshotPersistence(t *testing.T) {
	dir := t.TempDir()
	tool := NewComputerUseTool(dir)
	tool.driver = &fakeDriver{}

	out, err := tool.Execute(context.Background(), map[string]interface{}{"action": "screenshot"})
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	res, ok := out.(*computerUseResult)
	if !ok {
		t.Fatalf("unexpected result type %T", out)
	}
	if res.Width != 1600 || res.Height != 900 {
		t.Errorf("size = %dx%d, want 1600x900", res.Width, res.Height)
	}
	if res.Cursor != "10,20" {
		t.Errorf("cursor = %q, want \"10,20\"", res.Cursor)
	}
	if !strings.HasPrefix(filepath.Base(res.Screenshot), "desktop_") {
		t.Errorf("filename = %q, want desktop_ prefix", res.Screenshot)
	}
	if !strings.HasSuffix(res.Screenshot, ".png") {
		t.Errorf("filename = %q, want .png suffix", res.Screenshot)
	}
	if st, err := os.Stat(res.Screenshot); err != nil || st.Size() == 0 {
		t.Errorf("screenshot file missing or empty: %v", err)
	}
	if res.Hint == "" {
		t.Error("hint should explain how to inspect the screenshot")
	}
}

// 两次截图不能落到同一个文件名（旧实现固定写 screenshot.png，并发会话互相覆盖）。
func TestScreenshotFilenamesAreUnique(t *testing.T) {
	dir := t.TempDir()
	tool := NewComputerUseTool(dir)
	tool.driver = &fakeDriver{}

	first, err := tool.Execute(context.Background(), map[string]interface{}{"action": "screenshot"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := tool.Execute(context.Background(), map[string]interface{}{"action": "screenshot"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	a := first.(*computerUseResult).Screenshot
	b := second.(*computerUseResult).Screenshot
	if a == b {
		t.Errorf("two captures wrote the same file %q", a)
	}
}

// --- 动作后的观察截图 ---

func TestObserveDefaultCapturesAfterAction(t *testing.T) {
	d := &fakeDriver{}
	tool := newTestTool(t, d)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "type", "text": "hello",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res := out.(*computerUseResult)
	if d.captures != 1 {
		t.Errorf("captures = %d, want 1 (observe defaults to true)", d.captures)
	}
	if res.Screenshot == "" {
		t.Error("expected a screenshot path when observing")
	}

	// observe=false 时不截图，但要提示模型自己确认。
	d2 := &fakeDriver{}
	tool2 := newTestTool(t, d2)
	out2, err := tool2.Execute(context.Background(), map[string]interface{}{
		"action": "type", "text": "x", "observe": false,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if d2.captures != 0 {
		t.Errorf("captures = %d, want 0 with observe=false", d2.captures)
	}
	if out2.(*computerUseResult).Hint == "" {
		t.Error("expected a hint when not observing")
	}
}

// 动作已生效但截图失败时，不能把整次调用判成失败——否则模型会重复执行动作。
func TestCaptureFailureAfterActionIsReportedNotFatal(t *testing.T) {
	d := &fakeDriver{captureErr: errors.New("screen locked")}
	tool := newTestTool(t, d)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "click", "x": float64(1), "y": float64(1),
	})
	if err != nil {
		t.Fatalf("click should not fail because of the follow-up screenshot: %v", err)
	}
	res := out.(*computerUseResult)
	if res.Success {
		t.Error("result should be marked unsuccessful when verification failed")
	}
	if !strings.Contains(res.Error, "screen locked") {
		t.Errorf("error should carry the capture failure, got %q", res.Error)
	}
	if len(d.clicks) != 1 {
		t.Errorf("action should have been performed once, got %d", len(d.clicks))
	}
}

// --- 并发互斥 ---

func TestConcurrentActionsAreSerialized(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	d := &fakeDriver{onClick: func() {
		once.Do(func() { close(entered) })
		<-release
	}}
	tool := newTestTool(t, d)

	done := make(chan error, 1)
	go func() {
		_, err := tool.Execute(context.Background(), map[string]interface{}{
			"action": "click", "x": float64(1), "y": float64(1), "observe": false,
		})
		done <- err
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first action never reached the driver")
	}

	// 第二个动作应该被拒绝，而不是与第一个交错注入输入。
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "move", "x": float64(2), "y": float64(2), "observe": false,
	})
	if err == nil {
		t.Error("second concurrent action should be rejected while the first is running")
	} else if !strings.Contains(err.Error(), "another computer_use action") {
		t.Errorf("unexpected error: %v", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Errorf("first action failed: %v", err)
	}
}

// --- 注册与工具清单 ---

func TestComputerUseRegistered(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(t.TempDir())
	if !r.HasTool("computer_use") {
		t.Fatal("computer_use should be registered by RegisterAll")
	}
	if got := r.GetTimeout("computer_use"); got <= 0 {
		t.Errorf("computer_use timeout = %v, want a positive value", got)
	}
}
