package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	agentCliArgs "senhub-agent.go/internal/agent/cliArgs"
)

type Logger = zerolog.Logger

type LoggerConfig struct {
	logFile *os.File
}

func getLogPath() string {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	exeDir := filepath.Dir(exePath)
	logFilePath := filepath.Join(exeDir, "senhubagent.log")

	return logFilePath
}

func NewLogger(args *agentCliArgs.ParsedArgs) *Logger {
	var logger *Logger
	switch args.Env {
	case "development":
		logger = buildDevelopmentLogger()
	default:
		logger = buildProductionLogger()
	}

	if args.Verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	return logger
}

func buildDevelopmentLogger() *Logger {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	logger := zerolog.
		New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().
		Timestamp().
		Logger()

	return &logger
}

func buildProductionLogger() *Logger {
	logPath := getLogPath()
	logFile, err := os.OpenFile(
		logPath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0664,
	)

	if err != nil {
		panic(fmt.Sprintf("Cannot open logfile: %v", err))
	}

	zerolog.SetGlobalLevel(zerolog.WarnLevel)

	logger := zerolog.
		New(logFile).
		With().
		Timestamp().
		Logger()

	return &logger
}
