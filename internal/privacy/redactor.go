package privacy

import (
	"regexp"
	"strings"
	"sync"
)

// Pattern represents a PII pattern
type Pattern struct {
	Name        string
	Regex       *regexp.Regexp
	Replacement string
}

// Redactor handles PII detection and redaction
type Redactor struct {
	patterns []Pattern
	mu       sync.RWMutex
	enabled  bool
	auditLog []AuditEntry
	auditMu  sync.Mutex
}

// AuditEntry represents a redaction audit log entry
type AuditEntry struct {
	Original  string   `json:"original"`
	Detected  []string `json:"detected"` // Types of PII found
	Redacted  string   `json:"redacted"`
	Timestamp string   `json:"timestamp"`
}

// Config holds privacy configuration
type Config struct {
	Enabled        bool              `json:"enabled"`
	RedactPhone    bool              `json:"redact_phone"`
	RedactEmail    bool              `json:"redact_email"`
	RedactIDCard   bool              `json:"redact_id_card"`
	RedactBankCard bool              `json:"redact_bank_card"`
	RedactIP       bool              `json:"redact_ip"`
	RedactAddress  bool              `json:"redact_address"`
	CustomPatterns map[string]string `json:"custom_patterns"` // Name -> Regex
}

// DefaultConfig returns default privacy configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:        true,
		RedactPhone:    true,
		RedactEmail:    true,
		RedactIDCard:   true,
		RedactBankCard: true,
		RedactIP:       true,
		RedactAddress:  true,
		CustomPatterns: make(map[string]string),
	}
}

// DefaultRedactor creates a redactor with default patterns
func DefaultRedactor() *Redactor {
	cfg := DefaultConfig()
	return NewRedactor(cfg)
}

// NewRedactor creates a new redactor with the given configuration
func NewRedactor(cfg *Config) *Redactor {
	r := &Redactor{
		enabled:  cfg.Enabled,
		patterns: make([]Pattern, 0),
		auditLog: make([]AuditEntry, 0),
	}

	// Add default patterns based on config
	if cfg.RedactPhone {
		r.AddPattern("PHONE", `1[3-9]\d{9}`, "[PHONE]")
	}

	if cfg.RedactEmail {
		r.AddPattern("EMAIL", `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`, "[EMAIL]")
	}

	if cfg.RedactIDCard {
		// Chinese ID card (18 digits)
		r.AddPattern("ID_CARD", `[1-9]\d{5}(?:19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]`, "[ID_CARD]")
		// US SSN
		r.AddPattern("SSN", `\d{3}-\d{2}-\d{4}`, "[SSN]")
	}

	if cfg.RedactBankCard {
		// Credit card numbers (13-19 digits)
		r.AddPattern("BANK_CARD", `\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`, "[BANK_CARD]")
		// Generic bank card
		r.AddPattern("BANK_CARD_GENERIC", `\b\d{13,19}\b`, "[BANK_CARD]")
	}

	if cfg.RedactIP {
		// IPv4
		r.AddPattern("IP_V4", `\b(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`, "[IP]")
		// IPv6
		r.AddPattern("IP_V6", `(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}`, "[IP]")
	}

	if cfg.RedactAddress {
		// Chinese address patterns
		r.AddPattern("ADDRESS_CN", `(?:省|市|区|县|街|路|号|栋|单元|室|[0-9]+号)`, "[ADDRESS]")
	}

	// Add custom patterns
	for name, regex := range cfg.CustomPatterns {
		r.AddPattern(name, regex, "["+strings.ToUpper(name)+"]")
	}

	return r
}

// AddPattern adds a custom PII pattern
func (r *Redactor) AddPattern(name, regex, replacement string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	compiled, err := regexp.Compile(regex)
	if err != nil {
		return // Skip invalid regex
	}

	r.patterns = append(r.patterns, Pattern{
		Name:        name,
		Regex:       compiled,
		Replacement: replacement,
	})
}

