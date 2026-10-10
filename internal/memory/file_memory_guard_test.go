package memory

import (
	"os"
	"strings"
	"testing"
)

// TestFileMemoryHasSingleHome 是「文件型记忆只有一份」的守卫（2026-10-10）。
//
// 背景：Memory.md 拆成了两套，而两套都不自洽 ——
//
//	写/读 A：internal/memory/store.go 的 Read/Write/Append(Agent|User)Memory
//	         + ensureMemoryFiles，目标 <magicHome>/memories/{MEMORY,USER}.md
//	         （唯一使用者是同样零调用的 CLI internal/memory/tool.go）
//	写/读 B：cortex 侧 —— distiller 写 <cortexDir>/MEMORY.md、
//	         cortex.UserProfile 写 <cortexDir>/USER.md，SnapshotManager 读它们
//	         并注入提示词
//
// 结果：同一份「记忆」在磁盘上裂成两份，真正被注入提示词的那份（cortexDir）
// 从来没有被 A 写过，而 A 写出来的 <magicHome>/memories/*.md 没有任何读入口。
//
// A 已随 tool.go 一并删除；USER.md 的来源也收敛到 cortex.UserProfile
// （结构化偏好，自动过滤 "[Not set]" 占位符），SnapshotManager 只负责 MEMORY.md。
// 本用例读源码把这两个不变量钉死：光删掉调用点不够，还得防止有人加回来。
func TestFileMemoryHasSingleHome(t *testing.T) {
	sources := []struct {
		path    string
		banned  []string // 出现即失败（只写「只在代码里出现」的 token，避免误伤注释）
		mustHas []string
	}{
		{
			path: "store.go",
			banned: []string{
				"s.agentMemoryPath",
				"s.userMemoryPath",
				"func (s *Store) ensureMemoryFiles",
				"func (s *Store) ReadAgentMemory",
				"func (s *Store) WriteAgentMemory",
				"func (s *Store) AppendAgentMemory",
				"func (s *Store) ReadUserMemory",
				"func (s *Store) WriteUserMemory",
				"func (s *Store) AppendUserMemory",
			},
			mustHas: []string{"memory.db"},
		},
		{
			// SnapshotManager 只管 agent 记忆（MEMORY.md）；用户画像不经过这里。
			path: "snapshot_manager.go",
			banned: []string{
				"userPath",
				"frozenUser",
				"latestUser",
				"GetUserForPrompt",
				"UpdateUser",
				"AppendToUser",
				"compressUser",
				"UserLimitChars",
			},
			mustHas: []string{"MEMORY.md"},
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
				t.Errorf("%s 里出现 %q：文件型记忆（MEMORY.md/USER.md）只能有一份，"+
					"其唯一位置是 cortex 目录，别再往 <magicHome>/memories/ 另写一份",
					s.path, b)
			}
		}
		for _, m := range s.mustHas {
			if !strings.Contains(src, m) {
				t.Errorf("%s 里找不到 %q：写法被改动了？", s.path, m)
			}
		}
	}

	// 已删除的实现不得复活。
	if _, err := os.Stat("tool.go"); err == nil {
		t.Error("internal/memory/tool.go 又回来了：MemoryTool/ToolFunction 全仓零引用" +
			"（CLI memory 子命令从未挂载），且它是 Store 那套第二份 MEMORY.md/USER.md " +
			"的唯一使用者，已按死代码删除")
	}
}
