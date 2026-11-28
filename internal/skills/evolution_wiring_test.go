package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// ---------------------------------------------------------------------------
// P1-3：模式自动建技能的阈值与数据一致化
// ---------------------------------------------------------------------------

// 阈值必须只有一个默认来源，且 0/负数归一为默认值而不是 0。
// 回归点：此前构造器硬编码 5、配置默认 3；SetMinFrequency(0) 会把阈值改成 0，
// 使 `Frequency >= 0` 恒真 → 任一模式首见即建技能。
func TestMinFrequencyDefaultAndNormalization(t *testing.T) {
	if DefaultAutoSkillMinFrequency != config.DefaultAutoSkillMinFrequency {
		t.Fatalf("threshold default drifted away from pkg/config: %d != %d",
			DefaultAutoSkillMinFrequency, config.DefaultAutoSkillMinFrequency)
	}

	ac := NewEnhancedAutoCreator(t.TempDir())
	if got := ac.GetMinFrequency(); got != DefaultAutoSkillMinFrequency {
		t.Fatalf("constructor default = %d, want %d", got, DefaultAutoSkillMinFrequency)
	}

	for _, bad := range []int{0, -1, -100} {
		ac.SetMinFrequency(bad)
		if got := ac.GetMinFrequency(); got != DefaultAutoSkillMinFrequency {
			t.Fatalf("SetMinFrequency(%d) => %d, want normalized %d (0 must not disable the gate)",
				bad, got, DefaultAutoSkillMinFrequency)
		}
	}

	ac.SetMinFrequency(7)
	if got := ac.GetMinFrequency(); got != 7 {
		t.Fatalf("SetMinFrequency(7) => %d, want 7", got)
	}
}

// 未达阈值的模式不得生成技能；达到阈值的才生成。
// 注意：闸门在 checkAndGenerateSkillsFromSnapshot，GenerateSkillFromPattern
// 只是底层写盘动作。
func TestPatternThresholdGatesSkillGeneration(t *testing.T) {
	dir := t.TempDir()
	ac := NewEnhancedAutoCreator(dir)
	ac.SetMinFrequency(3)

	ac.mu.Lock()
	ac.patterns = []Pattern{
		{
			Name: "Pattern-below", Tools: []string{"read_file", "write_file", "terminal"},
			ToolSequence: []string{"read_file", "write_file", "terminal"},
			Frequency:    2, Confidence: 0.9,
		},
	}
	ac.mu.Unlock()
	ac.CheckAndGenerateSkills()

	if got := len(ac.GetGeneratedSkills()); got != 0 {
		t.Fatalf("below-threshold pattern produced %d skill(s), want 0", got)
	}

	ac.mu.Lock()
	ac.patterns = append(ac.patterns, Pattern{
		Name: "Pattern-at", Tools: []string{"search_in_files", "read_file", "write_file"},
		ToolSequence: []string{"search_in_files", "read_file", "write_file"},
		Frequency:    3, Confidence: 0.9,
	})
	ac.mu.Unlock()
	ac.CheckAndGenerateSkills()

	if got := len(ac.GetGeneratedSkills()); got != 1 {
		t.Fatalf("at-threshold pattern produced %d skill(s), want 1 (got %v)",
			got, ac.GetGeneratedSkills())
	}
}

// GetGeneratedSkills 必须扫描四态子目录（技能真实落盘位置），而不是 baseDir 顶层。
// 回归点：此前只扫顶层，顶层只有 pending/approved/archived/rejected 四个目录，
// 永远返回空 → cortex 的 "Generated N new skills" 从不打印。
func TestGetGeneratedSkillsScansStateDirs(t *testing.T) {
	dir := t.TempDir()
	ac := NewEnhancedAutoCreator(dir)

	if got := ac.GetGeneratedSkills(); len(got) != 0 {
		t.Fatalf("fresh creator should report 0 skills, got %v", got)
	}

	ac.GenerateSkillFromPattern(Pattern{
		Name: "Pattern-x", Tools: []string{"a", "b", "c"},
		ToolSequence: []string{"a", "b", "c"},
		Frequency:    5, Confidence: 0.9,
	})

	got := ac.GetGeneratedSkills()
	if len(got) != 1 {
		t.Fatalf("GetGeneratedSkills() = %v, want exactly 1 (written under pending/)", got)
	}

	// 必须真的位于 pending/ 下。
	if _, err := os.Stat(filepath.Join(dir, "auto_skills", "pending", got[0])); err != nil {
		t.Fatalf("reported skill %q is not under pending/: %v", got[0], err)
	}
}

