package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// =============================================================================
// Home Assistant Integration
// =============================================================================

// HAConfig holds Home Assistant connection configuration
type HAConfig struct {
	URL      string // e.g., "http://homeassistant.local:8123"
	Token    string // Long-lived access token
	Client   *http.Client
}

// NewHAConfig creates a new Home Assistant config from environment
func NewHAConfig() *HAConfig {
	return &HAConfig{
		URL:   os.Getenv("HASS_URL"),
		Token: os.Getenv("HASS_TOKEN"),
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// IsConfigured checks if Home Assistant is properly configured
func (c *HAConfig) IsConfigured() bool {
	return c.URL != "" && c.Token != ""
}

// callHA makes an authenticated API call to Home Assistant
func (c *HAConfig) callHA(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	url := strings.TrimSuffix(c.URL, "/") + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HA API error: %v", result)
	}

	return result, nil
}

// =============================================================================
// Home Assistant Tools
// =============================================================================

// HATool provides Home Assistant smart home control
type HATool struct {
	config *HAConfig
}

func NewHATool() *HATool {
	return &HATool{
		config: NewHAConfig(),
	}
}

func (t *HATool) Name() string { return "ha_list_entities" }

func (t *HATool) Description() string {
	return "List all Home Assistant entities with their states"
}

func (t *HATool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"domain": map[string]interface{}{
				"type":        "string",
				"description": "Filter by domain (e.g., light, switch, sensor, climate)",
			},
		},
	}
}

func (t *HATool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if !t.config.IsConfigured() {
		return nil, fmt.Errorf("Home Assistant not configured: set HASS_URL and HASS_TOKEN")
	}

	domain, _ := args["domain"].(string)
	path := "/api/states"
	if domain != "" {
		path = "/api/states?entity_id=" + domain + ".*"
	}

	result, err := t.config.callHA(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// Parse response
	states, ok := result[""].([]interface{})
	if !ok {
		// Try alternate format
		var allStates []map[string]interface{}
		raw, _ := json.Marshal(result)
		json.Unmarshal(raw, &allStates)

		if len(allStates) == 0 {
			return nil, fmt.Errorf("unexpected response format")
		}

		// Filter by domain if specified
		if domain != "" {
			var filtered []map[string]interface{}
			for _, state := range allStates {
				if entityID, ok := state["entity_id"].(string); ok {
					if strings.HasPrefix(entityID, domain+".") {
						filtered = append(filtered, state)
					}
				}
			}
			allStates = filtered
		}

		return map[string]interface{}{
			"count":    len(allStates),
			"entities": allStates,
		}, nil
	}

	return result, nil
}

// =============================================================================
// HA Get State Tool
// =============================================================================

// HAGetStateTool gets the state of a specific entity
type HAGetStateTool struct {
	config *HAConfig
}

func NewHAGetStateTool() *HAGetStateTool {
	return &HAGetStateTool{
		config: NewHAConfig(),
	}
}

func (t *HAGetStateTool) Name() string { return "ha_get_state" }

func (t *HAGetStateTool) Description() string {
	return "Get the current state of a specific Home Assistant entity"
}

func (t *HAGetStateTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"entity_id": map[string]interface{}{
				"type":        "string",
				"description": "The entity ID (e.g., light.living_room, switch.ac_unit)",
			},
		},
		"required": []string{"entity_id"},
	}
}

func (t *HAGetStateTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if !t.config.IsConfigured() {
		return nil, fmt.Errorf("Home Assistant not configured: set HASS_URL and HASS_TOKEN")
	}

	entityID, _ := args["entity_id"].(string)
	if entityID == "" {
		return nil, fmt.Errorf("entity_id is required")
	}

	result, err := t.config.callHA(ctx, "GET", "/api/states/"+entityID, nil)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"entity_id": result["entity_id"],
		"state":     result["state"],
		"attributes": result["attributes"],
		"last_changed": result["last_changed"],
		"last_updated": result["last_updated"],
	}, nil
}

// =============================================================================
// HA List Services Tool
// =============================================================================

// HAListServicesTool lists available Home Assistant services
type HAListServicesTool struct {
	config *HAConfig
}

func NewHAListServicesTool() *HAListServicesTool {
	return &HAListServicesTool{
		config: NewHAConfig(),
	}
}

func (t *HAListServicesTool) Name() string { return "ha_list_services" }

func (t *HAListServicesTool) Description() string {
	return "List all available Home Assistant services grouped by domain"
}

func (t *HAListServicesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"domain": map[string]interface{}{
				"type":        "string",
				"description": "Filter by domain (e.g., light, switch, climate)",
			},
		},
	}
}

