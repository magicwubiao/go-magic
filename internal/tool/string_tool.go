package tool

import (
	"context"
	"crypto/md5"
	crand "crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// String Tool - 字符串处理工具?
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
	crand.Read(b)

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
// Random Tool - 随机数生成?
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
		n := min + int(int64(rand.Intn(max-min+1)))
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
						"type": "string",
						"description": "Time string. Accepted: RFC3339 ('2026-11-24T19:45:41+08:00'), " +
							"'2006-01-02 15:04:05', '2006-01-02 15:04', '2006-01-02', unix seconds/milliseconds",
					},
					"time_str2": map[string]any{
						"type":        "string",
						"description": "Second time string (diff only); same formats as time_str",
					},
					"format": map[string]any{
						"type": "string",
						"description": "Output layout (Go reference: 2006-01-02 15:04:05). " +
							"For diff, this legacy field is still accepted as the second time when time_str2 is empty.",
						"default": "2006-01-02 15:04:05",
					},
					"duration": map[string]any{
						"type":        "string",
						"description": "Duration to add (e.g. '90m', '1h30m', '2d', '1w3d4h'). Units: ns/us/ms/s/m/h/d/w",
					},
					"timezone": map[string]any{
						"type": "string",
						"description": "Timezone (e.g. 'UTC', 'America/New_York', 'Asia/Shanghai'). " +
							"Zone-less inputs are parsed in this timezone and outputs are rendered in it. Default UTC",
						"default": "UTC",
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

// ============================================================================
// Time helpers
// ============================================================================

// timeLayouts 是 parseAnyTime 依次尝试的布局（不含带时区的 RFC3339 系列，
// 那些由 time.Parse 直接判定）。带时间但无时区的字符串按 loc 解析——这也是
// "UTC+8 用户看到的时间差 8 小时" 的根源：以前所有入口都硬编码
// "2006-01-02 15:04:05"，既拒绝 RFC3339，又把无时区串当 UTC。
var timeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
}

// resolveTZ 返回 args["timezone"] 指定的时区，缺省 UTC。
// 非法时区名不静默回落：显式报错比"结果差了 8 小时"好排查。
func resolveTZ(args map[string]any) (*time.Location, error) {
	tz, _ := args["timezone"].(string)
	if strings.TrimSpace(tz) == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q (use IANA names such as 'UTC', 'Asia/Shanghai')", tz)
	}
	return loc, nil
}

// parseAnyTime 解析一个时间字符串，兼容 RFC3339 与常见人类格式，以及
// unix 秒/毫秒时间戳。无时区的输入按 loc 解析。
//
// 之前 parse/add/diff 各自只认一种布局，导致同一串时间在 format 能用、在
// diff 报错；现在四个入口共用这一个解析器。
func parseAnyTime(s string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	// 纯数字：10 位按秒、13 位按毫秒（覆盖日志/接口里最常见的两种时间戳）。
	if allDigits(s) {
		switch len(s) {
		case 10:
			if sec, err := strconv.ParseInt(s, 10, 64); err == nil {
				return time.Unix(sec, 0).In(loc), nil
			}
		case 13:
			if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
				return time.UnixMilli(ms).In(loc), nil
			}
		}
	}

	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf(
		"cannot parse %q: accepted formats are RFC3339 (2026-11-24T19:45:41+08:00), "+
			"2006-01-02 15:04:05, 2006-01-02 15:04, 2006-01-02, and unix timestamps", s)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// durationTokenRe 匹配 "数字+单位" 片段，用于扩展 time.ParseDuration。
// Go 原生 duration 不支持 d/w，"2d3h" 会直接报 unknown unit。
var durationTokenRe = regexp.MustCompile(`(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h|d|w)`)

