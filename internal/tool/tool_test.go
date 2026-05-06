package tool

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// Test Helper Functions
// ============================================================================

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected float64
		ok       bool
	}{
		{"float64", float64(1.5), 1.5, true},
		{"int", int(42), 42, true},
		{"int64", int64(100), 100, true},
		{"string", "3.14", 3.14, true},
		{"invalid string", "abc", 0, false},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toFloat64(tt.input)
			if ok != tt.ok {
				t.Errorf("toFloat64(%v) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if ok && result != tt.expected {
				t.Errorf("toFloat64(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToFloat64Array(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected []float64
		ok       bool
	}{
		{"[]any", []any{1.0, 2.0, 3.0}, []float64{1, 2, 3}, true},
		{"[]float64", []float64{1.5, 2.5}, []float64{1.5, 2.5}, true},
		{"[]int", []int{1, 2, 3}, []float64{1, 2, 3}, true},
		{"empty", []any{}, []float64{}, true},
		{"invalid", "not an array", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toFloat64Array(tt.input)
			if ok != tt.ok {
				t.Errorf("toFloat64Array(%v) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if ok && len(result) != len(tt.expected) {
				t.Errorf("toFloat64Array(%v) length = %d, want %d", tt.input, len(result), len(tt.expected))
			}
		})
	}
}

// ============================================================================
// Test JSON Tool
// ============================================================================

func TestJSONTool(t *testing.T) {
	tool := NewJSONTool()
	ctx := context.Background()

	t.Run("parse valid JSON", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "parse",
			"data":      `{"key": "value"}`,
		})
		if err != nil {
			t.Errorf("parse failed: %v", err)
		}
		if result == nil {
			t.Error("parse returned nil")
		}
	})

	t.Run("parse invalid JSON", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"operation": "parse",
			"data":      `{invalid}`,
		})
		if err == nil {
			t.Error("parse should fail for invalid JSON")
		}
	})

	t.Run("validate valid JSON", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "validate",
			"data":      `{"key": "value"}`,
		})
		if err != nil {
			t.Errorf("validate failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["valid"] != true {
			t.Error("validate should return valid=true")
		}
	})

	t.Run("format JSON", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "format",
			"data":      `{"key":"value"}`,
			"indent":    2,
		})
		if err != nil {
			t.Errorf("format failed: %v", err)
		}
		resultStr := result.(string)
		if len(resultStr) == 0 {
			t.Error("format returned empty string")
		}
	})

	t.Run("minify JSON", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "minify",
			"data":      `{"key": "value", "nested": {"a": 1}}`,
		})
		if err != nil {
			t.Errorf("minify failed: %v", err)
		}
		resultStr := result.(string)
		if len(resultStr) == 0 {
			t.Error("minify returned empty string")
		}
	})

	t.Run("query JSON", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "query",
			"data":      `{"key": "value"}`,
			"path":      ".key",
		})
		if err != nil {
			t.Errorf("query failed: %v", err)
		}
		if result != "value" {
			t.Errorf("query returned %v, want 'value'", result)
		}
	})
}

// ============================================================================
// Test String Tool
// ============================================================================

func TestStringTool(t *testing.T) {
	tool := NewStringTool()
	ctx := context.Background()

	t.Run("upper", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "upper",
			"text":      "hello",
		})
		if err != nil {
			t.Errorf("upper failed: %v", err)
		}
		if result != "HELLO" {
			t.Errorf("upper returned %v, want 'HELLO'", result)
		}
	})

	t.Run("lower", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "lower",
			"text":      "HELLO",
		})
		if err != nil {
			t.Errorf("lower failed: %v", err)
		}
		if result != "hello" {
			t.Errorf("lower returned %v, want 'hello'", result)
		}
	})

	t.Run("trim", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "trim",
			"text":      "  hello  ",
		})
		if err != nil {
			t.Errorf("trim failed: %v", err)
		}
		if result != "hello" {
			t.Errorf("trim returned %v, want 'hello'", result)
		}
	})

	t.Run("replace", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation":   "replace",
			"text":        "hello world",
			"pattern":     "world",
			"replacement": "go",
		})
		if err != nil {
			t.Errorf("replace failed: %v", err)
		}
		if result != "hello go" {
			t.Errorf("replace returned %v, want 'hello go'", result)
		}
	})

	t.Run("length", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "length",
			"text":      "hello",
		})
		if err != nil {
			t.Errorf("length failed: %v", err)
		}
		if result != 5 {
			t.Errorf("length returned %v, want 5", result)
		}
	})

	t.Run("contains", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "contains",
			"text":      "hello world",
			"pattern":   "world",
		})
		if err != nil {
			t.Errorf("contains failed: %v", err)
		}
		if result != true {
			t.Errorf("contains returned %v, want true", result)
		}
	})

	t.Run("base64 encode", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "encode_base64",
			"text":      "hello",
		})
		if err != nil {
			t.Errorf("encode_base64 failed: %v", err)
		}
		if result != "aGVsbG8=" {
			t.Errorf("encode_base64 returned %v, want 'aGVsbG8='", result)
		}
	})
}

