package perception

import (
	"strings"
	"testing"
)

// TestParseIntent tests the 7 intent classifications
func TestParseIntent(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name     string
		input    string
		expected IntentType
	}{
		// IntentTask - action verbs
		{"task_write", "write a Python script", IntentTask},
		{"task_create", "create a new file", IntentTask},
		{"task_generate", "generate a report", IntentTask},
		{"task_build", "build the project", IntentTask},
		{"task_fix", "fix the bug", IntentTask},
		{"task_debug", "debug the issue", IntentTask},
		{"task_refactor", "refactor the code", IntentTask},
		{"task_optimize", "optimize performance", IntentTask},
		{"task_deploy", "deploy to production", IntentTask},
		{"task_run", "run the tests", IntentTask},
		{"task_execute", "execute the command", IntentTask},
		{"task_process", "process the data", IntentTask},
		{"task_convert", "convert to JSON", IntentTask},
		{"task_analyze", "analyze the logs", IntentTask},
		{"task_summarize", "summarize the document", IntentTask},
		{"task_download", "download the file", IntentTask},
		{"task_install", "install the package", IntentTask},
		{"task_configure", "configure the settings", IntentTask},
		{"task_extract", "extract the text", IntentTask},
		{"task_parse", "parse the CSV", IntentTask},

		// IntentQuestion - question words
		{"question_how", "how do I install Python?", IntentQuestion},
		{"question_what", "what is the config file?", IntentQuestion},
		{"question_why", "why did it fail?", IntentQuestion},
		{"question_when", "when was this created?", IntentQuestion},
		{"question_where", "where is the log file?", IntentQuestion},
		{"question_who", "who wrote this code?", IntentQuestion},

		// IntentClarification - clarification phrases
		{"clarify_i_mean", "I mean the second option", IntentClarification},
		{"clarify_actually", "actually, I wanted the first one", IntentClarification},
		{"clarify_rephrase", "let me rephrase my question", IntentClarification},

		// IntentCorrection - correction phrases
		{"correction_no", "no, that's not right", IntentCorrection},
		{"correction_wrong", "that's wrong", IntentCorrection},
		{"correction_incorrect", "this is incorrect", IntentCorrection},
		{"correction_misunderstood", "you misunderstood me", IntentCorrection},

		// IntentFeedback - feedback words
		{"feedback_good", "good job!", IntentFeedback},
		{"feedback_great", "great work!", IntentFeedback},
		{"feedback_excellent", "excellent!", IntentFeedback},
		{"feedback_bad", "bad result", IntentFeedback},

		// IntentChitChat - short greetings
		{"chitchat_hi", "hi", IntentChitChat},
		{"chitchat_hello", "hello", IntentChitChat},
		{"chitchat_thanks", "thanks", IntentChitChat},
		{"chitchat_bye", "bye", IntentChitChat},
		{"chitchat_ok", "ok", IntentChitChat},

		// IntentUnknown - unclassified
		{"unknown_generic", "some random text", IntentUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input, nil)
			if result.Intent.Type != tt.expected {
				t.Errorf("input %q: expected %s, got %s", tt.input, tt.expected, result.Intent.Type)
			}
		})
	}
}

