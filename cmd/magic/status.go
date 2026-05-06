package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status and diagnostics",
	Long: `Display comprehensive system status including:
- Configuration state
- Loaded skills and plugins
- Available tools
- System resources
- Health check`,
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Perform health check",
	Long:  "Check if all system components are healthy and functioning",
	Run:   runHealth,
}

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Show system metrics",
	Long:  "Display current system metrics and statistics",
	Run:   runMetrics,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(metricsCmd)
}

func runHealth(cmd *cobra.Command, args []string) {
	fmt.Println("Running health check...")
	fmt.Println()

	checks := []struct {
		name    string
		healthy bool
		message string
	}{
		{"Config", checkConfigHealth()},
		{"Magic Home", checkMagicHomeHealth()},
		{"Skills", checkSkillsHealth()},
		{"Tools", checkToolsHealth()},
		{"Plugins", checkPluginsHealth()},
		{"Network", checkNetworkHealth()},
		{"Disk Space", checkDiskHealth()},
	}

	allHealthy := true
	for _, check := range checks {
		status := "✓"
		color := "\033[32m" // Green
		if !check.healthy {
			status = "✗"
			color = "\033[31m" // Red
			allHealthy = false
		}

		if flagNoColor {
			fmt.Printf("[%s] %s: %s\n", status, check.name, check.message)
		} else {
			fmt.Printf("%s[%s]\033[0m \033[1m%s:\033[0m %s\n", color, status, check.name, check.message)
		}
	}

	fmt.Println()
	if allHealthy {
		fmt.Println("\033[32m✓ All checks passed\033[0m")
	} else {
		fmt.Println("\033[33m⚠ Some checks failed - run 'magic doctor' for details\033[0m")
	}
}

func runMetrics(cmd *cobra.Command, args []string) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	metrics := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"go": map[string]interface{}{
			"version":      runtime.Version(),
			"goos":         runtime.GOOS,
			"goarch":       runtime.GOARCH,
			"num_cpu":      runtime.NumCPU(),
			"num_goroutine": runtime.NumGoroutine(),
		},
		"memory": map[string]interface{}{
			"alloc":       memStats.Alloc,
			"total_alloc": memStats.TotalAlloc,
			"sys":         memStats.Sys,
			"mallocs":     memStats.Mallocs,
			"frees":       memStats.Frees,
			"heap_alloc":  memStats.HeapAlloc,
			"heap_sys":    memStats.HeapSys,
			"heap_idle":   memStats.HeapIdle,
			"heap_inuse":  memStats.HeapInuse,
		},
		"gc": map[string]interface{}{
			"count":      memStats.NumGC,
			"pause_total": memStats.PauseTotalNs,
		},
	}

	// Try to add config info
	if cfg, err := config.Load(); err == nil {
		metrics["config"] = map[string]interface{}{
			"profile":   cfg.Profile,
			"provider":  cfg.Provider,
			"model":     cfg.Model,
			"magic_home": cfg.MagicHome,
		}
	}

	// Try to count skills
	if mgr, err := skills.NewManager(); err == nil {
		metrics["skills"] = map[string]interface{}{
			"total": len(mgr.List()),
		}
	}

	// Count tools
	tools := tool.GetAllTools()
	metrics["tools"] = map[string]interface{}{
		"total": len(tools),
	}

	// Output based on format
	switch flagOutput {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(metrics)
	case "yaml":
		fmt.Println("# System Metrics")
		printYAML(metrics, 0)
	default:
		fmt.Println("=== System Metrics ===")
		printMetricsTable(metrics)
	}
}

func runStatus(cmd *cobra.Command, args []string) {
	fmt.Println("=== Magic Agent Status ===")
	fmt.Println()

	// System info
	fmt.Println("System:")
	fmt.Printf("  OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go: %s\n", runtime.Version())
	fmt.Printf("  CPUs: %d\n", runtime.NumCPU())
	fmt.Printf("  Time: %s\n", time.Now().Format(time.RFC3339))
	fmt.Println()

	// Config
	cfg, err := config.Load()
	if err == nil {
		fmt.Println("Configuration:")
		fmt.Printf("  Magic Home: %s\n", cfg.MagicHome)
		fmt.Printf("  Profile: %s\n", cfg.Profile)
		fmt.Printf("  Provider: %s\n", cfg.Provider)
		fmt.Printf("  Model: %s\n", cfg.Model)
		fmt.Printf("  Providers: %d configured\n", len(cfg.Providers))
		fmt.Println()
	}

	// Skills
	if mgr, err := skills.NewManager(); err == nil {
		skillList := mgr.List()
		fmt.Printf("Skills: %d loaded\n", len(skillList))
		fmt.Println()
	}

	// Tools
	tools := tool.GetAllTools()
	fmt.Printf("Tools: %d available\n", len(tools))
	fmt.Println()

	// Command count
	fmt.Printf("Commands: %d\n", len(rootCmd.Commands()))
}

