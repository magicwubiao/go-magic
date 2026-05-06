package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/olekukonko/tablewriter"
	"gopkg.in/yaml.v3"
)

// OutputFormatter handles different output formats
type OutputFormatter struct {
	writer io.Writer
	format string
	noColor bool
}

// NewOutputFormatter creates a new output formatter
func NewOutputFormatter(format string, noColor bool) *OutputFormatter {
	return &OutputFormatter{
		writer:  os.Stdout,
		format:  format,
		noColor: noColor,
	}
}

// SetWriter sets the output writer
func (f *OutputFormatter) SetWriter(w io.Writer) {
	f.writer = w
}

// Print prints data in the configured format
func (f *OutputFormatter) Print(data interface{}) error {
	switch f.format {
	case "json":
		return f.printJSON(data)
	case "yaml":
		return f.printYAML(data)
	case "table":
		return f.printTable(data)
	default:
		return f.printText(data)
	}
}

func (f *OutputFormatter) printJSON(data interface{}) error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func (f *OutputFormatter) printYAML(data interface{}) error {
	encoder := yaml.NewEncoder(f.writer)
	return encoder.Encode(data)
}

func (f *OutputFormatter) printText(data interface{}) error {
	switch v := data.(type) {
	case string:
		_, err := fmt.Fprintln(f.writer, v)
		return err
	case []string:
		for _, s := range v {
			if _, err := fmt.Fprintln(f.writer, s); err != nil {
				return err
			}
		}
		return nil
	case map[string]interface{}:
		return f.printMap(v)
	default:
		_, err := fmt.Fprintln(f.writer, v)
		return err
	}
}

func (f *OutputFormatter) printMap(m map[string]interface{}) error {
	// Sort keys for consistent output
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	SortKeys(keys)

	for _, k := range keys {
		fmt.Fprintf(f.writer, "%s: %v\n", k, m[k])
	}
	return nil
}

func (f *OutputFormatter) printTable(data interface{}) error {
	switch v := data.(type) {
	case []map[string]interface{}:
		return f.printMapSlice(v)
	case [][]string:
		return f.printStringSlice(v)
	case []string:
		return f.printStringSlice([][]string{v})
	default:
		return f.printText(data)
	}
}

func (f *OutputFormatter) printMapSlice(data []map[string]interface{}) error {
	if len(data) == 0 {
		fmt.Fprintln(f.writer, "(no data)")
		return nil
	}

	// Get all keys
	keys := make(map[string]bool)
	for _, row := range data {
		for k := range row {
			keys[k] = true
		}
	}

	var headers []string
	for k := range keys {
		headers = append(headers, k)
	}
	SortKeys(headers)

	// Create table
	table := tablewriter.CreateWriter(f.writer)
	table.SetHeader(headers)
	table.SetBorder(true)
	table.SetRowLine(true)
	table.SetAlignment(tablewriter.ALIGN_LEFT)

	// Add rows
	for _, row := range data {
		var line []string
		for _, h := range headers {
			line = append(line, formatValue(row[h]))
		}
		table.Append(line)
	}

	table.Render()
	return nil
}

func (f *OutputFormatter) printStringSlice(data [][]string) error {
	table := tablewriter.CreateWriter(f.writer)
	for _, row := range data {
		table.Append(row)
	}
	table.Render()
	return nil
}

// TableOutput provides a simple table output helper
type TableOutput struct {
	Headers []string
	Rows    [][]string
}

// NewTable creates a new table output
func NewTable(headers ...string) *TableOutput {
	return &TableOutput{
		Headers: headers,
		Rows:    make([][]string, 0),
	}
}

// AddRow adds a row to the table
func (t *TableOutput) AddRow(values ...string) {
	if len(values) != len(t.Headers) {
		// Pad or truncate to match headers
		row := make([]string, len(t.Headers))
		copy(row, values)
		for i := len(values); i < len(t.Headers); i++ {
			row[i] = ""
		}
		t.Rows = append(t.Rows, row)
	} else {
		t.Rows = append(t.Rows, values)
	}
}

// Render renders the table to the output
func (t *TableOutput) Render(format string) error {
	formatter := NewOutputFormatter(format, flagNoColor)
	return formatter.printTable(t.Rows)
}

// Color utilities
var (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"

	bold      = "\033[1m"
	dim       = "\033[2m"
	underline = "\033[4m"
)

// Color functions
func Red(s string) string {
	if flagNoColor {
		return s
	}
	return colorRed + s + colorReset
}

func Green(s string) string {
	if flagNoColor {
		return s
	}
	return colorGreen + s + colorReset
}

func Yellow(s string) string {
	if flagNoColor {
		return s
	}
	return colorYellow + s + colorReset
}

func Blue(s string) string {
	if flagNoColor {
		return s
	}
	return colorBlue + s + colorReset
}

func Purple(s string) string {
	if flagNoColor {
		return s
	}
	return colorPurple + s + colorReset
}

func Cyan(s string) string {
	if flagNoColor {
		return s
	}
	return colorCyan + s + colorReset
}

func Gray(s string) string {
	if flagNoColor {
		return s
	}
	return colorGray + s + colorReset
}

func Bold(s string) string {
	if flagNoColor {
		return s
	}
	return bold + s + colorReset
}

