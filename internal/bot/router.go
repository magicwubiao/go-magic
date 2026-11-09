package bot

import (
	"sort"
	"strings"
	"unicode"
)

// RouteDecision is the outcome of an automatic bot assignment.
type RouteDecision struct {
	Bot string `json:"bot"`
	// Score is the accumulated relevance score; Reason is a short
	// human-readable explanation ("title", "keyword:论文", "mention") that the
	// UI and logs can show instead of a magic number.
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// MinAutoRouteScore is the score a bot must reach before the gateway hands it
// an un-addressed message. Below it the message falls through to the general
// gateway agent: "no clear owner" is a legitimate answer, and guessing would
// silently steal messages that were never meant for a specialist bot.
const MinAutoRouteScore = 3.0

// RouteMessage picks the bot that should answer an un-addressed message.
//
// It is deliberately deterministic and local (no extra LLM call): every
// inbound gateway message is a hot path, and a routing round-trip would add
// seconds to each reply. The score uses exactly the fields a human sees when
// picking a bot in the dashboard — name/tag, title, description — with
// explicit mentions and title matches dominating weak keyword hits.
func (m *Manager) RouteMessage(text string) (*RouteDecision, bool) {
	return m.routeMessage(text, nil)
}

// RouteRoomOpeningSpeaker picks the opening speaker for a room round among the
// room's own members (auto-routing must never promote a bot that is not in the
// room, no matter how well its description matches).
func (m *Manager) RouteRoomOpeningSpeaker(room *RoomConfig, text string) (*RouteDecision, bool) {
	if room == nil {
		return nil, false
	}
	allowed := make(map[string]bool, len(room.Members))
	for _, mem := range room.Members {
		allowed[strings.ToLower(mem)] = true
	}
	return m.routeMessage(text, allowed)
}

// routeMessage scores every active bot (optionally restricted to allowed
// tags) and returns the best one when it clears MinAutoRouteScore.
func (m *Manager) routeMessage(text string, allowed map[string]bool) (*RouteDecision, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if len([]rune(lower)) < 2 {
		return nil, false
	}
	type candidate struct {
		name   string
		score  float64
		reason string
	}
	var cands []candidate
	for _, bc := range m.List() {
		if bc == nil || !bc.IsActive() {
			continue
		}
		tag := strings.ToLower(bc.MentionTag())
		if allowed != nil && !allowed[tag] && !allowed[strings.ToLower(bc.Name)] {
			continue
		}
		score, reason := routeScore(lower, bc)
		if score <= 0 {
			continue
		}
		cands = append(cands, candidate{name: strings.ToLower(bc.Name), score: score, reason: reason})
	}
	if len(cands) == 0 {
		return nil, false
	}
	// Stable tie-break by name: map iteration order must not decide who answers.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].name < cands[j].name
	})
	best := cands[0]
	if best.score < MinAutoRouteScore {
		return nil, false
	}
	return &RouteDecision{Bot: best.name, Score: best.score, Reason: best.reason}, true
}

// routeScore turns one bot's identity into a relevance score for a message.
func routeScore(lowerText string, bc *Config) (float64, string) {
	score := 0.0
	reason := ""

	tag := strings.ToLower(bc.MentionTag())
	if tag != "" && strings.Contains(lowerText, "@"+tag) {
		score += 20
		reason = "mention"
	} else if len(tag) >= 4 && containsWord(lowerText, tag) {
		// A bare name in the text is a strong hint, but only for long enough
		// tags: "qa" or "pm" appear inside ordinary English words all the time.
		score += 5
		reason = "name"
	}

	if title := strings.ToLower(strings.TrimSpace(bc.Title)); title != "" && strings.Contains(lowerText, title) {
		score += 8
		if reason == "" {
			reason = "title"
		}
	}

	hits := 0
	first := ""
	for _, kw := range routeKeywords(bc) {
		if strings.Contains(lowerText, kw) {
			hits++
			if first == "" {
				first = kw
			}
		}
	}
	if hits > 0 {
		// Cap keyword contribution so a long description cannot outvote a
		// title/mention match just by listing more synonyms.
		if hits > 6 {
			hits = 6
		}
		score += float64(hits)
		if reason == "" {
			reason = "keyword:" + first
		}
	}
	return score, reason
}

// routeKeywords extracts comparable keywords from a bot's name, title and
// description. ASCII words are matched whole-ish (>=3 chars), CJK runs are
// matched as bigrams — Chinese has no spaces, and a whole-phrase match would
// require the user to type the description verbatim.
func routeKeywords(bc *Config) []string {
	if bc == nil {
		return nil
	}
	src := strings.ToLower(strings.Join([]string{bc.Title, bc.Description}, " "))
	seen := map[string]bool{}
	var out []string
	add := func(kw string) {
		if kw == "" || seen[kw] || routeStopwords[kw] {
			return
		}
		seen[kw] = true
		out = append(out, kw)
	}

	for _, field := range strings.FieldsFunc(src, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		runes := []rune(field)
		if isCJK(runes[0]) {
			for i := 0; i+1 < len(runes); i++ {
				add(string(runes[i : i+2]))
			}
			continue
		}
		if len(runes) >= 3 {
			add(field)
		}
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// containsWord reports whether word appears in text delimited by non-word
// characters, so bot "writer" does not match "typewriter".
func containsWord(text, word string) bool {
	if word == "" {
		return false
	}
	idx := 0
	for {
		i := strings.Index(text[idx:], word)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(word)
		beforeOK := start == 0 || !isWordByte(text[start-1])
		afterOK := end == len(text) || !isWordByte(text[end])
		if beforeOK && afterOK {
			return true
		}
		idx = start + 1
		if idx >= len(text) {
			return false
		}
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// routeStopwords are words that carry no routing signal: they appear in most
// descriptions and most messages, so counting them would make every bot look
// equally relevant (and auto-routing would pick essentially at random).
var routeStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "you": true, "your": true,
	"are": true, "can": true, "will": true, "that": true, "this": true, "from": true,
	"have": true, "has": true, "all": true, "any": true, "our": true, "its": true,
	"not": true, "but": true, "use": true, "using": true, "about": true, "into": true,
	"over": true, "when": true, "what": true, "who": true, "how": true, "bot": true,
	"agent": true, "assistant": true, "help": true, "helps": true, "task": true,
	"tasks": true, "work": true, "works": true, "good": true, "also": true,
	"more": true, "than": true, "them": true, "they": true, "their": true,
	"助手": true, "负责": true, "可以": true, "一个": true, "以及": true, "进行": true,
	"相关": true, "对于": true, "需要": true, "我们": true, "你的": true, "他们": true,
	"工作": true, "任务": true, "内容": true, "支持": true, "使用": true, "通过": true,
}
