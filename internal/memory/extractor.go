package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// MemoryExtractor handles semantic extraction of memories from conversations
type MemoryExtractor struct {
	mu       sync.RWMutex
	provider provider.Provider
	enabled  bool
	store    *Store

	// Configuration
	autoExtract  bool
	batchSize    int
	extractEvery int // Extract every N turns
}

// MemoryExtractorConfig configures the memory extractor
type MemoryExtractorConfig struct {
	Enabled      bool
	AutoExtract  bool
	BatchSize    int
	ExtractEvery int
}

// DefaultMemoryExtractorConfig returns default config
func DefaultMemoryExtractorConfig() MemoryExtractorConfig {
	return MemoryExtractorConfig{
		Enabled:      true,
		AutoExtract:  true,
		BatchSize:    5,
		ExtractEvery: 3,
	}
}

// NewMemoryExtractor creates a new memory extractor
func NewMemoryExtractor(prov provider.Provider, store *Store, cfg MemoryExtractorConfig) *MemoryExtractor {
	return &MemoryExtractor{
		provider:     prov,
		store:        store,
		enabled:      cfg.Enabled,
		autoExtract:  cfg.AutoExtract,
		batchSize:    cfg.BatchSize,
		extractEvery: cfg.ExtractEvery,
	}
}

// ShouldExtract checks if it's time to extract memories
func (me *MemoryExtractor) ShouldExtract(turn int) bool {
	if !me.enabled || !me.autoExtract {
		return false
	}
	return turn > 0 && turn%me.extractEvery == 0
}

// ExtractMemories extracts memories from a conversation segment using LLM
func (me *MemoryExtractor) ExtractMemories(ctx context.Context, messages []provider.Message, sessionID string) ([]*Memory, error) {
	if !me.enabled || me.provider == nil {
		return nil, nil
	}

	var contextBuilder strings.Builder
	for _, msg := range messages {
		content := msg.Content
		if len(content) > 1000 {
			content = content[:1000] + "... [truncated]"
		}
		contextBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, content))
	}

	prompt := fmt.Sprintf(`Analyze the following conversation and extract key information to store in long-term memory.

Conversation:
%s

Extract memories in JSON format. For each memory, provide:
- content: The actual information to remember (concise, specific)
- importance: 0.0 - 1.0 (how important is this to remember long-term)
- type: "user" (user preferences/info), "session" (session context), "knowledge" (general facts), "project" (project info), "preference" (user preferences)
- categories: Array of relevant tags
- source: Where this came from (user, tool_output, assistant, etc.)

Only extract information that is:
1. Factually stated (not speculative)
2. Likely to be useful in the future
3. Specific enough to be actionable

Return ONLY a JSON array of memory objects. Example:
[
  {
    "content": "User prefers TypeScript over JavaScript",
    "importance": 0.8,
    "type": "preference",
    "categories": ["typescript", "javascript", "language"],
    "source": "user"
  }
]`, contextBuilder.String())

	resp, err := me.provider.Chat(ctx, []provider.Message{
		{Role: "system", Content: "You are a memory extraction assistant. Extract key facts and preferences from conversations."},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Warnf("[Memory] LLM extraction failed: %v", err)
		return nil, err
	}

	memories, err := me.parseExtractedMemories(resp.Content, sessionID)
	if err != nil {
		log.Warnf("[Memory] Failed to parse extracted memories: %v", err)
		return nil, err
	}

	log.Infof("[Memory] Extracted %d memories from conversation", len(memories))
	return memories, nil
}

func (me *MemoryExtractor) parseExtractedMemories(content string, sessionID string) ([]*Memory, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found in response")
	}
	content = content[start : end+1]

	var rawMemories []struct {
		Content    string   `json:"content"`
		Importance float64  `json:"importance"`
		Type       string   `json:"type"`
		Categories []string `json:"categories"`
		Source     string   `json:"source"`
	}

	if err := json.Unmarshal([]byte(content), &rawMemories); err != nil {
		return nil, fmt.Errorf("failed to parse memory JSON: %w", err)
	}

	now := time.Now().UTC()
	memories := make([]*Memory, 0, len(rawMemories))
	for _, rm := range rawMemories {
		if strings.TrimSpace(rm.Content) == "" {
			continue
		}

		memType := MemoryType(rm.Type)
		switch memType {
		case TypeAgent, TypeUser, TypeSession, TypeProject, TypeKnowledge, TypePreference:
		default:
			memType = TypeKnowledge
		}

		imp := rm.Importance
		if imp < 0 {
			imp = 0.3
		}
		if imp > 1 {
			imp = 1.0
		}

		metadata := map[string]interface{}{
			"confidence":     0.8,
			"decay_factor":   calculateDecayFactor(imp),
			"extracted_from": "llm",
		}
		metadataJSON, _ := json.Marshal(metadata)

		mem := &Memory{
			ID:          generateID(),
			Type:        memType,
			Content:     rm.Content,
			Categories:  rm.Categories,
			Importance:  imp,
			Metadata:    string(metadataJSON),
			CreatedAt:   now,
			UpdatedAt:   now,
			LastAccess:  now,
			AccessCount: 0,
			SessionID:   sessionID,
			Source:      rm.Source,
		}
		memories = append(memories, mem)
	}

	return memories, nil
}