// ============================================================================
// Test Hash Tool
// ============================================================================

func TestHashTool(t *testing.T) {
	tool := NewHashTool()
	ctx := context.Background()

	t.Run("md5", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"algorithm": "md5",
			"text":      "hello",
			"encoding":  "hex",
		})
		if err != nil {
			t.Errorf("md5 failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["hash"] != "5d41402abc4b2a76b9719d911017c592" {
			t.Errorf("md5 hash = %v, want '5d41402abc4b2a76b9719d911017c592'", resultMap["hash"])
		}
	})

	t.Run("sha256", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"algorithm": "sha256",
			"text":      "hello",
		})
		if err != nil {
			t.Errorf("sha256 failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["length"].(int) != 32 {
			t.Errorf("sha256 length = %v, want 32", resultMap["length"])
		}
	})
}

// ============================================================================
// Test UUID Tool
// ============================================================================

func TestUUIDTool(t *testing.T) {
	tool := NewUUIDTool()
	ctx := context.Background()

	t.Run("generate v4", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "generate",
			"version":   4,
		})
		if err != nil {
			t.Errorf("generate v4 failed: %v", err)
		}
		resultMap := result.(map[string]any)
		uuid := resultMap["uuid"].(string)
		if len(uuid) != 36 {
			t.Errorf("UUID length = %d, want 36", len(uuid))
		}
	})

	t.Run("validate valid UUID", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "validate",
			"name":      "550e8400-e29b-41d4-a716-446655440000",
		})
		if err != nil {
			t.Errorf("validate failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["valid"] != true {
			t.Error("valid UUID should return valid=true")
		}
	})

	t.Run("validate invalid UUID", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "validate",
			"name":      "not-a-uuid",
		})
		if err != nil {
			t.Errorf("validate failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["valid"] != false {
			t.Error("invalid UUID should return valid=false")
		}
	})
}

// ============================================================================
// Test Random Tool
// ============================================================================

func TestRandomTool(t *testing.T) {
	tool := NewRandomTool()
	ctx := context.Background()

	t.Run("random number", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "number",
			"min":       1,
			"max":       10,
		})
		if err != nil {
			t.Errorf("random number failed: %v", err)
		}
		resultMap := result.(map[string]any)
		num := resultMap["number"].(int)
		if num < 1 || num > 10 {
			t.Errorf("random number %d out of range 1-10", num)
		}
	})

	t.Run("random string", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "string",
			"length":    10,
			"charset":   "alphanumeric",
		})
		if err != nil {
			t.Errorf("random string failed: %v", err)
		}
		resultMap := result.(map[string]any)
		s := resultMap["string"].(string)
		if len(s) != 10 {
			t.Errorf("random string length = %d, want 10", len(s))
		}
	})

	t.Run("choice", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "choice",
			"choices":   []any{"a", "b", "c"},
		})
		if err != nil {
			t.Errorf("choice failed: %v", err)
		}
		resultMap := result.(map[string]any)
		choice := resultMap["choice"]
		validChoices := map[any]bool{"a": true, "b": true, "c": true}
		if !validChoices[choice] {
			t.Errorf("choice returned invalid value: %v", choice)
		}
	})
}

// ============================================================================
// Test Math Tool
// ============================================================================

