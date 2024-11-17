package logger

import (
	"os"

	"github.com/rs/zerolog"
	agentCliArgs "senhub-agent.go/internal/agent/cliArgs"
)

type Logger = zerolog.Logger

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
	zerolog.SetGlobalLevel(zerolog.WarnLevel)

	logger := zerolog.
		New(os.Stderr).
		With().
		Timestamp().
		Logger()

	return &logger
}
