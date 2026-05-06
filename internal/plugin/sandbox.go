package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Sandbox provides security isolation for plugins
type Sandbox struct {
	plugin    Plugin
	timeout   time.Duration
	memLimit  int64 // bytes
	cpuQuota  int   // percentage (1-100)
	network   bool  // allow network access
	fsRead    []string
	fsWrite   []string
	envWhitelist []string
	mu        sync.RWMutex
	active    bool
}

// SandboxConfig holds sandbox configuration
type SandboxConfig struct {
	Timeout      time.Duration // Execution timeout
	MemLimit     int64          // Memory limit in bytes (0 = unlimited)
	CPUQuota     int            // CPU quota percentage (0 = unlimited)
	AllowNetwork bool           // Allow network access
	FilesystemRead  []string     // Allowed read paths
	FilesystemWrite []string     // Allowed write paths
	EnvWhitelist    []string     // Allowed environment variables
}

// DefaultSandboxConfig returns default sandbox configuration
func DefaultSandboxConfig() *SandboxConfig {
	home, _ := os.UserHomeDir()
	return &SandboxConfig{
		Timeout:       30 * time.Second,
		MemLimit:      256 * 1024 * 1024, // 256MB
		CPUQuota:      50,                // 50% CPU
		AllowNetwork:  true,
		FilesystemRead: []string{
			home,
			"/tmp",
			"/var/tmp",
		},
		FilesystemWrite: []string{
			filepath.Join(home, ".magic", "plugins"),
			"/tmp",
			"/var/tmp",
		},
		EnvWhitelist: []string{
			"HOME",
			"USER",
			"PATH",
			"LANG",
			"LC_*",
		},
	}
}

// NewSandbox creates a new sandbox for a plugin
func NewSandbox(plugin Plugin, config *SandboxConfig) *Sandbox {
	if config == nil {
		config = DefaultSandboxConfig()
	}

	return &Sandbox{
		plugin:         plugin,
		timeout:        config.Timeout,
		memLimit:       config.MemLimit,
		cpuQuota:       config.CPUQuota,
		network:        config.AllowNetwork,
		fsRead:         config.FilesystemRead,
		fsWrite:        config.FilesystemWrite,
		envWhitelist:   config.EnvWhitelist,
	}
}

// RunWithSandbox executes the plugin with sandbox restrictions
func (s *Sandbox) RunWithSandbox(ctx context.Context, input interface{}) (interface{}, error) {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return nil, fmt.Errorf("sandbox already active for this plugin")
	}
	s.active = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.active = false
		s.mu.Unlock()
	}()

	// Create context with timeout
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Execute in goroutine to allow cancellation
	type result struct {
		value interface{}
		err   error
	}

	resultCh := make(chan result, 1)

	go func() {
		r := result{}

		// Check permissions
		if err := s.checkPermissions(); err != nil {
			r.err = fmt.Errorf("permission denied: %w", err)
			resultCh <- r
			return
		}

		// Execute plugin
		switch p := s.plugin.(type) {
		case *ScriptPlugin:
			r.value, r.err = s.runScript(p, input)
		case *BinaryPlugin:
			r.value, r.err = s.runBinary(p, input)
		default:
			r.value, r.err = p.Execute("", nil)
		}

		resultCh <- r
	}()

	select {
	case <-runCtx.Done():
		return nil, fmt.Errorf("execution timed out after %v", s.timeout)
	case r := <-resultCh:
		return r.value, r.err
	}
}

// checkPermissions checks if the plugin has necessary permissions
func (s *Sandbox) checkPermissions() error {
	manifest := s.plugin.Manifest()

	// Check permissions against whitelist
	for _, perm := range manifest.Permissions {
		switch perm {
		case "network":
			if !s.network {
				return fmt.Errorf("network access denied")
			}
		case "filesystem":
			if len(s.fsRead) == 0 && len(s.fsWrite) == 0 {
				return fmt.Errorf("filesystem access denied")
			}
		case "subprocess":
			// Allow by default in sandbox
		case "environment":
			// Already checked via whitelist
		default:
			// Unknown permission - allow but log
			fmt.Printf("Warning: unknown permission: %s\n", perm)
		}
	}

	return nil
}

