package config

import (
	"strings"
	"testing"
)

// TestListProvidersCatalogIntegrity guards the built-in provider catalog that
// the Web UI add-provider form consumes (GET /api/model/options). The form
// seeds base_url and the recommended model list from this catalog, so every
// entry must be complete and free of retired model IDs.
func TestListProvidersCatalogIntegrity(t *testing.T) {
	providers := ListProviders()
	if len(providers) == 0 {
		t.Fatal("ListProviders returned no providers")
	}

	seen := make(map[string]bool)
	for _, p := range providers {
		if p.Name == "" {
			t.Fatal("provider with empty Name")
		}
		if seen[p.Name] {
			t.Errorf("duplicate provider name %q", p.Name)
		}
		seen[p.Name] = true

		if p.DisplayName == "" {
			t.Errorf("provider %s: empty DisplayName", p.Name)
		}
		if len(p.Models) == 0 {
			t.Errorf("provider %s: empty model list", p.Name)
		}
		for _, m := range p.Models {
			if strings.TrimSpace(m) == "" {
				t.Errorf("provider %s: blank model ID in list %v", p.Name, p.Models)
			}
		}

		// custom providers have no fixed endpoint; every built-in one must
		// carry its official base URL (mirrors the constructor fallback).
		if p.Name != "custom" {
			if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
				t.Errorf("provider %s: BaseURL %q must be an absolute http(s) URL", p.Name, p.BaseURL)
			}
			if strings.HasSuffix(p.BaseURL, "/") && p.Name != "deepseek" {
				// Constructors normalize without trailing slashes except the
				// bare-domain deepseek endpoint; flag accidental trailing "/".
				t.Errorf("provider %s: BaseURL %q should not end with '/'", p.Name, p.BaseURL)
			}
		}

		if p.NeedsBaseURL && p.Name != "custom" && p.BaseURL == "" {
			t.Errorf("provider %s: NeedsBaseURL=true but BaseURL empty (form would have nothing to seed)", p.Name)
		}
	}

	// Providers the CLI wizard and docs reference must stay present.
	for _, required := range []string{
		"openai", "anthropic", "deepseek", "zhipu", "dashscope", "moonshot",
		"longcat", "hunyuan", "huoshan", "wenxin", "ollama", "custom",
	} {
		if !seen[required] {
			t.Errorf("required provider %q missing from catalog", required)
		}
	}
}

// TestListProvidersNoRetiredModels ensures retired/decommissioned model IDs
// never sneak back into the catalog (they hard-fail server-side).
func TestListProvidersNoRetiredModels(t *testing.T) {
	retired := map[string]string{
		"deepseek-chat":          "retired 2026-07-24, replaced by deepseek-v4-*",
		"deepseek-reasoner":      "retired 2026-07-24, replaced by deepseek-v4-*",
		"kimi-k2-0905-preview":   "retired 2026-05-25, replaced by kimi-k*",
		"kimi-k2-instruct":       "retired 2026-05-25, replaced by kimi-k*",
		"LongCat-Flash-Chat":     "Flash series decommissioned 2026-05-29, replaced by LongCat-2.0-Preview",
		"LongCat-Flash-Thinking": "Flash series decommissioned 2026-05-29",
		"LongCat-Flash-Omni":     "Flash series decommissioned 2026-05-29",
		"abab6-chat":             "ancient MiniMax ID, replaced by MiniMax-M*",
	}

	for _, p := range ListProviders() {
		for _, m := range p.Models {
			if reason, bad := retired[m]; bad {
				t.Errorf("provider %s: retired model %q in catalog (%s)", p.Name, m, reason)
			}
		}
	}
}
