package agentplugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// TestMigrateLegacyDirs 回归：旧命名目录 agent-plugins / agent-plugins-data
// 在启动时应一次性搬到 plugins / plugins-data。
// 三种情形各有明确预期：
//  1. 旧有新无     → rename，内容跟着走；
//  2. 旧有新也有   → 不动（避免覆盖新数据，旧目录保留）；
//  3. 旧无新无     → 什么都不发生（EnsureDefaultScanDir 负责建新目录）。
func TestMigrateLegacyDirs(t *testing.T) {
	t.Run("legacy renamed when target missing", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GO_MAGIC_HOME", home)
		if err := os.MkdirAll(filepath.Join(home, legacyScanDir, "demo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, legacyScanDir, "demo", "marker.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, legacyDataDir, "demo"), 0o755); err != nil {
			t.Fatal(err)
		}

		migrateLegacyDirs()

		if _, err := os.Stat(filepath.Join(home, "plugins", "demo", "marker.txt")); err != nil {
			t.Fatalf("scan dir not migrated: %v", err)
		}
		if _, err := os.Stat(filepath.Join(home, "plugins-data", "demo")); err != nil {
			t.Fatalf("data dir not migrated: %v", err)
		}
		if _, err := os.Stat(filepath.Join(home, legacyScanDir)); !os.IsNotExist(err) {
			t.Fatalf("legacy scan dir should be gone, got %v", err)
		}
	})

	t.Run("existing new dir is never overwritten", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GO_MAGIC_HOME", home)
		if err := os.MkdirAll(filepath.Join(home, legacyScanDir, "old"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, "plugins", "new"), 0o755); err != nil {
			t.Fatal(err)
		}

		migrateLegacyDirs()

		if _, err := os.Stat(filepath.Join(home, "plugins", "old")); err == nil {
			t.Fatal("legacy content must not be merged/overwritten into existing new dir")
		}
		if _, err := os.Stat(filepath.Join(home, "plugins", "new")); err != nil {
			t.Fatal("existing new dir content must survive")
		}
	})

	t.Run("no-op without legacy dirs", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GO_MAGIC_HOME", home)

		migrateLegacyDirs()

		if _, err := os.Stat(filepath.Join(home, "plugins")); !os.IsNotExist(err) {
			t.Fatalf("migrate must not create dirs, got %v", err)
		}
		// DefaultScanDir 应指向新命名，且 EnsureDefaultScanDir 会把它建出来。
		if got := DefaultScanDir(); got != filepath.Join(config.GetMagicHome(), "plugins") {
			t.Fatalf("DefaultScanDir = %q, want plugins dir", got)
		}
	})
}
