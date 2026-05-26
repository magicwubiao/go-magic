package personality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Personality represents an agent personality
type Personality struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SystemPrompt string          `json:"system_prompt"`
	Tone         string          `json:"tone"`            // formal, casual, technical
	Traits       []string        `json:"traits"`          // personality traits
	Examples     []string        `json:"examples"`        // example responses
	Knowledge    []string        `json:"knowledge_areas"` // areas of expertise
	Greeting     string          `json:"greeting"`
	Farewell     string          `json:"farewell"`
	Flags        map[string]bool `json:"flags"` // special behavior flags
	CreatedAt    int64           `json:"created_at"`
	Version      int             `json:"version"`
}

// Manager handles personalities
type Manager struct {
	dataDir       string
	personalities map[string]*Personality
	currentID     string
	defaultID     string
}

// Config holds personality configuration
type Config struct {
	Enabled            bool   `yaml:"enabled"`
	DefaultPersonality string `yaml:"default_personality"`
}

// NewManager creates a new personality manager
func NewManager(dataDir string) *Manager {
	m := &Manager{
		dataDir:       dataDir,
		personalities: make(map[string]*Personality),
	}

	// Load default personalities
	m.loadDefaults()

	return m
}

// loadDefaults loads built-in personalities
func (m *Manager) loadDefaults() {
	// Assistant - balanced and helpful
	m.personalities["assistant"] = &Personality{
		ID:           "assistant",
		Name:         "Assistant",
		Description:  "Balanced and helpful AI assistant",
		SystemPrompt: "You are a helpful, balanced AI assistant. Provide clear, accurate, and concise responses. Adapt your communication style to the user's needs.",
		Tone:         "neutral",
		Traits:       []string{"helpful", "clear", "adaptable"},
		Greeting:     "Hello! How can I help you today?",
		CreatedAt:    0,
		Version:      1,
	}

	// Coder - programming-focused
	m.personalities["coder"] = &Personality{
		ID:           "coder",
		Name:         "Coder",
		Description:  "Programming expert focused on code",
		SystemPrompt: "You are an expert programmer. Provide clean, well-documented code with explanations. Focus on best practices, readability, and efficiency. Include code examples and comments.",
		Tone:         "technical",
		Traits:       []string{"precise", "technical", "thorough"},
		Examples: []string{
			"Here's a clean implementation...",
			"This approach has O(n) complexity...",
		},
		Knowledge: []string{"programming", "algorithms", "software design"},
		Greeting:  "Hello! Ready to code. What are we building?",
		CreatedAt: 0,
		Version:   1,
	}

	// Researcher - analytical and thorough
	m.personalities["researcher"] = &Personality{
		ID:           "researcher",
		Name:         "Researcher",
		Description:  "Analytical research assistant",
		SystemPrompt: "You are a meticulous research assistant. Provide thorough, well-sourced information. Include relevant context, sources, and analysis. Think step by step.",
		Tone:         "formal",
		Traits:       []string{"analytical", "thorough", "objective"},
		Examples: []string{
			"Based on my analysis...",
			"Research indicates that...",
		},
		Knowledge: []string{"research", "analysis", "critical thinking"},
		Greeting:  "Hello! I'm ready to help with your research.",
		CreatedAt: 0,
		Version:   1,
	}

	// Creative - imaginative and artistic
	m.personalities["creative"] = &Personality{
		ID:           "creative",
		Name:         "Creative",
		Description:  "Creative and imaginative assistant",
		SystemPrompt: "You are a creative assistant. Think outside the box, suggest innovative ideas, and help with creative tasks. Embrace creativity and experimentation.",
		Tone:         "casual",
		Traits:       []string{"creative", "imaginative", "playful"},
		Examples: []string{
			"What if we tried...",
			"Here's a creative approach...",
		},
		Greeting:  "Hey! Let's brainstorm something amazing!",
		Farewell:  "Keep creating!",
		CreatedAt: 0,
		Version:   1,
	}

	// Expert - domain expert mode
	m.personalities["expert"] = &Personality{
		ID:           "expert",
		Name:         "Expert",
		Description:  "Subject matter expert in specified domain",
		SystemPrompt: "You are an expert in your domain. Provide authoritative, detailed answers. Share insights from experience and best practices.",
		Tone:         "formal",
		Traits:       []string{"authoritative", "detailed", "experienced"},
		Knowledge:    []string{"varies by domain"},
		Greeting:     "Hello! I'm here to help with expert guidance.",
		CreatedAt:    0,
		Version:      1,
	}

	// Teacher - educational and patient
	m.personalities["teacher"] = &Personality{
		ID:           "teacher",
		Name:         "Teacher",
		Description:  "Patient and educational assistant",
		SystemPrompt: "You are a patient teacher. Explain concepts clearly, use examples, and check understanding. Break down complex topics into manageable parts.",
		Tone:         "casual",
		Traits:       []string{"patient", "educational", "encouraging"},
		Examples: []string{
			"Let me explain...",
			"A good way to think about this is...",
		},
		Greeting:  "Hello! I'm here to help you learn. What would you like to explore?",
		CreatedAt: 0,
		Version:   1,
	}

	// Concise - brief and to the point
	m.personalities["concise"] = &Personality{
		ID:           "concise",
		Name:         "Concise",
		Description:  "Brief and efficient responses",
		SystemPrompt: "Provide concise, direct answers. Avoid unnecessary words and explanations. Get to the point quickly while remaining helpful.",
		Tone:         "neutral",
		Traits:       []string{"concise", "efficient", "direct"},
		Greeting:     "Hi.",
		Farewell:     "Done.",
		CreatedAt:    0,
		Version:      1,
	}

	m.defaultID = "assistant"
}

