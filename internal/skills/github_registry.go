package skills

import (
	"archive/zip"
	"context"
	"encoding/base64"
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

// GitHubRegistry implements SkillRegistry for GitHub
type GitHubRegistry struct {
	client    *http.Client
	baseURL   string
	authToken string
}

// NewGitHubRegistry creates a new GitHub registry
func NewGitHubRegistry() *GitHubRegistry {
	return &GitHubRegistry{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: "https://api.github.com",
	}
}

// Name returns the registry name
func (r *GitHubRegistry) Name() string {
	return "github"
}

// Search searches GitHub for skill repositories
func (r *GitHubRegistry) Search(ctx context.Context, query string, limit int) ([]HubSkill, error) {
	if limit <= 0 {
		limit = 10
	}

	// Empty query: return popular skills
	if query == "" {
		return r.getPopularSkills(), nil
	}

	// Search for repos with SKILL.md
	searchQueries := []string{
		fmt.Sprintf("%s filename:SKILL.md", query),
		fmt.Sprintf("%s filename:skill.md", query),
		fmt.Sprintf("%s in:name,description topic:ai-skill", query),
		fmt.Sprintf("%s in:name,description topic:claudeskill", query),
		fmt.Sprintf("%s in:name,description topic:hermes-skill", query),
	}

	var allResults []HubSkill
	seen := make(map[string]bool)
	var lastErr error

	for _, q := range searchQueries {
		apiURL := fmt.Sprintf("%s/search/code?q=%s&per_page=%d", r.baseURL, url.QueryEscape(q), limit)
		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}
		req.Header.Set("User-Agent", "go-magic-skill-hub")
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		if r.authToken != "" {
			req.Header.Set("Authorization", "token "+r.authToken)
		}

		resp, err := r.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		// Check for rate limiting
		if resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			lastErr = fmt.Errorf("github API rate limit exceeded")
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("github API returned %d", resp.StatusCode)
			continue
		}

		var result struct {
			Items []struct {
				Repository struct {
					FullName    string `json:"full_name"`
					Description string `json:"description"`
					HTMLURL     string `json:"html_url"`
					Stargazers  int    `json:"stargazers_count"`
				} `json:"repository"`
				Path string `json:"path"`
			} `json:"items"`
			Message string `json:"message"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("failed to decode response: %w", err)
			continue
		}
		resp.Body.Close()

		// Check for API error message
		if result.Message != "" {
			lastErr = fmt.Errorf("github API error: %s", result.Message)
			continue
		}

		for _, item := range result.Items {
			repo := item.Repository.FullName
			if seen[repo] {
				continue
			}
			seen[repo] = true

			skillName := repo
			if idx := strings.LastIndex(repo, "/"); idx >= 0 {
				skillName = repo[idx+1:]
			}

			desc := item.Repository.Description
			if desc == "" {
				desc = fmt.Sprintf("Skill from %s", repo)
			}

			allResults = append(allResults, HubSkill{
				Name:        skillName,
				Description: desc,
				Category:    "github",
				Source:      HubSourceGitHub,
				SourceID:    repo,
				URL:         item.Repository.HTMLURL,
				Stars:       item.Repository.Stargazers,
			})
		}

		// Rate limit: sleep between requests
		select {
		case <-ctx.Done():
			if len(allResults) > 0 {
				return allResults, nil
			}
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	// If no results from search, return popular skills as fallback
	if len(allResults) == 0 {
		fmt.Printf("GitHub search returned no results, returning popular skills as fallback\n")
		return r.getPopularSkills(), nil
	}

	return allResults, nil
}

// GetSkillMeta gets metadata for a specific skill
func (r *GitHubRegistry) GetSkillMeta(ctx context.Context, slug string) (*HubSkill, error) {
	// slug is "owner/repo"
	parts := strings.Split(slug, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid slug format: %s", slug)
	}

	apiURL := fmt.Sprintf("%s/repos/%s", r.baseURL, slug)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if r.authToken != "" {
		req.Header.Set("Authorization", "token "+r.authToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	var repo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		HTMLURL     string `json:"html_url"`
		Stargazers  int    `json:"stargazers_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return nil, err
	}

	return &HubSkill{
		Name:        repo.Name,
		Description: repo.Description,
		Category:    "github",
		Source:      HubSourceGitHub,
		SourceID:    slug,
		URL:         repo.HTMLURL,
		Stars:       repo.Stargazers,
	}, nil
}

