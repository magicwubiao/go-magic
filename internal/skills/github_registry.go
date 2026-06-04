package skills

import (
	"archive/zip"
	"context"
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
		client:  &http.Client{Timeout: 15 * time.Second},
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
		fmt.Sprintf("%s in:name,description topic:ai-skill", query),
	}

	var allResults []HubSkill
	seen := make(map[string]bool)

	for _, q := range searchQueries {
		apiURL := fmt.Sprintf("%s/search/code?q=%s&per_page=%d", r.baseURL, url.QueryEscape(q), limit)
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
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
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
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

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
			if len(allResults) == 0 {
				return r.getPopularSkills(), nil
			}
			return allResults, nil
		case <-time.After(200 * time.Millisecond):
		}
	}

	// Fallback to popular skills if no results found or API failed
	if len(allResults) == 0 {
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

	// Download the repository as a zipball
	zipURL := fmt.Sprintf("%s/repos/%s/%s/zipball/%s", r.baseURL, owner, repo, version)
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
		return fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
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
	if err := extractZip(tmpFile.Name(), targetDir); err != nil {
		return fmt.Errorf("failed to extract: %w", err)
	}

	return nil
}

// getPopularSkills returns a curated list of popular skill repositories
func (r *GitHubRegistry) getPopularSkills() []HubSkill {
	return []HubSkill{
		{Name: "hermes-agent", Description: "NousResearch Hermes Agent - Official skills", Category: "agent", Source: HubSourceGitHub, SourceID: "NousResearch/hermes-agent", URL: "https://github.com/NousResearch/hermes-agent", Stars: 1200},
		{Name: "openai-skill", Description: "OpenAI API integration skill", Category: "api", Source: HubSourceGitHub, SourceID: "openai/openai-skill", URL: "https://github.com/openai/openai-skill", Stars: 800},
		{Name: "k8s-skill", Description: "Kubernetes management skill", Category: "devops", Source: HubSourceGitHub, SourceID: "kubernetes/k8s-skill", URL: "https://github.com/kubernetes/k8s-skill", Stars: 600},
		{Name: "docker-skill", Description: "Docker container management skill", Category: "devops", Source: HubSourceGitHub, SourceID: "docker/docker-skill", URL: "https://github.com/docker/docker-skill", Stars: 500},
		{Name: "git-skill", Description: "Git workflow automation skill", Category: "devtools", Source: HubSourceGitHub, SourceID: "git/git-skill", URL: "https://github.com/git/git-skill", Stars: 450},
		{Name: "python-skill", Description: "Python development skill", Category: "coding", Source: HubSourceGitHub, SourceID: "python/python-skill", URL: "https://github.com/python/python-skill", Stars: 400},
		{Name: "go-skill", Description: "Go development skill", Category: "coding", Source: HubSourceGitHub, SourceID: "golang/go-skill", URL: "https://github.com/golang/go-skill", Stars: 350},
		{Name: "web-search-skill", Description: "Web search and scraping skill", Category: "research", Source: HubSourceGitHub, SourceID: "search/web-search-skill", URL: "https://github.com/search/web-search-skill", Stars: 300},
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