func calculateDecayFactor(importance float64) float64 {
	// Higher importance = slower decay
	// decay_factor represents daily decay rate
	switch {
	case importance >= 0.9:
		return 0.001 // Very slow decay
	case importance >= 0.7:
		return 0.005
	case importance >= 0.5:
		return 0.02
	case importance >= 0.3:
		return 0.05
	default:
		return 0.1 // Fast decay for low importance
	}
}

// StoreMemories stores extracted memories
func (me *MemoryExtractor) StoreMemories(memories []*Memory) error {
	if me.store == nil {
		return nil
	}

	for _, mem := range memories {
		if err := me.store.Store(mem); err != nil {
			log.Warnf("[Memory] Failed to store memory: %v", err)
		}
	}

	return nil
}

// CalculateRelevanceScore calculates the relevance of a memory considering decay
func CalculateRelevanceScore(mem *Memory, query string, now time.Time) float64 {
	if mem == nil {
		return 0
	}

	// Base relevance from content match
	baseScore := calculateContentRelevance(mem.Content, query)
	if baseScore <= 0 {
		return 0
	}

	// Importance multiplier (0.5 - 1.5 range)
	importanceBoost := 0.5 + mem.Importance

	// Recency decay
	recencyFactor := 1.0
	if !mem.CreatedAt.IsZero() {
		age := now.Sub(mem.CreatedAt)
		days := age.Hours() / 24

		decayFactor := 0.02 // Default 2% per day
		if mem.Metadata != "" {
			var meta map[string]interface{}
			if err := json.Unmarshal([]byte(mem.Metadata), &meta); err == nil {
				if df, ok := meta["decay_factor"]; ok {
					if dfVal, ok := df.(float64); ok && dfVal > 0 {
						decayFactor = dfVal
					}
				}
			}
		}

		// Exponential decay
		recencyFactor = math.Exp(-days * decayFactor)
	}

	// Access frequency boost
	accessBoost := 1.0
	if mem.AccessCount > 0 {
		accessBoost = 1.0 + math.Log10(float64(mem.AccessCount)+1)*0.3
	}

	score := baseScore * importanceBoost * recencyFactor * accessBoost
	return score
}

func calculateContentRelevance(content, query string) float64 {
	contentLower := strings.ToLower(content)
	queryLower := strings.ToLower(query)

	// Exact match bonus
	if strings.Contains(contentLower, queryLower) {
		return 1.0
	}

	// Word overlap
	queryWords := strings.Fields(queryLower)
	contentWords := strings.Fields(contentLower)

	if len(queryWords) == 0 {
		return 0
	}

	matchCount := 0.0
	meaningfulWords := 0
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"to": true, "for": true, "of": true, "in": true, "on": true,
		"and": true, "or": true, "with": true, "by": true, "from": true,
		"it": true, "this": true, "that": true, "be": true, "do": true,
		"how": true, "what": true, "why": true, "when": true, "where": true,
	}

	for _, qw := range queryWords {
		qw = strings.Trim(qw, ".,!?;:\"'()[]{}")
		if len(qw) < 2 || stopWords[qw] {
			continue
		}
		meaningfulWords++
		for _, cw := range contentWords {
			cw = strings.Trim(cw, ".,!?;:\"'()[]{}")
			if qw == cw {
				matchCount++
				break
			}
			if strings.HasPrefix(cw, qw) || strings.HasPrefix(qw, cw) {
				matchCount += 0.5
				break
			}
		}
	}

	if meaningfulWords == 0 {
		return 0
	}

	return float64(matchCount) / float64(meaningfulWords)
}

// GetTopMemories retrieves the most relevant memories considering all factors
func (me *MemoryExtractor) GetTopMemories(query string, limit int, now time.Time, memTypes ...MemoryType) []*Memory {
	if me.store == nil {
		return nil
	}

	memories, err := me.store.Recall(query, limit*3, memTypes...)
	if err != nil {
		log.Warnf("[Memory] Recall failed: %v", err)
		return nil
	}

	type scoredMem struct {
		mem   *Memory
		score float64
	}

	scored := make([]scoredMem, 0, len(memories))
	for _, m := range memories {
		score := CalculateRelevanceScore(m, query, now)
		scored = append(scored, scoredMem{mem: m, score: score})
	}

	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[i].score < scored[j].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	if limit > len(scored) {
		limit = len(scored)
	}

	result := make([]*Memory, 0, limit)
	for i := 0; i < limit; i++ {
		if scored[i].score > 0.01 {
			result = append(result, scored[i].mem)
		}
	}

	return result
}

// SummarizeMemories generates a summary of retrieved memories for injection
func SummarizeMemories(memories []*Memory) string {
	if len(memories) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n=== Relevant Memories ===\n")

	for i, m := range memories {
		impLabel := ""
		switch {
		case m.Importance >= 0.9:
			impLabel = "[critical]"
		case m.Importance >= 0.7:
			impLabel = "[high]"
		case m.Importance >= 0.5:
			impLabel = "[med]"
		default:
			impLabel = "[low]"
		}

		typeLabel := fmt.Sprintf("[%s]", m.Type)

		sb.WriteString(fmt.Sprintf("%d. %s%s %s\n", i+1, impLabel, typeLabel, m.Content))
	}

	sb.WriteString("\nUse these memories to inform your response.\n")
	return sb.String()
}
