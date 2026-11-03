---
name: debug-go
description: "Use when working with Go-specific debugging tooling — Delve breakpoints and goroutine inspection, pprof profiles and flame graphs, the race detector, goroutine dumps and goleak in tests, GOTRACEBACK, or slog logging setup. For language-agnostic root-cause workflow (log reading, hypothesis checklist, stack-trace triage), the built-in debug skill covers it."
version: 1.2.0
author: go-magic
license: MIT
tags: [go, delve, pprof, race-detector, goleak]
tools: [execute_command, read_file]
---

# Go Debugging Guide

Go-specific debugging **tooling**: Delve, pprof, the race detector, goroutine dumps, `goleak`, `GOTRACEBACK`, and `slog`.

## Scope

This skill is the Go **toolchain** companion to the general `debug` skill:

- **General root-cause workflow** — reproducing, reading logs and stack traces, the hypothesis checklist, emergency response → use the `debug` skill. It also has a short multi-language profiling quick reference (Go / Python / Node).
- **This skill** — how to actually drive the Go tools: Delve sessions, pprof deep usage (`-base` diffing, goroutine profiles), `-race`, `goleak`, runtime knobs like `GOTRACEBACK`.

When a problem is "a program misbehaves" and the language is Go, `debug` gives you the method and this skill gives you the instruments.

## When to Use

Load this skill when:
- Setting up or driving Delve (`dlv debug/attach/exec`)
- Collecting or reading pprof profiles (CPU, heap, goroutine, `-base` diffing)
- Running or interpreting the race detector
- Producing goroutine dumps (`SIGQUIT`, `runtime.Stack`) or adding `goleak` to tests
- Tuning runtime diagnostics (`GOTRACEBACK`) or structured logging (`slog`)

## Quick Reference

### Delve
```bash
go install github.com/go-delve/delve/cmd/dlv@latest

dlv debug            # debug the current package
dlv test ./...       # debug tests
dlv attach <pid>     # attach to a running process
dlv exec ./myapp     # debug a built binary

# Inside dlv
(dlv) break main.go:42      # set a breakpoint
(dlv) cond <bp> i == 10     # conditional breakpoint
(dlv) continue / next / step
(dlv) print variable
(dlv) goroutines            # list goroutines
(dlv) goroutine 12          # switch to goroutine 12
(dlv) stack                 # stack of the current goroutine
```

### Quick prints
```go
// Plain values — remove before committing
fmt.Printf("DEBUG: value=%v, type=%T\n", value, value)

// Structured dump
import "encoding/json"

func prettyPrint(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}
```

Add `log.SetFlags(log.LstdFlags | log.Lshortfile)` so every log line carries `file:line`.

## Tools

### Delve
| Task | Command |
|------|---------|
| Set breakpoint | `break <file>:<line>` |
| Conditional break | `cond <breakpoint> <expr>` |
| Print a variable | `print <var>` |
| Stack trace | `stack` |
| List goroutines | `goroutines` |
| List threads | `threads` |

### pprof
```go
import _ "net/http/pprof"   // registers /debug/pprof/*
```
```bash
# Profiles at http://localhost:6060/debug/pprof/
#   profile    CPU     heap       memory      goroutine   block   mutex

go tool pprof -http=:8081 http://localhost:6060/debug/pprof/profile?seconds=30
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/heap

# From tests
go test -cpuprofile=cpu.prof -bench=. && go tool pprof -http=:8081 cpu.prof
go test -memprofile=mem.prof -bench=. && go tool pprof -http=:8081 mem.prof
```

Add `-base` to a heap profile pair to see what leaked between two points in time:
```bash
go tool pprof -base heap1.prof heap2.prof
```

## Common Techniques

### 1. Panic recovery and stack traces
```go
func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered: %v\n%s", r, debug.Stack())
		}
	}()
	run()
}
```
`debug.Stack()` is the right tool here; it captures the stack of the *current* goroutine. For a panic in another goroutine, set `GOTRACEBACK=all` so the runtime prints every goroutine's stack:
```bash
GOTRACEBACK=all ./myapp
panic: ... [recovered]

goroutine 18 [running]: ...
goroutine 7 [chan receive]: ...
```

### 2. Data races
```bash
go run -race main.go
go test -race ./...
```
```go
var counter int
go func() { counter++ }()   // race: read-modify-write on shared memory
```
Fix with the minimum synchronization: a mutex, atomics, or by passing the value over a channel instead of sharing it. `-race` only reports races it actually observes — a clean run is not proof of absence, so run it in CI and under test load.

