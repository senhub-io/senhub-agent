//go:build !windows

package app

import (
	"senhub-agent.go/internal/agent/services/logger"
)

func ensureServiceRecovery(*logger.Logger) {}

func applyRecoveryAfterInstall() error { return nil }
