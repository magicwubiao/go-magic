package skills

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// Pattern represents a detected usage pattern
type Pattern struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tools        []string `json:"tools"`         // Tools used in order
	ToolSequence []string `json:"tool_sequence"` // Ordered sequence of tool calls
	Frequency    int      `json:"frequency"`     // How many times observed
	Confidence   float64  `json:"confidence"`    // Pattern quality score
	ExampleTasks []string `json:"example_tasks"` // Where this pattern was seen
}

// EnhancedAutoCreator builds on the basic auto-creator with pattern mining
// This is the core of System 6: Skill Self-Evolution
type EnhancedAutoCreator struct {
	baseDir      string
	patterns     []Pattern
	skillCount   atomic.Int64
	minFrequency int      // Minimum occurrences to generate skill
	manager      *Manager // optional: register generated skills
	mu           sync.Mutex
	// genMu 串行化「查重 + 生成」临界区。磁盘上的"已存在"检查与
	// GenerateSkillFromPattern 写盘之间原本没有互斥，多会话并发触发同一
	// pattern 时双方都判定不存在，各自生成一份重复技能（skillCount 双计，
	// skillID 的秒级时间戳同秒还会写进同一目录）。genMu 让这段 check-then-
	// generate 原子化：后到的一方一定能看到先到一方落盘的目录。
	genMu sync.Mutex
}

// DefaultAutoSkillMinFrequency 是"模式出现多少次才允许自动建技能"的默认阈值。
//
// P1-3 数据一致化：此前该阈值散落在三处且默认值互相矛盾 ——
// 构造器硬编码 5，cortex.IntegrationConfig / server / pkg/config 都默认 3，
// 而 cortex 只在 `SkillMinPatternFreq > 0` 时才下推（见 integration.go），
// 于是"配置说 3、实际生效 5"。这里指定唯一默认值，所有默认来源引用它。
const DefaultAutoSkillMinFrequency = config.DefaultAutoSkillMinFrequency

// MinPatternFreqOrDefault 把 0/负数（未配置）归一为默认阈值。
func MinPatternFreqOrDefault(freq int) int {
	if freq <= 0 {
		return DefaultAutoSkillMinFrequency
	}
	return freq
}

// NewEnhancedAutoCreator creates an enhanced skill auto-creator
func NewEnhancedAutoCreator(baseDir string) *EnhancedAutoCreator {
	skillsDir := filepath.Join(baseDir, "auto_skills")
	os.MkdirAll(skillsDir, 0755)
	// 四态子目录：pending / approved / archived / rejected（口径见 autoSkillStateDirs）
	for _, state := range autoSkillStateDirs {
		os.MkdirAll(filepath.Join(skillsDir, state), 0755)
	}

	patternsFile := filepath.Join(skillsDir, "patterns.json")
	var patterns []Pattern
	if data, err := os.ReadFile(patternsFile); err == nil {
		json.Unmarshal(data, &patterns)
	}
	// 清理历史遗留的低质量模式：丢弃只含单一工具的平凡序列，并对示例任务
	// 去重、封顶，避免 patterns.json 长期累积重复噪声数据。
	rawCount := len(patterns)
	patterns = prunePatterns(patterns)
	// prune 只改内存的话要等下一次"有变化"的 save 才落盘；加载期若确实清掉了
	// 噪声数据，立即回写一次，让清理效果马上持久化（与 savePatternsLocked 的
	// "空列表不覆写"口径一致：清空后不写）。
	if rawCount > 0 && len(patterns) > 0 && len(patterns) < rawCount {
		if data, err := json.MarshalIndent(patterns, "", "  "); err == nil {
			_ = os.WriteFile(patternsFile, data, 0644)
		}
	}

	return &EnhancedAutoCreator{
		baseDir:  skillsDir,
		patterns: patterns,
		// P1-3：默认阈值统一走 DefaultAutoSkillMinFrequency（此前硬编码 5 与
		// 配置默认 3 冲突，见该常量注释）。
		minFrequency: DefaultAutoSkillMinFrequency,
	}
}

