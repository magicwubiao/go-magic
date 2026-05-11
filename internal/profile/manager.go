package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// UserProfile represents a user's dialectic profile
type UserProfile struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Language     string                 `json:"language"`      // Primary language
	Timezone     string                 `json:"timezone"`      // User's timezone
	Tone         string                 `json:"tone"`          // casual, formal, technical
	Expertise    []string               `json:"expertise"`     // Areas of expertise
	Preferences  *PreferenceProfile     `json:"preferences"`
	Behavior     *BehaviorProfile       `json:"behavior"`
	History      []*InteractionSummary  `json:"interaction_history"`
	Traits       map[string]float64     `json:"personality_traits"` // Big Five traits
	CreatedAt    int64                  `json:"created_at"`
	UpdatedAt    int64                  `json:"updated_at"`
	Version      int                    `json:"version"`
}

// PreferenceProfile tracks user preferences
type PreferenceProfile struct {
	ResponseLength  string   `json:"response_length"`  // brief, medium, detailed
	CodeStyle       []string `json:"code_style"`        // preferred languages
	Tools           []string `json:"preferred_tools"`   // frequently used tools
	Platforms       []string `json:"preferred_platforms"`
	LearningStyle   string   `json:"learning_style"`   // visual, auditory, reading, kinesthetic
	CommunicationStyle string `json:"communication_style"` // direct, elaborate
}

// BehaviorProfile tracks user behavior patterns
type BehaviorProfile struct {
	SessionCount    int              `json:"session_count"`
	AvgSessionLength int              `json:"avg_session_length"` // minutes
	ActiveHours     []int            `json:"active_hours"`       // 0-23
	ActiveDays      []int            `json:"active_days"`        // 0-6 (Sunday=0)
	CommonTasks     map[string]int   `json:"common_tasks"`       // task -> count
	SuccessRate     float64          `json:"success_rate"`       // task success rate
	LastInteraction int64            `json:"last_interaction"`
	Patterns        []string         `json:"behavioral_patterns"`
}

// InteractionSummary summarizes an interaction
type InteractionSummary struct {
	Timestamp   int64   `json:"timestamp"`
	Task        string  `json:"task"`
	ToolsUsed   []string `json:"tools_used"`
	Success     bool    `json:"success"`
	Satisfaction float64 `json:"satisfaction"` // 1-5 scale
	Duration    int     `json:"duration_seconds"`
	Topic       string  `json:"topic"`
}

// Manager handles user profiles
type Manager struct {
	dataDir     string
	profiles    map[string]*UserProfile
	currentUser string
	model       *DialecticModel
}

// DialecticModel analyzes and predicts user behavior
type DialecticModel struct {
	TrainedAt     int64               `json:"trained_at"`
	Accuracy      float64             `json:"accuracy"`
	Predictions   map[string]float64  `json:"predictions"`
	Patterns      []string            `json:"learned_patterns"`
}

// Config holds profile manager configuration
type Config struct {
	Enabled         bool     `yaml:"enabled"`
	AutoLearn       bool     `yaml:"auto_learn"`        // Learn from interactions
	ProfilePath     string   `yaml:"profile_path"`       // Custom profile location
	SaveInterval    int      `yaml:"save_interval"`      // Seconds between auto-saves
	MinInteractions int      `yaml:"min_interactions"`   // Before generating insights
	Anonymize       bool     `yaml:"anonymize"`         // Don't store sensitive data
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:         true,
		AutoLearn:       true,
		SaveInterval:    300,
		MinInteractions: 10,
		Anonymize:       false,
	}
}

// NewManager creates a new profile manager
func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir:   dataDir,
		profiles:  make(map[string]*UserProfile),
		model:     &DialecticModel{},
	}
}

