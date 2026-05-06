package plugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Repository provides remote plugin discovery and installation
type Repository struct {
	baseURL    string
	client     *http.Client
	cacheDir   string
	cacheTTL   time.Duration
	indexCache *RepositoryIndex
	indexMu    int64 // Unix timestamp of last refresh
}

// RepositoryIndex represents the cached plugin index
type RepositoryIndex struct {
	UpdatedAt string           `json:"updated_at"`
	Plugins   []PluginManifest `json:"plugins"`
}

// NewRepository creates a new repository client
func NewRepository(baseURL string) (*Repository, error) {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".magic", "plugins", "cache", "repo")

	os.MkdirAll(cacheDir, 0755)

	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	repo := &Repository{
		baseURL:  baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		cacheDir: cacheDir,
		cacheTTL: 1 * time.Hour,
	}

	// Try to load cached index
	repo.loadCachedIndex()

	return repo, nil
}

// Search searches for plugins by query
func (r *Repository) Search(query string) ([]PluginManifest, error) {
	// First try local search
	if r.indexCache != nil {
		results := r.searchLocal(query)
		if len(results) > 0 {
			return results, nil
		}
	}

	// Fetch from remote
	if err := r.refreshIndex(); err != nil {
		return nil, fmt.Errorf("failed to refresh index: %w", err)
	}

	return r.searchLocal(query), nil
}

// searchLocal searches the local index
func (r *Repository) searchLocal(query string) []PluginManifest {
	if r.indexCache == nil {
		return nil
	}

	query = strings.ToLower(query)
	var results []PluginManifest

	for _, plugin := range r.indexCache.Plugins {
		// Check name
		if strings.Contains(strings.ToLower(plugin.Name), query) {
			results = append(results, plugin)
			continue
		}
		// Check description
		if strings.Contains(strings.ToLower(plugin.Description), query) {
			results = append(results, plugin)
			continue
		}
		// Check tags
		for _, tag := range plugin.Tags {
			if strings.Contains(strings.ToLower(tag), query) {
				results = append(results, plugin)
				break
			}
		}
		// Check ID
		if strings.Contains(strings.ToLower(plugin.ID), query) {
			results = append(results, plugin)
		}
	}

	return results
}

// ListAvailable returns all available plugins from the repository
func (r *Repository) ListAvailable() ([]PluginManifest, error) {
	if r.indexCache == nil || r.needsRefresh() {
		if err := r.refreshIndex(); err != nil {
			return nil, fmt.Errorf("failed to refresh index: %w", err)
		}
	}

	return r.indexCache.Plugins, nil
}

// Install downloads and installs a plugin from the repository
func (r *Repository) Install(pluginID string, version string, targetDir string) error {
	// Get plugin info
	manifest, err := r.GetPluginInfo(pluginID)
	if err != nil {
		return fmt.Errorf("plugin not found: %s", pluginID)
	}

	// Find compatible version
	installVersion := version
	if installVersion == "" {
		installVersion = manifest.Version
	}

	// Check if version exists in index
	var downloadURL string
	for _, v := range []string{installVersion, "latest"} {
		u := r.buildURL("plugins", pluginID, "download", map[string]string{"version": v})
		if r.pluginExists(u) {
			downloadURL = u
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("version not found: %s", installVersion)
	}

	// Download plugin
	data, err := r.download(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Create plugin directory
	pluginDir := filepath.Join(targetDir, pluginID)
	os.MkdirAll(pluginDir, 0755)

	// Extract if ZIP
	if isZip(data) {
		if err := r.extractZip(bytes.NewReader(data), pluginDir); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
	} else {
		// Write directly
		manifestPath := filepath.Join(pluginDir, "manifest.json")
		if err := os.WriteFile(manifestPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write manifest: %w", err)
		}
	}

	return nil
}

// InstallFromURL installs a plugin directly from a URL
func (r *Repository) InstallFromURL(pluginURL string, targetDir string) error {
	pluginID := guessPluginID(pluginURL)

	data, err := r.download(pluginURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	pluginDir := filepath.Join(targetDir, pluginID)
	os.MkdirAll(pluginDir, 0755)

	// Extract if ZIP
	if isZip(data) {
		return r.extractZip(bytes.NewReader(data), pluginDir)
	}

	// Try to parse as JSON manifest
	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err == nil && manifest.ID != "" {
		manifestPath := filepath.Join(pluginDir, "manifest.json")
		return os.WriteFile(manifestPath, data, 0644)
	}

	return fmt.Errorf("unsupported plugin format")
}

// Uninstall removes a plugin from the local directory
func (r *Repository) Uninstall(pluginID string, pluginDir string) error {
	targetDir := filepath.Join(pluginDir, pluginID)

	// Check if exists
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin not installed: %s", pluginID)
	}

	// Remove directory
	return os.RemoveAll(targetDir)
}

// Update checks for and installs updates
func (r *Repository) Update(pluginID string, currentVersion string, targetDir string) (bool, string, error) {
	// Get latest version from repository
	manifest, err := r.GetPluginInfo(pluginID)
	if err != nil {
		return false, "", err
	}

	latestVersion := manifest.Version

	// Compare versions
	if !CheckUpgrade(currentVersion, latestVersion) {
		return false, latestVersion, nil
	}

	// Install new version
	if err := r.Install(pluginID, latestVersion, targetDir); err != nil {
		return false, latestVersion, fmt.Errorf("update failed: %w", err)
	}

	return true, latestVersion, nil
}

// GetPluginInfo returns plugin information from the repository
func (r *Repository) GetPluginInfo(pluginID string) (*PluginManifest, error) {
	// Check local index first
	if r.indexCache != nil {
		for _, plugin := range r.indexCache.Plugins {
			if plugin.ID == pluginID {
				return &plugin, nil
			}
		}
	}

	// Fetch from remote
	url := r.buildURL("plugins", pluginID, "info")
	data, err := r.download(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch plugin info: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse plugin info: %w", err)
	}

	return &manifest, nil
}

// ListCategories returns available plugin categories
func (r *Repository) ListCategories() ([]string, error) {
	if r.indexCache == nil {
		if err := r.refreshIndex(); err != nil {
			return nil, err
		}
	}

	categories := make(map[string]bool)
	for _, plugin := range r.indexCache.Plugins {
		if plugin.Category != "" {
			categories[plugin.Category] = true
		}
	}

	result := make([]string, 0, len(categories))
	for cat := range categories {
		result = append(result, cat)
	}
	return result, nil
}

// ListByCategory returns plugins in a category
func (r *Repository) ListByCategory(category string) ([]PluginManifest, error) {
	if r.indexCache == nil {
		if err := r.refreshIndex(); err != nil {
			return nil, err
		}
	}

	var results []PluginManifest
	for _, plugin := range r.indexCache.Plugins {
		if plugin.Category == category {
			results = append(results, plugin)
		}
	}
	return results, nil
}

// refreshIndex fetches the latest plugin index
func (r *Repository) refreshIndex() error {
	url := r.buildURL("index.json")

	data, err := r.download(url)
	if err != nil {
		return fmt.Errorf("failed to download index: %w", err)
	}

	var index RepositoryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("failed to parse index: %w", err)
	}

	r.indexCache = &index
	r.indexMu = time.Now().Unix()

	// Save to cache
	r.saveCachedIndex(data)

	return nil
}

// needsRefresh checks if the cached index needs to be refreshed
func (r *Repository) needsRefresh() bool {
	if r.indexCache == nil {
		return true
	}

	age := time.Now().Unix() - r.indexMu
	return age > int64(r.cacheTTL.Seconds())
}

// loadCachedIndex loads the index from cache
func (r *Repository) loadCachedIndex() {
	cachePath := r.getCachePath()

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return
	}

	var index RepositoryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return
	}

	r.indexCache = &index
	r.indexMu = time.Now().Unix()
}

