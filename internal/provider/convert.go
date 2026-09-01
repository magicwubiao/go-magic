package provider

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// emptyAssistantPlaceholder is the provider-level placeholder for empty
// assistant content (used when a tool_calls assistant turn has no text).
//
// It must satisfy two conflicting constraints at once:
//  1. Survive strings.TrimSpace — zhipu/GLM throws error 1214
//     ("messages 参数非法") on assistant content that is empty, pure
//     whitespace, or nil, so a single space or "" is rejected.
//  2. Be invisible to the model — any READABLE placeholder becomes part of
//     the model's own visible history and is then echoed back verbatim as
//     output. This has bitten us repeatedly:
//     - "[no content]"        -> model wrapped every reply in brackets
//     - "..."                 -> model echoed the ellipsis
//     - "No response content" -> model repeated the sentence, flooding
//     non-zhipu providers' output with the phrase
//
// A single ZERO WIDTH SPACE (U+200B) is the only marker that satisfies both:
// it is NOT a Unicode White_Space codepoint (so TrimSpace leaves it intact),
// yet it renders as nothing and carries no natural-language signal for the
// model to copy. The downside — invisibility in logs — is accepted because
// every readable placeholder tried so far leaked into output.
const emptyAssistantPlaceholder = "\u200b"

// LegacyEmptyPlaceholder is the readable placeholder used in earlier releases.
// It leaked into model output (models echo their own visible history), so it
// is now stripped during normalization so already-contaminated histories
// self-heal. See IsEmptyAssistantContent and StripLegacyPlaceholder.
const LegacyEmptyPlaceholder = "No response content"

// IsEmptyAssistantContent reports whether content is effectively empty — i.e.
// carries no information for the model: pure whitespace, invisible zero-width
// markers only, or only the legacy readable placeholder phrase. Such content
// must not be (re)sent to a provider, and should be treated exactly like
// empty content by callers.
func IsEmptyAssistantContent(content string) bool {
	if strings.TrimSpace(content) == "" {
		return true
	}
	s := strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, content)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, strings.ToLower(LegacyEmptyPlaceholder), "")
	return strings.TrimSpace(s) == ""
}

// StripLegacyPlaceholder removes lines that consist solely of the legacy
// readable placeholder phrase (case-insensitive). The model echoed the phrase
// as standalone repeated lines, so line-scoped removal is safe and does not
// touch legitimate prose that merely contains the words.
func StripLegacyPlaceholder(content string) string {
	if content == "" || !strings.Contains(content, "response content") {
		return content
	}
	lines := strings.Split(content, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.EqualFold(strings.TrimSpace(ln), LegacyEmptyPlaceholder) {
			continue
		}
		cleaned = append(cleaned, ln)
	}
	return strings.Join(cleaned, "\n")
}

// DashScopeTurnLimit is the service-side hard cap on the total number of
// messages ("turns") per request for the DashScope compatible-mode API.
// The server rejects requests whose messages array exceeds 150 items with
// "exceeded maximum turns (150)". We leave a safety margin so messages
// produced during the current turn do not push us over the line.
const (
	DashScopeTurnLimit   = 150
	DashScopeTurnSafety  = 15
	DashScopeTurnMaxSend = DashScopeTurnLimit - DashScopeTurnSafety // 135
)

// TrimDashScopeTurns drops the *oldest* contiguous message blocks from the
// messages array so that the TOTAL message count stays within
// DashScopeTurnMaxSend. The first message is preserved when it has role
// "system" (the system prompt must stay anchored at the top).
//
// DashScope's compatible-mode API counts every message in the messages
// array (system, user, assistant, and tool) as one "turn" toward the
// 150-item hard cap. A long session with many short user↔assistant
// exchanges can accumulate 150+ messages even when individual tasks are
// short, because the agent's truncateHistory() trims by character length
// (200K) not message count.
//
// Trimming strategy:
//  1. Keep system message at index 0 if present.
//  2. If total messages > DashScopeTurnMaxSend, drop oldest
//     user→assistant→tool blocks from the front until under the limit.
//  3. Each dropped block includes the leading user message, the assistant
//     reply, and any trailing tool messages (so alternation stays valid).
func TrimDashScopeTurns(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	// Phase 1: enforce total message count limit.
	if len(msgs) > DashScopeTurnMaxSend {
		msgs = trimByTotalCount(msgs, DashScopeTurnMaxSend)
	}
	// Phase 2: the total-count trim above may have left consecutive
	// same-role messages; collapse them.
	msgs = collapseConsecutiveForTrim(msgs)
	return msgs
}

