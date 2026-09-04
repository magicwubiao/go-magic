package memory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Character limits (not tokens) because char counts are model-independent
// This simulates human memory - you don't remember every word, just the conclusions
const (
	MemoryLimitChars = 2200 // MEMORY.md max chars
	UserLimitChars   = 1375 // USER.md max chars
)

// SnapshotManager implements the "frozen snapshot" pattern from Cortex Agent
// Memory updates are written to disk immediately but
// the current turn uses a frozen snapshot to protect prefix cache
// This is crucial for cost optimization with Anthropic's prefix caching
type SnapshotManager struct {
	mu           sync.RWMutex
	memoryPath   string
	userPath     string
	frozenMemory string // Snapshot for current turn
	frozenUser   string
	latestMemory string // Latest version (on disk)
	latestUser   string
	version      int
	compressor   *MemoryCompressor
}

// MemoryCompressor handles memory summarization when limits are reached.
// Uses a section-aware strategy: preserves structure (headers, key facts)
// while trimming verbose sections and removing duplicates.
type MemoryCompressor struct{}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(baseDir string) *SnapshotManager {
	return &SnapshotManager{
		memoryPath: filepath.Join(baseDir, "MEMORY.md"),
		userPath:   filepath.Join(baseDir, "USER.md"),
		compressor: &MemoryCompressor{},
		version:    1,
	}
}

// Load loads the latest memory from disk
func (sm *SnapshotManager) Load() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if content, err := os.ReadFile(sm.memoryPath); err == nil {
		sm.latestMemory = string(content)
		sm.frozenMemory = sm.latestMemory
	}

	if content, err := os.ReadFile(sm.userPath); err == nil {
		sm.latestUser = string(content)
		sm.frozenUser = sm.latestUser
	}

	return nil
}

// OnTurnStart is called at the beginning of each turn.
// Uses the frozen snapshot for this turn, does NOT refresh.
// This protects prefix cache from being invalidated mid-conversation.
// 快照读取方（GetMemoryForPrompt）自带锁保护，这里无需再持锁，保持空操作即可。
func (sm *SnapshotManager) OnTurnStart() {
	// frozenMemory remains as-is for the entire turn
}

// RefreshSnapshot is called at session end or start of a new conversation.
// Refreshes the frozen snapshot with latest memory.
func (sm *SnapshotManager) RefreshSnapshot() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.frozenMemory = sm.latestMemory
	sm.frozenUser = sm.latestUser
	sm.version++
}

// GetMemoryForPrompt returns the memory content to include in system prompt.
// Uses the frozen snapshot, NOT the latest.
func (sm *SnapshotManager) GetMemoryForPrompt() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.frozenMemory
}

// GetUserForPrompt returns the user profile to include in system prompt.
// Uses the frozen snapshot, NOT the latest.
func (sm *SnapshotManager) GetUserForPrompt() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.frozenUser
}

// UpdateMemory updates memory, writes to disk immediately
// but does NOT refresh the frozen snapshot.
func (sm *SnapshotManager) UpdateMemory(content string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(content) > MemoryLimitChars {
		content = sm.compressor.CompressMemory(content, MemoryLimitChars)
	}

	sm.latestMemory = content
	return os.WriteFile(sm.memoryPath, []byte(content), 0644)
}

// UpdateUser updates user profile, writes to disk immediately
// but does NOT refresh the frozen snapshot.
func (sm *SnapshotManager) UpdateUser(content string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(content) > UserLimitChars {
		content = sm.compressor.compressUser(content, UserLimitChars)
	}

	sm.latestUser = content
	return os.WriteFile(sm.userPath, []byte(content), 0644)
}

// AppendToMemory appends a line to memory (P1-1: 共享分节合并)
func (sm *SnapshotManager) AppendToMemory(line string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	newContent := mergeIntoMarkdown(sm.latestMemory, line, MemoryLimitChars, sm.compressor.CompressMemory)

	sm.latestMemory = newContent
	return os.WriteFile(sm.memoryPath, []byte(newContent), 0644)
}

// AppendToUser appends a line to user profile (P1-1: 共享分节合并)
func (sm *SnapshotManager) AppendToUser(line string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	newContent := mergeIntoMarkdown(sm.latestUser, line, UserLimitChars, sm.compressor.compressUser)

	sm.latestUser = newContent
	return os.WriteFile(sm.userPath, []byte(newContent), 0644)
}