// saveCachedIndex saves the index to cache
func (r *Repository) saveCachedIndex(data []byte) error {
	cachePath := r.getCachePath()
	return os.WriteFile(cachePath, data, 0644)
}

// getCachePath returns the path to the cache file
func (r *Repository) getCachePath() string {
	// Create a hash of the base URL for the cache filename
	hash := hashURL(r.baseURL)
	return filepath.Join(r.cacheDir, hash+".json")
}

// buildURL builds a URL path
func (r *Repository) buildURL(parts ...string) string {
	u, _ := url.JoinPath(r.baseURL, parts...)
	return u
}

// buildURLWithQuery builds a URL with query parameters
func (r *Repository) buildURL(path string, query map[string]string) string {
	u, _ := url.Parse(r.baseURL)
	u.Path = filepath.Join(u.Path, path)

	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	return u.String()
}

// pluginExists checks if a URL returns 200
func (r *Repository) pluginExists(url string) bool {
	resp, err := r.client.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// download downloads content from a URL
func (r *Repository) download(url string) ([]byte, error) {
	resp, err := r.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// extractZip extracts a ZIP archive
func (r *Repository) extractZip(reader io.ReaderAt, targetDir string) error {
	zipReader, err := zip.NewReader(reader, int64(reader.Size()))
	if err != nil {
		return fmt.Errorf("failed to read zip: %w", err)
	}

	for _, f := range zipReader.File {
		path := filepath.Join(targetDir, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}

		// Create parent directories
		os.MkdirAll(filepath.Dir(path), 0755)

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		inFile, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open zip entry: %w", err)
		}

		_, err = io.Copy(outFile, inFile)
		outFile.Close()
		inFile.Close()

		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	return nil
}

// isZip checks if data is a ZIP archive
func isZip(data []byte) bool {
	return len(data) >= 4 &&
		data[0] == 0x50 && data[1] == 0x4B &&
		data[2] == 0x03 && data[3] == 0x04
}

// guessPluginID guesses the plugin ID from a URL
func guessPluginID(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "unknown"
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		// Remove extension
		if idx := strings.LastIndex(last, "."); idx > 0 {
			last = last[:idx]
		}
		return last
	}

	return "unknown"
}

// hashURL creates a simple hash of a URL for cache filename
func hashURL(s string) string {
	// Simple hash - not cryptographic
	hash := 0
	for _, c := range s {
		hash = hash*31 + int(c)
	}
	return fmt.Sprintf("%x", hash)
}

// DefaultRepositoryURL returns the default plugin repository URL
func DefaultRepositoryURL() string {
	return "https://plugins.go-magic.dev"
}
