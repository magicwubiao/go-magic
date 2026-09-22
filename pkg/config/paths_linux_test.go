//go:build linux

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBrowserProfileDirUnderMagicHomeOnLinux 是 Linux 侧的口径断言：宝塔以 root
// 起服务时 HOME 可能是空的（systemd unit 未设 User/Environment、sudo 清过环境），
// 而 GO_MAGIC_HOME 指向站点目录。此时 Chrome 拿到的 profile 目录不允许带字面量
// `~`，否则会在站点目录下建出一个叫 `~` 的文件夹。
//
// 只在 Linux 上跑（本机 Windows 跳过，CI 会真跑）——本仓"平台相关断言两头都断言"
// 的惯例：HOME 优先、`~` 展开都是 POSIX 语义，Windows 走 USERPROFILE。
func TestBrowserProfileDirUnderMagicHomeOnLinux(t *testing.T) {
	magicHome := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", magicHome)

	writeConfig := func(t *testing.T, body string) {
		t.Helper()
		if err := os.MkdirAll(magicHome, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(magicHome, ConfigFileName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("有 HOME 时保持约定位置不动", func(t *testing.T) {
		// 默认值仍是 `~/.magic/browser-profile`（与文档/示例配置一致）：
		// 有 HOME 的环境里不该搬家，否则老部署的登录态会凭空消失。
		home := t.TempDir()
		t.Setenv("HOME", home)

		got := (&Config{}).GetBrowserProfileDir()
		if want := filepath.Join(home, ".magic", "browser-profile"); got != want {
			t.Errorf("GetBrowserProfileDir() = %q, want %q", got, want)
		}
	})

	t.Run("HOME 缺失时落到 magic home", func(t *testing.T) {
		t.Setenv("HOME", "")
		if h := userHomeDir(); h != "" {
			t.Skipf("本环境仍有主目录兜底 (%q)，复现不出 HOME 缺失", h)
		}

		writeConfig(t, `{"provider":"deepseek","model":"m","browser_profile_dir":"~/.magic/browser-profile"}`)
		cfg, err := Load()
		if err != nil && err != ErrNoConfig {
			t.Fatalf("Load: %v", err)
		}

		got := cfg.GetBrowserProfileDir()
		if want := filepath.Join(magicHome, "browser-profile"); got != want {
			t.Errorf("HOME 缺失时 = %q, want %q（应落到站点 magic home）", got, want)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("必须是绝对路径: %q", got)
		}
	})
}