func (t *HAListServicesTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if !t.config.IsConfigured() {
		return nil, fmt.Errorf("Home Assistant not configured: set HASS_URL and HASS_TOKEN")
	}

	domain, _ := args["domain"].(string)
	path := "/api/services"
	if domain != "" {
		path = "/api/services/" + domain
	}

	result, err := t.config.callHA(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// =============================================================================
// HA Call Service Tool
// =============================================================================

// HACallServiceTool calls a Home Assistant service
type HACallServiceTool struct {
	config *HAConfig
}

func NewHACallServiceTool() *HACallServiceTool {
	return &HACallServiceTool{
		config: NewHAConfig(),
	}
}

func (t *HACallServiceTool) Name() string { return "ha_call_service" }

func (t *HACallServiceTool) Description() string {
	return "Call a Home Assistant service (e.g., turn on/off lights, set temperature)"
}

func (t *HACallServiceTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"domain": map[string]interface{}{
				"type":        "string",
				"description": "Service domain (e.g., light, switch, climate, automation)",
			},
			"service": map[string]interface{}{
				"type":        "string",
				"description": "Service to call (e.g., turn_on, turn_off, set_temperature)",
			},
			"entity_id": map[string]interface{}{
				"type":        "string",
				"description": "Target entity ID(s)",
			},
			"data": map[string]interface{}{
				"type":        "object",
				"description": "Additional service data/parameters",
			},
		},
		"required": []string{"domain", "service"},
	}
}

func (t *HACallServiceTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if !t.config.IsConfigured() {
		return nil, fmt.Errorf("Home Assistant not configured: set HASS_URL and HASS_TOKEN")
	}

	domain, _ := args["domain"].(string)
	service, _ := args["service"].(string)
	entityID, _ := args["entity_id"].(string)
	data, _ := args["data"].(map[string]interface{})

	if domain == "" || service == "" {
		return nil, fmt.Errorf("domain and service are required")
	}

	payload := map[string]interface{}{
		"entity_id": entityID,
	}
	if data != nil {
		for k, v := range data {
			payload[k] = v
		}
	}

	path := fmt.Sprintf("/api/services/%s/%s", domain, service)
	result, err := t.config.callHA(ctx, "POST", path, payload)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":   true,
		"domain":    domain,
		"service":   service,
		"results":   result,
	}, nil
}

// =============================================================================
// HA Events Tool
// =============================================================================

// HAEventsTool provides Home Assistant event monitoring
type HAEventsTool struct {
	config *HAConfig
}

func NewHAEventsTool() *HAEventsTool {
	return &HAEventsTool{
		config: NewHAConfig(),
	}
}

func (t *HAEventsTool) Name() string { return "ha_events" }

func (t *HAEventsTool) Description() string {
	return "Subscribe to Home Assistant events (e.g., state changes, automation triggers)"
}

func (t *HAEventsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":       []string{"subscribe", "unsubscribe", "list"},
				"description": "Action to perform",
			},
			"event_type": map[string]interface{}{
				"type":        "string",
				"description": "Event type to subscribe to (e.g., state_changed, automation_triggered)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *HAEventsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if !t.config.IsConfigured() {
		return nil, fmt.Errorf("Home Assistant not configured: set HASS_URL and HASS_TOKEN")
	}

	action, _ := args["action"].(string)
	eventType, _ := args["event_type"].(string)

	switch action {
	case "list":
		result, err := t.config.callHA(ctx, "GET", "/api/events", nil)
		if err != nil {
			return nil, err
		}
		return result, nil

	case "subscribe":
		if eventType == "" {
			return nil, fmt.Errorf("event_type is required for subscribe")
		}
		// Note: True WebSocket subscription requires WebSocket support
		// This is a simplified HTTP polling approach
		return map[string]interface{}{
			"subscribed":  true,
			"event_type":  eventType,
			"note":        "Use ha_get_state to poll for changes",
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// =============================================================================
// HA Config Tool
// =============================================================================

// HAConfigTool provides Home Assistant system information
type HAConfigTool struct {
	config *HAConfig
}

func NewHAConfigTool() *HAConfigTool {
	return &HAConfigTool{
		config: NewHAConfig(),
	}
}

func (t *HAConfigTool) Name() string { return "ha_config" }

func (t *HAConfigTool) Description() string {
	return "Get Home Assistant configuration and system information"
}

func (t *HAConfigTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"section": map[string]interface{}{
				"type":        "string",
				"description": "Config section: general, components, core",
			},
		},
	}
}

func (t *HAConfigTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if !t.config.IsConfigured() {
		return nil, fmt.Errorf("Home Assistant not configured: set HASS_URL and HASS_TOKEN")
	}

	section, _ := args["section"].(string)
	path := "/api/config"
	if section != "" {
		path = "/api/config/core_" + section
	}

	result, err := t.config.callHA(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	return result, nil
}
