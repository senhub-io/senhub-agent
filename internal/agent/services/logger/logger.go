package logger

import (
	"os"
	"fmt"
	"github.com/rs/zerolog"
	"log"
	"path/filepath"
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
	logPath := getLogPath()
	runLogFile, err := os.OpenFile(
			logPath,
			os.O_APPEND|os.O_CREATE|os.O_WRONLY,
			0664,
	)
	if err != nil {
			panic(fmt.Sprintf("Cannot open logfile: %v", err))
	}

	var logger *Logger
	switch args.Env {
	case "development":
		logger = buildDevelopmentLogger(runLogFile)
	default:
		logger = buildProductionLogger(runLogFile)
	}

	if args.Verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	return logger
}

func buildDevelopmentLogger(logFile *os.File) *Logger {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	multi := zerolog.MultiLevelWriter(
		zerolog.ConsoleWriter{Out: os.Stderr},
		logFile,
	)

	logger := zerolog.
		New(multi).
		With().
		Timestamp().
		Logger()

	return &logger
}

func buildProductionLogger(logFile *os.File) *Logger {
	zerolog.SetGlobalLevel(zerolog.WarnLevel)

	logger := zerolog.
		New(logFile).
		With().
		Timestamp().
		Logger()

	return &logger
}
