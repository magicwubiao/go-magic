---
name: debug-go
description: "Go debugging best practices, tools, and techniques for effective troubleshooting."
version: 1.0.0
author: magic
license: MIT
metadata:
  hermes:
    tags: [go, debugging, troubleshooting, delve]
    category: software-development
---

# Go Debugging Guide

Comprehensive guide to debugging Go applications using various tools and techniques.

## When to Use

Load this skill when:
- Debugging Go application crashes or panics
- Investigating performance issues
- Setting up debugging environment
- Troubleshooting concurrency issues (goroutines, channels)
- Profiling memory or CPU usage
- Need to understand Go's debugging tools

## Quick Reference

### Delve (go-delve)
```bash
# Install
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug current package
dlv debug

# Debug specific test
dlv test ./...

# Debug running process
dlv attach <pid>

# Debug binary
dlv exec ./myapp

# Common commands inside dlv
(dlv) break main.go:42    # Set breakpoint
(dlv) continue              # Continue execution
(dlv) next                  # Next line
(dlv) step                  # Step into
(dlv) print variable         # Print variable
(dlv) goroutines            # List goroutines
```

### Quick Debugging with fmt
```go
// Quick debugging - don't forget to remove!
fmt.Printf("DEBUG: value=%v, type=%T\n", value, value)

// Pretty print structs
import "encoding/json"
func prettyPrint(v interface{}) {
    b, _ := json.MarshalIndent(v, "", "  ")
    fmt.Println(string(b))
}
```

## Debugging Tools

### Delve (Recommended)
| Feature | Command |
|---------|---------|
| Set breakpoint | `dlv break <file>:<line>` |
| Conditional break | `dlv cond <breakpoint> <condition>` |
| View variables | `dlv print <var>` |
| Stack trace | `dlv stack` |
| Goroutines | `dlv goroutines` |
| Threads | `dlv threads` |

### pprof (Profiling)
```bash
# Add to your code
import _ "net/http/pprof"

# Start server
go run main.go

# Access profiles
# CPU: http://localhost:6060/debug/pprof/profile
# Memory: http://localhost:6060/debug/pprof/heap
# Goroutines: http://localhost:6060/debug/pprof/goroutine

# Command line
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

## Common Debugging Techniques

### 1. Panic Recovery
```go
func main() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Recovered from panic: %v", r)
            debug.PrintStack() // Print stack trace
        }
    }()
    
    // Your code here
    riskyOperation()
}
```

### 2. Race Condition Detection
```bash
# Run with race detector
go run -race main.go
go test -race ./...

# Example race condition
var counter int
go func() {
    counter++ // Race condition!
}()
```

### 3. Deadlock Detection
```go
// Enable deadlock detection with runtime
import "runtime/debug"

func init() {
    debug.SetGCPercent(-1) // Optional: disable GC for debugging
}

// Use go.uber.org/goleak to detect goroutine leaks
```

### 4. HTTP Debugging
```go
// Log HTTP requests
import "net/http/httputil"

func debugTransport() http.RoundTripper {
    return &dumpTransport{}
}

type dumpTransport struct{}

func (d *dumpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    dump, _ := httputil.DumpRequestOut(req, true)
    fmt.Println(string(dump))
    return http.DefaultTransport.RoundTrip(req)
}
```

## IDE Integration

### VS Code + Go Extension
- Set breakpoints by clicking gutter
- Launch debug: Run → Start Debugging (F5)
- Debug tests: Click "debug" above test function
- Variables pane shows local variables
- Call stack shows goroutine stack

### GoLand (JetBrains)
- Full-featured debugger
- Evaluate expressions
- Conditional breakpoints
- Inline variable values
- Goroutine debugging view

## Logging Best Practices

### Use log/slog (Go 1.21+)
```go
import "log/slog"

// Structured logging
slog.Info("User logged in", 
    "user_id", userID,
    "ip", ipAddress,
    "duration", time.Since(start))

// Debug level
slog.Debug("Cache miss", "key", cacheKey)

// With context
logger := slog.With("request_id", requestID)
logger.Info("Processing request")
```

### Log Levels
```go
// Development: show all
slog.SetLogLoggerLevel(slog.LevelDebug)

// Production: info and above
slog.SetLogLoggerLevel(slog.LevelInfo)
```

## Pitfalls

### Forgetting to Close Resources
**Problem**: File/connection leaks  
**Solution**: Always use `defer`:
```go
file, err := os.Open("file.txt")
if err != nil {
    return err
}
defer file.Close() // Will always run
```

### Ignoring Errors
**Problem**: Silent failures  
**Solution**: Always check errors:
```go
// Bad
doSomething()

// Good
if err := doSomething(); err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}
```

### Race Conditions
**Problem**: Concurrent access to shared state  
**Solution**: Use channels or sync primitives:
```go
// Use mutex
var mu sync.Mutex
var counter int

func increment() {
    mu.Lock()
    defer mu.Unlock()
    counter++
}
```

## Verification

After setting up debugging:
1. `dlv debug` starts without errors
2. Breakpoints hit correctly
3. Variables inspectable with `print`
4. `go test -race` passes
5. pprof profiles accessible at `/debug/pprof/`

## Tools & References

- [Delve Documentation](https://github.com/go-delve/delve)
- [Go Profiling with pprof](https://pkg.go.dev/net/http/pprof)
- [Go Race Detector](https://go.dev/blog/race-detector)
- [Effective Go - Debugging](https://go.dev/doc/effective_go#errors)
- [Uber Go Style Guide](https://github.com/uber-go/guide)
