package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestExpandHome 覆盖 `~` 展开的全部分支。期望值一律用 filepath.Join 拼，
// 保证 Windows / 类 Unix 两头都成立（本机 Windows 用 USERPROFILE，
// CI Linux 用 HOME）。
func TestExpandHome(t *testing.T) {
	home := userHomeDir()
	if home == "" {
		t.Fatal("userHomeDir() 返回空，测试环境异常")
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空字符串原样", "", ""},
		{"相对路径不动", "workspace/foo", "workspace/foo"},
		{"绝对路径不动", filepath.Join(string(filepath.Separator), "tmp", "x"), filepath.Join(string(filepath.Separator), "tmp", "x")},
		{"波浪号本身替换为主目录", "~", home},
		{"斜杠形式", "~/.magic/browser-profile", filepath.Join(home, ".magic", "browser-profile")},
		{"反斜杠形式", `~\.magic\browser-profile`, filepath.Join(home, ".magic", "browser-profile")},
		{"尾部斜杠", "~/.magic/", filepath.Join(home, ".magic")},
		{"非分隔符紧跟波浪号不展开", "~abc/x", "~abc/x"},
		{"~user 形式不展开", "~otheruser/.magic", "~otheruser/.magic"},
		{"中间出现的波浪号不动", "/tmp/~x", "/tmp/~x"},
	}

	for _, c := range cases {
		if got := ExpandHome(c.in); got != c.want {
			t.Errorf("%s: ExpandHome(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestExpandHomeUsesHomeEnv 只在类 Unix 上验证 HOME 优先——Windows 刻意不读
// HOME（Git Bash 会把它设成 /c/Users/xxx 这种 POSIX 路径，原生进程里拼出来是错的）。
func TestExpandHomeUsesHomeEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 ~ 由 USERPROFILE 决定，忽略 HOME")
	}
	fake := t.TempDir()
	t.Setenv("HOME", fake)

	if got, want := ExpandHome("~/.magic"), filepath.Join(fake, ".magic"); got != want {
		t.Errorf("ExpandHome(\"~/.magic\") = %q, want %q", got, want)
	}
}

// TestExpandHomeWindowsIgnoresPOSIXHome 是上一条的反向断言：Windows 上即便
// HOME 是 POSIX 风格路径，也不能拿它拼目录（否则会得到 `\c\Users\...`）。
func TestExpandHomeWindowsIgnoresPOSIXHome(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅 Windows 相关")
	}
	t.Setenv("HOME", "/c/Users/should-not-be-used")

	got := ExpandHome("~/.magic")
	if got == filepath.Join("/c/Users/should-not-be-used", ".magic") {
		t.Fatalf("Windows 上误用了 POSIX 风格的 HOME: %q", got)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("展开结果应是绝对路径，得到 %q", got)
	}
	if _, err := os.Stat(filepath.Dir(got)); err != nil {
		t.Fatalf("展开出的主目录不存在: %q (%v)", got, err)
	}
}

// TestLoadExpandsTildePaths 是这条 bug 的回归：config.json 里写
// `~/.magic/browser-profile` 时，Load 出来的路径必须已经展开成绝对路径，
// 否则浏览器会在进程 CWD（打包安装目录）下建出一个名为 `~` 的目录。
// 用 GO_MAGIC_HOME 隔离，绝不碰真实 ~/.magic。
func TestLoadExpandsTildePaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".magic")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
	  "provider": "deepseek",
	  "model": "m",
	  "working_dir": "~/w",
	  "browser_profile_dir": "~/.magic/browser-profile",
	  "memory": {"enabled": true, "db_path": "~/mem.db"}
	}`
	if err := os.WriteFile(filepath.Join(home, ConfigFileName), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_MAGIC_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	userHome := userHomeDir()
	checks := map[string]string{
		"working_dir":         cfg.WorkingDir,
		"browser_profile_dir": cfg.GetBrowserProfileDir(),
	}
	if cfg.Memory.DBPath != nil {
		checks["memory.db_path"] = *cfg.Memory.DBPath
	} else {
		t.Error("memory.db_path 未保留")
	}

	for name, got := range checks {
		if strings.HasPrefix(got, "~") {
			t.Errorf("%s 仍是字面量波浪号路径: %q（会在 CWD 下建出 `~` 目录）", name, got)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("%s 展开后应为绝对路径，得到 %q", name, got)
		}
	}

	if want := filepath.Join(userHome, ".magic", "browser-profile"); cfg.GetBrowserProfileDir() != want {
		t.Errorf("browser_profile_dir = %q, want %q", cfg.GetBrowserProfileDir(), want)
	}
	if want := filepath.Join(userHome, "w"); cfg.WorkingDir != want {
		t.Errorf("working_dir = %q, want %q", cfg.WorkingDir, want)
	}
	if want := filepath.Join(userHome, "mem.db"); *cfg.Memory.DBPath != want {
		t.Errorf("memory.db_path = %q, want %q", *cfg.Memory.DBPath, want)
	}
}

// TestBrowserProfileDirDefaultAndOptOut 钉住 browser_profile_dir 的三态语义：
//   - 配置里没这个键（含全新安装）→ 默认 ~/.magic/browser-profile，登录态持久
//   - 显式写 "" → 返回空，调用方退回"每次全新临时 profile"
//   - 写了别的路径（含 `~` 形式）→ 原样生效
//
// 用 GO_MAGIC_HOME 隔离，不碰真实 ~/.magic。
func TestBrowserProfileDirDefaultAndOptOut(t *testing.T) {
	loadWith := func(t *testing.T, body string) *Config {
		t.Helper()
		home := filepath.Join(t.TempDir(), ".magic")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ConfigFileName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GO_MAGIC_HOME", home)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		return cfg
	}

	wantDefault := ExpandHome(DefaultBrowserProfileDir)

	t.Run("配置缺键用默认目录", func(t *testing.T) {
		cfg := loadWith(t, `{"provider":"deepseek","model":"m"}`)
		if cfg.BrowserProfileDir != nil {
			t.Errorf("缺键时应保持 nil，得到 %q", *cfg.BrowserProfileDir)
		}
		if got := cfg.GetBrowserProfileDir(); got != wantDefault {
			t.Errorf("GetBrowserProfileDir() = %q, want %q", got, wantDefault)
		}
	})

	t.Run("显式空串退回临时 profile", func(t *testing.T) {
		cfg := loadWith(t, `{"provider":"deepseek","model":"m","browser_profile_dir":""}`)
		if got := cfg.GetBrowserProfileDir(); got != "" {
			t.Errorf("显式空串应返回空（临时 profile），得到 %q", got)
		}
	})

	t.Run("自定义路径展开波浪号", func(t *testing.T) {
		cfg := loadWith(t, `{"provider":"deepseek","model":"m","browser_profile_dir":"~/bp"}`)
		want := filepath.Join(userHomeDir(), "bp")
		if got := cfg.GetBrowserProfileDir(); got != want {
			t.Errorf("GetBrowserProfileDir() = %q, want %q", got, want)
		}
	})

	t.Run("默认配置自带该键", func(t *testing.T) {
		cfg := defaultConfig()
		if cfg.BrowserProfileDir == nil {
			t.Fatal("defaultConfig() 未带上 browser_profile_dir，新用户在 config.json 里看不到默认值")
		}
		if *cfg.BrowserProfileDir != DefaultBrowserProfileDir {
			t.Errorf("defaultConfig().BrowserProfileDir = %q, want %q", *cfg.BrowserProfileDir, DefaultBrowserProfileDir)
		}
		if got := cfg.GetBrowserProfileDir(); got != wantDefault {
			t.Errorf("GetBrowserProfileDir() = %q, want %q", got, wantDefault)
		}
	})

	t.Run("nil 接收者不 panic", func(t *testing.T) {
		var cfg *Config
		if got := cfg.GetBrowserProfileDir(); got != wantDefault {
			t.Errorf("nil Config 应返回默认目录，得到 %q", got)
		}
	})
}