// trimByTotalCount drops the oldest message blocks (preserving a leading
// system message) until the slice length is <= limit. A "block" is one
// user (or system) message followed by an assistant and any trailing tool
// messages. This keeps message alternation valid after trimming.
func trimByTotalCount(msgs []Message, limit int) []Message {
	// Preserve leading system message.
	startIdx := 0
	if msgs[0].Role == "system" {
		startIdx = 1
	}
	rest := msgs[startIdx:]
	for len(msgs) > limit && len(rest) > 0 {
		// Find the end of the first block: user → assistant → tools...
		blockEnd := 1
		// If the first message is a user/system, skip it and then
		// consume the assistant + trailing tools.
		if rest[0].Role == "user" || rest[0].Role == "system" {
			// Consume the assistant that follows (if any).
			if blockEnd < len(rest) && rest[blockEnd].Role == "assistant" {
				blockEnd++
				// Consume trailing tool messages.
				for blockEnd < len(rest) && rest[blockEnd].Role == "tool" {
					blockEnd++
				}
			}
		} else if rest[0].Role == "assistant" {
			// No leading user; consume assistant + trailing tools.
			for blockEnd < len(rest) && rest[blockEnd].Role == "tool" {
				blockEnd++
			}
		} else if rest[0].Role == "tool" {
			// Orphaned tool message; just drop it.
			blockEnd = 1
		}
		rest = rest[blockEnd:]
		msgs = append(msgs[:startIdx], rest...)
	}
	return msgs
}

// collapseConsecutiveForTrim merges back-to-back same-role non-tool
// messages produced by TrimDashScopeTurns (which can place two user
// messages adjacent after dropping an intermediate assistant+tools block).
// Tool messages are never merged — they are tied to tool_call_id ordering.
func collapseConsecutiveForTrim(msgs []Message) []Message {
	if len(msgs) <= 1 {
		return msgs
	}
	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Role == "tool" {
			out = append(out, m)
			continue
		}
		if i+1 < len(msgs) && msgs[i+1].Role == m.Role && msgs[i+1].Role != "tool" {
			// Keep the newer (later) message — it carries the actual user
			// intent and the older one is usually context that has already
			// been digested into history memory.
			continue
		}
		out = append(out, m)
	}
	return out
}

// FileStrategy defines how files should be converted
type FileStrategy int

const (
	FileStrategyAuto   FileStrategy = iota // Auto-select based on file type
	FileStrategyURL                        // Prefer URL references (for large files)
	FileStrategyBase64                     // Always use base64
)

// LargeFileThreshold is the size (bytes) above which files should use URL instead of base64
const LargeFileThreshold = 1024 * 1024 // 1MB

// ConvertConfig holds conversion settings
type ConvertConfig struct {
	UploadURLPrefix string       // Public URL prefix for uploaded files
	Strategy        FileStrategy // File conversion strategy
	StrategyName    string       // String representation of strategy ("auto", "url", "base64")
	SupportVision   bool         // Whether the model supports vision (image_url format)
}

// DefaultConvertConfig returns default conversion config
func DefaultConvertConfig() *ConvertConfig {
	return &ConvertConfig{
		Strategy:     FileStrategyAuto,
		StrategyName: "auto",
	}
}

// ParseFileStrategy converts string to FileStrategy
func ParseFileStrategy(s string) FileStrategy {
	switch strings.ToLower(s) {
	case "url":
		return FileStrategyURL
	case "base64":
		return FileStrategyBase64
	default:
		return FileStrategyAuto
	}
}

