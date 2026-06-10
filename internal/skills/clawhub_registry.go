package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
// Returns featured skills if query is empty
func (r *ClawHubRegistry) Search(ctx context.Context, query string, limit int) ([]HubSkill, error) {
	if limit <= 0 {
		limit = 10
	}

	// Return featured skills for empty query
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
		fmt.Printf("ClawHub search failed: %v\n", err)
		return []HubSkill{}, fmt.Errorf("clawhub search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("ClawHub API returned %d\n", resp.StatusCode)
		return []HubSkill{}, fmt.Errorf("clawhub API returned %d", resp.StatusCode)
	}

	// ClawHub search API returns: {"results": [{"score", "slug", "displayName", "summary", "version", "updatedAt", "ownerHandle", "owner": {...}}]}
	var result struct {
		Results []struct {
			Score       float64 `json:"score"`
			Slug        string  `json:"slug"`
			DisplayName string  `json:"displayName"`
			Summary     string  `json:"summary"`
			Version     *string `json:"version"`
			UpdatedAt   int64   `json:"updatedAt"`
			OwnerHandle string  `json:"ownerHandle"`
			Owner       struct {
				Handle      string `json:"handle"`
				DisplayName string `json:"displayName"`
				Image       string `json:"image"`
			} `json:"owner"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return r.getFeaturedSkills(), nil
	}

	// If no results found, return featured skills as fallback
	if len(result.Results) == 0 {
		return r.getFeaturedSkills(), nil
	}

	// Build featured skills map for quick lookup of stars/installs
	featuredMap := make(map[string]HubSkill)
	for _, f := range r.getFeaturedSkills() {
		featuredMap[f.SourceID] = f
	}

	var skills []HubSkill
	for _, s := range result.Results {
		// Use search result data directly (avoid N+1 API calls)
		stars := 0
		installs := 0
		var tags []string
		if f, ok := featuredMap[s.Slug]; ok {
			stars = f.Stars
			installs = f.Installs
			tags = f.Tags
		}
		skills = append(skills, HubSkill{
			Name:        s.DisplayName,
			Description: s.Summary,
			Tags:        tags,
			Source:      HubSourceHub,
			SourceID:    s.Slug,
			URL:         fmt.Sprintf("%s/skills/%s", r.baseURL, s.Slug),
			Verified:    true,
			Stars:       stars,
			Installs:    installs,
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

	// ClawHub API returns nested structure:
	// {"skill": {"slug", "displayName", "summary", "tags", "stats": {"stars", "downloads", ...}}, "latestVersion": {...}, "owner": {...}}
	var result struct {
		Skill struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"displayName"`
			Summary     string `json:"summary"`
			Tags        struct {
				Latest string `json:"latest"`
			} `json:"tags"`
			Stats struct {
				Stars           int `json:"stars"`
				Downloads       int `json:"downloads"`
				InstallsAllTime int `json:"installsAllTime"`
			} `json:"stats"`
			Verified bool `json:"verified"`
		} `json:"skill"`
		LatestVersion struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	s := result.Skill
	return &HubSkill{
		Name:        s.DisplayName,
		Description: s.Summary,
		Tags:        []string{s.Tags.Latest},
		Source:      HubSourceHub,
		SourceID:    s.Slug,
		URL:         fmt.Sprintf("%s/skills/%s", r.baseURL, s.Slug),
		Stars:       s.Stats.Stars,
		Installs:    s.Stats.InstallsAllTime,
		Verified:    s.Verified,
	}, nil
}

// DownloadAndInstall downloads and installs a skill from ClawHub
func (r *ClawHubRegistry) DownloadAndInstall(ctx context.Context, slug, version, targetDir string) error {
	// ClawHub download API: /api/v1/download?slug={slug}&version={version}
	apiURL := fmt.Sprintf("%s/api/v1/download?slug=%s", r.baseURL, url.QueryEscape(slug))
	if version != "" {
		apiURL += "&version=" + url.QueryEscape(version)
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
		// Fallback: create a local skill file for featured skills
		return r.createLocalSkillFallback(slug, targetDir)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fallback: create a local skill file for featured skills
		return r.createLocalSkillFallback(slug, targetDir)
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "clawhub-skill-*.zip")
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
		// Fallback: create a local skill file for featured skills
		return r.createLocalSkillFallback(slug, targetDir)
	}

	// Flatten zip structure if there's a single top-level directory
	if err := flattenZipStructure(targetDir); err != nil {
		// Non-fatal: continue with original structure
	}

	return nil
}

