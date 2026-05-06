// Package approval provides intelligent command approval system
// inspired by Cortex Agent's Smart Approvals
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
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Strategy defines how commands are approved
type Strategy string

const (
	// StrategyManual requires user confirmation for all commands
	StrategyManual Strategy = "manual"
	// StrategyAutoApprove automatically approves trusted commands
	StrategyAutoApprove Strategy = "auto"
	// StrategySmart learns from user decisions
	StrategySmart Strategy = "smart"
	// StrategyWhitelist only allows whitelisted commands
	StrategyWhitelist Strategy = "whitelist"
)

// RiskLevel represents the danger level of a command
type RiskLevel int

const (
	RiskLow      RiskLevel = 1 // Safe commands like ls, pwd
	RiskMedium   RiskLevel = 2 // Commands that modify files
	RiskHigh     RiskLevel = 3 // Destructive commands like rm -rf
	RiskCritical RiskLevel = 4 // System-level changes
)

// CommandPattern represents a learned command pattern
type CommandPattern struct {
	Pattern     string    `json:"pattern"`
	PatternHash string    `json:"pattern_hash"`
	Action      string    `json:"action"` // approved, denied
	Count       int       `json:"count"`
	RiskLevel   RiskLevel `json:"risk_level"`
	LastSeen    time.Time `json:"last_seen"`
	SessionIDs  []string  `json:"session_ids"`
	Trusted     bool      `json:"trusted"` // Auto-approved if count exceeds threshold
}

// ApprovalRequest represents a command approval request
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

// ApprovalResult is the result of an approval decision
type ApprovalResult struct {
	Approved  bool
	Strategy  Strategy
	Reason    string
	Trusted   bool
	AskUser   bool
	RiskLevel RiskLevel
	Pattern   *CommandPattern
}

// ApprovalConfig holds approval system configuration
type ApprovalConfig struct {
	Strategy          Strategy `mapstructure:"strategy"`
	TrustThreshold    int      `mapstructure:"trust_threshold"`      // Approvals before auto-trust
	DenylistThreshold int      `mapstructure:"denylist_threshold"`   // Denials before auto-deny
	EnableLearning    bool     `mapstructure:"enable_learning"`      // Learn from decisions
	EnableWhitelist   bool     `mapstructure:"enable_whitelist"`     // Use whitelist
	EnableCLIConfirm  bool     `mapstructure:"enable_cli_confirm"`   // CLI confirmation
	GatewayEnabled    bool     `mapstructure:"gateway_enabled"`      // Send to messaging platform
	GatewayURL        string   `mapstructure:"gateway_url"`          // Gateway endpoint
	DangerousPatterns []string `mapstructure:"dangerous_patterns"`   // Always deny patterns
	AllowedPatterns   []string `mapstructure:"allowed_patterns"`     // Always allow patterns
	ApprovalTimeout   int      `mapstructure:"approval_timeout"`     // Seconds to wait for approval
	LearnFromSameUser bool     `mapstructure:"learn_from_same_user"` // Learn per user
}

// DefaultConfig returns the default approval configuration
func DefaultConfig() *ApprovalConfig {
	return &ApprovalConfig{
		Strategy:          StrategySmart,
		TrustThreshold:    3,
		DenylistThreshold: 2,
		EnableLearning:    true,
		EnableWhitelist:   true,
		EnableCLIConfirm:  true,
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
			`^(ls|pwd|whoami|echo|date|cat|head|tail|grep|find|which)$`,
			`^(cd|mkdir|ls|rmdir)$`,
			`^git\s+(status|log|diff|show|branch)$`,
		},
		ApprovalTimeout:   60,
		LearnFromSameUser: true,
	}
}

// Manager handles command approvals
type Manager struct {
	config     *ApprovalConfig
	patterns   map[string]*CommandPattern
	whitelist  map[string]bool
	mu         sync.RWMutex
	patternsDB string
	callbacks  []ApprovalCallback
}