// ModelSupportsVision checks if a model supports vision (image_url format)
func ModelSupportsVision(modelName string) bool {
	if modelName == "" {
		return false
	}
	modelLower := strings.ToLower(modelName)

	// Models that support vision
	visionModels := []string{
		"gpt-5.6", "gpt-5", "gpt-4o", "gpt-4-turbo", "gpt-4-vision",
		"claude-sonnet-5", "claude-opus-5", "claude-fable", "claude-haiku-4",
		"claude-3", "claude-3.5", "claude-3-opus", "claude-3-sonnet",
		"gemini-3", "gemini-2.5", "gemini-2.0", "gemini-1.5", "gemini-pro-vision",
		"qwen-vl", "qwen-vl-max", "qwen2-vl", "qwen3-vl",
	}

	for _, m := range visionModels {
		if strings.Contains(modelLower, m) {
			return true
		}
	}

	// Check for common vision model patterns
	visionPatterns := []string{"vision", "vl", "gpt-4o", "gpt-5", "claude-3", "claude-sonnet-5", "claude-opus-5", "claude-fable", "gemini-1.5", "gemini-2", "gemini-3"}
	for _, pattern := range visionPatterns {
		if strings.Contains(modelLower, pattern) {
			return true
		}
	}

	return false
}

// MIME type categories
var (
	imageMimeTypes = map[string]bool{
		"image/png":     true,
		"image/jpeg":    true,
		"image/gif":     true,
		"image/webp":    true,
		"image/svg+xml": true,
	}

	textMimeTypes = map[string]bool{
		"text/plain":             true,
		"text/html":              true,
		"text/css":               true,
		"text/csv":               true,
		"text/markdown":          true,
		"application/json":       true,
		"application/xml":        true,
		"application/javascript": true,
		"application/x-yaml":     true,
		"application/x-sh":       true,
	}

	codeMimeTypes = map[string]bool{
		"text/x-go":          true,
		"text/x-python":      true,
		"text/x-java":        true,
		"text/x-c":           true,
		"text/x-c++":         true,
		"text/x-csharp":      true,
		"text/x-php":         true,
		"text/x-ruby":        true,
		"text/x-sql":         true,
		"text/x-typescript":  true,
		"text/x-java-script": true,
	}

	documentMimeTypes = map[string]bool{
		"application/pdf":    true,
		"application/msword": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.ms-excel": true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
		"application/vnd.ms-powerpoint":                                             true,
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	}
)

// isImage checks if mime type is an image
func isImage(mimeType string) bool {
	return imageMimeTypes[mimeType]
}

// isText checks if mime type is text content
func isText(mimeType string) bool {
	return textMimeTypes[mimeType] || codeMimeTypes[mimeType]
}

// isDocument checks if mime type is a document that can be read
func isDocument(mimeType string) bool {
	return documentMimeTypes[mimeType] || isText(mimeType)
}

// extractMimeType extracts mime type from data URL
func extractMimeType(dataURL string) string {
	if strings.HasPrefix(dataURL, "data:") {
		parts := strings.SplitN(dataURL, ";", 2)
		if len(parts) >= 2 {
			return strings.TrimPrefix(parts[0], "data:")
		}
	}
	return ""
}

