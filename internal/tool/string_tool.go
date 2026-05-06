package tool

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ============================================================================
// String Tool - 字符串处理工具
// ============================================================================

type StringTool struct {
	BaseTool
}

func NewStringTool() *StringTool {
	return &StringTool{
		BaseTool: *NewBaseTool(
			"string",
			"Process strings: regex, replace, encode/decode, case conversion, trim, split, join",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: upper, lower, title, trim, replace, regex, split, join, reverse, length, contains, startswith, endswith, encode_base64, decode_base64, url_encode, url_decode",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Input text",
					},
					"pattern": map[string]any{
						"type":        "string",
						"description": "Pattern for regex/replace operations",
					},
					"replacement": map[string]any{
						"type":        "string",
						"description": "Replacement string for replace operation",
					},
					"delimiter": map[string]any{
						"type":        "string",
						"description": "Delimiter for split/join operations (default: comma)",
						"default":     ",",
					},
					"options": map[string]any{
						"type":        "string",
						"description": "Additional options (e.g., 'i' for case-insensitive regex)",
					},
				},
				"required": []any{"operation", "text"},
			},
		),
	}
}

func (t *StringTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)
	text, _ := args["text"].(string)

	if text == "" {
		return nil, fmt.Errorf("text is required")
	}

	switch operation {
	case "upper":
		return strings.ToUpper(text), nil
	case "lower":
		return strings.ToLower(text), nil
	case "title":
		return strings.Title(strings.ToLower(text)), nil
	case "trim":
		return strings.TrimSpace(text), nil
	case "replace":
		pattern, _ := args["pattern"].(string)
		replacement, _ := args["replacement"].(string)
		return t.replace(text, pattern, replacement)
	case "regex":
		pattern, _ := args["pattern"].(string)
		return t.regex(text, pattern)
	case "split":
		delimiter, _ := args["delimiter"].(string)
		if delimiter == "" {
			delimiter = ","
		}
		return strings.Split(text, delimiter), nil
	case "join":
		delimiter, _ := args["delimiter"].(string)
		if delimiter == "" {
			delimiter = ","
		}
		return t.join(text, delimiter)
	case "reverse":
		return t.reverse(text), nil
	case "length":
		return len(text), nil
	case "contains":
		substr, _ := args["pattern"].(string)
		return strings.Contains(text, substr), nil
	case "startswith":
		prefix, _ := args["pattern"].(string)
		return strings.HasPrefix(text, prefix), nil
	case "endswith":
		suffix, _ := args["pattern"].(string)
		return strings.HasSuffix(text, suffix), nil
	case "encode_base64":
		return base64.StdEncoding.EncodeToString([]byte(text)), nil
	case "decode_base64":
		return t.decodeBase64(text)
	case "url_encode":
		return strings.ReplaceAll(text, " ", "%20"), nil
	case "url_decode":
		return strings.ReplaceAll(text, "%20", " "), nil
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *StringTool) replace(text, pattern, replacement string) (string, error) {
	if pattern == "" {
		return text, fmt.Errorf("pattern is required for replace")
	}
	return strings.ReplaceAll(text, pattern, replacement), nil
}

func (t *StringTool) regex(text, pattern string) (map[string]any, error) {
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required for regex")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	matches := re.FindAllString(text, -1)
	return map[string]any{
		"matches": matches,
		"count":   len(matches),
		"found":   len(matches) > 0,
	}, nil
}

func (t *StringTool) join(text, delimiter string) (string, error) {
	var parts []string
	if err := json.Unmarshal([]byte(text), &parts); err != nil {
		return "", fmt.Errorf("text should be a JSON array for join operation")
	}
	return strings.Join(parts, delimiter), nil
}

