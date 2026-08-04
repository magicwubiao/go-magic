// Package approval provides intelligent command approval system
// inspired by Cortex Agent's Smart Approvals.
// It features command normalization, risk assessment, session learning,
// approval history, statistics, and web-based approval callbacks.
package approval

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	configpkg "github.com/magicwubiao/go-magic/pkg/config"
)

// ---------------------------------------------------------------------------
// Core type definitions
// ---------------------------------------------------------------------------

// Strategy defines how commands are approved.
type Strategy string

const (
	StrategyManual      Strategy = "manual"
	StrategyAutoApprove Strategy = "auto"
	StrategySmart       Strategy = "smart"
	StrategyWhitelist   Strategy = "whitelist"
)

// RiskLevel represents the danger level of a command.
type RiskLevel int

const (
	RiskLow      RiskLevel = 1
	RiskMedium   RiskLevel = 2
	RiskHigh     RiskLevel = 3
	RiskCritical RiskLevel = 4
)

// String returns a human-readable label for the risk level.
func (r RiskLevel) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	case RiskCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// MarshalJSON serializes RiskLevel as a string.
func (r RiskLevel) MarshalJSON() ([]byte, error) {
	return []byte(`"` + r.String() + `"`), nil
}

// UnmarshalJSON deserializes RiskLevel from a string or number.
func (r *RiskLevel) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	switch s {
	case "low", "1", "0":
		*r = RiskLow
	case "medium", "2":
		*r = RiskMedium
	case "high", "3":
		*r = RiskHigh
	case "critical", "4":
		*r = RiskCritical
	default:
		*r = RiskLow // Default to low risk for unknown values
	}
	return nil
}

// ---------------------------------------------------------------------------
// ParsedCommand represents a parsed shell command structure.
// ---------------------------------------------------------------------------

// ParsedCommand represents a parsed command structure.
type ParsedCommand struct {
	Binary        string            // 命令二进制名 (如 "npm", "git")
	SubCommand    string            // 子命令 (如 "install", "push")
	Flags         map[string]string // 标志参数
	Args          []string          // 位置参数
	RawArgs       string            // 原始参数字符串
	HasPipe       bool              // 是否包含管道
	HasChain      bool              // 是否包含链式操作 (&&, ||, ;)
	PipeSegments  []string          // 管道分段
	ChainSegments []string          // 链式分段
}

// ---------------------------------------------------------------------------
// RiskAssessment contains detailed risk evaluation results.
// ---------------------------------------------------------------------------

// RiskAssessment 包含详细的风险评估结果.
type RiskAssessment struct {
	Level         RiskLevel // 最终风险等级
	Category      string    // "file_destruct", "network", "privilege_esc", "data_access", "system", "package_mgmt"
	Factors       []string  // 具体风险因素
	BypassAttempt bool      // 检测到绕过尝试
	BypassType    string    // 绕过类型 (encoding, variable, path_traversal)
	Score         float64   // 0-100 风险评分
}

// ---------------------------------------------------------------------------
// ApprovalRecord 审批历史记录
// ---------------------------------------------------------------------------