// fileContentType returns the content type based on filename extension
func fileContentType(filename string) string {
	ext := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(ext, ".png"):
		return "image/png"
	case strings.HasSuffix(ext, ".jpg") || strings.HasSuffix(ext, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(ext, ".gif"):
		return "image/gif"
	case strings.HasSuffix(ext, ".webp"):
		return "image/webp"
	case strings.HasSuffix(ext, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(ext, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(ext, ".txt"), strings.HasSuffix(ext, ".md"):
		return "text/plain"
	case strings.HasSuffix(ext, ".json"):
		return "application/json"
	case strings.HasSuffix(ext, ".html"), strings.HasSuffix(ext, ".htm"):
		return "text/html"
	case strings.HasSuffix(ext, ".css"):
		return "text/css"
	case strings.HasSuffix(ext, ".js"):
		return "application/javascript"
	case strings.HasSuffix(ext, ".ts"):
		return "text/typescript"
	case strings.HasSuffix(ext, ".py"):
		return "text/x-python"
	case strings.HasSuffix(ext, ".go"):
		return "text/x-go"
	case strings.HasSuffix(ext, ".java"):
		return "text/x-java"
	case strings.HasSuffix(ext, ".c"):
		return "text/x-c"
	case strings.HasSuffix(ext, ".cpp"), strings.HasSuffix(ext, ".cc"), strings.HasSuffix(ext, ".cxx"):
		return "text/x-c++"
	case strings.HasSuffix(ext, ".sh"), strings.HasSuffix(ext, ".bash"):
		return "application/x-sh"
	case strings.HasSuffix(ext, ".sql"):
		return "text/x-sql"
	case strings.HasSuffix(ext, ".xml"):
		return "application/xml"
	case strings.HasSuffix(ext, ".yaml"), strings.HasSuffix(ext, ".yml"):
		return "application/x-yaml"
	case strings.HasSuffix(ext, ".csv"):
		return "text/csv"
	case strings.HasSuffix(ext, ".doc"):
		return "application/msword"
	case strings.HasSuffix(ext, ".docx"):
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case strings.HasSuffix(ext, ".xls"), strings.HasSuffix(ext, ".xlsx"):
		return "application/vnd.ms-excel"
	case strings.HasSuffix(ext, ".ppt"), strings.HasSuffix(ext, ".pptx"):
		return "application/vnd.ms-powerpoint"
	default:
		return "application/octet-stream"
	}
}

// ConvertMessages converts internal Message type to OpenAI-compatible format
func ConvertMessages(messages []types.Message) []map[string]interface{} {
	return ConvertMessagesWithConfig(messages, nil)
}

// ConvertMessagesWithConfig converts messages with custom config
func ConvertMessagesWithConfig(messages []types.Message, config *ConvertConfig) []map[string]interface{} {
	if config == nil {
		config = DefaultConvertConfig()
	}

	result := make([]map[string]interface{}, 0, len(messages))

	for i, msg := range messages {
		openAIMsg := make(map[string]interface{})

		// Always set role
		openAIMsg["role"] = msg.Role

		// Handle content based on type
		if len(msg.ContentParts) > 0 {
			// Multi-modal content (text + images + files)
			parts := make([]map[string]interface{}, 0, len(msg.ContentParts))
			for _, part := range msg.ContentParts {
				convertedPart := convertContentPart(part, config)
				if convertedPart != nil {
					parts = append(parts, convertedPart)
				}
			}
			// Defensive: if all parts were dropped (e.g. unsupported type),
			// fall back to a text placeholder so content is never an empty array.
			if len(parts) == 0 {
				parts = append(parts, map[string]interface{}{
					"type": "text",
					"text": "(empty content)",
				})
			}
			openAIMsg["content"] = parts
		} else if len(msg.ToolCalls) > 0 && IsEmptyAssistantContent(msg.Content) {
			// Assistant message with tool_calls and no (effective) content:
			// use the invisible zero-width placeholder. A single space or ""
			// still triggers zhipu (GLM) error 1214 ("messages 参数非法")
			// because Zhipu's validator treats whitespace-only content
			// equivalently to truly empty. Deepseek, OpenAI and others
			// tolerate empty content here — this is exactly why "switch to
			// deepseek works, zhipu still fails" on an otherwise identical
			// payload. The placeholder is invisible precisely so the model
			// does not reproduce it (see emptyAssistantPlaceholder).
			openAIMsg["content"] = emptyAssistantPlaceholder
		} else if msg.Role == "assistant" && IsEmptyAssistantContent(msg.Content) && len(msg.ToolCalls) == 0 {
			// Defensive: empty assistant with no tool_calls reaching here means
			// buildLLMMessages did not filter it (e.g. a code path that
			// bypasses the agent layer). Emit the same natural-language
			// placeholder so strict providers (GLM/Zhipu 1214) don't reject
			// the payload.
			openAIMsg["content"] = emptyAssistantPlaceholder
		} else {
			// Fallback to plain text content
			openAIMsg["content"] = msg.Content
		}

		// Handle tool calls for assistant messages
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, 0, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				// Always use "function" as the type - some providers require this
				toolCallType := tc.Type
				if toolCallType == "" {
					toolCallType = "function"
				}

				// Ensure tool call ID is not empty
				tcID := tc.ID
				if tcID == "" {
					tcID = fmt.Sprintf("call_%d_%d", i, j)
				}

				toolCall := map[string]interface{}{
					"id":   tcID,
					"type": toolCallType,
					"function": map[string]interface{}{
						"name":      tc.GetToolName(),
						"arguments": normalizeToolArguments(tc.Function.Arguments),
					},
				}
				toolCalls = append(toolCalls, toolCall)
			}
			openAIMsg["tool_calls"] = toolCalls
		}

		// Handle tool_call_id for tool messages - ALWAYS include it for tool role
		if msg.Role == "tool" {
			// zhipu (GLM) rejects "content": null on tool messages with error
			// 1214 ("messages 参数非法"). Use a placeholder string instead —
			// accepted by zhipu, OpenAI, moonshot, deepseek and huoshan alike.
			if msg.Content == "" {
				openAIMsg["content"] = "(empty tool result)"
			}

			// Use ToolCallID directly if available (this is the primary source)
			if msg.ToolCallID != "" {
				openAIMsg["tool_call_id"] = msg.ToolCallID
			} else {
				// Fallback: try to find the matching tool_call ID from assistant messages
				toolCallID := findToolCallID(messages, i)
				if toolCallID != "" {
					openAIMsg["tool_call_id"] = toolCallID
				} else {
					// Last resort - generate a synthetic ID
					openAIMsg["tool_call_id"] = fmt.Sprintf("call_unknown_%d", i)
				}
			}
		}

		result = append(result, openAIMsg)
	}

	// Sanitize: remove incomplete tool_call sequences that would cause API errors
	result = sanitizeToolCallSequence(result)

	return result
}

