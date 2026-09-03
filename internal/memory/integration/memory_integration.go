package integration

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// preferencePatterns defines patterns for extracting user preferences
var preferencePatterns = []*struct {
	pattern    *regexp.Regexp
	category   string
	importance float64
}{
	// User preferences (English)
	{regexp.MustCompile(`(?i)I (prefer|like|love|hate|dislike) (.+)`), "preference", 0.7},
	{regexp.MustCompile(`(?i)my favorite (.+) is (.+)`), "preference", 0.7},
	{regexp.MustCompile(`(?i)I usually (.+)`), "preference", 0.6},
	{regexp.MustCompile(`(?i)(don't|do not) (.+)`), "preference", 0.6},

	// User preferences (Chinese)
	{regexp.MustCompile(`(?:我|俺|咱)(?:喜欢|偏爱|偏好|讨厌|不喜欢|爱用)(.{1,40})`), "preference", 0.7},
	{regexp.MustCompile(`我最喜欢的是(.{1,40})`), "preference", 0.7},
	{regexp.MustCompile(`我通常(.{1,40})`), "preference", 0.6},

	// Important facts
	{regexp.MustCompile(`(?i)I am (a |an )?(.+?)(?:,|\.|!|\n|$)`), "user_fact", 0.6},
	{regexp.MustCompile(`(?i)I work (?:as |at |in )?(.+?)(?:,|\.|!|\n|$)`), "work_info", 0.7},
	{regexp.MustCompile(`(?i)I live (?:in |at )?(.+?)(?:,|\.|!|\n|$)`), "location", 0.5},

	// Important facts (Chinese)
	{regexp.MustCompile(`我(?:是|在做|在用)(.{1,40})`), "user_fact", 0.6},
	{regexp.MustCompile(`我(?:住在|坐标)(.{1,20})`), "location", 0.5},
	{regexp.MustCompile(`我在(.{1,15}?)(?:公司|团队|项目)工作`), "work_info", 0.7},

	// Task reminders
	{regexp.MustCompile(`(?i)(remind me|todo|to-do|task):? (.+)`), "task_reminder", 0.8},
	{regexp.MustCompile(`(?i)(remember to|don't forget to|don't forget):? (.+)`), "task_reminder", 0.8},
	{regexp.MustCompile(`(?i)(ASAP|urgent|important):? (.+)`), "task_reminder", 0.9},

	// Task reminders (Chinese)
	{regexp.MustCompile(`(?:提醒我|记得|别忘了?|记得要)(.{1,60})`), "task_reminder", 0.8},
	{regexp.MustCompile(`(?:紧急|尽快|重要)[:：,，]?(.{1,60})`), "task_reminder", 0.9},

	// Contact info
	{regexp.MustCompile(`(?i)(?:email|e-mail|邮箱):?\s*([\w.-]+@[\w.-]+\.\w+)`), "contact", 0.8},
	{regexp.MustCompile(`(?i)(?:phone|tel|电话|手机)[:：]?\s*([+\d][\d\s-]{5,})`), "contact", 0.8},
}

// keywordPatterns define importance based on keywords
var keywordPatterns = map[string]float64{
	"urgent":    0.9,
	"important": 0.8,
	"critical":  0.9,
	"asap":      0.9,
	"deadline":  0.8,
	"meeting":   0.6,
	"project":   0.7,
	"client":    0.6,
	"boss":      0.5,
	"manager":   0.5,
}

// MemoryIntegration provides integration between agent and memory system
type MemoryIntegration struct {
	store       *memory.Store
	sessionID   string
	recallLimit int
	autoRecall  bool
}

// NewMemoryIntegration creates a new memory integration
func NewMemoryIntegration(store *memory.Store) *MemoryIntegration {
	return &MemoryIntegration{
		store:       store,
		recallLimit: 5,
		autoRecall:  true,
	}
}

// SetSession sets the current session ID
func (m *MemoryIntegration) SetSession(sessionID string) {
	m.sessionID = sessionID
}

// EnableAutoRecall enables automatic memory recall
func (m *MemoryIntegration) EnableAutoRecall(enabled bool) {
	m.autoRecall = enabled
}

// Recall retrieves relevant memories for a query
func (m *MemoryIntegration) Recall(query string, memoryTypes ...memory.MemoryType) ([]*memory.Memory, error) {
	return m.store.Recall(query, m.recallLimit, memoryTypes...)
}

