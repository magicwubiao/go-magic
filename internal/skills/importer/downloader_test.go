package importer

import (
	"testing"
)

func TestDetectURLType(t *testing.T) {
	downloader := &Downloader{}

	tests := []struct {
		url      string
		expected URLType
	}{
		// GitHub tree view
		{
			url:      "https://github.com/user/repo/tree/main/skills/my-skill",
			expected: URLTypeGitHubRepo,
		},
		{
			url:      "https://github.com/user/repo/tree/main/skills",
			expected: URLTypeGitHubRepo,
		},
		// GitHub raw content
		{
			url:      "https://raw.githubusercontent.com/user/repo/main/skills/my-skill/SKILL.md",
			expected: URLTypeGitHubRaw,
		},
		// GitHub Gist
		{
			url:      "https://gist.github.com/user/abc123",
			expected: URLTypeGist,
		},
		{
			url:      "https://gist.github.com/user/abc123456789",
			expected: URLTypeGist,
		},
		// HTTP ZIP file
		{
			url:      "https://example.com/skills/my-skill.zip",
			expected: URLTypeHTTPZip,
		},
		{
			url:      "https://example.com/skills/my-skill.ZIP",
			expected: URLTypeHTTPZip,
		},
		// GitHub archive
		{
			url:      "https://github.com/user/repo/archive/main.zip",
			expected: URLTypeGitHubZip,
		},
		// GitHub archive with tags
		{
			url:      "https://github.com/user/repo/archive/v1.0.zip",
			expected: URLTypeGitHubZip,
		},
		// Regular HTTP file
		{
			url:      "https://example.com/skills/my-skill.tar.gz",
			expected: URLTypeHTTPFile,
		},
		{
			url:      "https://example.com/skills/my-skill.tar",
			expected: URLTypeHTTPFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := downloader.DetectURLType(tt.url)
			if result != tt.expected {
				t.Errorf("DetectURLType(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

func TestIsURL(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"https://github.com/user/repo", true},
		{"http://example.com", true},
		{"./local/path", false},
		{"/absolute/path", false},
		{"path/to/skill", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsURL(tt.path)
			if result != tt.expected {
				t.Errorf("IsURL(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestURLTypeString(t *testing.T) {
	tests := []struct {
		urlType  URLType
		expected string
	}{
		{URLTypeUnknown, "Unknown"},
		{URLTypeGitHubRepo, "GitHub Repository"},
		{URLTypeGitHubRaw, "GitHub Raw"},
		{URLTypeGist, "GitHub Gist"},
		{URLTypeHTTPFile, "HTTP File"},
		{URLTypeHTTPZip, "HTTP ZIP Archive"},
		{URLTypeGitHubZip, "GitHub Archive"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.urlType.String()
			if result != tt.expected {
				t.Errorf("URLType(%d).String() = %v, want %v", tt.urlType, result, tt.expected)
			}
		})
	}
}

func TestURLTypeNames(t *testing.T) {
	names := URLTypeNames()
	expected := []string{
		"githubrepo",
		"githubraw",
		"gist",
		"httpfile",
		"httpzip",
		"githubzip",
	}

	if len(names) != len(expected) {
		t.Errorf("URLTypeNames() returned %d items, want %d", len(names), len(expected))
	}

	for i, name := range names {
		if name != expected[i] {
			t.Errorf("URLTypeNames()[%d] = %v, want %v", i, name, expected[i])
		}
	}
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		url           string
		expectedOwner string
		expectedRepo  string
		expectedBranch string
		expectedPath  string
		expectError   bool
	}{
		{
			url:           "https://github.com/user/repo/tree/main/skills/my-skill",
			expectedOwner: "user",
			expectedRepo:  "repo",
			expectedBranch: "main",
			expectedPath:  "skills/my-skill",
			expectError:   false,
		},
		{
			url:           "https://github.com/owner/project/tree/develop/path/to/skill",
			expectedOwner: "owner",
			expectedRepo:  "project",
			expectedBranch: "develop",
			expectedPath:  "path/to/skill",
			expectError:   false,
		},
		{
			url:         "invalid-url",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo, branch, path, err := parseGitHubURL(tt.url)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("parseGitHubURL(%q) expected error, got nil", tt.url)
				}
				return
			}

			if err != nil {
				t.Errorf("parseGitHubURL(%q) returned unexpected error: %v", tt.url, err)
				return
			}

			if owner != tt.expectedOwner {
				t.Errorf("parseGitHubURL(%q) owner = %v, want %v", tt.url, owner, tt.expectedOwner)
			}
			if repo != tt.expectedRepo {
				t.Errorf("parseGitHubURL(%q) repo = %v, want %v", tt.url, repo, tt.expectedRepo)
			}
			if branch != tt.expectedBranch {
				t.Errorf("parseGitHubURL(%q) branch = %v, want %v", tt.url, branch, tt.expectedBranch)
			}
			if path != tt.expectedPath {
				t.Errorf("parseGitHubURL(%q) path = %v, want %v", tt.url, path, tt.expectedPath)
			}
		})
	}
}

func TestParseURLTypeFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected URLType
	}{
		{"githubrepo", URLTypeGitHubRepo},
		{"github-repo", URLTypeGitHubRepo},
		{"GitHubRepo", URLTypeGitHubRepo},
		{"githubraw", URLTypeGitHubRaw},
		{"github-raw", URLTypeGitHubRaw},
		{"gist", URLTypeGist},
		{"Gist", URLTypeGist},
		{"httpfile", URLTypeHTTPFile},
		{"http-file", URLTypeHTTPFile},
		{"httpzip", URLTypeHTTPZip},
		{"http-zip", URLTypeHTTPZip},
		{"githubzip", URLTypeGitHubZip},
		{"github-zip", URLTypeGitHubZip},
		{"unknown", URLTypeUnknown},
		{"invalid", URLTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseURLTypeFromString(tt.input)
			if result != tt.expected {
				t.Errorf("ParseURLTypeFromString(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
