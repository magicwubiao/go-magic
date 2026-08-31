package tool

import (
	"context"
	"fmt"
)

// BrowserDialogTool lists and responds to native JavaScript dialogs
// (alert / confirm / prompt) on the current page.
// Referenced from hermes-agent's browser_dialog tool: dialogs are intercepted
// automatically during navigation; this tool inspects them and sets responses.
type BrowserDialogTool struct {
	bt *BrowserTools
}

// NewBrowserDialogTool creates a new browser dialog tool.
func NewBrowserDialogTool(bt *BrowserTools) *BrowserDialogTool {
	return &BrowserDialogTool{bt: bt}
}

// Name returns the tool name.
func (t *BrowserDialogTool) Name() string { return "browser_dialog" }

// Description returns the tool description.
func (t *BrowserDialogTool) Description() string {
	return "List or respond to native JavaScript dialogs (alert/confirm/prompt) on the current page. " +
		"Dialogs are intercepted automatically and recorded; use action=list to inspect them, " +
		"action=respond to pre-set the answer for confirm/prompt dialogs, or action=clear to reset."
}

// Schema returns the tool JSON schema.
func (t *BrowserDialogTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"list", "respond", "clear"},
				"description": "Action to perform (default: list)",
			},
			"dialog_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"confirm", "prompt"},
				"description": "Dialog type to respond to (required for respond)",
			},
			"response": map[string]interface{}{
				"type":        "string",
				"description": "Response value: 'true'/'false' for confirm, or the text value for prompt",
			},
			"tab_id": map[string]interface{}{
				"type":        "string",
				"description": "Tab ID from previous browser_navigate call (optional)",
			},
		},
	}
}

// Execute performs the dialog action.
func (t *BrowserDialogTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	action, _ := args["action"].(string)
	if action == "" {
		action = "list"
	}

	tabID := "default"
	if id, ok := args["tab_id"].(string); ok && id != "" {
		tabID = id
	}

	bm := GetBrowserManager()
	if _, ok := bm.GetTab(tabID); !ok {
		return nil, fmt.Errorf("no active browser tab. Please call browser_navigate first")
	}

	// Ensure the interceptor is installed (idempotent).
	if err := bm.InstallDialogInterceptor(tabID); err != nil {
		return nil, err
	}

	switch action {
	case "list":
		dialogs, err := bm.GetPendingDialogs(tabID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"status":  "ok",
			"count":   len(dialogs),
			"dialogs": dialogs,
			"hint":    "Use action=respond with dialog_type and response to pre-set answers for confirm/prompt dialogs.",
		}, nil

	case "respond":
		dialogType, _ := args["dialog_type"].(string)
		if dialogType != "confirm" && dialogType != "prompt" {
			return nil, fmt.Errorf("dialog_type must be 'confirm' or 'prompt'")
		}
		response, _ := args["response"].(string)
		if response == "" {
			return nil, fmt.Errorf("response is required for respond action")
		}
		if err := bm.SetDialogResponse(tabID, dialogType, response); err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"status":      "response_set",
			"dialog_type": dialogType,
			"response":    response,
		}, nil

	case "clear":
		if err := bm.ClearDialogs(tabID); err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"status": "cleared",
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (use list, respond, or clear)", action)
	}
}
