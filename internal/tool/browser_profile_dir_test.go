package tool

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testUserHome 复刻生产代码里 `~` 的解析口径：类 Unix 看 HOME，
// Windows 看 USERPROFILE（os.UserHomeDir）。
func testUserHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		if h := os.Getenv("HOME"); h != "" {
			return h
		}
	}
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		t.Skipf("拿不到用户主目录: %v", err)
	}
	return h
}

// 回归：配置 `~/.magic/browser-profile` 时，浏览器 profile 目录必须是展开后的
// 绝对路径——否则 Chrome 的 --user-data-dir 会被当成 CWD 下的相对目录，
// 在安装目录里建出一个名为 `~` 的字面量文件夹。
func TestBrowserProfileDirExpandsTilde(t *testing.T) {
	home := testUserHome(t)

	t.Run("SetProfileDir 展开", func(t *testing.T) {
		bm := &BrowserManager{}
		bm.SetProfileDir("~/.magic/browser-profile")

		got := bm.ProfileDir()
		if strings.HasPrefix(got, "~") {
			t.Fatalf("ProfileDir() 仍是字面量波浪号: %q", got)
		}
		if want := filepath.Join(home, ".magic", "browser-profile"); got != want {
			t.Errorf("ProfileDir() = %q, want %q", got, want)
		}
	})

	t.Run("BROWSER_PROFILE_DIR 环境变量展开", func(t *testing.T) {
		bm := &BrowserManager{}
		t.Setenv("BROWSER_PROFILE_DIR", "~/.magic/bp-env")

		got := bm.resolveProfileDir()
		if want := filepath.Join(home, ".magic", "bp-env"); got != want {
			t.Errorf("resolveProfileDir() = %q, want %q", got, want)
		}
	})

	t.Run("配置文件来源展开", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m","browser_profile_dir":"~/.magic/bp-config"}`)

		bm := &BrowserManager{}
		got := bm.resolveProfileDir()
		if want := filepath.Join(home, ".magic", "bp-config"); got != want {
			t.Errorf("resolveProfileDir() = %q, want %q", got, want)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("resolveProfileDir() 必须是绝对路径，得到 %q", got)
		}
	})

	// 配置里没写 browser_profile_dir（老配置文件 / 全新安装）→ 用默认目录，
	// 登录态跨会话持久。口径与 pkg/config 的 defaultBrowserProfileDirAbs 一致：
	// **有真实主目录就仍是 `~/.magic/browser-profile`**（老部署不能搬家），
	// 拿不到主目录时才回落到 magic home。
	t.Run("配置缺键回落到默认目录", func(t *testing.T) {
		magicHome := isolateConfig(t, `{"provider":"deepseek","model":"m"}`)

		want := filepath.Join(home, ".magic", "browser-profile")
		if os.Getenv("HOME") == "" && runtime.GOOS != "windows" {
			// 只在"类 Unix 且 HOME 缺失"这一支才该落到 magic home。
			want = filepath.Join(magicHome, "browser-profile")
		}

		bm := &BrowserManager{}
		got := bm.resolveProfileDir()
		if got != want {
			t.Errorf("resolveProfileDir() = %q, want %q", got, want)
		}
		if strings.Contains(got, "~") {
			t.Errorf("默认 profile 目录不得含字面量 `~`: %q", got)
		}
	})

	// 显式写 "" 才是"每次全新临时 profile"的开关。
	t.Run("显式空串保留临时 profile 语义", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m","browser_profile_dir":""}`)

		bm := &BrowserManager{}
		if got := bm.resolveProfileDir(); got != "" {
			t.Errorf("显式空串应返回空（=临时 profile），得到 %q", got)
		}
	})
}

// TestBrowserProfileDirNeverLiteralTilde 钉住一条部署约束：无论 profile 目录是从
// 配置、环境变量还是默认值来的，最终喂给 Chrome 的 `--user-data-dir` 都不能带
// 字面量 `~`。
//
// 触发场景（实测报障形态）：宝塔/面板类部署把站点放在
// `/www/wwwroot/ai.magictech.cc`，服务以 root 身份运行。若此时 `HOME` 缺失
// （面板守护、systemd unit 未设 User/Environment、`sudo` 清过环境），`~` 就无处
// 可展开，`~/.magic/browser-profile` 会被 Chrome 当成**相对路径**——于是一个名叫
// `~` 的目录出现在 CWD（也就是站点根目录）下，看起来就是
// `/www/wwwroot/ai.magictech.cc/~/.magic/browser-profile`。
//
// 正确做法是显式给一个绝对路径（配置里写 `browser_profile_dir`，或设
// GO_MAGIC_HOME / BROWSER_PROFILE_DIR），而不是指望 `~` 在服务环境里能展开。
func TestBrowserProfileDirNeverLiteralTilde(t *testing.T) {
	t.Run("HOME 缺失时也不得解析出字面量波浪号", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m","browser_profile_dir":"~/.magic/browser-profile"}`)

		// 模拟守护进程环境：HOME 不在。Windows 上 `~` 走 USERPROFILE，
		// os.UserHomeDir 有系统调用兜底，所以这条断言只在类 Unix 上有意义。
		if runtime.GOOS != "windows" {
			t.Setenv("HOME", "")
			if home, err := os.UserHomeDir(); err != nil || home == "" {
				bm := &BrowserManager{}
				got := bm.resolveProfileDir()
				if strings.HasPrefix(got, "~") {
					t.Fatalf("HOME 缺失且无法兜底时，解析结果仍是字面量波浪号 %q——"+
						"Chrome 会把它当相对路径，在 CWD 下建出 `~` 目录", got)
				}
			}
		}

		// 显式绝对路径（推荐部署写法）必须原样透传，不带任何 `~`。
		abs := filepath.Join(string(filepath.Separator), "www", "wwwroot", "ai.magictech.cc", ".magic", "browser-profile")
		t.Setenv("GO_MAGIC_HOME", t.TempDir())
		bm := &BrowserManager{}
		bm.SetProfileDir(abs)
		if got := bm.ProfileDir(); got != abs {
			t.Errorf("ProfileDir() = %q, want 原样透传的绝对路径 %q", got, abs)
		}
	})

	// 路径最终会被 os.MkdirAll 创建：绝对路径必须落在给定位置，不能是
	// 「CWD 下的 `~`」这种看着像家目录、其实是站点目录的东西。
	t.Run("绝对路径解析后不引入波浪号", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m"}`)

		got := (&BrowserManager{}).resolveProfileDir()
		if strings.Contains(got, "~") {
			t.Fatalf("默认目录解析结果含 `~`: %q", got)
		}
		if !filepath.IsAbs(got) {
			t.Fatalf("默认目录解析结果不是绝对路径: %q", got)
		}
	})
}

// isolateConfig 把 GO_MAGIC_HOME 指到临时目录并写入 body，同时清空
// BROWSER_PROFILE_DIR，确保解析结果只来自配置文件。
func isolateConfig(t *testing.T, body string) string {
	t.Helper()
	magicHome := filepath.Join(t.TempDir(), ".magic")
	if err := os.MkdirAll(magicHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(magicHome, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_MAGIC_HOME", magicHome)
	t.Setenv("BROWSER_PROFILE_DIR", "")
	return magicHome
}
