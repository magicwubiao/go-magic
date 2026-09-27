package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 回归：外部直接修改 config.json 后，chat 页依赖的只读接口（/api/model/options
// 等）读的是启动时的内存快照，永远看不到更新。syncConfigFromDisk 用 mtime 检测
// 外部变更并重载。见 config.go syncConfigFromDisk 的注释。
func TestSyncConfigFromDisk(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", tmp)

	cfgPath := filepath.Join(tmp, "config.json")
	old := `{"provider":"deepseek","model":"old-model","providers":{"deepseek":{"api_key":"k1","models":["old-model"]}}}`
	if err := os.WriteFile(cfgPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}

	s := &Server{magicHome: tmp}
	var err error
	s.cfg, err = appconfig.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.markConfigMtime()

	// mtime 未变化：不应重载（s.cfg 指针不变）
	snapshot := s.cfg
	s.syncConfigFromDisk()
	if s.cfg != snapshot {
		t.Fatal("mtime 未变化时不应重载配置")
	}

	// 外部修改 config.json：下一次同步应看到新值
	time.Sleep(50 * time.Millisecond) // 确保 mtime 前进（文件系统时间粒度）
	newCfg := `{"provider":"deepseek","model":"new-model","providers":{"deepseek":{"api_key":"k2","models":["new-model"]}}}`
	if err := os.WriteFile(cfgPath, []byte(newCfg), 0600); err != nil {
		t.Fatal(err)
	}
	s.syncConfigFromDisk()

	if s.cfg == nil || s.cfg.Model != "new-model" {
		t.Fatalf("外部修改后应重载配置，got model=%q", s.cfg.Model)
	}
	if got := s.cfg.Providers["deepseek"].APIKey; got != "k2" {
		t.Fatalf("providers 段应来自磁盘新值，got api_key=%q", got)
	}

	// 重载后基线已刷新：再次同步应为 no-op
	after := s.cfg
	s.syncConfigFromDisk()
	if s.cfg != after {
		t.Fatal("重载后基线已更新，再次同步不应重载")
	}
}