// GetPersonality returns a personality by ID
func (m *Manager) GetPersonality(id string) *Personality {
	if p, ok := m.personalities[id]; ok {
		return p
	}
	return m.personalities[m.defaultID]
}

// SetCurrentPersonality sets the current active personality
func (m *Manager) SetCurrentPersonality(id string) error {
	if _, ok := m.personalities[id]; !ok {
		return fmt.Errorf("personality not found: %s", id)
	}
	m.currentID = id
	return nil
}

// GetCurrentPersonality returns the current personality
func (m *Manager) GetCurrentPersonality() *Personality {
	if m.currentID != "" {
		return m.personalities[m.currentID]
	}
	return m.personalities[m.defaultID]
}

// ListPersonality returns all available personalities
func (m *Manager) ListPersonality() []*Personality {
	list := make([]*Personality, 0, len(m.personalities))
	for _, p := range m.personalities {
		list = append(list, p)
	}
	return list
}

// CreatePersonality creates a new custom personality
func (m *Manager) CreatePersonality(p *Personality) error {
	p.ID = strings.ToLower(strings.ReplaceAll(p.Name, " ", "_"))
	p.ID = regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(p.ID, "")

	if _, exists := m.personalities[p.ID]; exists {
		return fmt.Errorf("personality already exists: %s", p.ID)
	}

	p.Version = 1
	m.personalities[p.ID] = p
	return m.SavePersonality(p.ID)
}

// UpdatePersonality updates an existing personality
func (m *Manager) UpdatePersonality(id string, updates map[string]interface{}) error {
	p := m.GetPersonality(id)
	if p == nil {
		return fmt.Errorf("personality not found: %s", id)
	}

	// Apply updates
	if name, ok := updates["name"].(string); ok {
		p.Name = name
	}
	if desc, ok := updates["description"].(string); ok {
		p.Description = desc
	}
	if prompt, ok := updates["system_prompt"].(string); ok {
		p.SystemPrompt = prompt
	}
	if tone, ok := updates["tone"].(string); ok {
		p.Tone = tone
	}
	if traits, ok := updates["traits"].([]string); ok {
		p.Traits = traits
	}
	if greeting, ok := updates["greeting"].(string); ok {
		p.Greeting = greeting
	}

	p.Version++
	return m.SavePersonality(id)
}

// DeletePersonality deletes a custom personality
func (m *Manager) DeletePersonality(id string) error {
	// Can't delete built-in personalities
	if _, ok := m.personalities[id]; !ok {
		return fmt.Errorf("personality not found: %s", id)
	}

	// Check if it's a built-in
	builtIn := []string{"assistant", "coder", "researcher", "creative", "expert", "teacher", "concise"}
	for _, b := range builtIn {
		if id == b {
			return fmt.Errorf("cannot delete built-in personality: %s", id)
		}
	}

	delete(m.personalities, id)

	// Remove file
	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", id))
	os.Remove(path)

	return nil
}

// SavePersonality saves a personality to disk
func (m *Manager) SavePersonality(id string) error {
	p := m.personalities[id]
	if p == nil {
		return fmt.Errorf("personality not found: %s", id)
	}

	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", id))
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// LoadPersonality loads a personality from disk
func (m *Manager) LoadPersonality(id string) error {
	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", id))

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var p Personality
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}

	m.personalities[p.ID] = &p
	return nil
}