func (t *StringTool) reverse(text string) string {
	runes := []rune(text)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func (t *StringTool) decodeBase64(text string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	return string(decoded), nil
}

// ============================================================================
// Hash Tool - 哈希计算工具
// ============================================================================

type HashTool struct {
	BaseTool
}

func NewHashTool() *HashTool {
	return &HashTool{
		BaseTool: *NewBaseTool(
			"hash",
			"Calculate hash values: MD5, SHA1, SHA256, SHA512",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"algorithm": map[string]any{
						"type":        "string",
						"description": "Hash algorithm: md5, sha1, sha256, sha512",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Text to hash",
					},
					"encoding": map[string]any{
						"type":        "string",
						"description": "Output encoding: hex (default), base64",
						"default":     "hex",
					},
				},
				"required": []any{"algorithm", "text"},
			},
		),
	}
}

func (t *HashTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	algorithm, _ := args["algorithm"].(string)
	text, _ := args["text"].(string)
	encoding, _ := args["encoding"].(string)

	if text == "" {
		return nil, fmt.Errorf("text is required")
	}

	if encoding == "" {
		encoding = "hex"
	}

	data := []byte(text)
	var hash []byte

	switch algorithm {
	case "md5":
		h := md5.Sum(data)
		hash = h[:]
	case "sha1":
		h := sha1.Sum(data)
		hash = h[:]
	case "sha256":
		h := sha256.Sum256(data)
		hash = h[:]
	case "sha512":
		h := sha512.Sum512(data)
		hash = h[:]
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	var result string
	switch encoding {
	case "hex":
		result = hex.EncodeToString(hash)
	case "base64":
		result = base64.StdEncoding.EncodeToString(hash)
	default:
		return nil, fmt.Errorf("unsupported encoding: %s", encoding)
	}

	return map[string]any{
		"algorithm": algorithm,
		"encoding":  encoding,
		"hash":      result,
		"length":    len(hash),
	}, nil
}

// ============================================================================
// UUID Tool - UUID 生成工具
// ============================================================================

type UUIDTool struct {
	BaseTool
}

func NewUUIDTool() *UUIDTool {
	return &UUIDTool{
		BaseTool: *NewBaseTool(
			"uuid",
			"Generate UUIDs: v4 (random), v5 (name-based)",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: generate, validate",
						"enum":        []any{"generate", "validate"},
					},
					"version": map[string]any{
						"type":        "number",
						"description": "UUID version: 4 (random) or 5 (name-based)",
						"enum":        []any{4, 5},
					},
					"namespace": map[string]any{
						"type":        "string",
						"description": "Namespace for v5 UUID (e.g., dns, url, oid)",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Name for v5 UUID generation",
					},
				},
				"required": []any{"operation"},
			},
		),
	}
}

func (t *UUIDTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)

	switch operation {
	case "generate":
		return t.generate(args)
	case "validate":
		uuidStr, _ := args["name"].(string)
		return t.validate(uuidStr)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *UUIDTool) generate(args map[string]any) (map[string]any, error) {
	version := 4
	if v, ok := args["version"].(float64); ok {
		version = int(v)
	}

	var uuidStr string
	switch version {
	case 4:
		uuidStr = generateUUIDv4()
	case 5:
		namespace, _ := args["namespace"].(string)
		name, _ := args["name"].(string)
		uuidStr = generateUUIDv5(namespace, name)
	default:
		return nil, fmt.Errorf("unsupported UUID version: %d", version)
	}

	return map[string]any{
		"uuid":    uuidStr,
		"version": version,
	}, nil
}

func (t *UUIDTool) validate(uuid string) (map[string]any, error) {
	uuidRegex := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	isValid := uuidRegex.MatchString(uuid)

	return map[string]any{
		"valid": isValid,
		"uuid":  uuid,
	}, nil
}

