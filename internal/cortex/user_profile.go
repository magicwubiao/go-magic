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
	jsonPath    string // JSON 权威存储路径（无损持久化所有偏好键）
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
		jsonPath:    filepath.Join(baseDir, "USER.json"),
		preferences: make(map[string]*UserPreference),
		interests:   make([]string, 0),
		techStack:   make([]string, 0),
		content:     DefaultUserProfile,
	}
}

// Load loads the user profile from disk
// 优先从 JSON 权威存储加载（无损保留所有偏好键），不存在或失败时回退到解析 USER.md（向后兼容）
func (up *UserProfile) Load() error {
	up.mu.Lock()
	defer up.mu.Unlock()

	// 优先从 JSON 权威存储加载（无损持久化）
	if data, err := os.ReadFile(up.jsonPath); err == nil {
		if err := up.loadFromJSON(data); err == nil {
			// 同步刷新 markdown 人类可读视图
			up.updateContent()
			_ = os.WriteFile(up.userPath, []byte(up.content), 0644)
			return nil
		}
		// JSON 加载失败则回退到 markdown 解析
	}

	// 回退：从 USER.md 解析（向后兼容已有文件）
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

// loadFromJSON 从 JSON 数据加载偏好（调用方需持有锁）
func (up *UserProfile) loadFromJSON(data []byte) error {
	var importData struct {
		Preferences map[string]*UserPreference `json:"preferences"`
		Interests   []string                   `json:"interests"`
		TechStack   []string                   `json:"tech_stack"`
	}
	if err := json.Unmarshal(data, &importData); err != nil {
		return err
	}
	up.preferences = importData.Preferences
	if up.preferences == nil {
		up.preferences = make(map[string]*UserPreference)
	}
	up.interests = importData.Interests
	if up.interests == nil {
		up.interests = make([]string, 0)
	}
	up.techStack = importData.TechStack
	if up.techStack == nil {
		up.techStack = make([]string, 0)
	}
	return nil
}

// save saves the user profile to disk
// 同时写入 JSON 权威存储（无损）与 markdown 人类可读视图
func (up *UserProfile) save() error {
	up.updateContent()
	// 写入 JSON 权威存储（保留所有偏好键，避免持久化有损）
	if err := up.saveJSON(); err != nil {
		return err
	}
	// 写入 markdown 人类可读视图
	return os.WriteFile(up.userPath, []byte(up.content), 0644)
}

// saveJSON 将结构化偏好写入 JSON 权威存储（调用方需持有锁）
func (up *UserProfile) saveJSON() error {
	export := struct {
		Preferences map[string]*UserPreference `json:"preferences"`
		Interests   []string                   `json:"interests"`
		TechStack   []string                   `json:"tech_stack"`
	}{
		Preferences: up.preferences,
		Interests:   up.interests,
		TechStack:   up.techStack,
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(up.jsonPath, data, 0644)
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
// 遍历所有 preferences 输出，避免 SetPreference 设置的非固定键在持久化时丢失
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

	// 输出所有偏好键（含 explicit/learned/parsed 等），确保 SetPreference 设置的
	// 任意键都不会在持久化时丢失。JSON 权威存储已保留全部，此处为人类可读视图。
	lines = append(lines, "")
	lines = append(lines, "## All Preferences")
	if len(up.preferences) == 0 {
		lines = append(lines, "- [Not set]")
	} else {
		for _, pref := range up.preferences {
			lines = append(lines, "- "+pref.Key+": "+pref.Value+
				" (source: "+pref.Source+", confidence: "+formatConfidence(pref.Confidence)+")")
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
		// 更新已存在偏好，多次强化置信度最高可达 0.95（与 explicit 的 1.0 仍有差距）
		existing.Value = value
		existing.Context = context
		existing.Source = "learned"
		existing.Confidence = min(existing.Confidence+0.1, 0.95) // 不再封顶 0.8
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

	// Add high-confidence preferences（阈值从 0.6 降到 0.5，让强化后的 learned 偏好可显示）
	for _, p := range up.preferences {
		if p.Confidence >= 0.5 {
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

	if err := up.loadFromJSON(data); err != nil {
		return err
	}

	return up.save()
}

// Helper function
func formatConfidence(c float64) string {
	s := fmt.Sprintf("%.2f", c)
	// Convert to percentage format
	return s
}