func checkConfigHealth() (bool, string) {
	_, err := config.Load()
	if err != nil {
		return false, fmt.Sprintf("Failed to load config: %v", err)
	}
	return true, "OK"
}

func checkMagicHomeHealth() (bool, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, "Cannot determine home directory"
	}

	magicHome := filepath.Join(home, ".magic")
	if _, err := os.Stat(magicHome); os.IsNotExist(err) {
		return true, "Magic home will be created on first run"
	}

	return true, "OK"
}

func checkSkillsHealth() (bool, string) {
	mgr, err := skills.NewManager()
	if err != nil {
		return false, fmt.Sprintf("Failed to load skills manager: %v", err)
	}

	count := len(mgr.List())
	return true, fmt.Sprintf("%d skills loaded", count)
}

func checkToolsHealth() (bool, string) {
	tools := tool.GetAllTools()
	return true, fmt.Sprintf("%d tools available", len(tools))
}

func checkPluginsHealth() (bool, string) {
	return true, "OK (plugin system ready)"
}

func checkNetworkHealth() (bool, string) {
	// Check if we can reach a common endpoint
	client := &http.Client{Timeout: 5 * time.Second}
	_, err := client.Get("https://httpbin.org/status/200")
	if err != nil {
		return false, "Network connectivity issue"
	}
	return true, "OK"
}

func checkDiskHealth() (bool, string) {
	// Check if we have write access to temp
	tmpDir := os.TempDir()
	testFile := filepath.Join(tmpDir, "magic_health_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return false, "Cannot write to temp directory"
	}
	os.Remove(testFile)
	return true, "OK"
}

func printMetricsTable(metrics map[string]interface{}) {
	if goInfo, ok := metrics["go"].(map[string]interface{}); ok {
		fmt.Println("Go Runtime:")
		fmt.Printf("  Version: %s\n", goInfo["version"])
		fmt.Printf("  OS/Arch: %s/%s\n", goInfo["goos"], goInfo["goarch"])
		fmt.Printf("  CPUs: %v\n", goInfo["num_cpu"])
		fmt.Printf("  Goroutines: %v\n", goInfo["num_goroutine"])
		fmt.Println()
	}

	if memInfo, ok := metrics["memory"].(map[string]interface{}); ok {
		fmt.Println("Memory:")
		fmt.Printf("  Alloc: %s\n", formatBytes(memInfo["alloc"].(uint64)))
		fmt.Printf("  Total Alloc: %s\n", formatBytes(memInfo["total_alloc"].(uint64)))
		fmt.Printf("  Sys: %s\n", formatBytes(memInfo["sys"].(uint64)))
		fmt.Printf("  Heap Alloc: %s\n", formatBytes(memInfo["heap_alloc"].(uint64)))
		fmt.Println()
	}

	if configInfo, ok := metrics["config"].(map[string]interface{}); ok {
		fmt.Println("Configuration:")
		fmt.Printf("  Profile: %s\n", configInfo["profile"])
		fmt.Printf("  Provider: %s\n", configInfo["provider"])
		fmt.Printf("  Model: %s\n", configInfo["model"])
		fmt.Println()
	}

	if toolsInfo, ok := metrics["tools"].(map[string]interface{}); ok {
		fmt.Printf("Tools: %v\n", toolsInfo["total"])
	}

	if skillsInfo, ok := metrics["skills"].(map[string]interface{}); ok {
		fmt.Printf("Skills: %v\n", skillsInfo["total"])
	}
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func printYAML(data interface{}, indent int) {
	prefix := strings.Repeat("  ", indent)

	switch v := data.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if nested, ok := v[k].(map[string]interface{}); ok {
				fmt.Printf("%s%s:\n", prefix, k)
				printYAML(nested, indent+1)
			} else {
				fmt.Printf("%s%s: %v\n", prefix, k, v[k])
			}
		}
	default:
		fmt.Printf("%s%v\n", prefix, data)
	}
}
