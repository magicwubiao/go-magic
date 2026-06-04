package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ClawHubRegistry implements SkillRegistry for ClawHub (official registry)
type ClawHubRegistry struct {
	client  *http.Client
	baseURL string
	token   string
}

// NewClawHubRegistry creates a new ClawHub registry
func NewClawHubRegistry() *ClawHubRegistry {
	return &ClawHubRegistry{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://clawhub.ai",
	}
}

// Name returns the registry name
func (r *ClawHubRegistry) Name() string {
	return "clawhub"
}

// Search searches ClawHub for skills
func (r *ClawHubRegistry) Search(ctx context.Context, query string, limit int) ([]HubSkill, error) {
	if limit <= 0 {
		limit = 10
	}

	// Empty query: return featured skills
	if query == "" {
		return r.getFeaturedSkills(), nil
	}

	apiURL := fmt.Sprintf("%s/api/v1/search?q=%s&limit=%d", r.baseURL, url.QueryEscape(query), limit)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fallback to curated list on API failure
		return r.getFeaturedSkills(), nil
	}

	var result struct {
		Skills []struct {
			Slug        string   `json:"slug"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Category    string   `json:"category"`
			Tags        []string `json:"tags"`
			Version     string   `json:"version"`
			Stars       int      `json:"stars"`
			Installs    int      `json:"installs"`
			Verified    bool     `json:"verified"`
		} `json:"skills"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return r.getFeaturedSkills(), nil
	}

	var skills []HubSkill
	for _, s := range result.Skills {
		skills = append(skills, HubSkill{
			Name:        s.Name,
			Description: s.Description,
			Category:    s.Category,
			Tags:        s.Tags,
			Source:      HubSourceHub,
			SourceID:    s.Slug,
			URL:         fmt.Sprintf("%s/skills/%s", r.baseURL, s.Slug),
			Stars:       s.Stars,
			Installs:    s.Installs,
			Verified:    s.Verified,
		})
	}

	return skills, nil
}

// GetSkillMeta gets metadata for a specific skill
func (r *ClawHubRegistry) GetSkillMeta(ctx context.Context, slug string) (*HubSkill, error) {
	apiURL := fmt.Sprintf("%s/api/v1/skills/%s", r.baseURL, url.QueryEscape(slug))
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub API returned %d", resp.StatusCode)
	}

	var s struct {
		Slug        string   `json:"slug"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Category    string   `json:"category"`
		Tags        []string `json:"tags"`
		Version     string   `json:"version"`
		Stars       int      `json:"stars"`
		Installs    int      `json:"installs"`
		Verified    bool     `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}

	return &HubSkill{
		Name:        s.Name,
		Description: s.Description,
		Category:    s.Category,
		Tags:        s.Tags,
		Source:      HubSourceHub,
		SourceID:    s.Slug,
		URL:         fmt.Sprintf("%s/skills/%s", r.baseURL, s.Slug),
		Stars:       s.Stars,
		Installs:    s.Installs,
		Verified:    s.Verified,
	}, nil
}

// DownloadAndInstall downloads and installs a skill from ClawHub
func (r *ClawHubRegistry) DownloadAndInstall(ctx context.Context, slug, version, targetDir string) error {
	apiURL := fmt.Sprintf("%s/api/v1/download/%s", r.baseURL, url.QueryEscape(slug))
	if version != "" {
		apiURL += "?version=" + url.QueryEscape(version)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-magic-skill-hub")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clawhub download returned %d", resp.StatusCode)
	}

	// Save to temp file
	// TODO: implement zip download and extraction
	return fmt.Errorf("clawhub download not yet fully implemented")
}

// getFeaturedSkills returns curated featured skills
func (r *ClawHubRegistry) getFeaturedSkills() []HubSkill {
	return []HubSkill{
		{Name: "code-review", Description: "Automated code review and quality analysis", Category: "code-review", Source: HubSourceHub, SourceID: "code-review", Verified: true, Stars: 500},
		{Name: "documentation", Description: "Generate and maintain project documentation", Category: "documentation", Source: HubSourceHub, SourceID: "documentation", Verified: true, Stars: 400},
		{Name: "testing", Description: "Automated testing and test generation", Category: "testing", Source: HubSourceHub, SourceID: "testing", Verified: true, Stars: 350},
		{Name: "debug", Description: "Debug assistance and error analysis", Category: "debug", Source: HubSourceHub, SourceID: "debug", Verified: true, Stars: 300},
		{Name: "security-audit", Description: "Security vulnerability scanning", Category: "security", Source: HubSourceHub, SourceID: "security-audit", Verified: true, Stars: 280},
		{Name: "api-design", Description: "API design and documentation", Category: "development", Source: HubSourceHub, SourceID: "api-design", Verified: true, Stars: 250},
		{Name: "refactor", Description: "Code refactoring suggestions", Category: "code-quality", Source: HubSourceHub, SourceID: "refactor", Verified: true, Stars: 220},
		{Name: "git-workflow", Description: "Git workflow and commit message assistance", Category: "devtools", Source: HubSourceHub, SourceID: "git-workflow", Verified: true, Stars: 200},
	}
}