// ApprovalRecord 审批历史记录.
type ApprovalRecord struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	Normalized string    `json:"normalized"`
	RiskLevel  RiskLevel `json:"risk_level"`
	RiskScore  float64   `json:"risk_score"`
	Category   string    `json:"category"` // risk category
	Decision   string    `json:"decision"` // approved, denied, auto_approved, timeout
	Strategy   Strategy  `json:"strategy"`
	Reason     string    `json:"reason"`
	SessionID  string    `json:"session_id"`
	WorkingDir string    `json:"working_dir"`
	Duration   int64     `json:"duration_ms"` // 审批耗时
	Timestamp  time.Time `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// ApprovalStats / CommandStat 审批统计
// ---------------------------------------------------------------------------

// ApprovalStats 审批统计.
type ApprovalStats struct {
	TotalRequests   int               `json:"total_requests"`
	AutoApproved    int               `json:"auto_approved"`
	UserApproved    int               `json:"user_approved"`
	UserDenied      int               `json:"user_denied"`
	TimedOut        int               `json:"timed_out"`
	TrustedPatterns int               `json:"trusted_patterns"`
	DeniedPatterns  int               `json:"denied_patterns"`
	ByRiskLevel     map[RiskLevel]int `json:"by_risk_level"`
	ByCategory      map[string]int    `json:"by_category"`
	TopCommands     []CommandStat     `json:"top_commands"`
	AvgResponseTime float64           `json:"avg_response_time_ms"`
}

// CommandStat 单个命令的审批统计.
type CommandStat struct {
	Pattern   string    `json:"pattern"`
	Count     int       `json:"count"`
	Approved  int       `json:"approved"`
	Denied    int       `json:"denied"`
	RiskLevel RiskLevel `json:"risk_level"`
	LastSeen  time.Time `json:"last_seen"`
}

// ---------------------------------------------------------------------------
// WebApprovalCallback / PendingApproval Web端审批回调
// ---------------------------------------------------------------------------

// WebApprovalCallback Web端审批回调.
type WebApprovalCallback struct {
	pendingApprovals map[string]*PendingApproval
	mu               sync.Mutex
	// onCreated 在创建 pending 后同步触发，用于向 SSE 流推送 approval_required 事件。
	// 回调返回的 info 含 id/command/riskLevel/sessionId/expiresAt。
	// 回调为 nil 时跳过（如非 Web 场景）。
	onCreated func(info PendingApprovalInfo)
}

// PendingApprovalInfo 是 OnPendingCreated 回调传递给外部的待审批信息。
// 设计为值类型，避免外部拿到内部 *PendingApproval 指针后误改 channel。
type PendingApprovalInfo struct {
	ID         string
	Command    string
	SessionID  string
	WorkingDir string
	RiskLevel  string
	Reason     string
	Context    string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// SetOnPendingCreated 注册 pending 创建回调（SSE 流注入点）。
func (m *Manager) SetOnPendingCreated(fn func(info PendingApprovalInfo)) {
	m.webCallback.mu.Lock()
	m.webCallback.onCreated = fn
	m.webCallback.mu.Unlock()
}

// PendingApproval 待Web审批的请求.
type PendingApproval struct {
	ID        string
	Request   *ApprovalRequest
	Result    chan *ApprovalResult
	CreatedAt time.Time
	ExpiresAt time.Time
}

// ---------------------------------------------------------------------------
// SessionApprovalContext 会话级审批上下文
// ---------------------------------------------------------------------------

// SessionApprovalContext 会话级审批上下文，用于会话内学习.
type SessionApprovalContext struct {
	SessionID    string
	RecentCmds   []string    // 最近执行的命令（归一化后）
	ApprovedHash map[int]int // riskLevel -> 同类批准次数
	CreatedAt    time.Time
}

// ---------------------------------------------------------------------------
// CommandPattern / ApprovalRequest / ApprovalResult / ApprovalConfig
// ---------------------------------------------------------------------------

// CommandPattern represents a learned command pattern.
type CommandPattern struct {
	Pattern     string    `json:"pattern"`
	PatternHash string    `json:"pattern_hash"`
	Action      string    `json:"action"` // approved, denied
	Count       int       `json:"count"`
	RiskLevel   RiskLevel `json:"risk_level"`
	LastSeen    time.Time `json:"last_seen"`
	SessionIDs  []string  `json:"session_ids"`
	Trusted     bool      `json:"trusted"`
}

// ApprovalRequest represents a command approval request.
type ApprovalRequest struct {
	Command    string
	Args       []string
	WorkingDir string
	Env        map[string]string
	SessionID  string
	UserID     string
	RiskLevel  RiskLevel
	Category   string // risk category from assessment
	Reason     string
	Timestamp  time.Time
	// Context 携带与命令相关的可读上下文片段，例如 execute_code 的源码预览、
	// file_edit 的 diff 摘要等。审批 UI 可以展示该字段，避免审批者只看到
	// "execute_code python (N bytes)" 这样不透明的描述。
	Context string
}

// ApprovalResult is the result of an approval decision.
type ApprovalResult struct {
	Approved  bool
	Strategy  Strategy
	Reason    string
	Trusted   bool
	AskUser   bool
	RiskLevel RiskLevel
	Pattern   *CommandPattern
}

// ApprovalConfig holds approval system configuration.
type ApprovalConfig struct {
	Strategy          Strategy `mapstructure:"strategy"`
	TrustThreshold    int      `mapstructure:"trust_threshold"`
	DenylistThreshold int      `mapstructure:"denylist_threshold"`
	EnableLearning    bool     `mapstructure:"enable_learning"`
	EnableWhitelist   bool     `mapstructure:"enable_whitelist"`
	EnableCLIConfirm  bool     `mapstructure:"enable_cli_confirm"`
	GatewayEnabled    bool     `mapstructure:"gateway_enabled"`
	GatewayURL        string   `mapstructure:"gateway_url"`
	DangerousPatterns []string `mapstructure:"dangerous_patterns"`
	AllowedPatterns   []string `mapstructure:"allowed_patterns"`
	ApprovalTimeout   int      `mapstructure:"approval_timeout"`
	LearnFromSameUser bool     `mapstructure:"learn_from_same_user"`
}

// DefaultConfig returns the default approval configuration.
func DefaultConfig() *ApprovalConfig {
	return &ApprovalConfig{
		Strategy:          StrategySmart,
		TrustThreshold:    3,
		DenylistThreshold: 2,
		EnableLearning:    true,
		EnableWhitelist:   true,
		EnableCLIConfirm:  false,
		GatewayEnabled:    false,
		DangerousPatterns: []string{
			`rm\s+-rf\s+/(?:\*|$)`,
			`rm\s+-rf\s+/\*\s*$`,
			`rm\s+-rf\s+/(?:home|usr|var|etc|boot|root|bin|sbin|lib|proc|sys|dev)(?:/|\s|$)`,
			`rm\s+-rf\s+~(?:\s|$)`,
			`rm\s+-rf\s+\$HOME`,
			`dd\s+if=.*of=/dev/sd`,
			`mkfs\.`,
			`shutdown\s+-h\s+now`,
			`reboot`,
			`:\(\)\{:\|\:&\};:`,
			`>\s*/dev/sda`,
			`chmod\s+-R\s+777\s+/`,
			`chown\s+-R\s+.*\s+/`,
		},
		AllowedPatterns: []string{
			`^(ls|pwd|whoami|echo|date|cat|head|tail|grep|find|which|file|stat|env|id|uname|hostname|df|du|free|ps)(\s|$)`,
			`^(cd|mkdir|rmdir|touch|cp|mv|chmod|chown)(\s|$)`,
			`^git\s+(status|log|diff|show|branch|remote|stash)(\s|$)`,
		},
		ApprovalTimeout:   60,
		LearnFromSameUser: true,
	}
}

// ---------------------------------------------------------------------------
// ApprovalCallback interface
// ---------------------------------------------------------------------------

// ApprovalCallback is called on approval decisions.
type ApprovalCallback interface {
	OnApproval(result *ApprovalResult, req *ApprovalRequest)
	OnApprovalTimeout(req *ApprovalRequest)
}

// ---------------------------------------------------------------------------
// Manager handles command approvals
// ---------------------------------------------------------------------------

// Manager handles command approvals with history, session learning, and web callbacks.
type Manager struct {
	config         *ApprovalConfig
	patterns       map[string]*CommandPattern
	whitelist      map[string]bool
	history        []*ApprovalRecord
	sessionContext map[string]*SessionApprovalContext // 会话级审批上下文
	pendingWeb     map[string]*PendingApproval        // 待Web审批
	mu             sync.RWMutex
	patternsDB     string
	historyDB      string
	callbacks      []ApprovalCallback
	webCallback    *WebApprovalCallback
	// Stats cache to avoid O(n) computation on every page load
	statsCache     *ApprovalStats
	statsCacheTime time.Time
	statsCacheMu   sync.Mutex
	// Async save to avoid blocking on disk I/O
	patternsDirty bool
	historyDirty  bool
	saveMu        sync.Mutex // prevents concurrent disk writes
}

// NewManager creates a new approval manager.
func NewManager(config *ApprovalConfig) (*Manager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	dbDir := filepath.Join(configpkg.GetMagicHome(), "approval")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create approval directory: %w", err)
	}

	m := &Manager{
		config:         config,
		patterns:       make(map[string]*CommandPattern),
		whitelist:      make(map[string]bool),
		history:        make([]*ApprovalRecord, 0),
		sessionContext: make(map[string]*SessionApprovalContext),
		pendingWeb:     make(map[string]*PendingApproval),
		patternsDB:     filepath.Join(dbDir, "patterns.json"),
		historyDB:      filepath.Join(dbDir, "history.json"),
		webCallback: &WebApprovalCallback{
			pendingApprovals: make(map[string]*PendingApproval),
		},
	}

	m.loadPatterns()
	m.loadWhitelist()
	m.loadHistory()

	return m, nil
}

// ---------------------------------------------------------------------------
// Command normalization engine
// ---------------------------------------------------------------------------

// parseCommand parses a raw shell command string into a ParsedCommand structure.
// It handles quotes, escapes, pipes, and chain operators.
func parseCommand(cmd string) *ParsedCommand {
	pc := &ParsedCommand{
		Flags: make(map[string]string),
		Args:  make([]string, 0),
	}

	trimmed := strings.TrimSpace(cmd)
	pc.RawArgs = trimmed

	// 检测链式命令（&&、||、;），必须在管道检测之前处理
	if strings.Contains(trimmed, "&&") || strings.Contains(trimmed, "||") || strings.Contains(trimmed, ";") {
		pc.HasChain = true
		pc.ChainSegments = splitChainSegments(trimmed)
	}

	// 检测管道（单 |），排除 || 逻辑或
	// 先移除 || 后再检查是否还有单个 |
	strippedOr := strings.ReplaceAll(trimmed, "||", "\x00\x00")
	if strings.Contains(strippedOr, "|") {
		pc.HasPipe = true
		// 用移除 || 后的字符串分割，再映射回原始段
		pc.PipeSegments = splitSegments(strippedOr, '|')
	}

	// Tokenize the first segment (or the whole command if no pipes)
	segment := trimmed
	if len(pc.PipeSegments) > 0 {
		segment = pc.PipeSegments[0]
	}
	tokens := tokenizeShell(segment)
	if len(tokens) == 0 {
		return pc
	}

	pc.Binary = filepath.Base(tokens[0])
	if len(tokens) > 1 {
		pc.SubCommand = tokens[1]
	}
	// Parse flags and positional args from remaining tokens
	for i := 1; i < len(tokens); i++ {
		t := tokens[i]
		if strings.HasPrefix(t, "-") {
			key := t
			val := ""
			if strings.Contains(t, "=") {
				parts := strings.SplitN(t, "=", 2)
				key = parts[0]
				val = parts[1]
			} else if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				val = tokens[i+1]
				i++ // skip next token as value
			}
			pc.Flags[key] = val
		} else if i > 1 || (i == 1 && pc.SubCommand == "") {
			pc.Args = append(pc.Args, t)
		}
	}

	return pc
}

// tokenizeShell performs basic shell tokenization respecting quotes.
// 引号字符本身不会被写入 token，只保留引号内的内容。
func tokenizeShell(s string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, ch := range s {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				// 不写入引号字符本身
			} else {
				current.WriteRune(ch)
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				// 不写入引号字符本身
			} else {
				current.WriteRune(ch)
			}
		case ' ', '\t':
			if !inSingle && !inDouble {
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// splitSegments splits a string by a delimiter, respecting quoted regions.
func splitSegments(s string, delim rune) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for _, ch := range s {
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(ch)
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(ch)
		} else if ch == delim && !inSingle && !inDouble {
			segments = append(segments, strings.TrimSpace(current.String()))
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		segments = append(segments, strings.TrimSpace(current.String()))
	}
	return segments
}

// splitChainSegments splits a command string by chain operators (&&, ||, ;).
func splitChainSegments(s string) []string {
	// Replace ; with ; followed by space for consistent splitting
	re := regexp.MustCompile(`(;|&&|\|\|)`)
	parts := re.Split(s, -1)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// normalizeCommand normalizes a command string for pattern matching.
// It parses the command, generalizes arguments, and produces a normalized form.
func normalizeCommand(cmd string) string {
	pc := parseCommand(cmd)

	// Build normalized form: binary subcommand [flag_keys...]
	var sb strings.Builder
	sb.WriteString(pc.Binary)
	if pc.SubCommand != "" {
		sb.WriteString(" ")
		sb.WriteString(pc.SubCommand)
	}

	// Collect flag keys in sorted order for deterministic output
	flagKeys := make([]string, 0, len(pc.Flags))
	for k := range pc.Flags {
		flagKeys = append(flagKeys, k)
	}
	sort.Strings(flagKeys)
	for _, k := range flagKeys {
		sb.WriteString(" ")
		sb.WriteString(k)
	}

	// Append generalized positional args
	for _, a := range pc.Args {
		sb.WriteString(" ")
		sb.WriteString(generalizeArg(a))
	}

	result := sb.String()
	// Collapse whitespace
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")
	return strings.TrimSpace(strings.ToLower(result))
}

// generalizeArg replaces specific values with placeholders.
func generalizeArg(arg string) string {
	// UUID pattern
	if matched, _ := regexp.MatchString(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, strings.ToLower(arg)); matched {
		return "$UUID"
	}
	// Hash pattern (40-char hex for git, 64-char for sha256)
	if matched, _ := regexp.MatchString(`^[0-9a-f]{7,64}$`, strings.ToLower(arg)); matched {
		return "$HASH"
	}
	// URL pattern
	if matched, _ := regexp.MatchString(`^https?://\S+$`, arg); matched {
		return "$URL"
	}
	// Version pattern (e.g. 1.2.3, v1.2.3)
	if matched, _ := regexp.MatchString(`^v?\d+\.\d+(\.\d+)*(-[\w.]+)?$`, arg); matched {
		return "$VER"
	}
	// Absolute path -> $DIR/filename
	if strings.HasPrefix(arg, "/") {
		return "$DIR/" + filepath.Base(arg)
	}
	// Relative path with directory separator -> $REL/filename
	if strings.Contains(arg, "/") || strings.Contains(arg, string(filepath.Separator)) {
		return "$REL/" + filepath.Base(arg)
	}
	// Pure numbers -> N
	if matched, _ := regexp.MatchString(`^\d+$`, arg); matched {
		return "N"
	}
	return arg
}

