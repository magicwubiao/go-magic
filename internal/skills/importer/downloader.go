package importer

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/skills"
)

// URLType represents the type of URL
type URLType int

const (
	URLTypeUnknown URLType = iota
	URLTypeGitHubRepo   // github.com tree view
	URLTypeGitHubRaw    // raw.githubusercontent.com
	URLTypeGist         // gist.github.com
	URLTypeHTTPFile     // Regular HTTP file
	URLTypeHTTPZip      // HTTP ZIP archive
	URLTypeGitHubZip    // GitHub archive download
)

// GitHub API types
type GitHubTreeItem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type GitHubTreeResponse struct {
	Tree []GitHubTreeItem `json:"tree"`
}

// GitHub contents API response
type GitHubContent struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Content     string `json:"content,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	SHA         string `json:"sha"`
}

// Gist file info
type GistFile struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	RawURL      string `json:"raw_url"`
	Size        int    `json:"size"`
	Type        string `json:"type"`
	Language    string `json:"language"`
}

type GistInfo struct {
	ID     string          `json:"id"`
	Files  map[string]GistFile `json:"files"`
}

// Downloader handles downloading skills from various URL sources
type Downloader struct {
	Client   *http.Client
	TempDir  string
}

// NewDownloader creates a new URL downloader
func NewDownloader() (*Downloader, error) {
	tempDir, err := os.MkdirTemp("", "magic-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Downloader{
		Client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		TempDir: tempDir,
	}, nil
}

// Download downloads content from a URL and returns the local path
func (d *Downloader) Download(rawURL string) (string, error) {
	urlType := d.DetectURLType(rawURL)

	switch urlType {
	case URLTypeGitHubRepo:
		return d.DownloadGitHubRepo(rawURL)
	case URLTypeGitHubRaw:
		return d.DownloadHTTP(rawURL)
	case URLTypeGist:
		return d.DownloadGist(rawURL)
	case URLTypeHTTPZip:
		path, err := d.DownloadHTTP(rawURL)
		if err != nil {
			return "", err
		}
		return d.extractZip(path)
	case URLTypeGitHubZip:
		return d.DownloadGitHubZip(rawURL)
	default:
		return d.DownloadHTTP(rawURL)
	}
}

// DetectURLType detects the type of URL
func (d *Downloader) DetectURLType(rawURL string) URLType {
	u, err := url.Parse(rawURL)
	if err != nil {
		return URLTypeUnknown
	}

	host := strings.ToLower(u.Host)
	path := u.Path

	// GitHub raw content
	if host == "raw.githubusercontent.com" {
		return URLTypeGitHubRaw
	}

	// GitHub tree view
	if host == "github.com" && !strings.Contains(path, "/archive/") {
		if strings.Contains(path, "/tree/") {
			return URLTypeGitHubRepo
		}
	}

	// GitHub archive download
	if host == "github.com" && strings.Contains(path, "/archive/") {
		return URLTypeGitHubZip
	}

	// Gist
	if host == "gist.github.com" {
		return URLTypeGist
	}

	// Regular HTTP - check file extension
	lowerPath := strings.ToLower(path)
	if strings.HasSuffix(lowerPath, ".zip") {
		return URLTypeHTTPZip
	}

	return URLTypeHTTPFile
}

// parseGitHubURL parses a GitHub URL and extracts owner, repo, branch, and path
func parseGitHubURL(rawURL string) (owner, repo, branch, pathInRepo string, err error) {
	// Handle different GitHub URL formats
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/.*?/([^/]+)/(.+)`)
	matches := re.FindStringSubmatch(rawURL)

	if len(matches) < 5 {
		return "", "", "", "", fmt.Errorf("invalid GitHub URL format")
	}

	return matches[1], matches[2], matches[3], matches[4], nil
}

