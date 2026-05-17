package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents log level
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String returns the string representation of log level
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Color returns the ANSI color code for the level
func (l Level) Color() string {
	switch l {
	case LevelDebug:
		return "\033[36m" // Cyan
	case LevelInfo:
		return "\033[32m" // Green
	case LevelWarn:
		return "\033[33m" // Yellow
	case LevelError:
		return "\033[31m" // Red
	case LevelFatal:
		return "\033[35m" // Magenta
	default:
		return "\033[0m"
	}
}

const reset = "\033[0m"

// Logger is the application logger
type Logger struct {
	mu       sync.Mutex
	level    Level
	output   io.Writer
	prefix   string
	timeFmt  string
	colorful bool
}

// Options configures the logger
type Options struct {
	Level    Level
	Output   io.Writer
	Prefix   string
	TimeFmt  string
	Colorful bool
}

// DefaultOptions returns default logger options
func DefaultOptions() *Options {
	return &Options{
		Level:    LevelInfo,
		Output:   os.Stdout,
		Prefix:   "",
		TimeFmt:  "2006-01-02 15:04:05",
		Colorful: true,
	}
}

// New creates a new logger
func New(opts *Options) *Logger {
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.TimeFmt == "" {
		opts.TimeFmt = "2006-01-02 15:04:05"
	}

	return &Logger{
		level:    opts.Level,
		output:   opts.Output,
		prefix:   opts.Prefix,
		timeFmt:  opts.TimeFmt,
		colorful: opts.Colorful,
	}
}

// Default returns the default logger
func Default() *Logger {
	return New(DefaultOptions())
}

// SetLevel sets the log level
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput sets the output writer
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
}

// SetPrefix sets the log prefix
func (l *Logger) SetPrefix(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prefix = prefix
}

// SetColorful enables or disables colored output
func (l *Logger) SetColorful(colorful bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.colorful = colorful
}

// log logs a message at the specified level
func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Build the message
	var sb strings.Builder

	// Timestamp
	sb.WriteString(time.Now().Format(l.timeFmt))
	sb.WriteString(" ")

	// Level with color
	if l.colorful {
		sb.WriteString(level.Color())
	}
	sb.WriteString("[")
	sb.WriteString(level.String())
	sb.WriteString("]")
	if l.colorful {
		sb.WriteString(reset)
	}
	sb.WriteString(" ")

	// Caller info
	if pc, file, line, ok := runtime.Caller(2); ok {
		fn := runtime.FuncForPC(pc)
		if fn != nil {
			funcName := fn.Name()
			// Get just the function name without package
			if idx := strings.LastIndex(funcName, "."); idx >= 0 {
				funcName = funcName[idx+1:]
			}
			sb.WriteString(fmt.Sprintf("%s:%d %s: ", file, line, funcName))
		} else {
			sb.WriteString(fmt.Sprintf("%s:%d: ", file, line))
		}
	}

	// Prefix
	if l.prefix != "" {
		sb.WriteString(l.prefix)
		sb.WriteString(" ")
	}

	// Message
	sb.WriteString(fmt.Sprintf(format, args...))
	sb.WriteString("\n")

	// Write to output
	l.output.Write([]byte(sb.String()))

	// Handle fatal level
	if level == LevelFatal {
		os.Exit(1)
	}
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(LevelFatal, format, args...)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Fatalf logs a formatted fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.log(LevelFatal, format, args...)
}

// Package-level logger instance
var defaultLogger = New(DefaultOptions())

// SetLevel sets the default logger level
func SetLevel(level Level) {
	defaultLogger.SetLevel(level)
}

// SetOutput sets the default logger output
func SetOutput(w io.Writer) {
	defaultLogger.SetOutput(w)
}

// SetPrefix sets the default logger prefix
func SetPrefix(prefix string) {
	defaultLogger.SetPrefix(prefix)
}

// SetColorful enables or disables colored output
func SetColorful(colorful bool) {
	defaultLogger.SetColorful(colorful)
}

// Debug logs a debug message to the default logger
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

// Info logs an info message to the default logger
func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

// Warn logs a warning message to the default logger
func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

// Error logs an error message to the default logger
func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

// Fatal logs a fatal message to the default logger and exits
func Fatal(format string, args ...interface{}) {
	defaultLogger.Fatal(format, args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	defaultLogger.Debugf(format, args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	defaultLogger.Infof(format, args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	defaultLogger.Warnf(format, args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	defaultLogger.Errorf(format, args...)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(format string, args ...interface{}) {
	defaultLogger.Fatalf(format, args...)
}

// WithOutput returns a new logger with the specified output
func WithOutput(w io.Writer) *Logger {
	opts := &Options{
		Level:    defaultLogger.level,
		Output:   w,
		Prefix:   defaultLogger.prefix,
		TimeFmt:  defaultLogger.timeFmt,
		Colorful: defaultLogger.colorful,
	}
	return New(opts)
}

// WithPrefix returns a new logger with the specified prefix
func WithPrefix(prefix string) *Logger {
	opts := &Options{
		Level:    defaultLogger.level,
		Output:   defaultLogger.output,
		Prefix:   prefix,
		TimeFmt:  defaultLogger.timeFmt,
		Colorful: defaultLogger.colorful,
	}
	return New(opts)
}

// WithLevel returns a new logger with the specified level
func WithLevel(level Level) *Logger {
	opts := &Options{
		Level:    level,
		Output:   defaultLogger.output,
		Prefix:   defaultLogger.prefix,
		TimeFmt:  defaultLogger.timeFmt,
		Colorful: defaultLogger.colorful,
	}
	return New(opts)
}

// UseLogger sets the default logger
func UseLogger(l *Logger) {
	defaultLogger = l
}

// Global logger for use throughout the application
var Global = defaultLogger

// Initialize initializes the global logger with options
func Initialize(opts *Options) {
	defaultLogger = New(opts)
}

// StderrLogger returns a logger that writes to stderr
func StderrLogger() *Logger {
	return New(&Options{
		Level:    LevelInfo,
		Output:   os.Stderr,
		Colorful: false,
	})
}

// DiscardLogger returns a logger that discards all output
func DiscardLogger() *Logger {
	return New(&Options{
		Level:    LevelDebug,
		Output:   io.Discard,
		Colorful: false,
	})
}

// ExportLogger exports the logger for use by CGO or other packages
func ExportLogger() *log.Logger {
	return log.New(defaultLogger.output, "", 0)
}
