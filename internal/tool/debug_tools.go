package tool

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrorAnalysis represents the analysis of an error
type ErrorAnalysis struct {
	ErrorType   string          `json:"error_type"`
	Location    *ErrorLocation  `json:"location,omitempty"`
	Message     string          `json:"message"`
	StackTrace  []StackFrame    `json:"stack_trace,omitempty"`
	Causes      []string        `json:"possible_causes"`
	Suggestions []FixSuggestion `json:"suggestions"`
	Severity    string          `json:"severity"`
	Language    string          `json:"language,omitempty"`
}

// ErrorLocation represents where an error occurred
type ErrorLocation struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
	Function string `json:"function,omitempty"`
}

// StackFrame represents a single frame in a stack trace
type StackFrame struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// FixSuggestion represents a suggested fix
type FixSuggestion struct {
	Description string `json:"description"`
	Code        string `json:"code,omitempty"`
	Line        int    `json:"line,omitempty"`
	Confidence  string `json:"confidence"` // high, medium, low
}

// AnalyzeErrorTool analyzes error messages and stack traces
type AnalyzeErrorTool struct {
	*BaseTool
}

// NewAnalyzeErrorTool creates a new error analysis tool
func NewAnalyzeErrorTool() *AnalyzeErrorTool {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"error_message": map[string]interface{}{
				"type":        "string",
				"description": "The error message or stack trace to analyze",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Programming language (go, python, javascript, rust, etc.)",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Additional context about when the error occurred",
			},
		},
		"required": []string{"error_message"},
	}

	return &AnalyzeErrorTool{
		BaseTool: NewBaseTool(
			"analyze_error",
			"Analyze error messages and stack traces to identify root causes and suggest fixes",
			schema,
		),
	}
}

// Execute runs the error analysis tool
func (t *AnalyzeErrorTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	errorMsg, ok := params["error_message"].(string)
	if !ok || errorMsg == "" {
		return nil, fmt.Errorf("error_message parameter is required")
	}

	language := ""
	if lang, ok := params["language"].(string); ok {
		language = lang
	}

	if language == "" {
		language = detectLanguageFromError(errorMsg)
	}

	analysis := analyzeError(errorMsg, language)
	return analysis, nil
}

func detectLanguageFromError(errorMsg string) string {
	// Go patterns
	if strings.Contains(errorMsg, "goroutine") ||
		strings.Contains(errorMsg, "panic:") ||
		regexp.MustCompile(`\.go:\d+`).MatchString(errorMsg) {
		return "go"
	}

	// Python patterns
	if strings.Contains(errorMsg, "Traceback (most recent call last)") ||
		strings.Contains(errorMsg, "File \"") && strings.Contains(errorMsg, "line") {
		return "python"
	}

	// Rust patterns
	if strings.Contains(errorMsg, "thread 'main' panicked") ||
		strings.Contains(errorMsg, "error[Exxxx]:") {
		return "rust"
	}

	// Java patterns —— 必须在 JavaScript 之前：Java 的堆栈行
	// "at pkg.Class(File.java:42)" 同样包含 "at " 和 "("，宽泛的 JS
	// 条件会把 Java 输入误判成 JavaScript（反之 JS 不会带 ".java:"）。
	if strings.Contains(errorMsg, "Exception in thread") ||
		strings.Contains(errorMsg, ".java:") {
		return "java"
	}

	// JavaScript/Node.js patterns
	if strings.Contains(errorMsg, "at ") && strings.Contains(errorMsg, "(") ||
		strings.Contains(errorMsg, "TypeError:") ||
		strings.Contains(errorMsg, "ReferenceError:") {
		return "javascript"
	}

	return "unknown"
}