// GenerateSystemPrompt generates a dynamic system prompt based on context
func (m *Manager) GenerateSystemPrompt(id string, context map[string]string) (string, error) {
	p := m.GetPersonality(id)
	if p == nil {
		return "", fmt.Errorf("personality not found: %s", id)
	}

	prompt := p.SystemPrompt

	// Add context variables
	for key, value := range context {
		prompt = strings.ReplaceAll(prompt, fmt.Sprintf("{{%s}}", key), value)
	}

	return prompt, nil
}

// GetCompatibleTools returns tools suitable for a personality
func (m *Manager) GetCompatibleTools(id string) []string {
	p := m.GetPersonality(id)
	if p == nil {
		return nil
	}

	// Map personalities to tools
	toolMap := map[string][]string{
		"coder":      {"read_file", "write_file", "search_in_files", "execute_command", "execute_code"},
		"researcher": {"web_search", "web_extract", "read_file", "memory_recall"},
		"creative":   {"web_search", "read_file", "write_file"},
		"teacher":    {"web_search", "read_file", "memory_recall", "skills_list"},
		"assistant":  {"web_search", "read_file", "write_file", "execute_command", "skills_list"},
		"expert":     {"web_search", "read_file", "write_file", "execute_command"},
		"concise":    {},
	}

	return toolMap[id]
}

// AdaptResponse adapts a response based on personality
func (m *Manager) AdaptResponse(id, response string) string {
	p := m.GetPersonality(id)
	if p == nil {
		return response
	}

	switch p.Tone {
	case "casual":
		// Make more casual
		response = strings.ReplaceAll(response, "I will", "I'll")
		response = strings.ReplaceAll(response, "cannot", "can't")
		response = strings.ReplaceAll(response, "do not", "don't")
		response = strings.ReplaceAll(response, "however", "but")

	case "formal":
		// Make more formal
		response = strings.ReplaceAll(response, "I'll", "I will")
		response = strings.ReplaceAll(response, "can't", "cannot")
		response = strings.ReplaceAll(response, "don't", "do not")
	}

	return response
}

// SOUL represents a persona file (SOUL.md)
type SOUL struct {
	CoreIdentity  string   `json:"core_identity"`
	Values        []string `json:"values"`
	Communication string   `json:"communication_style"`
	Boundaries    []string `json:"boundaries"`
	KnowledgeBase []string `json:"knowledge_base"`
	LearningGoals []string `json:"learning_goals"`
}

// LoadSOUL loads a SOUL.md file
func LoadSOUL(path string) (*SOUL, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	soul := &SOUL{}

	// Simple parser for SOUL.md format
	sections := map[string]*string{
		"core_identity":  &soul.CoreIdentity,
		"values":         nil, // handled specially
		"communication":  &soul.Communication,
		"boundaries":     nil, // handled specially
		"knowledge_base": nil, // handled specially
		"learning_goals": nil, // handled specially
	}

	lines := strings.Split(content, "\n")
	var currentSection string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "## ") {
			currentSection = strings.ToLower(strings.TrimPrefix(line, "## "))
			continue
		}

		if strings.HasPrefix(line, "- ") {
			value := strings.TrimPrefix(line, "- ")
			switch currentSection {
			case "values":
				soul.Values = append(soul.Values, value)
			case "boundaries":
				soul.Boundaries = append(soul.Boundaries, value)
			case "knowledge_base":
				soul.KnowledgeBase = append(soul.KnowledgeBase, value)
			case "learning_goals":
				soul.LearningGoals = append(soul.LearningGoals, value)
			}
			continue
		}

		if ptr, ok := sections[currentSection]; ok && ptr != nil {
			*ptr += line + "\n"
		}
	}

	return soul, nil
}

// ToPersonality converts SOUL to a Personality
func (s *SOUL) ToPersonality(id string) *Personality {
	return &Personality{
		ID:           id,
		Name:         "Custom",
		Description:  s.CoreIdentity,
		SystemPrompt: s.CoreIdentity + "\n\nCommunication style: " + s.Communication,
		Tone:         "neutral",
		Traits:       s.Values,
		Knowledge:    s.KnowledgeBase,
		Greeting:     "Hello! I'm here to help.",
		Flags:        make(map[string]bool),
		CreatedAt:    0,
		Version:      1,
	}
}