### 3. Deadlocks and goroutine leaks

Go deadlocks that involve *all* goroutines are detected by the runtime:
```
fatal error: all goroutines are asleep - deadlock!
```
Deadlocks that leave some goroutine running (the common case) are not. To find them:

```bash
# Dump every goroutine's stack (does not need a debugger)
kill -QUIT <pid>            # or send SIGQUIT
# or set GOTRACEBACK=all before starting
```

```go
// Dump from inside the process, e.g. on a signal handler or debug endpoint
buf := make([]byte, 1<<20)
n := runtime.Stack(buf, true)   // true = all goroutines
fmt.Println(string(buf[:n]))
```

```bash
# Goroutine profile: which goroutines are blocked, and on what
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/goroutine
```

For **goroutine leaks** in tests, use `go.uber.org/goleak` so a leak fails the test instead of showing up as memory growth in production:
```go
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

Prevention beats detection:
- Give every `context.Context` a deadline; never pass `context.Background()` into a network call
- Never send/receive on an unbuffered channel without a `select` that can also take a timeout or `ctx.Done()`
- Always acquire multiple mutexes in one documented order
- Make sure every spawned goroutine has a defined exit condition

Note: `debug.SetGCPercent(-1)` disables automatic GC and has **nothing** to do with deadlock detection — do not reach for it when diagnosing a hang.

### 4. HTTP debugging
```go
import "net/http/httputil"

type dumpTransport struct{ rt http.RoundTripper }

func (d *dumpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if dump, err := httputil.DumpRequestOut(req, true); err == nil {
		fmt.Printf("%s\n", dump)
	}
	resp, err := d.rt.RoundTrip(req)
	if err == nil {
		if dump, derr := httputil.DumpResponse(resp, true); derr == nil {
			fmt.Printf("%s\n", dump)
		}
	}
	return resp, err
}

client := &http.Client{Transport: &dumpTransport{rt: http.DefaultTransport}}
```

## IDE Integration

### VS Code + Go
- Breakpoints from the gutter; F5 to launch
- "debug test" above a test function
- Variables and call-stack panes; goroutine view in the debug console

### GoLand
- Full debugger with expression evaluation
- Conditional breakpoints and inline values
- Dedicated goroutine view

## Logging

Use `log/slog` (Go 1.21+) for structured logs:
```go
slog.Info("user logged in",
	"user_id", userID,
	"ip", ip,
	"duration", time.Since(start))

slog.Debug("cache miss", "key", cacheKey)

logger := slog.With("request_id", requestID)
logger.Info("processing request")
```
```go
slog.SetLogLoggerLevel(slog.LevelDebug)  // development: everything
slog.SetLogLoggerLevel(slog.LevelInfo)   // production: info and above
```

## Pitfalls

### Forgetting to close resources
**Problem**: leaked file descriptors, connections, response bodies.
**Solution**: `defer` the close immediately after the successful open — and check the error before deferring, or you will dereference nil.

```go
f, err := os.Open("file.txt")
if err != nil {
	return err
}
defer f.Close()
```
For HTTP responses, also drain the body (`io.Copy(io.Discard, resp.Body)`) or the connection will not be reused.

### Ignoring errors
**Problem**: silent failures that surface far from the cause.
**Solution**: wrap and return:
```go
if err := doSomething(); err != nil {
	return fmt.Errorf("doSomething: %w", err)
}
```

### Sharing memory without synchronization
```go
var (
	mu      sync.Mutex
	counter int
)

func increment() {
	mu.Lock()
	defer mu.Unlock()
	counter++
}
```
Prefer channels for ownership transfer; use a mutex to protect shared state.

## Verification

After setting up debugging:
1. `dlv debug` starts and breakpoints hit
2. Variables inspectable with `print`
3. `go test -race ./...` passes
4. pprof endpoints respond at `/debug/pprof/`
5. A goroutine dump can be produced on demand (signal or endpoint)

## References

- [Delve](https://github.com/go-delve/delve)
- [Profiling Go programs](https://go.dev/blog/pprof)
- [net/http/pprof](https://pkg.go.dev/net/http/pprof)
- [Go race detector](https://go.dev/blog/race-detector)
- [Diagnostics — The Go Blog](https://go.dev/doc/diagnostics)
- [goleak](https://github.com/uber-go/goleak)
- [Uber Go Style Guide](https://github.com/uber-go/guide)