func analyzeError(errorMsg, language string) *ErrorAnalysis {
	analysis := &ErrorAnalysis{
		Message:     errorMsg,
		Language:    language,
		Severity:    "medium",
		Causes:      []string{},
		Suggestions: []FixSuggestion{},
	}

	switch language {
	case "go":
		analyzeGoError(analysis, errorMsg)
	case "python":
		analyzePythonError(analysis, errorMsg)
	case "javascript":
		analyzeJavaScriptError(analysis, errorMsg)
	case "rust":
		analyzeRustError(analysis, errorMsg)
	case "java":
		analyzeJavaError(analysis, errorMsg)
	default:
		analyzeGenericError(analysis, errorMsg)
	}

	// 兜底：规则没命中任何已知模式时不能返回空数组——空 causes/suggestions
	// 意味着"没有分析"，对调用方毫无价值。至少给出通用排查方向。
	if len(analysis.Causes) == 0 {
		analysis.Causes = append(analysis.Causes,
			"No known pattern matched; the cause cannot be determined from the message alone",
			"Check the full error text and surrounding log lines for the actual failure point",
		)
	}
	if len(analysis.Suggestions) == 0 {
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Re-run with more context (the failing code section and recent log output), then retry the analysis",
			Confidence:  "low",
		})
	}

	return analysis
}

func analyzeGoError(analysis *ErrorAnalysis, errorMsg string) {
	analysis.ErrorType = "Go Error"

	// Parse panic
	if strings.Contains(errorMsg, "panic:") {
		analysis.ErrorType = "Panic"
		analysis.Severity = "critical"

		// Extract panic message
		panicRegex := regexp.MustCompile(`panic: (.+)`)
		if matches := panicRegex.FindStringSubmatch(errorMsg); len(matches) > 1 {
			panicMsg := matches[1]
			analysis.Message = panicMsg

			if strings.Contains(panicMsg, "nil pointer dereference") {
				analysis.Causes = append(analysis.Causes,
					"Accessing a nil pointer",
					"Using an uninitialized pointer variable",
				)
				analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
					Description: "Add nil check before accessing the pointer",
					Code:        "if ptr != nil {\n    // access ptr\n}",
					Confidence:  "high",
				})
			} else if strings.Contains(panicMsg, "index out of range") {
				analysis.Causes = append(analysis.Causes,
					"Array/slice index exceeds bounds",
					"Empty slice accessed with index",
				)
				analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
					Description: "Check slice length before indexing",
					Code:        "if idx < len(slice) {\n    value := slice[idx]\n}",
					Confidence:  "high",
				})
			} else {
				// panic 文本千差万别（自定义错误、断言失败……），给不出精确
				// 分支时也要有方向，不能让 causes 留空等兜底。
				analysis.Causes = append(analysis.Causes,
					"A runtime condition the code did not handle (see panic message above)",
					"An unchecked error or unexpected state reached a fatal operation",
				)
				analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
					Description: "Locate the panic site in the stack trace and handle the failing condition (or add a recover in the goroutine)",
					Confidence:  "medium",
				})
			}
		}
	}

	// Parse stack trace
	analysis.StackTrace = parseGoStackTrace(errorMsg)
	if len(analysis.StackTrace) > 0 {
		analysis.Location = &ErrorLocation{
			File:     analysis.StackTrace[0].File,
			Line:     analysis.StackTrace[0].Line,
			Function: analysis.StackTrace[0].Function,
		}
	}

	// Common Go errors
	if strings.Contains(errorMsg, "undefined:") {
		analysis.ErrorType = "Undefined Symbol"
		analysis.Causes = append(analysis.Causes,
			"Variable or function not declared",
			"Missing import",
			"Typo in identifier name",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Declare the identifier, fix the typo, or add the missing import",
			Confidence:  "high",
		})
	}

	if strings.Contains(errorMsg, "cannot use") && strings.Contains(errorMsg, "as type") {
		analysis.ErrorType = "Type Mismatch"
		analysis.Causes = append(analysis.Causes,
			"Incorrect type used where another expected",
			"Missing type conversion",
			"Interface implementation mismatch",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Add an explicit conversion (e.g. string(v), int64(n)) or fix the value's type at its source",
			Confidence:  "high",
		})
	}

	// 依赖缺失（go build / go run 最常见的一类，此前无规则覆盖）
	if strings.Contains(errorMsg, "no required module provides package") ||
		strings.Contains(errorMsg, "cannot find package") {
		analysis.ErrorType = "Missing Dependency"
		analysis.Causes = append(analysis.Causes,
			"Imported package not present in go.mod",
			"Wrong import path (typo or moved module)",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Run 'go mod tidy' (or go get <package>) to fetch the missing module, and verify the import path",
			Code:        "go mod tidy",
			Confidence:  "high",
		})
	}

	if strings.Contains(errorMsg, "declared and not used") {
		analysis.ErrorType = "Unused Variable"
		analysis.Causes = append(analysis.Causes,
			"Variable assigned but never read",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Use the variable or replace the declaration with _",
			Confidence:  "high",
		})
	}
}

