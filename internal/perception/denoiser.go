package perception

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// inlineCodeRe 匹配 Markdown 行内代码 `xxx`；urlRe 匹配裸 URL。
// 二者内部常含 `?`（正则、查询串），不应参与"用户在提问"的判定。
var (
	inlineCodeRe = regexp.MustCompile("`[^`]*`")
	urlRe        = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
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

// detectNoise analyzes input for potential issues.
//
// 设计偏保守：这里的每一条命中都会把输入的 ambiguity 标志抬起来，而
// cognition 层会据此设置 ClarificationNeeded，最终表现为"模型反问用户"。
// 假阳性代价（简单任务被反复打断）远高于假阴性代价（模型按最合理解释
// 直接做），所以阈值一律收紧，宁可放过也不误报。
func (p *Parser) detectNoise(input string) NoiseDetection {
	result := NoiseDetection{
		HasNoise:    false,
		NoiseTypes:  make([]NoiseType, 0),
		Suggestions: make([]string, 0),
	}

	// Check for incomplete input
	// Note: CJK characters convey much more meaning per character than Latin,
	// so we use a lower threshold (2) for CJK-heavy input.
	// 但只有"词"才是有效信号：单个字符/纯标点（如 "?" 或 "~~"）不算缺信息，
	// 否则用户随手发个符号就被判定为不完整。
	runeCount := utf8.RuneCountInString(strings.TrimSpace(input))
	minLen := 5
	if hasCJK(input) {
		minLen = 2
	}
	if runeCount < minLen && runeCount > 0 && !isGreeting(input) && !isOnlyPunctuation(input) {
		result.HasNoise = true
		result.NoiseTypes = append(result.NoiseTypes, NoiseIncomplete)
		result.Suggestions = append(result.Suggestions, "Input seems incomplete - could you provide more details?")
	}

	// Check for ambiguity: 只有当问号 **数量** 表明用户真的并列抛了多个
	// 问题时才算歧义。中文用户习惯写 "怎么改？能顺便加个提示吗？" 这类
	// 多问句，旧阈值 (>2) 命中率过高且信息量为零——真正要判断的是
	// "并列的独立诉求有几个"，那是模型该做的事，不该在这里用字符计数
	// 硬编码。这里抬到 4 并且排除代码块/URL 里的问号。
	if countRealQuestions(input) > 3 {
		result.HasNoise = true
		result.NoiseTypes = append(result.NoiseTypes, NoiseAmbiguous)
		result.Suggestions = append(result.Suggestions, "Multiple questions detected - focusing on the main request first")
	}

	// Check for vague task requests.
	// 仅匹配"整词"，且忽略英文词边界误伤（如 "notebook" 里的 "not" 无关）。
	// value 不是 being 一个"任务描述里出现 things 就歧义"——用户在
	// "整理下这些东西" 里其实是明确的任务，缺的是范围而非意图。
	vaguePhrases := []string{
		"some stuff", "a bunch of stuff", "whatever you want",
		"do something", "make something", "fix something",
	}
	lower := strings.ToLower(input)
	for _, term := range vaguePhrases {
		if strings.Contains(lower, term) {
			result.HasNoise = true
			result.NoiseTypes = append(result.NoiseTypes, NoiseAmbiguous)
			result.Suggestions = append(result.Suggestions, "Request contains vague terms - trying to infer best action")
			break
		}
	}

	return result
}

// countRealQuestions 统计输入中"真实的"问号数量，跳过代码块与 URL。
// 代码里的 `?`（正则、三元、查询串）不代表用户在提问。
func countRealQuestions(input string) int {
	cleaned := stripCodeAndURLs(input)
	return strings.Count(cleaned, "?") + strings.Count(cleaned, "？")
}

// stripCodeAndURLs 移除围栏代码块、行内代码与 URL，避免其中的标点被当作
// 自然语言信号。
func stripCodeAndURLs(input string) string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(input, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	out := b.String()
	out = inlineCodeRe.ReplaceAllString(out, " ")
	out = urlRe.ReplaceAllString(out, " ")
	return out
}

// isOnlyPunctuation 判断输入是否只由空白与标点构成（无任何实际内容）。
func isOnlyPunctuation(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
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
