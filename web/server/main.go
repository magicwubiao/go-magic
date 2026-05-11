package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var (
	port       = flag.Int("port", 8648, "Server port")
	upstream   = flag.String("upstream", "http://localhost:8642", "Hermes gateway upstream")
	magicBin   = flag.String("magic", "magic", "go-magic binary path")
	staticPath = flag.String("static", "./dist", "Static files path")
)

func main() {
	flag.Parse()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Proxy to Hermes gateway
	gateway := r.Group("/api")
	gateway.Use(func(c *gin.Context) {
		target := *upstream + c.Request.URL.Path
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}

		req, err := http.NewRequest(c.Request.Method, target, c.Request.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		req.Header = c.Request.Header.Clone()
		req.Header.Set("X-Forwarded-For", c.ClientIP())

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			c.Header(k, v[0])
		}
		c.DataFromReader(resp.StatusCode, resp.ContentLength, resp.Header.Get("Content-Type"), resp.Body, nil)
	})

	// CLI proxy for sessions, logs, etc.
	r.GET("/cli/sessions", handleCLISessions)
	r.POST("/cli/sessions", handleCLISessionCreate)
	r.GET("/cli/logs", handleCLILogs)
	r.GET("/cli/health", handleCLIHealth)

	// Serve static files
	if _, err := os.Stat(*staticPath); err == nil {
		r.NoRoute(func(c *gin.Context) {
			file := filepath.Join(*staticPath, c.Request.URL.Path)
			if _, err := os.Stat(file); err == nil {
				c.File(file)
			} else {
				c.File(filepath.Join(*staticPath, "index.html"))
			}
		})
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("go-magic Web UI starting on %s", addr)
	log.Printf("Proxying to Hermes gateway at %s", *upstream)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func handleCLISessions(c *gin.Context) {
	output, err := runCLI("sessions", "list", "--json")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(output))
}

func handleCLISessionCreate(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	output, err := runCLI("sessions", "create", "--name", req.Name, "--json")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(output))
}

func handleCLILogs(c *gin.Context) {
	file := c.DefaultQuery("file", "agent")
	lines := c.DefaultQuery("lines", "100")

	output, err := runCLI("logs", "--file", file, "--lines", lines)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Parse logs into JSON
	logs := parseLogs(output)
	c.JSON(http.StatusOK, logs)
}

func handleCLIHealth(c *gin.Context) {
	output, err := runCLI("health")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": strings.TrimSpace(output)})
}

func runCLI(args ...string) (string, error) {
	cmd := execCommand(*magicBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cli error: %v, output: %s", err, string(output))
	}
	return string(output), nil
}

func parseLogs(output string) []map[string]string {
	var logs []map[string]string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		// Simple log parsing (implement proper parsing)
		logs = append(logs, map[string]string{
			"time":    "",
			"level":   "info",
			"message": line,
		})
	}
	return logs
}

// execCommand is a wrapper for testing
var execCommand = func(name string, args ...string) command {
	return command{name: name, args: args}
}

type command struct {
	name  string
	args  []string
}

func (c command) CombinedOutput() ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}