// CreateProfile creates a new user profile
func (m *Manager) CreateProfile(userID, name string) *UserProfile {
	profile := &UserProfile{
		ID:        userID,
		Name:      name,
		Language:  "en",
		Timezone:  "UTC",
		Tone:      "neutral",
		Expertise: []string{},
		Preferences: &PreferenceProfile{
			ResponseLength:     "medium",
			LearningStyle:      "reading",
			CommunicationStyle: "direct",
		},
		Behavior: &BehaviorProfile{
			CommonTasks:   make(map[string]int),
			ActiveHours:   []int{},
			ActiveDays:    []int{},
		},
		Traits: map[string]float64{
			"openness":        0.5,
			"conscientiousness": 0.5,
			"extraversion":     0.5,
			"agreeableness":    0.5,
			"neuroticism":      0.5,
		},
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Version:   1,
	}
	
	m.profiles[userID] = profile
	m.currentUser = userID
	return profile
}

// GetProfile returns a user profile
func (m *Manager) GetProfile(userID string) *UserProfile {
	if profile, ok := m.profiles[userID]; ok {
		return profile
	}
	return nil
}

// SetCurrentUser sets the current active user
func (m *Manager) SetCurrentUser(userID string) {
	m.currentUser = userID
}

// GetCurrentProfile returns the current user's profile
func (m *Manager) GetCurrentProfile() *UserProfile {
	if m.currentUser == "" {
		return nil
	}
	return m.GetProfile(m.currentUser)
}

// RecordInteraction records a user interaction
func (m *Manager) RecordInteraction(userID string, interaction *InteractionSummary) error {
	profile := m.GetProfile(userID)
	if profile == nil {
		profile = m.CreateProfile(userID, "default")
	}
	
	interaction.Timestamp = time.Now().Unix()
	profile.History = append(profile.History, interaction)
	profile.UpdatedAt = time.Now().Unix()
	
	// Update behavior profile
	profile.Behavior.LastInteraction = time.Now().Unix()
	profile.Behavior.SessionCount++
	
	// Extract topic and task patterns
	if interaction.Topic != "" {
		profile.Behavior.CommonTasks[interaction.Topic]++
	}
	
	// Update common tasks based on task text
	taskPatterns := extractTaskPatterns(interaction.Task)
	for _, pattern := range taskPatterns {
		profile.Behavior.CommonTasks[pattern]++
	}
	
	// Update timezone from interaction time
	hour := time.Now().Hour()
	profile.Behavior.ActiveHours = addUniqueInt(profile.Behavior.ActiveHours, hour)
	
	day := int(time.Now().Weekday())
	profile.Behavior.ActiveDays = addUniqueInt(profile.Behavior.ActiveDays, day)
	
	// Update success rate
	total := len(profile.History)
	successes := 0
	for _, h := range profile.History {
		if h.Success {
			successes++
		}
	}
	profile.Behavior.SuccessRate = float64(successes) / float64(total)
	
	return m.SaveProfile(userID)
}

// extractTaskPatterns extracts meaningful patterns from task text
func extractTaskPatterns(task string) []string {
	var patterns []string
	
	// Remove punctuation and lowercase
	task = strings.ToLower(task)
	task = regexp.MustCompile(`[^\w\s]`).ReplaceAllString(task, "")
	
	// Common task prefixes
	prefixes := []string{"create", "build", "fix", "update", "delete", "get", "list", "search", "generate", "analyze", "debug", "test", "deploy", "configure"}
	
	words := strings.Fields(task)
	for i, word := range words {
		for _, prefix := range prefixes {
			if strings.HasPrefix(word, prefix) {
				// Get the next word for context
				if i+1 < len(words) {
					patterns = append(patterns, fmt.Sprintf("%s %s", word, words[i+1]))
				}
				patterns = append(patterns, word)
			}
		}
	}
	
	// Tool patterns
	toolRe := regexp.MustCompile(`(\w+)_(list|get|set|create|delete|update)`)
	for _, match := range toolRe.FindAllStringSubmatch(task, -1) {
		if len(match) > 1 {
			patterns = append(patterns, match[1])
		}
	}
	
	return patterns
}

// UpdatePreference updates a user preference
func (m *Manager) UpdatePreference(userID string, pref string, value interface{}) error {
	profile := m.GetProfile(userID)
	if profile == nil {
		return fmt.Errorf("profile not found: %s", userID)
	}
	
	switch pref {
	case "response_length":
		if v, ok := value.(string); ok {
			profile.Preferences.ResponseLength = v
		}
	case "tone":
		if v, ok := value.(string); ok {
			profile.Tone = v
		}
	case "language":
		if v, ok := value.(string); ok {
			profile.Language = v
		}
	case "learning_style":
		if v, ok := value.(string); ok {
			profile.Preferences.LearningStyle = v
		}
	case "expertise":
		if v, ok := []string{}; true {
			// Handle expertise array
		}
	}
	
	profile.UpdatedAt = time.Now().Unix()
	return m.SaveProfile(userID)
}