// GetStats 的 skills_generated 必须与磁盘一致，而不是易失的进程内计数器。
// 回归点：计数器重启归零，导致面板出现 "patterns_detected=2, skills_generated=0"
// 这种与磁盘实际技能自相矛盾的读数。
func TestGetStatsSkillCountIsPersistent(t *testing.T) {
	dir := t.TempDir()
	ac := NewEnhancedAutoCreator(dir)
	ac.GenerateSkillFromPattern(Pattern{
		Name: "Pattern-y", Tools: []string{"a", "b", "c"},
		ToolSequence: []string{"a", "b", "c"},
		Frequency:    5, Confidence: 0.9,
	})

	// 模拟进程重启：新的 creator 指向同一目录，进程内计数器归零。
	restarted := NewEnhancedAutoCreator(dir)
	stats := restarted.GetStats()

	if got := stats["skills_generated"]; got != 1 {
		t.Fatalf("after restart skills_generated = %v, want 1 (persisted on disk)", got)
	}
	if got := stats["skills_generated_this_run"]; got != 0 {
		t.Fatalf("this-run counter should be 0 after restart, got %v", got)
	}
	if got := stats["min_frequency"]; got != DefaultAutoSkillMinFrequency {
		t.Fatalf("min_frequency = %v, want %d", got, DefaultAutoSkillMinFrequency)
	}
}

// ---------------------------------------------------------------------------
// P1-1：有效性统计 + 触发优化接线
// ---------------------------------------------------------------------------

// 技能被调用后必须留下有效性记录 —— 这是进化链路的唯一原始输入。
// 回归点：RecordInvocation 此前没有任何调用方，统计数据恒为空。
func TestRecordSkillInvocationFeedsEffectiveness(t *testing.T) {
	m := newTestManager(t)

	m.RecordSkillInvocation("demo-skill", "skill_invoke", "run the demo",
		75, true, 120*time.Millisecond, []string{"read_file"}, "sess-1")

	stats := m.GetAllStatistics()
	if len(stats) == 0 {
		t.Fatal("no statistics recorded after RecordSkillInvocation")
	}

	var found *SkillStatistics
	for _, s := range stats {
		if s.SkillName == "demo-skill" {
			found = s
			break
		}
	}
	if found == nil {
		t.Fatalf("statistics for demo-skill missing: %+v", stats)
	}
	if found.TotalInvocations != 1 {
		t.Fatalf("TotalInvocations = %d, want 1", found.TotalInvocations)
	}
	if found.SuccessRate != 100 {
		t.Fatalf("SuccessRate = %v, want 100 (one success / one invocation)", found.SuccessRate)
	}
}

// 触发优化必须接到真实统计数据上（此前 TriggerOptimizer 从未被构造）。
func TestTriggerOptimizerIsWired(t *testing.T) {
	m := newTestManager(t)

	m.RecordSkillInvocation("trig-skill", "skill_invoke", "input", 60, true, time.Millisecond, nil, "")
	for i := 0; i < 3; i++ {
		m.RecordMissedSkillTrigger("trig-skill", "deploy the docker image")
	}

	stats := m.GetTriggerStats()
	if len(stats) == 0 {
		t.Fatal("trigger stats empty: TriggerOptimizer is not being fed")
	}
	if _, ok := stats["trig-skill"]; !ok {
		t.Fatalf("trig-skill missing from trigger stats: %+v", stats)
	}

	cond, err := m.OptimizeSkillTriggers("trig-skill")
	if err != nil {
		t.Fatalf("OptimizeSkillTriggers: %v", err)
	}
	if cond == nil {
		t.Fatal("OptimizeSkillTriggers returned nil condition")
	}
	if len(cond.Keywords) == 0 {
		t.Fatal("optimizer produced no keywords from repeated missed triggers")
	}

	// 优化结果必须能被读取（闭环的读侧）。
	if got := m.triggerOpt.GetOptimizedTriggers("trig-skill"); got == nil || len(got.Keywords) == 0 {
		t.Fatal("optimized triggers were written but not readable")
	}
}

