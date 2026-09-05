package agent

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// messageWeight makes base64 media payloads visible to the history budget.
// Plain len(Content) accounting is blind to them, so a few screenshots could
// blow past the context limit without truncateHistory ever triggering.
func TestMessageWeightCountsMediaPayloads(t *testing.T) {
	payload := strings.Repeat("A", 1200) // 1200 base64 chars

	withImage := provider.Message{
		Role:    "user",
		Content: "hi",
		ContentParts: []types.ContentPart{
			{
				Type:     "image_url",
				ImageURL: &types.MediaURL{URL: "data:image/png;base64," + payload},
			},
		},
	}
	if got := messageWeight(withImage); got < 1200 {
		t.Fatalf("base64 payload should dominate the weight, got %d", got)
	}

	withFile := provider.Message{
		Role: "user",
		ContentParts: []types.ContentPart{
			{
				Type: "file",
				File: &types.FileInfo{Contents: "data:text/plain;base64," + payload},
			},
		},
	}
	if got := messageWeight(withFile); got < 1200 {
		t.Fatalf("file payload should count toward the weight, got %d", got)
	}

	// Reference URLs (not data URLs) count their literal length only.
	ref := provider.Message{
		Role: "user",
		ContentParts: []types.ContentPart{
			{
				Type:     "image_url",
				ImageURL: &types.MediaURL{URL: "/api/uploads/sess1/img.png"},
			},
		},
	}
	if got := messageWeight(ref); got != len("/api/uploads/sess1/img.png") {
		t.Fatalf("reference URL should count literal length, got %d", got)
	}
}
