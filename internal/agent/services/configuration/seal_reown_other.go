//go:build !linux

package configuration

import "senhub-agent.go/internal/agent/services/logger"

// reownAfterSeal is Linux-only. On Windows the store is protected by DPAPI +
// icacls (machine scope, readable by the service account) and on darwin the
// agent is a dev/test target, so there is nothing to re-own.
func reownAfterSeal(_ string, _ *logger.ModuleLogger) {}
