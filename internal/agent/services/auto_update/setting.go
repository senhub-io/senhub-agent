package auto_update

import (
	"senhub-agent.go/internal/agent/services/agentstate"
)

// publishSetting records the auto-update configuration in force in
// agentstate, so the metrics and the CLI status expose what the updater is
// actually doing rather than a stale file read. The CLI-driven updater has
// no config source and publishes nothing.
func (a *autoUpdate) publishSetting() {
	if a.configSource == nil {
		return
	}
	enabled, version := a.effectiveSettings()
	if version == "" {
		version = "latest"
	}
	source := agentstate.UpdateSourceLocal
	if CurrentDirective() != nil {
		source = agentstate.UpdateSourceServer
	}
	agentstate.SetUpdateSetting(agentstate.UpdateSetting{
		Enabled:     enabled,
		IncludeBeta: a.configSource.GetConfiguration().Agent.IncludeBeta,
		Target:      version,
		Source:      source,
		Paused:      Paused(),
	})
}
