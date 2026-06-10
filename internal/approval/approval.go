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
	Pattern  string `json:"pattern"`
	Count    int    `json:"count"`
	Approved int    `json:"approved"`
	Denied   int    `json:"denied"`
}

// ---------------------------------------------------------------------------
// WebApprovalCallback / PendingApproval Web端审批回调
// ---------------------------------------------------------------------------

// WebApprovalCallback Web端审批回调.
type WebApprovalCallback struct {
	pendingApprovals map[string]*PendingApproval
	mu               sync.Mutex
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
	Reason     string
	Timestamp  time.Time
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
		TrustThreshold:    1,
		DenylistThreshold: 2,
		EnableLearning:    true,
		EnableWhitelist:   true,
		EnableCLIConfirm:  false,
		GatewayEnabled:    false,
		DangerousPatterns: []string{
			`rm\s+-rf\s+/(?:\*|$)`,
			`rm\s+-rf\s+/\*\s*$`,
			`dd\s+if=.*of=/dev/sd`,
			`mkfs\.`,
			`shutdown\s+-h\s+now`,
			`reboot`,
			`:\(\)\{:\|\:&\};:`,
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
}

// NewManager creates a new approval manager.
func NewManager(config *ApprovalConfig) (*Manager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	home, _ := os.UserHomeDir()
	dbDir := filepath.Join(home, ".magic", "approval")
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

	// Detect pipes and chains
	if strings.Contains(trimmed, "|") {
		pc.HasPipe = true
		pc.PipeSegments = splitSegments(trimmed, '|')
	}
	if strings.Contains(trimmed, "&&") || strings.Contains(trimmed, "||") || strings.Contains(trimmed, "; ") {
		pc.HasChain = true
		pc.ChainSegments = splitChainSegments(trimmed)
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
			}
			current.WriteRune(ch)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			current.WriteRune(ch)
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
var bypassPatterns = map[string]string{
	`base64\s+-d`:           "encoding",
	`xxd\s+-r`:              "encoding",
	`\$\{?\w+\}?`:           "variable",
	`\$\(\s*\w+`:            "variable_subshell",
	`\.\./`:                 "path_traversal",
	`/proc/self/`:           "path_traversal",
	`eval\s+`:               "encoding",
	`echo\s+.*\|\s*(ba)?sh`: "encoding",
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
	if pc.Binary != "" {
		for _, sd := range sensitiveDirectories {
			if strings.HasPrefix(lower, sd) || strings.Contains(lower, " "+sd) {
				ra.Factors = append(ra.Factors, "sensitive_directory:"+sd)
				ra.Score += 20
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

	// 1. Check dangerous patterns (always deny)
	if m.isDangerous(req.Command) {
		result := &ApprovalResult{
			Approved:  false,
			Strategy:  m.config.Strategy,
			Reason:    "Command matches dangerous pattern",
			RiskLevel: RiskCritical,
		}
		m.recordDecision(req, result, start)
		return result, nil
	}

	// 2. Check whitelist
	if m.config.EnableWhitelist && m.isWhitelisted(req.Command) {
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

	// 4. Check learned patterns
	if m.config.EnableLearning {
		hash := m.hashPattern(req.Command)
		if pattern, exists := m.patterns[hash]; exists {
			if pattern.Trusted {
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
			if pattern.Action == "denied" && pattern.Count >= m.config.DenylistThreshold {
				result := &ApprovalResult{
					Approved: false,
					Strategy: StrategySmart,
					Reason:   "Command pattern has been denied multiple times",
					Pattern:  pattern,
				}
				m.recordDecision(req, result, start)
				return result, nil
			}
		}
	}

	// 5. Strategy-based decision
	var result *ApprovalResult
	switch m.config.Strategy {
	case StrategyAutoApprove:
		result = m.autoApprove(req)
	case StrategyManual:
		result = m.manualApprove(req)
	case StrategySmart:
		result = m.smartApprove(req, assessment)
	default:
		result = m.smartApprove(req, assessment)
	}

	m.recordDecision(req, result, start)
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
func (m *Manager) manualApprove(req *ApprovalRequest) *ApprovalResult {
	if !m.config.EnableCLIConfirm && req.RiskLevel >= RiskHigh {
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategyManual,
			Reason:   "CLI confirmation disabled for high-risk commands",
			AskUser:  false,
		}
	}
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
	if req.RiskLevel == RiskMedium && m.config.EnableLearning {
		hash := m.hashPattern(req.Command)
		if pattern, exists := m.patterns[hash]; exists {
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
	if !m.config.EnableLearning {
		return nil
	}

	hash := m.hashPattern(req.Command)
	m.mu.Lock()
	defer m.mu.Unlock()

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
	pattern.SessionIDs = append(pattern.SessionIDs, req.SessionID)

	if pattern.Count >= m.config.TrustThreshold {
		pattern.Trusted = true
	}

	// Update session context
	m.updateSessionContext(req.SessionID, req.RiskLevel)

	return m.savePatterns()
}

// Deny records a user denial decision.
func (m *Manager) Deny(req *ApprovalRequest) error {
	if !m.config.EnableLearning {
		return nil
	}

	hash := m.hashPattern(req.Command)
	m.mu.Lock()
	defer m.mu.Unlock()

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

	return m.savePatterns()
}

// AddToWhitelist adds a command pattern to whitelist.
func (m *Manager) AddToWhitelist(pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.whitelist[pattern] = true
	return m.saveWhitelist()
}

// RemoveFromWhitelist removes a pattern from whitelist.
func (m *Manager) RemoveFromWhitelist(pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.whitelist, pattern)
	return m.saveWhitelist()
}

// GetConfig returns the current approval configuration.
func (m *Manager) GetConfig() *ApprovalConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// SetStrategy updates the approval strategy (in-memory only; caller must persist to main config).
func (m *Manager) SetStrategy(s Strategy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Strategy = s
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
func (m *Manager) matchPattern(cmd, pattern string) *PatternMatchResult {
	result := &PatternMatchResult{
		Matched:   false,
		Pattern:   pattern,
		Variables: make(map[string]string),
	}

	if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
		regexPattern := regexp.QuoteMeta(pattern)
		regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.*`)
		regexPattern = strings.ReplaceAll(regexPattern, `\?`, `.`)
		regexPattern = `^` + regexPattern + `$`

		re, err := regexp.Compile(regexPattern)
		if err != nil {
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
	} else {
		re, err := regexp.Compile(`^` + pattern + `$`)
		if err != nil {
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

// isDangerous checks if command matches dangerous patterns.
func (m *Manager) isDangerous(cmd string) bool {
	return m.matchAnyPattern(cmd, m.config.DangerousPatterns).Matched
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
	return m.matchAnyPattern(cmd, m.config.AllowedPatterns).Matched
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
func (m *Manager) isReadOnlyCommand(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))
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
	if !m.config.EnableCLIConfirm {
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
	case "a", "":
		return true, nil
	case "d":
		return false, nil
	case "t":
		m.AddToWhitelist(req.Command)
		return true, nil
	case "q":
		fmt.Println("Exiting...")
		os.Exit(0)
	default:
		return false, nil
	}
	return false, fmt.Errorf("unreachable")
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

	record := &ApprovalRecord{
		ID:         uuid.New().String(),
		Command:    req.Command,
		Normalized: normalizeCommand(req.Command),
		RiskLevel:  req.RiskLevel,
		RiskScore:  float64(req.RiskLevel) * 25, // simplified score
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
	if len(m.history) > 10000 {
		m.history = m.history[len(m.history)-5000:]
	}
	m.mu.Unlock()

	_ = m.saveHistory()
}

// RecordDecision records a specific approval decision to history (public API).
func (m *Manager) RecordDecision(req *ApprovalRequest, result string, duration int64) {
	decision := result
	if decision == "" {
		decision = "approved"
	}

	record := &ApprovalRecord{
		ID:         uuid.New().String(),
		Command:    req.Command,
		Normalized: normalizeCommand(req.Command),
		RiskLevel:  req.RiskLevel,
		RiskScore:  float64(req.RiskLevel) * 25,
		Decision:   decision,
		Strategy:   m.config.Strategy,
		Reason:     "manual recording",
		SessionID:  req.SessionID,
		WorkingDir: req.WorkingDir,
		Duration:   duration,
		Timestamp:  time.Now(),
	}

	m.mu.Lock()
	m.history = append(m.history, record)
	m.mu.Unlock()

	_ = m.saveHistory()
}

// GetHistory returns approval history with pagination.
func (m *Manager) GetHistory(limit int, offset int) []*ApprovalRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.history)
	if offset >= total {
		return nil
	}
	end := offset + limit
	if end > total {
		end = total
	}

	// Return in reverse chronological order
	result := make([]*ApprovalRecord, 0, end-offset)
	for i := end - 1; i >= offset; i-- {
		result = append(result, m.history[i])
	}
	return result
}

// GetStats returns aggregated approval statistics.
func (m *Manager) GetStats() *ApprovalStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &ApprovalStats{
		ByRiskLevel: make(map[RiskLevel]int),
		ByCategory:  make(map[string]int),
	}

	totalTime := int64(0)
	cmdMap := make(map[string]*CommandStat)

	for _, rec := range m.history {
		stats.TotalRequests++
		stats.ByRiskLevel[rec.RiskLevel]++
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

		// Aggregate by normalized command
		cs, exists := cmdMap[rec.Normalized]
		if !exists {
			cs = &CommandStat{Pattern: rec.Normalized}
			cmdMap[rec.Normalized] = cs
		}
		cs.Count++
		if rec.Decision == "approved" || rec.Decision == "auto_approved" {
			cs.Approved++
		} else {
			cs.Denied++
		}
	}

	// Count trusted/denied patterns
	for _, p := range m.patterns {
		if p.Trusted {
			stats.TrustedPatterns++
		}
		if p.Action == "denied" {
			stats.DeniedPatterns++
		}
	}

	// Top commands by count
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

	return stats
}

// ClearHistory removes approval records older than the given duration.
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
	m.mu.Unlock()

	_ = m.saveHistory()
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
func (m *Manager) updateSessionContext(sessionID string, riskLevel RiskLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()

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
func (m *Manager) PendingWebApproval(req *ApprovalRequest) (*ApprovalResult, error) {
	id := uuid.New().String()
	timeout := time.Duration(m.config.ApprovalTimeout) * time.Second

	pa := &PendingApproval{
		ID:        id,
		Request:   req,
		Result:    make(chan *ApprovalResult, 1),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(timeout),
	}

	m.webCallback.mu.Lock()
	m.webCallback.pendingApprovals[id] = pa
	m.webCallback.mu.Unlock()

	// Wait for resolution or timeout
	select {
	case result := <-pa.Result:
		m.webCallback.mu.Lock()
		delete(m.webCallback.pendingApprovals, id)
		m.webCallback.mu.Unlock()
		return result, nil
	case <-time.After(timeout):
		m.webCallback.mu.Lock()
		delete(m.webCallback.pendingApprovals, id)
		m.webCallback.mu.Unlock()
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategyManual,
			Reason:   "Web approval timed out",
		}, nil
	}
}

// ResolveWebApproval resolves a pending web approval.
func (m *Manager) ResolveWebApproval(id string, approved bool, reason string) {
	m.webCallback.mu.Lock()
	pa, exists := m.webCallback.pendingApprovals[id]
	m.webCallback.mu.Unlock()

	if !exists {
		return
	}

	decision := "approved"
	if !approved {
		decision = "denied"
	}
	if reason == "" {
		reason = "Web approval: " + decision
	}

	pa.Result <- &ApprovalResult{
		Approved: approved,
		Strategy: StrategyManual,
		Reason:   reason,
	}
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

// savePatterns saves patterns to disk.
func (m *Manager) savePatterns() error {
	// Ensure directory exists
	dir := filepath.Dir(m.patternsDB)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var patterns []*CommandPattern
	for _, p := range m.patterns {
		patterns = append(patterns, p)
	}
	data, err := json.MarshalIndent(patterns, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.patternsDB, data, 0644)
}

// loadWhitelist loads whitelist from disk.
func (m *Manager) loadWhitelist() {
	home, _ := os.UserHomeDir()
	wlPath := filepath.Join(home, ".magic", "approval", "whitelist.txt")

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
	home, _ := os.UserHomeDir()
	wlPath := filepath.Join(home, ".magic", "approval", "whitelist.txt")

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
	m.history = records
}

// saveHistory saves approval history to disk.
// Caller must NOT hold m.mu lock — this function handles its own locking.
func (m *Manager) saveHistory() error {
	// Ensure directory exists (no lock needed)
	dir := filepath.Dir(m.historyDB)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	m.mu.RLock()
	records := make([]*ApprovalRecord, len(m.history))
	copy(records, m.history)
	m.mu.RUnlock()

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
				switch args[0] {
				case "manual":
					m.config.Strategy = StrategyManual
				case "auto":
					m.config.Strategy = StrategyAutoApprove
				case "smart":
					m.config.Strategy = StrategySmart
				case "whitelist":
					m.config.Strategy = StrategyWhitelist
				default:
					return fmt.Errorf("unknown strategy: %s", args[0])
				}
				viper.Set("approval.strategy", string(m.config.Strategy))
				return viper.WriteConfig()
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show approval system status",
			RunE: func(cmd *cobra.Command, args []string) error {
				stats := m.GetStats()
				fmt.Printf("Strategy: %s\n", m.config.Strategy)
				fmt.Printf("Learning: %v\n", m.config.EnableLearning)
				fmt.Printf("CLI Confirm: %v\n", m.config.EnableCLIConfirm)
				fmt.Printf("Trust Threshold: %d\n", m.config.TrustThreshold)
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
func truncateCmd(cmd string, maxLen int) string {
	if len(cmd) <= maxLen {
		return cmd
	}
	return cmd[:maxLen-3] + "..."
}

// SyncWithMemory syncs patterns with memory store.
func (m *Manager) SyncWithMemory() error {
	// Integrates with the memory store to record command history and learn from it.
	// Placeholder for future memory store integration.
	return nil
}
