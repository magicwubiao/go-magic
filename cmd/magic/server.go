package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/magicwubiao/go-magic/internal/server"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
			magicHome = config.GetMagicHome()
		}

		dbPath := filepath.Join(magicHome, "sessions.db")

		// Ensure directory exists
		if err := os.MkdirAll(magicHome, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create magic home directory: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Database: %s\n", dbPath)

		// Create server
		srv := server.NewServer(dbPath)

		// Handle shutdown signals
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			fmt.Printf("\nReceived signal %v, shutting down...\n", sig)
			if srv != nil {
				srv.Stop()
			}
			// 给一点时间让 goroutine 退出
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()

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

		// Wait for server error (signal handled by goroutine above)
		if err := <-errCh; err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	},
}

func waitForServer(port int) {
	// Simple wait - in production you'd want to check actual readiness
}

func openBrowser(url string) {
	// Browser opening is handled externally
}
