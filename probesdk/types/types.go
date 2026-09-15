// Package types is the public mirror of the agent's probe interface and
// param helpers (senhub-agent.go/internal/agent/probes/types).
package types

import (
	"time"

	itypes "senhub-agent.go/internal/agent/probes/types"
)

// Core probe contract. Aliases preserve type identity, so a probe that
// embeds BaseProbe and implements Probe satisfies the internal registry.
type (
	Probe             = itypes.Probe
	ProbeWithCallback = itypes.ProbeWithCallback
	BaseProbe         = itypes.BaseProbe
)

// The param helpers read a typed value out of the free-form probe params
// map decoded from YAML, accepting every encoding the decode path can
// produce for that type. Use them instead of a bare type assertion:
// `params["interval"].(int)` silently falls back to the default when the
// loader hands over a float64 or a string (#136).

func IntParam(m map[string]interface{}, key string) (int, bool) {
	return itypes.IntParam(m, key)
}

func FloatParam(m map[string]interface{}, key string) (float64, bool) {
	return itypes.FloatParam(m, key)
}

func StringParam(m map[string]interface{}, key string) (string, bool) {
	return itypes.StringParam(m, key)
}

func BoolParam(m map[string]interface{}, key string) (bool, bool) {
	return itypes.BoolParam(m, key)
}

// StringSlice coerces an already-extracted value into a []string; use
// StringSliceParam when the map lookup is still to be done.
func StringSlice(raw interface{}) []string {
	return itypes.StringSlice(raw)
}

func StringSliceParam(m map[string]interface{}, key string) ([]string, bool) {
	return itypes.StringSliceParam(m, key)
}

// DurationParam reads a bare number as seconds and a string through
// time.ParseDuration, so `timeout: 45` and `timeout: 45s` agree.
func DurationParam(m map[string]interface{}, key string) (time.Duration, bool) {
	return itypes.DurationParam(m, key)
}