// TestParseComplexity tests the 3-level complexity evaluation
func TestParseComplexity(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name     string
		input    string
		expected TaskComplexity
	}{
		// Simple tasks - no complexity keywords
		{"simple_basic", "write a file", ComplexitySimple},
		{"simple_single", "run ls", ComplexitySimple},
		{"simple_read", "read the config", ComplexitySimple},

		// Medium tasks - medium complexity keywords
		{"medium_refactor", "refactor the function", ComplexityMedium},
		{"medium_optimize", "optimize the query", ComplexityMedium},
		{"medium_debug", "debug the error", ComplexityMedium},
		{"medium_analyze", "analyze the data", ComplexityMedium},
		{"medium_convert", "convert to CSV", ComplexityMedium},
		{"medium_script", "create a script", ComplexityMedium},
		{"medium_function", "write a function", ComplexityMedium},
		{"medium_feature", "add a feature", ComplexityMedium},

		// Advanced tasks - advanced complexity keywords
		{"advanced_endtoend", "end to end integration", ComplexityAdvanced},
		{"advanced_fullsystem", "full system architecture", ComplexityAdvanced},
		{"advanced_production", "production ready deployment", ComplexityAdvanced},
		{"advanced_multistep", "multi-step workflow", ComplexityAdvanced},
		{"advanced_multiplefiles", "multiple files", ComplexityAdvanced},
		{"advanced_integrate", "integrate with external API", ComplexityAdvanced},
		{"advanced_deploy", "deploy to kubernetes", ComplexityAdvanced},
		{"advanced_cicd", "setup CI/CD pipeline", ComplexityAdvanced},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input, nil)
			if result.Intent.Complexity != tt.expected {
				t.Errorf("input %q: expected %s, got %s", tt.input, tt.expected, result.Intent.Complexity)
			}
		})
	}
}

// TestExtractEntities tests entity extraction
func TestExtractEntities(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name          string
		input         string
		wantLanguages  int
		wantFiles     int
		wantTools     int
		wantEntityCnt int
	}{
		{"python_script", "write a Python script", 1, 0, 0, 1},
		{"javascript_file", "read the app.js file", 1, 1, 0, 2},
		{"golang_with_git", "use git to clone the Go project", 1, 0, 1, 2},
		{"docker_config", "deploy with Docker and docker-compose", 0, 1, 1, 2},
		{"multi_language", "convert Python to TypeScript and run npm", 2, 1, 1, 4},
		{"config_files", "parse config.json and settings.yaml", 0, 2, 0, 2},
		{"all_tools", "use git, docker, npm and pip", 0, 0, 4, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input, nil)
			entities := result.Intent.Entities

			// Count by type
			langCount := 0
			fileCount := 0
			toolCount := 0

			for _, e := range entities {
				switch e.Type {
				case "language":
					langCount++
				case "file":
					fileCount++
				case "tool":
					toolCount++
				}
			}

			if langCount != tt.wantLanguages {
				t.Errorf("languages: expected %d, got %d", tt.wantLanguages, langCount)
			}
			if fileCount != tt.wantFiles {
				t.Errorf("files: expected %d, got %d", tt.wantFiles, fileCount)
			}
			if toolCount != tt.wantTools {
				t.Errorf("tools: expected %d, got %d", tt.wantTools, toolCount)
			}
			if len(entities) != tt.wantEntityCnt {
				t.Errorf("total entities: expected %d, got %d", tt.wantEntityCnt, len(entities))
			}
		})
	}
}

// TestDenoiser tests noise detection and cleanup
func TestDenoiser(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name            string
		input           string
		wantHasNoise    bool
		wantNoiseTypes  []NoiseType
	}{
		{"clean_input", "write a Python script to process data", false, nil},
		{"excessive_punct", "What!!! Is this!!! Working???", true, []NoiseType{NoiseTypo}},
		{"too_short", "ab", true, []NoiseType{NoiseIncomplete}},
		{"short_question", "why?", false, nil}, // questions are valid
		{"contradiction_yesno", "yes I will, no I won't", true, []NoiseType{NoiseContradicts}},
		{"contradiction_do", "do it, don't do it", true, []NoiseType{NoiseContradicts}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input, nil)

			if result.Noise.HasNoise != tt.wantHasNoise {
				t.Errorf("HasNoise: expected %v, got %v", tt.wantHasNoise, result.Noise.HasNoise)
			}

			if tt.wantHasNoise {
				if len(result.Noise.NoiseTypes) == 0 && len(tt.wantNoiseTypes) > 0 {
					t.Errorf("expected noise types %v, got none", tt.wantNoiseTypes)
				}

				// Check at least one of the expected noise types is present
				found := false
				for _, want := range tt.wantNoiseTypes {
					for _, got := range result.Noise.NoiseTypes {
						if want == got {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found && len(tt.wantNoiseTypes) > 0 {
					t.Errorf("expected noise types %v, got %v", tt.wantNoiseTypes, result.Noise.NoiseTypes)
				}
			}
		})
	}
}

