package tool

import (
	"context"
	"fmt"
)

// SendMessageTool allows the agent to send messages to any connected gateway platform
// This enables the agent to proactively communicate with users across Telegram, Discord, Slack, etc.
type SendMessageTool struct {
	BaseTool
	gateway GatewaySender
}

// GatewaySender defines the interface for sending messages through the gateway
type GatewaySender interface {
	SendToPlatform(platform, channelID, content string) error
	Broadcast(content string)
	GetConnectedPlatforms() []string
}

// SendMessageRequest represents a request to send a message
type SendMessageRequest struct {
	Platform  string `json:"platform"`   // "telegram", "discord", "slack", "all"
	ChannelID string `json:"channel_id"` // Channel or user ID to send to
	Content   string `json:"content"`    // Message content
	Broadcast bool   `json:"broadcast"`  // If true, send to all connected users
}

// SendMessageResult represents the result of sending a message
type SendMessageResult struct {
	Success   bool   `json:"success"`
	Platform  string `json:"platform"`
	ChannelID string `json:"channel_id,omitempty"`
	Content   string `json:"content"`
	Error     string `json:"error,omitempty"`
}

// NewSendMessageTool creates a new send message tool
func NewSendMessageTool(gateway GatewaySender) *SendMessageTool {
	return &SendMessageTool{
		BaseTool: *NewBaseTool(
			"send_message",
			"Send a message to a user on any connected messaging platform (Telegram, Discord, Slack, etc.). Use this to proactively notify users, send updates, or respond across platforms. Set broadcast=true to send to all connected users.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"platform": map[string]interface{}{
						"type":        "string",
						"description": "Platform to send to: telegram, discord, slack, wechat, or 'all' for broadcast",
						"enum":        []string{"telegram", "discord", "slack", "wechat", "all"},
					},
					"channel_id": map[string]interface{}{
						"type":        "string",
						"description": "Channel ID or user ID to send to (not needed if broadcast=true)",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Message content to send",
					},
					"broadcast": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, broadcast to all connected users on all platforms",
						"default":     false,
					},
				},
				"required": []string{"platform", "content"},
			},
		),
		gateway: gateway,
	}
}

// Execute sends a message through the gateway
func (t *SendMessageTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	platform, _ := params["platform"].(string)
	content, _ := params["content"].(string)
	channelID, _ := params["channel_id"].(string)
	broadcast := false
	if v, ok := params["broadcast"].(bool); ok {
		broadcast = v
	}

	if platform == "" {
		return nil, fmt.Errorf("platform is required")
	}
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	result := &SendMessageResult{
		Platform:  platform,
		ChannelID: channelID,
		Content:   content,
	}

	if t.gateway == nil {
		result.Error = "gateway not configured"
		return result, nil
	}

	if broadcast || platform == "all" {
		t.gateway.Broadcast(content)
		result.Success = true
		return result, nil
	}

	if channelID == "" {
		result.Error = "channel_id is required when not broadcasting"
		return result, nil
	}

	err := t.gateway.SendToPlatform(platform, channelID, content)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	result.Success = true
	return result, nil
}

// SetGateway updates the gateway sender (for late binding)
func (t *SendMessageTool) SetGateway(gateway GatewaySender) {
	t.gateway = gateway
}

// GetConnectedPlatforms returns list of connected platforms
func (t *SendMessageTool) GetConnectedPlatforms() []string {
	if t.gateway == nil {
		return nil
	}
	return t.gateway.GetConnectedPlatforms()
}
