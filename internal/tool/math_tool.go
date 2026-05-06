package tool

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ============================================================================
// Math Tool - 数学计算工具
// ============================================================================

type MathTool struct {
	BaseTool
}

func NewMathTool() *MathTool {
	return &MathTool{
		BaseTool: *NewBaseTool(
			"math",
			"Mathematical operations: basic arithmetic, statistics, trigonometry, constants",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: add, subtract, multiply, divide, power, sqrt, abs, round, floor, ceil, sin, cos, tan, log, log10, exp, min, max, sum, avg, median, stddev",
					},
					"a": map[string]any{
						"type":        "number",
						"description": "First operand",
					},
					"b": map[string]any{
						"type":        "number",
						"description": "Second operand",
					},
					"numbers": map[string]any{
						"type":        "array",
						"description": "Array of numbers for aggregate operations",
					},
					"precision": map[string]any{
						"type":        "number",
						"description": "Decimal precision for rounding (default: 2)",
						"default":     2,
					},
				},
				"required": []any{"operation"},
			},
		),
	}
}

func (t *MathTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)

	switch operation {
	case "add":
		return t.binaryOp(args, func(a, b float64) float64 { return a + b })
	case "subtract":
		return t.binaryOp(args, func(a, b float64) float64 { return a - b })
	case "multiply":
		return t.binaryOp(args, func(a, b float64) float64 { return a * b })
	case "divide":
		return t.binaryOp(args, func(a, b float64) float64 {
			if b == 0 {
				return math.Inf(1)
			}
			return a / b
		})
	case "power":
		return t.binaryOp(args, math.Pow)
	case "sqrt":
		return t.unaryOp(args, math.Sqrt)
	case "abs":
		return t.unaryOp(args, math.Abs)
	case "round":
		return t.round(args)
	case "floor":
		return t.unaryOp(args, math.Floor)
	case "ceil":
		return t.unaryOp(args, math.Ceil)
	case "sin":
		return t.unaryOp(args, math.Sin)
	case "cos":
		return t.unaryOp(args, math.Cos)
	case "tan":
		return t.unaryOp(args, math.Tan)
	case "log":
		return t.unaryOp(args, math.Log)
	case "log10":
		return t.unaryOp(args, math.Log10)
	case "exp":
		return t.unaryOp(args, math.Exp)
	case "min":
		return t.aggregateOp(args, func(nums []float64) float64 {
			return math.Inf(1)
		}, math.Min)
	case "max":
		return t.aggregateOp(args, func(nums []float64) float64 {
			return math.Inf(-1)
		}, math.Max)
	case "sum":
		return t.sum(args)
	case "avg":
		return t.avg(args)
	case "median":
		return t.median(args)
	case "stddev":
		return t.stddev(args)
	case "constants":
		return t.constants()
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *MathTool) unaryOp(args map[string]any, op func(float64) float64) (map[string]any, error) {
	a, ok := toFloat64(args["a"])
	if !ok {
		return nil, fmt.Errorf("operand 'a' is required")
	}
	return map[string]any{"result": op(a)}, nil
}

func (t *MathTool) binaryOp(args map[string]any, op func(float64, float64) float64) (map[string]any, error) {
	a, ok := toFloat64(args["a"])
	if !ok {
		return nil, fmt.Errorf("operand 'a' is required")
	}
	b, ok := toFloat64(args["b"])
	if !ok {
		return nil, fmt.Errorf("operand 'b' is required")
	}
	return map[string]any{"result": op(a, b)}, nil
}

func (t *MathTool) round(args map[string]any) (map[string]any, error) {
	a, ok := toFloat64(args["a"])
	if !ok {
		return nil, fmt.Errorf("operand 'a' is required")
	}
	precision := 2
	if p, ok := toFloat64(args["precision"]); ok {
		precision = int(p)
	}
	multiplier := math.Pow(10, float64(precision))
	return map[string]any{"result": math.Round(a*multiplier) / multiplier}, nil
}

func (t *MathTool) sum(args map[string]any) (map[string]any, error) {
	nums, ok := toFloat64Array(args["numbers"])
	if !ok || len(nums) == 0 {
		return nil, fmt.Errorf("numbers array is required for sum")
	}
	var sum float64
	for _, n := range nums {
		sum += n
	}
	return map[string]any{"result": sum, "count": len(nums)}, nil
}

func (t *MathTool) avg(args map[string]any) (map[string]any, error) {
	nums, ok := toFloat64Array(args["numbers"])
	if !ok || len(nums) == 0 {
		return nil, fmt.Errorf("numbers array is required for avg")
	}
	var sum float64
	for _, n := range nums {
		sum += n
	}
	return map[string]any{"result": sum / float64(len(nums)), "count": len(nums)}, nil
}

