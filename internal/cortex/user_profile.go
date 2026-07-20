package cortex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// UserPreference represents a single user preference
type UserPreference struct {
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Context    string    `json:"context,omitempty"`
	Source     string    `json:"source"`     // "explicit", "learned", "feedback"
	Confidence float64   `json:"confidence"` // 0.0-1.0
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// UserProfile manages the USER.md with structured preferences
type UserProfile struct {
	mu          sync.RWMutex
	baseDir     string
	userPath    string
	preferences map[string]*UserPreference
	interests   []string
	workStyle   string
	techStack   []string
	content     string
}

// DefaultUserProfile is the default USER.md template
const DefaultUserProfile = `# User Profile

## About
- Name: [Not set]
- Role: [Not set]

## Preferences
- Communication style: [Not set]
- Code style: [Not set]

## Tech Stack
- Languages: [Not set]
- Frameworks: [Not set]

## Interests
- [Not set]

## Notes
[Any notes about the user]
`

// NewUserProfile creates a new user profile manager
func NewUserProfile(baseDir string) *UserProfile {
	return &UserProfile{
		baseDir:     baseDir,
		userPath:    filepath.Join(baseDir, "USER.md"),
		preferences: make(map[string]*UserPreference),
		interests:   make([]string, 0),
		techStack:   make([]string, 0),
		content:     DefaultUserProfile,
	}
}

// Load loads the USER.md file
func (up *UserProfile) Load() error {
	up.mu.Lock()
	defer up.mu.Unlock()

	data, err := os.ReadFile(up.userPath)
	if err != nil {
		if os.IsNotExist(err) {
			up.content = DefaultUserProfile
			return up.save()
		}
		return err
	}

	up.content = string(data)
	up.parseContent()

	return nil
}

// save saves the user profile to disk
func (up *UserProfile) save() error {
	up.updateContent()
	return os.WriteFile(up.userPath, []byte(up.content), 0644)
}

// parseContent parses USER.md into structured data
func (up *UserProfile) parseContent() {
	lines := strings.Split(up.content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)

			// Parse key-value pairs
			if idx := strings.Index(line, ":"); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				value := strings.TrimSpace(line[idx+1:])

				if key != "" && value != "" && value != "[Not set]" {
					up.preferences[key] = &UserPreference{
						Key:        key,
						Value:      value,
						Source:     "parsed",
						Confidence: 0.8,
						CreatedAt:  time.Now(),
						UpdatedAt:  time.Now(),
					}
				}
			}
		}
	}
}

