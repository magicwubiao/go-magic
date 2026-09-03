package types

import (
	"encoding/json"
	"time"
)

type Message struct {
	ID        string    `json:"id,omitempty"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	// 多模态内容，非空时优先于 Content
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	ChannelID    string        `json:"channel_id,omitempty"`
	From         string        `json:"from,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"` // Required for tool role messages
	// 本轮回复改动的文件（write/delete/batch 等写操作），UI 用于展示"变更的文件"。
	// 注意：仅供会话持久化/展示使用；agent history 里的消息该字段恒为 nil，
	// 序列化给 provider 时因 omitempty 不会泄漏到请求载荷。
	FileOps []FileOp `json:"file_ops,omitempty"`
}

// FileOp 描述一次工具调用对单个文件的操作。Action 取值与后端 extractFileOps
// 保持一致：read / write / delete / list / search / batch / access。
type FileOp struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	Param  string `json:"param,omitempty"`
}

// ContentPart represents a part of a multimodal message content
type ContentPart struct {
	Type     string    `json:"type"` // "text", "image_url", "video_url", "audio_url", "file"
	Text     string    `json:"text,omitempty"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
	File     *FileInfo `json:"file,omitempty"`
}

// MediaURL represents a URL with optional detail level (for images)
type MediaURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high" (for images)
}

// FileInfo represents file metadata
type FileInfo struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Contents string `json:"contents,omitempty"` // Base64 encoded file contents
}

type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	// 兼容旧代码
	Type     string   `json:"type,omitempty"`
	Function Function `json:"function,omitempty"`
}

type Function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// GetToolName returns the tool name, trying Function.Name first, then falling back to Name.
// This handles inconsistencies across different providers where some set Function.Name
// and others set only the top-level Name field.
func (tc *ToolCall) GetToolName() string {
	if tc.Function.Name != "" {
		return tc.Function.Name
	}
	return tc.Name
}

// Normalize ensures both Function.Name and Name are populated for consistency.
// After normalization, GetToolName() will return the correct value regardless of
// which field the provider originally set.
func (tc *ToolCall) Normalize() {
	name := tc.GetToolName()
	if name == "" {
		return
	}
	tc.Name = name
	if tc.Function.Name == "" {
		tc.Function.Name = name
	}
	if tc.Function.Arguments == "" && tc.Arguments != nil {
		if argsBytes, err := json.Marshal(tc.Arguments); err == nil {
			tc.Function.Arguments = string(argsBytes)
		}
	}
}

type ChatResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