// normalizeToolArguments guarantees the arguments field is always a JSON
// object string. zhipu (GLM) rejects empty-string arguments with error 1214;
// "{}" is the canonical empty argument payload accepted by all providers.
func normalizeToolArguments(args string) string {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return "{}"
	}
	return trimmed
}

// ConvertMessagesForProvider converts messages using the provider's configuration
func ConvertMessagesForProvider(messages []types.Message, bp *BaseProvider) []map[string]interface{} {
	var config *ConvertConfig
	if bp != nil && bp.ConvertCfg != nil {
		config = bp.ConvertCfg
	}
	return ConvertMessagesWithConfig(messages, config)
}

// ConvertContentPartsToMap converts content parts using the provider's configuration
func ConvertContentPartsToMap(parts []types.ContentPart, config *ConvertConfig) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		converted := convertContentPart(part, config)
		if converted != nil {
			result = append(result, converted)
		}
	}
	return result
}

// convertContentPart converts a single content part based on its type and config
func convertContentPart(part types.ContentPart, config *ConvertConfig) map[string]interface{} {
	switch part.Type {
	case "text":
		return map[string]interface{}{
			"type": "text",
			"text": part.Text,
		}

	case "image_url":
		// Check if model supports vision
		supportVision := config != nil && config.SupportVision
		log.Debugf("[convertContentPart] image_url: SupportVision=%v", supportVision)
		if !supportVision {
			// Model doesn't support vision, convert to text description
			desc := "Image attachment"
			if part.ImageURL != nil && part.ImageURL.URL != "" {
				// NOTE: avoid square brackets — GLM mimics them in its output.
				desc = "Image: " + part.ImageURL.URL
			}
			log.Debugf("[convertContentPart] Converting image_url to text: %s", desc)
			return map[string]interface{}{
				"type": "text",
				"text": desc,
			}
		}
		// Model supports vision, use image_url format
		detail := "auto"
		if part.ImageURL != nil && part.ImageURL.Detail != "" {
			detail = part.ImageURL.Detail
		}
		return map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url":    part.ImageURL.URL,
				"detail": detail,
			},
		}

	case "file":
		return convertFilePart(part.File, config)

	default:
		log.Debugf("[ConvertMessages] Unknown content part type: %s", part.Type)
		return nil
	}
}