// DownloadAndInstall downloads and installs a skill from GitHub
// Uses GitHub Contents API to download files recursively (like PicoClaw)
func (r *GitHubRegistry) DownloadAndInstall(ctx context.Context, slug, version, targetDir string) error {
	// slug is "owner/repo"
	parts := strings.Split(slug, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid slug format: %s", slug)
	}
	owner, repo := parts[0], parts[1]

	if version == "" {
		version = "main"
	}

	// Try GitHub Contents API first (like PicoClaw)
	err := r.downloadViaContentsAPI(ctx, owner, repo, version, targetDir)
	if err == nil {
		return nil
	}
	fmt.Printf("Contents API failed: %v, trying zipball fallback...\n", err)

	// Fallback to zipball download
	err = r.downloadViaZipball(ctx, owner, repo, version, targetDir)
	if err == nil {
		return nil
	}
	fmt.Printf("Zipball download failed: %v, trying raw fallback...\n", err)

	// Final fallback: raw file download
	return r.downloadViaRaw(ctx, owner, repo, version, targetDir)
}

// downloadViaContentsAPI uses GitHub Contents API to download files recursively
func (r *GitHubRegistry) downloadViaContentsAPI(ctx context.Context, owner, repo, ref, targetDir string) error {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/contents?ref=%s", r.baseURL, owner, repo, url.QueryEscape(ref))
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if r.authToken != "" {
		req.Header.Set("Authorization", "token "+r.authToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("contents API returned %d", resp.StatusCode)
	}

	var items []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Type  string `json:"type"`
		URL   string `json:"url"`
		Links struct {
			Self string `json:"self"`
		} `json:"_links"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return err
	}

	// Download each file
	for _, item := range items {
		if item.Type == "dir" {
			// Recursively download subdirectory
			subDir := filepath.Join(targetDir, item.Name)
			if err := os.MkdirAll(subDir, 0755); err != nil {
				return err
			}
			if err := r.downloadDirContents(ctx, item.URL, subDir); err != nil {
				return err
			}
			continue
		}
		if item.Type != "file" {
			continue
		}

		// Download file
		filePath := filepath.Join(targetDir, item.Name)
		if err := r.downloadFile(ctx, item.URL, filePath); err != nil {
			return err
		}
	}

	return nil
}

// downloadDirContents recursively downloads directory contents
func (r *GitHubRegistry) downloadDirContents(ctx context.Context, apiURL, targetDir string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if r.authToken != "" {
		req.Header.Set("Authorization", "token "+r.authToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dir contents API returned %d", resp.StatusCode)
	}

	var items []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Type  string `json:"type"`
		URL   string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return err
	}

	for _, item := range items {
		if item.Type == "dir" {
			subDir := filepath.Join(targetDir, item.Name)
			if err := os.MkdirAll(subDir, 0755); err != nil {
				return err
			}
			if err := r.downloadDirContents(ctx, item.URL, subDir); err != nil {
				return err
			}
			continue
		}
		if item.Type != "file" {
			continue
		}

		filePath := filepath.Join(targetDir, item.Name)
		if err := r.downloadFile(ctx, item.URL, filePath); err != nil {
			return err
		}
	}

	return nil
}

// downloadFile downloads a single file from GitHub
func (r *GitHubRegistry) downloadFile(ctx context.Context, apiURL, filePath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if r.authToken != "" {
		req.Header.Set("Authorization", "token "+r.authToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("file API returned %d", resp.StatusCode)
	}

	var fileInfo struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Download string `json:"download_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fileInfo); err != nil {
		return err
	}

	// Decode base64 content
	if fileInfo.Encoding == "base64" && fileInfo.Content != "" {
		decoded, err := base64.StdEncoding.DecodeString(fileInfo.Content)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}
		return os.WriteFile(filePath, decoded, 0644)
	}

	// Fallback to download URL
	if fileInfo.Download != "" {
		return r.downloadFromURL(ctx, fileInfo.Download, filePath)
	}

	return fmt.Errorf("no content or download URL available")
}

