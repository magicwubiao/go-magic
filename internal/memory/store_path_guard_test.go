package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMemoryStorePathIsNotForked 是「记忆库路径唯一」的守卫（2026-10-10）。
//
// 背景：进程里曾经同时存在三份记忆数据 ——
//
//	cortex 主库  <magicHome>/cortex/memories/memory.db  （4059 行）
//	cortex 副本  <magicHome>/cortex/fts/memory.sqlite   （4059 行，逐条同源）
//	工具层库     <magicHome>/memories/memory.db         （实测 53 行）
//
// 成因：cortex 在 integration.go 里把 Store 的 DBPath 覆盖成 cortexDir 下的
// 路径，而工具层用 DefaultConfig()，两边各自成库；search_history 想看 cortex
// 抽取的记忆，就只能靠那份 FTS 副本当中转 —— 同一批数据存两遍，副本还会随
// 蒸馏漂移（主库存带日期前缀的摘要、副本存裸摘要）。
//
// 现在统一到 DefaultConfig().DBPath 一份。本用例读源码把「不得再分裂」钉死：
// 光删掉调用点不够，还得防止有人哪天又加回来。
func TestMemoryStorePathIsNotForked(t *testing.T) {
	canonical := DefaultConfig().DBPath
	if filepath.Base(canonical) != "memory.db" {
		t.Fatalf("记忆库规范路径的文件名不是 memory.db: %q", canonical)
	}
	if filepath.Base(filepath.Dir(canonical)) != "memories" {
		t.Fatalf("记忆库规范路径不在 memories/ 下: %q", canonical)
	}

	sources := []struct {
		path    string
		banned  []string // 出现即失败
		mustHas []string // 必须出现（防止"把调用点删干净所以没违规"式的假绿）
	}{
		{
			path:    filepath.Join("..", "cortex", "integration.go"),
			banned:  []string{"memCfg.DBPath", "NewFTSStore", "FTSMemory"},
			mustHas: []string{"memory.DefaultConfig()"},
		},
		{
			path:    filepath.Join("..", "tool", "memory_tools.go"),
			banned:  []string{"NewFTSStore", "GetSharedFTSStore", "sharedFTSStore"},
			mustHas: []string{"NewStore(memory.DefaultConfig())"},
		},
	}

	for _, s := range sources {
		data, err := os.ReadFile(s.path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", s.path, err)
		}
		src := string(data)
		for _, b := range s.banned {
			if strings.Contains(src, b) {
				t.Errorf("%s 里出现 %q：记忆库不得再分裂成多份，"+
					"cortex 与工具层必须共用 memory.DefaultConfig().DBPath", s.path, b)
			}
		}
		for _, m := range s.mustHas {
			if !strings.Contains(src, m) {
				t.Errorf("%s 里找不到 %q：统一路径的写法被改动了？", s.path, m)
			}
		}
	}

	// 已删除的实现不得复活。
	if _, err := os.Stat("fts_store.go"); err == nil {
		t.Error("internal/memory/fts_store.go 又回来了：FTS 副本已废弃，" +
			"结构化 Store 自带 memories_fts 索引，别再维护第二套")
	}
	if _, err := os.Stat(filepath.Join("integration", "memory_integration.go")); err == nil {
		t.Error("internal/memory/integration/ 又回来了：该包（MemoryIntegration）全仓零引用，已按死代码删除")
	}
}