// hashPattern creates a structural hash for pattern matching based on the
// ParsedCommand (binary + subcommand + sorted flag keys), not the raw string.
func (m *Manager) hashPattern(cmd string) string {
	normalized := normalizeCommand(cmd)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:8])
}

// ---------------------------------------------------------------------------
// Enhanced risk assessment engine
// ---------------------------------------------------------------------------

// riskCategoryPatterns maps risk categories to their indicator patterns.
var riskCategoryPatterns = map[string][]string{
	"file_destruct": {
		"rm -rf", "rm -r", "truncate", "shred", "dd if=", "> /dev/",
	},
	"network": {
		"curl ", "wget ", "nc ", "ncat ", "telnet ",
	},
	"privilege_esc": {
		"chmod 777", "chown", "sudo ", "su ", "setuid",
	},
	"data_access": {
		"cat /etc/", "cat /var/", ".ssh/", "/etc/shadow", "/etc/passwd",
	},
	"system": {
		"shutdown", "reboot", "halt", "init ", "systemctl restart",
		"mkfs", "fdisk", "parted", ":(){:|:&};:",
	},
	"package_mgmt": {
		"pip install", "npm install", "yarn add", "cargo install",
		"go install", "apt install", "brew install", "docker run",
	},
}

// sensitiveDirectories are directories that elevate risk when commands run inside them.
var sensitiveDirectories = []string{
	"/etc", "/usr", "/bin", "/sbin", "/lib", "/boot",
	"/var/log", "/root", "/sys", "/proc",
}

// bypassPatterns detect attempts to evade approval checks.
// 仅匹配真正可疑的绕过模式，避免对正常命令的大量误报。
var bypassPatterns = map[string]string{
	`base64\s+-d`:            "encoding",
	`xxd\s+-r`:               "encoding",
	`eval\s+\$`:              "eval_var",
	`eval\s*\$\(`:            "eval_subshell",
	`echo\s+.*\|\s*(ba)?sh`:  "pipe_to_shell",
	`curl\s+.*\|\s*(ba)?sh`:  "curl_pipe_shell",
	`wget\s+.*\|\s*(ba)?sh`:  "wget_pipe_shell",
	`\$\(\s*curl`:            "curl_subshell",
	`\$\(\s*wget`:            "wget_subshell",
	`/proc/self/fd`:          "proc_fd_access",
	`>\s*/dev/(tcp|udp)`:     "dev_network",
	`python\s+-c\s+import\s`: "python_import_c",
}

// calculateRiskLevel assesses the risk of a command and returns a RiskAssessment.
func (m *Manager) calculateRiskLevel(cmd string) RiskLevel {
	assessment := m.assessRisk(cmd)
	return assessment.Level
}

// assessRisk performs a comprehensive risk assessment of a command.
func (m *Manager) assessRisk(cmd string) *RiskAssessment {
	ra := &RiskAssessment{
		Factors: make([]string, 0),
		Score:   0,
	}

	lower := strings.ToLower(cmd)
	pc := parseCommand(cmd)

	// --- Bypass detection ---
	for pattern, btype := range bypassPatterns {
		if matched, _ := regexp.MatchString(pattern, lower); matched {
			ra.BypassAttempt = true
			ra.BypassType = btype
			ra.Factors = append(ra.Factors, fmt.Sprintf("bypass_attempt:%s", btype))
			ra.Score += 25
		}
	}

	// --- Pipe chain danger detection ---
	if pc.HasPipe && len(pc.PipeSegments) > 1 {
		lastCmd := strings.TrimSpace(pc.PipeSegments[len(pc.PipeSegments)-1])
		dangerousSinks := []string{"bash", "sh", "zsh", "fish", "xargs rm", "xargs chmod", "xargs chown"}
		for _, sink := range dangerousSinks {
			if strings.HasPrefix(lastCmd, sink) {
				ra.Factors = append(ra.Factors, fmt.Sprintf("dangerous_pipe_sink:%s", sink))
				ra.Score += 30
			}
		}
	}

	// --- Category-based scoring ---
	for category, patterns := range riskCategoryPatterns {
		for _, p := range patterns {
			if strings.Contains(lower, p) {
				ra.Category = category
				ra.Factors = append(ra.Factors, fmt.Sprintf("%s:%s", category, p))
				switch category {
				case "system", "file_destruct":
					ra.Score += 40
				case "privilege_esc":
					ra.Score += 35
				case "data_access":
					ra.Score += 30
				case "network":
					ra.Score += 15
				case "package_mgmt":
					ra.Score += 10
				}
			}
		}
	}

	// --- Environment awareness: sensitive directory ---
	// 检查命令参数中是否访问敏感目录，而非检查命令字符串前缀
	if pc.Binary != "" {
		for _, arg := range pc.Args {
			for _, sd := range sensitiveDirectories {
				if strings.HasPrefix(arg, sd) {
					ra.Factors = append(ra.Factors, "sensitive_directory:"+sd)
					ra.Score += 20
					break
				}
			}
		}
		// 也检查完整命令中是否有空格后跟敏感目录路径
		for _, sd := range sensitiveDirectories {
			if strings.Contains(lower, " "+sd) || strings.Contains(lower, "="+sd) {
				ra.Factors = append(ra.Factors, "sensitive_directory:"+sd)
				ra.Score += 20
				break
			}
		}
	}

	// --- 重定向到敏感文件检测 ---
	if strings.Contains(lower, ">") || strings.Contains(lower, ">>") {
		sensitiveTargets := []string{"/etc/passwd", "/etc/shadow", "/etc/sudoers", "/boot/", "/sys/", "/proc/"}
		for _, st := range sensitiveTargets {
			if strings.Contains(lower, st) {
				ra.Factors = append(ra.Factors, "redirect_to_sensitive:"+st)
				ra.Score += 40
				break
			}
		}
	}

	// --- Determine level from score ---
	if ra.Score >= 70 {
		ra.Level = RiskCritical
	} else if ra.Score >= 50 {
		ra.Level = RiskHigh
	} else if ra.Score >= 25 {
		ra.Level = RiskMedium
	} else {
		ra.Level = RiskLow
	}

	return ra
}

// ---------------------------------------------------------------------------
// RequestApproval asks for approval of a command (enhanced)
// ---------------------------------------------------------------------------

