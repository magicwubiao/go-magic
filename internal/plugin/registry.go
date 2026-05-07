package plugin

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Registry manages plugin registration and discovery
type Registry struct {
	mu          sync.RWMutex
	plugins     map[string]*PluginEntry
	index       *PluginIndex
	eventBus    *RegistryEventBus
}

// PluginEntry represents a registered plugin entry
type PluginEntry struct {
	Plugin    Plugin
	Manifest  *PluginManifest
	State     PluginState
	Info      *PluginInfo
	RefCount  int // Number of other plugins depending on this
	mu        sync.RWMutex
}

// PluginIndex provides fast lookup by various criteria
type PluginIndex struct {
	mu      sync.RWMutex
	byName  map[string]string   // name -> id
	byTag   map[string][]string // tag -> []id
	byCat   map[string][]string // category -> []id
	byDep   map[string][]string // plugin -> []dependents
}

// RegistryEventBus notifies on registry changes
type RegistryEventBus struct {
	mu         sync.RWMutex
	listeners  map[string][]RegistryListener
}

// RegistryListener receives registry events
type RegistryListener interface {
	OnPluginRegistered(plugin *PluginManifest)
	OnPluginUnregistered(id string)
	OnPluginStateChanged(id string, oldState, newState PluginState)
	OnPluginEnabled(id string)
	OnPluginDisabled(id string)
}

// NewRegistry creates a new plugin registry
func NewRegistry() *Registry {
	return &Registry{
		plugins:  make(map[string]*PluginEntry),
		index:    newPluginIndex(),
		eventBus: newRegistryEventBus(),
	}
}

// newPluginIndex creates a new plugin index
func newPluginIndex() *PluginIndex {
	return &PluginIndex{
		byName: make(map[string]string),
		byTag:  make(map[string][]string),
		byCat:  make(map[string][]string),
		byDep:  make(map[string][]string),
	}
}

// newRegistryEventBus creates a new event bus
func newRegistryEventBus() *RegistryEventBus {
	return &RegistryEventBus{
		listeners: make(map[string][]RegistryListener),
	}
}

// Register registers a plugin with the registry
func (r *Registry) Register(plugin Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	manifest := plugin.Manifest()

	// Validate manifest
	if err := ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	// Check if already registered
	if _, exists := r.plugins[manifest.ID]; exists {
		return fmt.Errorf("plugin already registered: %s", manifest.ID)
	}

	// Check for duplicate name
	if existingID, exists := r.index.byName[manifest.Name]; exists {
		return fmt.Errorf("plugin name already registered: %s (as %s)", manifest.Name, existingID)
	}

	// Create entry
	entry := &PluginEntry{
		Plugin:   plugin,
		Manifest: manifest,
		State:    StateLoaded,
		Info:     toPluginInfo(manifest, StateLoaded),
	}

	// Register
	r.plugins[manifest.ID] = entry
	r.updateIndex(manifest)

	// Notify listeners
	r.eventBus.notify(r, EventRegister, manifest.ID)

	return nil
}

// Unregister unregisters a plugin
func (r *Registry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.plugins[id]
	if !exists {
		return fmt.Errorf("plugin not found: %s", id)
	}

	// Check if any plugins depend on this one
	if entry.RefCount > 0 {
		return fmt.Errorf("cannot unregister plugin with %d dependents", entry.RefCount)
	}

	// Get manifest before removing
	manifest := entry.Manifest

	// Shutdown plugin if running
	if err := entry.Plugin.Shutdown(); err != nil {
		// Log but don't fail
		fmt.Printf("Warning: failed to shutdown plugin %s: %v\n", id, err)
	}

	// Remove from index
	r.removeFromIndex(manifest)

	// Remove from registry
	delete(r.plugins, id)

	// Notify listeners
	r.eventBus.notify(r, EventUnregister, id)

	return nil
}

// Get retrieves a plugin by ID
func (r *Registry) Get(id string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.plugins[id]
	if !exists {
		return nil, false
	}
	return entry.Plugin, true
}

// GetInfo retrieves plugin info by ID
func (r *Registry) GetInfo(id string) (*PluginInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.plugins[id]
	if !exists {
		return nil, false
	}
	return entry.Info, true
}

// List returns all registered plugin manifests
func (r *Registry) List() []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manifests := make([]*PluginManifest, 0, len(r.plugins))
	for _, entry := range r.plugins {
		manifests = append(manifests, entry.Manifest)
	}
	return manifests
}

// ListInfos returns all plugin info entries
func (r *Registry) ListInfos() []*PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]*PluginInfo, 0, len(r.plugins))
	for _, entry := range r.plugins {
		infos = append(infos, entry.Info)
	}
	SortPluginInfos(infos)
	return infos
}