func (t *MathTool) median(args map[string]any) (map[string]any, error) {
	nums, ok := toFloat64Array(args["numbers"])
	if !ok || len(nums) == 0 {
		return nil, fmt.Errorf("numbers array is required for median")
	}
	
	// Sort copy
	sorted := make([]float64, len(nums))
	copy(sorted, nums)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	
	var median float64
	n := len(sorted)
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	} else {
		median = sorted[n/2]
	}
	
	return map[string]any{"result": median, "count": n}, nil
}

func (t *MathTool) stddev(args map[string]any) (map[string]any, error) {
	nums, ok := toFloat64Array(args["numbers"])
	if !ok || len(nums) == 0 {
		return nil, fmt.Errorf("numbers array is required for stddev")
	}
	
	var sum, sumSq float64
	for _, n := range nums {
		sum += n
		sumSq += n * n
	}
	n := float64(len(nums))
	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	
	return map[string]any{
		"result":   math.Sqrt(variance),
		"variance": variance,
		"mean":     mean,
		"count":    len(nums),
	}, nil
}

func (t *MathTool) aggregateOp(args map[string]any, initFn func([]float64) float64, opFn func(float64, float64) float64) (map[string]any, error) {
	nums, ok := toFloat64Array(args["numbers"])
	if !ok || len(nums) == 0 {
		return nil, fmt.Errorf("numbers array is required")
	}
	
	result := initFn(nums)
	for _, n := range nums {
		result = opFn(result, n)
	}
	
	return map[string]any{"result": result, "count": len(nums)}, nil
}

func (t *MathTool) constants() (map[string]any, error) {
	m := map[string]any{
		"pi":       math.Pi,
		"e":        math.E,
		"phi":      math.Phi,
		"sqrt2":    math.Sqrt2,
		"sqrt_e":   math.SqrtE,
		"ln2":      math.Ln2,
		"ln10":     math.Ln10,
		"log2_e":   math.Log2E,
		"log10_e":  math.Log10E,
		"inf":      math.Inf(1),
		"neg_inf":  math.Inf(-1),
		"nan":      math.NaN(),
	}
	return m, nil
}
// CSV Tool - CSV 处理工具
// ============================================================================

type CSVTool struct {
	BaseTool
}

func NewCSVTool() *CSVTool {
	return &CSVTool{
		BaseTool: *NewBaseTool(
			"csv",
			"Process CSV data: parse, format, filter, transform, query",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: parse, format, filter, transform, stats",
					},
					"data": map[string]any{
						"type":        "string",
						"description": "CSV string to process",
					},
					"delimiter": map[string]any{
						"type":        "string",
						"description": "CSV delimiter (default: comma)",
						"default":     ",",
					},
					"has_header": map[string]any{
						"type":        "boolean",
						"description": "Whether CSV has header row",
						"default":     true,
					},
					"column": map[string]any{
						"type":        "string",
						"description": "Column name for filter/transform operations",
					},
					"value": map[string]any{
						"type":        "string",
						"description": "Value to filter by",
					},
					"operator": map[string]any{
						"type":        "string",
						"description": "Comparison operator: eq, ne, gt, lt, ge, le, contains",
						"default":     "eq",
					},
				},
				"required": []any{"operation", "data"},
			},
		),
	}
}

func (t *CSVTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)
	data, _ := args["data"].(string)

	if data == "" {
		return nil, fmt.Errorf("data is required")
	}

	delimiter := ","
	if d, ok := args["delimiter"].(string); ok {
		delimiter = d
	}

	hasHeader := true
	if h, ok := args["has_header"].(bool); ok {
		hasHeader = h
	}

	switch operation {
	case "parse":
		return t.parse(data, delimiter, hasHeader)
	case "format":
		return t.format(data, delimiter)
	case "filter":
		return t.filter(data, delimiter, hasHeader, args)
	case "stats":
		return t.stats(data, delimiter, hasHeader)
	case "transform":
		return t.transform(data, delimiter, hasHeader, args)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *CSVTool) parse(data, delimiter string, hasHeader bool) (map[string]any, error) {
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = rune(delimiter[0])

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("empty CSV data")
	}

	var headers []string
	var rows []map[string]string

	if hasHeader {
		headers = records[0]
		for i, record := range records[1:] {
			row := make(map[string]string)
			for j, value := range record {
				if j < len(headers) {
					row[headers[j]] = value
				}
			}
			row["_row"] = fmt.Sprintf("%d", i+1)
			rows = append(rows, row)
		}
	} else {
		for i, record := range records {
			row := make(map[string]string)
			for j, value := range record {
				row[fmt.Sprintf("col%d", j)] = value
			}
			row["_row"] = fmt.Sprintf("%d", i+1)
			rows = append(rows, row)
		}
	}

	return map[string]any{
		"headers": headers,
		"rows":    rows,
		"count":   len(rows),
	}, nil
}

