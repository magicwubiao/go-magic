package tool

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// 回归：浏览器默认必须无头。
//
// 背景：服务器（容器 / 云主机 / CI / systemd）上没有显示服务，有头 Chrome 会
// 直接死在 "cannot open display"，而 agent 看不见这个报错原因（它只能调工具）。
// 所以"什么都没配"的动态必须是 headless=true，这是本文件所有断言的基准。
func TestBrowserHeadlessDefaultsToTrue(t *testing.T) {
	t.Run("没配置文件也不设环境变量 → 无头", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m"}`)
		t.Setenv("BROWSER_HEADLESS", "")

		bm := &BrowserManager{}
		if got, src := bm.resolveHeadless(); !got {
			t.Errorf("默认必须无头，得到 headless=false（来源 %s）", src)
		}
	})

	t.Run("环境变量优先于配置文件", func(t *testing.T) {
		// 配置说要开窗，但环境变量强制无头 → 环境变量赢。
		isolateConfig(t, `{"provider":"deepseek","model":"m","browser_headless":false}`)
		t.Setenv("BROWSER_HEADLESS", "true")

		bm := &BrowserManager{}
		got, src := bm.resolveHeadless()
		if !got {
			t.Errorf("环境变量应覆盖配置，得到 headless=false（来源 %s）", src)
		}
		if src != config.BrowserHeadlessFromEnv {
			t.Errorf("取值来源 = %q, want %q", src, config.BrowserHeadlessFromEnv)
		}
	})

	// 显式关掉无头是唯一能让有头窗口出来的开关，必须真的有效——
	// 否则"人工登录"这条路就彻底断了。
	t.Run("配置显式 false → 有头（且来源正确）", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m","browser_headless":false}`)
		t.Setenv("BROWSER_HEADLESS", "")

		bm := &BrowserManager{}
		got, src := bm.resolveHeadless()
		if got {
			t.Errorf("显式 browser_headless=false 应开有头窗口，得到 headless=true（来源 %s）", src)
		}
		if src != config.BrowserHeadlessFromConfig {
			t.Errorf("取值来源 = %q, want %q", src, config.BrowserHeadlessFromConfig)
		}
	})

	t.Run("配置显式 true → 无头", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m","browser_headless":true}`)
		t.Setenv("BROWSER_HEADLESS", "")

		bm := &BrowserManager{}
		if got, _ := bm.resolveHeadless(); !got {
			t.Error("显式 browser_headless=true 应为无头")
		}
	})

	// `BROWSER_HEADLESS=` / `docker run -e BROWSER_HEADLESS`（不带值）在 shell 脚本里
	// 很常见，把它当成"显式开启"会让用户莫名其妙地开不了有头窗口。
	t.Run("空环境变量等同没设，不覆盖配置", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m","browser_headless":false}`)
		t.Setenv("BROWSER_HEADLESS", "  ")

		bm := &BrowserManager{}
		got, src := bm.resolveHeadless()
		if got {
			t.Errorf("空白环境变量不该被当成开启，得到 headless=true（来源 %s）", src)
		}
		if src != config.BrowserHeadlessFromConfig {
			t.Errorf("取值来源 = %q, want %q（应回落到配置）", src, config.BrowserHeadlessFromConfig)
		}
	})

	// 环境变量写成 "false"/"0"/"no"/"off" 都要认。旧实现只判 `== "true"`，
	// 于是 `BROWSER_HEADLESS=false` 被静默当成"没设"→ 反而回到无头，
	// 用户会以为自己的开关坏了。
	t.Run("BROWSER_HEADLESS=false 真的能关掉无头", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m"}`)
		for _, v := range []string{"false", "FALSE", "0", "no", "off", " false "} {
			t.Setenv("BROWSER_HEADLESS", v)
			bm := &BrowserManager{}
			if got, _ := bm.resolveHeadless(); got {
				t.Errorf("BROWSER_HEADLESS=%q 应关闭无头，得到 headless=true", v)
			}
		}
	})

	t.Run("无配置文件时环境变量仍生效", func(t *testing.T) {
		// GO_MAGIC_HOME 指向不存在的目录 → Load 返回 ErrNoConfig + 默认配置，
		// Tool 侧必须自己兜住环境变量。
		t.Setenv("GO_MAGIC_HOME", t.TempDir())
		t.Setenv("BROWSER_HEADLESS", "false")

		bm := &BrowserManager{}
		if got, _ := bm.resolveHeadless(); got {
			t.Error("无配置文件时 BROWSER_HEADLESS=false 仍应关闭无头")
		}
	})
}

// 回归：运行中的浏览器"实际用什么模式启动的"必须可查、且不受配置后续变化影响。
// 热更新靠这个判断要不要重启浏览器——报告错了要么白关一次浏览器，要么新模式
// 永远不生效。
func TestHeadlessEffectiveTracksAppliedValue(t *testing.T) {
	isolateConfig(t, `{"provider":"deepseek","model":"m"}`)
	t.Setenv("BROWSER_HEADLESS", "")

	t.Run("未启动时回落到生效配置值", func(t *testing.T) {
		bm := &BrowserManager{}
		if !bm.HeadlessEffective() {
			t.Error("未启动时应返回配置生效值（默认 true）")
		}
	})

	t.Run("已应用值优先于配置", func(t *testing.T) {
		bm := &BrowserManager{}
		// 模拟"浏览器已按有头启动"：allocCtx 非 nil 即视为已启动。
		// 这里不需要真正可用的 allocator——HeadlessEffective 只看它是否为 nil。
		bm.allocCtx = context.Background()
		bm.NoteHeadlessApplied(false)

		if bm.HeadlessEffective() {
			t.Error("已按有头启动时 HeadlessEffective() 应为 false")
		}
	})
}

// 回归：有头模式在没有显示服务时必须报错，且错误信息要能指出"这个 false 是哪来的"。
// 不报错就会退化回 Chrome 深处那句 "cannot open display"，排障成本极高。
func TestInitializeRejectsHeadedWithoutDisplay(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("Windows/macOS 恒有窗口系统，hasDisplay() 永远为 true，无法构造该场景")
	}
	if hasDisplay() {
		t.Skipf("当前环境有显示服务（DISPLAY/WAYLAND_DISPLAY 非空），无法构造无显示场景")
	}

	isolateConfig(t, `{"provider":"deepseek","model":"m","browser_headless":false}`)
	t.Setenv("BROWSER_HEADLESS", "")

	bm := &BrowserManager{}
	err := bm.Initialize()
	if err == nil {
		t.Fatal("无显示服务时初始化有头浏览器应报错")
	}
	// 错误信息里必须带上来源，否则用户不知道是环境变量还是配置文件写的 false。
	if !strings.Contains(err.Error(), string(config.BrowserHeadlessFromConfig)) {
		t.Errorf("错误信息应指出取值来源 %q，实际: %v", config.BrowserHeadlessFromConfig, err)
	}
}

// hasDisplay 的判定必须两头都成立：Wayland 会话只有 WAYLAND_DISPLAY，
// X 会话只有 DISPLAY，漏掉任一个都会把"其实有显示服务"误判成无显示。
func TestHasDisplayChecksBothXAndWayland(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("Windows/macOS 恒为 true")
	}

	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if hasDisplay() {
		t.Error("两个变量都为空时应判定为无显示")
	}

	t.Setenv("DISPLAY", ":0")
	if !hasDisplay() {
		t.Error("X 会话（DISPLAY 非空）应判定为有显示")
	}

	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if !hasDisplay() {
		t.Error("Wayland 会话（WAYLAND_DISPLAY 非空）应判定为有显示")
	}
}
