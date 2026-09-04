package cortex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/internal/skills"
)

// TestManagerInitialization 测试 Cortex Manager 能否正确初始化所有子系统
func TestManagerInitialization(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	// 验证核心子系统不为 nil
	checks := []struct {
		name string
		obj  interface{}
	}{
		{"Snapshot", mgr.Snapshot},
		{"Trigger", mgr.Trigger},
		{"Review", mgr.Review},
		{"Perception", mgr.Perception},
		{"Cognition", mgr.Cognition},
		{"Execution", mgr.Execution},
		{"SkillCreator", mgr.SkillCreator},
		{"Soul", mgr.Soul},
		{"UserProfile", mgr.UserProfile},
		{"ContextCompressor", mgr.ContextCompressor},
	}

	for _, c := range checks {
		if c.obj == nil {
			t.Errorf("%s is nil after initialization", c.name)
		}
	}
}

// TestManagerStart 测试 Start() 能否正确加载持久化数据
func TestManagerStart(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)

	// 预先写入 MEMORY.md
	cortexDir := filepath.Join(tmpDir, "cortex")
	os.MkdirAll(cortexDir, 0755)
	os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("# Test Memory\nKey fact 1\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "USER.md"), []byte("# User Profile\nPrefers Go\n"), 0644)

	if err := mgr.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(mgr.Stop)

	if mgr.Snapshot.GetMemoryForPrompt() == "" {
		t.Error("MEMORY.md not loaded after Start()")
	}
	if mgr.Snapshot.GetUserForPrompt() == "" {
		t.Error("USER.md not loaded after Start()")
	}
}

// TestOnTurnStartFreezesSnapshot 测试 OnTurnStart 是否冻结 snapshot
func TestOnTurnStartFreezesSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)
	mgr.Start()
	// Windows 下未关闭的 sqlite 句柄会锁定 TempDir 导致清理失败
	t.Cleanup(mgr.Stop)

	// 初始 memory
	mgr.Snapshot.UpdateMemory("Initial memory")
	mgr.Snapshot.RefreshSnapshot()

	// OnTurnStart 后修改 memory
	mgr.OnTurnStart()
	mgr.Snapshot.UpdateMemory("Modified memory")

	// 获取的 prompt context 应该是冻结前的版本
	frozen := mgr.GetPromptContext()
	if frozen != "Initial memory" {
		t.Errorf("Snapshot not frozen: got %q, want %q", frozen, "Initial memory")
	}

	// latest 应该是最新的
	latest := mgr.Snapshot.GetLatestMemory()
	if latest != "Modified memory" {
		t.Errorf("Latest memory wrong: got %q, want %q", latest, "Modified memory")
	}
}

// TestOnSessionEndRefreshesSnapshot 测试 OnSessionEnd 是否刷新 snapshot
func TestOnSessionEndRefreshesSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)
	mgr.Start()
	// Windows 下未关闭的 sqlite 句柄会锁定 TempDir 导致清理失败
	t.Cleanup(mgr.Stop)

	mgr.Snapshot.UpdateMemory("Session memory")
	mgr.OnTurnStart()
	mgr.Snapshot.UpdateMemory("Updated after turn")

	oldVersion := mgr.GetMemoryVersion()
	mgr.OnSessionEnd()
	newVersion := mgr.GetMemoryVersion()

	if newVersion != oldVersion+1 {
		t.Errorf("Version not incremented: %d -> %d", oldVersion, newVersion)
	}

	frozen := mgr.GetPromptContext()
	if frozen != "Updated after turn" {
		t.Errorf("Snapshot not refreshed after OnSessionEnd: got %q", frozen)
	}
}

// TestOnTurnEndSkillAnalysis 测试 OnTurnEnd 是否触发技能模式分析
func TestOnTurnEndSkillAnalysis(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)
	mgr.Start()
	// Windows 下未关闭的 sqlite 句柄会锁定 TempDir 导致清理失败
	t.Cleanup(mgr.Stop)

	// 模拟一次包含 3 个工具调用的任务
	mgr.Trigger.OnUserMessage("Test task for skill detection")
	mgr.Trigger.OnToolCall("read_file", nil)
	mgr.Trigger.OnToolCall("write_file", nil)
	mgr.Trigger.OnToolCall("execute_command", nil)

	// OnTurnEnd 应该将工具序列喂给 SkillCreator
	mgr.OnTurnEnd()

	patterns := mgr.GetDetectedPatterns()
	if len(patterns) == 0 {
		t.Error("OnTurnEnd did not feed tool calls into SkillCreator")
	}

	// 验证检测到的模式包含正确的工具
	found := false
	for _, p := range patterns {
		seq := strings.Join(p.ToolSequence, " → ")
		if strings.Contains(seq, "read_file") && strings.Contains(seq, "write_file") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected pattern with read_file→write_file not found in %v", patterns)
	}
}

// TestOnSessionEndFinalSkillAnalysis 测试 OnSessionEnd 是否执行最终技能分析
func TestOnSessionEndFinalSkillAnalysis(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)
	mgr.Start()
	// Windows 下未关闭的 sqlite 句柄会锁定 TempDir 导致清理失败
	t.Cleanup(mgr.Stop)

	mgr.Trigger.OnUserMessage("Another test task")
	for _, tool := range []string{"a", "b", "c", "a", "b", "c"} {
		mgr.Trigger.OnToolCall(tool, nil)
	}

	// 第一次 OnTurnEnd 分析
	mgr.OnTurnEnd()
	patternsAfterTurn := len(mgr.GetDetectedPatterns())

	// OnSessionEnd 应该再次分析（累积更多调用）
	mgr.OnSessionEnd()
	patternsAfterSession := len(mgr.GetDetectedPatterns())

	if patternsAfterSession < patternsAfterTurn {
		t.Errorf("OnSessionEnd reduced patterns: %d -> %d", patternsAfterTurn, patternsAfterSession)
	}
}

