package privacy

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("Expected Enabled to be true")
	}

	if !cfg.RedactPhone {
		t.Error("Expected RedactPhone to be true")
	}

	if !cfg.RedactEmail {
		t.Error("Expected RedactEmail to be true")
	}

	if !cfg.RedactIDCard {
		t.Error("Expected RedactIDCard to be true")
	}

	if !cfg.RedactBankCard {
		t.Error("Expected RedactBankCard to be true")
	}

	if !cfg.RedactIP {
		t.Error("Expected RedactIP to be true")
	}

	if !cfg.RedactAddress {
		t.Error("Expected RedactAddress to be true")
	}

	if cfg.CustomPatterns == nil {
		t.Error("Expected CustomPatterns to be initialized")
	}
}

func TestDefaultRedactor(t *testing.T) {
	r := DefaultRedactor()

	if !r.IsEnabled() {
		t.Error("Expected redactor to be enabled")
	}

	patterns := r.GetPatterns()
	if len(patterns) == 0 {
		t.Error("Expected at least one pattern")
	}
}

func TestNewRedactor(t *testing.T) {
	cfg := &Config{
		Enabled:      true,
		RedactPhone:  true,
		RedactEmail:  false,
		RedactIDCard: false,
	}

	r := NewRedactor(cfg)
	patterns := r.GetPatterns()

	// Should only have phone pattern
	if len(patterns) != 1 {
		t.Errorf("Expected 1 pattern, got %d", len(patterns))
	}

	if !contains(patterns, "PHONE") {
		t.Error("Expected PHONE pattern")
	}
}

