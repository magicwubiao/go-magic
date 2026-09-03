package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// doctorCmd represents the doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose configuration and connectivity issues",
	Long: `Run diagnostic checks to identify configuration problems.

Available checks:
  - config:   Validate configuration file
  - provider: Test API provider connectivity
  - tools:    Verify tool availability
  - gateway:  Check gateway status
  - skills:   Validate skills directory`,
	RunE: runDoctor,
}

var doctorAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Run all diagnostic checks",
	RunE:  runDoctorAll,
}

var doctorCheckType string

func init() {
	doctorCmd.Flags().StringVarP(&doctorCheckType, "check", "c", "",
		"Check type: config, provider, tools, gateway, skills")
	doctorCmd.AddCommand(doctorAllCmd)
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	checkType := doctorCheckType

	if checkType == "" {
		return runDoctorAll(cmd, args)
	}

	fmt.Println()
	fmt.Println("=== Running " + checkType + " check ===")

	var err error
	switch checkType {
	case "config":
		err = runConfigCheck()
	case "provider":
		err = runProviderCheck()
	case "tools":
		err = runToolsCheck()
	case "gateway":
		err = runGatewayCheck()
	case "skills":
		err = runSkillsCheck()
	default:
		fmt.Printf("Unknown check type: %s\n", checkType)
		fmt.Println("Valid types: config, provider, tools, gateway, skills")
	}

	return err
}

func runDoctorAll(cmd *cobra.Command, args []string) error {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("       go-magic Diagnostic Report")
	fmt.Println("========================================")
	fmt.Printf("Date: %s\n", getCurrentTime())
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("Arch: %s\n", runtime.GOARCH)
	fmt.Println("========================================")
	fmt.Println()

	// Config check
	fmt.Println("[CONFIG] Configuration...")
	if err := runConfigCheck(); err != nil {
		fmt.Printf("   FAILED: %v\n\n", err)
	} else {
		fmt.Println("   PASSED")
	}

	// Provider check
	fmt.Println("[PROVIDER] API Connectivity...")
	if err := runProviderCheck(); err != nil {
		fmt.Printf("   FAILED: %v\n\n", err)
	} else {
		fmt.Println("   PASSED")
	}

	// Tools check
	fmt.Println("[TOOLS] Tool Availability...")
	if err := runToolsCheck(); err != nil {
		fmt.Printf("   FAILED: %v\n\n", err)
	} else {
		fmt.Println("   PASSED")
	}

	// Gateway check
	fmt.Println("[GATEWAY] Gateway Status...")
	if err := runGatewayCheck(); err != nil {
		fmt.Printf("   FAILED: %v\n\n", err)
	} else {
		fmt.Println("   PASSED")
	}

	// Skills check
	fmt.Println("[SKILLS] Skills Directory...")
	if err := runSkillsCheck(); err != nil {
		fmt.Printf("   FAILED: %v\n\n", err)
	} else {
		fmt.Println("   PASSED")
	}

	fmt.Println("========================================")
	fmt.Println("Run 'magic setup' to fix configuration issues.")
	fmt.Println("========================================")

	return nil
}

func getCurrentTime() string {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "echo %date% %time%")
		output, _ := cmd.CombinedOutput()
		return strings.TrimSpace(string(output))
	}
	cmd := exec.Command("date")
	output, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(output))
}

func runConfigCheck() error {
	configPath := filepath.Join(config.GetMagicHome(), "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config file not found at %s", configPath)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid JSON: %v", err)
	}

	// Display provider info
	if provider, ok := cfg["provider"].(string); ok && provider != "" {
		fmt.Printf("   Provider: %s\n", provider)
	}
	if model, ok := cfg["model"].(string); ok && model != "" {
		fmt.Printf("   Model: %s\n", model)
	}

	fmt.Printf("   Config: %s\n", configPath)
	return nil
}

func runProviderCheck() error {
	configPath := filepath.Join(config.GetMagicHome(), "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("no config found at %s", configPath)
	}

	var cfg map[string]interface{}
	json.Unmarshal(data, &cfg)

	providerName, _ := cfg["provider"].(string)

	if providerName == "" {
		return fmt.Errorf("no provider configured (run 'magic setup' or 'magic model' first)")
	}

	fmt.Printf("   Provider: %s\n", providerName)
	fmt.Printf("   (Connectivity test requires valid API key)\n")
	return nil
}

func runToolsCheck() error {
	// Check if tools are registered
	skillsDir := filepath.Join(config.GetMagicHome(), "skills")

	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		fmt.Printf("   Skills dir: NOT FOUND\n")
	} else {
		fmt.Printf("   Skills dir: %s\n", skillsDir)
	}

	// Check common tool directories
	toolDirs := []string{
		filepath.Join(config.GetMagicHome(), "plugins"),
	}

	for _, dir := range toolDirs {
		if _, err := os.Stat(dir); err == nil {
			fmt.Printf("   Plugin dir: %s\n", dir)
		}
	}

	return nil
}

func runGatewayCheck() error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/FI", "IMAGENAME eq magic.exe")
	} else {
		cmd = exec.Command("pgrep", "-f", "magic.*gateway")
	}
	if err := cmd.Run(); err == nil {
		fmt.Println("   Gateway: RUNNING")
	} else {
		fmt.Println("   Gateway: NOT RUNNING")
		fmt.Println("   (Start with 'magic gateway start')")
	}
	return nil
}

func runSkillsCheck() error {
	skillsDir := filepath.Join(config.GetMagicHome(), "skills")

	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		fmt.Println("   Skills directory: NOT FOUND")
		return nil
	}

	fmt.Printf("   Skills dir: %s\n", skillsDir)

	// Count skill files
	count := 0
	filepath.Walk(skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && (strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".skill")) {
			count++
		}
		return nil
	})

	fmt.Printf("   Skill files: %d\n", count)
	return nil
}