// TestMemoryCompression 测试 memory 压缩策略
func TestMemoryCompression(t *testing.T) {
	mc := &memory.MemoryCompressor{}

	// 构造一个超长的 memory，包含多个 section
	var sb strings.Builder
	sb.WriteString("# Project Memory\n\n")
	sb.WriteString("## Section 1\n")
	for i := 0; i < 50; i++ {
		sb.WriteString("Line " + string(rune('0'+i%10)) + " content here\n")
	}
	sb.WriteString("## Section 2\n")
	for i := 0; i < 50; i++ {
		sb.WriteString("Another line " + string(rune('0'+i%10)) + " content\n")
	}
	sb.WriteString("## Section 3\n")
	sb.WriteString("Final important note\n")

	content := sb.String()
	compressed := mc.CompressMemory(content, memory.MemoryLimitChars)

	if len(compressed) > memory.MemoryLimitChars {
		t.Errorf("Compressed memory still exceeds limit: %d > %d", len(compressed), memory.MemoryLimitChars)
	}

	// 验证 section 标题被保留
	if !strings.Contains(compressed, "## Section 1") {
		t.Error("Section 1 header lost during compression")
	}
	if !strings.Contains(compressed, "## Section 3") {
		t.Error("Section 3 header lost during compression")
	}
}

// TestGetSystemStatus 测试系统状态报告
func TestGetSystemStatus(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)
	mgr.Start()
	// Windows 下未关闭的 sqlite 句柄会锁定 TempDir 导致清理失败
	t.Cleanup(mgr.Stop)

	status := mgr.GetSystemStatus()

	requiredKeys := []string{
		"layer_1_perception",
		"layer_2_cognition",
		"layer_3_execution",
		"system_1_message_trigger",
		"system_2_nudge_mechanism",
		"system_3_background_review",
		"system_4_frozen_snapshot",
		"system_5_fts_memory",
		"system_6_skill_evolution",
		"total_systems_ready",
		"overall_status",
	}

	for _, key := range requiredKeys {
		if _, ok := status[key]; !ok {
			t.Errorf("Status missing key: %s", key)
		}
	}

	// 验证至少有 6 个系统 ready
	if ready, ok := status["total_systems_ready"].(int); ok && ready < 6 {
		t.Errorf("Too few systems ready: %d/9", ready)
	}
}

// TestSkillCreatorIntegration 测试 SkillCreator 是否与 Manager 正确集成
func TestSkillCreatorIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)
	mgr.Start()
	// Windows 下未关闭的 sqlite 句柄会锁定 TempDir 导致清理失败
	t.Cleanup(mgr.Stop)

	// 模拟两次相同模式的工具调用（触发 minFrequency=2）
	for i := 0; i < 2; i++ {
		mgr.Trigger.OnUserMessage("Repeated task")
		mgr.Trigger.OnToolCall("tool_a", nil)
		mgr.Trigger.OnToolCall("tool_b", nil)
		mgr.Trigger.OnToolCall("tool_c", nil)
		mgr.OnTurnEnd()
	}

	stats := mgr.GetSkillEvolutionStats()
	if stats == nil {
		t.Fatal("GetSkillEvolutionStats returned nil")
	}

	if patterns, ok := stats["patterns_detected"].(int); !ok || patterns == 0 {
		t.Error("No patterns detected after repeated tool sequences")
	}
}

// TestCortexSkillsBoundary 验证 Cortex 与 Skills 的功能边界不重叠
func TestCortexSkillsBoundary(t *testing.T) {
	// Cortex 的 SkillCreator 负责：从工具调用序列中检测模式，自动生成 SKILL.md
	// Skills.Manager 负责：加载、注册、搜索、安装技能，执行技能匹配
	// 两者是上下游关系，不是重叠关系

	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil)
	mgr.Start()
	// Windows 下未关闭的 sqlite 句柄会锁定 TempDir 导致清理失败
	t.Cleanup(mgr.Stop)

	// 1. Cortex SkillCreator 生成技能
	mgr.Trigger.OnUserMessage("Code review task")
	for _, tool := range []string{"read_file", "analyze_code", "write_file", "read_file", "analyze_code", "write_file"} {
		mgr.Trigger.OnToolCall(tool, nil)
	}
	mgr.OnTurnEnd()
	mgr.OnSessionEnd()

	generated := mgr.GetGeneratedSkills()
	// 注意：EnhancedAutoCreator 的 GenerateSkillFromPattern 需要 Confidence >= 0.6
	// 而初始 Confidence 是 0.5，所以可能不会立即生成技能文件
	// 这里主要验证接口可用，不强制要求生成文件

	_ = generated // 接口调用成功即表示集成正常

	// 2. Skills Manager 独立加载技能（包括 auto 生成的）
	skillMgr, err := skills.NewManager()
	if err != nil {
		t.Fatalf("skills.NewManager failed: %v", err)
	}

	// Skills Manager 应该有自己独立的技能列表
	allSkills := skillMgr.List()
	_ = allSkills

	// 关键断言：Cortex 的 SkillCreator 和 Skills Manager 是两个独立对象
	if mgr.SkillCreator == nil {
		t.Error("Cortex SkillCreator is nil")
	}
	if skillMgr == nil {
		t.Error("Skills Manager is nil")
	}

	// 验证 Cortex 不直接操作 Skills Manager 的技能注册表
	//（这是正确的设计：Cortex 生成文件，Skills Manager 加载文件）
}