// ApprovalCallback is called on approval decisions
type ApprovalCallback interface {
	OnApproval(result *ApprovalResult, req *ApprovalRequest)
	OnApprovalTimeout(req *ApprovalRequest)
}

// NewManager creates a new approval manager
func NewManager(config *ApprovalConfig) (*Manager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	home, _ := os.UserHomeDir()
	dbDir := filepath.Join(home, ".magic", "approval")
	os.MkdirAll(dbDir, 0755)

	m := &Manager{
		config:     config,
		patterns:   make(map[string]*CommandPattern),
		whitelist:  make(map[string]bool),
		patternsDB: filepath.Join(dbDir, "patterns.json"),
	}

	// Load existing patterns
	m.loadPatterns()

	// Load whitelist
	m.loadWhitelist()

	return m, nil
}

// RequestApproval asks for approval of a command
func (m *Manager) RequestApproval(req *ApprovalRequest) (*ApprovalResult, error) {
	req.Timestamp = time.Now()

	// First, check for dangerous patterns (always deny)
	if m.isDangerous(req.Command) {
		return &ApprovalResult{
			Approved:  false,
			Strategy:  m.config.Strategy,
			Reason:    "Command matches dangerous pattern",
			RiskLevel: RiskCritical,
		}, nil
	}

	// Check whitelist
	if m.config.EnableWhitelist && m.isWhitelisted(req.Command) {
		return &ApprovalResult{
			Approved: true,
			Strategy: StrategyWhitelist,
			Reason:   "Command is whitelisted",
			Trusted:  true,
		}, nil
	}

	// Calculate risk level
	req.RiskLevel = m.calculateRiskLevel(req.Command)

	// Check learned patterns
	if m.config.EnableLearning {
		hash := m.hashPattern(req.Command)
		if pattern, exists := m.patterns[hash]; exists {
			if pattern.Trusted {
				return &ApprovalResult{
					Approved: true,
					Strategy: StrategySmart,
					Reason:   "Trusted command pattern",
					Trusted:  true,
					Pattern:  pattern,
				}, nil
			}
			if pattern.Action == "denied" && pattern.Count >= m.config.DenylistThreshold {
				return &ApprovalResult{
					Approved: false,
					Strategy: StrategySmart,
					Reason:   "Command pattern has been denied multiple times",
					Pattern:  pattern,
				}, nil
			}
		}
	}

	// Strategy-based decision
	switch m.config.Strategy {
	case StrategyAutoApprove:
		return m.autoApprove(req), nil

	case StrategyManual:
		return m.manualApprove(req), nil

	case StrategySmart:
		return m.smartApprove(req), nil

	default:
		return m.smartApprove(req), nil
	}
}

// autoApprove approves all commands (use with caution)
func (m *Manager) autoApprove(req *ApprovalRequest) *ApprovalResult {
	return &ApprovalResult{
		Approved: true,
		Strategy: StrategyAutoApprove,
		Reason:   "Auto-approve strategy",
		AskUser:  req.RiskLevel >= RiskHigh,
	}
}

// manualApprove requires explicit user confirmation
func (m *Manager) manualApprove(req *ApprovalRequest) *ApprovalResult {
	// If CLI confirm disabled, deny high-risk commands
	if !m.config.EnableCLIConfirm && req.RiskLevel >= RiskHigh {
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategyManual,
			Reason:   "CLI confirmation disabled for high-risk commands",
			AskUser:  false,
		}
	}

	return &ApprovalResult{
		Approved: false, // Requires user confirmation
		Strategy: StrategyManual,
		Reason:   "Manual approval required",
		AskUser:  true,
	}
}

