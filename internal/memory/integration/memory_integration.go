package integration

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// preferencePatterns defines patterns for extracting user preferences
var preferencePatterns = []*struct {
	pattern *regexp.Regexp
	category string
	importance float64
}{
	// User preferences
	{regexp.MustCompile(`(?i)I (prefer|like|love|hate|dislike) (.+)`), "preference", 0.7},
	{regexp.MustCompile(`(?i)my favorite (.+) is (.+)`), "preference", 0.7},
	{regexp.MustCompile(`(?i)I usually (.+)`), "preference", 0.6},
	{regexp.MustCompile(`(?i)(don't|do not) (.+)`), "preference", 0.6},

	// Important facts
	{regexp.MustCompile(`(?i)I am (a |an )?(.+?)(?:,|\.|!|\n|$)`), "user_fact", 0.6},
	{regexp.MustCompile(`(?i)I work (?:as |at |in )?(.+?)(?:,|\.|!|\n|$)`), "work_info", 0.7},
	{regexp.MustCompile(`(?i)I live (?:in |at )?(.+?)(?:,|\.|!|\n|$)`), "location", 0.5},

	// Task reminders
	{regexp.MustCompile(`(?i)(remind me|todo|to-do|task):? (.+)`), "task_reminder", 0.8},
	{regexp.MustCompile(`(?i)(remember to|don't forget to|don't forget):? (.+)`), "task_reminder", 0.8},
	{regexp.MustCompile(`(?i)(ASAP|urgent|important):? (.+)`), "task_reminder", 0.9},

	// Contact info
	{regexp.MustCompile(`(?i)(?:email|e-mail):?\s*([\w.-]+@[\w.-]+\.\w+)`), "contact", 0.8},
	{regexp.MustCompile(`(?i)(?:phone|tel):?\s*([\d\s-]+)`), "contact", 0.8},
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

// StoreFromConversation extracts and stores key information from conversation
func (m *MemoryIntegration) StoreFromConversation(userMsg, agentMsg string) error {
	// Store user preferences and important information
	// This is a simple implementation - a more sophisticated one would use LLM

	// Example: extract potential user info patterns
	if len(userMsg) > 10 && len(userMsg) < 500 {
		mem := &memory.Memory{
			Type:       memory.TypeSession,
			Content:    userMsg,
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

	if len(memories) < 10 {
		return nil // Not enough to compact
	}

	// Keep recent memories, summarize older ones
	keepRecent := 5
	sumarizeOlder := memories[:len(memories)-keepRecent]

	// Build summary
	var summary strings.Builder
	summary.WriteString("Session summary:\n")
	for _, mem := range sumarizeOlder {
		if len(mem.Content) > 100 {
			summary.WriteString("- " + mem.Content[:100] + "...\n")
		} else {
			summary.WriteString("- " + mem.Content + "\n")
		}
	}

	// Store summary as new memory
	summaryMem := &memory.Memory{
		Type:       memory.TypeSession,
		Content:    summary.String(),
		Importance: 0.7,
		SessionID:  m.sessionID,
		Source:     "compact",
		Categories: []string{"summary"},
	}

	// Delete old memories
	for _, mem := range sumarizeOlder {
		m.store.Delete(mem.ID)
	}

	// Store new summary
	return m.store.Store(summaryMem)
}

// PeriodicRecall performs periodic recall to refresh context
func (m *MemoryIntegration) PeriodicRecall(ctx context.Context, keywords []string) ([]*memory.Memory, error) {
	var allMemories []*memory.Memory

	for _, kw := range keywords {
		memories, err := m.store.Recall(kw, 3)
		if err != nil {
			continue
		}
		allMemories = append(allMemories, memories...)
	}

	return allMemories, nil
}

// Stats returns memory statistics
func (m *MemoryIntegration) Stats() (*memory.MemoryStats, error) {
	return m.store.Stats()
}