// Store saves a new memory
func (m *MemoryIntegration) Store(mem *memory.Memory) error {
	if m.sessionID != "" {
		mem.SessionID = m.sessionID
	}
	return m.store.Store(mem)
}

// StoreFromConversation extracts and stores key information from conversation.
// 旧实现把每条 10~500 字的用户消息无条件入库（TypeSession/0.5），
// 长期运行会制造大量低质噪声稀释召回质量，agentMsg 也被完全忽略。
// 现在只抽取结构化信息（偏好/事实/提醒/联系方式），
// 未命中模式但含重要关键词时才降级存整条消息。
func (m *MemoryIntegration) StoreFromConversation(userMsg, agentMsg string) error {
	if len(userMsg) == 0 || len(userMsg) > 2000 {
		return nil
	}

	// 复用 pattern 抽取器处理用户消息
	if err := m.ExtractAndStore([]struct {
		Role    string
		Content string
	}{{Role: "user", Content: userMsg}}); err != nil {
		return err
	}

	// 助手消息通常较长，只挑其中带关键洞察的短句入库，
	// 且必须有重要关键词背书，避免污染
	if agentMsg != "" && len(agentMsg) < 500 && m.hasImportantKeywords(agentMsg) {
		mem := &memory.Memory{
			Type:       memory.TypeKnowledge,
			Content:    strings.TrimSpace(agentMsg),
			Importance: 0.5,
			SessionID:  m.sessionID,
			Source:     "conversation",
		}
		return m.store.Store(mem)
	}
	return nil
}

// BuildContext builds a context string from relevant memories
func (m *MemoryIntegration) BuildContext(query string) (string, error) {
	if !m.autoRecall {
		return "", nil
	}

	// Recall relevant memories
	memories, err := m.store.Recall(query, m.recallLimit)
	if err != nil {
		return "", err
	}

	if len(memories) == 0 {
		return "", nil
	}

	// Build context string
	var context strings.Builder
	context.WriteString("\n\n[Relevant memories]\n")

	for _, mem := range memories {
		if mem.Type == memory.TypeUser || mem.Type == memory.TypePreference {
			context.WriteString("User preference: ")
		} else if mem.Type == memory.TypeAgent {
			context.WriteString("Note: ")
		}
		context.WriteString(mem.Content)
		context.WriteString("\n")
	}

	return context.String(), nil
}

// ExtractAndStore extracts key information from the conversation using pattern matching
func (m *MemoryIntegration) ExtractAndStore(conversation []struct {
	Role    string
	Content string
}) error {
	if m.store == nil {
		return nil
	}

	for _, msg := range conversation {
		if msg.Role != "user" {
			continue
		}

		// Extract using pattern matching
		extractions := m.extractFromText(msg.Content)

		for _, ext := range extractions {
			mem := &memory.Memory{
				Type:       m.getMemoryType(ext.Category),
				Content:    ext.Content,
				Importance: ext.Importance,
				SessionID:  m.sessionID,
				Source:     "extract",
				Categories: []string{ext.Category},
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				LastAccess: time.Now(),
			}

			if err := m.store.Store(mem); err != nil {
				log.Warnf("Failed to store extracted memory: %v", err)
			}
		}

		// Also store the full message if it contains important keywords
		if len(extractions) == 0 && m.hasImportantKeywords(msg.Content) {
			mem := &memory.Memory{
				Type:       memory.TypeSession,
				Content:    msg.Content,
				Importance: 0.6,
				SessionID:  m.sessionID,
				Source:     "conversation",
				Categories: []string{"conversation"},
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				LastAccess: time.Now(),
			}

			if err := m.store.Store(mem); err != nil {
				log.Warnf("Failed to store conversation memory: %v", err)
			}
		}
	}
	return nil
}

// extraction represents a single extraction result
type extraction struct {
	Content    string
	Category   string
	Importance float64
}

// extractFromText extracts structured information from text using patterns
func (m *MemoryIntegration) extractFromText(text string) []extraction {
	var results []extraction

	for _, p := range preferencePatterns {
		matches := p.pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}

			// Build extraction content
			var content string
			if len(match) == 3 {
				content = match[0] // Full match
			} else {
				content = match[0]
			}

			// Calculate dynamic importance
			importance := p.importance
			for keyword, weight := range keywordPatterns {
				if strings.Contains(strings.ToLower(text), keyword) {
					importance = weight
					break
				}
			}

			results = append(results, extraction{
				Content:    strings.TrimSpace(content),
				Category:   p.category,
				Importance: importance,
			})
		}
	}

	return results
}

