package config

import (
	"os"
	"path/filepath"
	"sync"
)

// Profile represents a named configuration profile
type Profile struct {
	Name    string `json:"name"`
	HomeDir string `json:"home_dir"`
	Active  bool   `json:"active"`
}

// ProfileManager manages multiple isolated configuration profiles
type ProfileManager struct {
	homeBase  string               // Base directory for all profiles (e.g., ~/.go-magic/profiles)
	mu        sync.RWMutex
	current   *Profile
	profiles  map[string]*Profile
}

// ProfileManagerConfig holds configuration for the profile manager
type ProfileManagerConfig struct {
	BaseDir string `json:"base_dir"` // Base directory for profiles
	Current string `json:"current"` // Current active profile name
}

// NewProfileManager creates a new profile manager
func NewProfileManager(baseDir string) (*ProfileManager, error) {
	pm := &ProfileManager{
		homeBase: baseDir,
		profiles: make(map[string]*Profile),
	}

	// Ensure base directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	// Load existing profiles
	if err := pm.loadProfiles(); err != nil {
		return nil, err
	}

	return pm, nil
}

// loadProfiles loads all profiles from the profiles directory
func (pm *ProfileManager) loadProfiles() error {
	profilesDir := filepath.Join(pm.homeBase, "profiles")

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No profiles yet
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		pm.profiles[name] = &Profile{
			Name:    name,
			HomeDir: filepath.Join(profilesDir, name),
			Active:  false,
		}
	}

	// Load current profile from marker file
	currentFile := filepath.Join(pm.homeBase, ".current_profile")
	if data, err := os.ReadFile(currentFile); err == nil {
		name := string(data)
		if p, ok := pm.profiles[name]; ok {
			p.Active = true
			pm.current = p
		}
	}

	// If no current profile, create default
	if pm.current == nil {
		pm.Create("default")
	}

	return nil
}

// Create creates a new profile with the given name
func (pm *ProfileManager) Create(name string) (*Profile, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if name == "" {
		name = "default"
	}

	if _, exists := pm.profiles[name]; exists {
		return pm.profiles[name], nil // Already exists
	}

	profileDir := filepath.Join(pm.homeBase, "profiles", name)

	// Create profile directory structure
	dirs := []string{
		profileDir,
		filepath.Join(profileDir, "skills"),
		filepath.Join(profileDir, "memory"),
		filepath.Join(profileDir, "sessions"),
		filepath.Join(profileDir, "plugins"),
		filepath.Join(profileDir, "cache"),
		filepath.Join(profileDir, "logs"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	profile := &Profile{
		Name:    name,
		HomeDir: profileDir,
		Active:  false,
	}
	pm.profiles[name] = profile

	// If this is the first profile, make it active
	if pm.current == nil {
		if err := pm.setActive(name); err != nil {
			return nil, err
		}
	}

	return profile, nil
}

// Delete deletes a profile by name
func (pm *ProfileManager) Delete(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if name == "" || name == "default" {
		return nil // Cannot delete default
	}

	profile, exists := pm.profiles[name]
	if !exists {
		return nil // Already doesn't exist
	}

	// If deleting current profile, switch to default first
	if pm.current != nil && pm.current.Name == name {
		pm.setActive("default")
	}

	// Remove profile directory
	if err := os.RemoveAll(profile.HomeDir); err != nil {
		return err
	}

	delete(pm.profiles, name)
	return nil
}

// Switch switches to a different profile
func (pm *ProfileManager) Switch(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.profiles[name]; !exists {
		return ErrProfileNotFound
	}

	return pm.setActive(name)
}

func (pm *ProfileManager) setActive(name string) error {
	// Deactivate current
	if pm.current != nil {
		pm.current.Active = false
	}

	// Activate new
	pm.profiles[name].Active = true
	pm.current = pm.profiles[name]

	// Write marker file
	currentFile := filepath.Join(pm.homeBase, ".current_profile")
	return os.WriteFile(currentFile, []byte(name), 0644)
}

// Current returns the currently active profile
func (pm *ProfileManager) Current() *Profile {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.current
}

// List returns all profiles
func (pm *ProfileManager) List() []*Profile {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	profiles := make([]*Profile, 0, len(pm.profiles))
	for _, p := range pm.profiles {
		profiles = append(profiles, p)
	}
	return profiles
}

// Get returns a profile by name
func (pm *ProfileManager) Get(name string) (*Profile, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if p, exists := pm.profiles[name]; exists {
		return p, nil
	}
	return nil, ErrProfileNotFound
}

// GetHomeDir returns the home directory for a profile
func (pm *ProfileManager) GetHomeDir(name string) string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if p, exists := pm.profiles[name]; exists {
		return p.HomeDir
	}
	return ""
}

// CloneProfile clones an existing profile to a new name
func (pm *ProfileManager) CloneProfile(source, target string) (*Profile, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	sourceProfile, exists := pm.profiles[source]
	if !exists {
		return nil, ErrProfileNotFound
	}

	if _, exists := pm.profiles[target]; exists {
		return nil, ErrProfileExists
	}

	targetDir := filepath.Join(pm.homeBase, "profiles", target)
	if err := copyDir(sourceProfile.HomeDir, targetDir); err != nil {
		return nil, err
	}

	profile := &Profile{
		Name:    target,
		HomeDir: targetDir,
		Active:  false,
	}
	pm.profiles[target] = profile

	return profile, nil
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// Errors
var (
	ErrProfileNotFound = &ProfileError{"profile not found"}
	ErrProfileExists   = &ProfileError{"profile already exists"}
)

type ProfileError struct {
	msg string
}

func (e *ProfileError) Error() string {
	return e.msg
}
