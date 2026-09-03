---
name: debug
description: "Comprehensive debugging assistance with error analysis, log parsing, debugging strategies, and performance profiling"
version: 1.0.0
author: go-magic
tags: [debug, error-analysis, logging, profiling, troubleshooting]
tools: [read_file, file_search, analyze_error, suggest_fix, trace_execution]
---

# Debug Skill

## When to Use

Load this skill when:
- Debugging errors or crashes
- Analyzing application logs
- Investigating performance issues
- Troubleshooting unexpected behavior
- Setting up debugging workflows
- Optimizing application performance

## Capabilities

### 1. Error Analysis

Systematic error investigation:
- **Stack Trace Analysis**: Parse and understand stack traces
- **Error Pattern Recognition**: Identify common error patterns
- **Root Cause Analysis**: Trace errors to their source
- **Error Classification**: Categorize by type and severity

### 2. Log Parsing

Log analysis techniques:
- **Structured Log Parsing**: JSON, logfmt formats
- **Log Level Filtering**: Debug, Info, Warn, Error levels
- **Temporal Analysis**: Time-based log correlation
- **Pattern Matching**: Regex-based log searching
- **Log Aggregation**: Multi-source log analysis

### 3. Debugging Strategies

Systematic debugging approaches:
- **Binary Search Debugging**: Isolate problem areas
- **Rubber Duck Debugging**: Explain code line by line
- **Backtracking**: Trace from error to cause
- **Divide and Conquer**: Split complex problems
- **Reproduction**: Create minimal test cases

### 4. Performance Analysis

Performance investigation:
- **CPU Profiling**: Identify CPU bottlenecks
- **Memory Profiling**: Find memory leaks
- **I/O Analysis**: Optimize input/output operations
- **Concurrency Analysis**: Detect race conditions
- **Latency Analysis**: Trace request latencies

## Error Analysis Framework

### Error Classification

| Severity | Description | Response Time |
|----------|-------------|---------------|
| Critical | System down, data loss | Immediate |
| High | Major feature broken | < 1 hour |
| Medium | Feature degraded | < 4 hours |
| Low | Minor issue, workaround | < 24 hours |

### Common Error Patterns

#### Null/Undefined Errors
```
Pattern: Cannot read property 'X' of undefined
Cause: Accessing property on null/undefined object
Fix: Add null checks or optional chaining
```

#### Type Errors
```
Pattern: TypeError: X is not a function
Cause: Calling non-function or wrong type
Fix: Validate types before operations
```

#### Resource Exhaustion
```
Pattern: Out of memory, connection timeout
Cause: Resource leaks or insufficient resources
Fix: Resource cleanup, pooling, scaling
```

#### Race Conditions
```
Pattern: Intermittent failures, inconsistent state
Cause: Unsynchronized concurrent access
Fix: Proper locking, atomic operations
```

## Log Analysis Guide

### Log Levels

```
DEBUG: Detailed information for debugging
INFO: General operational information
WARN: Warning conditions that don't affect operation
ERROR: Error conditions that affect operation
FATAL: Severe errors causing termination
```

### Log Parsing Patterns

#### Go Logs
```
2024-01-15T10:30:00Z ERROR handler.go:42: failed to process request: connection timeout

Pattern: TIMESTAMP LEVEL FILE:LINE: MESSAGE
```

#### Python Logs
```
2024-01-15 10:30:00,000 - module.name - ERROR - failed to process request

Pattern: TIMESTAMP - LOGGER - LEVEL - MESSAGE
```

#### JavaScript Logs
```
[2024-01-15T10:30:00.000Z] ERROR (app/12345): failed to process request

Pattern: [TIMESTAMP] LEVEL (CONTEXT): MESSAGE
```

### Correlation IDs

Always include correlation IDs for distributed tracing:
```
request_id=abc123 user_id=456 method=GET path=/api/users
```

## Debugging Strategies

### The DEBUG Method

1. **D**escribe the problem
   - What is happening?
   - What should happen?
   - When did it start?

2. **E**xamine the context
   - Recent changes?
   - Environment differences?
   - Reproduction steps?

3. **B**reak down the problem
   - Identify components involved
   - Isolate the failing part
   - Create minimal reproduction

4. **U**nderstand the code
   - Trace execution flow
   - Check assumptions
   - Verify inputs/outputs

5. **G**enerate hypotheses
   - List possible causes
   - Prioritize by likelihood
   - Design tests for each

### Debugging Checklist

```
[ ] Reproduce the issue consistently
[ ] Check recent code changes
[ ] Verify environment configuration
[ ] Review relevant logs
[ ] Check resource usage (CPU, memory, disk)
[ ] Verify network connectivity
[ ] Test with minimal data/config
[ ] Isolate to specific component
[ ] Check for race conditions
[ ] Verify error handling paths
```

