package skin

import "fmt"

// Config represents a complete skin configuration
type Config struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Colors      Colors   `json:"colors" yaml:"colors"`
	Spinner     Spinner  `json:"spinner" yaml:"spinner"`
	Branding    Branding `json:"branding" yaml:"branding"`
	ToolPrefix  string   `json:"tool_prefix" yaml:"tool_prefix"`
	ToolEmojis  ToolEmojis `json:"tool_emojis" yaml:"tool_emojis"`
}

// Colors defines all color customizations
type Colors struct {
	// Banner colors
	BannerBorder string `json:"banner_border" yaml:"banner_border"`
	BannerTitle  string `json:"banner_title" yaml:"banner_title"`
	BannerAccent string `json:"banner_accent" yaml:"banner_accent"`
	BannerDim    string `json:"banner_dim" yaml:"banner_dim"`
	BannerText   string `json:"banner_text" yaml:"banner_text"`

	// Response box colors
	ResponseBorder string `json:"response_border" yaml:"response_border"`

	// Spinner colors
	SpinnerActive string `json:"spinner_active" yaml:"spinner_active"`

	// Tool output colors
	ToolPrefixColor string `json:"tool_prefix_color" yaml:"tool_prefix_color"`
	ToolText        string `json:"tool_text" yaml:"tool_text"`

	// Prompt colors
	PromptSymbol string `json:"prompt_symbol" yaml:"prompt_symbol"`
	PromptText   string `json:"prompt_text" yaml:"prompt_text"`

	// Error/Warning colors
	Error   string `json:"error" yaml:"error"`
	Warning string `json:"warning" yaml:"warning"`
	Success string `json:"success" yaml:"success"`
}

// Spinner defines spinner animation settings
type Spinner struct {
	// Spinner characters
	Frames []string `json:"frames" yaml:"frames"`

	// Waiting state faces (emoji)
	WaitingFaces []string `json:"waiting_faces" yaml:"waiting_faces"`

	// Thinking state faces (emoji)
	ThinkingFaces []string `json:"thinking_faces" yaml:"thinking_faces"`

	// Thinking verbs (actions shown during thinking)
	ThinkingVerbs []string `json:"thinking_verbs" yaml:"thinking_verbs"`

	// Wings (decorative characters on each side)
	Wings [][]string `json:"wings" yaml:"wings"`

	// Animation speed (milliseconds per frame)
	Speed int `json:"speed" yaml:"speed"`
}

// Branding defines branding text customizations
type Branding struct {
	// Agent display name
	AgentName string `json:"agent_name" yaml:"agent_name"`

	// Welcome message
	Welcome string `json:"welcome" yaml:"welcome"`

	// Response box label
	ResponseLabel string `json:"response_label" yaml:"response_label"`

	// Prompt symbol (> , $, etc.)
	PromptSymbol string `json:"prompt_symbol" yaml:"prompt_symbol"`

	// ASCII art banner
	Banner string `json:"banner" yaml:"banner"`
}

// ToolEmojis maps tool names to emoji icons
type ToolEmojis map[string]string

// GetToolEmoji returns the emoji for a specific tool
func (te ToolEmojis) GetToolEmoji(toolName string) string {
	if emoji, ok := te[toolName]; ok {
		return emoji
	}
	return "⚡" // Default
}

// Validate validates the skin configuration
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("skin name is required")
	}
	if len(c.Spinner.Frames) == 0 {
		return fmt.Errorf("spinner frames are required")
	}
	return nil
}

// Merge merges another config into this one, only overwriting empty values
func (c *Config) Merge(other *Config) {
	if other == nil {
		return
	}

	// Merge colors
	c.Colors = mergeColors(c.Colors, other.Colors)

	// Merge spinner
	if len(other.Spinner.Frames) > 0 {
		c.Spinner.Frames = other.Spinner.Frames
	}
	if len(other.Spinner.WaitingFaces) > 0 {
		c.Spinner.WaitingFaces = other.Spinner.WaitingFaces
	}
	if len(other.Spinner.ThinkingFaces) > 0 {
		c.Spinner.ThinkingFaces = other.Spinner.ThinkingFaces
	}
	if len(other.Spinner.ThinkingVerbs) > 0 {
		c.Spinner.ThinkingVerbs = other.Spinner.ThinkingVerbs
	}
	if other.Spinner.Speed > 0 {
		c.Spinner.Speed = other.Spinner.Speed
	}

	// Merge branding
	if other.Branding.AgentName != "" {
		c.Branding.AgentName = other.Branding.AgentName
	}
	if other.Branding.Welcome != "" {
		c.Branding.Welcome = other.Branding.Welcome
	}
	if other.Branding.ResponseLabel != "" {
		c.Branding.ResponseLabel = other.Branding.ResponseLabel
	}
	if other.Branding.PromptSymbol != "" {
		c.Branding.PromptSymbol = other.Branding.PromptSymbol
	}
	if other.Branding.Banner != "" {
		c.Branding.Banner = other.Branding.Banner
	}

	// Merge other fields
	if other.ToolPrefix != "" {
		c.ToolPrefix = other.ToolPrefix
	}
}

// mergeColors merges color configs
func mergeColors(base, overlay Colors) Colors {
	if overlay.BannerBorder != "" {
		base.BannerBorder = overlay.BannerBorder
	}
	if overlay.BannerTitle != "" {
		base.BannerTitle = overlay.BannerTitle
	}
	if overlay.BannerAccent != "" {
		base.BannerAccent = overlay.BannerAccent
	}
	if overlay.BannerDim != "" {
		base.BannerDim = overlay.BannerDim
	}
	if overlay.BannerText != "" {
		base.BannerText = overlay.BannerText
	}
	if overlay.ResponseBorder != "" {
		base.ResponseBorder = overlay.ResponseBorder
	}
	if overlay.SpinnerActive != "" {
		base.SpinnerActive = overlay.SpinnerActive
	}
	if overlay.ToolPrefixColor != "" {
		base.ToolPrefixColor = overlay.ToolPrefixColor
	}
	if overlay.ToolText != "" {
		base.ToolText = overlay.ToolText
	}
	if overlay.PromptSymbol != "" {
		base.PromptSymbol = overlay.PromptSymbol
	}
	if overlay.PromptText != "" {
		base.PromptText = overlay.PromptText
	}
	if overlay.Error != "" {
		base.Error = overlay.Error
	}
	if overlay.Warning != "" {
		base.Warning = overlay.Warning
	}
	if overlay.Success != "" {
		base.Success = overlay.Success
	}
	return base
}