// smartApprove uses learned patterns and risk assessment
func (m *Manager) smartApprove(req *ApprovalRequest) *ApprovalResult {
	// Always ask for critical risk
	if req.RiskLevel >= RiskCritical {
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategySmart,
			Reason:   "Critical risk level requires confirmation",
			AskUser:  true,
		}
	}

	// Low risk and allowed patterns can be auto-approved
	if req.RiskLevel == RiskLow && m.isAllowedPattern(req.Command) {
		return &ApprovalResult{
			Approved: true,
			Strategy: StrategySmart,
			Reason:   "Low risk command",
			Trusted:  true,
		}
	}

	// Ask for medium and high risk
	if req.RiskLevel >= RiskMedium {
		return &ApprovalResult{
			Approved: false,
			Strategy: StrategySmart,
			Reason:   fmt.Sprintf("Medium/High risk command (level %d)", req.RiskLevel),
			AskUser:  true,
		}
	}

	// Default: ask for confirmation
	return &ApprovalResult{
		Approved: false,
		Strategy: StrategySmart,
		Reason:   "Approval required",
		AskUser:  true,
	}
}

// Approve records a user approval decision
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

	// Trust if threshold reached
	if pattern.Count >= m.config.TrustThreshold {
		pattern.Trusted = true
	}

	return m.savePatterns()
}

// Deny records a user denial decision
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

	// Remove from trusted if was trusted
	if pattern.Trusted {
		pattern.Trusted = false
	}

	return m.savePatterns()
}

// AddToWhitelist adds a command pattern to whitelist
func (m *Manager) AddToWhitelist(pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.whitelist[pattern] = true
	return m.saveWhitelist()
}

// RemoveFromWhitelist removes a pattern from whitelist
func (m *Manager) RemoveFromWhitelist(pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.whitelist, pattern)
	return m.saveWhitelist()
}

// GetTrustedCommands returns all trusted command patterns
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

// GetDeniedCommands returns denied command patterns
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

// PatternMatchResult contains the result of a pattern match
type PatternMatchResult struct {
	Matched    bool
	Pattern    string
	Variables  map[string]string // Extracted variables from wildcard matches
}

// CLIConfirm prompts user for confirmation in terminal
func (m *Manager) CLIConfirm(req *ApprovalRequest) (bool, error) {
	if !m.config.EnableCLIConfirm {
		return false, nil
	}

	fmt.Printf("\n⚠️  Command requires approval\n")
	fmt.Printf("   Command: %s\n", req.Command)
	fmt.Printf("   Risk Level: %d\n", req.RiskLevel)
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

// matchPattern matches a command against a pattern with variable extraction
func (m *Manager) matchPattern(cmd, pattern string) *PatternMatchResult {
	result := &PatternMatchResult{
		Matched:   false,
		Pattern:   pattern,
		Variables: make(map[string]string),
	}

	// Check if pattern contains wildcards
	if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
		// Convert shell-style wildcards to regex
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
			// Extract variables
			matches := re.FindStringSubmatch(cmd)
			if len(matches) > 1 {
				for i, name := range re.SubexpNames() {
					if i > 0 && i < len(matches) && name != "" {
						result.Variables[name] = matches[i]
					}
				}
			}
		}
	} else {
		// Regular regex pattern
		re, err := regexp.Compile(`^` + pattern + `$`)
		if err != nil {
			// Fallback to simple string match
			if pattern == cmd {
				result.Matched = true
			}
			return result
		}

		if re.MatchString(cmd) {
			result.Matched = true
			matches := re.FindStringSubmatch(cmd)
			if len(matches) > 1 {
				for i, name := range re.SubexpNames() {
					if i > 0 && i < len(matches) && name != "" {
						result.Variables[name] = matches[i]
					}
				}
			}
		}
	}

	return result
}

// matchAnyPattern checks if command matches any of the given patterns
func (m *Manager) matchAnyPattern(cmd string, patterns []string) *PatternMatchResult {
	for _, pattern := range patterns {
		result := m.matchPattern(cmd, pattern)
		if result.Matched {
			return result
		}
	}
	return &PatternMatchResult{Matched: false}
}

// isDangerous checks if command matches dangerous patterns
func (m *Manager) isDangerous(cmd string) bool {
	result := m.matchAnyPattern(cmd, m.config.DangerousPatterns)
	return result.Matched
}

// isWhitelisted checks if command is whitelisted
func (m *Manager) isWhitelisted(cmd string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for pattern := range m.whitelist {
		result := m.matchPattern(cmd, pattern)
		if result.Matched {
			return true
		}
	}
	return false
}

