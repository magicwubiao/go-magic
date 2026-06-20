package provider

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// FileStrategy defines how files should be converted
type FileStrategy int

const (
	FileStrategyAuto FileStrategy = iota // Auto-select based on file type
	FileStrategyURL                      // Prefer URL references (for large files)
	FileStrategyBase64                   // Always use base64
)

// LargeFileThreshold is the size (bytes) above which files should use URL instead of base64
const LargeFileThreshold = 1024 * 1024 // 1MB

// ConvertConfig holds conversion settings
type ConvertConfig struct {
	UploadURLPrefix string        // Public URL prefix for uploaded files
	Strategy        FileStrategy  // File conversion strategy
	StrategyName    string        // String representation of strategy ("auto", "url", "base64")
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

// MIME type categories
var (
	imageMimeTypes = map[string]bool{
		"image/png":  true,
		"image/jpeg": true,
		"image/gif":  true,
		"image/webp": true,
		"image/svg+xml": true,
	}

	textMimeTypes = map[string]bool{
		"text/plain":                  true,
		"text/html":                   true,
		"text/css":                    true,
		"text/csv":                    true,
		"text/markdown":                true,
		"application/json":            true,
		"application/xml":             true,
		"application/javascript":       true,
		"application/x-yaml":          true,
		"application/x-sh":            true,
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
		"application/pdf":         true,
		"application/msword":                         true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.ms-excel":                  true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
		"application/vnd.ms-powerpoint":             true,
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
			openAIMsg["content"] = parts
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
						"arguments": tc.Function.Arguments,
					},
				}
				toolCalls = append(toolCalls, toolCall)
			}
			openAIMsg["tool_calls"] = toolCalls
		}

		// Handle tool_call_id for tool messages - ALWAYS include it for tool role
		if msg.Role == "tool" {
			// OpenAI requires tool messages to have content (null is ok, but not missing)
			// If content is empty, set it to null
			if msg.Content == "" {
				openAIMsg["content"] = nil
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

// convertContentPart converts a single content part based on its type and config
func convertContentPart(part types.ContentPart, config *ConvertConfig) map[string]interface{} {
	switch part.Type {
	case "text":
		return map[string]interface{}{
			"type": "text",
			"text": part.Text,
		}

	case "image_url":
		// Direct image URL reference
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
				fileSize = len(parts[1]) * 3 / 4
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

	// Helper to build image_url part
	buildImagePart := func(url string) map[string]interface{} {
		return map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": url,
			},
		}
	}

	// Helper to build text part
	buildTextPart := func(text string) map[string]interface{} {
		return map[string]interface{}{
			"type": "text",
			"text": text,
		}
	}

	// Helper to build file reference text
	buildFileRef := func() map[string]interface{} {
		if file.URL != "" && config.UploadURLPrefix != "" {
			url := file.URL
			if strings.HasPrefix(url, "/") {
				url = config.UploadURLPrefix + url
			}
			return buildTextPart(fmt.Sprintf("[File: %s](%s)", file.Name, url))
		}
		return buildTextPart(fmt.Sprintf("[File: %s] (type: %s, size: %d bytes)", file.Name, mimeType, fileSize))
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
			return buildTextPart(fmt.Sprintf("[File: %s](%s)", file.Name, url))
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
					return buildTextPart(fmt.Sprintf("[File: %s]\n%s", file.Name, content))
				}
			}
			return buildFileRef()
		}
		return buildTextPart(fmt.Sprintf("[File: %s] - no content available", file.Name))
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
		return buildTextPart(fmt.Sprintf("[File: %s](%s)", file.Name, url))
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
				return buildTextPart(fmt.Sprintf("[File: %s]\n%s", file.Name, content))
			}
		}

		// Documents (PDF, Office): try to extract readable content
		if isDocument(mimeType) {
			content := decodeFileContent(file.Contents)
			if content != "" && isReadable(content) {
				return buildTextPart(fmt.Sprintf("[File: %s]\n%s", file.Name, content))
			}
		}

		// Binary files: return metadata
		return buildTextPart(fmt.Sprintf("[File: %s] (type: %s, size: %d bytes) - content not directly readable",
			file.Name, mimeType, fileSize))
	}

	// No content available
	return buildTextPart(fmt.Sprintf("[File: %s] - no content available", file.Name))
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