func Dim(s string) string {
	if flagNoColor {
		return s
	}
	return dim + s + colorReset
}

func Underline(s string) string {
	if flagNoColor {
		return s
	}
	return underline + s + colorReset
}

// Spinner provides a simple loading indicator
type Spinner struct {
	message  string
	frames   []string
	current  int
	stopped  bool
}

var defaultFrames = []string{
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
}

// NewSpinner creates a new spinner
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message: message,
		frames:  defaultFrames,
	}
}

// Start starts the spinner (non-blocking, just returns)
func (s *Spinner) Start() {
	s.current = 0
	s.stopped = false
}

// Stop stops the spinner
func (s *Spinner) Stop() {
	s.stopped = true
}

// Current returns the current frame
func (s *Spinner) Current() string {
	if s.stopped {
		return ""
	}
	frame := s.frames[s.current%len(s.frames)]
	s.current++
	return fmt.Sprintf("%s %s", frame, s.message)
}

// Progress provides a simple progress bar
type Progress struct {
	total   int
	current int
	width   int
	prefix  string
}

// NewProgress creates a new progress bar
func NewProgress(total int, prefix string) *Progress {
	return &Progress{
		total:  total,
		current: 0,
		width:  50,
		prefix: prefix,
	}
}

// Increment increments the progress
func (p *Progress) Increment() {
	p.current++
}

// Set sets the current progress
func (p *Progress) Set(current int) {
	p.current = current
}

// Render renders the progress bar
func (p *Progress) Render() string {
	if p.total <= 0 {
		return ""
	}

	percent := float64(p.current) / float64(p.total)
	filled := int(float64(p.width) * percent)
	empty := p.width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return fmt.Sprintf("\r%s [%s] %d%%", p.prefix, bar, int(percent*100))
}

// Helper functions
func formatValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.RFC3339)
	case time.Duration:
		return val.String()
	case []byte:
		return string(val)
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// SortKeys sorts a slice of strings in place
func SortKeys(keys []string) {
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if strings.Compare(keys[i], keys[j]) > 0 {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
}

// Prompt provides interactive prompts
func Prompt(message string, defaultValue string) (string, error) {
	if defaultValue != "" {
		message = fmt.Sprintf("%s [%s]", message, defaultValue)
	}
	message = message + ": "

	fmt.Print(message)

	var input string
	_, err := fmt.Scanln(&input)
	if err != nil {
		// User just pressed enter with no input
		if defaultValue != "" {
			return defaultValue, nil
		}
		return "", err
	}

	if input == "" && defaultValue != "" {
		return defaultValue, nil
	}

	return input, nil
}

// Confirm asks for confirmation
func Confirm(message string) bool {
	for {
		fmt.Printf("%s [y/n]: ", message)
		var input string
		fmt.Scanln(&input)

		switch strings.ToLower(input) {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Println("Please enter 'y' or 'n'")
		}
	}
}

// Select provides a selection prompt
func Select(message string, options []string) (int, error) {
	fmt.Println(message)
	fmt.Println()

	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt)
	}
	fmt.Println()

	for {
		fmt.Print("Enter selection: ")
		var input int
		_, err := fmt.Scanln(&input)
		if err != nil {
			continue
		}

		if input >= 1 && input <= len(options) {
			return input - 1, nil
		}

		fmt.Printf("Please enter a number between 1 and %d\n", len(options))
	}
}

// PrintHeader prints a section header
func PrintHeader(title string) {
	if flagNoColor {
		fmt.Printf("\n=== %s ===\n\n", title)
	} else {
		fmt.Printf("\n%s=== %s ===%s\n\n", Bold(Cyan("")), title, colorReset)
	}
}

// PrintSuccess prints a success message
func PrintSuccess(message string) {
	if flagNoColor {
		fmt.Printf("✓ %s\n", message)
	} else {
		fmt.Printf("%s✓%s %s\n", Green(""), colorReset, message)
	}
}

// PrintError prints an error message
func PrintError(message string) {
	if flagNoColor {
		fmt.Printf("✗ %s\n", message)
	} else {
		fmt.Printf("%s✗%s %s\n", Red(""), colorReset, message)
	}
}

// PrintWarning prints a warning message
func PrintWarning(message string) {
	if flagNoColor {
		fmt.Printf("⚠ %s\n", message)
	} else {
		fmt.Printf("%s⚠%s %s\n", Yellow(""), colorReset, message)
	}
}

// PrintInfo prints an info message
func PrintInfo(message string) {
	if flagNoColor {
		fmt.Printf("ℹ %s\n", message)
	} else {
		fmt.Printf("%sℹ%s %s\n", Blue(""), colorReset, message)
	}
}

// KVOutput prints key-value pairs in a formatted way
func KVOutput(format string, data map[string]interface{}) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Sort keys
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	SortKeys(keys)

	for _, k := range keys {
		key := k
		value := formatValue(data[k])

		switch format {
		case "json", "yaml":
			fmt.Fprintf(w, "%s:\t%s\n", key, value)
		default:
			if flagNoColor {
				fmt.Fprintf(w, "%s: %s\n", key, value)
			} else {
				fmt.Fprintf(w, "%s%s:%s %s\n", Cyan(""), Bold(""), colorReset, value)
			}
		}
	}
}
