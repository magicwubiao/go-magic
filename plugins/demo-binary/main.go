// Binary Plugin Demo for go-magic
// Build: go build -o demo-binary main.go
// This demonstrates how to write a binary plugin that communicates via stdin/stdout JSON.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"
)

// Request is the JSON structure go-magic sends to the binary via stdin
type Request struct {
	Command string                 `json:"command"`
	Args    []string               `json:"args"`
	Config  map[string]interface{} `json:"config"`
	Session string                 `json:"session_id"`
}

// Response is the JSON structure the binary returns via stdout
type Response struct {
	Status  string      `json:"status"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func main() {
	if len(os.Args) > 1 {
		// Direct CLI invocation: demo-binary <command> [args...]
		handleDirect(os.Args[1], os.Args[2:])
		return
	}

	// stdin/stdout mode: read JSON request from stdin
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	var req Request
	if err := dec.Decode(&req); err != nil {
		enc.Encode(Response{Status: "error", Error: fmt.Sprintf("invalid request: %v", err)})
		return
	}

	resp := handleCommand(req.Command, req.Args, req.Config, req.Session)
	enc.Encode(resp)
}

func handleDirect(cmd string, args []string) {
	resp := handleCommand(cmd, args, nil, "cli")
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
}

func handleCommand(cmd string, args []string, config map[string]interface{}, session string) Response {
	switch cmd {
	case "hello":
		name := "World"
		if len(args) > 0 {
			name = args[0]
		}
		return Response{
			Status:  "ok",
			Message: fmt.Sprintf("Hello, %s! From binary plugin on %s/%s.", name, runtime.GOOS, runtime.GOARCH),
		}

	case "info":
		return Response{
			Status: "ok",
			Data: map[string]interface{}{
				"os":        runtime.GOOS,
				"arch":      runtime.GOARCH,
				"go_version": runtime.Version(),
				"cpus":      runtime.NumCPU(),
				"pid":       os.Getpid(),
				"session":   session,
				"timestamp": time.Now().Format(time.RFC3339),
			},
		}

	case "ping":
		if len(args) == 0 {
			return Response{Status: "error", Error: "host argument required"}
		}
		return Response{
			Status:  "ok",
			Message: fmt.Sprintf("Ping %s: binary plugin received (actual ping not implemented in demo)", args[0]),
		}

	case "on_load":
		return Response{Status: "ok", Message: "Binary plugin loaded successfully"}

	case "help":
		return Response{
			Status:  "ok",
			Message: "Available commands: hello, info, ping, on_load, help",
		}

	default:
		return Response{Status: "error", Error: fmt.Sprintf("unknown command: %s", cmd)}
	}
}