// isAllowedPattern checks if command matches allowed patterns
func (m *Manager) isAllowedPattern(cmd string) bool {
	result := m.matchAnyPattern(cmd, m.config.AllowedPatterns)
	return result.Matched
}

// calculateRiskLevel assesses the risk of a command
func (m *Manager) calculateRiskLevel(cmd string) RiskLevel {
	// Critical patterns
	critical := []string{
		"rm -rf /", "dd if=", "mkfs", "shutdown", "reboot",
		":(){:|:&};:", ">/dev/sda", ">%0", "format",
	}
	for _, p := range critical {
		if strings.Contains(cmd, p) {
			return RiskCritical
		}
	}

	// High risk - destructive
	high := []string{
		"rm -rf", "rm -r", "del /", "chmod 777", "chown",
		"kill -9", "killall", "pkill", "service ",
	}
	for _, p := range high {
		if strings.Contains(cmd, p) {
			return RiskHigh
		}
	}

	// Medium risk - modifying
	medium := []string{
		"mkdir", "touch", "mv ", "cp ", "ln -s",
		"curl ", "wget ", "pip install", "npm install",
		"git push", "git commit", "docker run",
		"ssh ", "scp ", "rsync",
	}
	for _, p := range medium {
		if strings.Contains(cmd, p) {
			return RiskMedium
		}
	}

	return RiskLow
}

// hashPattern creates a hash for pattern matching
func (m *Manager) hashPattern(cmd string) string {
	// Normalize the command
	normalized := normalizeCommand(cmd)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:8])
}

// normalizeCommand normalizes command for pattern matching
func normalizeCommand(cmd string) string {
	// Lowercase
	cmd = strings.ToLower(cmd)
	// Remove multiple spaces
	cmd = regexp.MustCompile(`\s+`).ReplaceAllString(cmd, " ")
	// Replace specific values with placeholders
	cmd = regexp.MustCompile(`\d+`).ReplaceAllString(cmd, "N")
	cmd = regexp.MustCompile(`["'][^"']+["']`).ReplaceAllString(cmd, "'X'")
	return strings.TrimSpace(cmd)
}

// loadPatterns loads patterns from disk
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

// savePatterns saves patterns to disk
func (m *Manager) savePatterns() error {
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

// loadWhitelist loads whitelist from disk
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

// saveWhitelist saves whitelist to disk
func (m *Manager) saveWhitelist() error {
	home, _ := os.UserHomeDir()
	wlPath := filepath.Join(home, ".magic", "approval", "whitelist.txt")

	var lines []string
	for pattern := range m.whitelist {
		lines = append(lines, pattern)
	}

	return os.WriteFile(wlPath, []byte(strings.Join(lines, "\n")), 0644)
}

// RegisterCallback registers an approval callback
func (m *Manager) RegisterCallback(cb ApprovalCallback) {
	m.callbacks = append(m.callbacks, cb)
}

// NotifyApproval notifies all callbacks of an approval result
func (m *Manager) NotifyApproval(result *ApprovalResult, req *ApprovalRequest) {
	for _, cb := range m.callbacks {
		cb.OnApproval(result, req)
	}
}

// ConfigCommand returns CLI commands for approval management
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
				fmt.Printf("Strategy: %s\n", m.config.Strategy)
				fmt.Printf("Learning: %v\n", m.config.EnableLearning)
				fmt.Printf("CLI Confirm: %v\n", m.config.EnableCLIConfirm)
				fmt.Printf("Trust Threshold: %d\n", m.config.TrustThreshold)
				fmt.Printf("Trusted Patterns: %d\n", len(m.GetTrustedCommands()))
				fmt.Printf("Denied Patterns: %d\n", len(m.GetDeniedCommands()))
				return nil
			},
		},
	)

	return cmd
}

// SyncWithMemory syncs patterns with memory store
func (m *Manager) SyncWithMemory() error {
	// This would integrate with the memory store
	// to record command history and learn from it
	return nil
}
