package http

import (
	"senhub-agent.go/internal/agent/services/agentstate"
)

// withTargetDown turns a PRTG answer into a Warning that names the cause
// when the probe last reported its target down: the sensor text says
// why, and the first channel carries the warning flag.
func withTargetDown(res PRTGResult, state agentstate.ProbeRunState, known, enabled bool) PRTGResult {
	if !enabled || !known || !state.Running || !state.TargetDown || len(res.Result) == 0 {
		return res
	}
	channels := make([]PRTGChannel, len(res.Result))
	copy(channels, res.Result)
	channels[0].Warning = 1
	res.Result = channels
	if state.TargetError != "" {
		res.Text = "Target down: " + state.TargetError
	} else {
		res.Text = "Target down"
	}
	return res
}
