package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
)

func TestQueuedTurnErrorNoticeIsVisible(t *testing.T) {
	for _, response := range []string{"", "partial answer", "<think>reasoning", "<think>reasoning</think>"} {
		t.Run(response, func(t *testing.T) {
			result := response + queuedTurnErrorNotice(response, errors.New("provider disconnected"))
			visible := provider.StripThinkTrails(result)
			if !strings.Contains(visible, "provider disconnected") || !strings.Contains(visible, "This turn was interrupted") {
				t.Fatalf("error hidden or missing: %q", visible)
			}
			if !strings.HasPrefix(result, response) {
				t.Fatal("partial response lost")
			}
		})
	}
}
