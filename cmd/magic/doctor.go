package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/pkg/config"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose any issues",
	Long:  "Run diagnostics to check magic Agent setup and configuration",
	Run:   runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║       magic Agent Doctor               ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	allGood := true

	// Check OS
	fmt.Printf("[%s] Operating System: %s\n", checkMark(true), runtime.GOOS+" "+runtime.GOARCH)

	// Check Go version
	fmt.Printf("[%s] Go Runtime: %s\n", checkMark(true), runtime.Version())

	// Check config file
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("[%s] Cannot determine home directory\n", crossMark())
		allGood = false
	} else {
		configPath := filepath.Join(home, ".magic", "config.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Printf("[%s] Config file not found: %s\n", crossMark(), configPath)
			fmt.Printf("    Run 'magic setup' to create configuration\n")
			allGood = false
		} else {
			fmt.Printf("[%s] Config file exists: %s\n", checkMark(true), configPath)

			// Validate config
			cfg, err := config.Load()
			if err != nil {
				fmt.Printf("[%s] Config file invalid: %v\n", crossMark(), err)
				allGood = false
			} else {
				fmt.Printf("[%s] Config valid\n", checkMark(true))

				// Check provider config
				if _, ok := cfg.Providers[cfg.Provider]; !ok {
					fmt.Printf("[%s] Provider '%s' not configured\n", crossMark(), cfg.Provider)
					allGood = false
				} else {
					fmt.Printf("[%s] Provider configured: %s\n", checkMark(true), cfg.Provider)
				}
			}
		}
	}

	// Check internet connectivity
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://www.google.com")
	if err != nil {
		fmt.Printf("[%s] Internet connectivity: FAILED\n", crossMark())
		fmt.Println("    Check your network connection")
		allGood = false
	} else {
		resp.Body.Close()
		fmt.Printf("[%s] Internet connectivity: OK\n", checkMark(true))
	}

	// Check magic directory
	magicDir := filepath.Join(home, ".magic")
	if _, err := os.Stat(magicDir); os.IsNotExist(err) {
		fmt.Printf("[%s] magic directory missing: %s\n", crossMark(), magicDir)
		allGood = false
	} else {
		fmt.Printf("[%s] magic directory exists\n", checkMark(true))
	}

	fmt.Println()
	if allGood {
		fmt.Println("✓ All checks passed! magic is ready to use.")
	} else {
		fmt.Println("✗ Some checks failed. Please review the issues above.")
		os.Exit(1)
	}
}

func checkMark(good bool) string {
	if good {
		return "✓"
	}
	return "✗"
}

func crossMark() string {
	return "✗"
}