## Performance Analysis

### Profiling Tools by Language

#### Go
```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof

# Trace
go test -trace=trace.out
```

#### Python
```bash
# cProfile
python -m cProfile -o output.prof script.py

# line_profiler
kernprof -l -v script.py

# memory_profiler
python -m memory_profiler script.py
```

#### Node.js
```bash
# Built-in profiler
node --prof script.js
node --prof-process isolate-0x*.log

# Clinic.js
clinic doctor -- node script.js
clinic flame -- node script.js
```

### Performance Metrics

| Metric | Good | Warning | Critical |
|--------|------|---------|----------|
| CPU Usage | < 70% | 70-85% | > 85% |
| Memory Usage | < 70% | 70-85% | > 85% |
| Response Time | < 200ms | 200-500ms | > 500ms |
| Error Rate | < 0.1% | 0.1-1% | > 1% |

### Common Performance Issues

#### Memory Leaks
```
Symptoms: Gradual memory increase, eventual OOM
Detection: Memory profiling, heap dumps
Causes: Unclosed resources, global caches, event listeners
```

#### CPU Bottlenecks
```
Symptoms: High CPU usage, slow response times
Detection: CPU profiling, flame graphs
Causes: Inefficient algorithms, tight loops, regex
```

#### I/O Bottlenecks
```
Symptoms: Waiting on external services, timeouts
Detection: I/O tracing, latency histograms
Causes: Synchronous I/O, missing caching, N+1 queries
```

## Error Analysis Examples

### Example 1: Go Panic Analysis
```
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x123456]

goroutine 1 [running]:
main.processUser(0x0, 0xc000012345)
    /app/main.go:42 +0x25
main.main()
    /app/main.go:15 +0x45

Analysis:
1. Nil pointer dereference at main.go:42
2. processUser called with nil user
3. Check caller at main.go:15
4. Add nil check before calling processUser
```

### Example 2: Python Exception Analysis
```
Traceback (most recent call last):
  File "/app/handler.py", line 42, in handle_request
    result = process_data(data)
  File "/app/processor.py", line 15, in process_data
    return data['key'] / data['value']
ZeroDivisionError: division by zero

Analysis:
1. Division by zero in processor.py:15
2. data['value'] is 0
3. Add validation before division
4. Consider default value or error handling
```

### Example 3: JavaScript Error Analysis
```
TypeError: Cannot read property 'name' of undefined
    at UserProfile.render (/app/components/UserProfile.jsx:25)
    at processChild (/app/node_modules/react-dom/cjs/react-dom.development.js:1234)

Analysis:
1. Accessing .name on undefined object
2. User data not loaded when component renders
3. Add loading state or optional chaining: user?.name
4. Check data fetching logic
```

## Debugging Tools Integration

### analyze_error Tool
Parses error messages and stack traces to:
- Identify error type and location
- Suggest potential causes
- Recommend fixes
- Link to relevant documentation

### suggest_fix Tool
Analyzes code and errors to:
- Generate fix suggestions
- Provide code patches
- Explain the fix rationale
- Show before/after comparison

### trace_execution Tool
Traces program execution to:
- Show call stack
- Display variable values
- Track state changes
- Identify execution path

## Best Practices

### Logging Best Practices
1. **Use structured logging**: JSON format for machine parsing
2. **Include context**: Request IDs, user IDs, timestamps
3. **Log at appropriate levels**: Don't log debug info as error
4. **Be concise**: Log what's needed, not everything
5. **Sanitize sensitive data**: Remove passwords, tokens

### Debugging Best Practices
1. **Reproduce first**: Can't fix what you can't reproduce
2. **Change one thing at a time**: Isolate variables
3. **Use version control**: Easy rollback of debug changes
4. **Write tests**: Prevent regression
5. **Document findings**: Share knowledge

### Performance Best Practices
1. **Measure first**: Profile before optimizing
2. **Focus on hotspots**: 80% of time in 20% of code
3. **Consider trade-offs**: Speed vs memory vs readability
4. **Test under load**: Real-world conditions matter
5. **Monitor continuously**: Performance degrades over time

## Emergency Response

### Critical Error Response
1. **Assess impact**: How many users affected?
2. **Mitigate**: Rollback, feature flag, or workaround
3. **Investigate**: Root cause analysis
4. **Fix**: Implement and test solution
5. **Deploy**: Push fix with monitoring
6. **Post-mortem**: Document and prevent recurrence

### Performance Emergency
1. **Identify bottleneck**: CPU, memory, I/O?
2. **Scale horizontally**: Add instances if possible
3. **Enable circuit breakers**: Fail fast on dependencies
4. **Reduce load**: Rate limiting, queue draining
5. **Optimize hot paths**: Quick wins only