// SetManager binds a skills Manager so generated skills get registered
func (e *EnhancedAutoCreator) SetManager(mgr *Manager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.manager = mgr
}

// SetBaseDir 更新自动技能根目录（供 cortex 绑定 Manager 时同步路径，
// 避免 SkillCreator 与 Manager 各自维护不同的 auto_skills 路径导致重启后丢失已批准技能）。
// 会确保 pending/approved/archived 子目录存在，并迁移已有的 patterns.json。
func (e *EnhancedAutoCreator) SetBaseDir(dir string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	oldBase := e.baseDir
	e.baseDir = dir
	// 确保四态子目录存在（口径见 autoSkillStateDirs）
	for _, state := range autoSkillStateDirs {
		os.MkdirAll(filepath.Join(dir, state), 0755)
	}
	// 若新目录下没有 patterns.json 但旧目录有，则迁移过去
	newPatternsFile := filepath.Join(dir, "patterns.json")
	oldPatternsFile := filepath.Join(oldBase, "patterns.json")
	if _, err := os.Stat(newPatternsFile); os.IsNotExist(err) {
		if data, err := os.ReadFile(oldPatternsFile); err == nil {
			os.WriteFile(newPatternsFile, data, 0644)
		}
	}
}

// SavePatterns persists patterns to disk
func (e *EnhancedAutoCreator) SavePatterns() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.savePatternsLocked()
}

// savePatternsLocked 持锁保存 patterns（调用者需持有 e.mu）
func (e *EnhancedAutoCreator) savePatternsLocked() error {
	if len(e.patterns) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(e.patterns, "", "  ")
	if err != nil {
		return err
	}
	patternsFile := filepath.Join(e.baseDir, "patterns.json")
	return os.WriteFile(patternsFile, data, 0644)
}

// AnalyzeToolSequence analyzes a sequence of tool calls for patterns
func (e *EnhancedAutoCreator) AnalyzeToolSequence(task string, tools []string) {
	if len(tools) < 3 {
		return // Need at least 3 tools to form a meaningful pattern
	}

	e.mu.Lock()

	changed := false

	// Look for subsequences that repeat across tasks
	for i := 0; i < len(tools)-2; i++ {
		subsequence := tools[i : i+3]

		// 跳过平凡子序列：模式必须涉及至少 2 种不同工具才具备可复用的技能
		// 价值。形如 "execute_command → execute_command → execute_command"
		// 的序列只是“连续执行了几条命令”，几乎匹配任何多步任务，记录下来
		// 只会产生噪声模式。
		if !hasAtLeastTwoDistinctTools(subsequence) {
			continue
		}

		// Check if we've seen this pattern before
		patternKey := strings.Join(subsequence, " → ")
		found := false

		for pIdx := range e.patterns {
			existingKey := strings.Join(e.patterns[pIdx].ToolSequence, " → ")
			if existingKey == patternKey {
				e.patterns[pIdx].Frequency++
				// Update confidence based on frequency
				e.patterns[pIdx].Confidence = 0.5 + float64(e.patterns[pIdx].Frequency)*0.1
				if e.patterns[pIdx].Confidence > 0.95 {
					e.patterns[pIdx].Confidence = 0.95
				}
				addExampleTask(&e.patterns[pIdx], task)
				found = true
				changed = true
				break
			}
		}

		if !found {
			// New pattern detected
			e.patterns = append(e.patterns, Pattern{
				Name:         fmt.Sprintf("Pattern-%d", len(e.patterns)+1),
				Description:  fmt.Sprintf("Detected sequence: %s", patternKey),
				Tools:        subsequence,
				ToolSequence: subsequence,
				Frequency:    1,
				Confidence:   0.5,
				ExampleTasks: []string{task},
			})
			changed = true
		}
	}

	// 持锁保存 patterns 到磁盘。本次分析没有任何变化时不写盘：每轮会话
	// 都无条件 MarshalIndent+WriteFile 是纯磁盘写放大。
	if changed {
		if err := e.savePatternsLocked(); err != nil {
			log.Printf("[SkillCreator] failed to save patterns: %v", err)
		}
	}

	// 拷贝一份 patterns 快照用于后续生成检查，避免长时间持锁做 IO
	snapshot := make([]Pattern, len(e.patterns))
	copy(snapshot, e.patterns)
	e.mu.Unlock()

	// Check if any pattern is ready for skill generation（基于快照，不持锁）
	e.checkAndGenerateSkillsFromSnapshot(snapshot)
}