// convertFilePart converts a file to the appropriate API format
func convertFilePart(file *types.FileInfo, config *ConvertConfig) map[string]interface{} {
	if file == nil {
		return nil
	}

	// Determine file type from name or stored MIME type
	mimeType := file.MimeType
	if mimeType == "" {
		mimeType = fileContentType(file.Name)
	}

	// Calculate actual file size (estimate from base64 if not stored)
	fileSize := file.Size
	if fileSize == 0 && file.Contents != "" {
		// Estimate: base64 is ~4/3 of original, so divide by 4/3
		if strings.HasPrefix(file.Contents, "data:") {
			parts := strings.SplitN(file.Contents, ",", 2)
			if len(parts) == 2 {
				fileSize = int64(len(parts[1]) * 3 / 4)
			}
		}
	}

	// Determine strategy: check config, then apply rules
	strategy := config.Strategy
	if config.StrategyName != "" {
		strategy = ParseFileStrategy(config.StrategyName)
	} else if config.Strategy == 0 && config.StrategyName == "" {
		strategy = FileStrategyAuto
	}

	// Helper to build text part (must be defined first as it's used by buildImagePart)
	buildTextPart := func(text string) map[string]interface{} {
		return map[string]interface{}{
			"type": "text",
			"text": text,
		}
	}

	// Helper to build image_url part
	buildImagePart := func(url string) map[string]interface{} {
		// Check if model supports vision
		if config != nil && !config.SupportVision {
			// Model doesn't support vision, return text description instead
			// NOTE: avoid square brackets — GLM mimics them in its output.
			return buildTextPart("Image: " + url)
		}
		return map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": url,
			},
		}
	}

	// Helper to build file reference text
	buildFileRef := func() map[string]interface{} {
		if file.URL != "" && config.UploadURLPrefix != "" {
			url := file.URL
			if strings.HasPrefix(url, "/") {
				url = config.UploadURLPrefix + url
			}
			return buildTextPart(fmt.Sprintf("File: %s (%s)", file.Name, url))
		}
		return buildTextPart(fmt.Sprintf("File: %s (type: %s, size: %d bytes)", file.Name, mimeType, fileSize))
	}

	// === Strategy: URL (always prefer URL when available) ===
	if strategy == FileStrategyURL {
		if file.URL != "" && config.UploadURLPrefix != "" {
			url := file.URL
			if strings.HasPrefix(url, "/") {
				url = config.UploadURLPrefix + url
			}
			if isImage(mimeType) {
				return buildImagePart(url)
			}
			return buildTextPart(fmt.Sprintf("File: %s (%s)", file.Name, url))
		}
		// Fall back to base64 if no URL
	}

	// === Strategy: Base64 (always use base64) ===
	if strategy == FileStrategyBase64 {
		if file.Contents != "" {
			if isImage(mimeType) {
				return buildImagePart(file.Contents)
			}
			// For text files, decode and embed
			if isText(mimeType) || isDocument(mimeType) {
				content := decodeFileContent(file.Contents)
				if content != "" {
					return buildTextPart(fmt.Sprintf("File: %s:\n%s", file.Name, content))
				}
			}
			return buildFileRef()
		}
		return buildTextPart(fmt.Sprintf("File: %s - no content available", file.Name))
	}

	// === Strategy: Auto (smart routing) ===
	// Large file (>1MB) + URL available: use URL
	if fileSize > LargeFileThreshold && file.URL != "" && config.UploadURLPrefix != "" {
		url := file.URL
		if strings.HasPrefix(url, "/") {
			url = config.UploadURLPrefix + url
		}
		if isImage(mimeType) {
			return buildImagePart(url)
		}
		return buildTextPart(fmt.Sprintf("File: %s (%s)", file.Name, url))
	}

	// Small file or no URL: use base64 / embedded content
	if file.Contents != "" {
		// Images: always use image_url format with base64
		if isImage(mimeType) {
			return buildImagePart(file.Contents)
		}

		// Text/code files: decode and embed as text
		if isText(mimeType) {
			content := decodeFileContent(file.Contents)
			if content != "" {
				return buildTextPart(fmt.Sprintf("File: %s:\n%s", file.Name, content))
			}
		}

		// Documents (PDF, Office): try to extract readable content
		if isDocument(mimeType) {
			content := decodeFileContent(file.Contents)
			if content != "" && isReadable(content) {
				return buildTextPart(fmt.Sprintf("File: %s:\n%s", file.Name, content))
			}
		}

		// Binary files: return metadata
		return buildTextPart(fmt.Sprintf("File: %s (type: %s, size: %d bytes) - content not directly readable",
			file.Name, mimeType, fileSize))
	}

	// No content available
	return buildTextPart(fmt.Sprintf("File: %s - no content available", file.Name))
}

