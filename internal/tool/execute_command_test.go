package tool_test

import (
	"context"
	"os"
	"testing"

	"github.com/magicwubiao/go-magic/internal/tool"
)

func TestExecuteCommandTool_Whitelist(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	// Test allowed command
	result, err := cmd.Execute(ctx, map[string]interface{}{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := result.(map[string]interface{})
	if r["exit_code"].(int) != 0 {
		t.Errorf("expected exit_code 0, got %v", r["exit_code"])
	}
}

func TestExecuteCommandTool_BlockNotWhitelisted(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	// Test disallowed command
	result, err := cmd.Execute(ctx, map[string]interface{}{
		"command": "rm -rf /",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := result.(map[string]interface{})
	if r["blocked"] != "not_whitelisted" {
		t.Errorf("expected blocked=not_whitelisted, got %v", r["blocked"])
	}
}

func TestExecuteCommandTool_BlockDangerous(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	// Test dangerous pattern
	result, err := cmd.Execute(ctx, map[string]interface{}{
		"command": "chmod 777 /etc/passwd",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := result.(map[string]interface{})
	if r["blocked"] != "dangerous" {
		t.Errorf("expected blocked=dangerous, got %v", r["blocked"])
	}
}

func TestExecuteCommandTool_BlockInjection(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	// Test shell injection with semicolon
	result, err := cmd.Execute(ctx, map[string]interface{}{
		"command": "echo hello; rm -rf /",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := result.(map[string]interface{})
	if r["blocked"] != "injection" {
		t.Errorf("expected blocked=injection, got %v", r["blocked"])
	}
}

func TestExecuteCommandTool_Timeout(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	// Test timeout with sleep
	result, err := cmd.Execute(ctx, map[string]interface{}{
		"command": "sleep 60",
		"timeout": float64(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := result.(map[string]interface{})
	if r["error"] == nil {
		t.Error("expected timeout error")
	}
}

func TestExecuteCommandTool_Workdir(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "go-magic-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with workdir
	result, err := cmd.Execute(ctx, map[string]interface{}{
		"command": "pwd",
		"workdir": tmpDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := result.(map[string]interface{})
	if r["exit_code"].(int) != 0 {
		t.Errorf("expected exit_code 0, got %v", r["exit_code"])
	}
}

func TestExecuteCommandTool_EmptyCommand(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	_, err := cmd.Execute(ctx, map[string]interface{}{
		"command": "",
	})
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestExecuteCommandTool_MissingCommand(t *testing.T) {
	cmd := tool.NewSecureExecuteCommandTool()
	ctx := context.Background()

	_, err := cmd.Execute(ctx, map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing command")
	}
}