func parseGoStackTrace(errorMsg string) []StackFrame {
	var frames []StackFrame
	lines := strings.Split(errorMsg, "\n")

	// Pattern: function_name(0x0, 0x0)
	//    /path/to/file.go:42 +0x25
	funcRegex := regexp.MustCompile(`^(\S+)\(`)
	fileRegex := regexp.MustCompile(`^\s+(\S+):(\d+)`)

	for i := 0; i < len(lines); i++ {
		if matches := funcRegex.FindStringSubmatch(lines[i]); len(matches) > 1 {
			function := matches[1]
			if i+1 < len(lines) {
				if fileMatches := fileRegex.FindStringSubmatch(lines[i+1]); len(fileMatches) > 2 {
					file := fileMatches[1]
					line, _ := strconv.Atoi(fileMatches[2])
					frames = append(frames, StackFrame{
						Function: function,
						File:     file,
						Line:     line,
					})
					i++ // Skip next line
				}
			}
		}
	}

	return frames
}

func analyzePythonError(analysis *ErrorAnalysis, errorMsg string) {
	analysis.ErrorType = "Python Exception"

	// Parse exception type
	exceptionRegex := regexp.MustCompile(`(\w+Error):\s*(.+)`)
	if matches := exceptionRegex.FindStringSubmatch(errorMsg); len(matches) > 2 {
		analysis.ErrorType = matches[1]
		analysis.Message = matches[2]
	}

	// Parse traceback
	analysis.StackTrace = parsePythonTraceback(errorMsg)
	if len(analysis.StackTrace) > 0 {
		analysis.Location = &ErrorLocation{
			File:     analysis.StackTrace[0].File,
			Line:     analysis.StackTrace[0].Line,
			Function: analysis.StackTrace[0].Function,
		}
	}

	// Specific error analysis
	switch analysis.ErrorType {
	case "KeyError":
		analysis.Causes = append(analysis.Causes,
			"Accessing a non-existent dictionary key",
			"Key was deleted or never added",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Use dict.get() or check key existence",
			Code:        "value = my_dict.get('key', default_value)\n# or\nif 'key' in my_dict:\n    value = my_dict['key']",
			Confidence:  "high",
		})
	case "IndexError":
		analysis.Causes = append(analysis.Causes,
			"List index out of range",
			"Accessing empty list",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Check list length before indexing",
			Code:        "if idx < len(my_list):\n    value = my_list[idx]",
			Confidence:  "high",
		})
	case "TypeError":
		if strings.Contains(errorMsg, "NoneType") {
			analysis.Causes = append(analysis.Causes,
				"Calling method on None",
				"Function returned None unexpectedly",
			)
			analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
				Description: "Add None check before method call",
				Code:        "if obj is not None:\n    obj.method()",
				Confidence:  "high",
			})
		} else {
			analysis.Causes = append(analysis.Causes,
				"Wrong type used for operation",
				"Missing type conversion",
			)
		}
	case "ZeroDivisionError":
		analysis.Causes = append(analysis.Causes,
			"Division by zero",
			"Modulo by zero",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Check divisor before division",
			Code:        "if divisor != 0:\n    result = dividend / divisor\nelse:\n    result = 0  # or handle appropriately",
			Confidence:  "high",
		})
	case "AttributeError":
		analysis.Causes = append(analysis.Causes,
			"Attribute or method name misspelled",
			"Object is of an unexpected type (e.g. None, or a different class)",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Print/inspect the object's type and available attributes (dir(obj)) at the failure line",
			Confidence:  "medium",
		})
	case "ImportError", "ModuleNotFoundError":
		analysis.Causes = append(analysis.Causes,
			"Module not installed in the current environment",
			"Wrong sys.path / virtualenv in use",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Install the missing module into the active interpreter (python -m pip install <module>) and confirm the right venv is active",
			Code:        "python -m pip install <module>",
			Confidence:  "high",
		})
	case "FileNotFoundError":
		analysis.Causes = append(analysis.Causes,
			"Path does not exist (typo or relative path resolved against an unexpected CWD)",
			"File removed or never created before use",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Verify the path exists (os.path.exists) and use absolute paths where the CWD is uncertain",
			Code:        "import os\nos.path.exists(path)",
			Confidence:  "high",
		})
	case "PermissionError":
		analysis.Causes = append(analysis.Causes,
			"File/dir owned by another user or opened by another process",
			"Trying to write to a read-only location",
		)
	case "ValueError":
		analysis.Causes = append(analysis.Causes,
			"Invalid value passed to a conversion or function (e.g. int('abc'))",
			"Malformed input data",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Validate/normalize the input value before conversion (try/except or pre-check the format)",
			Confidence:  "medium",
		})
	}
}

