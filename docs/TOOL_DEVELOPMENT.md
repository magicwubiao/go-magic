# Tool Development Guide

## Table of Contents

1. [Creating a New Tool](#creating-a-new-tool)
2. [Tool Interface](#tool-interface)
3. [Parameter Schema](#parameter-schema)
4. [Testing](#testing)
5. [Best Practices](#best-practices)

---

## Creating a New Tool

### Basic Structure

Create a new file in `internal/tool/` directory:

```go
package tool

import (
    "context"
    "fmt"
)

// MyCustomTool is a custom tool for doing something useful
type MyCustomTool struct {
    BaseTool
}

// NewMyCustomTool creates a new instance of MyCustomTool
func NewMyCustomTool() *MyCustomTool {
    return &MyCustomTool{
        BaseTool: *NewBaseTool(
            "my_custom_tool",           // Tool name (unique identifier)
            "Description of what this tool does", // Brief description
            map[string]interface{}{     // Parameter schema
                "type": "object",
                "properties": map[string]interface{}{
                    "param1": map[string]interface{}{
                        "type":        "string",
                        "description": "Description of param1",
                        "default":     "default_value", // Optional default
                    },
                    "param2": map[string]interface{}{
                        "type":        "number",
                        "description": "Description of param2",
                        "minimum":     0,
                        "maximum":     100,
                    },
                },
                "required": []string{"param1"},
            },
        ),
    }
}

// Execute performs the tool's main functionality
func (t *MyCustomTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Get parameters
    param1, ok := args["param1"].(string)
    if !ok {
        return nil, fmt.Errorf("param1 is required")
    }
    
    param2 := 0
    if p, ok := args["param2"].(float64); ok {
        param2 = int(p)
    }
    
    // Do something useful...
    result := map[string]interface{}{
        "processed": true,
        "param1":    param1,
        "param2":    param2,
    }
    
    return result, nil
}
```

### Registering the Tool

Add the tool to `registry.go` in the `RegisterAll()` function:

```go
// Custom tools
r.Register(NewMyCustomTool())
```

---

## Tool Interface

All tools must implement the `Tool` interface:

```go
type Tool interface {
    Name() string              // Unique identifier
    Description() string       // Human-readable description
    Schema() map[string]interface{} // Parameter schema
    Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}
```

### Name()

- Must be unique across all tools
- Use lowercase with underscores (snake_case)
- Examples: `read_file`, `web_search`, `my_custom_tool`

### Description()

- Brief explanation of what the tool does
- Include input/output expectations
- Max 1-2 sentences

### Schema()

Returns a JSON Schema defining accepted parameters:

```go
map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "param_name": map[string]interface{}{
            "type":        "string", // string, number, boolean, array, object
            "description": "What this parameter does",
            "default":     "default value", // Optional
            "enum":        []interface{}{"opt1", "opt2"}, // Optional
            "minimum":     0, // For numbers
            "maximum":     100,
            "pattern":     "^[a-z]+$", // Regex for strings
        },
    },
    "required": []string{"required_param"},
}
```

### Execute()

Main implementation. Must:
- Accept `context.Context` for cancellation
- Return `(interface{}, error)`
- Be thread-safe

---

## Parameter Schema

### Common Parameter Types

#### String
```go
"properties": map[string]interface{}{
    "name": map[string]interface{}{
        "type":        "string",
        "description": "User's name",
        "default":     "Anonymous",
        "pattern":     "^[A-Za-z]+$", // Regex validation
    },
}
```

#### Number
```go
"properties": map[string]interface{}{
    "age": map[string]interface{}{
        "type":        "number",
        "description": "User's age",
        "minimum":     0,
        "maximum":     150,
    },
}
```

#### Boolean
```go
"properties": map[string]interface{}{
    "enabled": map[string]interface{}{
        "type":        "boolean",
        "description": "Enable feature",
        "default":     false,
    },
}
```

#### Array
```go
"properties": map[string]interface{}{
    "tags": map[string]interface{}{
        "type":        "array",
        "description": "List of tags",
        "items": map[string]interface{}{
            "type": "string",
        },
    },
}
```

#### Enum
```go
"properties": map[string]interface{}{
    "status": map[string]interface{}{
        "type":        "string",
        "description": "Task status",
        "enum":        []interface{}{"pending", "running", "completed"},
    },
}
```

---

## Testing

### Unit Test Structure

```go
package tool

import (
    "context"
    "testing"
)

func TestMyCustomTool(t *testing.T) {
    tool := NewMyCustomTool()
    ctx := context.Background()

    t.Run("successful execution", func(t *testing.T) {
        result, err := tool.Execute(ctx, map[string]interface{}{
            "param1": "test_value",
            "param2": 42,
        })
        if err != nil {
            t.Errorf("unexpected error: %v", err)
        }
        if result == nil {
            t.Error("expected non-nil result")
        }
    })

    t.Run("missing required param", func(t *testing.T) {
        _, err := tool.Execute(ctx, map[string]interface{}{
            "param2": 42,
        })
        if err == nil {
            t.Error("expected error for missing required param")
        }
    })
}
```

### Integration Tests

```go
func TestMyCustomToolIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    
    // Test with real resources
    registry := NewRegistry()
    registry.Register(NewMyCustomTool())
    
    result, err := registry.Execute(context.Background(), "my_custom_tool", map[string]interface{}{
        "param1": "value",
    })
    
    if err != nil {
        t.Errorf("execution failed: %v", err)
    }
    
    // Verify result
    // ...
}
```

---

## Best Practices

### 1. Parameter Validation

Always validate inputs:

```go
func (t *MyTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Check required parameters
    param, ok := args["param"].(string)
    if !ok || param == "" {
        return nil, fmt.Errorf("param is required and must be a non-empty string")
    }
    
    // Validate range
    if num, ok := args["num"].(float64); ok {
        if num < 0 || num > 100 {
            return nil, fmt.Errorf("num must be between 0 and 100")
        }
    }
    
    // ... rest of implementation
}
```

### 2. Context Handling

Respect context cancellation:

```go
func (t *MyTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Check if cancelled
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    
    // For long operations, periodically check
    for i := 0; i < 1000; i++ {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }
        // Do work...
    }
    
    return result, nil
}
```

### 3. Error Handling

Return meaningful errors:

```go
// Bad
return nil, fmt.Errorf("error")

// Good
return nil, fmt.Errorf("failed to process file %s: %w", filePath, err)
```

### 4. Thread Safety

Don't share mutable state between tool instances:

```go
// Bad - shared state
type BadTool struct {
    cache map[string]interface{} // Shared mutable state
}

// Good - immutable or thread-safe
type GoodTool struct {
    baseTool BaseTool
    // No mutable shared state
}
```

### 5. Documentation

Document complex logic:

```go
// ProcessData performs X transformation on the input data.
// It uses Y algorithm to achieve Z result.
//
// Algorithm details:
//   - Step 1: ...
//   - Step 2: ...
func (t *MyTool) processData(input string) (string, error) {
    // ...
}
```

### 6. Timeout Configuration

Allow configurable timeouts:

```go
func (t *MyTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    timeout := 60 * time.Second
    if t, ok := args["timeout"].(float64); ok {
        timeout = time.Duration(t) * time.Second
    }
    
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    
    // ...
}
```

---

## Examples

### Example: File Processing Tool

```go
type FileProcessorTool struct {
    BaseTool
}

func NewFileProcessorTool() *FileProcessorTool {
    return &FileProcessorTool{
        BaseTool: *NewBaseTool(
            "file_processor",
            "Process and transform file contents",
            map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "file_path": map[string]interface{}{
                        "type":        "string",
                        "description": "Path to the file to process",
                    },
                    "operation": map[string]interface{}{
                        "type":        "string",
                        "description": "Operation to perform",
                        "enum":        []interface{}{"uppercase", "lowercase", "reverse"},
                    },
                },
                "required": []string{"file_path", "operation"},
            },
        ),
    }
}

func (t *FileProcessorTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    filePath, ok := args["file_path"].(string)
    if !ok {
        return nil, fmt.Errorf("file_path is required")
    }
    
    operation, ok := args["operation"].(string)
    if !ok {
        return nil, fmt.Errorf("operation is required")
    }
    
    // Read file
    content, err := os.ReadFile(filePath)
    if err != nil {
        return nil, fmt.Errorf("failed to read file: %w", err)
    }
    
    // Process
    var result string
    switch operation {
    case "uppercase":
        result = strings.ToUpper(string(content))
    case "lowercase":
        result = strings.ToLower(string(content))
    case "reverse":
        runes := []rune(string(content))
        for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
            runes[i], runes[j] = runes[j], runes[i]
        }
        result = string(runes)
    default:
        return nil, fmt.Errorf("unknown operation: %s", operation)
    }
    
    return map[string]interface{}{
        "original_length": len(content),
        "result":         result,
    }, nil
}
```

### Example: API Tool

```go
type APICallTool struct {
    BaseTool
    client *http.Client
}

func NewAPICallTool() *APICallTool {
    return &APICallTool{
        BaseTool: *NewBaseTool(
            "api_call",
            "Make HTTP API calls",
            map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "url": map[string]interface{}{
                        "type":        "string",
                        "description": "API endpoint URL",
                    },
                    "method": map[string]interface{}{
                        "type":        "string",
                        "description": "HTTP method",
                        "enum":        []interface{}{"GET", "POST", "PUT", "DELETE"},
                        "default":     "GET",
                    },
                    "headers": map[string]interface{}{
                        "type":        "object",
                        "description": "HTTP headers",
                    },
                    "body": map[string]interface{}{
                        "type":        "string",
                        "description": "Request body (for POST/PUT)",
                    },
                },
                "required": []string{"url"},
            },
        ),
        client: &http.Client{Timeout: 30 * time.Second},
    }
}

func (t *APICallTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    url, ok := args["url"].(string)
    if !ok {
        return nil, fmt.Errorf("url is required")
    }
    
    method := "GET"
    if m, ok := args["method"].(string); ok {
        method = m
    }
    
    req, err := http.NewRequestWithContext(ctx, method, url, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }
    
    // Add headers
    if headers, ok := args["headers"].(map[string]interface{}); ok {
        for k, v := range headers {
            if vs, ok := v.(string); ok {
                req.Header.Set(k, vs)
            }
        }
    }
    
    resp, err := t.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    
    return map[string]interface{}{
        "status":  resp.StatusCode,
        "headers": resp.Header,
        "body":    string(body),
    }, nil
}
```

---

## Checklist

Before submitting a new tool, ensure:

- [ ] Tool name is unique and follows naming conventions
- [ ] Description is clear and concise
- [ ] Parameter schema is complete with types and descriptions
- [ ] Required parameters are specified
- [ ] Execute() handles all error cases
- [ ] Context cancellation is respected
- [ ] Unit tests are written
- [ ] Tool is registered in registry.go
- [ ] Documentation is added to TOOLS.md
