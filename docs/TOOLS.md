# Tool System Documentation

## Overview

The Tool system provides a comprehensive set of built-in tools for various operations including file manipulation, web requests, code execution, data processing, and more.

## Core Features

### Tool Interface

All tools implement the `Tool` interface:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() map[string]interface{}
    Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}
```

### Registry

Tools are registered in a central registry that manages execution, timeout, and logging:

```go
registry := tool.NewRegistry()
registry.RegisterAll() // Register all built-in tools
```

### Context Support

Extended context with metadata, session tracking, and metrics:

```go
tc := tool.NewToolContext(ctx).
    WithSession("session-id").
    WithUser("user-id").
    WithRequest("request-id").
    WithTool("tool-name")
```

### Middleware Chain

Tools can be wrapped with middleware for logging, metrics, retry, rate limiting, etc.:

```go
middleware := tool.ChainMiddlewares(
    tool.LoggingMiddleware(logger),
    tool.MetricsMiddleware(metrics),
    tool.RetryMiddleware(),
)
```

### Caching

Built-in in-memory caching with TTL support:

```go
cache := tool.NewInMemoryCache(1000)
executor := tool.NewCachedToolExecutor(registry, cache)
```

### Async Execution

Support for long-running async tasks:

```go
asyncExecutor := tool.NewAsyncToolExecutor(registry, 10) // 10 workers
jobID, err := asyncExecutor.Submit(ctx, "tool_name", params)
```

---

## Built-in Tools

### File Operations

#### read_file
Read contents of a file.

```json
{
  "operation": "read",
  "file_path": "/path/to/file",
  "limit": 1000,
  "offset": 0
}
```

#### write_file
Write content to a file.

```json
{
  "operation": "write",
  "file_path": "/path/to/file",
  "content": "file content",
  "append": false
}
```

#### list_files
List files in a directory.

```json
{
  "path": "/path/to/dir",
  "pattern": "*.go",
  "include_hidden": false
}
```

#### search_in_files
Search for text in files.

```json
{
  "pattern": "search term",
  "path": "/path/to/search",
  "file_pattern": "*.go",
  "case_sensitive": false
}
```

#### directory_tree
Display directory tree structure.

```json
{
  "path": "/path/to/dir",
  "max_depth": 3
}
```

### Web Operations

#### web_search
Search the web for information.

```json
{
  "query": "search query",
  "num_results": 5
}
```

#### web_extract
Extract content from a URL.

```json
{
  "url": "https://example.com",
  "selector": ".content",
  "timeout": 60
}
```

### Code Execution

#### execute_command
Execute shell commands (with security whitelist).

```json
{
  "command": "ls -la",
  "timeout": 30
}
```

#### python_execute
Execute Python code.

```json
{
  "code": "print('Hello, World!')",
  "timeout": 60
}
```

#### node_execute
Execute JavaScript/Node.js code.

```json
{
  "code": "console.log('Hello')",
  "timeout": 60
}
```

### Data Processing

#### json
Process JSON data.

```json
{
  "operation": "parse|format|query|validate|minify",
  "data": "{\"key\": \"value\"}",
  "path": "$.key",
  "indent": 2
}
```

Operations:
- `parse`: Parse JSON string to object
- `format`: Pretty-print JSON with indentation
- `query`: Query JSON using JSONPath
- `validate`: Validate JSON syntax
- `minify`: Minify JSON (remove whitespace)

#### yaml
Process YAML data.

```json
{
  "operation": "parse|format|to_json|from_json",
  "data": "key: value"
}
```

#### csv
Process CSV data.

```json
{
  "operation": "parse|filter|stats|transform",
  "data": "header1,header2\nval1,val2",
  "delimiter": ",",
  "has_header": true,
  "column": "header1",
  "value": "val1",
  "operator": "eq|ne|gt|lt|ge|le|contains"
}
```

#### math
Mathematical operations.

```json
{
  "operation": "add|subtract|multiply|divide|power|sqrt|abs|round|floor|ceil|sin|cos|tan|log|log10|exp|min|max|sum|avg|median|stddev|constants",
  "a": 10,
  "b": 5,
  "numbers": [1, 2, 3, 4, 5],
  "precision": 2
}
```

### String Operations

#### string
String manipulation operations.

```json
{
  "operation": "upper|lower|title|trim|replace|regex|split|join|reverse|length|contains|startswith|endswith|encode_base64|decode_base64|url_encode|url_decode",
  "text": "Hello World",
  "pattern": "pattern or substring",
  "replacement": "replacement text",
  "delimiter": ","
}
```

### Hash & Crypto

#### hash
Calculate hash values.

```json
{
  "algorithm": "md5|sha1|sha256|sha512",
  "text": "input text",
  "encoding": "hex|base64"
}
```

#### uuid
Generate or validate UUIDs.

```json
{
  "operation": "generate|validate",
  "version": 4,
  "namespace": "dns|url|oid",
  "name": "name for v5"
}
```

### Random & Time

#### random
Generate random values.

```json
{
  "operation": "number|string|choice|uuid",
  "min": 1,
  "max": 100,
  "length": 16,
  "charset": "alphanumeric|alpha|numeric|hex",
  "choices": ["a", "b", "c"],
  "count": 5
}
```

#### time
Time operations.

```json
{
  "operation": "now|parse|format|add|diff|timestamp|sleep",
  "time_str": "2024-01-01 12:00:00",
  "format": "2006-01-02 15:04:05",
  "duration": "1h30m",
  "timezone": "UTC"
}
```

### Environment & System

#### env
Environment variable operations.

```json
{
  "operation": "get|set|list|unset|get_all",
  "name": "VAR_NAME",
  "value": "var_value"
}
```

#### system
Get system information.

```json
{
  "info_type": "os|cpu|memory|go|all"
}
```

### Other Tools

#### memory_store
Store information in memory.

```json
{
  "key": "my_key",
  "value": "information to store",
  "ttl": 3600
}
```

#### memory_recall
Retrieve stored information.

```json
{
  "key": "my_key"
}
```

#### vision
Analyze images.

```json
{
  "operation": "analyze",
  "image_path": "/path/to/image"
}
```

#### image_gen
Generate images.

```json
{
  "prompt": "image description",
  "count": 1
}
```

#### web_fetch
Fetch web page content.

```json
{
  "url": "https://example.com"
}
```

---

## Tool Groups

Tools can be organized into groups for easier management:

```go
groupManager := tool.NewGroupManager()
fileGroup := groupManager.CreateGroup("file", "File operations")
fileGroup.Add(registry.Get("read_file"))
```

Default groups:
- `file`: File operations
- `web`: Web operations
- `code`: Code execution
- `memory`: Memory/retrieval

---

## Documentation Generation

Generate documentation for tools:

```go
gen := tool.NewDocumentationGenerator()

// Markdown documentation
mdDoc := gen.GenerateMarkdown(tools)

// OpenAPI specification
openapiSpec := gen.GenerateOpenAPISpec(tools)

// Single tool help
helpText := gen.GenerateToolHelp("json", tools)
```

---

## Best Practices

1. **Use Context**: Always pass context for cancellation and timeout support
2. **Handle Errors**: Check both error return and success flag in results
3. **Set Timeouts**: Configure appropriate timeouts for long-running operations
4. **Use Caching**: Enable caching for expensive, repeatable operations
5. **Validate Input**: Use the built-in parameter validation
6. **Log Operations**: Enable logging middleware for debugging

---

## Performance Considerations

- **Tool Caching**: Enable caching for read-heavy operations
- **Rate Limiting**: Use rate limiting middleware to prevent abuse
- **Async Execution**: Use async execution for long-running tasks
- **Resource Limits**: Configure appropriate resource limits per tool