// flattenZipStructure moves files from a single top-level subdirectory up to targetDir
func flattenZipStructure(targetDir string) error {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return err
	}

	// Find single top-level directory
	var subDir string
	for _, e := range entries {
		if e.IsDir() {
			if subDir != "" {
				// More than one directory, don't flatten
				return nil
			}
			subDir = e.Name()
		} else {
			// Files exist at top level, don't flatten
			return nil
		}
	}

	if subDir == "" {
		return nil
	}

	subDirPath := filepath.Join(targetDir, subDir)
	subEntries, err := os.ReadDir(subDirPath)
	if err != nil {
		return err
	}

	// Move all files from subDir to targetDir
	for _, e := range subEntries {
		src := filepath.Join(subDirPath, e.Name())
		dst := filepath.Join(targetDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			// If rename fails (cross-device), copy instead
			if e.IsDir() {
				if err := copyDirectory(src, dst); err != nil {
					return err
				}
				os.RemoveAll(src)
			} else {
				data, err := os.ReadFile(src)
				if err != nil {
					return err
				}
				if err := os.WriteFile(dst, data, 0644); err != nil {
					return err
				}
				os.Remove(src)
			}
		}
	}

	// Remove empty subDir
	os.Remove(subDirPath)
	return nil
}

// createLocalSkillFallback creates a local skill file when remote download fails
func (r *ClawHubRegistry) createLocalSkillFallback(slug, targetDir string) error {
	// Get skill info from featured skills
	featuredSkills := r.getFeaturedSkills()
	var skill *HubSkill
	for i := range featuredSkills {
		if featuredSkills[i].SourceID == slug {
			skill = &featuredSkills[i]
			break
		}
	}

	if skill == nil {
		return fmt.Errorf("skill %s not found in featured skills", slug)
	}

	// Create skill directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	// Create SKILL.md file with basic content
	// Format tags as YAML list
	tagsYAML := ""
	if len(skill.Tags) > 0 {
		for _, tag := range skill.Tags {
			tagsYAML += fmt.Sprintf("  - %s\n", tag)
		}
	}
	skillContent := fmt.Sprintf(`---
name: %s
description: "%s"
tags:
%s---

# %s

%s
`, skill.Name, skill.Description, tagsYAML, skill.Name, skill.Description)

	skillFile := filepath.Join(targetDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(skillContent), 0644); err != nil {
		return err
	}

	fmt.Printf("Created local skill fallback for %s\n", slug)
	return nil
}

// getFeaturedSkills returns curated featured skills from ClawHub
// SourceID must be the ClawHub slug (not owner/repo format)
func (r *ClawHubRegistry) getFeaturedSkills() []HubSkill {
	return []HubSkill{
		{Name: "Self-Improving Agent", Description: "Captures learnings, errors, and corrections to enable continuous improvement", Tags: []string{"agent"}, Source: HubSourceHub, SourceID: "self-improving-agent", URL: "https://clawhub.ai/skills/self-improving-agent", Verified: true, Stars: 3700, Installs: 457000},
		{Name: "Self-Improving + Proactive", Description: "Self-reflection + Self-criticism + Self-learning + Self-organizing memory", Tags: []string{"agent"}, Source: HubSourceHub, SourceID: "self-improving", URL: "https://clawhub.ai/skills/self-improving", Verified: true, Stars: 1200, Installs: 197000},
		{Name: "Skill Vetter", Description: "Security-first skill vetting for AI agents", Tags: []string{"security"}, Source: HubSourceHub, SourceID: "skill-vetter", URL: "https://clawhub.ai/skills/skill-vetter", Verified: true, Stars: 1200, Installs: 255000},
		{Name: "Gog", Description: "Google Workspace CLI for Gmail, Calendar, Drive, Contacts, Sheets, and Docs", Tags: []string{"productivity"}, Source: HubSourceHub, SourceID: "gog", URL: "https://clawhub.ai/skills/gog", Verified: true, Stars: 910, Installs: 184000},
		{Name: "Proactive Agent", Description: "Transform AI agents from task-followers into proactive partners", Tags: []string{"agent"}, Source: HubSourceHub, SourceID: "proactive-agent", URL: "https://clawhub.ai/skills/proactive-agent", Verified: true, Stars: 789, Installs: 167000},
		{Name: "Multi Search Engine", Description: "Multi search engine integration with 16 engines (7 CN + 9 Global)", Tags: []string{"search"}, Source: HubSourceHub, SourceID: "multi-search-engine", URL: "https://clawhub.ai/skills/multi-search-engine", Verified: true, Stars: 711, Installs: 151000},
		{Name: "Humanizer", Description: "Remove signs of AI-generated writing from text", Tags: []string{"writing"}, Source: HubSourceHub, SourceID: "humanizer", URL: "https://clawhub.ai/skills/humanizer", Verified: true, Stars: 648, Installs: 119000},
		{Name: "Ontology", Description: "Typed knowledge graph for structured agent memory and composable skills", Tags: []string{"memory"}, Source: HubSourceHub, SourceID: "ontology", URL: "https://clawhub.ai/skills/ontology", Verified: true, Stars: 626, Installs: 188000},
	}
}
