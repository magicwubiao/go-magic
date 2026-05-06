package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View magic logs",
	Long: `View and manage magic Agent log files.
	
Log files are stored in ~/.magic/logs/ and contain
debugging information about magic operations.
	
Examples:
  magic logs list
  magic logs show magic_2026-04-28_14-42-28.log
  magic logs latest`,
}

var logsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all log files",
	Long: `List all log files in the magic logs directory.
	
Shows file names and sizes.
Log files are automatically created when magic runs.`,
	Run: runLogsList,
}

var logsShowCmd = &cobra.Command{
	Use:   "show [file]",
	Short: "Show contents of a log file",
	Long: `Display the contents of a specific log file.
	
If no file is specified, shows the most recent log file.
Log file names follow the format: magic_YYYY-MM-DD_HH-MM-SS.log`,
	Args: cobra.MaximumNArgs(1),
	Run:  runLogsShow,
}

var logsLatestCmd = &cobra.Command{
	Use:   "latest",
	Short: "Show the most recent log file",
	Long: `Automatically find and display the most recent log file.
	
This is useful for quickly checking the latest magic activity
without having to look up the exact log file name.`,
	Run: runLogsLatest,
}

func init() {
	logsCmd.AddCommand(logsListCmd)
	logsCmd.AddCommand(logsShowCmd)
	logsCmd.AddCommand(logsLatestCmd)
	rootCmd.AddCommand(logsCmd)
}

func runLogsList(cmd *cobra.Command, args []string) {
	home, _ := os.UserHomeDir()
	logsDir := filepath.Join(home, ".magic", "logs")

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		fmt.Printf("Failed to read logs directory: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No log files found.")
		return
	}

	fmt.Println("Log Files:")
	fmt.Println("=========")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, _ := entry.Info()
		fmt.Printf("  %s (%s)\n", entry.Name(), formatSize(info.Size()))
	}
}

func runLogsShow(cmd *cobra.Command, args []string) {
	home, _ := os.UserHomeDir()
	logsDir := filepath.Join(home, ".magic", "logs")

	var logFile string
	var logName string

	if len(args) == 0 {
		// Find latest log file
		entries, err := os.ReadDir(logsDir)
		if err != nil || len(entries) == 0 {
			fmt.Println("No log files found.")
			return
		}

		// Find latest file by modification time
		var latestEntry os.DirEntry
		var latestTime time.Time

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(latestTime) {
				latestTime = info.ModTime()
				latestEntry = entry
			}
		}

		if latestEntry == nil {
			fmt.Println("No log files found.")
			return
		}

		logFile = filepath.Join(logsDir, latestEntry.Name())
		logName = latestEntry.Name()
	} else {
		logFile = filepath.Join(logsDir, args[0])
		logName = args[0]
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		fmt.Printf("Failed to read log file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== %s ===\n\n", logName)
	fmt.Println(string(data))
}

func runLogsLatest(cmd *cobra.Command, args []string) {
	home, _ := os.UserHomeDir()
	logsDir := filepath.Join(home, ".magic", "logs")

	entries, err := os.ReadDir(logsDir)
	if err != nil || len(entries) == 0 {
		fmt.Println("No log files found.")
		return
	}

	// Find latest file by modification time
	var latestEntry os.DirEntry
	var latestTime time.Time

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestEntry = entry
		}
	}

	if latestEntry == nil {
		fmt.Println("No log files found.")
		return
	}

	logFile := filepath.Join(logsDir, latestEntry.Name())
	data, err := os.ReadFile(logFile)
	if err != nil {
		fmt.Printf("Failed to read log file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Latest: %s ===\n\n", latestEntry.Name())
	fmt.Println(string(data))
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	} else {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
}
