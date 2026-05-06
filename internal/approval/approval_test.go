package approval

import (
	"testing"
)

func TestApprovalManager(t *testing.T) {
	config := &ApprovalConfig{
		Strategy:       StrategySmart,
		EnableLearning: true,
		TrustThreshold: 2,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test dangerous command detection
	req := &ApprovalRequest{
		Command:   "rm -rf /",
		SessionID: "test-session",
	}

	result, err := manager.RequestApproval(req)
	if err != nil {
		t.Fatalf("RequestApproval failed: %v", err)
	}

	if result.Approved {
		t.Error("Dangerous command should not be approved")
	}

	// Test allowed pattern
	safeReq := &ApprovalRequest{
		Command:   "ls -la",
		SessionID: "test-session",
	}

	safeResult, _ := manager.RequestApproval(safeReq)
	if !safeResult.Approved {
		t.Error("Safe command should be auto-approved")
	}

	// Test learning
	manager.Approve(safeReq)
	manager.Approve(safeReq)

	// Second approval should make it trusted
	trusted := manager.GetTrustedCommands()
	if len(trusted) == 0 {
		t.Error("Expected at least one trusted command")
	}
}

func TestRiskLevel(t *testing.T) {
	config := &ApprovalConfig{}
	manager, _ := NewManager(config)

	tests := []struct {
		command string
		level   RiskLevel
	}{
		{"rm -rf /", RiskCritical},
		{"rm -rf /tmp/*", RiskCritical},
		{"ls -la", RiskLow},
		{"pwd", RiskLow},
		{"mkdir test", RiskMedium},
		{"git commit -m 'test'", RiskMedium},
		{"docker run nginx", RiskMedium},
		{"chmod 777 /tmp", RiskHigh},
	}

	for _, tt := range tests {
		level := manager.calculateRiskLevel(tt.command)
		if level != tt.level {
			t.Errorf("Risk level for '%s': got %d, want %d", tt.command, level, tt.level)
		}
	}
}

func TestWhitelist(t *testing.T) {
	config := &ApprovalConfig{
		EnableWhitelist: true,
	}
	manager, _ := NewManager(config)

	// Test adding to whitelist
	err := manager.AddToWhitelist("docker run")
	if err != nil {
		t.Fatalf("AddToWhitelist failed: %v", err)
	}

	// Docker run should now be whitelisted
	req := &ApprovalRequest{
		Command: "docker run -it ubuntu bash",
	}

	result, _ := manager.RequestApproval(req)
	if !result.Approved {
		t.Error("Whitelisted command should be approved")
	}

	// Test removal
	manager.RemoveFromWhitelist("docker run")
	result, _ = manager.RequestApproval(req)
	if result.Trusted {
		t.Error("Command should not be trusted after removal")
	}
}

func TestPatternNormalization(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"rm -rf /tmp/test123"},
		{"curl 'https://api.example.com?key=secret123'"},
		{"git commit -m \"Test commit\""},
	}

	for _, tt := range tests {
		normalized := normalizeCommand(tt.input)
		if normalized == "" {
			t.Errorf("normalizeCommand(%q) returned empty", tt.input)
		}
	}
}

func TestLearning(t *testing.T) {
	config := &ApprovalConfig{
		Strategy:       StrategySmart,
		EnableLearning: true,
		TrustThreshold: 3,
	}
	manager, _ := NewManager(config)

	// First occurrence - not trusted
	req := &ApprovalRequest{
		Command:   "git status",
		SessionID: "session-1",
		RiskLevel: RiskLow,
	}

	result, _ := manager.RequestApproval(req)
	if result.Trusted {
		t.Error("First occurrence should not be trusted")
	}

	// Approve multiple times
	for i := 0; i < 3; i++ {
		manager.Approve(req)
	}

	// Now should be trusted
	trusted := manager.GetTrustedCommands()
	found := false
	for _, p := range trusted {
		if p.Pattern == "git status" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected git status to be trusted")
	}
}

func TestStrategies(t *testing.T) {
	// Test Manual strategy
	manualConfig := &ApprovalConfig{
		Strategy:         StrategyManual,
		EnableCLIConfirm: false,
	}
	manualMgr, _ := NewManager(manualConfig)

	req := &ApprovalRequest{
		Command:   "ls",
		RiskLevel: RiskLow,
	}

	result, _ := manualMgr.RequestApproval(req)
	if result.Approved {
		t.Error("Manual strategy should not auto-approve")
	}
	if !result.AskUser {
		t.Error("Manual strategy should ask user")
	}
}

func TestCommandPatterns(t *testing.T) {
	pattern := &CommandPattern{
		Pattern:     "git push",
		PatternHash: "abc123",
		Action:      "approved",
		Count:       5,
		RiskLevel:   RiskMedium,
		Trusted:     true,
	}

	if pattern.Pattern != "git push" {
		t.Errorf("Pattern mismatch: %s", pattern.Pattern)
	}

	if pattern.Count != 5 {
		t.Errorf("Count mismatch: %d", pattern.Count)
	}

	if !pattern.Trusted {
		t.Error("Pattern should be trusted")
	}
}

func TestApprovalResult(t *testing.T) {
	result := &ApprovalResult{
		Approved: true,
		Strategy: StrategySmart,
		Reason:   "Trusted command",
		Trusted:  true,
	}

	if !result.Approved {
		t.Error("Result should be approved")
	}

	if result.Strategy != StrategySmart {
		t.Errorf("Strategy mismatch: %s", result.Strategy)
	}
}

func TestDangerousPatterns(t *testing.T) {
	config := &ApprovalConfig{
		DangerousPatterns: []string{
			`rm\s+-rf\s+/(?:\*|$)`,
			`dd\s+if=.*of=/dev/sd`,
		},
	}
	manager, _ := NewManager(config)

	// These should be detected as dangerous
	dangerous := []string{
		"rm -rf /",
		"rm -rf /*",
	}

	for _, cmd := range dangerous {
		if !manager.isDangerous(cmd) {
			t.Errorf("Expected '%s' to be dangerous", cmd)
		}
	}

	// These should not be dangerous
	safe := []string{
		"rm -rf /tmp/test",
		"ls -la",
	}

	for _, cmd := range safe {
		if manager.isDangerous(cmd) {
			t.Errorf("Expected '%s' to be safe", cmd)
		}
	}
}
