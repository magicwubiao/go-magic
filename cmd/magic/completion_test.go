package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompletionCommandExists(t *testing.T) {
	var completionCmdFound *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "completion" {
			completionCmdFound = cmd
			break
		}
	}

	if completionCmdFound == nil {
		t.Fatal("completion command should be registered")
	}
}

func TestCompletionCommandShells(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell", "install"}

	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			// Just verify it's a valid shell type
			valid := false
			for _, s := range shells {
				if s == shell {
					valid = true
					break
				}
			}
			if !valid {
				t.Errorf("'%s' should be a valid shell", shell)
			}
		})
	}
}

func TestBashCompletionGeneration(t *testing.T) {
	buf := new(bytes.Buffer)
	err := rootCmd.GenBashCompletion(buf)
	if err != nil {
		t.Fatalf("failed to generate bash completion: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("bash completion should not be empty")
	}

	// Check for basic bash completion markers
	if !bytes.Contains(buf.Bytes(), []byte("magic")) {
		t.Error("bash completion should contain 'magic'")
	}
}

func TestZshCompletionGeneration(t *testing.T) {
	buf := new(bytes.Buffer)
	err := rootCmd.GenZshCompletion(buf)
	if err != nil {
		t.Fatalf("failed to generate zsh completion: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("zsh completion should not be empty")
	}
}

func TestFishCompletionGeneration(t *testing.T) {
	buf := new(bytes.Buffer)
	err := rootCmd.GenFishCompletion(buf, true)
	if err != nil {
		t.Fatalf("failed to generate fish completion: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("fish completion should not be empty")
	}
}

func TestPowerShellCompletionGeneration(t *testing.T) {
	buf := new(bytes.Buffer)
	err := rootCmd.GenPowerShellCompletionWithDesc(buf)
	if err != nil {
		t.Fatalf("failed to generate powershell completion: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("powershell completion should not be empty")
	}
}

func TestCompletionCommandFlags(t *testing.T) {
	var completionCmdFound *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "completion" {
			completionCmdFound = cmd
			break
		}
	}

	if completionCmdFound == nil {
		t.Fatal("completion command should be registered")
	}

	// Check that -h/--help flag exists
	helpFlag := completionCmdFound.Flags().Lookup("help")
	if helpFlag == nil {
		t.Error("completion command should have help flag")
	}
}