// updateContent updates the markdown content from structured data
func (up *UserProfile) updateContent() {
	var lines []string

	lines = append(lines, "# User Profile")
	lines = append(lines, "")
	lines = append(lines, "## About")

	if pref, ok := up.preferences["Name"]; ok {
		lines = append(lines, "- Name: "+pref.Value)
	} else {
		lines = append(lines, "- Name: [Not set]")
	}

	if pref, ok := up.preferences["Role"]; ok {
		lines = append(lines, "- Role: "+pref.Value)
	} else {
		lines = append(lines, "- Role: [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Preferences")

	if pref, ok := up.preferences["Communication style"]; ok {
		lines = append(lines, "- Communication style: "+pref.Value)
	} else {
		lines = append(lines, "- Communication style: [Not set]")
	}

	if pref, ok := up.preferences["Code style"]; ok {
		lines = append(lines, "- Code style: "+pref.Value)
	} else {
		lines = append(lines, "- Code style: [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Tech Stack")

	if len(up.techStack) > 0 {
		for _, tech := range up.techStack {
			lines = append(lines, "- "+tech)
		}
	} else {
		lines = append(lines, "- Languages: [Not set]")
		lines = append(lines, "- Frameworks: [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Interests")

	if len(up.interests) > 0 {
		for _, interest := range up.interests {
			lines = append(lines, "- "+interest)
		}
	} else {
		lines = append(lines, "- [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Learned Preferences")

	for _, pref := range up.preferences {
		if pref.Source == "learned" {
			lines = append(lines, "- "+pref.Key+": "+pref.Value+" (confidence: "+formatConfidence(pref.Confidence)+")")
		}
	}

	lines = append(lines, "")
	lines = append(lines, "## Notes")
	lines = append(lines, "[Auto-managed by go-magic]")

	up.content = strings.Join(lines, "\n")
}

// SetPreference sets a user preference explicitly
func (up *UserProfile) SetPreference(key, value string) error {
	up.mu.Lock()
	defer up.mu.Unlock()

	up.preferences[key] = &UserPreference{
		Key:        key,
		Value:      value,
		Source:     "explicit",
		Confidence: 1.0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	return up.save()
}

// LearnPreference learns a preference from interaction
func (up *UserProfile) LearnPreference(key, value, context string) error {
	up.mu.Lock()
	defer up.mu.Unlock()

	if existing, ok := up.preferences[key]; ok {
		// Update existing with lower confidence
		existing.Value = value
		existing.Context = context
		existing.Source = "learned"
		existing.Confidence = min(existing.Confidence+0.1, 0.8) // Cap at 0.8
		existing.UpdatedAt = time.Now()
	} else {
		up.preferences[key] = &UserPreference{
			Key:        key,
			Value:      value,
			Context:    context,
			Source:     "learned",
			Confidence: 0.5,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	return up.save()
}

// AddInterest adds an interest
func (up *UserProfile) AddInterest(interest string) error {
	up.mu.Lock()
	defer up.mu.Unlock()

	// Check if already exists
	for _, existing := range up.interests {
		if strings.EqualFold(existing, interest) {
			return nil
		}
	}

	up.interests = append(up.interests, interest)
	return up.save()
}

// AddTech adds a technology to the tech stack
func (up *UserProfile) AddTech(tech string) error {
	up.mu.Lock()
	defer up.mu.Unlock()

	// Check if already exists
	for _, existing := range up.techStack {
		if strings.EqualFold(existing, tech) {
			return nil
		}
	}

	up.techStack = append(up.techStack, tech)
	return up.save()
}

// GetPreference returns a preference by key
func (up *UserProfile) GetPreference(key string) *UserPreference {
	up.mu.RLock()
	defer up.mu.RUnlock()
	return up.preferences[key]
}

// GetAllPreferences returns all preferences
func (up *UserProfile) GetAllPreferences() []*UserPreference {
	up.mu.RLock()
	defer up.mu.RUnlock()

	prefs := make([]*UserPreference, 0, len(up.preferences))
	for _, p := range up.preferences {
		prefs = append(prefs, p)
	}
	return prefs
}

// GetPreferencesBySource returns preferences filtered by source
func (up *UserProfile) GetPreferencesBySource(source string) []*UserPreference {
	up.mu.RLock()
	defer up.mu.RUnlock()

	var prefs []*UserPreference
	for _, p := range up.preferences {
		if p.Source == source {
			prefs = append(prefs, p)
		}
	}
	return prefs
}

// GetHighConfidence returns preferences with high confidence
func (up *UserProfile) GetHighConfidence(minConfidence float64) []*UserPreference {
	up.mu.RLock()
	defer up.mu.RUnlock()

	var prefs []*UserPreference
	for _, p := range up.preferences {
		if p.Confidence >= minConfidence {
			prefs = append(prefs, p)
		}
	}
	return prefs
}

// GetForPrompt returns the user profile formatted for system prompt
func (up *UserProfile) GetForPrompt() string {
	up.mu.RLock()
	defer up.mu.RUnlock()

	var lines []string
	lines = append(lines, "## User Profile")
	lines = append(lines, "")

	// Add high-confidence preferences
	for _, p := range up.preferences {
		if p.Confidence >= 0.6 {
			lines = append(lines, "- "+p.Key+": "+p.Value)
		}
	}

	// Add tech stack
	if len(up.techStack) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Tech stack: "+strings.Join(up.techStack, ", "))
	}

	// Add interests
	if len(up.interests) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Interests: "+strings.Join(up.interests, ", "))
	}

	return strings.Join(lines, "\n")
}

// Reset resets the user profile to default
func (up *UserProfile) Reset() error {
	up.mu.Lock()
	defer up.mu.Unlock()

	up.preferences = make(map[string]*UserPreference)
	up.interests = make([]string, 0)
	up.techStack = make([]string, 0)
	up.content = DefaultUserProfile

	return up.save()
}

// Export exports the profile as JSON
func (up *UserProfile) Export() ([]byte, error) {
	up.mu.RLock()
	defer up.mu.RUnlock()

	export := struct {
		Preferences map[string]*UserPreference `json:"preferences"`
		Interests   []string                   `json:"interests"`
		TechStack   []string                   `json:"tech_stack"`
	}{
		Preferences: up.preferences,
		Interests:   up.interests,
		TechStack:   up.techStack,
	}

	return json.MarshalIndent(export, "", "  ")
}

// Import imports a profile from JSON
func (up *UserProfile) Import(data []byte) error {
	up.mu.Lock()
	defer up.mu.Unlock()

	var importData struct {
		Preferences map[string]*UserPreference `json:"preferences"`
		Interests   []string                   `json:"interests"`
		TechStack   []string                   `json:"tech_stack"`
	}

	if err := json.Unmarshal(data, &importData); err != nil {
		return err
	}

	up.preferences = importData.Preferences
	up.interests = importData.Interests
	up.techStack = importData.TechStack

	return up.save()
}

// Helper function
func formatConfidence(c float64) string {
	s := fmt.Sprintf("%.2f", c)
	// Convert to percentage format
	return s
}