// maxExampleTasksPerPattern 限制每个模式保存的示例任务数量，避免示例列表
// 随着调用次数无限增长而充斥重复描述。
const maxExampleTasksPerPattern = 10

// hasAtLeastTwoDistinctTools 判断工具序列是否涉及至少 2 种不同工具。只含单一
// 工具的平凡序列不构成可复用技能，应在模式检测时跳过。
func hasAtLeastTwoDistinctTools(seq []string) bool {
	seen := make(map[string]struct{}, len(seq))
	for _, t := range seq {
		seen[t] = struct{}{}
	}
	return len(seen) >= 2
}

// addExampleTask 向模式的示例任务列表追加一条任务，自动跳过重复项，并在达到
// 上限时丢弃最旧的示例，使列表成为近期不同任务的滚动窗口。
func addExampleTask(p *Pattern, task string) {
	if task == "" {
		return
	}
	for _, existing := range p.ExampleTasks {
		if existing == task {
			return // 已记录过相同任务，跳过
		}
	}
	if len(p.ExampleTasks) >= maxExampleTasksPerPattern {
		p.ExampleTasks = p.ExampleTasks[1:]
	}
	p.ExampleTasks = append(p.ExampleTasks, task)
}

// prunePatterns 清理已加载的模式列表：丢弃只含单一工具的平凡序列，并对示例
// 任务去重、封顶，用于在加载历史 patterns.json 时回收之前累积的噪声数据。
func prunePatterns(patterns []Pattern) []Pattern {
	pruned := make([]Pattern, 0, len(patterns))
	for _, p := range patterns {
		if !hasAtLeastTwoDistinctTools(p.ToolSequence) {
			continue // 丢弃平凡单工具模式
		}
		p.ExampleTasks = dedupeExampleTasks(p.ExampleTasks)
		pruned = append(pruned, p)
	}
	return pruned
}

// dedupeExampleTasks 对示例任务去重并封顶到 maxExampleTasksPerPattern 条，
// 保留最近出现的示例。
func dedupeExampleTasks(tasks []string) []string {
	seen := make(map[string]struct{}, len(tasks))
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) > maxExampleTasksPerPattern {
		out = out[len(out)-maxExampleTasksPerPattern:]
	}
	return out
}

// CheckAndGenerateSkills checks patterns and generates skills for those meeting criteria
func (e *EnhancedAutoCreator) CheckAndGenerateSkills() {
	e.mu.Lock()
	snapshot := make([]Pattern, len(e.patterns))
	copy(snapshot, e.patterns)
	e.mu.Unlock()
	e.checkAndGenerateSkillsFromSnapshot(snapshot)
}

