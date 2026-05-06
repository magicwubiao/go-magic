package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestOutputFormatterJSON(t *testing.T) {
	data := map[string]interface{}{
		"name": "test",
		"value": 123,
	}

	formatter := NewOutputFormatter("json", true)
	buf := new(bytes.Buffer)
	formatter.SetWriter(buf)

	err := formatter.Print(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid JSON
	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	if err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "test" {
		t.Errorf("expected name 'test', got '%v'", result["name"])
	}
}

func TestOutputFormatterText(t *testing.T) {
	data := "hello world"

	formatter := NewOutputFormatter("text", true)
	buf := new(bytes.Buffer)
	formatter.SetWriter(buf)

	err := formatter.Print(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.String() != "hello world\n" {
		t.Errorf("expected 'hello world\\n', got '%s'", buf.String())
	}
}

func TestOutputFormatterSlice(t *testing.T) {
	data := []string{"a", "b", "c"}

	formatter := NewOutputFormatter("text", true)
	buf := new(bytes.Buffer)
	formatter.SetWriter(buf)

	err := formatter.Print(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "a\nb\nc\n"
	if buf.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, buf.String())
	}
}

func TestColorFunctions(t *testing.T) {
	// Test with no-color mode
	flagNoColor = true

	tests := []struct {
		name string
		fn   func(string) string
	}{
		{"Red", Red},
		{"Green", Green},
		{"Yellow", Yellow},
		{"Blue", Blue},
		{"Purple", Purple},
		{"Cyan", Cyan},
		{"Gray", Gray},
		{"Bold", Bold},
		{"Dim", Dim},
		{"Underline", Underline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn("test")
			if result != "test" {
				t.Errorf("expected 'test', got '%s'", result)
			}
		})
	}

	// Reset
	flagNoColor = false
}

func TestColorFunctionsWithColor(t *testing.T) {
	// Test with color mode
	flagNoColor = false

	tests := []struct {
		name   string
		fn     func(string) string
		expect string
	}{
		{"Red", Red, "\033[31mtest\033[0m"},
		{"Green", Green, "\033[32mtest\033[0m"},
		{"Bold", Bold, "\033[1mtest\033[0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn("test")
			if result != tt.expect {
				t.Errorf("expected '%s', got '%s'", tt.expect, result)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    uint64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.input)
		if result != tt.expected {
			t.Errorf("formatBytes(%d): expected '%s', got '%s'", tt.input, tt.expected, result)
		}
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{nil, ""},
		{"test", "test"},
		{123, "123"},
		{[]byte("test"), "test"},
	}

	for _, tt := range tests {
		result := formatValue(tt.input)
		if result != tt.expected {
			t.Errorf("formatValue(%v): expected '%s', got '%s'", tt.input, tt.expected, result)
		}
	}
}

func TestNewTable(t *testing.T) {
	table := NewTable("Name", "Age", "City")

	table.AddRow("Alice", "30", "NYC")
	table.AddRow("Bob", "25", "LA")

	if len(table.Headers) != 3 {
		t.Errorf("expected 3 headers, got %d", len(table.Headers))
	}

	if len(table.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(table.Rows))
	}
}

func TestTableAddRowPadding(t *testing.T) {
	table := NewTable("A", "B", "C")

	// Add row with too few values
	table.AddRow("x")

	if len(table.Rows[0]) != 3 {
		t.Errorf("expected 3 columns after padding, got %d", len(table.Rows[0]))
	}
}

func TestPrintSuccess(t *testing.T) {
	flagNoColor = true

	buf := new(bytes.Buffer)
	old := osStdout
	osStdout = buf

	PrintSuccess("test message")

	osStdout = old

	expected := "✓ test message\n"
	if buf.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, buf.String())
	}
}

func TestPrintError(t *testing.T) {
	flagNoColor = true

	buf := new(bytes.Buffer)
	old := osStdout
	osStdout = buf

	PrintError("error message")

	osStdout = old

	expected := "✗ error message\n"
	if buf.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, buf.String())
	}
}

func TestPrintWarning(t *testing.T) {
	flagNoColor = true

	buf := new(bytes.Buffer)
	old := osStdout
	osStdout = buf

	PrintWarning("warning message")

	osStdout = old

	expected := "⚠ warning message\n"
	if buf.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, buf.String())
	}
}

func TestSortKeys(t *testing.T) {
	keys := []string{"zebra", "apple", "banana"}
	SortKeys(keys)

	expected := []string{"apple", "banana", "zebra"}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("expected keys[%d]='%s', got '%s'", i, expected[i], k)
		}
	}
}

// Helper to capture stdout
var osStdout interface{} = os.Stdout

func init() {
	// We need to redirect os.Stdout for some tests
	// This is a simplified version
}
