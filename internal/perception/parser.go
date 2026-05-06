package perception

import (
	"strings"
	"unicode/utf8"
)

// Parser handles intent classification and entity extraction
type Parser struct {
	// Can be enhanced with ML models in the future
}

// NewParser creates a new perception parser
func NewParser() *Parser {
	return &Parser{}
}

// Parse performs full perception analysis on user input
func (p *Parser) Parse(input string, contextHistory []string) *PerceptionResult {
	result := &PerceptionResult{
		Input:    input,
		Priority: 3, // Default medium priority
	}

	// Classify intent
	result.Intent = p.classifyIntent(input, contextHistory)

	// Detect noise
	result.Noise = p.detectNoise(input)

	// Extract context hints
	result.ContextHints = p.extractContextHints(input, contextHistory)

	// Adjust priority based on intent
	if result.Intent.Type == IntentCorrection {
		result.Priority = 1 // Corrections are high priority
	} else if result.Intent.Type == IntentTask && result.Intent.Complexity == ComplexityAdvanced {
		result.Priority = 2 // Complex tasks are higher priority
	}

	return result
}

// classifyIntent determines what type of input this is
func (p *Parser) classifyIntent(input string, context []string) IntentClassification {
	lower := strings.ToLower(input)
	result := IntentClassification{
		Confidence: 0.8,
	}

	// Keyword-based classification (rule-based first, can enhance with LLM later)
	switch {
	// Task detection - verbs indicating action required
	case containsAny(lower, []string{"write", "create", "generate", "build", "implement",
		"fix", "debug", "refactor", "optimize", "deploy", "setup",
		"run", "execute", "process", "convert", "analyze", "summarize",
		"download", "install", "configure", "extract", "parse"}):
		result.Type = IntentTask
		result.Complexity = p.estimateComplexity(input)

	// Question detection
	case containsAny(lower, []string{"how", "what", "why", "when", "where", "who", "?"}):
		result.Type = IntentQuestion

	// Clarification detection
	case containsAny(lower, []string{"i mean", "actually", "to clarify", "what i meant", "let me rephrase"}):
		result.Type = IntentClarification

	// Correction detection
	case containsAny(lower, []string{"no,", "that's wrong", "incorrect", "not what i meant", "you misunderstood"}):
		result.Type = IntentCorrection

	// Feedback detection
	case containsAny(lower, []string{"good", "great", "excellent", "perfect", "nice", "bad", "terrible", "wrong"}):
		result.Type = IntentFeedback

	// Chit-chat detection - short greetings
	case utf8.RuneCountInString(input) < 15 && containsAny(lower, []string{"hi", "hello", "hey", "thanks", "bye", "ok", "okay"}):
		result.Type = IntentChitChat

	default:
		result.Type = IntentUnknown
		result.Confidence = 0.5
	}

	// Extract entities
	result.Entities = p.extractEntities(input)

	// Extract keywords
	result.Keywords = p.extractKeywords(input)

	return result
}

// estimateComplexity estimates task complexity based on keywords
func (p *Parser) estimateComplexity(input string) TaskComplexity {
	lower := strings.ToLower(input)

	// Advanced tasks keywords
	advancedKeywords := []string{
		"end to end", "full system", "entire project", "production ready",
		"multi-step", "multiple files", "integrate with", "deploy to",
		"build and test", "ci/cd", "pipeline", "architecture",
	}

	// Medium tasks keywords
	mediumKeywords := []string{
		"refactor", "optimize", "debug", "analyze", "convert",
		"create a script", "write a function", "add a feature",
	}

	if containsAny(lower, advancedKeywords) {
		return ComplexityAdvanced
	}
	if containsAny(lower, mediumKeywords) {
		return ComplexityMedium
	}
	return ComplexitySimple
}

// extractEntities extracts named entities from input
func (p *Parser) extractEntities(input string) []Entity {
	var entities []Entity
	lower := strings.ToLower(input)

	// Programming languages
	languages := []string{"python", "javascript", "typescript", "go", "golang", "rust", "java", "c++", "ruby", "php"}
	for _, lang := range languages {
		if strings.Contains(lower, lang) {
			entities = append(entities, Entity{Type: "language", Value: lang})
		}
	}

	// File extensions
	extensions := []string{".py", ".js", ".ts", ".go", ".rs", ".json", ".csv", ".txt", ".md", ".yaml", ".yml"}
	for _, ext := range extensions {
		if strings.Contains(lower, ext) {
			// Try to extract the filename (simplified)
			words := strings.Fields(input)
			for _, word := range words {
				if strings.HasSuffix(strings.ToLower(word), ext) {
					entities = append(entities, Entity{Type: "file", Value: word})
				}
			}
		}
	}

	// Tools
	tools := []string{"git", "docker", "kubernetes", "k8s", "npm", "yarn", "pip", "cargo"}
	for _, tool := range tools {
		if strings.Contains(lower, tool) {
			entities = append(entities, Entity{Type: "tool", Value: tool})
		}
	}

	return entities
}

// extractKeywords extracts important keywords
func (p *Parser) extractKeywords(input string) []string {
	// Remove common stop words
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true,
		"i": true, "you": true, "to": true, "for": true, "of": true,
		"and": true, "or": true, "but": true, "in": true, "on": true,
		"with": true, "that": true, "this": true, "please": true, "can": true,
		"could": true, "would": true, "should": true, "my": true,
	}

	var keywords []string
	words := strings.Fields(strings.ToLower(input))
	for _, word := range words {
		word = strings.Trim(word, ".,?!;:\"'()[]{}")
		if !stopWords[word] && len(word) > 2 {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// containsAny checks if string contains any of the substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// denoiser.go contains detectNoise and extractContextHints implementations