// checkAndGenerateSkillsFromSnapshot 基于快照检查并生成技能。
//
// 并发安全：整段「查重 + 生成」在 genMu 内原子执行（见结构体 genMu 注释），
// 快照由调用方提前拷贝，锁内不做长 IO。e.baseDir / e.minFrequency 在入口处
// 持 e.mu 快照——运行期 SetBaseDir / SetMinFrequency 与生成路径并发时不再
// 构成数据竞态（此前这两个字段全程锁外裸读）。
func (e *EnhancedAutoCreator) checkAndGenerateSkillsFromSnapshot(patterns []Pattern) {
	e.genMu.Lock()
	defer e.genMu.Unlock()

	e.mu.Lock()
	baseDir := e.baseDir
	minFrequency := e.minFrequency
	e.mu.Unlock()

	for _, pattern := range patterns {
		// 提高置信度阈值也提高到 0.8，避免低质量的自动生成
		if pattern.Frequency >= minFrequency && pattern.Confidence >= 0.8 {
			// Check if skill already generated for this pattern
			// (look in pending, approved, archived, rejected)
			exists := false
			for _, subdir := range autoSkillStateDirs {
				entries, err := os.ReadDir(filepath.Join(baseDir, subdir))
				if err != nil {
					continue // 状态目录可能尚未创建；忽略错误但**不**当作"不存在"去重复生成——genMu 已保证进程内串行，跨进程由时间戳目录名兜底
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), fmt.Sprintf("auto-%s-", pattern.Name)) && entry.IsDir() {
						exists = true
						break
					}
				}
				if exists {
					break
				}
			}
			if !exists {
				log.Printf("[SkillCreator] Pattern '%s' meets criteria (freq=%d, conf=%.2f), generating skill...",
					pattern.Name, pattern.Frequency, pattern.Confidence)
				if err := e.GenerateSkillFromPattern(pattern); err != nil {
					log.Printf("[SkillCreator] failed to generate skill for pattern '%s': %v",
						pattern.Name, err)
				}
			}
		}
	}
}