// RequestApproval asks for approval of a command.
func (m *Manager) RequestApproval(req *ApprovalRequest) (*ApprovalResult, error) {
	req.Timestamp = time.Now()
	start := time.Now()
	// 在入口处快照配置，避免后续多处解锁读 m.config 造成的竞态。
	cfg := m.GetConfig()

	// 1. Check dangerous patterns (always deny)
	if m.isDangerous(req.Command) {
		result := &ApprovalResult{
			Approved:  false,
			Strategy:  cfg.Strategy,
			Reason:    "Command matches dangerous pattern",
			RiskLevel: RiskCritical,
		}
		m.recordDecision(req, result, start)
		return result, nil
	}

	// 2. Check whitelist
	if cfg.EnableWhitelist && m.isWhitelisted(req.Command) {
		result := &ApprovalResult{
			Approved: true,
			Strategy: StrategyWhitelist,
			Reason:   "Command is whitelisted",
			Trusted:  true,
		}
		m.recordDecision(req, result, start)
		return result, nil
	}

	// 3. Calculate risk level via enhanced engine
	assessment := m.assessRisk(req.Command)
	req.RiskLevel = assessment.Level
	req.Category = assessment.Category

	// 4. Check learned patterns
	if cfg.EnableLearning {
		hash := m.hashPattern(req.Command)
		m.mu.RLock()
		pattern, exists := m.patterns[hash]
		m.mu.RUnlock()
		if exists {
			// 受信 pattern 仅在 smart 策略下自动放行；manual/whitelist 策略
			// 要求每条都问或只认白名单，不应被受信 pattern 旁路。
			// 此外 High/Critical 风险命令即使受信也需重新确认，避免一次批准
			// 后高危命令被永久静默放行。
			if pattern.Trusted && cfg.Strategy == StrategySmart && req.RiskLevel < RiskHigh {
				result := &ApprovalResult{
					Approved:  true,
					Strategy:  StrategySmart,
					Reason:    "Trusted command pattern",
					Trusted:   true,
					RiskLevel: req.RiskLevel,
					Pattern:   pattern,
				}
				m.recordDecision(req, result, start)
				return result, nil
			}
			if pattern.Action == "denied" && pattern.Count >= cfg.DenylistThreshold {
				// 历史上被多次拒绝的 pattern 不再静默拒绝，改为询问用户。
				// 静默拒绝会导致用户永远无法重新批准该命令（审批卡片不弹出），
				// 与"所有命令需要审批"的预期不符。仍保留 denied 标记用于风险提示。
				result := &ApprovalResult{
					Approved: false,
					Strategy: StrategySmart,
					Reason:   "Command pattern previously denied — confirm to proceed",
					AskUser:  true,
					Pattern:  pattern,
				}
				return result, nil
			}
		}
	}

	// 5. Strategy-based decision
	var result *ApprovalResult
	switch cfg.Strategy {
	case StrategyAutoApprove:
		result = m.autoApprove(req)
	case StrategyManual:
		result = m.manualApprove(req)
	case StrategyWhitelist:
		// 白名单策略：仅放行白名单命令（已在第2步检查），其余一律拒绝
		result = &ApprovalResult{
			Approved: false,
			Strategy: StrategyWhitelist,
			Reason:   "Command not in whitelist",
			AskUser:  false,
		}
	case StrategySmart:
		result = m.smartApprove(req, assessment)
	default:
		result = m.smartApprove(req, assessment)
	}

	// 仅在策略给出最终决策（无需询问用户）时记录历史；AskUser=true 的决策
	// 会在后续 Approve/Deny/超时时由 recordDecision 记录，避免在此提前记一条
	// 假的 "denied" 污染历史与统计。
	if !result.AskUser {
		m.recordDecision(req, result, start)
	}
	return result, nil
}

// autoApprove approves all commands except dangerous patterns.
// StrategyAutoApprove means "trust me, I'm an expert" — only dangerous patterns are blocked.
func (m *Manager) autoApprove(req *ApprovalRequest) *ApprovalResult {
	return &ApprovalResult{
		Approved: true,
		Strategy: StrategyAutoApprove,
		Reason:   "Auto-approve strategy",
		AskUser:  false,
	}
}

// manualApprove requires explicit user confirmation.
// manual 策略下所有命令都应询问用户（Web 模式走审批卡片，CLI 模式走交互确认），
// 不因 EnableCLIConfirm 或风险等级而静默拒绝——静默拒绝会让用户看到"命令被拒"
// 却没有任何审批入口，与"所有命令需要审批"的预期不符。
// EnableCLIConfirm 仅在 agent approval hook 的 CLI 路径中决定是否阻塞 stdin。
func (m *Manager) manualApprove(req *ApprovalRequest) *ApprovalResult {
	return &ApprovalResult{
		Approved: false,
		Strategy: StrategyManual,
		Reason:   "Manual approval required",
		AskUser:  true,
	}
}

// smartApprove uses a tiered approach:
//   - Dangerous: always block (already checked in RequestApproval)
//   - Read-only / safe: auto-approve
//   - Medium risk: auto-approve if pattern known, else ask
//   - High risk: ask for confirmation
//   - Critical: always ask
func (m *Manager) smartApprove(req *ApprovalRequest, assessment *RiskAssessment) *ApprovalResult {
	// Critical risk → ask
	if req.RiskLevel >= RiskCritical {
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategySmart,
			Reason:   "Critical risk level requires confirmation",
			AskUser:  true,
		}
	}

	// Bypass attempt → ask
	if assessment.BypassAttempt {
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategySmart,
			Reason:   fmt.Sprintf("Bypass attempt detected (%s)", assessment.BypassType),
			AskUser:  true,
		}
	}

	// Read-only / safe commands → auto-approve
	if m.isReadOnlyCommand(req.Command) {
		return &ApprovalResult{
			Approved: true,
			Strategy: StrategySmart,
			Reason:   "Read-only command",
			Trusted:  true,
		}
	}

	// Low risk → auto-approve (no need to check isAllowedPattern)
	if req.RiskLevel == RiskLow {
		return &ApprovalResult{
			Approved: true,
			Strategy: StrategySmart,
			Reason:   "Low risk command",
			Trusted:  true,
		}
	}

	// Medium risk: auto-approve if pattern known and approved before
	m.mu.RLock()
	enableLearning := m.config.EnableLearning
	m.mu.RUnlock()
	if req.RiskLevel == RiskMedium && enableLearning {
		hash := m.hashPattern(req.Command)
		m.mu.RLock()
		pattern, exists := m.patterns[hash]
		m.mu.RUnlock()
		if exists {
			if pattern.Action == "approved" && pattern.Count >= 2 {
				return &ApprovalResult{
					Approved: true,
					Strategy: StrategySmart,
					Reason:   "Medium risk command approved before",
					Trusted:  true,
					Pattern:  pattern,
				}
			}
		}
		// Session learning: 3+ similar approvals this session
		ctx := m.getSessionContext(req.SessionID)
		if ctx != nil && ctx.ApprovedHash[int(req.RiskLevel)] >= 3 {
			return &ApprovalResult{
				Approved: true,
				Strategy: StrategySmart,
				Reason:   "Session learning: similar commands previously approved",
				Trusted:  true,
			}
		}
	}

	// High risk → ask
	if req.RiskLevel >= RiskHigh {
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategySmart,
			Reason:   fmt.Sprintf("High risk (%s) requires confirmation", assessment.Category),
			AskUser:  true,
		}
	}

	// Medium risk without prior approval → ask
	return &ApprovalResult{
		Approved: false,
		Strategy: StrategySmart,
		Reason:   fmt.Sprintf("Medium risk (%s) — first time, requires confirmation", assessment.Category),
		AskUser:  true,
	}
}

// ---------------------------------------------------------------------------
// Approval / Deny / Whitelist
// ---------------------------------------------------------------------------

// Approve records a user approval decision.
func (m *Manager) Approve(req *ApprovalRequest) error {
	m.mu.RLock()
	enableLearning := m.config.EnableLearning
	trustThreshold := m.config.TrustThreshold
	m.mu.RUnlock()
	if !enableLearning {
		return nil
	}

	hash := m.hashPattern(req.Command)
	m.mu.Lock()

	pattern, exists := m.patterns[hash]
	if !exists {
		pattern = &CommandPattern{
			Pattern:     req.Command,
			PatternHash: hash,
			RiskLevel:   req.RiskLevel,
		}
		m.patterns[hash] = pattern
	}

	pattern.Action = "approved"
	pattern.Count++
	pattern.LastSeen = time.Now()

	// 限制 SessionIDs 长度，避免无限增长
	// 只添加未记录的 session ID，且总数不超过 100
	if req.SessionID != "" {
		alreadyExists := false
		for _, sid := range pattern.SessionIDs {
			if sid == req.SessionID {
				alreadyExists = true
				break
			}
		}
		if !alreadyExists {
			pattern.SessionIDs = append(pattern.SessionIDs, req.SessionID)
			if len(pattern.SessionIDs) > 100 {
				pattern.SessionIDs = pattern.SessionIDs[len(pattern.SessionIDs)-100:]
			}
		}
	}

	if pattern.Count >= trustThreshold {
		pattern.Trusted = true
	}

	// Update session context
	m.updateSessionContext(req.SessionID, req.RiskLevel)
	m.patternsDirty = true
	m.mu.Unlock()

	// Save async to avoid blocking
	go m.savePatterns()

	// 记录历史，使 stats 完整
	m.recordDecision(req, &ApprovalResult{
		Approved: true,
		Strategy: StrategySmart,
		Reason:   "User approved",
		Trusted:  pattern.Trusted,
	}, time.Now())
	return nil
}

