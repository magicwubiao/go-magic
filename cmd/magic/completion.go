package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for magic.

To load completions:

Bash:

  $ source <(magic completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ magic completion bash > /etc/bash_completion.d/magic
  # macOS:
  $ magic completion bash > /usr/local/etc/bash_completion.d/magic

Zsh:

  # If shell completion is not enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ magic completion zsh > "${fpath[1]}/_magic"

  # You will need to start a new shell for this setup to take effect.

Fish:

  $ magic completion fish | source

  # To load completions for each session, execute once:
  $ magic completion fish > ~/.config/fish/completions/magic.fish

PowerShell:

  PS> magic completion powershell | Out-String | Invoke-Expression

  # To load completions for each session, execute once:
  PS> magic completion powershell > magic.ps1
  # and source this file from your PowerShell profile.

Install:

  # Automatically detect and install completion for the current shell
  $ magic completion install`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell", "install"},
	Args:                  cobra.MatchAll,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("shell type required: bash, zsh, fish, or powershell")
		}

		shell := args[0]
		switch shell {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		case "install":
			return installCompletion()
		default:
			return fmt.Errorf("unsupported shell: %s", shell)
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
	completionCmd.Flags().BoolP("help", "h", false, "Show help for completion command")
}

func installCompletion() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	var shell string
	switch runtime.GOOS {
	case "darwin":
		shell = "bash" // Default to bash on macOS
	case "windows":
		shell = "powershell"
	default:
		// Try to detect shell from environment
		if os.Getenv("ZSH_VERSION") != "" {
			shell = "zsh"
		} else if os.Getenv("FISH_VERSION") != "" {
			shell = "fish"
		} else {
			shell = "bash"
		}
	}

	fmt.Printf("Detected shell: %s\n", shell)

	var installPath string
	var sourceCmd string

	switch shell {
	case "bash":
		home, _ := os.UserHomeDir()
		if runtime.GOOS == "darwin" {
			installPath = "/usr/local/etc/bash_completion.d/magic"
		} else {
			installPath = "/etc/bash_completion.d/magic"
			// Try to create the directory if it doesn't exist
			os.MkdirAll("/etc/bash_completion.d", 0755)
		}

		// Generate completion
		f, err := os.Create(installPath)
		if err != nil {
			return fmt.Errorf("failed to create completion file: %w", err)
		}
		defer f.Close()

		if err := rootCmd.GenBashCompletion(f); err != nil {
			return fmt.Errorf("failed to generate bash completion: %w", err)
		}

		sourceCmd = fmt.Sprintf("source %s", installPath)
		fmt.Printf("Bash completion installed to: %s\n", installPath)

	case "zsh":
		// Get fpath
		fpath := os.Getenv("FPATH")
		if fpath == "" {
			fpath = filepath.Join(home, ".oh-my-zsh", "completions")
		}

		installPath = filepath.Join(fpath, "_magic")
		os.MkdirAll(filepath.Dir(installPath), 0755)

		f, err := os.Create(installPath)
		if err != nil {
			return fmt.Errorf("failed to create completion file: %w", err)
		}
		defer f.Close()

		if err := rootCmd.GenZshCompletion(f); err != nil {
			return fmt.Errorf("failed to generate zsh completion: %w", err)
		}

		sourceCmd = fmt.Sprintf("source %s", installPath)
		fmt.Printf("Zsh completion installed to: %s\n", installPath)
		fmt.Println("Note: You may need to start a new shell or run: autoload -U compinit; compinit")

	case "fish":
		configPath := filepath.Join(home, ".config", "fish", "completions")
		os.MkdirAll(configPath, 0755)

		installPath = filepath.Join(configPath, "magic.fish")

		f, err := os.Create(installPath)
		if err != nil {
			return fmt.Errorf("failed to create completion file: %w", err)
		}
		defer f.Close()

		if err := rootCmd.GenFishCompletion(f, true); err != nil {
			return fmt.Errorf("failed to generate fish completion: %w", err)
		}

		sourceCmd = fmt.Sprintf("source %s", installPath)
		fmt.Printf("Fish completion installed to: %s\n", installPath)

	case "powershell":
		installPath := "magic.ps1"

		f, err := os.Create(installPath)
		if err != nil {
			return fmt.Errorf("failed to create completion file: %w", err)
		}
		defer f.Close()

		if err := rootCmd.GenPowerShellCompletionWithDesc(f); err != nil {
			return fmt.Errorf("failed to generate powershell completion: %w", err)
		}

		fmt.Printf("PowerShell completion saved to: %s\n", installPath)
		fmt.Printf("Add this to your PowerShell profile:\n")
		fmt.Printf("  . %s\n", installPath)
		return nil
	}

	fmt.Println()
	fmt.Println("Installation complete!")
	fmt.Printf("To enable completion for current session, run:\n")
	fmt.Printf("  %s\n", sourceCmd)
	fmt.Println()

	// Try to detect and execute shell configuration
	switch shell {
	case "bash":
		shellConfig := filepath.Join(home, ".bashrc")
		if runtime.GOOS == "darwin" {
			shellConfig = filepath.Join(home, ".bash_profile")
		}

		// Check if already configured
		if data, err := os.ReadFile(shellConfig); err == nil {
			if string(data) != "" {
				fmt.Printf("Would you like to add the completion to %s? (y/n): ", shellConfig)
			}
		}

	case "zsh":
		fmt.Printf("Would you like to verify zsh completion is working? (y/n): ")
	}

	return nil
}

// BashCompletionFunc implements bash completion
func bashCompletionFunc(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	var completions []string

	// Add subcommands
	for _, sub := range rootCmd.Commands() {
		if sub.IsAvailableCommand() {
			completions = append(completions, sub.Name)
		}
	}

	// Add files for certain commands
	cmdsNeedingFiles := map[string]bool{
		"load":    true,
		"config":  true,
		"session": true,
	}

	parent, _, _ := cmd.Find(os.Args[2:])
	if parent != nil {
		if cmdsNeedingFiles[parent.Name()] {
			if files, err := os.ReadDir("."); err == nil {
				for _, f := range files {
					completions = append(completions, f.Name())
				}
			}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// Reload completion for a specific shell
func reloadCompletion(shell string) error {
	var cmd *exec.Cmd

	switch shell {
	case "bash":
		cmd = exec.Command("bash", "-l", "-c", "source ~/.bashrc")
	case "zsh":
		cmd = exec.Command("zsh", "-l", "-c", "compinit")
	}

	if cmd != nil {
		return cmd.Run()
	}

	return nil
}
