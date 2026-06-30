package app

import "senhub-agent.go/internal/agent/services/auto_update"

// checkAutoUpdateWritability reports an actionable diagnostic when
// auto_update.enabled is set but the running binary cannot be replaced in
// place by the in-process updater (root-owned binary under the hardened
// non-root unit would fail every update cycle — #377), or "" when it can.
// Used by `config check`; the running daemon emits the same diagnostic at
// startup (internal/agent.NewAgentWithArgs).
func checkAutoUpdateWritability() string {
	return auto_update.CheckBinaryReplaceable()
}
