package skin

// Built-in skin definitions

// Default skin - classic Hermes gold/kawaii style
var DefaultSkin = &Config{
	Name:        "default",
	Description: "Classic Hermes gold/kawaii style",
	Colors: Colors{
		BannerBorder:   "\033[38;5;220m", // Gold
		BannerTitle:    "\033[38;5;214m", // Orange
		BannerAccent:   "\033[38;5;220m", // Gold
		BannerDim:      "\033[38;5;241m", // Gray
		BannerText:     "\033[38;5;255m", // White
		ResponseBorder: "\033[38;5;220m", // Gold
		SpinnerActive:  "\033[38;5;214m", // Orange
		ToolPrefixColor: "\033[38;5;75m",  // Cyan
		ToolText:       "\033[38;5;255m", // White
		PromptSymbol:   "\033[38;5;214m", // Orange
		PromptText:     "\033[38;5;255m", // White
		Error:          "\033[38;5;196m", // Red
		Warning:        "\033[38;5;214m", // Orange
		Success:        "\033[38;5;82m",  // Green
	},
	Spinner: Spinner{
		Frames: []string{
			"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
		},
		WaitingFaces: []string{
			"(*・ω・)ノ",
			"(￣ω￣)ノ",
			"(ノ・ω・)ノ",
			"(・ω・)ノ",
			"(~ω~)ノ",
		},
		ThinkingFaces: []string{
			"(･ω･)ゞ",
			"(｀・ω・)ゞ",
			"(・ω・)ゞ",
			"(￣ω￣)ゞ",
		},
		ThinkingVerbs: []string{
			"thinking",
			"analyzing",
			"processing",
			"researching",
			"planning",
		},
		Wings: [][]string{
			{"⟨⚡", "⚡⟩"},
		},
		Speed: 80,
	},
	Branding: Branding{
		AgentName:     "magic",
		Welcome:       "Type /help for commands, /exit to quit",
		ResponseLabel: "Response",
		PromptSymbol:  ">",
	},
	ToolPrefix: "┊",
	ToolEmojis: ToolEmojis{
		"web_search":        "🌐",
		"web_extract":       "🔍",
		"read_file":         "📄",
		"write_file":        "✏️",
		"execute_command":   "⚡",
		"browser_navigate":  "🌍",
		"delegate_task":     "🎭",
		"memory_store":      "💾",
		"memory_recall":     "🧠",
		"session_search":    "🔎",
		"cronjob":           "⏰",
		"skill_list":        "📚",
		"skill_view":        "📖",
		"skill_create":      "✨",
		"ha_list_entities":  "🏠",
		"ha_call_service":   "🔌",
		"execute_code":      "💻",
		"json":              "{}",
		"yaml":              "📝",
		"hash":              "#️⃣",
		"uuid":              "🆔",
	},
}

// Mono skin - clean grayscale monochrome
var MonoSkin = &Config{
	Name:        "mono",
	Description: "Clean grayscale monochrome",
	Colors: Colors{
		BannerBorder:   "\033[90m",
		BannerTitle:    "\033[97m",
		BannerAccent:   "\033[37m",
		BannerDim:      "\033[90m",
		BannerText:     "\033[97m",
		ResponseBorder: "\033[90m",
		SpinnerActive:  "\033[97m",
		ToolPrefixColor: "\033[90m",
		ToolText:       "\033[97m",
		PromptSymbol:   "\033[97m",
		PromptText:     "\033[90m",
		Error:          "\033[97m",
		Warning:        "\033[90m",
		Success:        "\033[37m",
	},
	Spinner: Spinner{
		Frames: []string{
			"◐", "◓", "◑", "◒",
		},
		WaitingFaces: []string{
			"[ ]",
			"[=]",
			"[*]",
		},
		ThinkingFaces: []string{
			"[?]",
			"[~]",
		},
		ThinkingVerbs: []string{
			"processing",
			"loading",
			"working",
		},
		Wings: [][]string{
			{"[", "]"},
		},
		Speed: 150,
	},
	Branding: Branding{
		AgentName:     "magic",
		Welcome:       "Type /help for commands, /exit to quit",
		ResponseLabel: "OUT",
		PromptSymbol:  "$",
	},
	ToolPrefix: "│",
	ToolEmojis: ToolEmojis{
		"web_search":      "[W]",
		"read_file":       "[F]",
		"write_file":      "[F]",
		"execute_command": "[X]",
		"browser_navigate":"[B]",
		"delegate_task":   "[D]",
		"memory_store":     "[M]",
		"memory_recall":    "[M]",
		"execute_code":     "[C]",
	},
}

