package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// ClarifyBridge：Web chat 澄清通道（由 server 注入）。
//
// clarify 工具是双通道的：
//   - Telegram/Discord/CLI：Execute 直接返回结构化 ClarifyResult，由网关平台
//     渲染原生按钮/编号选项，用户答复作为下一条消息进入新回合；
//   - Web chat（本桥已注入且该会话注册了活跃 SSE 通道）：Execute 经
//     ClarifyBridge.Ask 阻塞挂起，server 推送 clarify_required 卡片事件，
//     用户选择/追加说明后唤醒，答复文本作为本工具结果回流模型，
//     agent 在同一回合内继续完成原任务。
// ---------------------------------------------------------------------------

// ClarifyRequest is the structured payload of a clarification request.
type ClarifyRequest struct {
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	Context     string   `json:"context,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
	Header      string   `json:"header,omitempty"`
}

// ClarifyAnswer is the user's reply to a clarification request.
type ClarifyAnswer struct {
	Choices []string `json:"choices,omitempty"` // selected option texts (0..n)
	Note    string   `json:"note,omitempty"`    // free-form追加说明
}

// ErrClarifyUnavailable reports that the current session has no interactive
// clarification channel (bot / CLI / gateway without button support). Callers
// should fall back to the plain ClarifyResult path.
var ErrClarifyUnavailable = errors.New("clarification channel not available in this session")

// ClarifyBridge blocks until the user answers a clarification request on an
// interactive channel (Web chat card). Implemented by internal/server.
type ClarifyBridge interface {
	Ask(ctx context.Context, sessionID string, req ClarifyRequest) (*ClarifyAnswer, error)
}

var clarifyBridge ClarifyBridge

// SetClarifyBridge injects the Web-chat clarification bridge (server side).
// Passing nil removes it. Idempotent; safe for tests.
func SetClarifyBridge(b ClarifyBridge) { clarifyBridge = b }

// ClarifyTool asks the user for clarification with native button support on messaging platforms
type ClarifyTool struct{}

// NewClarifyTool creates a new clarify tool
func NewClarifyTool() *ClarifyTool {
	return &ClarifyTool{}
}

// Name returns the tool name
func (t *ClarifyTool) Name() string {
	return "clarify"
}

// Description returns the tool description
func (t *ClarifyTool) Description() string {
	return "Ask the user for clarification when a request is ambiguous or missing information. " +
		"In the Web chat, calling this tool pauses the turn and pops up an interactive card " +
		"(question + selectable options + free-text note); the turn resumes with the user's answer " +
		"so you can finish the original task. On Telegram and Discord, options are shown as native " +
		"interactive buttons. On CLI, options are shown as numbered choices."
}

// Parameters returns the tool parameters schema
func (t *ClarifyTool) Schema() map[string]interface{} { return t.Parameters() }

func (t *ClarifyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question": map[string]interface{}{
				"type":        "string",
				"description": "The clarification question to ask the user",
			},
			"options": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional list of options for the user to choose from. These will be rendered as native buttons on Telegram/Discord and as selectable chips in the Web chat card.",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Additional context about why clarification is needed",
			},
			"multi_select": map[string]interface{}{
				"type":        "boolean",
				"description": "Allow the user to select multiple options (default: false)",
			},
			"header": map[string]interface{}{
				"type":        "string",
				"description": "Short label for the button group (max 12 chars, e.g., 'Auth method', 'Format')",
			},
		},
		"required": []string{"question"},
	}
}

// ClarifyResult is the structured result returned by the clarify tool
type ClarifyResult struct {
	Status      string   `json:"status"` // "clarification_needed"
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	Context     string   `json:"context,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
	Header      string   `json:"header,omitempty"`
	// Platform-specific rendering hints
	RenderAsButtons bool `json:"render_as_buttons"` // always true when options are provided
}