// parseDurationExt 解析时长，支持 Go 原生单位外加 d（天）和 w（周）。
// 分段累加而非字符串替换：把 "2d" 替换成 "48h" 会得到 "48h3h" 这种非法串。
func parseDurationExt(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}

	matches := durationTokenRe.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return 0, invalidDurationError(s)
	}

	var total time.Duration
	consumed := 0
	for _, m := range matches {
		// 片段之间只允许空白，出现其它字符说明写法不合法
		if strings.TrimSpace(s[consumed:m[0]]) != "" {
			return 0, invalidDurationError(s)
		}
		numStr, unit := s[m[2]:m[3]], s[m[4]:m[5]]
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, invalidDurationError(s)
		}
		switch unit {
		case "d":
			total += time.Duration(num * 24 * float64(time.Hour))
		case "w":
			total += time.Duration(num * 7 * 24 * float64(time.Hour))
		default:
			d, err := time.ParseDuration(numStr + unit)
			if err != nil {
				return 0, invalidDurationError(s)
			}
			total += d
		}
		consumed = m[1]
	}
	if strings.TrimSpace(s[consumed:]) != "" {
		return 0, invalidDurationError(s)
	}
	if neg {
		total = -total
	}
	return total, nil
}

func invalidDurationError(s string) error {
	return fmt.Errorf("invalid duration %q: use units ns/us/ms/s/m/h, plus d (day) and w (week); e.g. \"90m\", \"1h30m\", \"2d3h\", \"1w\"", s)
}

// requireTimeStr 读取并校验必填的时间参数。
func requireTimeStr(args map[string]any, key string) (string, error) {
	s, _ := args[key].(string)
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return s, nil
}

func (t *TimeTool) now(args map[string]any) (map[string]any, error) {
	loc, err := resolveTZ(args)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(loc)
	format := "2006-01-02 15:04:05"
	if f, ok := args["format"].(string); ok && f != "" {
		format = f
	}

	return map[string]any{
		"unix":      now.Unix(),
		"unix_nano": now.UnixNano(),
		"formatted": now.Format(format),
		"timezone":  loc.String(),
		"iso":       now.Format(time.RFC3339),
		"weekday":   now.Weekday().String(),
	}, nil
}

func (t *TimeTool) parse(args map[string]any) (map[string]any, error) {
	timeStr, err := requireTimeStr(args, "time_str")
	if err != nil {
		return nil, err
	}
	loc, err := resolveTZ(args)
	if err != nil {
		return nil, err
	}
	parsed, err := parseAnyTime(timeStr, loc)
	if err != nil {
		return nil, err
	}

	format := "2006-01-02 15:04:05"
	if f, ok := args["format"].(string); ok && f != "" {
		format = f
	}

	renderLoc := parsed.Location()
	if strings.TrimSpace(paramString(args, "timezone")) != "" {
		renderLoc = loc
	}

	// 回显解析结果所用的时区：无时区输入被当作 loc 处理，必须让调用方看到，
	// 否则 UTC+8 场景下 "2026-11-24 19:45:41" 会被误解成本地 19:45。
	return map[string]any{
		"unix":      parsed.Unix(),
		"formatted": parsed.In(renderLoc).Format(format),
		"iso":       parsed.Format(time.RFC3339),
		"timezone":  renderLoc.String(),
		"utc":       parsed.UTC().Format(time.RFC3339),
		"weekday":   parsed.Weekday().String(),
	}, nil
}

func (t *TimeTool) format(args map[string]any) (map[string]any, error) {
	timeStr, err := requireTimeStr(args, "time_str")
	if err != nil {
		return nil, err
	}
	loc, err := resolveTZ(args)
	if err != nil {
		return nil, err
	}

	parsed, err := parseAnyTime(timeStr, loc)
	if err != nil {
		return nil, err
	}

	// 输入自带时区（RFC3339）时按输入时区渲染，只有调用方显式给了 timezone
	// 才换算——否则 "2026-11-24T19:45:41+08:00" 在默认 UTC 下会变成 11:45，
	// 看起来像时间被算错了。
	renderLoc := parsed.Location()
	if strings.TrimSpace(paramString(args, "timezone")) != "" {
		renderLoc = loc
	}

	format := "2006-01-02 15:04:05"
	if f, ok := args["format"].(string); ok && f != "" {
		format = f
	}

	return map[string]any{
		"formatted": parsed.In(renderLoc).Format(format),
		"unix":      parsed.Unix(),
		"iso":       parsed.Format(time.RFC3339),
		"timezone":  renderLoc.String(),
	}, nil
}