// getMemoryType maps category to memory type
func (m *MemoryIntegration) getMemoryType(category string) memory.MemoryType {
	switch category {
	case "preference":
		return memory.TypePreference
	case "user_fact", "work_info", "location":
		return memory.TypeUser
	case "task_reminder":
		return memory.TypeSession
	case "contact":
		return memory.TypeUser
	default:
		return memory.TypeSession
	}
}

// hasImportantKeywords checks if text contains important keywords
func (m *MemoryIntegration) hasImportantKeywords(text string) bool {
	lower := strings.ToLower(text)
	for keyword := range keywordPatterns {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// UpdateAgentMemory updates the agent's persistent memory
func (m *MemoryIntegration) UpdateAgentMemory(content string) error {
	return m.store.AppendAgentMemory(content)
}

// UpdateUserMemory updates the user's persistent profile
func (m *MemoryIntegration) UpdateUserMemory(content string) error {
	return m.store.AppendUserMemory(content)
}

// GetUserMemory retrieves user memory
func (m *MemoryIntegration) GetUserMemory() (string, error) {
	return m.store.ReadUserMemory()
}

// GetAgentMemory retrieves agent memory
func (m *MemoryIntegration) GetAgentMemory() (string, error) {
	return m.store.ReadAgentMemory()
}

// RecordCommandAction records a command action for learning
func (m *MemoryIntegration) RecordCommandAction(command, action string) error {
	return m.store.RecordCommandAction(command, action, m.sessionID)
}

// GetCommandTrustLevel returns how trusted a command is
func (m *MemoryIntegration) GetCommandTrustLevel(commandHash string) (string, int, error) {
	return m.store.GetCommandTrustLevel(commandHash)
}

// GetSessionMemories retrieves all memories from the current session
func (m *MemoryIntegration) GetSessionMemories() ([]*memory.Memory, error) {
	return m.store.List(memory.TypeSession, 50, 0)
}

// CompactSessionMemories compacts session memories when session is long
func (m *MemoryIntegration) CompactSessionMemories() error {
	memories, err := m.GetSessionMemories()
	if err != nil {
		return err
	}

	// List() 按 importance DESC 排序，不能直接当作时间序使用；
	// 这里按 created_at 升序重排，保证"旧的被压缩、新的被保留"的语义正确。
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt.Before(memories[j].CreatedAt)
	})

	if len(memories) < 10 {
		return nil // Not enough to compact
	}

	keepRecent := 5
	sumarizeOlder := memories[:len(memories)-keepRecent]

	// Build summary
	var summary strings.Builder
	summary.WriteString("Session summary:\n")
	for _, mem := range sumarizeOlder {
		content := []rune(mem.Content)
		if len(content) > 100 {
			summary.WriteString("- " + string(content[:100]) + "...\n")
		} else {
			summary.WriteString("- " + mem.Content + "\n")
		}
	}

	summaryMem := &memory.Memory{
		Type:       memory.TypeSession,
		Content:    summary.String(),
		Importance: 0.7,
		SessionID:  m.sessionID,
		Source:     "compact",
		Categories: []string{"summary"},
	}

	// 先写入 summary，再删除旧记忆，避免 summary 写失败导致数据丢失
	if err := m.store.Store(summaryMem); err != nil {
		return fmt.Errorf("failed to store session summary, skipping compaction: %w", err)
	}

	for _, mem := range sumarizeOlder {
		if err := m.store.Delete(mem.ID); err != nil {
			log.Warnf("failed to delete compacted memory %s: %v", mem.ID, err)
		}
	}

	return nil
}

// PeriodicRecall performs periodic recall to refresh context
func (m *MemoryIntegration) PeriodicRecall(ctx context.Context, keywords []string) ([]*memory.Memory, error) {
	var allMemories []*memory.Memory
	seen := make(map[string]bool) // 按 ID 去重，避免多关键词命中同一条记忆

	for _, kw := range keywords {
		memories, err := m.store.Recall(kw, 3)
		if err != nil {
			continue
		}
		for _, mem := range memories {
			if seen[mem.ID] {
				continue
			}
			seen[mem.ID] = true
			allMemories = append(allMemories, mem)
		}
	}

	return allMemories, nil
}

// Stats returns memory statistics
func (m *MemoryIntegration) Stats() (*memory.MemoryStats, error) {
	return m.store.Stats()
}
