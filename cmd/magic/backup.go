package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup magic configuration and data",
	Long: `Create a backup of all magic data.
	
This command copies the entire ~/.magic directory to a timestamped backup folder.
The backup includes:
  - Configuration file (config.json)
  - Skills (skills/)
  - Session database (sessions.db)
  - Log files (logs/)
  - SOUL.md, MEMORY.md, USER.md
	
Backup location: ~/.magic_backup_YYYYMMDD_HHMMSS
	
Use 'magic stats' to check your current data status.`,
	Run: runBackup,
}

func init() {
	rootCmd.AddCommand(backupCmd)
}

func runBackup(cmd *cobra.Command, args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get home directory: %v\n", err)
		os.Exit(1)
	}

	magicDir := filepath.Join(home, ".magic")
	backupDir := filepath.Join(home, ".magic_backup_"+getTimestamp())

	fmt.Printf("Backing up %s to %s...\n", magicDir, backupDir)

	err = copyDir(magicDir, backupDir)
	if err != nil {
		fmt.Printf("Backup failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Backup completed!")
	fmt.Printf("Backup location: %s\n", backupDir)
}

func getTimestamp() string {
	t := time.Now()
	return fmt.Sprintf("%d%02d%02d_%02d%02d%02d",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second())
}