func TestMathTool(t *testing.T) {
	tool := NewMathTool()
	ctx := context.Background()

	t.Run("add", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "add",
			"a":         1.5,
			"b":         2.5,
		})
		if err != nil {
			t.Errorf("add failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["result"].(float64) != 4.0 {
			t.Errorf("add result = %v, want 4.0", resultMap["result"])
		}
	})

	t.Run("sqrt", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "sqrt",
			"a":         16,
		})
		if err != nil {
			t.Errorf("sqrt failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["result"].(float64) != 4.0 {
			t.Errorf("sqrt result = %v, want 4.0", resultMap["result"])
		}
	})

	t.Run("sum", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "sum",
			"numbers":   []any{1.0, 2.0, 3.0, 4.0, 5.0},
		})
		if err != nil {
			t.Errorf("sum failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["result"].(float64) != 15.0 {
			t.Errorf("sum result = %v, want 15.0", resultMap["result"])
		}
	})

	t.Run("avg", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "avg",
			"numbers":   []any{1.0, 2.0, 3.0},
		})
		if err != nil {
			t.Errorf("avg failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["result"].(float64) != 2.0 {
			t.Errorf("avg result = %v, want 2.0", resultMap["result"])
		}
	})

	t.Run("constants", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "constants",
		})
		if err != nil {
			t.Errorf("constants failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["pi"] == nil {
			t.Error("constants should include pi")
		}
	})
}

// ============================================================================
// Test Time Tool
// ============================================================================

func TestTimeTool(t *testing.T) {
	tool := NewTimeTool()
	ctx := context.Background()

	t.Run("now", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "now",
		})
		if err != nil {
			t.Errorf("now failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["unix"] == nil {
			t.Error("now should return unix timestamp")
		}
	})

	t.Run("timestamp", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "timestamp",
		})
		if err != nil {
			t.Errorf("timestamp failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["unix"] == nil {
			t.Error("timestamp should return unix")
		}
	})

	t.Run("format", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "format",
			"time_str":  "2024-01-01T12:00:00Z",
			"format":    "2006-01-02",
		})
		if err != nil {
			t.Errorf("format failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["formatted"] == nil {
			t.Error("format should return formatted string")
		}
	})
}

// ============================================================================
// Test Env Tool
// ============================================================================

func TestEnvTool(t *testing.T) {
	tool := NewEnvTool()
	ctx := context.Background()

	t.Run("get PATH", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "get",
			"name":      "PATH",
		})
		if err != nil {
			t.Errorf("get failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["exists"] != true {
			t.Error("PATH should exist")
		}
	})

	t.Run("list", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "list",
		})
		if err != nil {
			t.Errorf("list failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["count"].(int) == 0 {
			t.Error("list should return environment variables")
		}
	})

	t.Run("get_all", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"operation": "get_all",
		})
		if err != nil {
			t.Errorf("get_all failed: %v", err)
		}
		resultMap := result.(map[string]any)
		if resultMap["count"].(int) == 0 {
			t.Error("get_all should return environment variables")
		}
	})
}

// ============================================================================
// Test ToolResult
// ============================================================================

func TestToolResult(t *testing.T) {
	t.Run("NewSuccessResult", func(t *testing.T) {
		result := NewSuccessResult("test data")
		if !result.Success {
			t.Error("NewSuccessResult should create success result")
		}
		if result.Data != "test data" {
			t.Errorf("Data = %v, want 'test data'", result.Data)
		}
	})

	t.Run("NewErrorResult", func(t *testing.T) {
		result := NewErrorResult("error message")
		if result.Success {
			t.Error("NewErrorResult should create error result")
		}
		if result.Error != "error message" {
			t.Errorf("Error = %v, want 'error message'", result.Error)
		}
	})

	t.Run("WithMetadata", func(t *testing.T) {
		result := NewSuccessResult("data").WithMetadata("key", "value")
		if result.Metadata["key"] != "value" {
			t.Errorf("Metadata[key] = %v, want 'value'", result.Metadata["key"])
		}
	})

	t.Run("ToMap", func(t *testing.T) {
		result := NewSuccessResult("data")
		m := result.ToMap()
		if m["success"] != true {
			t.Error("ToMap should include success field")
		}
	})
}

// ============================================================================
// Test ToolCache
// ============================================================================