// Deny records a user denial decision.
func (m *Manager) Deny(req *ApprovalRequest) error {
	m.mu.RLock()
	enableLearning := m.config.EnableLearning
	m.mu.RUnlock()
	if !enableLearning {
		return nil
	}

	hash := m.hashPattern(req.Command)
	m.mu.Lock()

	pattern, exists := m.patterns[hash]
	if !exists {
		pattern = &CommandPattern{
			Pattern:     req.Command,
			PatternHash: hash,
			RiskLevel:   req.RiskLevel,
		}
		m.patterns[hash] = pattern
	}

	pattern.Action = "denied"
	pattern.Count++
	pattern.LastSeen = time.Now()

	if pattern.Trusted {
		pattern.Trusted = false
	}
	m.patternsDirty = true
	m.mu.Unlock()

	// Save async to avoid blocking
	go m.savePatterns()

	// 记录历史，使 stats 完整
	m.recordDecision(req, &ApprovalResult{
		Approved: false,
		Strategy: StrategySmart,
		Reason:   "User denied",
	}, time.Now())
	return nil
}

// AddToWhitelist adds a command pattern to whitelist.
// 校验 pattern，禁止过宽模式（如 .*）以防全局绕过审批。
func (m *Manager) AddToWhitelist(pattern string) error {
	// 校验 pattern 不为空
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("whitelist pattern cannot be empty")
	}
	// 禁止过宽模式
	lower := strings.ToLower(pattern)
	dangerous := []string{".*", "^.*$", ".*", "^.", ".+"}
	for _, d := range dangerous {
		if lower == d {
			return fmt.Errorf("pattern %q is too broad and would bypass all approvals", pattern)
		}
	}
	// 校验正则可编译且不会灾难性回溯
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}
	_ = re // 编译成功即可

	m.mu.Lock()
	m.whitelist[pattern] = true
	m.mu.Unlock()

	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	return m.saveWhitelist()
}

// RemovePattern 从 patterns 中直接删除指定模式的记录（不触发 Approve/Deny 副作用）。
func (m *Manager) RemovePattern(pattern string) error {
	hash := m.hashPattern(pattern)
	m.mu.Lock()
	delete(m.patterns, hash)
	m.patternsDirty = true
	m.mu.Unlock()

	go m.savePatterns()
	return nil
}

// RemovePatternByHash 按 hash 从 patterns 中删除记录。
func (m *Manager) RemovePatternByHash(hash string) error {
	m.mu.Lock()
	delete(m.patterns, hash)
	m.patternsDirty = true
	m.mu.Unlock()

	go m.savePatterns()
	return nil
}

// RemoveFromWhitelist removes a pattern from whitelist.
func (m *Manager) RemoveFromWhitelist(pattern string) error {
	m.mu.Lock()
	delete(m.whitelist, pattern)
	m.mu.Unlock()

	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	return m.saveWhitelist()
}

// GetConfig 返回当前审批配置的拷贝（调用方可安全修改，不影响内部状态）。
func (m *Manager) GetConfig() ApprovalConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.config
}

// SetStrategy updates the approval strategy (in-memory only; caller must persist to main config).
func (m *Manager) SetStrategy(s Strategy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Strategy = s
}

// SetTrustThreshold 更新信任阈值。
func (m *Manager) SetTrustThreshold(t int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.TrustThreshold = t
}

// SetEnableLearning 更新学习开关。
func (m *Manager) SetEnableLearning(b bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.EnableLearning = b
}

// SetEnableCLIConfirm 更新 CLI 确认开关。
func (m *Manager) SetEnableCLIConfirm(b bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.EnableCLIConfirm = b
}

// SetApprovalTimeout 更新审批超时（秒）。
func (m *Manager) SetApprovalTimeout(t int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.ApprovalTimeout = t
}

// SaveConfig is a no-op; config is persisted via the main config file.
func (m *Manager) SaveConfig() error {
	return nil
}

// GetWhitelist returns all whitelisted patterns.
func (m *Manager) GetWhitelist() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	patterns := make([]string, 0, len(m.whitelist))
	for p := range m.whitelist {
		patterns = append(patterns, p)
	}
	return patterns
}

// GetTrustedCommands returns all trusted command patterns.
func (m *Manager) GetTrustedCommands() []*CommandPattern {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var trusted []*CommandPattern
	for _, p := range m.patterns {
		if p.Trusted {
			trusted = append(trusted, p)
		}
	}
	return trusted
}

// GetDeniedCommands returns denied command patterns.
func (m *Manager) GetDeniedCommands() []*CommandPattern {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var denied []*CommandPattern
	for _, p := range m.patterns {
		if p.Action == "denied" {
			denied = append(denied, p)
		}
	}
	return denied
}

// ---------------------------------------------------------------------------
// Pattern matching helpers
// ---------------------------------------------------------------------------

// PatternMatchResult contains the result of a pattern match.
type PatternMatchResult struct {
	Matched   bool
	Pattern   string
	Variables map[string]string
}

// matchPattern matches a command against a pattern with variable extraction.
// 对于白名单 glob 模式（含 * 或 ?），将 glob 转为正则；对于纯正则，直接编译。
func (m *Manager) matchPattern(cmd, pattern string) *PatternMatchResult {
	result := &PatternMatchResult{
		Matched:   false,
		Pattern:   pattern,
		Variables: make(map[string]string),
	}

	// 纯正则匹配（不转义），用于 AllowedPatterns 等正则模式
	re, err := regexp.Compile(`^` + pattern + `$`)
	if err != nil {
		// 正则编译失败，回退到字面量匹配
		if pattern == cmd {
			result.Matched = true
		}
		return result
	}
	if re.MatchString(cmd) {
		result.Matched = true
		matches := re.FindStringSubmatch(cmd)
		for i, name := range re.SubexpNames() {
			if i > 0 && i < len(matches) && name != "" {
				result.Variables[name] = matches[i]
			}
		}
	}
	return result
}

// matchAnyPattern checks if command matches any of the given patterns.
func (m *Manager) matchAnyPattern(cmd string, patterns []string) *PatternMatchResult {
	for _, pattern := range patterns {
		result := m.matchPattern(cmd, pattern)
		if result.Matched {
			return result
		}
	}
	return &PatternMatchResult{Matched: false}
}

// matchDangerousPattern 检查命令是否匹配危险模式（无锚点正则，匹配子串即可）。
func (m *Manager) matchDangerousPattern(cmd string) bool {
	m.mu.RLock()
	patterns := m.config.DangerousPatterns
	m.mu.RUnlock()
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if re.MatchString(cmd) {
			return true
		}
	}
	return false
}

// isDangerous 检查命令（含管道和链式命令的每个段）是否匹配危险模式。
func (m *Manager) isDangerous(cmd string) bool {
	// 检查完整命令
	if m.matchDangerousPattern(cmd) {
		return true
	}
	// 解析管道和链式段，逐段检查
	pc := parseCommand(cmd)
	for _, seg := range pc.PipeSegments {
		if m.matchDangerousPattern(seg) {
			return true
		}
	}
	for _, seg := range pc.ChainSegments {
		if m.matchDangerousPattern(seg) {
			return true
		}
	}
	return false
}

// isWhitelisted checks if command is whitelisted.
func (m *Manager) isWhitelisted(cmd string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for pattern := range m.whitelist {
		if m.matchPattern(cmd, pattern).Matched {
			return true
		}
	}
	return false
}

// isAllowedPattern checks if command matches allowed patterns.
func (m *Manager) isAllowedPattern(cmd string) bool {
	m.mu.RLock()
	patterns := append([]string(nil), m.config.AllowedPatterns...)
	m.mu.RUnlock()
	return m.matchAnyPattern(cmd, patterns).Matched
}

