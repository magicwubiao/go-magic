package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update to the latest version",
	Run:   runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) {
	fmt.Println("magic Agent Updater")
	fmt.Println("====================")
	fmt.Println()

	// Get current binary path
	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to determine current executable path: %v\n", err)
		os.Exit(1)
	}

	// Check if it's a git repository
	repoPath := findMagicRepoPath()

	if repoPath != "" {
		if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
			fmt.Printf("Detected local git repository: %s\n", repoPath)
			fmt.Println("Updating via git pull...")
			fmt.Println()

			pullCmd := exec.Command("git", "-C", repoPath, "pull")
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr

			if err := pullCmd.Run(); err != nil {
				fmt.Printf("Git pull failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Println()
			fmt.Println("Rebuilding...")

			// Detect binary name
			binaryName := "magic"
			if runtime.GOOS == "windows" {
				binaryName = "magic.exe"
			}

			buildCmd := exec.Command("go", "build", "-o", filepath.Join(repoPath, binaryName), "./cmd/magic")
			buildCmd.Dir = repoPath
			buildCmd.Stdout = os.Stdout
			buildCmd.Stderr = os.Stderr

			if err := buildCmd.Run(); err != nil {
				fmt.Printf("Build failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Println()
			fmt.Printf("✓ Update complete! Binary: %s\n", filepath.Join(repoPath, binaryName))
			return
		}
	}

	// Try go install
	fmt.Println("Updating via go install...")
	fmt.Printf("Current binary: %s\n", execPath)
	fmt.Println()

	installCmd := exec.Command("go", "install", "github.com/magicwubiao/go-magic/cmd/magic@latest")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	if err := installCmd.Run(); err != nil {
		fmt.Printf("go install failed: %v\n", err)
		fmt.Println()
		fmt.Println("Manual update instructions:")
		fmt.Println("  1. Clone or pull the repository:")
		fmt.Println("     git clone https://github.com/magicwubiao/go-magic")
		fmt.Println("     cd go-magic && git pull")
		fmt.Println("  2. Build:")
		fmt.Println("     go build -o magic ./cmd/magic")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Update complete!")

	// Show where the binary was installed
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = filepath.Join(os.Getenv("HOME"), "go")
	}
	fmt.Printf("Make sure %s is in your PATH\n", filepath.Join(goPath, "bin"))
}

// findMagicRepoPath tries to find the git repository path
func findMagicRepoPath() string {
	// Check current directory
	if _, err := os.Stat(".git"); err == nil {
		wd, _ := os.Getwd()
		return wd
	}

	// Check parent directories
	wd, _ := os.Getwd()
	dir := wd
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break // Reached root
		}
		dir = parent
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
	}

	// Check common locations
	home, _ := os.UserHomeDir()
	commonPaths := []string{
		filepath.Join(home, "go", "go-magic"),
		filepath.Join(home, "projects", "go-magic"),
		filepath.Join(home, "dev", "go-magic"),
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return p
		}
	}

	return ""
}