func TestInMemoryCache(t *testing.T) {
	cache := NewInMemoryCache(100)

	t.Run("set and get", func(t *testing.T) {
		result := NewSuccessResult("test")
		cache.Set("key1", result, time.Hour)
		
		cached, found := cache.Get("key1")
		if !found {
			t.Error("cache.Get should return found=true")
		}
		if cached.Data != "test" {
			t.Errorf("cached.Data = %v, want 'test'", cached.Data)
		}
	})

	t.Run("delete", func(t *testing.T) {
		result := NewSuccessResult("test")
		cache.Set("key2", result, time.Hour)
		cache.Delete("key2")
		
		_, found := cache.Get("key2")
		if found {
			t.Error("cache.Get should return found=false after delete")
		}
	})

	t.Run("expired", func(t *testing.T) {
		result := NewSuccessResult("test")
		cache.Set("key3", result, time.Millisecond)
		
		time.Sleep(time.Millisecond * 10)
		
		_, found := cache.Get("key3")
		if found {
			t.Error("expired key should not be found")
		}
	})

	t.Run("size", func(t *testing.T) {
		cache.Clear()
		cache.Set("k1", NewSuccessResult("v1"), time.Hour)
		cache.Set("k2", NewSuccessResult("v2"), time.Hour)
		
		if cache.Size() != 2 {
			t.Errorf("Size = %d, want 2", cache.Size())
		}
	})
}

// ============================================================================
// Test Middleware
// ============================================================================

func TestLoggingMiddleware(t *testing.T) {
	logger := &defaultLogger{}
	middleware := LoggingMiddleware(logger)
	
	var executed bool
	wrapped := middleware(func(ctx context.Context, params map[string]any) (*ToolResult, error) {
		executed = true
		return NewSuccessResult("test"), nil
	})
	
	_, _ = wrapped(context.Background(), map[string]any{})
	if !executed {
		t.Error("middleware should execute wrapped function")
	}
}

func TestPanicRecoveryMiddleware(t *testing.T) {
	middleware := PanicRecoveryMiddleware()
	
	wrapped := middleware(func(ctx context.Context, params map[string]any) (*ToolResult, error) {
		panic("test panic")
	})
	
	result, err := wrapped(context.Background(), map[string]any{})
	if err != nil {
		t.Errorf("panic should not return error, got: %v", err)
	}
	if result == nil || !result.Success {
		t.Error("panic should return success=false result")
	}
}

// ============================================================================
// Test Context
// ============================================================================

func TestToolContext(t *testing.T) {
	t.Run("NewToolContext", func(t *testing.T) {
		ctx := NewToolContext(context.Background())
		if ctx == nil {
			t.Error("NewToolContext should not return nil")
		}
	})

	t.Run("WithSession", func(t *testing.T) {
		ctx := NewToolContext(context.Background()).WithSession("session123")
		if ctx.SessionID != "session123" {
			t.Errorf("SessionID = %s, want 'session123'", ctx.SessionID)
		}
	})

	t.Run("WithUser", func(t *testing.T) {
		ctx := NewToolContext(context.Background()).WithUser("user123")
		if ctx.UserID != "user123" {
			t.Errorf("UserID = %s, want 'user123'", ctx.UserID)
		}
	})

	t.Run("Metadata", func(t *testing.T) {
		ctx := NewToolContext(context.Background())
		ctx.SetMetadata("key", "value")
		val, ok := ctx.GetMetadata("key")
		if !ok {
			t.Error("GetMetadata should return ok=true")
		}
		if val != "value" {
			t.Errorf("Metadata[key] = %v, want 'value'", val)
		}
	})

	t.Run("Elapsed", func(t *testing.T) {
		ctx := NewToolContext(context.Background())
		time.Sleep(time.Millisecond)
		elapsed := ctx.Elapsed()
		if elapsed < time.Millisecond {
			t.Errorf("Elapsed = %v, should be >= 1ms", elapsed)
		}
	})

	t.Run("ToContext and FromContext", func(t *testing.T) {
		tc := NewToolContext(context.Background()).WithSession("session123")
		ctx := tc.ToContext()
		
		fromCtx := FromContext(ctx)
		if fromCtx.SessionID != "session123" {
			t.Errorf("FromContext SessionID = %s, want 'session123'", fromCtx.SessionID)
		}
	})
}

// ============================================================================
// Test CacheKeyBuilder
// ============================================================================

func TestCacheKeyBuilder(t *testing.T) {
	builder := NewCacheKeyBuilder("test")
	
	t.Run("build consistent key", func(t *testing.T) {
		key1 := builder.Build("tool", map[string]any{"a": "1"})
		key2 := builder.Build("tool", map[string]any{"a": "1"})
		if key1 != key2 {
			t.Error("same inputs should produce same key")
		}
	})

	t.Run("build different keys", func(t *testing.T) {
		key1 := builder.Build("tool", map[string]any{"a": "1"})
		key2 := builder.Build("tool", map[string]any{"a": "2"})
		if key1 == key2 {
			t.Error("different inputs should produce different keys")
		}
	})
}
