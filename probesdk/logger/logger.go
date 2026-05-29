// Package logger is the public mirror of the agent's structured logger
// (senhub-agent.go/internal/agent/services/logger). Methods ride along
// with the type aliases, so probe code logs exactly as before.
package logger

import (
	icliArgs "senhub-agent.go/internal/agent/cliArgs"
	ilogger "senhub-agent.go/internal/agent/services/logger"
)

type (
	Logger       = ilogger.Logger
	ModuleLogger = ilogger.ModuleLogger
)

// NewLogger builds a root logger from parsed CLI args. Probe tests use
// it to construct a logger the way the runtime does.
func NewLogger(args *icliArgs.ParsedArgs) *Logger {
	return ilogger.NewLogger(args)
}

// NewModuleLogger derives a module-scoped logger from a base logger.
func NewModuleLogger(base *Logger, module string) *ModuleLogger {
	return ilogger.NewModuleLogger(base, module)
}
