package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage chat sessions",
	Long: `Manage chat sessions stored in the local database.
	
Sessions are automatically created when you use the chat command.
Each session contains the full conversation history with the AI.

Examples:
  magic sessions list
  magic sessions show <session-id>
  magic sessions delete <session-id>`,
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all chat sessions",
	Long: `List all chat sessions stored in the local database.
	
Shows session ID, platform, creation time, and message count.
Sessions are sorted by last update time (most recent first).`,
	Run: runSessionsList,
}

var sessionsShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a session's full conversation",
	Long: `Display the full conversation history of a specific session.
	
Shows all messages in the session with their roles (USER, ASSISTANT, etc.).
Use the session ID from the 'magic sessions list' output.`,
	Args: cobra.ExactArgs(1),
	Run:  runSessionsShow,
}

var sessionsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a session permanently",
	Long: `Permanently delete a chat session from the database.
	
This action cannot be undone. Make sure to backup your data first if needed.
Use 'magic backup' to create a backup of all magic data.`,
	Args: cobra.ExactArgs(1),
	Run:  runSessionsDelete,
}

func init() {
	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsShowCmd)
	sessionsCmd.AddCommand(sessionsDeleteCmd)
	rootCmd.AddCommand(sessionsCmd)
}

func runSessionsList(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".magic", "sessions.db")

	store, err := session.NewStore(dbPath)
	if err != nil {
		fmt.Printf("Failed to open session store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	sessions, err := store.ListSessions(context.Background(), cfg.Profile)
	if err != nil {
		fmt.Printf("Failed to list sessions: %v\n", err)
		os.Exit(1)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	fmt.Println("Chat Sessions:")
	fmt.Println("==============")
	for _, s := range sessions {
		fmt.Printf("ID: %s\n", s.ID)
		fmt.Printf("  Platform:  %s\n", s.Platform)
		fmt.Printf("  Created:  %s\n", s.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated:  %s\n", s.UpdatedAt.Format("2006-01-02 15:04:05"))
		if len(s.Messages) > 0 {
			fmt.Printf("  Messages: %d\n", len(s.Messages))
		}
		fmt.Println()
	}
}

func runSessionsShow(cmd *cobra.Command, args []string) {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".magic", "sessions.db")

	store, err := session.NewStore(dbPath)
	if err != nil {
		fmt.Printf("Failed to open session store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	sess, err := store.LoadSession(context.Background(), args[0])
	if err != nil {
		fmt.Printf("Failed to load session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Session: %s\n", sess.ID)
	fmt.Printf("Platform: %s\n", sess.Platform)
	fmt.Printf("Created:  %s\n", sess.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:  %s\n", sess.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println("\nMessages:")
	fmt.Println("==========")

	for i, msg := range sess.Messages {
		role := strings.ToUpper(msg.Role)
		fmt.Printf("\n[%d] %s:\n%s\n", i+1, role, msg.Content)
	}
}

func runSessionsDelete(cmd *cobra.Command, args []string) {
	sessionID := args[0]

	// Confirm deletion
	fmt.Printf("Are you sure you want to delete session '%s'? (y/N): ", sessionID)
	var confirm string
	fmt.Scanln(&confirm)

	if confirm != "y" && confirm != "Y" {
		fmt.Println("Deletion cancelled.")
		return
	}

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".magic", "sessions.db")

	store, err := session.NewStore(dbPath)
	if err != nil {
		fmt.Printf("Failed to open session store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	err = store.DeleteSession(context.Background(), sessionID)
	if err != nil {
		fmt.Printf("Failed to delete session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Session '%s' deleted successfully.\n", sessionID)
}
