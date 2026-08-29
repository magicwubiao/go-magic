package cortex

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// SoulManager manages the system personality (SOUL.md)
type SoulManager struct {
	baseDir     string
	soulPath    string
	content     string
	mu          sync.RWMutex
	defaultSoul string
}

// DefaultSoul is the default personality when no SOUL.md exists
const DefaultSoul = `You are Magic, a helpful AI assistant powered by go-magic framework.

Your core traits:
- Helpful and knowledgeable
- Precise and practical
- Growth-oriented, learning from interactions
- Transparent about your capabilities and limitations

Your capabilities:
- File operations (read, write, edit)
- Web search and content extraction
- Code execution and debugging
- Task planning and decomposition
- Memory and learning from past interactions

Guidelines:
- Be concise and actionable
- Ask clarifying questions when needed
- Admit when you don't know something
- Learn from user preferences and feedback
`

// NewSoulManager creates a new soul manager
func NewSoulManager(baseDir string) *SoulManager {
	return &SoulManager{
		baseDir:     baseDir,
		soulPath:    filepath.Join(baseDir, "SOUL.md"),
		content:     DefaultSoul,
		defaultSoul: DefaultSoul,
	}
}

// Load loads the SOUL.md file from disk
func (m *SoulManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.soulPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create default SOUL.md
			m.content = m.defaultSoul
			return m.save()
		}
		return err
	}

	m.content = string(data)
	return nil
}

// save saves the soul content to disk
func (m *SoulManager) save() error {
	return os.WriteFile(m.soulPath, []byte(m.content), 0644)
}

// GetSoul returns the current soul/personality
func (m *SoulManager) GetSoul() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.content
}

// SetSoul updates the soul/personality.
// Enforces a maximum size limit; trims oldest Learned Preferences if exceeded.
func (m *SoulManager) SetSoul(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	const maxSoulSize = 64 * 1024
	if len(content) > maxSoulSize {
		for len(content) > maxSoulSize {
			lpMarker := "## Learned Preferences"
			idx := strings.Index(content, lpMarker)
			if idx == -1 {
				cut := content[len(content)-maxSoulSize:]
				for len(cut) > 0 && !utf8.RuneStart(cut[0]) {
					cut = cut[1:]
				}
				content = cut
				break
			}
			nextSection := strings.Index(content[idx+1:], "\n## ")
			if nextSection == -1 {
				content = content[:idx]
			} else {
				end := idx + 1 + nextSection
				content = content[:idx] + content[end:]
			}
		}
	}

	m.content = content
	return m.save()
}

// UpdateFromFeedback updates the soul based on user feedback
func (m *SoulManager) UpdateFromFeedback(feedback string) error {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return nil
	}

	// 仅在反馈中包含明确的偏好信号时才更新 SOUL，
	// 排除 "I would like"/"It always depends" 等非偏好句式，降低误报率。
	preference := extractPreference(feedback)
	if preference == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.Contains(m.content, preference) {
		return nil
	}

	const maxSoulSize = 64 * 1024
	update := "\n\n## Learned Preferences\n" + preference + "\n(Auto-generated from interactions)"
	newContent := m.content + update
	if len(newContent) > maxSoulSize {
		// Trim the oldest content to stay within the limit rather than wiping
		// everything back to defaultSoul (which would discard all learning).
		// 按字节回退到有效的 UTF-8 边界，避免在多字节字符（如中文）中间截断造成乱码。
		cut := newContent[len(newContent)-maxSoulSize:]
		for len(cut) > 0 && !utf8.RuneStart(cut[0]) {
			cut = cut[1:]
		}
		newContent = cut
	}
	m.content = newContent

	return m.save()
}

// extractPreference 从反馈文本中抽取明确的用户偏好。
// 要求带具体动词的偏好信号（如 "I prefer X"/"用户喜欢 X"/"always use X"），
// 排除 "I would like"/"It always depends" 等非偏好句式。
func extractPreference(feedback string) string {
	lower := strings.ToLower(feedback)

	// 明确的非偏好句式：命中即跳过，即使同时包含偏好关键词也不灌入 SOUL
	nonPreferencePatterns := []string{
		"i would like", "i'd like", "it always depends", "it depends on",
		"i would prefer not", "let me think", "i'm not sure", "maybe later",
	}
	for _, p := range nonPreferencePatterns {
		if strings.Contains(lower, p) {
			return ""
		}
	}

	// 要求明确的偏好信号（带具体动词）
	preferenceSignals := []string{
		"i prefer", "i like", "i love", "i always use", "i always prefer",
		"please always", "always use", "always prefer", "always respond",
		"i want you to always", "from now on",
		"用户喜欢", "用户偏好", "用户希望", "用户总是",
		"我喜欢", "我偏好", "我希望", "我总是",
		"总是使用", "请总是", "以后总是", "从现在开始",
	}
	for _, signal := range preferenceSignals {
		if strings.Contains(lower, signal) {
			return feedback
		}
	}

	return ""
}

// GetSoulForPrompt returns the soul formatted for system prompt
func (m *SoulManager) GetSoulForPrompt() string {
	soul := m.GetSoul()
	return "## System Personality (SOUL.md)\n\n" + soul + "\n\n---"
}

// ResetToDefault resets the soul to default personality
func (m *SoulManager) ResetToDefault() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.content = m.defaultSoul
	return m.save()
}
