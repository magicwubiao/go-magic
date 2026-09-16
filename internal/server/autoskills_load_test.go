package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/skills"
)

// TestLoadAutoSkillsRoundTrip 覆盖「沉淀的技能感知不到」的两个断点：
//  1. 回扫路径必须是 Manager 的 autoSkillsDir（写入方），否则重启后
//     pending/approved 技能一个都扫不到；
//  2. 批准后技能必须进入 GetSkillsList（即注入 agent 的清单）。
//
// 断点 2 的在线生效由 server 每回合 SetSkillsContext 保证（见
// agent.TestSetSkillsContextReplaces）。
func TestLoadAutoSkillsRoundTrip(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	autoDir := filepath.Join(skillsRoot, "auto_skills")

	// 写出一个 pending 自动技能（模拟 SkillCreator 的落盘产物）
	skillDir := filepath.Join(autoDir, "pending", "auto-Pattern-1-1700000000")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"id":"auto-Pattern-1-1700000000","name":"Automated Demo","description":"demo pattern",` +
		`"author":"cortex-auto","status":"pending","pattern_tools":["read_file","write_file"],` +
		`"frequency":3,"created_at":"2026-09-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(skillDir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Automated Demo\n\ndemo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := skills.NewManagerWithConfig(&skills.ManagerConfig{
		SearchDirs:    []string{skillsRoot},
		AutoSkillsDir: autoDir,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	loadAutoSkillsIntoManager(mgr, autoDir)

	// 待批准技能必须能被状态接口看到（用户才能在 UI 里批准）
	var pendingFound bool
	for _, s := range mgr.ListAutoSkillsByStatus(skills.SkillStatusPending) {
		if s.Name == "Automated Demo" {
			pendingFound = true
		}
	}
	if !pendingFound {
		t.Fatal("pending auto skill not registered by loader (回扫路径错误?)")
	}

	// 待批准技能不注入 agent 清单（设计如此）
	if strings.Contains(mgr.GetSkillsList(), "Automated Demo") {
		t.Fatal("pending skill must not be injected into agent context before approval")
	}

	// 批准后必须进入 agent 技能清单
	if err := mgr.ApproveAutoSkill("Automated Demo"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !strings.Contains(mgr.GetSkillsList(), "Automated Demo") {
		t.Fatal("approved auto skill missing from agent skills list")
	}

	// 重启（重新回扫同一目录）：已批准技能必须仍在清单里
	mgr2, err := skills.NewManagerWithConfig(&skills.ManagerConfig{
		SearchDirs:    []string{skillsRoot},
		AutoSkillsDir: autoDir,
	})
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}
	loadAutoSkillsIntoManager(mgr2, autoDir)
	if !strings.Contains(mgr2.GetSkillsList(), "Automated Demo") {
		t.Fatal("approved auto skill lost after restart (回扫未覆盖 approved/)")
	}

	// 状态迁移机制也必须可用（archive 往返；rejected 目录曾在创建期缺失，
	// 父目录不存在会让 Rename 失败）
	if err := mgr2.ArchiveAutoSkill("Automated Demo"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := mgr2.RestoreAutoSkill("Automated Demo"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !strings.Contains(mgr2.GetSkillsList(), "Automated Demo") {
		t.Fatal("restored auto skill missing from agent skills list")
	}
}
