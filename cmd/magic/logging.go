package main

import (
	"fmt"
	"io"
	"os"
	stdlog "log"
	"time"

	pkglog "github.com/magicwubiao/go-magic/pkg/log"
)

var (
	verbose bool
	logFile *os.File
	logger  *pkglog.Logger
)

func init() {
	for _, arg := range os.Args {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
			break
		}
	}
}

func logInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.Info("%s", msg)
	writeLogFile("[INFO] " + msg)
}

func logError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.Error("%s", msg)
	writeLogFile("[ERROR] " + msg)
}

func logDebug(format string, args ...interface{}) {
	if !verbose {
		return
	}
	msg := fmt.Sprintf(format, args...)
	logger.Debug("%s", msg)
	writeLogFile("[DEBUG] " + msg)
}

func logWarn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.Warn("%s", msg)
	writeLogFile("[WARN] " + msg)
}

func writeLogFile(prefixMsg string) {
	if logFile != nil {
		logFile.WriteString(time.Now().Format("2006-01-02 15:04:05 ") + prefixMsg + "\n")
	}
}

func initLogging() {
	level := pkglog.LevelInfo
	if verbose {
		level = pkglog.LevelDebug
	}

	logger = pkglog.New(&pkglog.Options{
		Level:    level,
		Output:   os.Stderr,
		Prefix:   "magic",
		TimeFmt:  "2006-01-02 15:04:05",
		Colorful: true,
	})

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	logDir := home + "/.magic/logs"
	os.MkdirAll(logDir, 0755)

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	logPath := logDir + "/magic_" + timestamp + ".log"

	f, err := os.Create(logPath)
	if err != nil {
		return
	}

	logFile = f
	writeLogFile("[INFO] Logging initialized: " + logPath)

	// Redirect Go's standard log to write to our log file only (not terminal).
	// This captures stdlog.Printf calls from internal packages (agent, provider, etc.)
	// without cluttering the user's conversation output.
	stdlog.SetOutput(&logWriter{file: f})
}

// logWriter wraps a log file to implement io.Writer for stdlog redirection.
// It prepends a timestamp to each line.
type logWriter struct {
	file *os.File
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	if w.file != nil {
		// stdlog already adds its own timestamp via its format flags,
		// so we write directly
		w.file.Write(p)
	}
	return len(p), nil
}

// truncateLogMsg truncates a message for logging purposes
func truncateLogMsg(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
