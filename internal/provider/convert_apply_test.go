package provider

import (
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// ApplyConvertConfig must reach the storage of every provider shape: the
// OpenAI-compatible family (deepseek/zhipu/custom on the server), the DeepSeek
// wrapper that embeds it, DashScope's standalone *BaseProvider, and the
// interface fallback for the rest. The server relies on this to push the
// vision toggle onto the LIVE provider without rebuilding it.
func TestApplyConvertConfig(t *testing.T) {
	cfg := &ConvertConfig{SupportVision: false, AutoVision: true, StrategyName: "auto"}

	oc := NewOpenAICompatibleProvider("custom", "sk-x", "https://example.invalid/v1", "m", nil)
	if !ApplyConvertConfig(oc, cfg) {
		t.Fatal("ApplyConvertConfig(openai-compatible) = false")
	}
	if GetConvertConfig(oc) != cfg {
		t.Fatal("openai-compatible provider did not keep the config")
	}

	ds := NewDeepSeekProvider("sk-x", "https://example.invalid", "deepseek-v4-flash", nil)
	if !ApplyConvertConfig(ds, cfg) {
		t.Fatal("ApplyConvertConfig(deepseek) = false")
	}
	if GetConvertConfig(ds) != cfg {
		t.Fatal("deepseek provider did not keep the config")
	}

	dash := NewDashScopeProvider("sk-x", "https://example.invalid/v1", "qwen3.7-plus")
	if !ApplyConvertConfig(dash, cfg) {
		t.Fatal("ApplyConvertConfig(dashscope) = false")
	}
	if GetConvertConfig(dash) != cfg {
		t.Fatal("dashscope provider did not keep the config")
	}

	// Nil-safety: a missing provider must not panic.
	if ApplyConvertConfig(nil, cfg) {
		t.Fatal("ApplyConvertConfig(nil) = true")
	}
	if GetConvertConfig(nil) != nil {
		t.Fatal("GetConvertConfig(nil) should be nil")
	}
}

// The installed config is what the request path reads (prepMessagesWithConfig
// → WithAutoVision → ConvertMessagesWithConfig): an override of true keeps
// image parts, an override of false — or auto-detection on a text-only-looking
// name — must downgrade them to placeholders.
func TestAppliedConvertConfigDrivesImageParts(t *testing.T) {
	model := "deepseek-v4-flash" // curated registry entry: text-only → auto says "no vision"
	msgs := []types.Message{{
		Role: "user",
		ContentParts: []types.ContentPart{
			{Type: "text", Text: "what is this?"},
			{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,AAAA"}},
		},
	}}
	p := NewOpenAICompatibleProvider("custom", "sk-x", "https://example.invalid/v1", model, nil)

	yes, no := true, false
	for _, tc := range []struct {
		name       string
		override   *bool
		wantImages bool
	}{
		{"auto (text-looking name)", nil, false},
		{"override off", &no, false},
		{"override on", &yes, true},
	} {
		ApplyConvertConfig(p, &ConvertConfig{AutoVision: true, VisionOverride: tc.override})
		got := convertedHasImagePart(GetConvertConfig(p).WithAutoVision(p.GetModel()), msgs)
		if got != tc.wantImages {
			t.Errorf("%s: image part kept = %v, want %v", tc.name, got, tc.wantImages)
		}
	}
}

// convertedHasImagePart reports whether the OpenAI-style conversion of messages
// kept the image as an image_url part (true) or replaced it with a text
// placeholder (false) — mirrors what prepMessagesWithConfig produces.
func convertedHasImagePart(cfg *ConvertConfig, messages []types.Message) bool {
	for _, m := range ConvertMessagesWithConfig(messages, cfg) {
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