func generateUUIDv4() string {
	b := make([]byte, 16)
	rand.Read(b)

	// Set version 4 and variant bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func generateUUIDv5(namespace, name string) string {
	// Simple v5 implementation using SHA1
	nsBytes := sha1.Sum([]byte(namespace))
	h := sha1.Sum(append(nsBytes[:], []byte(name)...))

	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ============================================================================
// Random Tool - 随机数生成工具
// ============================================================================

type RandomTool struct {
	BaseTool
}

func NewRandomTool() *RandomTool {
	return &RandomTool{
		BaseTool: *NewBaseTool(
			"random",
			"Generate random values: number, string, choice, uuid",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: number, string, choice, uuid",
					},
					"min": map[string]any{
						"type":        "number",
						"description": "Minimum value for number operation",
						"default":     0,
					},
					"max": map[string]any{
						"type":        "number",
						"description": "Maximum value for number operation",
						"default":     100,
					},
					"length": map[string]any{
						"type":        "number",
						"description": "Length for string operation",
						"default":     16,
					},
					"charset": map[string]any{
						"type":        "string",
						"description": "Character set for string: alphanumeric, alpha, numeric, hex",
						"default":     "alphanumeric",
					},
					"choices": map[string]any{
						"type":        "array",
						"description": "Choices for choice operation",
					},
					"count": map[string]any{
						"type":        "number",
						"description": "Number of random values to generate",
						"default":     1,
					},
				},
				"required": []any{"operation"},
			},
		),
	}
}

func (t *RandomTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)

	switch operation {
	case "number":
		return t.randomNumber(args)
	case "string":
		return t.randomString(args)
	case "choice":
		return t.choice(args)
	case "uuid":
		return generateUUIDv4(), nil
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *RandomTool) randomNumber(args map[string]any) (map[string]any, error) {
	min := 0
	max := 100
	count := 1

	if m, ok := args["min"].(float64); ok {
		min = int(m)
	}
	if m, ok := args["max"].(float64); ok {
		max = int(m)
	}
	if c, ok := args["count"].(float64); ok {
		count = int(c)
	}

	var numbers []int
	for i := 0; i < count; i++ {
		n := min + int(rand.Int63n(int64(max-min+1)))
		numbers = append(numbers, n)
	}

	if count == 1 && len(numbers) == 1 {
		return map[string]any{"number": numbers[0]}, nil
	}
	return map[string]any{"numbers": numbers}, nil
}

func (t *RandomTool) randomString(args map[string]any) (map[string]any, error) {
	length := 16
	charset := "alphanumeric"

	if l, ok := args["length"].(float64); ok {
		length = int(l)
	}
	if c, ok := args["charset"].(string); ok {
		charset = c
	}

	var chars string
	switch charset {
	case "alphanumeric":
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	case "alpha":
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	case "numeric":
		chars = "0123456789"
	case "hex":
		chars = "0123456789abcdef"
	default:
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	}

	result := make([]byte, length)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}

	return map[string]any{"string": string(result), "length": length}, nil
}

func (t *RandomTool) choice(args map[string]any) (map[string]any, error) {
	choices, ok := args["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, fmt.Errorf("choices array is required for choice operation")
	}

	idx := rand.Intn(len(choices))
	return map[string]any{
		"choice": choices[idx],
		"index":  idx,
	}, nil
}

// ============================================================================
// Time Tool - 时间处理工具
// ============================================================================

type TimeTool struct {
	BaseTool
}

func NewTimeTool() *TimeTool {
	return &TimeTool{
		BaseTool: *NewBaseTool(
			"time",
			"Time operations: now, parse, format, add, diff, sleep",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: now, parse, format, add, diff, sleep, timestamp",
					},
					"time_str": map[string]any{
						"type":        "string",
						"description": "Time string to parse",
					},
					"format": map[string]any{
						"type":        "string",
						"description": "Format string (Go reference: 2006-01-02 15:04:05)",
						"default":     "2006-01-02 15:04:05",
					},
					"duration": map[string]any{
						"type":        "string",
						"description": "Duration to add (e.g., '1h30m', '2d')",
					},
					"timezone": map[string]any{
						"type":        "string",
						"description": "Timezone (e.g., 'UTC', 'America/New_York', 'Asia/Shanghai')",
						"default":     "UTC",
					},
				},
				"required": []any{"operation"},
			},
		),
	}
}