// 触发优化的数据源必须是 "keyword" 命中。
//
// 回归点：生产侧此前唯一的上报是 RecordSkillInvocation 内部的
// RecordTriggerHit(name, "invocation", "", ...)，而 OptimizeTriggers 只处理
// triggerType == "keyword" ⇒ keywordStats 恒空 ⇒ cond.Keywords 恒为空 ⇒
// GetOptimizedTriggers 恒返回 nil ⇒ applyOptimizedTriggers 恒为 no-op。
// 显式 /skill 命中（RecordSkillDirectiveHit）补上了这个缺口。
func TestDirectiveHitProducesOptimizableKeywords(t *testing.T) {
	m := newTestManager(t)

	for i := 0; i < 3; i++ {
		m.RecordSkillDirectiveHit("email-drafter", "draft a reply")
	}

	cond, err := m.OptimizeSkillTriggers("email-drafter")
	if err != nil {
		t.Fatalf("OptimizeSkillTriggers: %v", err)
	}
	if cond == nil || len(cond.Keywords) == 0 {
		t.Fatalf("explicit directive hits produced no keywords: %+v", cond)
	}
	found := false
	for _, kw := range cond.Keywords {
		if kw == "email-drafter" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the explicitly invoked skill name must become an optimizable keyword, got %v", cond.Keywords)
	}

	// 读侧闭环：优化结果必须能被 applyOptimizedTriggers 取到。
	if got := m.triggerOpt.GetOptimizedTriggers("email-drafter"); got == nil || len(got.Keywords) == 0 {
		t.Fatal("optimized triggers were written but not readable")
	}
}