// GenerateSkillFromPattern generates a skill file from a detected pattern
// Newly generated skills go to auto_skills/pending/ and won't be used by the Agent
// until the user approves them (via CLI or Web UI).
//
// 错误处理：所有写盘步骤的错误现在逐级上抛（此前全部被吞、恒返回 nil），
// 调用方（checkAndGenerateSkillsFromSnapshot）只打日志不中断其余 pattern。
// skillCount 与 RegisterSkill 只在磁盘写入全部成功后执行，保证"内存已注册"
// 蕴含"磁盘已落盘"，避免重启后技能凭空消失的幽灵状态。
func (e *EnhancedAutoCreator) GenerateSkillFromPattern(pattern Pattern) error {
	// baseDir / manager 持锁快照：与 SetBaseDir / SetManager 并发时不裸读。
	e.mu.Lock()
	baseDir := e.baseDir
	mgr := e.manager
	e.mu.Unlock()

	skillID := fmt.Sprintf("auto-%s-%d", pattern.Name, time.Now().Unix())
	skillName := fmt.Sprintf("Automated %s", pattern.Name)
	// 四态管理：新生成的技能放入 pending 目录
	skillDir := filepath.Join(baseDir, "pending", skillID)
	_, statErr := os.Stat(skillDir)
	dirExisted := statErr == nil
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("[skillgen] create skill dir %s: %w", skillDir, err)
	}
	// 写盘失败时清掉本次新建的半成品目录（目录已存在时不清理，避免误删
	// 同秒碰撞的另一份生成产物——genMu 已挡住进程内的这种情况）。
	fail := func(err error) error {
		if !dirExisted {
			os.RemoveAll(skillDir)
		}
		return err
	}

	// Generate skill metadata
	skillMeta := map[string]interface{}{
		"id":            skillID,
		"name":          skillName,
		"description":   pattern.Description,
		"author":        "cortex-auto",
		"created_at":    time.Now().Format(time.RFC3339),
		"status":        string(SkillStatusPending),
		"pattern_tools": pattern.Tools,
		"frequency":     pattern.Frequency,
		"examples":      pattern.ExampleTasks,
	}

	metaJSON, err := json.MarshalIndent(skillMeta, "", "  ")
	if err != nil {
		return fail(fmt.Errorf("[skillgen] marshal meta for %s: %w", skillID, err))
	}
	if err := os.WriteFile(filepath.Join(skillDir, "meta.json"), metaJSON, 0644); err != nil {
		return fail(fmt.Errorf("[skillgen] write meta.json for %s: %w", skillID, err))
	}

	// Generate SKILL.md - the main skill file using progressive disclosure
	skillMD := e.generateSkillMarkdown(pattern, skillMeta)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		return fail(fmt.Errorf("[skillgen] write SKILL.md for %s: %w", skillID, err))
	}

	// 原子自增：GenerateSkillFromPattern 的调用链（checkAndGenerateSkillsFromSnapshot）
	// 已经**出了 e.mu**，此前这里的 `e.skillCount++` 与 GetStats 的读取构成真实数据竞态。
	// 且只在上面的磁盘写入全部成功后才计数（此前写盘失败也 +1）。
	e.skillCount.Add(1)

	// Register with skills manager so it shows up in /api/skills with pending status
	// Note: pending status skills are NOT injected into Agent context until approved
	//
	// 同名冲突保护：若已有非 auto 来源的同名技能（用户手写/内置/registry 安装），
	// 自动生成不覆盖——与 loadAutoSkillsInto 的"非 auto 优先"口径保持一致。
	// 此前 RegisterSkill 会把用户技能压进 shadowed 栈，直到下次 Reload 才恢复。
	if mgr != nil {
		register := true
		if existing, err := mgr.Get(skillName); err == nil && existing != nil && existing.Source != SkillSourceAuto {
			log.Printf("[SkillCreator] auto skill %q shadowed by existing %s skill of the same name; skipping registration",
				skillName, existing.Source)
			register = false
		}
		if register {
			mgr.RegisterSkill(&Skill{
				SkillMeta: SkillMeta{
					Name:        skillName,
					Description: pattern.Description,
					Version:     "1.0.0",
					Author:      "cortex-auto",
					// 与写进 SKILL.md frontmatter 的 tags 保持一致：状态由 Status
					// 字段（以及所在目录）表达，不再塞进 tags 里当第二份真相。
					Tags:        []string{"auto-generated"},
					Source:      SkillSourceAuto,
					Status:      SkillStatusPending,
					InstalledAt: time.Now(),
				},
				Tools:   uniqueToolNames(pattern.ToolSequence),
				Content: skillMD,
				Dir:     skillDir,
			})
		}
	}

	// Log skill generation（追加写：此前 WriteFile 每次覆盖，"日志"只剩最后一条）
	appendLine(fmt.Sprintf("[%s] Generated pending skill '%s' from pattern seen %d times (dir: %s)\n",
		time.Now().Format(time.RFC3339), pattern.Name, pattern.Frequency, skillDir),
		filepath.Join(baseDir, "generation.log"))

	return nil
}

// appendLine 以追加模式写一行文本；失败只记日志不影响主流程。
func appendLine(line, path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[SkillCreator] failed to open %s: %v", path, err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		log.Printf("[SkillCreator] failed to append to %s: %v", path, err)
	}
}

