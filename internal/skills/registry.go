package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SkillRegistry defines the interface for skill registries (ClawHub, GitHub, etc.)
type SkillRegistry interface {
	Name() string
	Search(ctx context.Context, query string, limit int) ([]HubSkill, error)
	GetSkillMeta(ctx context.Context, slug string) (*HubSkill, error)
	DownloadAndInstall(ctx context.Context, slug, version, targetDir string) error
}

// RegistrySearchResult represents a search result from any registry
type RegistrySearchResult struct {
	Score         float64   `json:"score"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Category      string    `json:"category,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	Source        HubSource `json:"source"`
	SourceID      string    `json:"source_id"`
	URL           string    `json:"url,omitempty"`
	Author        string    `json:"author,omitempty"`
	Stars         int       `json:"stars,omitempty"`
	Version       string    `json:"version,omitempty"`
	Installed     bool      `json:"installed,omitempty"`
	InstalledName string    `json:"installed_name,omitempty"`
}

// RegistryManager manages multiple skill registries
type RegistryManager struct {
	registries    []SkillRegistry
	maxConcurrent int
	mu            sync.RWMutex
}

// NewRegistryManager creates a new registry manager with default registries
func NewRegistryManager() *RegistryManager {
	rm := &RegistryManager{
		maxConcurrent: 3,
	}
	// Add default registries
	rm.AddRegistry(NewGitHubRegistry())
	rm.AddRegistry(NewClawHubRegistry())
	return rm
}

// AddRegistry adds a registry to the manager
func (rm *RegistryManager) AddRegistry(r SkillRegistry) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.registries = append(rm.registries, r)
}

// SearchAll searches all registries concurrently
func (rm *RegistryManager) SearchAll(ctx context.Context, query string, limit int) ([]HubSkill, error) {
	rm.mu.RLock()
	regs := make([]SkillRegistry, len(rm.registries))
	copy(regs, rm.registries)
	rm.mu.RUnlock()

	if len(regs) == 0 {
		return []HubSkill{}, nil
	}

	// Semaphore for concurrency control
	sem := make(chan struct{}, rm.maxConcurrent)
	var wg sync.WaitGroup
	resultsMu := sync.Mutex{}
	var allResults []HubSkill
	var firstErr error

	for _, reg := range regs {
		wg.Add(1)
		go func(r SkillRegistry) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			results, err := r.Search(ctx, query, limit)
			if err != nil {
				resultsMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				resultsMu.Unlock()
				return
			}
			resultsMu.Lock()
			allResults = append(allResults, results...)
			resultsMu.Unlock()
		}(reg)
	}

	wg.Wait()

	// If no results but there was an error, return the error
	if len(allResults) == 0 && firstErr != nil {
		return nil, firstErr
	}

	return allResults, nil
}

// GetRegistry returns a registry by name
func (rm *RegistryManager) GetRegistry(name string) SkillRegistry {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	for _, r := range rm.registries {
		if r.Name() == name {
			return r
		}
	}
	return nil
}

// ListRegistries returns all registered registry names
func (rm *RegistryManager) ListRegistries() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	names := make([]string, len(rm.registries))
	for i, r := range rm.registries {
		names[i] = r.Name()
	}
	return names
}

// SkillOriginMeta tracks where a skill was installed from
type SkillOriginMeta struct {
	Version     int    `json:"version"`
	OriginKind  string `json:"origin_kind"`  // "builtin" | "third_party" | "manual"
	Registry    string `json:"registry"`     // registry name
	Slug        string `json:"slug"`         // skill slug
	RegistryURL string `json:"registry_url"` // original URL
	VersionStr  string `json:"version_str"`  // installed version
	InstalledAt int64  `json:"installed_at"` // timestamp
}

// OriginMetaFilename is the filename for skill origin metadata
const OriginMetaFilename = ".skill-origin.json"

// LoadSkillOriginMeta loads origin metadata from a skill directory
func LoadSkillOriginMeta(skillDir string) (*SkillOriginMeta, error) {
	metaPath := filepath.Join(skillDir, OriginMetaFilename)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var meta SkillOriginMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SaveSkillOriginMeta saves origin metadata to a skill directory
func SaveSkillOriginMeta(skillDir string, meta *SkillOriginMeta) error {
	metaPath := filepath.Join(skillDir, OriginMetaFilename)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0644)
}
