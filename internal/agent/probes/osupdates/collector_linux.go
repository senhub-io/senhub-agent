//go:build linux

package osupdates

import "senhub-agent.go/internal/agent/services/logger"

func newOSUpdatesCollector(moduleLogger *logger.ModuleLogger) updatesCollector {
	return newLinuxCollector(moduleLogger)
}