// Slate skin - cool blue developer-focused theme
var SlateSkin = &Config{
	Name:        "slate",
	Description: "Cool blue developer-focused theme",
	Colors: Colors{
		BannerBorder:   "\033[38;5;75m",  // Blue
		BannerTitle:    "\033[38;5;39m",  // Bright blue
		BannerAccent:   "\033[38;5;117m", // Light blue
		BannerDim:      "\033[38;5;59m",  // Dark blue
		BannerText:     "\033[38;5;195m", // Very light blue
		ResponseBorder: "\033[38;5;75m",  // Blue
		SpinnerActive:  "\033[38;5;39m",  // Bright blue
		ToolPrefixColor: "\033[38;5;75m", // Blue
		ToolText:        "\033[38;5;195m", // Light blue
		PromptSymbol:    "\033[38;5;39m",  // Bright blue
		PromptText:      "\033[38;5;195m", // Light blue
		Error:           "\033[38;5;203m", // Red
		Warning:         "\033[38;5;214m", // Orange
		Success:         "\033[38;5;114m", // Green
	},
	Spinner: Spinner{
		Frames: []string{
			"▁", "▂", "▃", "▅", "▆", "▇", "▆", "▅", "▃", "▂",
		},
		WaitingFaces: []string{
			"[-]",
			"[\\]",
			"[|]",
			"[/]",
			"[-]",
		},
		ThinkingFaces: []string{
			"[~]",
			"[≈]",
		},
		ThinkingVerbs: []string{
			"loading",
			"building",
			"compiling",
			"testing",
		},
		Wings: [][]string{
			{"━", "━"},
		},
		Speed: 100,
	},
	Branding: Branding{
		AgentName:     "magic",
		Welcome:       "Type /help for commands, /exit to quit",
		ResponseLabel: "▸",
		PromptSymbol:  "›",
	},
	ToolPrefix: "│",
	ToolEmojis: ToolEmojis{
		"web_search":      "🌐",
		"read_file":       "📄",
		"write_file":      "📝",
		"execute_command": "⚡",
		"browser_navigate":"🌍",
		"delegate_task":   "🎭",
		"memory_store":    "💾",
		"memory_recall":   "🧠",
		"execute_code":    "💻",
	},
}

// Cyber skin - neon cyberpunk theme
var CyberSkin = &Config{
	Name:        "cyber",
	Description: "Neon cyberpunk terminal theme",
	Colors: Colors{
		BannerBorder:   "\033[38;5;201m", // Magenta
		BannerTitle:    "\033[38;5;51m",  // Cyan
		BannerAccent:   "\033[38;5;219m", // Pink
		BannerDim:      "\033[38;5;57m",  // Dark gray
		BannerText:     "\033[38;5;231m", // White
		ResponseBorder: "\033[38;5;201m", // Magenta
		SpinnerActive:  "\033[38;5;51m",  // Cyan
		ToolPrefixColor: "\033[38;5;201m", // Magenta
		ToolText:        "\033[38;5;231m", // White
		PromptSymbol:    "\033[38;5;51m",  // Cyan
		PromptText:      "\033[38;5;219m", // Pink
		Error:           "\033[38;5;196m", // Red
		Warning:         "\033[38;5;214m", // Orange
		Success:         "\033[38;5;82m",  // Green
	},
	Spinner: Spinner{
		Frames: []string{
			"█", "▓", "▒", "░", "▒", "▓",
		},
		WaitingFaces: []string{
			"█[░]█",
			"█[▒]█",
			"█[▓]█",
		},
		ThinkingFaces: []string{
			"█[█]█",
			"▓[█]▓",
		},
		ThinkingVerbs: []string{
			"jacking in",
			"decrypting",
			"uploading",
			"hacking",
		},
		Wings: [][]string{
			{"⟨⚡", "⚡⟩"},
		},
		Speed: 80,
	},
	Branding: Branding{
		AgentName:     "Cyber Agent",
		Welcome:       "Type /help for commands, /exit to quit",
		ResponseLabel: "⚡ Cyber ",
		PromptSymbol:  "›",
	},
	ToolPrefix: "▏",
	ToolEmojis: ToolEmojis{
		"web_search":      "🌐",
		"read_file":      "📄",
		"write_file":     "💾",
		"execute_command": "⚡",
		"browser_navigate":"🌍",
		"delegate_task":  "🎭",
		"memory_store":    "🧠",
		"memory_recall":   "📤",
		"execute_code":    "💻",
	},
}

// BuiltinSkins returns all built-in skins
func BuiltinSkins() map[string]*Config {
	return map[string]*Config{
		"default": DefaultSkin,
		"mono":    MonoSkin,
		"slate":   SlateSkin,
		"cyber":   CyberSkin,
	}
}

// GetBuiltinSkin returns a built-in skin by name
func GetBuiltinSkin(name string) *Config {
	skins := BuiltinSkins()
	if skin, ok := skins[name]; ok {
		return skin
	}
	return DefaultSkin
}