// readOnlyCommands are commands that only read data and never modify the system.
var readOnlyCommands = []string{
	"ls", "pwd", "whoami", "date", "cat", "head", "tail", "grep", "find",
	"which", "whereis", "file", "stat", "echo", "printf", "env", "id",
	"uname", "hostname", "df", "du", "free", "top", "ps", "pgrep",
	"git status", "git log", "git diff", "git show", "git branch",
	"git remote", "git config --list",
}

// isReadOnlyCommand returns true if the command is known to be read-only.
// 先检查重定向操作符（>, >>, <, tee, dd），有则不是 read-only。
func (m *Manager) isReadOnlyCommand(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))

	// 检查重定向操作符 — 有重定向的命令不是 read-only
	redirPatterns := []string{">", ">>", "<", "<<", "tee", "dd "}
	for _, rp := range redirPatterns {
		if strings.Contains(lower, rp) {
			return false
		}
	}

	// Extract the binary (first token)
	tokens := strings.Fields(lower)
	if len(tokens) == 0 {
		return false
	}
	binary := tokens[0]

	// Check simple read-only binaries
	for _, ro := range readOnlyCommands {
		if binary == ro {
			return true
		}
	}

	// Check git read-only subcommands
	if binary == "git" && len(tokens) >= 2 {
		roGit := map[string]bool{
			"status": true, "log": true, "diff": true, "show": true,
			"branch": true, "remote": true, "config": true,
		}
		if roGit[tokens[1]] {
			// git config 只有 --list 或 get 操作才是 read-only
			if tokens[1] == "config" {
				hasSet := false
				for _, t := range tokens[2:] {
					if t == "--global" || t == "--local" || t == "--system" {
						continue
					}
					if strings.Contains(t, "=") || t == "--add" || t == "--unset" || t == "--replace-all" {
						hasSet = true
						break
					}
				}
				if hasSet {
					return false
				}
			}
			return true
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// CLI confirmation
// ---------------------------------------------------------------------------

// CLIConfirm prompts user for confirmation in terminal.
func (m *Manager) CLIConfirm(req *ApprovalRequest) (bool, error) {
	m.mu.RLock()
	enableCLI := m.config.EnableCLIConfirm
	m.mu.RUnlock()
	if !enableCLI {
		return false, nil
	}

	fmt.Printf("\n⚠️  Command requires approval\n")
	fmt.Printf("   Command: %s\n", req.Command)
	fmt.Printf("   Risk Level: %s\n", req.RiskLevel)
	fmt.Printf("   Working Dir: %s\n", req.WorkingDir)
	fmt.Print("   [A]pprove / [D]eny / [T]rust this pattern / [Q]uit: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	input = strings.TrimSpace(strings.ToLower(input))
	switch input {
	case "a":
		return true, nil
	case "d", "": // 空输入默认拒绝，防止误操作
		return false, nil
	case "t":
		m.AddToWhitelist(req.Command)
		return true, nil
	case "q":
		return false, fmt.Errorf("user requested to quit")
	default:
		return false, nil
	}
}

// ---------------------------------------------------------------------------
// Approval history
// ---------------------------------------------------------------------------

// recordDecision records an approval decision to the history.
func (m *Manager) recordDecision(req *ApprovalRequest, result *ApprovalResult, start time.Time) {
	duration := time.Since(start).Milliseconds()

	decision := "denied"
	if result.Approved {
		if result.Trusted {
			decision = "auto_approved"
		} else {
			decision = "approved"
		}
	}

	// 如果 req.RiskLevel 未设置（危险/白名单早退路径），使用 result.RiskLevel
	riskLevel := req.RiskLevel
	if riskLevel < RiskLow || riskLevel > RiskCritical {
		riskLevel = result.RiskLevel
	}
	if riskLevel < RiskLow || riskLevel > RiskCritical {
		riskLevel = RiskLow
	}

	record := &ApprovalRecord{
		ID:         uuid.New().String(),
		Command:    req.Command,
		Normalized: normalizeCommand(req.Command),
		RiskLevel:  riskLevel,
		RiskScore:  float64(riskLevel) * 25, // simplified score
		Category:   req.Category,
		Decision:   decision,
		Strategy:   result.Strategy,
		Reason:     result.Reason,
		SessionID:  req.SessionID,
		WorkingDir: req.WorkingDir,
		Duration:   duration,
		Timestamp:  time.Now(),
	}

	m.mu.Lock()
	m.history = append(m.history, record)
	// Keep history bounded to last 10000 records
	// 用 make + copy 避免底层数组内存泄漏
	if len(m.history) > 10000 {
		newHistory := make([]*ApprovalRecord, 5000)
		copy(newHistory, m.history[len(m.history)-5000:])
		m.history = newHistory
	}
	m.historyDirty = true
	m.mu.Unlock()

	// 失效 stats 缓存
	m.invalidateStatsCache()

	// 异步保存历史，避免阻塞热路径
	go m.saveHistory()
}

// RecordDecision records a specific approval decision to history (public API).
func (m *Manager) RecordDecision(req *ApprovalRequest, result string, duration int64) {
	decision := result
	if decision == "" {
		decision = "approved"
	}

	m.mu.RLock()
	strategy := m.config.Strategy
	m.mu.RUnlock()

	record := &ApprovalRecord{
		ID:         uuid.New().String(),
		Command:    req.Command,
		Normalized: normalizeCommand(req.Command),
		RiskLevel:  req.RiskLevel,
		RiskScore:  float64(req.RiskLevel) * 25,
		Decision:   decision,
		Strategy:   strategy,
		Reason:     "manual recording",
		SessionID:  req.SessionID,
		WorkingDir: req.WorkingDir,
		Duration:   duration,
		Timestamp:  time.Now(),
	}

	m.mu.Lock()
	m.history = append(m.history, record)
	m.mu.Unlock()

	m.invalidateStatsCache()
	_ = m.saveHistory()
}

// GetHistory returns approval history with pagination.
//
// m.history 按时间戳升序排列（索引 0 最旧，末尾最新）。
// 分页从最新端开始：offset=0 返回最近的 limit 条记录，
// offset=limit 返回更早的下一页，依次类推。
// 返回结果按时间倒序（最新在前）。
func (m *Manager) GetHistory(limit int, offset int) []*ApprovalRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.history)
	if offset >= total || limit <= 0 {
		return nil
	}

	// 从最新端（切片末尾）向前分页，保证 offset=0 取到最新记录。
	end := total - offset // newest-side exclusive bound
	start := end - limit  // older-side inclusive bound
	if start < 0 {
		start = 0
	}

	result := make([]*ApprovalRecord, 0, end-start)
	for i := end - 1; i >= start; i-- {
		result = append(result, m.history[i])
	}
	return result
}

// HistoryLen returns the total number of history records.
func (m *Manager) HistoryLen() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.history)
}

// GetStats returns aggregated approval statistics with caching.
// The cache is invalidated when history changes (RecordDecision, ClearHistory).
func (m *Manager) GetStats() *ApprovalStats {
	m.statsCacheMu.Lock()
	defer m.statsCacheMu.Unlock()

	// Return cached stats if still valid (5 second TTL)
	if m.statsCache != nil && time.Since(m.statsCacheTime) < 5*time.Second {
		return m.statsCache
	}

	m.mu.RLock()
	history := make([]*ApprovalRecord, len(m.history))
	copy(history, m.history)
	patterns := make(map[string]*CommandPattern, len(m.patterns))
	for k, v := range m.patterns {
		patterns[k] = v
	}
	m.mu.RUnlock()

	stats := &ApprovalStats{
		ByRiskLevel: make(map[RiskLevel]int),
		ByCategory:  make(map[string]int),
	}

	totalTime := int64(0)
	cmdMap := make(map[string]*CommandStat)

	for _, rec := range history {
		stats.TotalRequests++
		stats.ByRiskLevel[rec.RiskLevel]++
		if rec.Category != "" {
			stats.ByCategory[rec.Category]++
		}
		totalTime += rec.Duration

		switch rec.Decision {
		case "auto_approved":
			stats.AutoApproved++
		case "approved":
			stats.UserApproved++
		case "denied":
			stats.UserDenied++
		case "timeout":
			stats.TimedOut++
		}

		cs, exists := cmdMap[rec.Normalized]
		if !exists {
			cs = &CommandStat{Pattern: rec.Normalized, RiskLevel: rec.RiskLevel, LastSeen: rec.Timestamp}
			cmdMap[rec.Normalized] = cs
		}
		cs.Count++
		if rec.Timestamp.After(cs.LastSeen) {
			cs.LastSeen = rec.Timestamp
			cs.RiskLevel = rec.RiskLevel
		}
		if rec.Decision == "approved" || rec.Decision == "auto_approved" {
			cs.Approved++
		} else {
			cs.Denied++
		}
	}

	for _, p := range patterns {
		if p.Trusted {
			stats.TrustedPatterns++
		}
		if p.Action == "denied" {
			stats.DeniedPatterns++
		}
	}

	var cmdStats []CommandStat
	for _, cs := range cmdMap {
		cmdStats = append(cmdStats, *cs)
	}
	sort.Slice(cmdStats, func(i, j int) bool {
		return cmdStats[i].Count > cmdStats[j].Count
	})
	if len(cmdStats) > 10 {
		cmdStats = cmdStats[:10]
	}
	stats.TopCommands = cmdStats

	if stats.TotalRequests > 0 {
		stats.AvgResponseTime = float64(totalTime) / float64(stats.TotalRequests)
	}

	m.statsCache = stats
	m.statsCacheTime = time.Now()
	return stats
}

