package types

type Message struct {
	ID         string        `json:"id,omitempty"`
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	// 多模态内容，非空时优先于 Content
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	ChannelID   string        `json:"channel_id,omitempty"`
	From       string        `json:"from,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"` // Required for tool role messages
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

type ChatResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