// generateSkillMarkdown 生成 SKILL.md：YAML frontmatter + 渐进披露正文。
//
// 此前这里只写正文、**没有 frontmatter**，落盘的技能不是一份合法技能文件：
// 任何按 SKILL.md 加载的路径（Reload / 批准后被扫描 / `magic skills show`）
// 只能拿目录名当技能名、默认描述当描述，tags 与 tools 全空——与仓库内其余技能
// 的口径不一致，也让"技能声明自己需要哪些工具"这条校验对自动技能完全失效。
//
// 状态（pending/approved/...）**不写进 frontmatter**：它由所在目录与 meta.json
// 表达，避免同一事实存在两处真相。
func (e *EnhancedAutoCreator) generateSkillMarkdown(pattern Pattern, meta map[string]interface{}) string {
	name, _ := meta["name"].(string)
	tools := uniqueToolNames(pattern.ToolSequence)
	description := strings.TrimSpace(pattern.Description)
	if description == "" {
		description = fmt.Sprintf("Use when the task matches this learned tool sequence: %s.",
			strings.Join(tools, " -> "))
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", skillFrontmatterValue(name)))
	sb.WriteString(fmt.Sprintf("description: %s\n", skillFrontmatterValue(description)))
	sb.WriteString("version: 1.0.0\n")
	sb.WriteString("author: cortex-auto\n")
	sb.WriteString("tags: [auto-generated]\n")
	if len(tools) > 0 {
		sb.WriteString(fmt.Sprintf("tools: [%s]\n", strings.Join(tools, ", ")))
	}
	sb.WriteString("---\n\n")

	// Level 0: Header + brief description (disclosed first)
	sb.WriteString(fmt.Sprintf("# %s\n\n", name))
	sb.WriteString(fmt.Sprintf("**Auto-generated by Cortex Agent** | Pattern seen %d times\n\n", pattern.Frequency))
	sb.WriteString(fmt.Sprintf("## What this skill does\n\n%s\n\n", description))

	// Level 1: Full instructions (disclosed when user actually uses the skill)
	sb.WriteString("## Level 1: Complete Usage Guide\n\n")
	sb.WriteString("### Tool Sequence\n\n")
	sb.WriteString("This skill uses the following tool sequence:\n\n")
	for i, tool := range pattern.ToolSequence {
		sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, tool))
	}

	sb.WriteString("\n### How to Use\n\n")
	if len(pattern.ToolSequence) > 0 {
		sb.WriteString(fmt.Sprintf("When you need to perform this task, start with `%s`:\n", pattern.ToolSequence[0]))
	} else {
		sb.WriteString("When you need to perform this task:\n")
	}
	sb.WriteString("1. Call the first tool with the concrete parameters the task requires\n")
	sb.WriteString("2. Use the result of each step as the input to the next one\n")
	sb.WriteString("3. Stop as soon as the task is satisfied — do not run the sequence for its own sake\n\n")

	sb.WriteString("### Example Tasks\n\n")
	if len(pattern.ExampleTasks) == 0 {
		sb.WriteString("- (no examples captured yet)\n")
	}
	for _, example := range pattern.ExampleTasks {
		sb.WriteString(fmt.Sprintf("- %s\n", example))
	}

	// Level 2: Reference material (disclosed for complex tasks)
	sb.WriteString("\n## Level 2: Reference and Tips\n\n")
	sb.WriteString("### Common Pitfalls\n\n")
	sb.WriteString("- Make sure each tool call completes successfully before proceeding\n")
	sb.WriteString("- Verify intermediate results before moving to complex operations\n")
	sb.WriteString("- If a step fails, consider alternative approaches or ask for clarification\n\n")

	sb.WriteString("### Optimization Tips\n\n")
	sb.WriteString("- Consider parallel execution for independent sub-tasks\n")
	sb.WriteString("- Cache intermediate results when appropriate\n")
	sb.WriteString("- Document any deviations from the standard pattern\n\n")

	sb.WriteString("*This skill was auto-generated by Cortex Agent's pattern recognition system.*\n")
	sb.WriteString("*It will be improved and refined as the pattern is used more frequently.*\n")

	return sb.String()
}

// skillFrontmatterValue 把任意文本变成可安全放进 frontmatter 的双引号标量。
//
// 解析器（parser.parseSimpleYAML）只做"按第一个引号外的冒号切分 + 去掉首尾
// 双引号"，不处理转义，因此这里把内部的 `"` 换成 `'`、把换行压成空格，
// 保证一条 key 永远是单行、且不会提前闭合引号。
func skillFrontmatterValue(v string) string {
	v = strings.ReplaceAll(v, `"`, `'`)
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.Join(strings.Fields(v), " ")
	return `"` + v + `"`
}