// GenerateInsights generates insights from user profile
func (m *Manager) GenerateInsights(userID string) *Insights {
	profile := m.GetProfile(userID)
	if profile == nil {
		return nil
	}
	
	insights := &Insights{
		Profile:     profile,
		GeneratedAt: time.Now().Unix(),
	}
	
	// Analyze behavior patterns
	if len(profile.History) > 0 {
		insights.TopTasks = m.getTopTasks(profile, 5)
		insights.PreferredTools = m.getPreferredTools(profile)
		insights.BusiestHours = m.getBusiestHours(profile)
		insights.SuccessRate = profile.Behavior.SuccessRate
	}
	
	// Generate personality insights
	insights.PersonalitySummary = m.generatePersonalitySummary(profile)
	
	// Learning recommendations
	insights.Recommendations = m.generateRecommendations(profile)
	
	return insights
}

// Insights contains generated user insights
type Insights struct {
	Profile            *UserProfile `json:"profile"`
	TopTasks           []string     `json:"top_tasks"`
	PreferredTools     []string     `json:"preferred_tools"`
	BusiestHours       []int        `json:"busiest_hours"`
	SuccessRate        float64      `json:"success_rate"`
	PersonalitySummary string       `json:"personality_summary"`
	Recommendations    []string     `json:"recommendations"`
	GeneratedAt        int64        `json:"generated_at"`
}

func (m *Manager) getTopTasks(profile *UserProfile, limit int) []string {
	type taskCount struct {
		task  string
		count int
	}
	
	var tasks []taskCount
	for task, count := range profile.Behavior.CommonTasks {
		tasks = append(tasks, taskCount{task, count})
	}
	
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].count > j.count
	})
	
	var result []string
	for i := 0; i < min(limit, len(tasks)); i++ {
		result = append(result, tasks[i].task)
	}
	return result
}

func (m *Manager) getPreferredTools(profile *UserProfile) []string {
	toolCounts := make(map[string]int)
	
	for _, h := range profile.History {
		for _, tool := range h.ToolsUsed {
			toolCounts[tool]++
		}
	}
	
	type toolCount struct {
		tool  string
		count int
	}
	var tools []toolCount
	for tool, count := range toolCounts {
		tools = append(tools, toolCount{tool, count})
	}
	
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].count > j.count
	})
	
	var result []string
	for i := 0; i < min(5, len(tools)); i++ {
		result = append(result, tools[i].tool)
	}
	return result
}

func (m *Manager) getBusiestHours(profile *UserProfile) []int {
	hourCounts := make(map[int]int)
	for _, h := range profile.History {
		hour := time.Unix(h.Timestamp, 0).Hour()
		hourCounts[hour]++
	}
	
	type hourCount struct {
		hour  int
		count int
	}
	var hours []hourCount
	for hour, count := range hourCounts {
		hours = append(hours, hourCount{hour, count})
	}
	
	sort.Slice(hours, func(i, j int) bool {
		return hours[i].count > j.count
	})
	
	var result []int
	for i := 0; i < min(3, len(hours)); i++ {
		result = append(result, hours[i].hour)
	}
	return result
}

func (m *Manager) generatePersonalitySummary(profile *UserProfile) string {
	var traits []string
	
	if profile.Traits["openness"] > 0.7 {
		traits = append(traits, "creative and curious")
	} else if profile.Traits["openness"] < 0.3 {
		traits = append(traits, "practical and focused")
	}
	
	if profile.Traits["conscientiousness"] > 0.7 {
		traits = append(traits, "organized and thorough")
	}
	
	if profile.Traits["extraversion"] > 0.7 {
		traits = append(traits, "outgoing and communicative")
	} else if profile.Traits["extraversion"] < 0.3 {
		traits = append(traits, "reserved and independent")
	}
	
	if len(traits) == 0 {
		traits = append(traits, "balanced")
	}
	
	return strings.Join(traits, ", ")
}