func TestRedactPhone(t *testing.T) {
	r := DefaultRedactor()

	tests := []struct {
		input    string
		expected string
	}{
		{"My phone is 13812345678", "My phone is [PHONE]"},
		{"Call: 19912345678", "Call: [PHONE]"},
		{"Multiple: 13812345678 and 15912345678", "Multiple: [PHONE] and [PHONE]"},
		{"No phone here", "No phone here"},
	}

	for _, tc := range tests {
		result := r.Redact(tc.input)
		if result != tc.expected {
			t.Errorf("Redact(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestRedactEmail(t *testing.T) {
	r := DefaultRedactor()

	tests := []struct {
		input    string
		expected string
	}{
		{"Email: test@example.com", "Email: [EMAIL]"},
		{"Contact: user.name@domain.co.uk", "Contact: [EMAIL]"},
		{"Multiple: a@b.com and c@d.org", "Multiple: [EMAIL] and [EMAIL]"},
		{"No email here", "No email here"},
	}

	for _, tc := range tests {
		result := r.Redact(tc.input)
		if result != tc.expected {
			t.Errorf("Redact(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestRedactChineseIDCard(t *testing.T) {
	r := DefaultRedactor()

	// Valid Chinese ID card
	result := r.Redact("My ID is 110101199001011234")
	expected := "My ID is [ID_CARD]"
	if result != expected {
		t.Errorf("Redact() = %q, want %q", result, expected)
	}
}

func TestRedactSSN(t *testing.T) {
	r := DefaultRedactor()

	result := r.Redact("SSN: 123-45-6789")
	expected := "SSN: [SSN]"
	if result != expected {
		t.Errorf("Redact() = %q, want %q", result, expected)
	}
}

func TestRedactBankCard(t *testing.T) {
	r := DefaultRedactor()

	tests := []struct {
		input    string
		expected string
	}{
		{"Card: 4111111111111111", "Card: [BANK_CARD]"},
		{"Visa: 4111111111111111", "Visa: [BANK_CARD]"},
		{"MasterCard: 5500000000000004", "MasterCard: [BANK_CARD]"},
	}

	for _, tc := range tests {
		result := r.Redact(tc.input)
		if result != tc.expected {
			t.Errorf("Redact(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestRedactIP(t *testing.T) {
	r := DefaultRedactor()

	tests := []struct {
		input    string
		expected string
	}{
		{"Server IP: 192.168.1.1", "Server IP: [IP]"},
		{"Localhost: 127.0.0.1", "Localhost: [IP]"},
		{"Public: 8.8.8.8", "Public: [IP]"},
	}

	for _, tc := range tests {
		result := r.Redact(tc.input)
		if result != tc.expected {
			t.Errorf("Redact(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestRedactDisabled(t *testing.T) {
	r := DefaultRedactor()
	r.Disable()

	input := "Phone: 13812345678"
	result := r.Redact(input)

	if result != input {
		t.Errorf("Redact() should return original when disabled, got %q", result)
	}
}

func TestEnableDisable(t *testing.T) {
	r := DefaultRedactor()

	r.Disable()
	if r.IsEnabled() {
		t.Error("Expected IsEnabled() to be false after Disable()")
	}

	r.Enable()
	if !r.IsEnabled() {
		t.Error("Expected IsEnabled() to be true after Enable()")
	}
}

func TestRedactWithContext(t *testing.T) {
	r := DefaultRedactor()

	input := "Phone: 13812345678, Email: test@example.com"
	result, detected := r.RedactWithContext(input)

	expected := "Phone: [PHONE], Email: [EMAIL]"
	if result != expected {
		t.Errorf("Result = %q, want %q", result, expected)
	}

	if !contains(detected, "PHONE") {
		t.Error("Expected PHONE in detected")
	}

	if !contains(detected, "EMAIL") {
		t.Error("Expected EMAIL in detected")
	}
}

func TestDetect(t *testing.T) {
	r := DefaultRedactor()

	input := "Phone: 13812345678, Email: test@example.com"
	matches := r.Detect(input)

	if len(matches) != 2 {
		t.Errorf("Expected 2 matches, got %d", len(matches))
	}

	foundPhone := false
	foundEmail := false
	for _, m := range matches {
		if m.Type == "PHONE" {
			foundPhone = true
			if m.Value != "13812345678" {
				t.Errorf("Phone value = %q, want %q", m.Value, "13812345678")
			}
		}
		if m.Type == "EMAIL" {
			foundEmail = true
			if m.Value != "test@example.com" {
				t.Errorf("Email value = %q, want %q", m.Value, "test@example.com")
			}
		}
	}

	if !foundPhone {
		t.Error("Expected to find PHONE match")
	}

	if !foundEmail {
		t.Error("Expected to find EMAIL match")
	}
}

func TestCountPII(t *testing.T) {
	r := DefaultRedactor()

	input := "Phone: 13812345678, Email: a@b.com, Phone: 19912345678"
	counts := r.CountPII(input)

	if counts["PHONE"] != 2 {
		t.Errorf("PHONE count = %d, want 2", counts["PHONE"])
	}

	if counts["EMAIL"] != 1 {
		t.Errorf("EMAIL count = %d, want 1", counts["EMAIL"])
	}
}

func TestAddPattern(t *testing.T) {
	r := DefaultRedactor()

	patternsBefore := len(r.GetPatterns())

	r.AddPattern("CUSTOM", `\bCUSTOM\d+\b`, "[CUSTOM]")
	patternsAfter := r.GetPatterns()

	if len(patternsAfter) != patternsBefore+1 {
		t.Error("Expected pattern to be added")
	}

	result := r.Redact("CUSTOM123")
	if result != "[CUSTOM]" {
		t.Errorf("Redact() = %q, want %q", result, "[CUSTOM]")
	}
}

func TestAddInvalidPattern(t *testing.T) {
	r := DefaultRedactor()
	patternsBefore := len(r.GetPatterns())

	r.AddPattern("INVALID", "[", "[INVALID]") // Invalid regex
	patternsAfter := len(r.GetPatterns())

	if patternsAfter != patternsBefore {
		t.Error("Invalid pattern should not be added")
	}
}

func TestAuditLog(t *testing.T) {
	r := DefaultRedactor()
	r.ClearAuditLog()

	r.Redact("Phone: 13812345678")

	log := r.GetAuditLog()
	if len(log) != 1 {
		t.Errorf("Expected 1 audit log entry, got %d", len(log))
	}

	if len(log[0].Detected) != 1 || log[0].Detected[0] != "PHONE" {
		t.Error("Audit log should contain PHONE detection")
	}

	r.ClearAuditLog()
	log = r.GetAuditLog()
	if len(log) != 0 {
		t.Error("Expected empty audit log after clear")
	}
}

func TestRedactStruct(t *testing.T) {
	r := DefaultRedactor()

	// Test string
	result := r.RedactStruct("Phone: 13812345678")
	if result != "Phone: [PHONE]" {
		t.Errorf("RedactStruct(string) = %q", result)
	}

	// Test *string
	original := "Email: test@example.com"
	ptr := &original
	resultPtr := r.RedactStruct(ptr).(*string)
	if *resultPtr != "Email: [EMAIL]" {
		t.Errorf("RedactStruct(*string) = %q", *resultPtr)
	}

	// Test []string
	slice := []string{"Phone: 13812345678", "Email: test@example.com"}
	resultSlice := r.RedactStruct(slice).([]string)
	if len(resultSlice) != 2 {
		t.Errorf("Expected 2 elements, got %d", len(resultSlice))
	}

	// Test map[string]string
	m := map[string]string{
		"phone": "13812345678",
		"email": "test@example.com",
	}
	resultMap := r.RedactStruct(m).(map[string]string)
	if resultMap["phone"] != "[PHONE]" {
		t.Errorf("Map phone = %q", resultMap["phone"])
	}
}

func TestRestore(t *testing.T) {
	r := DefaultRedactor()

	mapping := map[string]string{
		"[PHONE]": "13812345678",
		"[EMAIL]": "test@example.com",
	}

	result := r.Restore("Phone: [PHONE], Email: [EMAIL]", mapping)
	expected := "Phone: 13812345678, Email: test@example.com"

	if result != expected {
		t.Errorf("Restore() = %q, want %q", result, expected)
	}
}

func TestValidateChineseIDCard(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{"110101199001011234", true},
		{"11010119900101123X", true},  // X as checksum
		{"11010119900101123x", true},  // lowercase x
		{"110101199001011235", false}, // invalid checksum
		{"123456789012345678", false}, // invalid format
		{"1234567890", false},         // too short
	}

	for _, tc := range tests {
		result := ValidateIDCard(tc.id)
		if result != tc.valid {
			t.Errorf("ValidateIDCard(%q) = %v, want %v", tc.id, result, tc.valid)
		}
	}
}

func TestValidatePhone(t *testing.T) {
	tests := []struct {
		phone string
		valid bool
	}{
		{"13812345678", true},
		{"19912345678", true},
		{"1591234567", false},   // too short
		{"138123456789", false}, // too long
		{"12345678901", false},  // wrong prefix
	}

	for _, tc := range tests {
		result := ValidatePhone(tc.phone)
		if result != tc.valid {
			t.Errorf("ValidatePhone(%q) = %v, want %v", tc.phone, result, tc.valid)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"test@example.com", true},
		{"user.name@domain.co.uk", true},
		{"invalid-email", false},
		{"@invalid.com", false},
		{"invalid@", false},
	}

	for _, tc := range tests {
		result := ValidateEmail(tc.email)
		if result != tc.valid {
			t.Errorf("ValidateEmail(%q) = %v, want %v", tc.email, result, tc.valid)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is a ..."},
	}

	for _, tc := range tests {
		result := truncate(tc.s, tc.max)
		if result != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.max, result, tc.want)
		}
	}
}

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}

	if !contains(slice, "b") {
		t.Error("Expected contains to find 'b'")
	}

	if contains(slice, "d") {
		t.Error("Expected contains to not find 'd'")
	}
}

func TestComplexText(t *testing.T) {
	r := DefaultRedactor()

	input := `
	Name: Zhang San
	Phone: 13812345678
	Email: zhangsan@example.com
	ID Card: 110101199001011234
	SSN: 123-45-6789
	Bank Card: 4111111111111111
	IP: 192.168.1.100
	Address: Beijing Chaoyang District XX Street XX
	`

	result := r.Redact(input)

	// Check that no PII is in the result
	if strings.Contains(result, "13812345678") {
		t.Error("Phone should be redacted")
	}
	if strings.Contains(result, "zhangsan@example.com") {
		t.Error("Email should be redacted")
	}
	if strings.Contains(result, "110101199001011234") {
		t.Error("ID Card should be redacted")
	}
	if strings.Contains(result, "123-45-6789") {
		t.Error("SSN should be redacted")
	}
	if strings.Contains(result, "4111111111111111") {
		t.Error("Bank card should be redacted")
	}
	if strings.Contains(result, "192.168.1.100") {
		t.Error("IP should be redacted")
	}
}
