package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/magicwubiao/go-magic/internal/server"
)

var (
	serverPort         string
	serverStatic       bool
	serverOpenBrowser  bool
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the web dashboard server",
	Long: `Start the web-based dashboard for go-magic.
	
The dashboard provides:
- Interactive chat interface
- Session management
- Configuration tools
- Skill browser
- System logs

Example:
  magic server                    Start server on default port 5000
  magic server -p 8080           Start server on port 8080
  magic server --open             Start server and open browser`,
	RunE: runServer,
}

func init() {
	serverCmd.Flags().StringVar(&serverPort, "port", "5000", "Port to listen on")
	serverCmd.Flags().BoolVar(&serverStatic, "static", true, "Serve static files from embedded web UI")
	serverCmd.Flags().BoolVar(&serverOpenBrowser, "open", false, "Open browser on startup")
	
	rootCmd.AddCommand(serverCmd)
}

func runServer(cmd *cobra.Command, args []string) error {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║              go-magic Web Dashboard                    ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Get database path
	magicHome := viper.GetString("magic-home")
	if magicHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = os.TempDir()
		}
		magicHome = homeDir + "/.go-magic"
	}

	dbPath := magicHome + "/sessions.db"
	
	// Ensure directory exists
	if err := os.MkdirAll(magicHome, 0755); err != nil {
		return fmt.Errorf("failed to create magic home directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	fmt.Printf("Database: %s\n", dbPath)

	// Create server
	srv := server.NewServer(serverPort, db)

	// Handle shutdown signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("Starting server on http://localhost:%s\n", serverPort)
		fmt.Println()
		fmt.Println("Dashboard features:")
		fmt.Println("  - Chat interface")
		fmt.Println("  - Session management")
		fmt.Println("  - Configuration tools")
		fmt.Println("  - Skill browser")
		fmt.Println("  - System logs")
		fmt.Println()
		errCh <- srv.Start()
	}()

	// Open browser if requested
	if serverOpenBrowser {
		go func() {
			waitForServer(serverPort)
			openBrowser(fmt.Sprintf("http://localhost:%s", serverPort))
		}()
	}

	// Wait for shutdown signal or error
	select {
	case err := <-errCh:
		return err
	case <-quit:
		fmt.Println("\nShutting down server...")
		return srv.Stop(nil)
	}
}

// waitForServer waits for the server to be ready
func waitForServer(port string) {
	// Simple wait - in production, use HTTP health check
	for i := 0; i < 30; i++ {
		fmt.Printf("Waiting for server... (%d/30)\r", i+1)
	}
	fmt.Println()
}

// openBrowser opens the default browser at the given URL
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("cmd", "/c", "start", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		fmt.Printf("Failed to open browser: %v\n", err)
	}
}