func (t *TimeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)

	switch operation {
	case "now":
		return t.now(args)
	case "parse":
		return t.parse(args)
	case "format":
		return t.format(args)
	case "add":
		return t.add(args)
	case "diff":
		return t.diff(args)
	case "timestamp":
		return t.timestamp()
	case "sleep":
		return t.sleep(args)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *TimeTool) now(args map[string]any) (map[string]any, error) {
	loc := time.UTC
	if tz, ok := args["timezone"].(string); ok {
		var err error
		loc, err = time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
	}

	now := time.Now().In(loc)
	format := "2006-01-02 15:04:05"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	return map[string]any{
		"unix":       now.Unix(),
		"unix_nano":  now.UnixNano(),
		"formatted":  now.Format(format),
		"timezone":   loc.String(),
		"iso":        now.Format(time.RFC3339),
	}, nil
}

func (t *TimeTool) parse(args map[string]any) (map[string]any, error) {
	timeStr, _ := args["time_str"].(string)
	if timeStr == "" {
		return nil, fmt.Errorf("time_str is required")
	}

	format := "2006-01-02 15:04:05"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	parsed, err := time.Parse(format, timeStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time: %w", err)
	}

	return map[string]any{
		"unix":      parsed.Unix(),
		"formatted": parsed.Format("2006-01-02 15:04:05"),
		"iso":       parsed.Format(time.RFC3339),
		"weekday":   parsed.Weekday().String(),
	}, nil
}

func (t *TimeTool) format(args map[string]any) (map[string]any, error) {
	timeStr, _ := args["time_str"].(string)
	if timeStr == "" {
		return nil, fmt.Errorf("time_str is required")
	}

	format := "2006-01-02 15:04:05"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	// Try parsing with multiple common formats
	parsed, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		parsed, err = time.Parse("2006-01-02 15:04:05", timeStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time: %w", err)
		}
	}

	return map[string]any{
		"formatted": parsed.Format(format),
		"unix":      parsed.Unix(),
	}, nil
}

func (t *TimeTool) add(args map[string]any) (map[string]any, error) {
	timeStr, _ := args["time_str"].(string)
	durationStr, _ := args["duration"].(string)

	if durationStr == "" {
		return nil, fmt.Errorf("duration is required")
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return nil, fmt.Errorf("invalid duration: %w", err)
	}

	var result time.Time
	if timeStr == "" {
		result = time.Now().Add(duration)
	} else {
		parsed, err := time.Parse("2006-01-02 15:04:05", timeStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time: %w", err)
		}
		result = parsed.Add(duration)
	}

	return map[string]any{
		"unix":      result.Unix(),
		"formatted": result.Format("2006-01-02 15:04:05"),
		"iso":       result.Format(time.RFC3339),
	}, nil
}

func (t *TimeTool) diff(args map[string]any) (map[string]any, error) {
	timeStr1, _ := args["time_str"].(string)
	timeStr2, _ := args["format"].(string) // using format field for second time

	if timeStr1 == "" || timeStr2 == "" {
		return nil, fmt.Errorf("time_str and format (second time) are required")
	}

	t1, err := time.Parse("2006-01-02 15:04:05", timeStr1)
	if err != nil {
		return nil, fmt.Errorf("failed to parse first time: %w", err)
	}

	t2, err := time.Parse("2006-01-02 15:04:05", timeStr2)
	if err != nil {
		return nil, fmt.Errorf("failed to parse second time: %w", err)
	}

	diff := t2.Sub(t1)

	return map[string]any{
		"seconds":      int(diff.Seconds()),
		"minutes":      int(diff.Minutes()),
		"hours":        int(diff.Hours()),
		"days":         int(diff.Hours() / 24),
		"formatted":    diff.String(),
	}, nil
}

func (t *TimeTool) timestamp() (map[string]any, error) {
	now := time.Now()
	return map[string]any{
		"unix":      now.Unix(),
		"unix_nano": now.UnixNano(),
		"iso":       now.Format(time.RFC3339),
	}, nil
}

func (t *TimeTool) sleep(args map[string]any) (map[string]any, error) {
	durationStr, _ := args["duration"].(string)
	if durationStr == "" {
		return nil, fmt.Errorf("duration is required")
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return nil, fmt.Errorf("invalid duration: %w", err)
	}

	time.Sleep(duration)

	return map[string]any{
		"completed": true,
		"slept":     duration.String(),
	}, nil
}
