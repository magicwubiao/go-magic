package plugin

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors plugin directories for changes and triggers hot reloads
type Watcher struct {
	loader   *Loader
	watcher  *fsnotify.Watcher
	stopCh   chan struct{}
	mu       sync.RWMutex
	running  bool
	debounce map[string]time.Time // pluginID -> last event time
}

// NewWatcher creates a new plugin watcher
func NewWatcher(loader *Loader) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Watcher{
		loader:   loader,
		watcher:  w,
		stopCh:   make(chan struct{}),
		debounce: make(map[string]time.Time),
	}, nil
}

// WatchDirectory starts watching a plugin directory for changes
func (pw *Watcher) WatchDirectory(dir string) error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if err := pw.watcher.Add(dir); err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	// Also watch subdirectories (plugin folders)
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if isDir(entry) {
			if err := pw.watcher.Add(entry); err != nil {
				// Log but don't fail
				fmt.Printf("Warning: failed to watch plugin dir %s: %v\n", entry, err)
			}
		}
	}

	return nil
}

// Start begins watching for file changes
func (pw *Watcher) Start() {
	pw.mu.Lock()
	if pw.running {
		pw.mu.Unlock()
		return
	}
	pw.running = true
	pw.mu.Unlock()

	go pw.loop()
}

// Stop stops the watcher
func (pw *Watcher) Stop() {
	pw.mu.Lock()
	if !pw.running {
		pw.mu.Unlock()
		return
	}
	pw.running = false
	pw.mu.Unlock()

	close(pw.stopCh)
	pw.watcher.Close()
}

// IsRunning returns whether the watcher is running
func (pw *Watcher) IsRunning() bool {
	pw.mu.RLock()
	defer pw.mu.RUnlock()
	return pw.running
}

func (pw *Watcher) loop() {
	debounceInterval := 500 * time.Millisecond

	for {
		select {
		case <-pw.stopCh:
			return

		case event, ok := <-pw.watcher.Events:
			if !ok {
				return
			}
			// Only react to write/rename/create events on manifest or script files
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			// Skip temp files and hidden files
			name := filepath.Base(event.Name)
			if len(name) > 0 && name[0] == '.' {
				continue
			}
			if filepath.Ext(name) == ".tmp" || filepath.Ext(name) == ".swp" {
				continue
			}

			// Extract plugin ID from path
			pluginID := pw.extractPluginID(event.Name)
			if pluginID == "" {
				continue
			}

			// Debounce: ignore events too close together
			pw.mu.Lock()
			if lastTime, ok := pw.debounce[pluginID]; ok && time.Since(lastTime) < debounceInterval {
				pw.mu.Unlock()
				continue
			}
			pw.debounce[pluginID] = time.Now()
			pw.mu.Unlock()

			// Trigger hot reload
			fmt.Printf("[Plugin Watcher] Detected change in %s, triggering hot reload...\n", pluginID)
			if err := pw.loader.HotReload(pluginID); err != nil {
				fmt.Printf("[Plugin Watcher] Hot reload failed for %s: %v\n", pluginID, err)
			} else {
				fmt.Printf("[Plugin Watcher] Hot reload succeeded for %s\n", pluginID)
			}

		case err, ok := <-pw.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[Plugin Watcher] Error: %v\n", err)
		}
	}
}

func (pw *Watcher) extractPluginID(path string) string {
	// Path format: /pluginDir/pluginID/file
	dir := filepath.Dir(path)
	base := filepath.Base(dir)

	// Check if the parent directory is a plugin directory (contains manifest.json)
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := filepath.Glob(manifestPath); err == nil {
		// Verify manifest exists
		if _, err := filepath.Glob(manifestPath); err == nil {
			// Simple existence check - just return the directory name
			return base
		}
	}

	// Alternative: check if grandparent is the plugin dir
	grandparent := filepath.Dir(dir)
	manifestPath = filepath.Join(grandparent, "manifest.json")
	if matches, _ := filepath.Glob(manifestPath); len(matches) > 0 {
		return filepath.Base(grandparent)
	}

	return ""
}

func isDir(path string) bool {
	info, err := filepath.Glob(path)
	return err == nil && len(info) > 0
}