// invalidateStatsCache clears the stats cache. Call after any history mutation.
func (m *Manager) invalidateStatsCache() {
	m.statsCacheMu.Lock()
	m.statsCache = nil
	m.statsCacheMu.Unlock()
}

// ClearHistory removes approval records older than the given duration.
// 同时清理过期的 session 上下文和非 trusted 的 stale patterns。
func (m *Manager) ClearHistory(olderThan time.Duration) {
	cutoff := time.Now().Add(-olderThan)

	m.mu.Lock()
	var filtered []*ApprovalRecord
	for _, rec := range m.history {
		if rec.Timestamp.After(cutoff) {
			filtered = append(filtered, rec)
		}
	}
	m.history = filtered
	m.historyDirty = true

	// 清理过期的 session 上下文
	for sid, ctx := range m.sessionContext {
		if ctx.CreatedAt.Before(cutoff) {
			delete(m.sessionContext, sid)
		}
	}

	// 清理非 trusted 且 LastSeen 早于 cutoff 的 stale patterns
	for hash, p := range m.patterns {
		if !p.Trusted && p.LastSeen.Before(cutoff) {
			delete(m.patterns, hash)
		}
	}
	m.patternsDirty = true
	m.mu.Unlock()

	m.invalidateStatsCache()
	go m.saveHistory()
	go m.savePatterns()
}

// ---------------------------------------------------------------------------
// Session context management
// ---------------------------------------------------------------------------

// getSessionContext returns the session context for a given session ID.
func (m *Manager) getSessionContext(sessionID string) *SessionApprovalContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionContext[sessionID]
}

// updateSessionContext updates session context after an approval.
// updateSessionContext updates session context. Caller must hold m.mu.
func (m *Manager) updateSessionContext(sessionID string, riskLevel RiskLevel) {
	ctx, exists := m.sessionContext[sessionID]
	if !exists {
		ctx = &SessionApprovalContext{
			SessionID:    sessionID,
			ApprovedHash: make(map[int]int),
			CreatedAt:    time.Now(),
		}
		m.sessionContext[sessionID] = ctx
	}
	ctx.ApprovedHash[int(riskLevel)]++
}

// ---------------------------------------------------------------------------
// Web approval callbacks
// ---------------------------------------------------------------------------

// PendingWebApproval creates a pending approval that waits for web resolution.
// 兼容旧调用方：内部调用 CreatePendingApproval + WaitPendingApproval。
func (m *Manager) PendingWebApproval(req *ApprovalRequest) (*ApprovalResult, error) {
	pa := m.CreatePendingApproval(req)
	return m.WaitPendingApproval(pa)
}

// CreatePendingApproval 创建一个待审批请求并返回，不阻塞。
// 调用方可在此之后触发 OnPendingCreated 回调，再调用 WaitPendingApproval 等待结果。
// 这种拆分让 ApprovalHook 能在"创建后、等待前"拿到 id 通知 SSE 流。
func (m *Manager) CreatePendingApproval(req *ApprovalRequest) *PendingApproval {
	id := uuid.New().String()
	m.mu.RLock()
	timeout := time.Duration(m.config.ApprovalTimeout) * time.Second
	m.mu.RUnlock()
	// 兜底：若 ApprovalTimeout 被错误地设为 0 或负值（如配置文件漏写），
	// 使用 60s 默认值，避免 pending 审批立即过期导致用户来不及操作。
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	pa := &PendingApproval{
		ID:        id,
		Request:   req,
		Result:    make(chan *ApprovalResult, 1),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(timeout),
	}

	m.webCallback.mu.Lock()
	m.webCallback.pendingApprovals[id] = pa
	onCreated := m.webCallback.onCreated
	m.webCallback.mu.Unlock()

	// 触发回调，通知外部（SSE 流）有新的待审批
	if onCreated != nil {
		onCreated(PendingApprovalInfo{
			ID:         pa.ID,
			Command:    req.Command,
			SessionID:  req.SessionID,
			WorkingDir: req.WorkingDir,
			RiskLevel:  req.RiskLevel.String(),
			Reason:     req.Reason,
			Context:    req.Context,
			CreatedAt:  pa.CreatedAt,
			ExpiresAt:  pa.ExpiresAt,
		})
	}

	return pa
}

// WaitPendingApproval 阻塞等待待审批结果（被 Resolve 或超时）。
func (m *Manager) WaitPendingApproval(pa *PendingApproval) (*ApprovalResult, error) {
	timeout := time.Until(pa.ExpiresAt)
	if timeout <= 0 {
		m.webCallback.mu.Lock()
		delete(m.webCallback.pendingApprovals, pa.ID)
		m.webCallback.mu.Unlock()
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategyManual,
			Reason:   "Web approval timed out",
		}, nil
	}

	select {
	case result := <-pa.Result:
		m.webCallback.mu.Lock()
		delete(m.webCallback.pendingApprovals, pa.ID)
		m.webCallback.mu.Unlock()
		return result, nil
	case <-time.After(timeout):
		m.webCallback.mu.Lock()
		delete(m.webCallback.pendingApprovals, pa.ID)
		m.webCallback.mu.Unlock()
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategyManual,
			Reason:   "Web approval timed out",
		}, nil
	}
}

// ResolveWebApproval resolves a pending web approval.
// 返回 error 以便 handler 向前端返回正确的错误状态。
func (m *Manager) ResolveWebApproval(id string, approved bool, reason string) error {
	m.webCallback.mu.Lock()
	pa, exists := m.webCallback.pendingApprovals[id]
	m.webCallback.mu.Unlock()

	if !exists {
		return fmt.Errorf("pending approval %s not found", id)
	}

	// 检查是否已过期
	if time.Now().After(pa.ExpiresAt) {
		m.webCallback.mu.Lock()
		delete(m.webCallback.pendingApprovals, id)
		m.webCallback.mu.Unlock()
		return fmt.Errorf("pending approval %s has expired", id)
	}

	decision := "approved"
	if !approved {
		decision = "denied"
	}
	if reason == "" {
		reason = "Web approval: " + decision
	}

	result := &ApprovalResult{
		Approved: approved,
		Strategy: StrategyManual,
		Reason:   reason,
	}

	// 非阻塞 send，防止重复 resolve 导致 goroutine 泄漏
	select {
	case pa.Result <- result:
	default:
		// channel 已满（已被 resolve 或 timeout），安全忽略
	}

	return nil
}

// GetPendingApprovals returns all pending web approvals.
func (m *Manager) GetPendingApprovals() []*PendingApproval {
	m.webCallback.mu.Lock()
	defer m.webCallback.mu.Unlock()

	result := make([]*PendingApproval, 0, len(m.webCallback.pendingApprovals))
	for _, pa := range m.webCallback.pendingApprovals {
		result = append(result, pa)
	}
	return result
}

// CleanupExpired removes expired pending approvals.
func (m *Manager) CleanupExpired() {
	m.webCallback.mu.Lock()
	defer m.webCallback.mu.Unlock()

	now := time.Now()
	for id, pa := range m.webCallback.pendingApprovals {
		if now.After(pa.ExpiresAt) {
			pa.Result <- &ApprovalResult{
				Approved: false,
				Strategy: StrategyManual,
				Reason:   "Approval expired",
			}
			delete(m.webCallback.pendingApprovals, id)
		}
	}
}

