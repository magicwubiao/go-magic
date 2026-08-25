package bot

import (
	"strings"
)

// ParseBotMention extracts a bot routing directive from message text.
//
// Supported forms:
//   - "/bot <name> <text>"   (slash form, works on all platforms)
//   - "@<tag> <text>"        (mention form; tag must match a known bot)
//
// Returns (botNameOrTag, remainingText, matched). The slash form always
// matches when a name is present; the mention form only matches known tags
// to avoid hijacking ordinary messages that start with @.
func ParseBotMention(content string, resolve func(tag string) bool) (string, string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", false
	}

	// Slash form: /bot <name> <text>
	lower := strings.ToLower(content)
	for _, prefix := range []string{"/bot ", "/bots "} {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(content[len(prefix):])
			if rest == "" {
				return "", "", false
			}
			name, text, _ := strings.Cut(rest, " ")
			return strings.TrimPrefix(name, "@"), strings.TrimSpace(text), true
		}
	}

	// Mention form: @<tag> <text>
	if strings.HasPrefix(content, "@") {
		tag, text, found := strings.Cut(content[1:], " ")
		tag = strings.ToLower(strings.TrimSpace(tag))
		if found && tag != "" && tag != "user" && resolve != nil && resolve(tag) {
			return tag, strings.TrimSpace(text), true
		}
	}

	return "", "", false
}