// mergeIntoMarkdown 把新增内容按分节合并进现有 Markdown（P1-1 统一写入门面）：
//   - 新增内容带 "## header" 时合并进现有同名分节（同一主题不再裂成多个分节），
//     新分节追加到末尾；分节体内逐行去重
//   - 新增内容不带分节头时按旧行为直接追加到末尾
//   - 全文去重连续重复行
//   - 超过 limit 时调用 compress 压缩（MEMORY.md 用分节压缩，USER.md 用
//     简单去重截断），由调用方决定压缩策略
//
// 返回合并后的完整内容，不落盘——落盘路径由调用方（SnapshotManager /
// Store 的文件 API）各自持锁完成。
func mergeIntoMarkdown(existing, addition string, limit int, compress func(string, int) string) string {
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		out := addition + "\n"
		if len(out) > limit {
			out = compress(out, limit)
		}
		return out
	}

	existing = strings.TrimRight(existing, "\n")
	addSections := splitSections(addition)

	// 简单追加路径：addition 不含分节头
	hasHeader := false
	for _, s := range addSections {
		if s.header != "" {
			hasHeader = true
			break
		}
	}
	if !hasHeader {
		merged := existing + "\n" + addition + "\n"
		merged = deduplicateLines(merged)
		if len(merged) > limit {
			merged = compress(merged, limit)
		}
		return merged
	}

	// 分节合并路径
	exSections := splitSections(existing)
	headerIndex := make(map[string]int)
	for i, s := range exSections {
		if s.header != "" {
			headerIndex[s.header] = i
		}
	}

	var preamble []string // addition 中首个 "##" 之前的无分节行
	for _, as := range addSections {
		if as.header == "" {
			if strings.TrimSpace(as.body) != "" {
				preamble = append(preamble, strings.TrimSpace(as.body))
			}
			continue
		}
		if i, ok := headerIndex[as.header]; ok {
			exSections[i].body = mergeBodyLines(exSections[i].body, as.body)
		} else {
			exSections = append(exSections, as)
			headerIndex[as.header] = len(exSections) - 1
		}
	}

	var sb strings.Builder
	for i, s := range exSections {
		if s.header != "" {
			sb.WriteString(s.header)
			sb.WriteString("\n")
		}
		sb.WriteString(strings.TrimRight(s.body, "\n"))
		sb.WriteString("\n")
		if i < len(exSections)-1 {
			sb.WriteString("\n")
		}
	}
	if len(preamble) > 0 {
		sb.WriteString("\n")
		sb.WriteString(strings.Join(preamble, "\n"))
		sb.WriteString("\n")
	}

	merged := deduplicateLines(sb.String())
	if len(merged) > limit {
		merged = compress(merged, limit)
	}
	return merged
}

// mergeBodyLines 把 additionBody 中现 body 没有的行追加进现有分节体
func mergeBodyLines(existingBody, additionBody string) string {
	seen := make(map[string]bool)
	for _, l := range strings.Split(existingBody, "\n") {
		t := strings.TrimSpace(l)
		if t != "" {
			seen[t] = true
		}
	}
	var out []string
	for _, l := range strings.Split(additionBody, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, l)
	}
	if len(out) == 0 {
		return existingBody
	}
	body := strings.TrimRight(existingBody, "\n")
	if body != "" {
		body += "\n"
	}
	return body + strings.Join(out, "\n")
}

// GetLatestMemory returns the latest memory (not frozen)
func (sm *SnapshotManager) GetLatestMemory() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.latestMemory
}

// GetLatestUser returns the latest user profile (not frozen)
func (sm *SnapshotManager) GetLatestUser() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.latestUser
}

// GetVersion returns the current memory version
func (sm *SnapshotManager) GetVersion() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.version
}

// CompressMemory uses a section-aware strategy to compress memory:
// 1. Split by sections (## headers)
// 2. Keep all section headers
// 3. Within each section, keep the first and last sentence
// 4. Deduplicate consecutive identical lines
// 5. If still over limit, keep first 60% and last 40% of sections
func (mc *MemoryCompressor) CompressMemory(content string, limit int) string {
	// Step 1: Deduplicate consecutive identical lines
	content = deduplicateLines(content)

	// Step 2: Split into sections by ## headers
	sections := splitSections(content)
	if len(sections) == 0 {
		return truncateString(content, limit)
	}

	// Step 3: Compress each section individually
	compressed := make([]string, 0, len(sections))
	for _, sec := range sections {
		compressed = append(compressed, mc.compressSection(sec))
	}

	result := strings.Join(compressed, "\n\n")

	// Step 4: If still over limit, trim sections from the middle
	if len(result) > limit {
		result = mc.trimSectionsToLimit(sections, limit)
	}

	return result
}