func parsePythonTraceback(errorMsg string) []StackFrame {
	var frames []StackFrame
	lines := strings.Split(errorMsg, "\n")

	// Pattern: File "/path/to/file.py", line 42, in function_name
	traceRegex := regexp.MustCompile(`File "([^"]+)", line (\d+), in (\S+)`)

	for _, line := range lines {
		if matches := traceRegex.FindStringSubmatch(line); len(matches) > 3 {
			lineNum, _ := strconv.Atoi(matches[2])
			frames = append(frames, StackFrame{
				File:     matches[1],
				Line:     lineNum,
				Function: matches[3],
			})
		}
	}

	// Reverse to get most recent first
	for i, j := 0, len(frames)-1; i < j; i, j = i+1, j-1 {
		frames[i], frames[j] = frames[j], frames[i]
	}

	return frames
}

func analyzeJavaScriptError(analysis *ErrorAnalysis, errorMsg string) {
	analysis.ErrorType = "JavaScript Error"

	// Parse error type
	errorRegex := regexp.MustCompile(`^(\w+Error):\s*(.+)`)
	if matches := errorRegex.FindStringSubmatch(errorMsg); len(matches) > 2 {
		analysis.ErrorType = matches[1]
		analysis.Message = matches[2]
	}

	// Parse stack trace
	analysis.StackTrace = parseJavaScriptStackTrace(errorMsg)
	if len(analysis.StackTrace) > 0 {
		analysis.Location = &ErrorLocation{
			File:     analysis.StackTrace[0].File,
			Line:     analysis.StackTrace[0].Line,
			Function: analysis.StackTrace[0].Function,
		}
	}

	// Specific error analysis
	switch analysis.ErrorType {
	case "TypeError":
		if strings.Contains(errorMsg, "Cannot read property") ||
			strings.Contains(errorMsg, "Cannot read properties") {
			analysis.Causes = append(analysis.Causes,
				"Accessing property on undefined/null",
				"Object not initialized before use",
			)
			analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
				Description: "Use optional chaining or null check",
				Code:        "const value = obj?.property;\n// or\nif (obj) {\n    const value = obj.property;\n}",
				Confidence:  "high",
			})
		} else if strings.Contains(errorMsg, "is not a function") {
			analysis.Causes = append(analysis.Causes,
				"Calling non-function as function",
				"Variable reassigned to non-function",
				"Wrong import shape (default vs named export mismatch)",
			)
			analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
				Description: "Log the value (console.log(typeof fn, fn)) and check the import/export declaration matches how it is called",
				Confidence:  "medium",
			})
		} else if strings.Contains(errorMsg, "Cannot set properties") {
			analysis.Causes = append(analysis.Causes,
				"Assignment to a property of undefined/null",
				"Object not initialized before use",
			)
			analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
				Description: "Initialize the object before assigning (or use optional chaining on the read side)",
				Confidence:  "high",
			})
		}
	case "ReferenceError":
		analysis.Causes = append(analysis.Causes,
			"Variable not declared",
			"Variable used before definition",
			"Typo in variable name",
		)
	case "SyntaxError":
		analysis.Causes = append(analysis.Causes,
			"Invalid JavaScript syntax",
			"Missing bracket or parenthesis",
		)
		analysis.Severity = "high"
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Run the file through a linter/formatter (prettier/eslint) to pinpoint the syntax error",
			Confidence:  "medium",
		})
	}

	// Node.js 系统级错误（errno）：不走 \w+Error 正则，单独识别
	if strings.Contains(errorMsg, "ENOENT") {
		analysis.ErrorType = "File Not Found (ENOENT)"
		analysis.Causes = append(analysis.Causes,
			"Referenced file or module path does not exist",
			"Relative path resolved against an unexpected working directory",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Verify the path exists and prefer absolute paths (path.resolve/__dirname) where CWD is uncertain",
			Confidence:  "high",
		})
	}
	if strings.Contains(errorMsg, "ECONNREFUSED") {
		analysis.ErrorType = "Connection Refused (ECONNREFUSED)"
		analysis.Causes = append(analysis.Causes,
			"Target service not running or wrong port",
			"Firewall/proxy blocking the connection",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Confirm the service is up and listening on the expected host:port (netstat/curl), then retry",
			Confidence:  "high",
		})
	}
}

