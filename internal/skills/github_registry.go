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

// SkillCollection defines a GitHub repository that contains multiple skills
type SkillCollection struct {
	Owner       string // GitHub owner
	Repo        string // GitHub repo
	SkillsPath  string // Path to skills directory (e.g., "optional-skills")
	Description string // Description for display
	Category    string // Category
}

// Known skill collections on GitHub - verified to exist
var knownSkillCollections = []SkillCollection{
	{"NousResearch", "hermes-agent", "optional-skills", "Hermes Agent - Self-evolving AI Agent with skills", "agent"},
	{"andrewyng", "context-hub", "skills", "API documentation knowledge base for Claude Code", "docs"},
	{"garrytan", "gstack", "skills", "Skills collection to turn Claude Code into a virtual dev team", "skills"},
}

// SearchSkillCollections searches for skills within known skill collections
func (r *GitHubRegistry) SearchSkillCollections(ctx context.Context, query string) ([]HubSkill, error) {
	var allResults []HubSkill
	keyword := strings.ToLower(query)

	for _, coll := range knownSkillCollections {
		skills, err := r.getSkillsFromCollection(ctx, coll, keyword)
		if err != nil {
			fmt.Printf("Failed to fetch skills from %s/%s: %v\n", coll.Owner, coll.Repo, err)
			continue
		}
		allResults = append(allResults, skills...)
	}

	return allResults, nil
}

// getSkillsFromCollection fetches skills from a specific GitHub skill collection
func (r *GitHubRegistry) getSkillsFromCollection(ctx context.Context, coll SkillCollection, keyword string) ([]HubSkill, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", r.baseURL, coll.Owner, coll.Repo, url.QueryEscape(coll.SkillsPath))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var contents []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, err
	}

	var results []HubSkill
	for _, item := range contents {
		if item.Type != "dir" {
			continue
		}

		// Build SourceID as "owner/repo/path" for installation
		sourceID := fmt.Sprintf("%s/%s/%s/%s", coll.Owner, coll.Repo, coll.SkillsPath, item.Name)

		skill := HubSkill{
			Name:        item.Name,
			Description: fmt.Sprintf("%s: %s", coll.Description, item.Name),
			Category:    coll.Category,
			Source:      HubSourceGitHub,
			SourceID:    sourceID,
			URL:         fmt.Sprintf("https://github.com/%s/%s/tree/main/%s/%s", coll.Owner, coll.Repo, coll.SkillsPath, item.Name),
		}

		// Keyword filtering
		if keyword != "" {
			if !strings.Contains(strings.ToLower(skill.Name), keyword) &&
				!strings.Contains(strings.ToLower(skill.Description), keyword) {
				continue
			}
		}

		results = append(results, skill)
	}

	return results, nil
}