func (m *Manager) generateRecommendations(profile *UserProfile) []string {
	var recs []string
	
	if profile.Preferences.ResponseLength == "brief" {
		recs = append(recs, "I'll keep responses concise and to the point.")
	} else if profile.Preferences.ResponseLength == "detailed" {
		recs = append(recs, "I'll provide comprehensive explanations with examples.")
	}
	
	if len(profile.Behavior.CommonTasks) > 0 {
		recs = append(recs, fmt.Sprintf("I notice you often work with: %s", strings.Join(m.getTopTasks(profile, 3), ", ")))
	}
	
	if profile.Behavior.SuccessRate > 0.9 {
		recs = append(recs, "Great track record! Let's keep the momentum going.")
	} else if profile.Behavior.SuccessRate < 0.7 {
		recs = append(recs, "I'll be extra careful with those tricky tasks.")
	}
	
	return recs
}

// GetPromptContext returns context for system prompt
func (m *Manager) GetPromptContext(userID string) string {
	profile := m.GetProfile(userID)
	if profile == nil {
		return ""
	}
	
	var ctx strings.Builder
	
	// Language and communication style
	ctx.WriteString(fmt.Sprintf("User prefers %s responses in %s.\n",
		profile.Preferences.ResponseLength, profile.Language))
	
	if profile.Preferences.CommunicationStyle != "" {
		ctx.WriteString(fmt.Sprintf("Communication style: %s.\n",
			profile.Preferences.CommunicationStyle))
	}
	
	// Expertise
	if len(profile.Expertise) > 0 {
		ctx.WriteString(fmt.Sprintf("User expertise: %s.\n",
			strings.Join(profile.Expertise, ", ")))
	}
	
	// Top tasks
	topTasks := m.getTopTasks(profile, 3)
	if len(topTasks) > 0 {
		ctx.WriteString(fmt.Sprintf("Frequently: %s.\n",
			strings.Join(topTasks, ", ")))
	}
	
	// Tone
	ctx.WriteString(fmt.Sprintf("Tone: %s.\n", profile.Tone))
	
	// Learning style
	ctx.WriteString(fmt.Sprintf("Learning style: %s.\n",
		profile.Preferences.LearningStyle))
	
	return ctx.String()
}

// SaveProfile saves a user profile to disk
func (m *Manager) SaveProfile(userID string) error {
	profile := m.GetProfile(userID)
	if profile == nil {
		return fmt.Errorf("profile not found: %s", userID)
	}
	
	profile.UpdatedAt = time.Now().Unix()
	
	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", userID))
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(path, data, 0644)
}

// LoadProfile loads a user profile from disk
func (m *Manager) LoadProfile(userID string) error {
	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", userID))
	
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	
	var profile UserProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return err
	}
	
	m.profiles[userID] = &profile
	return nil
}

// ListProfiles lists all available profiles
func (m *Manager) ListProfiles() []*UserProfile {
	profiles := make([]*UserProfile, 0, len(m.profiles))
	for _, p := range m.profiles {
		profiles = append(profiles, p)
	}
	return profiles
}

// DeleteProfile deletes a user profile
func (m *Manager) DeleteProfile(userID string) error {
	if _, ok := m.profiles[userID]; !ok {
		return fmt.Errorf("profile not found: %s", userID)
	}
	
	delete(m.profiles, userID)
	
	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", userID))
	os.Remove(path)
	
	return nil
}

// ExportProfile exports a profile for sharing
func (m *Manager) ExportProfile(userID string) ([]byte, error) {
	profile := m.GetProfile(userID)
	if profile == nil {
		return nil, fmt.Errorf("profile not found: %s", userID)
	}
	
	return json.MarshalIndent(profile, "", "  ")
}

// ImportProfile imports a profile
func (m *Manager) ImportProfile(data []byte) error {
	var profile UserProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return err
	}
	
	profile.UpdatedAt = time.Now().Unix()
	m.profiles[profile.ID] = &profile
	
	return m.SaveProfile(profile.ID)
}

// Helper function
func addUniqueInt(slice []int, val int) []int {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
