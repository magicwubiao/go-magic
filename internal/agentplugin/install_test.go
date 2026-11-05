package agentplugin

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePluginZip 生成一个最小插件 zip：plugin.json + 附加文件。
func writePluginZip(t *testing.T, dir, name, extraFile, extraContent string) string {
	t.Helper()
	path := filepath.Join(dir, name+".zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"demo","version":"1.0.0"}`
	w, err := zw.Create(manifestName)
	if err != nil {
		t.Fatalf("zip manifest: %v", err)
	}
	if _, err := w.Write([]byte(manifest)); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if extraFile != "" {
		w, err = zw.Create(extraFile)
		if err != nil {
			t.Fatalf("zip extra: %v", err)
		}
		if _, err := w.Write([]byte(extraContent)); err != nil {
			t.Fatalf("write extra: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

// TestInstallFromZip_UpgradeReplacesFilesAndKeepsData 回归：
// "升级 = 卸载 + 重装"会连带删掉 PLUGIN_DATA；现在默认原地升级，
// 文件被替换而数据目录原样保留。
func TestInstallFromZip_UpgradeReplacesFilesAndKeepsData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)

	work := t.TempDir()
	v1 := writePluginZip(t, work, "v1", "payload.txt", "v1")
	dest, manifest, err := InstallFromZip(v1, InstallOptions{})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	if manifest.Name != "demo" {
		t.Fatalf("manifest name = %q", manifest.Name)
	}

	// 写入一份"跨更新持久"的插件数据。
	dataDir := filepath.Join(home, "plugins-data", "demo")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	stateFile := filepath.Join(dataDir, "state.json")
	if err := os.WriteFile(stateFile, []byte(`{"indexed":42}`), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	// 未允许升级时必须是可识别的哨兵错误，而不是一句英文报错。
	if _, _, err := InstallFromZip(writePluginZip(t, work, "v2", "payload.txt", "v2"), InstallOptions{}); !errors.Is(err, ErrPluginExists) {
		t.Fatalf("second install without upgrade: err = %v, want ErrPluginExists", err)
	}

	v3 := writePluginZip(t, work, "v3", "payload.txt", "v3")
	dest2, _, err := InstallFromZip(v3, InstallOptions{Upgrade: true})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if dest2 != dest {
		t.Fatalf("upgrade moved the plugin: %q -> %q", dest, dest2)
	}
	body, err := os.ReadFile(filepath.Join(dest, "payload.txt"))
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if strings.TrimSpace(string(body)) != "v3" {
		t.Fatalf("payload = %q, want v3", body)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("PLUGIN_DATA must survive an upgrade: %v", err)
	}
	// 升级的临时目录不能留在扫描目录里（它们以 "." 开头，LoadAll 会跳过，
	// 但也不能无限堆积）。
	entries, _ := os.ReadDir(DefaultScanDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") || strings.HasPrefix(e.Name(), ".backup-") {
			t.Errorf("leftover internal dir after upgrade: %s", e.Name())
		}
	}
}

// TestLoadAll_KeepsRejectedPlugins 回归：清单致命失败的插件必须仍然出现在
// 结果里（可卸载），否则用户面对的是"装不上、看不见、目录还占着名字"。
func TestLoadAll_KeepsRejectedPlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)

	scanDir := DefaultScanDir()
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		t.Fatalf("mkdir scan dir: %v", err)
	}
	badDir := filepath.Join(scanDir, "broken-plugin")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	// 缺 $schema = 清单致命违反。
	if err := os.WriteFile(filepath.Join(badDir, manifestName), []byte(`{"name":"broken-plugin"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	plugins := NewManager([]string{scanDir}, map[string]bool{}).LoadAll()
	mp, ok := plugins["broken-plugin"]
	if !ok {
		t.Fatalf("rejected plugin missing from LoadAll result: %v", plugins)
	}
	if !mp.Rejected || mp.Plugin == nil || mp.Plugin.FatalError == "" {
		t.Fatalf("plugin should be marked rejected with a fatal error: %+v", mp)
	}
	if mp.DirName != "broken-plugin" {
		t.Fatalf("dir_name = %q, want broken-plugin", mp.DirName)
	}
	if mp.Runtime != nil {
		t.Fatal("rejected plugin must not get a runtime")
	}
}

// TestLoadAll_SkipsInternalDirs 回归：安装/升级的隐藏目录不能被当成插件。
func TestLoadAll_SkipsInternalDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)
	scanDir := DefaultScanDir()
	staging := filepath.Join(scanDir, ".staging-abc123")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"ghost"}`
	if err := os.WriteFile(filepath.Join(staging, manifestName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if plugins := NewManager([]string{scanDir}, map[string]bool{}).LoadAll(); len(plugins) != 0 {
		t.Fatalf("hidden staging dir was loaded as a plugin: %v", plugins)
	}
}
