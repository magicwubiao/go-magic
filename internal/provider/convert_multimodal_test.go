package provider

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// Regression: the non-vision fallback used to inline the full image URL.
// Chat URLs are data URLs (multi-MB base64 payloads), so the fallback must
// emit a short placeholder instead — otherwise token budgets explode.
func TestConvertContentPartNonVisionPlaceholder(t *testing.T) {
	bigDataURL := "data:image/png;base64," + strings.Repeat("QUJD", 10000)
	part := types.ContentPart{
		Type:     "image_url",
		ImageURL: &types.MediaURL{URL: bigDataURL},
	}
	out := convertContentPart(part, &ConvertConfig{SupportVision: false})
	if out == nil {
		t.Fatal("expected a text part, got nil")
	}
	text, _ := out["text"].(string)
	if strings.Contains(text, "QUJD") || strings.Contains(text, "data:") {
		t.Fatalf("fallback leaked base64 payload into text: %q", text)
	}
	if len(text) > 200 {
		t.Fatalf("placeholder too long: %d chars", len(text))
	}
}

// detail:"auto" must not be emitted unless the caller explicitly set one —
// stricter OpenAI-compatible gateways reject unknown/constant fields.
func TestConvertContentPartDetailOmittedUnlessSet(t *testing.T) {
	part := types.ContentPart{
		Type:     "image_url",
		ImageURL: &types.MediaURL{URL: "data:image/png;base64,AAAA"},
	}
	cfg := &ConvertConfig{SupportVision: true}

	out := convertContentPart(part, cfg)
	iu, _ := out["image_url"].(map[string]interface{})
	if _, has := iu["detail"]; has {
		t.Fatal("detail should be omitted when not explicitly set")
	}

	part.ImageURL.Detail = "high"
	out = convertContentPart(part, cfg)
	iu, _ = out["image_url"].(map[string]interface{})
	if got, _ := iu["detail"].(string); got != "high" {
		t.Fatalf("explicit detail lost: got %q", got)
	}
}

// Anthropic native endpoint requires image/source blocks, not OpenAI-style
// image_url parts. The old code forwarded image_url verbatim → guaranteed 400.
func TestToAnthropicContentParts(t *testing.T) {
	parts := []map[string]interface{}{
		{"type": "text", "text": "hello"},
		{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": "data:image/png;base64,AAAA",
			},
		},
		{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": "https://example.com/cat.png",
			},
		},
		{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": "data:image/png;base64,!!!not-base64!!!",
			},
		},
	}

	out := toAnthropicContentParts(parts)
	if len(out) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(out))
	}

	if out[0]["type"] != "text" {
		t.Fatalf("text part should pass through, got %v", out[0]["type"])
	}

	img := out[1]
	if img["type"] != "image" {
		t.Fatalf("image_url should become image block, got %v", img["type"])
	}
	src, _ := img["source"].(map[string]interface{})
	if src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] != "AAAA" {
		t.Fatalf("bad source block: %v", src)
	}

	if out[2]["type"] != "text" {
		t.Fatalf("http image should degrade to text, got %v", out[2]["type"])
	}
	if out[3]["type"] != "text" {
		t.Fatalf("invalid base64 should degrade to text, got %v", out[3]["type"])
	}
}

// Gemini inlineData must carry the camelCase "mimeType" JSON key (REST API
// contract) and pass the base64 payload through without re-encoding.
func TestConvertToGeminiPartInlineData(t *testing.T) {
	part := map[string]interface{}{
		"type": "image_url",
		"image_url": map[string]interface{}{
			"url": "data:image/webp;base64,QUJD",
		},
	}
	gp := convertToGeminiPart(part)
	if gp.InlineData == nil {
		t.Fatal("expected inlineData part")
	}
	if gp.InlineData.MimeType != "image/webp" {
		t.Fatalf("mime mismatch: %q", gp.InlineData.MimeType)
	}
	if gp.InlineData.Data != "QUJD" {
		t.Fatalf("payload should pass through unchanged, got %q", gp.InlineData.Data)
	}

	// http URL images must not be silently dropped.
	httpPart := map[string]interface{}{
		"type": "image_url",
		"image_url": map[string]interface{}{
			"url": "https://example.com/cat.png",
		},
	}
	gp = convertToGeminiPart(httpPart)
	if gp.Text == "" {
		t.Fatal("http image should produce a text placeholder, not an empty part")
	}
}

// AutoVision: request-scoped recompute must not mutate the shared config.
func TestWithAutoVision(t *testing.T) {
	cfg := &ConvertConfig{SupportVision: false, AutoVision: true}

	got := cfg.WithAutoVision("gpt-4o")
	if !got.SupportVision {
		t.Fatal("gpt-4o should be detected as vision-capable")
	}
	if cfg.SupportVision {
		t.Fatal("shared config must not be mutated")
	}

	static := &ConvertConfig{SupportVision: true, AutoVision: false}
	if static.WithAutoVision("deepseek-chat") != static {
		t.Fatal("AutoVision off must return the config unchanged")
	}
}

// Explicit per-provider "vision" declaration must beat name-based detection.
// Regression: glm-4.1v-thinking-flashx (a vision model) matched no pattern,
// so images were silently downgraded to text placeholders.
func TestModelSupportsVisionPatterns(t *testing.T) {
	visionModels := []string{
		"glm-4.1v-thinking-flashx",
		"glm-4.5v", "glm-4v-plus", "glm-4.6v",
		"qwen-vl-max", "qwen3-vl-plus",
		"claude-sonnet-4", "gpt-4o-mini", "gemini-2.5-flash",
	}
	for _, m := range visionModels {
		if !ModelSupportsVision(m) {
			t.Errorf("ModelSupportsVision(%q) = false, want true", m)
		}
	}
	textModels := []string{
		"glm-5.3-flash", "deepseek-v4-pro", "qwen3.8-flash",
		"kimi-k2", "deepseek-chat",
	}
	for _, m := range textModels {
		if ModelSupportsVision(m) {
			t.Errorf("ModelSupportsVision(%q) = true, want false", m)
		}
	}
}

// VisionOverride (user's explicit config) wins over both the detected value
// and the current model name.
func TestWithAutoVisionOverride(t *testing.T) {
	yes := true
	no := false

	// Provider declared vision-capable → vision stays on even for a model
	// the detector would miss (or a text model the user insists supports it).
	cfg := &ConvertConfig{SupportVision: false, AutoVision: true, VisionOverride: &yes}
	if got := cfg.WithAutoVision("glm-5.3-flash"); !got.SupportVision {
		t.Fatal("VisionOverride=true must force SupportVision on")
	}
	if cfg.SupportVision {
		t.Fatal("shared config must not be mutated")
	}

	// Provider declared non-vision → detection must not turn it on.
	cfg = &ConvertConfig{SupportVision: true, AutoVision: true, VisionOverride: &no}
	if got := cfg.WithAutoVision("gpt-4o"); got.SupportVision {
		t.Fatal("VisionOverride=false must force SupportVision off")
	}

	// nil override → fall back to detection.
	cfg = &ConvertConfig{SupportVision: false, AutoVision: true}
	if got := cfg.WithAutoVision("gpt-4o"); !got.SupportVision {
		t.Fatal("nil override should use name detection")
	}
}