// Redact replaces all PII in the input text
func (r *Redactor) Redact(text string) string {
	if !r.enabled {
		return text
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := text
	var detected []string

	for _, pattern := range r.patterns {
		if pattern.Regex.MatchString(result) {
			if !contains(detected, pattern.Name) {
				detected = append(detected, pattern.Name)
			}
			result = pattern.Regex.ReplaceAllString(result, pattern.Replacement)
		}
	}

	// Log audit entry if PII was detected
	if len(detected) > 0 {
		r.logAudit(text, detected, result)
	}

	return result
}

// RedactWithContext redacts PII and returns detected types
func (r *Redactor) RedactWithContext(text string) (string, []string) {
	if !r.enabled {
		return text, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := text
	var detected []string

	for _, pattern := range r.patterns {
		if pattern.Regex.MatchString(result) {
			if !contains(detected, pattern.Name) {
				detected = append(detected, pattern.Name)
			}
			result = pattern.Regex.ReplaceAllString(result, pattern.Replacement)
		}
	}

	if len(detected) > 0 {
		r.logAudit(text, detected, result)
	}

	return result, detected
}

// Detect finds all PII in the text without replacing
func (r *Redactor) Detect(text string) []Match {
	if !r.enabled {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []Match

	for _, pattern := range r.patterns {
		found := pattern.Regex.FindAllStringIndex(text, -1)
		for _, loc := range found {
			matches = append(matches, Match{
				Type:  pattern.Name,
				Start: loc[0],
				End:   loc[1],
				Value: text[loc[0]:loc[1]],
			})
		}
	}

	return matches
}

// Match represents a detected PII match
type Match struct {
	Type  string
	Start int
	End   int
	Value string
}

// Restore reverses redaction (for local processing only)
func (r *Redactor) Restore(text string, mapping map[string]string) string {
	result := text

	// Replace placeholders with original values
	for placeholder, original := range mapping {
		result = strings.ReplaceAll(result, placeholder, original)
	}

	return result
}

// Enable enables PII redaction
func (r *Redactor) Enable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = true
}

// Disable disables PII redaction
func (r *Redactor) Disable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = false
}

// IsEnabled returns whether redaction is enabled
func (r *Redactor) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enabled
}

// GetAuditLog returns the audit log
func (r *Redactor) GetAuditLog() []AuditEntry {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()

	log := make([]AuditEntry, len(r.auditLog))
	copy(log, r.auditLog)
	return log
}

// ClearAuditLog clears the audit log
func (r *Redactor) ClearAuditLog() {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	r.auditLog = make([]AuditEntry, 0)
}

// logAudit logs a redaction event
func (r *Redactor) logAudit(original string, detected []string, redacted string) {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()

	r.auditLog = append(r.auditLog, AuditEntry{
		Original: truncate(original, 100),
		Detected: detected,
		Redacted: truncate(redacted, 100),
	})

	// Keep only last 1000 entries
	if len(r.auditLog) > 1000 {
		r.auditLog = r.auditLog[len(r.auditLog)-1000:]
	}
}

// GetPatterns returns all configured patterns
func (r *Redactor) GetPatterns() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, len(r.patterns))
	for i, p := range r.patterns {
		names[i] = p.Name
	}
	return names
}

// CountPII returns the count of PII items by type
func (r *Redactor) CountPII(text string) map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := make(map[string]int)

	for _, pattern := range r.patterns {
		count := len(pattern.Regex.FindAllStringIndex(text, -1))
		if count > 0 {
			counts[pattern.Name] = count
		}
	}

	return counts
}

// RedactStruct recursively redacts all string fields in a struct
func (r *Redactor) RedactStruct(v interface{}) interface{} {
	if !r.enabled {
		return v
	}

	switch val := v.(type) {
	case string:
		return r.Redact(val)
	case *string:
		if val != nil {
			redacted := r.Redact(*val)
			return &redacted
		}
		return val
	case []string:
		result := make([]string, len(val))
		for i, s := range val {
			result[i] = r.Redact(s)
		}
		return result
	case map[string]string:
		result := make(map[string]string)
		for k, v := range val {
			result[k] = r.Redact(v)
		}
		return result
	default:
		return v
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ValidateIDCard validates Chinese ID card number
func ValidateIDCard(id string) bool {
	if len(id) != 18 {
		return false
	}

	// Check format
	matched, _ := regexp.MatchString(`^[1-9]\d{5}(?:19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]$`, id)
	if !matched {
		return false
	}

	// Verify checksum
	weights := []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	checksum := "10X98765432"

	sum := 0
	for i := 0; i < 17; i++ {
		digit := int(id[i] - '0')
		sum += digit * weights[i]
	}

	remainder := sum % 11
	expectedCheck := checksum[remainder]
	actualCheck := strings.ToUpper(string(id[17]))

	return string(expectedCheck) == actualCheck
}

// ValidatePhone validates Chinese mobile phone number
func ValidatePhone(phone string) bool {
	matched, _ := regexp.MatchString(`^1[3-9]\d{9}$`, phone)
	return matched
}

// ValidateEmail validates email address
func ValidateEmail(email string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`, email)
	return matched
}
