package cortex

import (
	"os"
	"path/filepath"
	"sync"
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

// SetSoul updates the soul/personality
func (m *SoulManager) SetSoul(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.content = content
	return m.save()
}

// UpdateFromFeedback updates the soul based on user feedback
func (m *SoulManager) UpdateFromFeedback(feedback string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Append feedback as learning
	update := "\n\n## Learned Preferences\n" + feedback + "\n[Auto-generated from interactions]"
	m.content += update

	return m.save()
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