// ListByCategory returns plugins in a category
func (r *Registry) ListByCategory(category string) []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.index.byCat[category]
	manifests := make([]*PluginManifest, 0, len(ids))
	for _, id := range ids {
		if entry, ok := r.plugins[id]; ok {
			manifests = append(manifests, entry.Manifest)
		}
	}
	return manifests
}

// ListByTag returns plugins with a tag
func (r *Registry) ListByTag(tag string) []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.index.byTag[tag]
	manifests := make([]*PluginManifest, 0, len(ids))
	for _, id := range ids {
		if entry, ok := r.plugins[id]; ok {
			manifests = append(manifests, entry.Manifest)
		}
	}
	return manifests
}

// Search searches plugins by name, description, or tags
func (r *Registry) Search(query string) []*PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query = toLower(query)
	var results []*PluginManifest

	for _, entry := range r.plugins {
		m := entry.Manifest
		// Check name
		if contains(toLower(m.Name), query) {
			results = append(results, m)
			continue
		}
		// Check description
		if contains(toLower(m.Description), query) || contains(toLower(m.LongDesc), query) {
			results = append(results, m)
			continue
		}
		// Check tags
		for _, tag := range m.Tags {
			if contains(toLower(tag), query) {
				results = append(results, m)
				break
			}
		}
	}
	return results
}

// Enable enables a plugin
func (r *Registry) Enable(id string) error {
	return r.setState(id, StateEnabled)
}

// Disable disables a plugin
func (r *Registry) Disable(id string) error {
	// Check dependents
	r.mu.RLock()
	entry, exists := r.plugins[id]
	hasDependents := exists && entry.RefCount > 0
	r.mu.RUnlock()

	if hasDependents {
		return fmt.Errorf("plugin has active dependents")
	}

	return r.setState(id, StateDisabled)
}

// SetState sets the plugin state
func (r *Registry) SetState(id string, state PluginState) error {
	return r.setState(id, state)
}

// setState internal state setter
func (r *Registry) setState(id string, newState PluginState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.plugins[id]
	if !exists {
		return fmt.Errorf("plugin not found: %s", id)
	}

	oldState := entry.State

	// Validate state transition
	if !isValidTransition(oldState, newState) {
		return fmt.Errorf("invalid state transition: %s -> %s", oldState, newState)
	}

	entry.State = newState
	entry.Info.State = newState

	// Update timestamps
	now := getTime()
	switch newState {
	case StateEnabled:
		entry.Info.EnabledAt = now
	case StateDisabled:
		entry.Info.DisabledAt = now
	}

	// Notify listeners
	r.eventBus.notifyStateChange(r, id, oldState, newState)

	return nil
}

// UpdateRefCount updates the reference count for dependencies
func (r *Registry) UpdateRefCount(id string, delta int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, exists := r.plugins[id]; exists {
		entry.RefCount += delta
		if entry.RefCount < 0 {
			entry.RefCount = 0
		}
		entry.Info.Dependents = r.getDependents(id)
	}
}

// ResolveDependencies resolves plugin dependencies
func (r *Registry) ResolveDependencies(id string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.plugins[id]
	if !exists {
		return fmt.Errorf("plugin not found: %s", id)
	}

	// Check each dependency
	for _, dep := range entry.Manifest.Dependencies {
		depEntry, exists := r.plugins[dep.ID]
		if !exists {
			if dep.Optional {
				continue
			}
			return fmt.Errorf("missing required dependency: %s", dep.ID)
		}

		// Check version compatibility
		if !CheckVersion(depEntry.Manifest.Version, dep.Version) {
			return fmt.Errorf("incompatible version for %s: need %s, have %s",
				dep.ID, dep.Version, depEntry.Manifest.Version)
		}
	}

	return nil
}

// GetDependents returns plugins that depend on this one
func (r *Registry) GetDependents(id string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getDependents(id)
}

func (r *Registry) getDependents(id string) []string {
	return r.index.byDep[id]
}

// Count returns the number of registered plugins
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}

// CountByState returns plugin count by state
func (r *Registry) CountByState() map[PluginState]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := make(map[PluginState]int)
	for _, entry := range r.plugins {
		counts[entry.State]++
	}
	return counts
}

// Categories returns all unique categories
func (r *Registry) Categories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cats := make(map[string]bool)
	for _, entry := range r.plugins {
		if entry.Manifest.Category != "" {
			cats[entry.Manifest.Category] = true
		}
	}

	result := make([]string, 0, len(cats))
	for cat := range cats {
		result = append(result, cat)
	}
	return result
}

// Tags returns all unique tags
func (r *Registry) Tags() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tags := make(map[string]bool)
	for _, entry := range r.plugins {
		for _, tag := range entry.Manifest.Tags {
			tags[tag] = true
		}
	}

	result := make([]string, 0, len(tags))
	for tag := range tags {
		result = append(result, tag)
	}
	return result
}