// Search searches GitHub for skill repositories
// Returns empty results if query is empty - user must explicitly search
func (r *GitHubRegistry) Search(ctx context.Context, query string, limit int) ([]HubSkill, error) {
	if limit <= 0 {
		limit = 10
	}

	// Don't return results for empty query - user must search explicitly
	if query == "" {
		return []HubSkill{}, nil
	}

	var allResults []HubSkill
	seen := make(map[string]bool)

	// Search for repositories containing skills
	// Use repository search instead of code search for better results
	searchQueries := []string{
		fmt.Sprintf("%s SKILL.md", query),
		fmt.Sprintf("%s skill", query),
		fmt.Sprintf("%s claude skill", query),
		fmt.Sprintf("%s hermes skill", query),
	}

	for _, q := range searchQueries {
		// Use repository search API instead of code search
		apiURL := fmt.Sprintf("%s/search/repositories?q=%s&per_page=%d", r.baseURL, url.QueryEscape(q), limit)
		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "go-magic-skill-hub")
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		if r.authToken != "" {
			req.Header.Set("Authorization", "token "+r.authToken)
		}

		resp, err := r.client.Do(req)
		if err != nil {
			continue
		}

		// Check for rate limiting
		if resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var result struct {
			Items []struct {
				FullName    string   `json:"full_name"`
				Name        string   `json:"name"`
				Description string   `json:"description"`
				HTMLURL     string   `json:"html_url"`
				Stargazers  int      `json:"stargazers_count"`
				Topics      []string `json:"topics"`
			} `json:"items"`
			Message string `json:"message"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		// Check for API error message
		if result.Message != "" {
			continue
		}

		for _, item := range result.Items {
			repo := item.FullName
			if seen[repo] {
				continue
			}
			seen[repo] = true

			// Skip known skill collection repositories (they are handled separately)
			isCollection := false
			for _, coll := range knownSkillCollections {
				if coll.Owner+"/"+coll.Repo == repo {
					isCollection = true
					break
				}
			}
			if isCollection {
				continue
			}

			// Determine category from topics
			category := "github"
			for _, topic := range item.Topics {
				switch topic {
				case "ai", "machine-learning", "llm":
					category = "ai"
				case "devops", "kubernetes", "docker":
					category = "devops"
				case "security":
					category = "security"
				case "productivity", "automation":
					category = "productivity"
				case "testing", "qa":
					category = "testing"
				case "documentation":
					category = "documentation"
				case "web", "api":
					category = "web"
				}
			}

			desc := item.Description
			if desc == "" {
				desc = fmt.Sprintf("Skill from %s", repo)
			}

			allResults = append(allResults, HubSkill{
				Name:        item.Name,
				Description: desc,
				Category:    category,
				Source:      HubSourceGitHub,
				SourceID:    repo,
				URL:         item.HTMLURL,
				Stars:       item.Stargazers,
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

	// Search within skill collections (hermes-agent, context-hub, gstack)
	// These are repositories that contain multiple skills
	collectionSkills, err := r.SearchSkillCollections(ctx, query)
	if err == nil {
		for _, skill := range collectionSkills {
			if !seen[skill.SourceID] {
				seen[skill.SourceID] = true
				allResults = append(allResults, skill)
			}
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
// slug can be:
//   - "owner/repo" - a single skill repository
//   - "owner/repo/path/to/skill" - a skill within a collection repository
func (r *GitHubRegistry) DownloadAndInstall(ctx context.Context, slug, version, targetDir string) error {
	// slug format: "owner/repo" or "owner/repo/path/to/skill"
	parts := strings.Split(slug, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid slug format: %s", slug)
	}

	owner := parts[0]
	repo := parts[1]
	skillPath := ""

	// Check if this is a skill collection path (owner/repo/path/to/skill)
	if len(parts) > 2 {
		skillPath = strings.Join(parts[2:], "/")
	}

	if version == "" {
		version = "main"
	}

	// If this is a skill within a collection (has skill path), download just that directory
	if skillPath != "" {
		return r.downloadSkillFromCollection(ctx, owner, repo, version, skillPath, targetDir)
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

// downloadSkillFromCollection downloads a single skill from a skill collection repository
// This supports recursive downloading of subdirectories
func (r *GitHubRegistry) downloadSkillFromCollection(ctx context.Context, owner, repo, ref, skillPath, targetDir string) error {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", r.baseURL, owner, repo, url.QueryEscape(skillPath), url.QueryEscape(ref))

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
		return fmt.Errorf("GitHub API returned %d for skill path %s", resp.StatusCode, skillPath)
	}

	var contents []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Path string `json:"path"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return err
	}

	// Download each item (file or directory) in the skill directory
	for _, item := range contents {
		if item.Type == "dir" {
			// Recursively download subdirectory
			subDir := filepath.Join(targetDir, item.Name)
			if err := os.MkdirAll(subDir, 0755); err != nil {
				return err
			}
			if err := r.downloadDirContents(ctx, item.URL, subDir); err != nil {
				return fmt.Errorf("failed to download directory %s: %w", item.Name, err)
			}
			continue
		}

		if item.Type != "file" {
			continue
		}

		filePath := filepath.Join(targetDir, item.Name)
		if err := r.downloadFile(ctx, item.URL, filePath); err != nil {
			return fmt.Errorf("failed to download file %s: %w", item.Name, err)
		}
	}

	return nil
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
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
		URL  string `json:"url"`
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
// NOTE: hermes-agent, context-hub, gstack are skill COLLECTIONS containing multiple skills
// They should not be listed here as they will download all skills when installed
func (r *GitHubRegistry) getPopularSkills() []HubSkill {
	return []HubSkill{
		{Name: "claude-mem", Description: "Persistent memory plugin for Claude Code", Category: "memory", Source: HubSourceGitHub, SourceID: "thedotmack/claude-mem", URL: "https://github.com/thedotmack/claude-mem", Stars: 80000},
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