// DownloadGitHubRepo downloads files from a GitHub repository directory
func (d *Downloader) DownloadGitHubRepo(rawURL string) (string, error) {
	owner, repo, branch, pathInRepo, err := parseGitHubURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse GitHub URL: %w", err)
	}

	// Create a temporary directory for this download
	downloadDir, err := os.MkdirTemp(d.TempDir, "github-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Use GitHub Contents API to get files
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		owner, repo, pathInRepo, branch)

	fmt.Printf("Fetching from: %s\n", apiURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent (required by GitHub API)
	req.Header.Set("User-Agent", "go-magic-skill-importer")

	resp, err := d.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// Check for rate limit
		if strings.Contains(resp.Header.Get("X-RateLimit-Remaining"), "0") {
			return "", errors.New("GitHub API rate limit exceeded. Please try again later or provide a GitHub token")
		}
		return "", fmt.Errorf("access forbidden: %s", resp.Status)
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("path not found: %s/%s/%s/%s", owner, repo, branch, pathInRepo)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var contents []GitHubContent
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Handle single file
	if len(contents) == 0 {
		return "", fmt.Errorf("no content found at path")
	}

	// If this is a file, contents will be a single item
	// If this is a directory, contents will be an array

	// Check if it's a single file response (no array)
	if len(contents) == 1 && contents[0].Type == "file" && contents[0].DownloadURL != "" {
		// Single file - download it
		filePath, err := d.downloadFile(contents[0].DownloadURL, contents[0].Name, downloadDir)
		if err != nil {
			return "", fmt.Errorf("failed to download file: %w", err)
		}
		return filePath, nil
	}

	// Directory - download all files recursively
	for _, content := range contents {
		if content.Type == "file" {
			filePath, err := d.downloadFile(content.DownloadURL, content.Name, downloadDir)
			if err != nil {
				return "", fmt.Errorf("failed to download %s: %w", content.Name, err)
			}
			fmt.Printf("  Downloaded: %s\n", filePath)
		} else if content.Type == "dir" {
			// Recursively download subdirectory
			subDir := filepath.Join(downloadDir, filepath.Base(content.Path))
			if err := os.MkdirAll(subDir, 0755); err != nil {
				return "", fmt.Errorf("failed to create subdirectory: %w", err)
			}
			if err := d.downloadGitHubDir(owner, repo, branch, content.Path, subDir); err != nil {
				return "", fmt.Errorf("failed to download directory %s: %w", content.Path, err)
			}
		}
	}

	return downloadDir, nil
}

// downloadGitHubDir downloads a directory from GitHub using contents API
func (d *Downloader) downloadGitHubDir(owner, repo, branch, path, destDir string) error {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		owner, repo, path, branch)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "go-magic-skill-importer")

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error: %s", resp.Status)
	}

	var contents []GitHubContent
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	for _, content := range contents {
		if content.Type == "file" {
			_, err := d.downloadFile(content.DownloadURL, content.Name, destDir)
			if err != nil {
				return fmt.Errorf("failed to download %s: %w", content.Name, err)
			}
			fmt.Printf("    Downloaded: %s\n", content.Name)
		} else if content.Type == "dir" {
			subDir := filepath.Join(destDir, content.Name)
			if err := os.MkdirAll(subDir, 0755); err != nil {
				return fmt.Errorf("failed to create subdirectory: %w", err)
			}
			if err := d.downloadGitHubDir(owner, repo, branch, content.Path, subDir); err != nil {
				return err
			}
		}
	}

	return nil
}