// RegisterListener registers a registry event listener
func (r *Registry) RegisterListener(listener RegistryListener) {
	r.eventBus.register(listener)
}

// UnregisterListener unregisters a registry event listener
func (r *Registry) UnregisterListener(listener RegistryListener) {
	r.eventBus.unregister(listener)
}

// updateIndex updates the search index
func (r *Registry) updateIndex(m *PluginManifest) {
	r.index.byName[m.Name] = m.ID

	for _, tag := range m.Tags {
		r.index.byTag[tag] = append(r.index.byTag[tag], m.ID)
	}

	if m.Category != "" {
		r.index.byCat[m.Category] = append(r.index.byCat[m.Category], m.ID)
	}
}

// removeFromIndex removes a plugin from the index
func (r *Registry) removeFromIndex(m *PluginManifest) {
	delete(r.index.byName, m.Name)

	for _, tag := range m.Tags {
		ids := r.index.byTag[tag]
		for i, id := range ids {
			if id == m.ID {
				r.index.byTag[tag] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
	}

	if m.Category != "" {
		ids := r.index.byCat[m.Category]
		for i, id := range ids {
			if id == m.ID {
				r.index.byCat[m.Category] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
	}

	delete(r.index.byDep, m.ID)
}

// toPluginInfo converts a manifest to PluginInfo
func toPluginInfo(m *PluginManifest, state PluginState) *PluginInfo {
	info := &PluginInfo{
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		State:       state,
		Author:      m.Author,
		Category:    m.Category,
		Tags:        m.Tags,
		Permissions: m.Permissions,
	}

	now := getTime()
	switch state {
	case StateLoaded:
		info.LoadedAt = now
	case StateEnabled:
		info.EnabledAt = now
		info.LoadedAt = now
	}

	for _, cmd := range m.Commands {
		info.Commands = append(info.Commands, cmd.Name)
	}
	info.Hooks = m.Hooks

	for _, dep := range m.Dependencies {
		info.Dependencies = append(info.Dependencies, dep.ID)
	}

	return info
}

// Registry event types
const (
	EventRegister       = "register"
	EventUnregister     = "unregister"
	EventStateChange    = "state_change"
	EventEnable         = "enable"
	EventDisable        = "disable"
)

// notify notifies listeners of an event
func (eb *RegistryEventBus) notify(r *Registry, event string, id string) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, listener := range eb.listeners[event] {
		switch event {
		case EventRegister:
			if entry, ok := r.plugins[id]; ok {
				listener.OnPluginRegistered(entry.Manifest)
			}
		case EventUnregister:
			listener.OnPluginUnregistered(id)
		}
	}

	// Also notify wildcard listeners
	for _, listener := range eb.listeners["*"] {
		switch event {
		case EventRegister:
			if entry, ok := r.plugins[id]; ok {
				listener.OnPluginRegistered(entry.Manifest)
			}
		case EventUnregister:
			listener.OnPluginUnregistered(id)
		}
	}
}

// notifyStateChange notifies of state changes
func (eb *RegistryEventBus) notifyStateChange(r *Registry, id string, oldState, newState PluginState) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, listener := range eb.listeners[EventStateChange] {
		listener.OnPluginStateChanged(id, oldState, newState)
	}
	for _, listener := range eb.listeners["*"] {
		listener.OnPluginStateChanged(id, oldState, newState)
	}

	if newState == StateEnabled {
		for _, listener := range eb.listeners[EventEnable] {
			listener.OnPluginEnabled(id)
		}
	} else if newState == StateDisabled {
		for _, listener := range eb.listeners[EventDisable] {
			listener.OnPluginDisabled(id)
		}
	}
}

// register registers a listener for an event
func (eb *RegistryEventBus) register(listener RegistryListener) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.listeners["*"] = append(eb.listeners["*"], listener)
}

// unregister removes a listener
func (eb *RegistryEventBus) unregister(listener RegistryListener) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for event, listeners := range eb.listeners {
		for i, l := range listeners {
			if l == listener {
				eb.listeners[event] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
	}
}

// Helper functions
func toLower(s string) string {
	return strings.ToLower(s)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func isValidTransition(from, to PluginState) bool {
	switch from {
	case StateUnloaded:
		return to == StateLoading || to == StateLoaded
	case StateLoading:
		return to == StateLoaded || to == StateUnloaded || to == StateError
	case StateLoaded:
		return to == StateEnabled || to == StateDisabled || to == StateUnloaded
	case StateEnabled:
		return to == StateDisabled || to == StateLoaded
	case StateDisabled:
		return to == StateEnabled || to == StateLoaded
	case StateError:
		return to == StateLoaded || to == StateUnloaded
	}
	return false
}

var timeFn = time.Now

func getTime() *time.Time {
	t := timeFn()
	return &t
}
