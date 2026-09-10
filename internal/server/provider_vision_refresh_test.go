package server

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Regression: flipping "图片输入（视觉）" in the model settings page only wrote
// config.json. The conversion/vision policy lives on the provider instance
// (BaseProvider.ConvertCfg) and used to be installed exclusively when a NEW
// agent was built — while the web UI keeps reusing the cached per-session
// agent. The toggle therefore stayed invisible to the running turn and only
// took effect after a restart. Saving provider settings must refresh the LIVE
// provider instead.
func TestProviderVisionToggleAppliesWithoutRestart(t *testing.T) {
	// Model name that name-based detection classifies as text-only (curated
	// registry, text-only by omission) — the exact situation where the user
	// has to declare vision manually.
	const model = "deepseek-v4-flash"

	s := &Server{cfg: &appconfig.Config{
		Provider: "custom",
		Model:    model,
		Providers: map[string]appconfig.ProviderConfig{
			"custom": {APIKey: "sk-live", BaseURL: "https://example.invalid/v1", Models: []string{model}},
		},
	}}
	s.provider = createProvider(s.cfg)
	if s.provider == nil {
		t.Fatal("provider not built from config")
	}
	// What the server does when it builds the first agent for a session.
	s.refreshConvertConfig()

	cfg := provider.GetConvertConfig(s.provider)
	if cfg == nil {
		t.Fatal("conversion config never installed on the live provider")
	}
	if cfg.VisionOverride != nil {
		t.Fatalf("no declaration expected yet, got %v", *cfg.VisionOverride)
	}
	if imagePartKept(cfg, model) {
		t.Fatal("precondition failed: images should be downgraded before the toggle")
	}

	// User picks "支持视觉" and saves → PUT /api/providers/custom
	putProvider(t, s, `{"vision": true}`)

	cfg = provider.GetConvertConfig(s.provider)
	if cfg == nil || cfg.VisionOverride == nil || !*cfg.VisionOverride {
		t.Fatalf("vision=true not applied to the live provider (restart would be required): %+v", cfg)
	}
	if !imagePartKept(cfg, model) {
		t.Fatal("images still replaced by placeholders right after the toggle")
	}

	// "自动检测" clears the declaration → back to name-based detection.
	putProvider(t, s, `{"vision": null}`)
	cfg = provider.GetConvertConfig(s.provider)
	if cfg == nil || cfg.VisionOverride != nil {
		t.Fatalf("clearing the declaration must drop the override, got %+v", cfg)
	}
	if imagePartKept(cfg, model) {
		t.Fatal("auto detection should downgrade images again for this model name")
	}

	// "不支持视觉" forces it off even for a vision-looking model name.
	putProvider(t, s, `{"vision": false}`)
	cfg = provider.GetConvertConfig(s.provider)
	if cfg == nil || cfg.VisionOverride == nil || *cfg.VisionOverride {
		t.Fatalf("vision=false not applied to the live provider: %+v", cfg)
	}
}

// imagePartKept mirrors the per-request decision made by
// prepMessagesWithConfig: install AutoVision on a copy, convert the message and
// check whether the image survived as an image_url part.
func imagePartKept(cfg *provider.ConvertConfig, model string) bool {
	msgs := []types.Message{{
		Role: "user",
		ContentParts: []types.ContentPart{
			{Type: "text", Text: "what is this?"},
			{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,AAAA"}},
		},
	}}
	for _, m := range provider.ConvertMessagesWithModel(msgs, cfg, model) {
		parts, ok := m["content"].([]map[string]interface{})
		if !ok {
			continue
		}
		for _, p := range parts {
			if p["type"] == "image_url" {
				return true
			}
		}
	}
	return false
}
