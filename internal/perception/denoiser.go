package perception

import (
	"strings"
	"unicode/utf8"
)

// Denoiser handles input noise detection and suggestion
type Denoiser struct {
	// Common typos map (can be expanded)
	commonTypos map[string]string
}

// NewDenoiser creates a new denoiser
func NewDenoiser() *Denoiser {
	return &Denoiser{
		commonTypos: map[string]string{
			"pyhton":     "python",
			"javasript":  "javascript",
			"typesript":  "typescript",
			"javscript":  "javascript",
			"golang":     "go",
			"dockerfile": "Dockerfile",
			"json file":  "JSON file",
			"csv file":   "CSV file",
			"dont":       "don't",
			"cant":       "can't",
			"wont":       "won't",
			"im":         "I'm",
			"ive":        "I've",
			"youre":      "you're",
		},
	}
}

// detectNoise analyzes input for potential issues
func (p *Parser) detectNoise(input string) NoiseDetection {
	result := NoiseDetection{
		HasNoise:    false,
		NoiseTypes:  make([]NoiseType, 0),
		Suggestions: make([]string, 0),
	}

	// Check for incomplete input
	// Note: CJK characters convey much more meaning per character than Latin,
	// so we use a lower threshold (2) for CJK-heavy input
	runeCount := utf8.RuneCountInString(input)
	minLen := 5
	if hasCJK(input) {
		minLen = 2
	}
	if runeCount < minLen && !isGreeting(input) {
		result.HasNoise = true
		result.NoiseTypes = append(result.NoiseTypes, NoiseIncomplete)
		result.Suggestions = append(result.Suggestions, "Input seems incomplete - could you provide more details?")
	}

	// Check for ambiguity (multiple question marks, vague terms)
	if strings.Count(input, "?") > 2 {
		result.HasNoise = true
		result.NoiseTypes = append(result.NoiseTypes, NoiseAmbiguous)
		result.Suggestions = append(result.Suggestions, "Multiple questions detected - focusing on the main request first")
	}

	// Check for vague task requests
	vagueTerms := []string{"something", "some stuff", "things", "a thing", "whatever"}
	lower := strings.ToLower(input)
	for _, term := range vagueTerms {
		if strings.Contains(lower, term) {
			result.HasNoise = true
			result.NoiseTypes = append(result.NoiseTypes, NoiseAmbiguous)
			result.Suggestions = append(result.Suggestions, "Request contains vague terms - trying to infer best action")
			break
		}
	}

	// Check for potential contradictions (simplified)
	if strings.Contains(lower, "not") && strings.Contains(lower, "and") {
		// This is a simplified check - actual contradiction detection needs NLP
		// result.HasNoise = true
		// result.NoiseTypes = append(result.NoiseTypes, NoiseContradicts)
	}

	return result
}

// extractContextHints extracts hints for memory retrieval and context building
func (p *Parser) extractContextHints(input string, history []string) []string {
	var hints []string
	lower := strings.ToLower(input)

	// Referencing previous messages
	if strings.Contains(lower, "previous") || strings.Contains(lower, "earlier") || strings.Contains(lower, "before") {
		hints = append(hints, "reference_previous_conversation")
	}

	// Referencing a file created earlier
	if strings.Contains(lower, "the file") || strings.Contains(lower, "that file") {
		hints = append(hints, "reference_recent_files")
	}

	// Continuing a task
	if strings.Contains(lower, "continue") || strings.Contains(lower, "next step") {
		hints = append(hints, "continue_task")
	}

	// Asking to fix or change something
	if strings.Contains(lower, "fix") || strings.Contains(lower, "change") || strings.Contains(lower, "update") {
		hints = append(hints, "review_recent_changes")
	}

	return hints
}

// isGreeting checks if input is just a greeting
func isGreeting(input string) bool {
	greetings := []string{"hi", "hello", "hey", "bye", "thanks", "thank you", "ok", "okay", "yes", "no"}
	lower := strings.ToLower(strings.Trim(input, ".,?!"))
	for _, g := range greetings {
		if lower == g {
			return true
		}
	}
	return false
}

// hasCJK returns true if the string contains CJK (Chinese/Japanese/Korean) characters
func hasCJK(s string) bool {
	for _, r := range s {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3040 && r <= 0x309F) ||
			(r >= 0x30A0 && r <= 0x30FF) || (r >= 0xAC00 && r <= 0xD7AF) {
			return true
		}
	}
	return false
}

// SuggestCorrection suggests corrections for noisy input
func (d *Denoiser) SuggestCorrection(input string) string {
	words := strings.Fields(input)
	for i, word := range words {
		lowerWord := strings.ToLower(word)
		if correction, ok := d.commonTypos[lowerWord]; ok {
			words[i] = correction
		}
	}
	return strings.Join(words, " ")
}
