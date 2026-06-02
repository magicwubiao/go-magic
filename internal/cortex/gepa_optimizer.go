package cortex

import (
	"context"
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// ============================================================================
// PromptOptimizer - Applies optimization strategies to prompts
// ============================================================================
// Applies the changes defined in optimization strategies to system prompts.
// Supports multiple prompt types: SOUL, SYSTEM, SKILLS.
// ============================================================================

// PromptOptimizer applies optimization strategies to prompts
type PromptOptimizer struct {
	provider provider.Provider
}

// NewPromptOptimizer creates a new prompt optimizer
func NewPromptOptimizer(prov provider.Provider) *PromptOptimizer {
	return &PromptOptimizer{
		provider: prov,
	}
}

// Optimize applies optimization changes to a prompt
func (o *PromptOptimizer) Optimize(prompt string, changes []PromptChange) (string, error) {
	optimized := prompt

	for _, change := range changes {
		switch change.Type {
		case "add":
			optimized = o.applyAdd(optimized, change)
		case "remove":
			optimized = o.applyRemove(optimized, change)
		case "modify":
			optimized = o.applyModify(optimized, change)
		case "reorder":
			optimized = o.applyReorder(optimized, change)
		}
	}

	return optimized, nil
}

// applyAdd adds content to a section
func (o *PromptOptimizer) applyAdd(prompt string, change PromptChange) string {
	// Find the section
	sectionHeader := "## " + change.Section
	if change.Section == "" {
		// Add to end of prompt
		return prompt + "\n\n" + change.NewContent
	}

	idx := strings.Index(prompt, sectionHeader)
	if idx == -1 {
		// Section not found, create it
		return prompt + "\n\n" + sectionHeader + "\n\n" + change.NewContent
	}

	// Find end of section (next ## or end of prompt)
	sectionStart := idx + len(sectionHeader)
	sectionEnd := len(prompt)

	nextSection := strings.Index(prompt[sectionStart:], "\n## ")
	if nextSection != -1 {
		sectionEnd = sectionStart + nextSection
	}

	// Insert new content at end of section
	return prompt[:sectionEnd] + "\n\n" + change.NewContent + prompt[sectionEnd:]
}

// applyRemove removes content from a section
func (o *PromptOptimizer) applyRemove(prompt string, change PromptChange) string {
	if change.OldContent == "" {
		return prompt
	}

	// Remove the old content
	return strings.Replace(prompt, change.OldContent, "", 1)
}

// applyModify modifies content in a section
func (o *PromptOptimizer) applyModify(prompt string, change PromptChange) string {
	if change.OldContent == "" {
		// If no old content specified, treat as add
		return o.applyAdd(prompt, change)
	}

	// Replace old content with new content
	return strings.Replace(prompt, change.OldContent, change.NewContent, 1)
}

// applyReorder reorders sections (simplified implementation)
func (o *PromptOptimizer) applyReorder(prompt string, change PromptChange) string {
	// For now, just return as-is
	// Full implementation would parse sections and reorder them
	return prompt
}

// OptimizeWithLLM uses LLM to optimize a prompt
func (o *PromptOptimizer) OptimizeWithLLM(
	ctx context.Context,
	prompt string,
	strategy *OptimizationStrategy,
) (string, error) {
	optimizePrompt := fmt.Sprintf(`Optimize the following system prompt based on the strategy.

Strategy: %s
Description: %s
Changes to apply:
%s

Current Prompt:
%s

Provide the optimized prompt in full.`,
		strategy.Name,
		strategy.Description,
		o.formatChanges(strategy.Changes),
		prompt,
	)

	type openAIlike interface {
		Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error)
	}

	var resp *provider.ChatResponse
	var err error

	if oa, ok := o.provider.(openAIlike); ok {
		resp, err = oa.Chat(ctx, []provider.Message{
			{Role: "user", Content: optimizePrompt},
		})
	} else {
		return "", fmt.Errorf("provider does not support chat")
	}

	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// ValidateChanges validates that changes can be applied to a prompt
func (o *PromptOptimizer) ValidateChanges(prompt string, changes []PromptChange) []error {
	var errors []error

	for _, change := range changes {
		switch change.Type {
		case "add":
			// Always valid
		case "remove":
			if change.OldContent != "" && !strings.Contains(prompt, change.OldContent) {
				errors = append(errors, fmt.Errorf("content to remove not found: %s", change.OldContent[:minInt(len(change.OldContent), 50)]))
			}
		case "modify":
			if change.OldContent != "" && !strings.Contains(prompt, change.OldContent) {
				errors = append(errors, fmt.Errorf("content to modify not found: %s", change.OldContent[:minInt(len(change.OldContent), 50)]))
			}
		case "reorder":
			// Check if section exists
			if change.Section != "" && !strings.Contains(prompt, "## "+change.Section) {
				errors = append(errors, fmt.Errorf("section not found: %s", change.Section))
			}
		default:
			errors = append(errors, fmt.Errorf("unknown change type: %s", change.Type))
		}
	}

	return errors
}

// PreviewChanges shows what changes would look like without applying
func (o *PromptOptimizer) PreviewChanges(prompt string, changes []PromptChange) string {
	var preview []string

	preview = append(preview, "=== CHANGE PREVIEW ===")
	preview = append(preview, "")

	for i, change := range changes {
		preview = append(preview, fmt.Sprintf("Change %d:", i+1))
		preview = append(preview, fmt.Sprintf("  Type: %s", change.Type))
		preview = append(preview, fmt.Sprintf("  Section: %s", change.Section))
		preview = append(preview, fmt.Sprintf("  Reason: %s", change.Reason))

		switch change.Type {
		case "add":
			preview = append(preview, fmt.Sprintf("  Add: %s", truncateString(change.NewContent, 100)))
		case "remove":
			preview = append(preview, fmt.Sprintf("  Remove: %s", truncateString(change.OldContent, 100)))
		case "modify":
			preview = append(preview, fmt.Sprintf("  From: %s", truncateString(change.OldContent, 100)))
			preview = append(preview, fmt.Sprintf("  To: %s", truncateString(change.NewContent, 100)))
		}
		preview = append(preview, "")
	}

	return strings.Join(preview, "\n")
}

// RollbackChanges rolls back applied changes (requires original)
func (o *PromptOptimizer) RollbackChanges(optimizedPrompt string, changes []PromptChange, originalPrompt string) string {
	// For simplicity, just return the original
	// A more sophisticated implementation would reverse each change
	return originalPrompt
}

// formatChanges formats changes for display
func (o *PromptOptimizer) formatChanges(changes []PromptChange) string {
	var parts []string
	for _, c := range changes {
		parts = append(parts, fmt.Sprintf("- [%s] %s: %s", c.Type, c.Section, c.Reason))
	}
	return strings.Join(parts, "\n")
}

// Helper function
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
