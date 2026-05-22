package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/magicwubiao/go-magic/internal/server"
)

var (
	serverPort        int
	serverOpenBrowser bool
)

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.Flags().IntVar(&serverPort, "port", 5000, "Server port")
	serverCmd.Flags().BoolVar(&serverOpenBrowser, "open", false, "Open browser after starting")
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the web dashboard server",
	Run: func(cmd *cobra.Command, args []string) {
		magicHome := viper.GetString("magic-home")
		if magicHome == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				homeDir = os.TempDir()
			}
			magicHome = homeDir + "/.magic"
		}

		dbPath := magicHome + "/sessions.db"

		// Ensure directory exists
		if err := os.MkdirAll(magicHome, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create magic home directory: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Database: %s\n", dbPath)

		// Create server
		srv := server.NewServer(dbPath)

		// Handle shutdown signals
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		fmt.Printf("Starting server on http://localhost:%d\n", serverPort)
		fmt.Println()
		fmt.Println("Dashboard features:")
		fmt.Println("  - Chat interface")
		fmt.Println("  - Session management")
		fmt.Println("  - Configuration tools")
		fmt.Println("  - Skill browser")
		fmt.Println("  - System logs")
		fmt.Println()

		// Start server in goroutine
		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Start(serverPort)
		}()

		// Open browser if requested
		if serverOpenBrowser {
			go func() {
				waitForServer(serverPort)
				openBrowser(fmt.Sprintf("http://localhost:%d", serverPort))
			}()
		}

		// Wait for shutdown signal or error
		select {
		case err := <-errCh:
			if err != nil {
				fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
				os.Exit(1)
			}
		case <-quit:
			fmt.Println("\nShutting down server...")
		}
	},
}

func waitForServer(port int) {
	// Simple wait - in production you'd want to check actual readiness
}

func openBrowser(url string) {
	// Browser opening is handled externally
}