// DownloadGitHubZip downloads a GitHub archive (zip)
func (d *Downloader) DownloadGitHubZip(rawURL string) (string, error) {
	// Convert github.com URL to archive URL
	// https://github.com/user/repo/archive/refs/heads/main.zip
	// or https://github.com/user/repo/archive/refs/tags/v1.0.zip
	
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/(archive|tarball)/(.+)`)
	matches := re.FindStringSubmatch(rawURL)
	
	if len(matches) < 5 {
		return "", fmt.Errorf("invalid GitHub archive URL format")
	}

	// Convert to zipball URL
	zipURL := fmt.Sprintf("https://github.com/%s/%s/archive/%s.zip", 
		matches[1], matches[2], matches[4])

	return d.DownloadHTTP(zipURL)
}

// DownloadGist downloads all files from a GitHub Gist
func (d *Downloader) DownloadGist(rawURL string) (string, error) {
	// Extract Gist ID from URL
	re := regexp.MustCompile(`gist\.github\.com/([^/]+)/([a-f0-9]+)`)
	matches := re.FindStringSubmatch(rawURL)

	var gistID string
	if len(matches) >= 3 {
		gistID = matches[2]
	} else {
		// Try as direct gist ID
		re2 := regexp.MustCompile(`([a-f0-9]{20,})`)
		matches2 := re2.FindStringSubmatch(rawURL)
		if len(matches2) < 2 {
			return "", fmt.Errorf("invalid Gist URL format")
		}
		gistID = matches2[1]
	}

	// Fetch Gist info from API
	apiURL := fmt.Sprintf("https://api.github.com/gists/%s", gistID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "go-magic-skill-importer")

	resp, err := d.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Gist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return "", errors.New("GitHub API rate limit exceeded. Please try again later")
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("Gist not found: %s", gistID)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var gist GistInfo
	if err := json.NewDecoder(resp.Body).Decode(&gist); err != nil {
		return "", fmt.Errorf("failed to decode Gist response: %w", err)
	}

	// Create temp directory
	downloadDir, err := os.MkdirTemp(d.TempDir, "gist-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Download each file
	for filename, file := range gist.Files {
		content, err := d.fetchContent(file.RawURL)
		if err != nil {
			return "", fmt.Errorf("failed to download %s: %w", filename, err)
		}

		// Write file
		filePath := filepath.Join(downloadDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("failed to write %s: %w", filename, err)
		}
		fmt.Printf("  Downloaded: %s\n", filename)
	}

	return downloadDir, nil
}

// DownloadHTTP downloads a file from a regular HTTP URL
func (d *Downloader) DownloadHTTP(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	filename := filepath.Base(parsedURL.Path)
	if filename == "" || filename == "." {
		filename = "downloaded_file"
	}

	downloadPath := filepath.Join(d.TempDir, filename)

	return d.downloadFile(rawURL, filename, downloadPath)
}

// downloadFile downloads a file from a URL and saves it to the destination
func (d *Downloader) downloadFile(fileURL, filename, destDir string) (string, error) {
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "go-magic-skill-importer")

	resp, err := d.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error: %s", resp.Status)
	}

	// Check content length for progress
	contentLength := resp.ContentLength
	if contentLength > 0 {
		fmt.Printf("  Downloading %s (%.2f KB)...\n", filename, float64(contentLength)/1024)
	}

	// Create file
	destPath := filepath.Join(destDir, filename)
	file, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Download with progress tracking
	var totalWritten int64
	reader := io.TeeReader(resp.Body, &progressWriter{
		total:   contentLength,
		display: filename,
	})

	written, err := io.Copy(file, reader)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	totalWritten += written
	fmt.Printf("  Downloaded: %s (%.2f KB)\n", filename, float64(totalWritten)/1024)

	return destPath, nil
}

// fetchContent fetches raw content from a URL
func (d *Downloader) fetchContent(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "go-magic-skill-importer")

	resp, err := d.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

// extractZip extracts a ZIP file and returns the extraction directory
func (d *Downloader) extractZip(zipPath string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to open ZIP: %w", err)
	}
	defer reader.Close()

	// Create extraction directory
	extractDir := strings.TrimSuffix(zipPath, filepath.Ext(zipPath))
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create extraction directory: %w", err)
	}

	// Track the root directory name in the archive
	var rootDir string

	for _, file := range reader.File {
		if rootDir == "" {
			// Extract root directory name
			parts := strings.SplitN(file.Name, "/", 2)
			if len(parts) > 0 {
				rootDir = parts[0]
			}
		}

		if err := d.extractFile(file, extractDir); err != nil {
			return "", fmt.Errorf("failed to extract %s: %w", file.Name, err)
		}
	}

	// If there's a single root directory, return that instead
	if rootDir != "" {
		possibleRoot := filepath.Join(d.TempDir, rootDir)
		if _, err := os.Stat(possibleRoot); err == nil {
			return possibleRoot, nil
		}
	}

	return extractDir, nil
}

// extractFile extracts a single file from a ZIP archive
func (d *Downloader) extractFile(file *zip.File, destDir string) error {
	filePath := filepath.Join(destDir, file.Name)

	// Check for Zip Slip vulnerability
	if !strings.HasPrefix(filePath, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("invalid file path: %s", file.Name)
	}

	if file.FileInfo().IsDir() {
		return os.MkdirAll(filePath, file.Mode())
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	destFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	srcFile, err := file.Open()
	if err != nil {
		return err
	}
	defer srcFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

// Cleanup removes the temporary directory
func (d *Downloader) Cleanup() error {
	if d.TempDir != "" {
		return os.RemoveAll(d.TempDir)
	}
	return nil
}

// IsURL checks if the given path is a URL
func IsURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

// DownloadAndImport downloads a skill from a URL and imports it
func DownloadAndImport(manager *skills.Manager, rawURL string, force bool) (*ImportResult, error) {
	downloader, err := NewDownloader()
	if err != nil {
		return nil, fmt.Errorf("failed to create downloader: %w", err)
	}
	defer downloader.Cleanup()

	fmt.Printf("Downloading from: %s\n", rawURL)

	downloadDir, err := downloader.Download(rawURL)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	fmt.Printf("\nDownloaded to: %s\n", downloadDir)

	// Import from downloaded directory
	imp := NewImporter(manager)
	return imp.Import(downloadDir, force), nil
}

// DownloadAndImportRecursive downloads skills from a URL directory and imports all
func DownloadAndImportRecursive(manager *skills.Manager, rawURL string, force bool) ([]*ImportResult, error) {
	downloader, err := NewDownloader()
	if err != nil {
		return nil, fmt.Errorf("failed to create downloader: %w", err)
	}
	defer downloader.Cleanup()

	fmt.Printf("Downloading from: %s\n", rawURL)

	downloadDir, err := downloader.Download(rawURL)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	fmt.Printf("\nDownloaded to: %s\n", downloadDir)

	// Import recursively from downloaded directory
	imp := NewImporter(manager)
	return imp.ImportRecursive(downloadDir, force), nil
}

// GetFilesFromURL downloads files from a URL and returns them as a map
func GetFilesFromURL(rawURL string) (map[string]string, error) {
	downloader, err := NewDownloader()
	if err != nil {
		return nil, fmt.Errorf("failed to create downloader: %w", err)
	}
	defer downloader.Cleanup()

	downloadDir, err := downloader.Download(rawURL)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	// Read all files from the downloaded directory
	files := make(map[string]string)

	err = filepath.WalkDir(downloadDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(downloadDir, path)
		if err != nil {
			return err
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files[relPath] = string(content)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read downloaded files: %w", err)
	}

	return files, nil
}

// progressWriter implements io.Writer for progress tracking
type progressWriter struct {
	total   int64
	display string
	last    int64
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	pw.last += int64(n)

	if pw.total > 0 && pw.last > 0 {
		percent := float64(pw.last) / float64(pw.total) * 100
		if percent >= 100 {
			fmt.Printf("\r  Downloading %s... %.0f%%", pw.display, percent)
		}
	}

	return n, nil
}

// ParseURLTypeFromString converts a string to URLType
func ParseURLTypeFromString(s string) URLType {
	switch strings.ToLower(s) {
	case "githubrepo", "github-repo":
		return URLTypeGitHubRepo
	case "githubraw", "github-raw":
		return URLTypeGitHubRaw
	case "gist":
		return URLTypeGist
	case "httpfile", "http-file":
		return URLTypeHTTPFile
	case "httpzip", "http-zip":
		return URLTypeHTTPZip
	case "githubzip", "github-zip":
		return URLTypeGitHubZip
	default:
		return URLTypeUnknown
	}
}

// String returns the string representation of URLType
func (u URLType) String() string {
	switch u {
	case URLTypeGitHubRepo:
		return "GitHub Repository"
	case URLTypeGitHubRaw:
		return "GitHub Raw"
	case URLTypeGist:
		return "GitHub Gist"
	case URLTypeHTTPFile:
		return "HTTP File"
	case URLTypeHTTPZip:
		return "HTTP ZIP Archive"
	case URLTypeGitHubZip:
		return "GitHub Archive"
	default:
		return "Unknown"
	}
}

// URLTypeNames returns all valid URL type names
func URLTypeNames() []string {
	return []string{
		"githubrepo",
		"githubraw",
		"gist",
		"httpfile",
		"httpzip",
		"githubzip",
	}
}