// TestExtractKeywords tests keyword extraction
func TestExtractKeywords(t *testing.T) {
	parser := NewParser()

	result := parser.Parse("write a Python script to parse CSV files and analyze data", nil)
	keywords := result.Intent.Keywords

	// Should not contain stop words
	for _, kw := range keywords {
		lower := strings.ToLower(kw)
		if lower == "a" || lower == "to" || lower == "the" || lower == "and" {
			t.Errorf("stop word %q should not be in keywords", kw)
		}
	}

	// Should contain important words
	expected := map[string]bool{"write": true, "python": true, "script": true, "parse": true, "csv": true, "files": true, "analyze": true, "data": true}
	for _, kw := range keywords {
		delete(expected, kw)
	}
	if len(expected) > 0 {
		for k := range expected {
			t.Errorf("missing expected keyword: %s", k)
		}
	}
}

// TestContextHints tests context hint extraction
func TestContextHints(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name           string
		input          string
		history        []string
		wantMinHints   int
		wantHasRef     bool
		wantHasTime    bool
	}{
		{
			name:         "pronoun_reference",
			input:        "fix it now",
			history:      []string{"there is a bug in the code"},
			wantMinHints: 2,
			wantHasRef:   true,
			wantHasTime:  true,
		},
		{
			name:         "continuation",
			input:        "continue with the deployment",
			history:      nil,
			wantMinHints: 1,
			wantHasRef:   false,
			wantHasTime:  true,
		},
		{
			name:         "no_context",
			input:        "write a new file",
			history:      nil,
			wantMinHints: 0,
			wantHasRef:   false,
			wantHasTime:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input, tt.history)
			hints := result.ContextHints

			if len(hints) < tt.wantMinHints {
				t.Errorf("expected at least %d hints, got %d: %v", tt.wantMinHints, len(hints), hints)
			}

			if tt.wantHasRef {
				found := false
				for _, h := range hints {
					if strings.HasPrefix(h, "reference:") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected reference hint, got %v", hints)
				}
			}

			if tt.wantHasTime {
				found := false
				for _, h := range hints {
					if strings.HasPrefix(h, "time:") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected time hint, got %v", hints)
				}
			}
		})
	}
}

// TestPriorityAdjustment tests priority adjustment based on intent
func TestPriorityAdjustment(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"correction_high", "no, that's wrong", 1},                    // Corrections = priority 1
		{"complex_task_high", "build a full system architecture", 2}, // Complex tasks = priority 2
		{"normal_task", "write a file", 3},                           // Normal = priority 3
		{"simple_question", "what is this?", 3},                      // Questions = priority 3
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input, nil)
			if result.Priority != tt.expected {
				t.Errorf("expected priority %d, got %d", tt.expected, result.Priority)
			}
		})
	}
}

// TestConfidenceLevels tests intent confidence scoring
func TestConfidenceLevels(t *testing.T) {
	parser := NewParser()

	// Known intents should have higher confidence
	knownResult := parser.Parse("write a Python script", nil)
	if knownResult.Intent.Confidence < 0.7 {
		t.Errorf("known intent should have confidence >= 0.7, got %f", knownResult.Intent.Confidence)
	}

	// Unknown intents should have lower confidence
	unknownResult := parser.Parse("xyzabc123", nil)
	if unknownResult.Intent.Confidence > 0.6 {
		t.Errorf("unknown intent should have confidence < 0.6, got %f", unknownResult.Intent.Confidence)
	}
}

// BenchmarkParse benchmarks the Parse function
func BenchmarkParse(b *testing.B) {
	parser := NewParser()
	input := "write a Python script to parse CSV files and analyze the data"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser.Parse(input, nil)
	}
}

// BenchmarkParseWithHistory benchmarks Parse with context history
func BenchmarkParseWithHistory(b *testing.B) {
	parser := NewParser()
	input := "continue with the deployment"
	history := []string{
		"build the Docker image",
		"run the tests",
		"deploy to staging",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser.Parse(input, history)
	}
}