// downloadFromURL downloads a file from a direct URL
func (r *GitHubRegistry) downloadFromURL(ctx context.Context, urlStr, filePath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	out, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// downloadViaZipball downloads repository as zipball
func (r *GitHubRegistry) downloadViaZipball(ctx context.Context, owner, repo, ref, targetDir string) error {
	zipURL := fmt.Sprintf("%s/repos/%s/%s/zipball/%s", r.baseURL, owner, repo, url.QueryEscape(ref))
	req, err := http.NewRequestWithContext(ctx, "GET", zipURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	if r.authToken != "" {
		req.Header.Set("Authorization", "token "+r.authToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("zipball returned %d", resp.StatusCode)
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "skill-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	// Extract zip
	return extractZip(tmpFile.Name(), targetDir)
}

// downloadViaRaw downloads SKILL.md directly from raw.githubusercontent.com
func (r *GitHubRegistry) downloadViaRaw(ctx context.Context, owner, repo, ref, targetDir string) error {
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/SKILL.md", owner, repo, url.QueryEscape(ref))
	
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("raw download returned %d", resp.StatusCode)
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	out, err := os.Create(filepath.Join(targetDir, "SKILL.md"))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// getPopularSkills returns a curated list of REAL skill repositories that exist on GitHub
func (r *GitHubRegistry) getPopularSkills() []HubSkill {
	return []HubSkill{
		{Name: "hermes-agent", Description: "NousResearch Hermes Agent - Self-evolving AI Agent with skills", Category: "agent", Source: HubSourceGitHub, SourceID: "NousResearch/hermes-agent", URL: "https://github.com/NousResearch/hermes-agent", Stars: 164000},
		{Name: "context-hub", Description: "API documentation knowledge base for Claude Code", Category: "docs", Source: HubSourceGitHub, SourceID: "andrewyng/context-hub", URL: "https://github.com/andrewyng/context-hub", Stars: 13000},
		{Name: "claude-mem", Description: "Persistent memory plugin for Claude Code", Category: "memory", Source: HubSourceGitHub, SourceID: "thedotmack/claude-mem", URL: "https://github.com/thedotmack/claude-mem", Stars: 80000},
		{Name: "gstack", Description: "Skills collection to turn Claude Code into a virtual dev team", Category: "skills", Source: HubSourceGitHub, SourceID: "garrytan/gstack", URL: "https://github.com/garrytan/gstack", Stars: 101000},
		{Name: "page-agent", Description: "Alibaba Page Agent - Web page interaction agent", Category: "agent", Source: HubSourceGitHub, SourceID: "alibaba/page-agent", URL: "https://github.com/alibaba/page-agent", Stars: 18000},
		{Name: "FireRed-OpenStoryline", Description: "AI-driven conversational video creation agent", Category: "video", Source: HubSourceGitHub, SourceID: "FireRedTeam/FireRed-OpenStoryline", URL: "https://github.com/FireRedTeam/FireRed-OpenStoryline", Stars: 2800},
		{Name: "nezha", Description: "Multi-project AI coding assistant manager", Category: "coding", Source: HubSourceGitHub, SourceID: "hanshuaikang/nezha", URL: "https://github.com/hanshuaikang/nezha", Stars: 1300},
		{Name: "paseo", Description: "Unified platform for Claude Code, Codex and OpenCode", Category: "platform", Source: HubSourceGitHub, SourceID: "getpaseo/paseo", URL: "https://github.com/getpaseo/paseo", Stars: 6600},
	}
}

// extractZip extracts a zip file to the target directory
func extractZip(zipPath, targetDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	// GitHub zipballs have a single root directory like "owner-repo-hash/"
	var rootDir string
	for _, f := range r.File {
		parts := strings.Split(f.Name, "/")
		if len(parts) > 0 && rootDir == "" {
			rootDir = parts[0]
		}
		break
	}

	for _, f := range r.File {
		// Skip the root directory itself
		relPath := strings.TrimPrefix(f.Name, rootDir+"/")
		if relPath == "" {
			continue
		}

		path := filepath.Join(targetDir, relPath)
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	return nil
}