func parseJavaScriptStackTrace(errorMsg string) []StackFrame {
	var frames []StackFrame
	lines := strings.Split(errorMsg, "\n")

	// Pattern: at functionName (file.js:42:10)
	// or: at file.js:42:10
	traceRegex := regexp.MustCompile(`at\s+(?:(\S+)\s+\()?([^:]+):(\d+):(\d+)\)?`)

	for _, line := range lines {
		if matches := traceRegex.FindStringSubmatch(line); len(matches) > 4 {
			function := matches[1]
			if function == "" {
				function = "<anonymous>"
			}
			lineNum, _ := strconv.Atoi(matches[3])
			frames = append(frames, StackFrame{
				Function: function,
				File:     matches[2],
				Line:     lineNum,
			})
		}
	}

	return frames
}

func analyzeRustError(analysis *ErrorAnalysis, errorMsg string) {
	analysis.ErrorType = "Rust Error"

	if strings.Contains(errorMsg, "panicked") {
		analysis.ErrorType = "Panic"
		analysis.Severity = "critical"
	}

	// Parse error location
	locationRegex := regexp.MustCompile(`-->\s+(\S+):(\d+):(\d+)`)
	if matches := locationRegex.FindStringSubmatch(errorMsg); len(matches) > 3 {
		line, _ := strconv.Atoi(matches[2])
		col, _ := strconv.Atoi(matches[3])
		analysis.Location = &ErrorLocation{
			File:   matches[1],
			Line:   line,
			Column: col,
		}
	}

	// Common Rust errors
	if strings.Contains(errorMsg, "borrow of moved value") {
		analysis.Causes = append(analysis.Causes,
			"Value was moved and can no longer be used",
			"Missing Clone trait implementation",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Clone the value or use references",
			Code:        "let new_val = val.clone();\n// or\nfn func(val: &Type) { }",
			Confidence:  "high",
		})
	}

	if strings.Contains(errorMsg, "cannot borrow") && strings.Contains(errorMsg, "as mutable") {
		analysis.Causes = append(analysis.Causes,
			"Multiple mutable borrows",
			"Borrow checker violation",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Serialize the borrows (end the first &mut before taking the next) or use RefCell/interior mutability",
			Confidence:  "medium",
		})
	}

	if strings.Contains(errorMsg, "cannot find value") || strings.Contains(errorMsg, "unresolved") {
		analysis.ErrorType = "Unresolved Name"
		analysis.Causes = append(analysis.Causes,
			"Identifier not declared or not in scope",
			"Missing use/import of the module or trait",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Add the missing use declaration or fix the identifier spelling",
			Confidence:  "high",
		})
	}

	if strings.Contains(errorMsg, "expected") && strings.Contains(errorMsg, "found") {
		analysis.ErrorType = "Type/Syntax Mismatch"
		analysis.Causes = append(analysis.Causes,
			"Compiler expected a different token or type at this position",
			"Missing bracket, comma, or type annotation",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Look at the --> location and the expected-vs-found tokens; fix the surrounding syntax or add the annotation",
			Confidence:  "medium",
		})
	}
}

