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
	// 登录态跨会话持久。
	t.Run("配置缺键回落到默认目录", func(t *testing.T) {
		isolateConfig(t, `{"provider":"deepseek","model":"m"}`)

		bm := &BrowserManager{}
		got := bm.resolveProfileDir()
		if want := filepath.Join(home, ".magic", "browser-profile"); got != want {
			t.Errorf("resolveProfileDir() = %q, want %q", got, want)
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