// Execute asks the user for clarification
func (t *ClarifyTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return nil, fmt.Errorf("question is required")
	}

	req := ClarifyRequest{Question: question}

	if options, ok := args["options"].([]interface{}); ok && len(options) > 0 {
		opts := make([]string, len(options))
		for i, opt := range options {
			opts[i] = fmt.Sprintf("%v", opt)
		}
		req.Options = opts
	}

	if contextVal, ok := args["context"].(string); ok && contextVal != "" {
		req.Context = contextVal
	}

	if multiSelect, ok := args["multi_select"].(bool); ok {
		req.MultiSelect = multiSelect
	}

	if header, ok := args["header"].(string); ok {
		req.Header = header
	}

	// Web chat path: block until the user answers on the interactive card.
	// The answer text flows back as this tool's result so the agent can keep
	// working on the original task within the same turn.
	if sid := SessionIDFromContext(ctx); sid != "" && clarifyBridge != nil {
		ans, err := clarifyBridge.Ask(ctx, sid, req)
		switch {
		case err == nil:
			return formatClarifyAnswer(req, ans), nil
		case errors.Is(err, ErrClarifyUnavailable):
			// No active SSE channel for this session — fall through to the
			// plain structured result (gateway/CLI behavior).
		default:
			return nil, err
		}
	}

	// Gateway / CLI / no-bridge path: return a structured result for the
	// platform renderer (Telegram/Discord buttons, CLI numbered choices).
	result := &ClarifyResult{
		Status:          "clarification_needed",
		Question:        req.Question,
		Options:         req.Options,
		Context:         req.Context,
		MultiSelect:     req.MultiSelect,
		Header:          req.Header,
		RenderAsButtons: len(req.Options) > 0,
	}
	return result, nil
}

// formatClarifyAnswer renders the user's answer as readable text for the model.
func formatClarifyAnswer(req ClarifyRequest, ans *ClarifyAnswer) string {
	var b strings.Builder
	b.WriteString("The user answered your clarification question.\n")
	b.WriteString("Question: " + req.Question + "\n")
	if len(ans.Choices) > 0 {
		b.WriteString("Selected option" + plural(len(ans.Choices)) + ": " + strings.Join(ans.Choices, ", ") + "\n")
	}
	if strings.TrimSpace(ans.Note) != "" {
		b.WriteString("Additional note: " + strings.TrimSpace(ans.Note) + "\n")
	}
	b.WriteString("Proceed with the original task based on this answer.")
	return b.String()
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

// SerializeForGateway serializes the clarify result for gateway platforms
// This is used by Telegram/Discord handlers to render native buttons
func SerializeForGateway(result *ClarifyResult) ([]byte, error) {
	return json.Marshal(result)
}

// BuildTelegramKeyboard builds an InlineKeyboardMarkup for Telegram
func BuildTelegramKeyboard(result *ClarifyResult) string {
	if !result.RenderAsButtons || len(result.Options) == 0 {
		return ""
	}

	// Return structured data that the Telegram handler can parse
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{},
	}

	var rows [][]map[string]interface{}
	row := make([]map[string]interface{}, 0)
	for i, opt := range result.Options {
		btn := map[string]interface{}{
			"text":          opt,
			"callback_data": fmt.Sprintf("clarify:%d", i),
		}
		row = append(row, btn)
		// Max 3 buttons per row
		if len(row) == 3 || i == len(result.Options)-1 {
			rows = append(rows, row)
			row = make([]map[string]interface{}, 0)
		}
	}
	keyboard["inline_keyboard"] = rows

	data, _ := json.Marshal(keyboard)
	return string(data)
}

// BuildDiscordComponents builds ActionRow components for Discord
func BuildDiscordComponents(result *ClarifyResult) string {
	if !result.RenderAsButtons || len(result.Options) == 0 {
		return ""
	}

	components := []map[string]interface{}{
		{
			"type":       1, // ACTION_ROW
			"components": []map[string]interface{}{},
		},
	}

	buttons := make([]map[string]interface{}, 0)
	for i, opt := range result.Options {
		customID := fmt.Sprintf("clarify_%d", i)
		if len(customID) > 100 {
			customID = fmt.Sprintf("cl_%d", i)
		}
		buttons = append(buttons, map[string]interface{}{
			"type":      2, // BUTTON
			"label":     opt,
			"custom_id": customID,
			"style":     1, // PRIMARY
		})
	}
	components[0]["components"] = buttons

	data, _ := json.Marshal(components)
	return string(data)
}
