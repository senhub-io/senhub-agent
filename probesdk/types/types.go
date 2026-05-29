// Package types is the public mirror of the agent's probe interface and
// param helpers (senhub-agent.go/internal/agent/probes/types).
package types

import itypes "senhub-agent.go/internal/agent/probes/types"

// Core probe contract. Aliases preserve type identity, so a probe that
// embeds BaseProbe and implements Probe satisfies the internal registry.
type (
	Probe             = itypes.Probe
	ProbeWithCallback = itypes.ProbeWithCallback
	BaseProbe         = itypes.BaseProbe
)

// IntParam / FloatParam read a numeric value from the free-form probe
// params map decoded from YAML.
func IntParam(m map[string]interface{}, key string) (int, bool) {
	return itypes.IntParam(m, key)
}

func FloatParam(m map[string]interface{}, key string) (float64, bool) {
	return itypes.FloatParam(m, key)
}