// 整条触发链路闭环：显式命中 → 学到关键词 → 真正影响匹配结果。
func TestOptimizedTriggersFeedBackIntoMatching(t *testing.T) {
	m := newTestManager(t)
	if err := m.Add(&Skill{
		SkillMeta: SkillMeta{Name: "pdf-tool", Description: "unrelated description"},
		Content:   "body",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// 前置：优化的关键词（= 技能名）出现前，这句话匹配不到该技能。
	for _, sk := range m.MatchSkillsByInput("please reach for the tool") {
		if sk.Name == "pdf-tool" {
			t.Fatal("precondition broken: skill matched before optimization")
		}
	}

	for i := 0; i < 3; i++ {
		m.RecordSkillDirectiveHit("pdf-tool", "")
	}
	if _, err := m.OptimizeSkillTriggers("pdf-tool"); err != nil {
		t.Fatalf("OptimizeSkillTriggers: %v", err)
	}

	found := false
	for _, sk := range m.MatchSkillsByInput("please use pdf-tool now") {
		if sk.Name == "pdf-tool" {
			found = true
		}
	}
	if !found {
		t.Fatal("optimized keyword did not feed back into skill matching (link still broken)")
	}
}

// ---------------------------------------------------------------------------
// P1-2：版本快照 + 回滚
// ---------------------------------------------------------------------------

// 创建/更新/删除都必须落版本快照，且回滚必须**真的恢复 SKILL.md 内容**。
// 回归点有四个，此前都不可用：
//  1. SaveVersion 没有任何调用方 → 版本历史恒为空；
//  2. SaveVersion 内部二次加锁 → 一旦被调用即自死锁；
//  3. Rollback 只改版本标记、不回写内容 → "回滚成功"但内容一字未变；
//  4. 快照存的是**裸正文**而回滚是**整文件覆写** → 回滚到初始版本会抹掉 YAML
//     frontmatter（技能名退化成目录名、description/tags 丢失）。
func TestVersionSnapshotAndRollback(t *testing.T) {
	m := newTestManager(t)

	skill, err := m.Create("snap-skill", "snapshot demo", "# v1 body", []string{"test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 快照口径 = 磁盘上的 SKILL.md 全文（含 frontmatter）。
	v1OnDisk, err := os.ReadFile(filepath.Join(skill.Dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md after Create: %v", err)
	}
	if !strings.HasPrefix(string(v1OnDisk), "---") {
		t.Fatalf("Create must write a frontmatter-headed SKILL.md, got:\n%s", v1OnDisk)
	}
	if !strings.Contains(string(v1OnDisk), "# v1 body") {
		t.Fatalf("Create must embed the user body, got:\n%s", v1OnDisk)
	}

	versions := m.GetVersions("snap-skill")
	if len(versions) == 0 {
		t.Fatal("Create did not record a version snapshot")
	}

	var target string
	for _, v := range versions {
		if v.Content == string(v1OnDisk) {
			target = v.Version
			break
		}
	}
	if target == "" {
		t.Fatalf("no version holds the original on-disk content; versions=%+v", versions)
	}

	// 模拟"被进化改坏"：整文件覆写。
	if err := m.Update("snap-skill", "# v2 body (broken by evolution)"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := m.GetVersions("snap-skill"); len(got) < 2 {
		t.Fatalf("expected >=2 versions after create+update, got %d", len(got))
	}

	if err := m.RollbackSkillVersion("snap-skill", target); err != nil {
		t.Fatalf("RollbackSkillVersion(%q): %v", target, err)
	}

	// 回滚必须落到磁盘上的 SKILL.md，且**保住 frontmatter**。
	onDisk, err := os.ReadFile(filepath.Join(skill.Dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md after rollback: %v", err)
	}
	if string(onDisk) != string(v1OnDisk) {
		t.Fatalf("SKILL.md not restored by rollback:\n got: %q\nwant: %q", string(onDisk), string(v1OnDisk))
	}

	// 重新加载后内存视图也应回到 v1（loader 会剥掉 frontmatter 取正文）。
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, err := m.Get("snap-skill")
	if err != nil {
		t.Fatalf("Get after rollback: %v", err)
	}
	if !strings.Contains(got.Content, "# v1 body") {
		t.Fatalf("in-memory content not restored after rollback: %q", got.Content)
	}
	// frontmatter 里的元数据必须还在（否则技能名/描述/标签全丢）。
	if got.Name != "snap-skill" {
		t.Fatalf("skill name lost after rollback: %q (frontmatter was overwritten)", got.Name)
	}
	if got.Description != "snapshot demo" {
		t.Fatalf("skill description lost after rollback: %q", got.Description)
	}
}

// 被删除的技能必须能从版本历史恢复 —— Delete 落了快照并注释承诺"误删可恢复"，
// 而 restoreContentToDisk 此前要求目录存在，回滚直接报 skill directory not found。
func TestRollbackRecreatesDeletedSkill(t *testing.T) {
	m := newTestManager(t)

	skill, err := m.Create("gone-skill", "deleted demo", "# body", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	versions := m.GetVersions("gone-skill")
	if len(versions) == 0 {
		t.Fatal("no versions recorded for the skill")
	}
	target := versions[0].Version
	dir := skill.Dir

	if err := m.Delete("gone-skill"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("skill dir should be gone after Delete, stat err = %v", err)
	}

	if err := m.RollbackSkillVersion("gone-skill", target); err != nil {
		t.Fatalf("rollback of a deleted skill must recreate it: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read restored SKILL.md: %v", err)
	}
	if !strings.HasPrefix(string(restored), "---") || !strings.Contains(string(restored), "# body") {
		t.Fatalf("restored file is not a valid skill file:\n%s", restored)
	}
}

// 版本历史必须有上界：每份版本内嵌全文且只增不减，否则版本目录随编辑次数线性膨胀。
// 轮转保留最早一版（"回到出厂"的最后安全网）与最近若干版。
func TestVersionHistoryIsBounded(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Create("bounded-skill", "d", "# v0", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	initial := m.GetVersions("bounded-skill")
	if len(initial) == 0 {
		t.Fatal("no initial version")
	}
	initialVersion := initial[len(initial)-1].Version // ListVersions 是最新在前

	for i := 0; i < maxVersionsPerSkill+20; i++ {
		if err := m.Update("bounded-skill", fmt.Sprintf("# rev %d", i)); err != nil {
			t.Fatalf("Update %d: %v", i, err)
		}
	}

	versions := m.GetVersions("bounded-skill")
	if len(versions) > maxVersionsPerSkill {
		t.Fatalf("version history not bounded: %d > %d", len(versions), maxVersionsPerSkill)
	}
	foundInitial := false
	for _, v := range versions {
		if v.Version == initialVersion {
			foundInitial = true
			break
		}
	}
	if !foundInitial {
		t.Fatal("the initial version must be kept as the last-resort rollback point")
	}
	// 最新内容必须仍在列表里（当前版本不可被轮转掉）。
	if versions[0].Content != "# rev "+fmt.Sprint(maxVersionsPerSkill+19) {
		t.Fatalf("newest version missing from history: %q", versions[0].Content)
	}
}

// 内容逐字节相同时不得产生新版本 —— Update 会在写盘前后各落一次快照，
// 而"改后那份"与"下次改前的"内容一致，无去重就会堆出一串一模一样的版本。
func TestVersionSnapshotDeduplicatesIdenticalContent(t *testing.T) {
	m := newTestManager(t)
	skill, err := m.Create("dedup-skill", "d", "# same", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(skill.Dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	before := len(m.GetVersions("dedup-skill"))

	// 用与磁盘完全相同的字节"更新"：不该产生任何新版本。
	if err := m.Update("dedup-skill", string(onDisk)); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if after := len(m.GetVersions("dedup-skill")); after != before {
		t.Fatalf("identical content produced %d new version(s), want 0", after-before)
	}

	// 真正改了内容时必须产生新版本（去重不能把正常快照也吞掉）。
	if err := m.Update("dedup-skill", "# changed"); err != nil {
		t.Fatalf("Update(changed): %v", err)
	}
	if after := len(m.GetVersions("dedup-skill")); after != before+1 {
		t.Fatalf("real change produced %d new version(s), want 1", after-before)
	}
}

// ---------------------------------------------------------------------------
// 主干接线：EvolveSkill 真的跑起来之后必须保持 SKILL.md 合法
// ---------------------------------------------------------------------------

// fakeEvoProvider 是最小的 provider 桩：直接返回一段带 new_content 的 JSON。
type fakeEvoProvider struct {
	body string
}

func (f *fakeEvoProvider) Name() string { return "fake-evo" }

func (f *fakeEvoProvider) Chat(_ context.Context, _ []provider.Message) (*provider.ChatResponse, error) {
	payload := fmt.Sprintf(`{"strategy":"simplify","new_content":%q,"reason":"test","confidence":0.9}`, f.body)
	return &provider.ChatResponse{Content: payload}, nil
}

// 进化改写的必须是**正文**：SKILL.md 的 YAML frontmatter 必须原样保留，
// 否则 round 一次之后技能名就退化成目录名、描述与标签全丢。
func TestEvolveSkillPreservesFrontmatter(t *testing.T) {
	m := newTestManager(t)
	skill, err := m.Create("evo-skill", "evolution demo", "# original body", []string{"demo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 让 CheckEvolutionNeeded 放行：调用数够、平均质量低。
	for i := 0; i < 6; i++ {
		m.RecordSkillInvocation("evo-skill", "skill_invoke", "in", 20, true, time.Millisecond, nil, "")
	}

	em := NewSkillEvolutionManager(m, m.effMgr,
		&fakeEvoProvider{body: "# evolved body\n\nbetter instructions"}, filepath.Dir(skill.Dir))
	if err := em.LoadRecords(); err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}

	rec, err := em.EvolveSkill(context.Background(), "evo-skill")
	if err != nil {
		t.Fatalf("EvolveSkill: %v", err)
	}
	if rec == nil {
		t.Fatal("EvolveSkill returned no record")
	}

	onDisk, err := os.ReadFile(filepath.Join(skill.Dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read evolved SKILL.md: %v", err)
	}
	got := string(onDisk)
	if !strings.HasPrefix(got, "---") {
		t.Fatalf("evolution stripped the YAML frontmatter:\n%s", got)
	}
	if !strings.Contains(got, "name: evo-skill") {
		t.Fatalf("frontmatter name lost:\n%s", got)
	}
	if !strings.Contains(got, "# evolved body") {
		t.Fatalf("evolved body not written:\n%s", got)
	}

	// 重新加载后元数据必须完好（证明 frontmatter 仍然合法）。
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	reloaded, err := m.Get("evo-skill")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reloaded.Name != "evo-skill" || reloaded.Description != "evolution demo" {
		t.Fatalf("metadata lost after evolution: name=%q description=%q", reloaded.Name, reloaded.Description)
	}

	// 进化历史必须可读（此前 evolution_records.json 没有任何写入方）。
	history := m.GetEvolutionRecords("evo-skill")
	if len(history) == 0 {
		t.Fatal("evolution record was not persisted / not readable")
	}
}

// mergeFrontmatter 的边界：没有 frontmatter 时原样返回正文，不臆造元数据。
func TestMergeFrontmatterEdgeCases(t *testing.T) {
	body := "# body"
	if got := mergeFrontmatter("# no frontmatter at all", body); got != body {
		t.Fatalf("expected body verbatim, got %q", got)
	}
	if got := mergeFrontmatter("---\nname: x\n", body); got != body {
		t.Fatalf("unterminated frontmatter must not be treated as valid, got %q", got)
	}

	full := "---\nname: x\ntags: [a]\n---\n\n# old\n"
	got := mergeFrontmatter(full, body)
	if !strings.HasPrefix(got, "---\nname: x\ntags: [a]\n---") {
		t.Fatalf("frontmatter not preserved: %q", got)
	}
	if !strings.HasSuffix(got, body) {
		t.Fatalf("new body not appended: %q", got)
	}
	if strings.Contains(got, "# old") {
		t.Fatalf("old body must be replaced: %q", got)
	}
}

// ---------------------------------------------------------------------------
// 2026-11-28 审查修复：并发生成、错误上抛、同名冲突保护
// ---------------------------------------------------------------------------

// check-then-generate 必须原子：多会话并发触发同一 pattern 时只允许生成一份。
// 回归点：磁盘查重与写盘之间无互斥，并发时双方都判定不存在、各自生成，
// skillCount 双计、同秒还写进同一目录。
func TestConcurrentCheckDoesNotDuplicateSkills(t *testing.T) {
	dir := t.TempDir()
	ac := NewEnhancedAutoCreator(dir)
	ac.SetMinFrequency(3)

	ac.mu.Lock()
	ac.patterns = []Pattern{
		{
			Name: "Pattern-race", Tools: []string{"a", "b", "c"},
			ToolSequence: []string{"a", "b", "c"},
			Frequency:    5, Confidence: 0.9,
		},
	}
	ac.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ac.CheckAndGenerateSkills()
		}()
	}
	wg.Wait()

	if got := len(ac.GetGeneratedSkills()); got != 1 {
		t.Fatalf("concurrent generation produced %d skill dirs, want exactly 1: %v",
			got, ac.GetGeneratedSkills())
	}
	if got := ac.skillCount.Load(); got != 1 {
		t.Fatalf("skillCount = %d, want 1", got)
	}
}

// 写盘失败必须上抛错误，且不得计数、不得注册内存表。
// 回归点：此前所有 os.WriteFile 错误被吞、函数恒返回 nil，写盘失败仍
// skillCount+1 并 RegisterSkill → "内存有、磁盘无"，重启后技能凭空消失。
func TestGenerateSkillPropagatesWriteErrors(t *testing.T) {
	dir := t.TempDir()
	// 把 auto_skills 变成普通文件，让其下任何 MkdirAll 都失败。
	if err := os.WriteFile(filepath.Join(dir, "auto_skills"), []byte("not a dir"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	ac := NewEnhancedAutoCreator(dir) // 构造器忽略 MkdirAll 错误，兼容这种环境

	err := ac.GenerateSkillFromPattern(Pattern{
		Name: "Pattern-err", Tools: []string{"a", "b", "c"},
		ToolSequence: []string{"a", "b", "c"},
		Frequency:    5, Confidence: 0.9,
	})
	if err == nil {
		t.Fatal("GenerateSkillFromPattern must return an error when the disk write fails")
	}
	if got := ac.skillCount.Load(); got != 0 {
		t.Fatalf("skillCount = %d after failed generation, want 0", got)
	}
}

// 同名冲突保护：已存在非 auto 来源的同名技能时，自动生成不得覆盖它。
// 回归点：此前 RegisterSkill 会把用户技能压进 shadowed 栈（生成侧覆盖），
// 与 loadAutoSkillsInto 的"非 auto 优先"（加载侧保留用户技能）语义相反。
func TestAutoSkillDoesNotShadowUserSkill(t *testing.T) {
	m := newTestManager(t)
	// RegisterSkill 不做名字校验（模拟从磁盘加载进来的同名用户技能；自动技能
	// 名固定带空格，Manager.Add 的名字校验根本放不进来这种名字）。
	m.RegisterSkill(&Skill{
		SkillMeta: SkillMeta{Name: "Automated Pattern-c", Description: "user written", Source: SkillSourceLocal},
		Content:   "# user skill body",
	})

	dir := t.TempDir()
	ac := NewEnhancedAutoCreator(dir)
	ac.SetManager(m)

	if err := ac.GenerateSkillFromPattern(Pattern{
		Name: "Pattern-c", Tools: []string{"a", "b", "c"},
		ToolSequence: []string{"a", "b", "c"},
		Frequency:    5, Confidence: 0.9,
	}); err != nil {
		t.Fatalf("GenerateSkillFromPattern: %v", err)
	}

	got, err := m.Get("Automated Pattern-c")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Source != SkillSourceLocal {
		t.Fatalf("user skill was shadowed by the auto skill (source=%s)", got.Source)
	}
	if got.Description != "user written" {
		t.Fatalf("user skill content replaced: %q", got.Description)
	}

	// 磁盘上的 pending 技能仍应存在（只是不注册进内存表盖掉用户技能）。
	if n := len(ac.GetGeneratedSkills()); n != 1 {
		t.Fatalf("pending skill dir missing: %v", ac.GetGeneratedSkills())
	}
}

// generation.log 必须是追加写：连续两次生成后两条记录都要在。
// 回归点：此前 WriteFile 每次覆盖，"日志"只剩最后一条。
func TestGenerationLogAppends(t *testing.T) {
	dir := t.TempDir()
	ac := NewEnhancedAutoCreator(dir)

	for _, name := range []string{"Pattern-l1", "Pattern-l2"} {
		if err := ac.GenerateSkillFromPattern(Pattern{
			Name: name, Tools: []string{"a", "b", "c"},
			ToolSequence: []string{"a", "b", "c"},
			Frequency:    5, Confidence: 0.9,
		}); err != nil {
			t.Fatalf("GenerateSkillFromPattern(%s): %v", name, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "auto_skills", "generation.log"))
	if err != nil {
		t.Fatalf("read generation.log: %v", err)
	}
	if strings.Count(string(data), "Generated pending skill") != 2 {
		t.Fatalf("generation.log lost earlier entries:\n%s", data)
	}
	if !strings.Contains(string(data), "Pattern-l1") || !strings.Contains(string(data), "Pattern-l2") {
		t.Fatalf("generation.log does not contain both entries:\n%s", data)
	}
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

// newTestManager 构造一个指向临时目录的 Manager，隔离全局 MAGIC_HOME。
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManagerWithConfig(&ManagerConfig{
		SearchDirs: []string{dir},
		BuiltinDir: filepath.Join(dir, "builtin"),
	})
	if err != nil {
		t.Fatalf("NewManagerWithConfig: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}
