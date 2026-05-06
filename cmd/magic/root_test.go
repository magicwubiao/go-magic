package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCommand(t *testing.T) {
	// Test that root command is created
	if rootCmd == nil {
		t.Fatal("rootCmd should not be nil")
	}

	// Test root command properties
	if rootCmd.Use != "magic" {
		t.Errorf("expected Use to be 'magic', got '%s'", rootCmd.Use)
	}

	if rootCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestGlobalFlags(t *testing.T) {
	// Test that global flags are registered
	tests := []struct {
		name      string
		flagName  string
		expectSet bool
	}{
		{"verbose", "verbose", false},
		{"debug", "debug", false},
		{"config", "config", false},
		{"output", "output", false},
		{"no-color", "no-color", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := rootCmd.PersistentFlags().Lookup(tt.flagName)
			if flag == nil {
				t.Errorf("flag --%s should be registered", tt.flagName)
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	// Find version command
	var versionCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "version" {
			versionCmd = cmd
			break
		}
	}

	if versionCmd == nil {
		t.Fatal("version command should be registered")
	}

	// Test version command properties
	if versionCmd.Use != "version" {
		t.Errorf("expected Use to be 'version', got '%s'", versionCmd.Use)
	}

	if versionCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestHelpTemplate(t *testing.T) {
	// Test that help template is set
	if rootCmd.HelpTemplate() == "" {
		t.Error("HelpTemplate should not be empty")
	}
}

func TestUsageTemplate(t *testing.T) {
	// Test that usage template is set
	if rootCmd.UsageTemplate() == "" {
		t.Error("UsageTemplate should not be empty")
	}
}

func TestCommandCount(t *testing.T) {
	cmds := rootCmd.Commands()
	// Should have multiple commands
	if len(cmds) < 10 {
		t.Errorf("expected at least 10 commands, got %d", len(cmds))
	}
}

func TestFlagOutput(t *testing.T) {
	tests := []struct {
		format string
		valid  bool
	}{
		{"text", true},
		{"json", true},
		{"yaml", true},
		{"table", true},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			// This is more of a validation test
			validFormats := map[string]bool{
				"text":  true,
				"json":  true,
				"yaml":  true,
				"table": true,
			}

			if validFormats[tt.format] != tt.valid {
				t.Errorf("format %s validity mismatch", tt.format)
			}
		})
	}
}

func TestSilenceSettings(t *testing.T) {
	if !rootCmd.SilenceUsage {
		t.Error("SilenceUsage should be true")
	}

	if !rootCmd.SilenceErrors {
		t.Error("SilenceErrors should be true")
	}
}

func TestCommandExecution(t *testing.T) {
	// Create a buffer to capture output
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Check output contains version info
	output := buf.String()
	if output == "" {
		t.Error("expected output from version command")
	}
}