// decodeFileContent decodes base64 content and returns text
func decodeFileContent(dataURL string) string {
	// Handle data URL format: data:<mime>;base64,<content>
	if strings.HasPrefix(dataURL, "data:") {
		parts := strings.SplitN(dataURL, ",", 2)
		if len(parts) == 2 {
			decoded, err := base64.StdEncoding.DecodeString(parts[1])
			if err == nil {
				content := string(decoded)
				// Truncate if too large (30KB limit for text content)
				if len(content) > 30000 {
					content = content[:30000] + "\n... [file truncated]"
				}
				return content
			}
		}
	}
	return ""
}

// isReadable checks if content appears to be readable text
func isReadable(content string) bool {
	if len(content) == 0 {
		return false
	}
	// Simple check: if most characters are printable or common control chars
	readable := 0
	for _, r := range content {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			readable++
		} else if r >= 32 && r < 127 {
			readable++
		} else if r >= 128 {
			// Allow UTF-8 characters
			readable++
		}
	}
	return float64(readable)/float64(len(content)) > 0.8
}

// sanitizeToolCallSequence removes incomplete tool_call sequences from the message list.
// OpenAI requires that every assistant message with tool_calls must be immediately
// followed by tool messages with matching tool_call_ids.
func sanitizeToolCallSequence(messages []map[string]interface{}) []map[string]interface{} {
	cleaned := make([]map[string]interface{}, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		role, _ := msg["role"].(string)

		if role == "assistant" && msg["tool_calls"] != nil {
			toolCalls, ok := msg["tool_calls"].([]map[string]interface{})
			if !ok || len(toolCalls) == 0 {
				cleaned = append(cleaned, msg)
				continue
			}

			// Check if all tool_calls have corresponding tool messages following.
			// We need exactly `len(toolCalls)` consecutive tool messages right after
			// the assistant message, but we also tolerate interleaved non-tool
			// messages as long as the *total* number of tool messages that belong
			// to this sequence is correct.
			toolMsgCount := 0
			for k := i + 1; k < len(messages); k++ {
				r, _ := messages[k]["role"].(string)
				if r == "tool" {
					toolMsgCount++
				} else if r == "assistant" {
					// Another assistant message starts – stop counting for this sequence
					break
				}
				// user / system messages are ignored (they shouldn't appear here,
				// but if they do we keep counting until the next assistant)
			}

			if toolMsgCount != len(toolCalls) {
				// Incomplete sequence - remove the assistant message with tool_calls
				// and any orphaned tool messages that follow
				log.Debugf("[ConvertMessages] Removing incomplete tool_call sequence at message %d (%d tool_calls, %d tool replies)", i, len(toolCalls), toolMsgCount)
				// Skip this assistant message
				i++
				// Skip any tool messages that belong to this sequence
				for i < len(messages) {
					if r, ok := messages[i]["role"].(string); ok && r == "tool" {
						i++
					} else {
						break
					}
				}
				i-- // adjust for loop increment
				continue
			}
		}

		cleaned = append(cleaned, msg)
	}

	return cleaned
}

// findToolCallID searches backwards to find the matching tool_call ID
func findToolCallID(messages []types.Message, currentIdx int) string {
	// Count only the consecutive tool messages immediately before this one
	consecutiveToolCount := 0
	for i := currentIdx - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			consecutiveToolCount++
		} else if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			// Found the assistant message that initiated these tool calls
			tcIdx := consecutiveToolCount
			if tcIdx < len(messages[i].ToolCalls) {
				return messages[i].ToolCalls[tcIdx].ID
			}
			return ""
		} else {
			// Not a tool or assistant-with-tool-calls message, stop searching
			break
		}
	}
	return ""
}
