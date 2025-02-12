package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
	"senhub-agent.go/internal/agent/cliArgs"
)

// Logger is an alias type for zerolog.Logger
type Logger = zerolog.Logger

// LoggerConfig holds the configuration for the logger
type LoggerConfig struct {
	logFile io.WriteCloser
}

// getLogPath determines the appropriate log file location based on the operating system.
// It follows OS-specific conventions for log placement:
// - Windows: %ProgramData%\SenHub\logs
// - macOS: /Library/Logs/SenHub
// - Linux/Unix: /var/log/senhub
// If the desired location is not writable, it falls back to the executable's directory
func getLogPath() string {
	var basePath string

	switch runtime.GOOS {
	case "windows":
		// For Windows, try to use %ProgramData% directory first
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		basePath = filepath.Join(programData, "SenHub", "logs")
	case "darwin":
		// For macOS, use the standard system logs directory
		basePath = "/Library/Logs/SenHub"
	default:
		// For Linux/Unix systems, use the conventional /var/log directory
		basePath = "/var/log/senhub"
	}

	// Attempt to create the log directory if it doesn't exist
	// This will also test if we have sufficient permissions
	if err := os.MkdirAll(basePath, 0755); err != nil {
		log.Printf("Unable to create log directory: %v", err)
		// Fall back to executable directory if we can't write to the preferred location
		exePath, _ := os.Executable()
		basePath = filepath.Dir(exePath)
	}

	return filepath.Join(basePath, "senhubagent.log")
}

// NewLogger creates a new logger instance based on the provided arguments.
// It switches between development and production configurations based on the environment.
func NewLogger(args *cliArgs.ParsedArgs) *Logger {
	var logger *Logger
	switch args.Env {
	case "development":
		logger = buildDevelopmentLogger(args)
	default:
		logger = buildProductionLogger(args)
	}

	// Enable debug level logging if verbose mode is requested
	if args.Verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	return logger
}

// buildDevelopmentLogger creates a development-oriented logger configuration
// that writes formatted logs to stderr with debug level enabled
func buildDevelopmentLogger(*cliArgs.ParsedArgs) *Logger {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	logger := zerolog.
		New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().
		Timestamp().
		Logger()
	return &logger
}

// buildProductionLogger creates a production-oriented logger configuration with:
// - Log rotation (10MB file size trigger)
// - Compression of rotated logs
// - Maximum of 5 backup files
// - 30-day retention period
// If verbose mode is enabled, it will also output to stderr alongside the file
func buildProductionLogger(args *cliArgs.ParsedArgs) *Logger {
	logPath := getLogPath()

	// Configure log rotation settings
	logRotator := &lumberjack.Logger{
		Filename:   logPath, // Path to the log file
		MaxSize:    10,      // Megabytes before rotation
		MaxBackups: 5,       // Number of backup files to keep
		MaxAge:     30,      // Days to keep backup files
		Compress:   true,    // Enable compression of rotated logs
	}

	var logWriter io.Writer = logRotator

	// If verbose mode is enabled, create a multi-writer to output to both
	// stderr and the log file
	if args.Verbose {
		logWriter = zerolog.MultiLevelWriter(
			zerolog.ConsoleWriter{Out: os.Stderr},
			logRotator,
		)
	}

	// Set default production log level to warn
	zerolog.SetGlobalLevel(zerolog.WarnLevel)

	// Create and configure the logger with timestamp
	logger := zerolog.
		New(logWriter).
		With().
		Timestamp().
		Logger()

	return &logger
}