// runScript runs a script plugin in sandbox
func (s *Sandbox) runScript(plugin *ScriptPlugin, input interface{}) (interface{}, error) {
	// Get script path
	scriptPath := plugin.scriptPath

	// Check read permissions
	if !s.canRead(scriptPath) {
		return nil, fmt.Errorf("read access denied: %s", scriptPath)
	}

	// Script execution is handled by external process
	// The sandbox here is more about environment restrictions
	return nil, fmt.Errorf("script plugins must be executed via Execute method")
}

// runBinary runs a binary plugin in sandbox
func (s *Sandbox) runBinary(plugin *BinaryPlugin, input interface{}) (interface{}, error) {
	binaryPath := plugin.binaryPath

	// Check read permissions
	if !s.canRead(binaryPath) {
		return nil, fmt.Errorf("read access denied: %s", binaryPath)
	}

	// Binary execution is handled externally
	return nil, fmt.Errorf("binary plugins must be executed via Execute method")
}

// canRead checks if a path can be read
func (s *Sandbox) canRead(path string) bool {
	if len(s.fsRead) == 0 {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, allowed := range s.fsRead {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}

		if strings.HasPrefix(absPath, absAllowed) {
			return true
		}
	}

	return false
}

// canWrite checks if a path can be written
func (s *Sandbox) canWrite(path string) bool {
	if len(s.fsWrite) == 0 {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, allowed := range s.fsWrite {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}

		if strings.HasPrefix(absPath, absAllowed) {
			return true
		}
	}

	return false
}

// FilterEnv filters environment variables based on whitelist
func (s *Sandbox) FilterEnv(env []string) []string {
	if len(s.envWhitelist) == 0 {
		return nil
	}

	var filtered []string
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		for _, pattern := range s.envWhitelist {
			if strings.HasSuffix(pattern, "*") {
				prefix := strings.TrimSuffix(pattern, "*")
				if strings.HasPrefix(key, prefix) {
					filtered = append(filtered, e)
					break
				}
			} else if key == pattern {
				filtered = append(filtered, e)
				break
			}
		}
	}

	return filtered
}

// IsActive returns whether the sandbox is currently active
func (s *Sandbox) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// SandboxManager manages multiple sandboxes
type SandboxManager struct {
	mu       sync.RWMutex
	sandboxes map[string]*Sandbox
	config   *SandboxConfig
}

// NewSandboxManager creates a new sandbox manager
func NewSandboxManager(config *SandboxConfig) *SandboxManager {
	if config == nil {
		config = DefaultSandboxConfig()
	}

	return &SandboxManager{
		sandboxes: make(map[string]*Sandbox),
		config:    config,
	}
}

// CreateSandbox creates a sandbox for a plugin
func (sm *SandboxManager) CreateSandbox(plugin Plugin) *Sandbox {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	manifest := plugin.Manifest()
	id := manifest.ID

	// Check if sandbox already exists
	if sandbox, exists := sm.sandboxes[id]; exists {
		return sandbox
	}

	sandbox := NewSandbox(plugin, sm.config)
	sm.sandboxes[id] = sandbox
	return sandbox
}

// GetSandbox returns a sandbox for a plugin
func (sm *SandboxManager) GetSandbox(pluginID string) (*Sandbox, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	sandbox, exists := sm.sandboxes[pluginID]
	return sandbox, exists
}

// RemoveSandbox removes a sandbox
func (sm *SandboxManager) RemoveSandbox(pluginID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sandboxes, pluginID)
}

// SetDefaultConfig sets the default sandbox configuration
func (sm *SandboxManager) SetDefaultConfig(config *SandboxConfig) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.config = config
}

// GetDefaultConfig returns the default sandbox configuration
func (sm *SandboxManager) GetDefaultConfig() *SandboxConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.config
}

// ListActive returns all active sandboxes
func (sm *SandboxManager) ListActive() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var active []string
	for id, sandbox := range sm.sandboxes {
		if sandbox.IsActive() {
			active = append(active, id)
		}
	}
	return active
}

// EnforceMemoryLimit is a no-op placeholder for actual memory enforcement
// In a real implementation, this would use cgroups or similar
func EnforceMemoryLimit(memLimit int64) error {
	// This is a placeholder
	// Real implementation would use cgroups or similar OS features
	return nil
}

// EnforceTimeout creates a context with timeout
func EnforceTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}
