package gateway

import (
	"context"
	"fmt"
	"sync"
)

// PlatformInfo holds metadata about a platform
type PlatformInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
}

// PlatformFactory creates a new platform instance
type PlatformFactory func(ctx context.Context, config map[string]interface{}) (PlatformHandler, error)

// PlatformRegistry holds registered platforms
type PlatformRegistry struct {
	mu        sync.RWMutex
	platforms map[string]PlatformInfo
	factories map[string]PlatformFactory
}

var (
	registry     *PlatformRegistry
	registryOnce sync.Once
)

// GetRegistry returns the global platform registry
func GetRegistry() *PlatformRegistry {
	registryOnce.Do(func() {
		registry = &PlatformRegistry{
			platforms: make(map[string]PlatformInfo),
			factories: make(map[string]PlatformFactory),
		}
	})
	return registry
}

// Register registers a platform with the registry
func (r *PlatformRegistry) Register(info PlatformInfo, factory PlatformFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.platforms[info.ID] = info
	r.factories[info.ID] = factory
}

// GetInfo returns platform info by ID
func (r *PlatformRegistry) GetInfo(id string) (PlatformInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.platforms[id]
	return info, ok
}

// GetFactory returns platform factory by ID
func (r *PlatformRegistry) GetFactory(id string) (PlatformFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, ok := r.factories[id]
	return factory, ok
}

// List returns all registered platforms
func (r *PlatformRegistry) List() []PlatformInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]PlatformInfo, 0, len(r.platforms))
	for _, info := range r.platforms {
		result = append(result, info)
	}
	return result
}

// Create creates a new platform instance by ID
func (r *PlatformRegistry) Create(ctx context.Context, id string, config map[string]interface{}) (PlatformHandler, error) {
	factory, ok := r.GetFactory(id)
	if !ok {
		return nil, fmt.Errorf("platform '%s' not found in registry", id)
	}
	return factory(ctx, config)
}

func getConfigString(config map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := config[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
