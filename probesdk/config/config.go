// Package configuration is the public mirror of the slice of the agent
// configuration loader that out-of-core commands need
// (senhub-agent.go/internal/agent/services/configuration). It exposes
// the read-only "load for show/inspection" path — enough for an
// enterprise subcommand (e.g. `ibmi check`) to read the merged config
// and walk its probe entries without importing the internal package.
package configuration

import (
	iconfig "senhub-agent.go/internal/agent/services/configuration"
	ilogger "senhub-agent.go/internal/agent/services/logger"
)

type (
	// ShowMode selects how ${env:}/${file:} references are rendered.
	ShowMode = iconfig.ShowMode
	// ProbeConfig is a single probe entry from the merged configuration.
	ProbeConfig = iconfig.ProbeConfig
	// LocalConfigurationData is the merged, on-disk configuration.
	LocalConfigurationData = iconfig.LocalConfigurationData
)

const (
	ShowResolved = iconfig.ShowResolved
	ShowRaw      = iconfig.ShowRaw
	ShowRedact   = iconfig.ShowRedact
)

// LoadForShow loads and merges the configuration at configPath for
// read-only inspection, applying reference rendering per mode.
func LoadForShow(configPath string, mode ShowMode, log *ilogger.ModuleLogger) (LocalConfigurationData, error) {
	return iconfig.LoadForShow(configPath, mode, log)
}