func analyzeJavaError(analysis *ErrorAnalysis, errorMsg string) {
	analysis.ErrorType = "Java Exception"

	// Parse exception type
	exceptionRegex := regexp.MustCompile(`(\w+Exception):\s*(.+)`)
	if matches := exceptionRegex.FindStringSubmatch(errorMsg); len(matches) > 2 {
		analysis.ErrorType = matches[1]
		analysis.Message = matches[2]
	}

	// Parse stack trace
	analysis.StackTrace = parseJavaStackTrace(errorMsg)
	if len(analysis.StackTrace) > 0 {
		analysis.Location = &ErrorLocation{
			File:     analysis.StackTrace[0].File,
			Line:     analysis.StackTrace[0].Line,
			Function: analysis.StackTrace[0].Function,
		}
	}

	if strings.Contains(analysis.ErrorType, "NullPointerException") {
		analysis.Causes = append(analysis.Causes,
			"Method called on null object",
			"Object not initialized",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Add null check before method call",
			Code:        "if (obj != null) {\n    obj.method();\n}",
			Confidence:  "high",
		})
	}

	if strings.Contains(analysis.ErrorType, "ArrayIndexOutOfBoundsException") {
		analysis.Causes = append(analysis.Causes,
			"Index beyond array length",
			"Off-by-one in loop bound",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Check the index against array.length before access (or use enhanced for)",
			Confidence:  "high",
		})
	}
	if strings.Contains(analysis.ErrorType, "NumberFormatException") {
		analysis.Causes = append(analysis.Causes,
			"Non-numeric string passed to Integer.parseInt/Long.parseLong etc.",
			"Input contains whitespace, sign, or separators not accepted by the parser",
		)
	}
	if strings.Contains(analysis.ErrorType, "ClassCastException") {
		analysis.Causes = append(analysis.Causes,
			"Object's runtime type is not compatible with the target cast",
		)
		analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
			Description: "Check getClass()/instanceof before casting",
			Confidence:  "high",
		})
	}
}

func parseJavaStackTrace(errorMsg string) []StackFrame {
	var frames []StackFrame
	lines := strings.Split(errorMsg, "\n")

	// Pattern: at package.Class.method(File.java:42)
	traceRegex := regexp.MustCompile(`at\s+([\w.]+)\.([\w<>]+)\(([^)]+)\)`)

	for _, line := range lines {
		if matches := traceRegex.FindStringSubmatch(line); len(matches) > 3 {
			function := matches[1] + "." + matches[2]
			location := matches[3]

			// Parse file:line
			fileLineRegex := regexp.MustCompile(`(\w+\.\w+):(\d+)`)
			if flMatches := fileLineRegex.FindStringSubmatch(location); len(flMatches) > 2 {
				lineNum, _ := strconv.Atoi(flMatches[2])
				frames = append(frames, StackFrame{
					Function: function,
					File:     flMatches[1],
					Line:     lineNum,
				})
			}
		}
	}

	return frames
}

func analyzeGenericError(analysis *ErrorAnalysis, errorMsg string) {
	analysis.ErrorType = "Unknown Error"
	analysis.Causes = append(analysis.Causes,
		"Unable to determine specific cause",
		"Review error message for details",
	)
	analysis.Suggestions = append(analysis.Suggestions, FixSuggestion{
		Description: "Check logs and reproduce the issue",
		Confidence:  "low",
	})
}
