package main

import (
	"fmt"
	"os"
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
	logger.Info(msg)
	writeLogFile("[INFO] " + msg)
}

func logError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.Error(msg)
	writeLogFile("[ERROR] " + msg)
}

func logDebug(format string, args ...interface{}) {
	if !verbose {
		return
	}
	msg := fmt.Sprintf(format, args...)
	logger.Debug(msg)
	writeLogFile("[DEBUG] " + msg)
}

func logWarn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.Warn(msg)
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
	logInfo("Logging initialized: %s", logPath)
}