// ---------------------------------------------------------------------------
// Callbacks and notifications
// ---------------------------------------------------------------------------

// RegisterCallback registers an approval callback.
func (m *Manager) RegisterCallback(cb ApprovalCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, cb)
}

// NotifyApproval notifies all callbacks of an approval result.
func (m *Manager) NotifyApproval(result *ApprovalResult, req *ApprovalRequest) {
	m.mu.RLock()
	cbs := make([]ApprovalCallback, len(m.callbacks))
	copy(cbs, m.callbacks)
	m.mu.RUnlock()

	for _, cb := range cbs {
		cb.OnApproval(result, req)
	}
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

// loadPatterns loads patterns from disk.
func (m *Manager) loadPatterns() {
	data, err := os.ReadFile(m.patternsDB)
	if err != nil {
		return
	}
	var patterns []*CommandPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		return
	}
	for _, p := range patterns {
		m.patterns[p.PatternHash] = p
	}
}

// savePatterns saves patterns to disk if dirty.
// 在同一个 Lock 下读取 snapshot 并清除 dirty flag，避免 lost-update。
func (m *Manager) savePatterns() error {
	m.mu.RLock()
	if !m.patternsDirty {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	m.saveMu.Lock()
	defer m.saveMu.Unlock()

	// 在同一个 Lock 下读取 snapshot 并清除 dirty flag，保证原子性
	m.mu.Lock()
	if !m.patternsDirty {
		m.mu.Unlock()
		return nil
	}
	var patterns []*CommandPattern
	for _, p := range m.patterns {
		patterns = append(patterns, p)
	}
	m.patternsDirty = false
	m.mu.Unlock()

	// 确保目录存在
	dir := filepath.Dir(m.patternsDB)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(patterns, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.patternsDB, data, 0644)
}

// loadWhitelist loads whitelist from disk.
func (m *Manager) loadWhitelist() {
	wlPath := filepath.Join(configpkg.GetMagicHome(), "approval", "whitelist.txt")

	data, err := os.ReadFile(wlPath)
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			m.whitelist[line] = true
		}
	}
}

// saveWhitelist saves whitelist to disk.
func (m *Manager) saveWhitelist() error {
	wlPath := filepath.Join(configpkg.GetMagicHome(), "approval", "whitelist.txt")

	// Ensure directory exists
	dir := filepath.Dir(wlPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var lines []string
	for pattern := range m.whitelist {
		lines = append(lines, pattern)
	}
	return os.WriteFile(wlPath, []byte(strings.Join(lines, "\n")), 0644)
}

// loadHistory loads approval history from disk.
func (m *Manager) loadHistory() {
	data, err := os.ReadFile(m.historyDB)
	if err != nil {
		return
	}
	var records []*ApprovalRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return
	}
	// 按时间升序排序，保证 m.history 末尾是最新记录。
	// 历史文件可能因并发保存或旧版本写入导致顺序错乱，
	// GetHistory 依赖物理顺序倒序取，不排序会返回错误的"最新"记录。
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
	m.history = records
}

// saveHistory saves approval history to disk if dirty.
func (m *Manager) saveHistory() error {
	// 检查 dirty flag
	m.mu.RLock()
	if !m.historyDirty {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	m.saveMu.Lock()
	defer m.saveMu.Unlock()

	// 在同一个 Lock 下读取 snapshot 并清除 dirty flag，避免 lost-update
	m.mu.Lock()
	if !m.historyDirty {
		m.mu.Unlock()
		return nil
	}
	records := make([]*ApprovalRecord, len(m.history))
	copy(records, m.history)
	m.historyDirty = false
	m.mu.Unlock()

	// 确保目录存在
	dir := filepath.Dir(m.historyDB)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.historyDB, data, 0644)
}

// ---------------------------------------------------------------------------
// CLI commands
// ---------------------------------------------------------------------------

// ConfigCommand returns CLI commands for approval management.
func (m *Manager) ConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approval",
		Short: "Manage command approval settings",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list-trusted",
			Short: "List trusted command patterns",
			RunE: func(cmd *cobra.Command, args []string) error {
				trusted := m.GetTrustedCommands()
				if len(trusted) == 0 {
					fmt.Println("No trusted patterns.")
					return nil
				}
				fmt.Println("Trusted command patterns:")
				for _, p := range trusted {
					fmt.Printf("  %s (count: %d, last seen: %s)\n",
						p.Pattern, p.Count, p.LastSeen.Format("2006-01-02"))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "list-denied",
			Short: "List denied command patterns",
			RunE: func(cmd *cobra.Command, args []string) error {
				denied := m.GetDeniedCommands()
				if len(denied) == 0 {
					fmt.Println("No denied patterns.")
					return nil
				}
				fmt.Println("Denied command patterns:")
				for _, p := range denied {
					fmt.Printf("  %s (count: %d)\n", p.Pattern, p.Count)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "whitelist [pattern]",
			Short: "Add pattern to whitelist",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return m.AddToWhitelist(args[0])
			},
		},
		&cobra.Command{
			Use:   "set-strategy [manual|auto|smart|whitelist]",
			Short: "Set approval strategy",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				var s Strategy
				switch args[0] {
				case "manual":
					s = StrategyManual
				case "auto":
					s = StrategyAutoApprove
				case "smart":
					s = StrategySmart
				case "whitelist":
					s = StrategyWhitelist
				default:
					return fmt.Errorf("unknown strategy: %s", args[0])
				}
				m.SetStrategy(s)
				viper.Set("approval.strategy", string(s))
				return viper.WriteConfig()
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show approval system status",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg := m.GetConfig()
				stats := m.GetStats()
				fmt.Printf("Strategy: %s\n", cfg.Strategy)
				fmt.Printf("Learning: %v\n", cfg.EnableLearning)
				fmt.Printf("CLI Confirm: %v\n", cfg.EnableCLIConfirm)
				fmt.Printf("Trust Threshold: %d\n", cfg.TrustThreshold)
				fmt.Printf("Trusted Patterns: %d\n", stats.TrustedPatterns)
				fmt.Printf("Denied Patterns: %d\n", stats.DeniedPatterns)
				fmt.Printf("Total Requests: %d\n", stats.TotalRequests)
				fmt.Printf("Auto Approved: %d\n", stats.AutoApproved)
				fmt.Printf("User Approved: %d\n", stats.UserApproved)
				fmt.Printf("User Denied: %d\n", stats.UserDenied)
				fmt.Printf("Avg Response Time: %.1fms\n", stats.AvgResponseTime)
				return nil
			},
		},
		&cobra.Command{
			Use:   "history",
			Short: "Show recent approval history",
			RunE: func(cmd *cobra.Command, args []string) error {
				records := m.GetHistory(20, 0)
				if len(records) == 0 {
					fmt.Println("No approval history.")
					return nil
				}
				fmt.Println("Recent approval history:")
				for _, rec := range records {
					fmt.Printf("  [%s] %s → %s (risk: %s, %dms)\n",
						rec.Timestamp.Format("15:04:05"),
						truncateCmd(rec.Command, 50),
						rec.Decision,
						rec.RiskLevel,
						rec.Duration,
					)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "stats",
			Short: "Show approval statistics",
			RunE: func(cmd *cobra.Command, args []string) error {
				stats := m.GetStats()
				fmt.Printf("Total: %d | Auto: %d | Approved: %d | Denied: %d | Timeout: %d\n",
					stats.TotalRequests, stats.AutoApproved,
					stats.UserApproved, stats.UserDenied, stats.TimedOut)
				if len(stats.TopCommands) > 0 {
					fmt.Println("\nTop commands:")
					for _, cs := range stats.TopCommands {
						fmt.Printf("  %s (total: %d, approved: %d, denied: %d)\n",
							cs.Pattern, cs.Count, cs.Approved, cs.Denied)
					}
				}
				return nil
			},
		},
	)

	return cmd
}

// truncateCmd truncates a command string for display.
// 使用 []rune 切割，避免截断多字节 UTF-8 字符。
func truncateCmd(cmd string, maxLen int) string {
	runes := []rune(cmd)
	if len(runes) <= maxLen {
		return cmd
	}
	return string(runes[:maxLen-3]) + "..."
}

// SyncWithMemory syncs patterns with memory store.
func (m *Manager) SyncWithMemory() error {
	// Integrates with the memory store to record command history and learn from it.
	// Placeholder for future memory store integration.
	return nil
}