func (t *TimeTool) add(args map[string]any) (map[string]any, error) {
	durationStr, _ := args["duration"].(string)
	if strings.TrimSpace(durationStr) == "" {
		return nil, fmt.Errorf("duration is required")
	}
	duration, err := parseDurationExt(durationStr)
	if err != nil {
		return nil, err
	}

	loc, err := resolveTZ(args)
	if err != nil {
		return nil, err
	}

	var base time.Time
	explicitZone := strings.TrimSpace(paramString(args, "timezone")) != ""
	if timeStr, _ := args["time_str"].(string); strings.TrimSpace(timeStr) != "" {
		base, err = parseAnyTime(timeStr, loc)
		if err != nil {
			return nil, err
		}
	} else {
		base = time.Now().In(loc)
	}

	// 与 format 一致：输入自带时区就按输入时区渲染，显式传 timezone 才换算。
	renderLoc := base.Location()
	if explicitZone {
		renderLoc = loc
	}

	result := base.Add(duration)
	return map[string]any{
		"unix":      result.Unix(),
		"formatted": result.In(renderLoc).Format("2006-01-02 15:04:05"),
		"iso":       result.Format(time.RFC3339),
		"timezone":  renderLoc.String(),
		"duration":  duration.String(),
	}, nil
}

func (t *TimeTool) diff(args map[string]any) (map[string]any, error) {
	timeStr1, err := requireTimeStr(args, "time_str")
	if err != nil {
		return nil, err
	}

	// 第二个时间优先用 time_str2。历史上它借用 "format" 字段传递（format 同时
	// 被当成输出布局，语义完全对不上），为兼容旧调用保留，但在结果里提示。
	timeStr2, _ := args["time_str2"].(string)
	legacyField := false
	if strings.TrimSpace(timeStr2) == "" {
		if v, ok := args["format"].(string); ok && strings.TrimSpace(v) != "" {
			timeStr2 = v
			legacyField = true
		}
	}
	if strings.TrimSpace(timeStr2) == "" {
		return nil, fmt.Errorf("time_str2 is required (second time to compare against time_str)")
	}

	loc, err := resolveTZ(args)
	if err != nil {
		return nil, err
	}

	t1, err := parseAnyTime(timeStr1, loc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time_str: %w", err)
	}
	t2, err := parseAnyTime(timeStr2, loc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time_str2: %w", err)
	}

	diff := t2.Sub(t1)
	result := map[string]any{
		"seconds":   int(diff.Seconds()),
		"minutes":   int(diff.Minutes()),
		"hours":     int(diff.Hours()),
		"days":      int(diff.Hours() / 24),
		"formatted": diff.String(),
		"sign":      diffSign(diff),
		"absolute":  absDuration(diff).String(),
		"timezone":  loc.String(),
	}
	if legacyField {
		result["note"] = "the second time was read from the legacy 'format' field; use time_str2 instead"
	}
	return result, nil
}

func diffSign(d time.Duration) string {
	switch {
	case d > 0:
		return "positive" // t2 is later than t1
	case d < 0:
		return "negative"
	default:
		return "zero"
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
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

	duration, err := parseDurationExt(durationStr)
	if err != nil {
		return nil, err
	}

	time.Sleep(duration)

	return map[string]any{
		"completed": true,
		"slept":     duration.String(),
	}, nil
}