// uniqueToolNames 去重并保持出现顺序。
func uniqueToolNames(tools []string) []string {
	seen := make(map[string]struct{}, len(tools))
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// GetPatterns returns all detected patterns（返回拷贝，避免外部修改影响内部状态）
func (e *EnhancedAutoCreator) GetPatterns() []Pattern {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Pattern, len(e.patterns))
	copy(out, e.patterns)
	return out
}

// GetGeneratedSkills returns all auto-generated skills.
//
// P1-3 数据一致化：技能实际写入 `<baseDir>/<状态>/auto-*`（四态子目录），
// 而此前这里只扫 baseDir 顶层。顶层只有 pending/approved/archived/rejected
// 四个目录，没有任何以 "auto-" 开头的条目 → 永远返回空。
// 后果：cortex 每轮会话末尾的 "Generated N new skills" 从不打印，
// 上游也无从得知自动建技能是否真的发生。
func (e *EnhancedAutoCreator) GetGeneratedSkills() []string {
	e.mu.Lock()
	baseDir := e.baseDir
	e.mu.Unlock()

	skills := []string{}
	for _, state := range autoSkillStateDirs {
		entries, err := os.ReadDir(filepath.Join(baseDir, state))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "auto-") {
				skills = append(skills, entry.Name())
			}
		}
	}
	return skills
}

// autoSkillStateDirs 是四态目录名，唯一来源。NewEnhancedAutoCreator /
// SetBaseDir / GetGeneratedSkills 全部引用它，避免再出现"写 pending/、读顶层"
// 这类两侧目录口径不一致。
var autoSkillStateDirs = []string{"pending", "approved", "archived", "rejected"}

// GetStats returns statistics about pattern detection and skills.
//
// P1-3 数据一致化：`skills_generated` 改为以磁盘四态目录中的实际技能数为准，
// 而不是 e.skillCount。计数器的语义是"本进程内生成过几次"，进程重启即归零；
// 而 patterns.json / 技能目录是持久化的。两者混用会让面板在重启后显示
// "patterns_detected=2, skills_generated=0"，与磁盘上真实存在的技能自相矛盾。
func (e *EnhancedAutoCreator) GetStats() map[string]interface{} {
	e.mu.Lock()
	patternsCount := len(e.patterns)
	pendingCount := 0
	for _, p := range e.patterns {
		if p.Frequency < e.minFrequency {
			pendingCount++
		}
	}
	minFreq := e.minFrequency
	e.mu.Unlock()

	return map[string]interface{}{
		"patterns_detected": patternsCount,
		"skills_generated":  len(e.GetGeneratedSkills()),
		"min_frequency":     minFreq,
		"pending_patterns":  pendingCount,
		// 本进程内新建的技能数（易失，重启归零），保留供排查用。
		// 转成 int 输出：JSON 消费方与既有断言都按 int 口径比较。
		"skills_generated_this_run": int(e.skillCount.Load()),
	}
}

// SetMinFrequency sets the minimum frequency threshold for skill generation.
//
// P1-3：0/负数会归一为默认阈值。此前直接赋值，一旦上游传入 0（如配置缺省
// 被解析为 0），阈值就变成 0 —— 条件 `Frequency >= 0` 恒真，任何模式第一次
// 被观测到就会立刻生成技能，自动建技能直接退化成噪声生产线。
func (e *EnhancedAutoCreator) SetMinFrequency(freq int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.minFrequency = MinPatternFreqOrDefault(freq)
}

// GetMinFrequency 返回当前生效的最小模式频率阈值。
func (e *EnhancedAutoCreator) GetMinFrequency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.minFrequency
}

// ExportPatterns exports all patterns for analysis
func (e *EnhancedAutoCreator) ExportPatterns(path string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, err := json.MarshalIndent(e.patterns, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