func (t *CSVTool) format(data, delimiter string) (string, error) {
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = rune(delimiter[0])

	records, err := reader.ReadAll()
	if err != nil {
		return "", fmt.Errorf("failed to parse CSV: %w", err)
	}

	var buf strings.Builder
	writer := csv.NewWriter(&buf)
	writer.Comma = rune(delimiter[0])
	writer.WriteAll(records)

	return buf.String(), nil
}

func (t *CSVTool) filter(data, delimiter string, hasHeader bool, args map[string]any) (map[string]any, error) {
	parsed, err := t.parse(data, delimiter, hasHeader)
	if err != nil {
		return nil, err
	}

	column, _ := args["column"].(string)
	value, _ := args["value"].(string)
	operator, _ := args["operator"].(string)
	if operator == "" {
		operator = "eq"
	}

	rows := parsed["rows"].([]map[string]string)
	var filtered []map[string]string

	for _, row := range rows {
		if t.matchRow(row, column, value, operator) {
			filtered = append(filtered, row)
		}
	}

	return map[string]any{
		"rows":  filtered,
		"count": len(filtered),
	}, nil
}

func (t *CSVTool) matchRow(row map[string]string, column, value, operator string) bool {
	rowValue, exists := row[column]
	if !exists {
		return false
	}

	switch operator {
	case "eq":
		return rowValue == value
	case "ne":
		return rowValue != value
	case "contains":
		return strings.Contains(rowValue, value)
	case "gt":
		return compareNumeric(rowValue, value) > 0
	case "lt":
		return compareNumeric(rowValue, value) < 0
	case "ge":
		return compareNumeric(rowValue, value) >= 0
	case "le":
		return compareNumeric(rowValue, value) <= 0
	default:
		return rowValue == value
	}
}

func compareNumeric(a, b string) int {
	af, err1 := strconv.ParseFloat(a, 64)
	bf, err2 := strconv.ParseFloat(b, 64)
	if err1 != nil || err2 != nil {
		return strings.Compare(a, b)
	}
	if af > bf {
		return 1
	} else if af < bf {
		return -1
	}
	return 0
}

func (t *CSVTool) stats(data, delimiter string, hasHeader bool) (map[string]any, error) {
	parsed, err := t.parse(data, delimiter, hasHeader)
	if err != nil {
		return nil, err
	}

	rows := parsed["rows"].([]map[string]string)
	stats := make(map[string]map[string]any)

	// Collect column values
	for _, row := range rows {
		for col, value := range row {
			if col == "_row" {
				continue
			}
			if _, exists := stats[col]; !exists {
				stats[col] = map[string]any{
					"count":    0,
					"non_empty": 0,
				}
			}
			stats[col]["count"] = stats[col]["count"].(int) + 1
			if value != "" {
				stats[col]["non_empty"] = stats[col]["non_empty"].(int) + 1
			}
		}
	}

	return map[string]any{
		"total_rows":  len(rows),
		"columns":     stats,
	}, nil
}

func (t *CSVTool) transform(data, delimiter string, hasHeader bool, args map[string]any) (map[string]any, error) {
	// Simple transformation - add/rename columns
	column := args["column"].(string)
	value := args["value"].(string)

	parsed, err := t.parse(data, delimiter, hasHeader)
	if err != nil {
		return nil, err
	}

	rows := parsed["rows"].([]map[string]string)
	var transformed []map[string]string

	for _, row := range rows {
		newRow := make(map[string]string)
		for k, v := range row {
			newRow[k] = v
		}
		// Add computed column
		if column != "" && value != "" {
			newRow[column] = value
		}
		transformed = append(transformed, newRow)
	}

	return map[string]any{
		"rows":  transformed,
		"count": len(transformed),
	}, nil
}

// ============================================================================
// Helper functions
// ============================================================================

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toFloat64Array(v any) ([]float64, bool) {
	switch arr := v.(type) {
	case []any:
		result := make([]float64, 0, len(arr))
		for _, item := range arr {
			if f, ok := toFloat64(item); ok {
				result = append(result, f)
			}
		}
		return result, len(result) > 0
	case []float64:
		return arr, true
	case []int:
		result := make([]float64, len(arr))
		for i, n := range arr {
			result[i] = float64(n)
		}
		return result, true
	default:
		return nil, false
	}
}