// compressUser compresses user profile with a simpler strategy:
// Keep all unique lines, truncating verbose ones
func (mc *MemoryCompressor) compressUser(content string, limit int) string {
	content = deduplicateLines(content)
	return truncateString(content, limit)
}

// section represents a markdown section with its header and body
type section struct {
	header string
	body   string
}

// splitSections splits content by ## markdown headers
func splitSections(content string) []section {
	lines := strings.Split(content, "\n")
	var sections []section
	var current section

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			// Save previous section
			if current.header != "" || current.body != "" {
				sections = append(sections, current)
			}
			current = section{header: trimmed}
		} else {
			if current.body != "" {
				current.body += "\n"
			}
			current.body += line
		}
	}
	// Don't forget the last section
	if current.header != "" || current.body != "" {
		sections = append(sections, current)
	}

	return sections
}

// compressSection keeps the header and trims the body to key sentences
func (mc *MemoryCompressor) compressSection(sec section) string {
	if sec.header == "" {
		// No header - this is a preamble, keep first 2 lines
		lines := nonEmptyLines(sec.body)
		if len(lines) <= 2 {
			return sec.body
		}
		return strings.Join(lines[:2], "\n")
	}

	bodyLines := nonEmptyLines(sec.body)
	if len(bodyLines) <= 3 {
		return sec.header + "\n" + sec.body
	}

	// Keep first 2 and last line of body
	result := sec.header + "\n"
	result += strings.Join(bodyLines[:2], "\n") + "\n"
	result += bodyLines[len(bodyLines)-1]
	return result
}

// trimSectionsToLimit keeps first 60% and last 40% of sections to fit limit
func (mc *MemoryCompressor) trimSectionsToLimit(sections []section, limit int) string {
	if len(sections) <= 2 {
		result := sections[0].header + "\n" + sections[0].body
		if len(sections) > 1 {
			result += "\n\n" + sections[1].header + "\n" + sections[1].body
		}
		return truncateString(result, limit)
	}

	// Keep first 60% of sections
	headCount := len(sections) * 3 / 5
	if headCount < 1 {
		headCount = 1
	}
	tailCount := len(sections) - headCount
	if tailCount < 1 {
		tailCount = 1
		headCount = len(sections) - 1
	}

	var sb strings.Builder
	for i := 0; i < headCount; i++ {
		sb.WriteString(sections[i].header)
		sb.WriteString("\n")
		sb.WriteString(sections[i].body)
		sb.WriteString("\n\n")
	}

	sb.WriteString("[... compressed ...]\n\n")

	for i := len(sections) - tailCount; i < len(sections); i++ {
		sb.WriteString(sections[i].header)
		sb.WriteString("\n")
		sb.WriteString(sections[i].body)
		if i < len(sections)-1 {
			sb.WriteString("\n\n")
		}
	}

	result := sb.String()
	if len(result) > limit {
		return truncateString(result, limit)
	}
	return result
}

// deduplicateLines removes consecutive identical lines
func deduplicateLines(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 1 {
		return content
	}

	filtered := make([]string, 0, len(lines))
	prev := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			filtered = append(filtered, line)
			continue
		}
		if trimmed != prev {
			filtered = append(filtered, line)
			prev = trimmed
		}
		// Skip consecutive duplicates
	}

	return strings.Join(filtered, "\n")
}

// nonEmptyLines returns non-empty trimmed lines from content
func nonEmptyLines(content string) []string {
	lines := strings.Split(content, "\n")
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

func truncateString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	truncated := s[:limit]
	// Walk back to a valid UTF-8 rune boundary so we don't cut a multi-byte
	// character in half (which would produce invalid UTF-8 in the output).
	for len(truncated) > 0 {
		r, size := utf8.DecodeLastRuneInString(truncated)
		if r == utf8.RuneError && size == 1 {
			truncated = truncated[:len(truncated)-1]
		} else {
			break
		}
	}
	if len(truncated) > 3 {
		truncated = truncated[:len(truncated)-3]
	}
	return truncated + "..."
}
