package http

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// withNagiosTargetDown adds the probe's last check outcome to its Nagios answer:
// a probe whose check failed is WARNING with the cause, unless the answer
// is already worse, in which case the cause is appended to its message.
func withNagiosTargetDown(resp NagiosResponse, probeName string, state agentstate.ProbeRunState, known bool) NagiosResponse {
	if !known || !state.Running || !state.TargetDown {
		return resp
	}
	cause := state.TargetError
	if cause == "" {
		cause = "check failed"
	}
	if resp.Status >= 1 {
		resp.Message += " (last check: " + cause + ")"
		return resp
	}
	resp.Status = 1
	resp.StatusText = "WARNING"
	resp.Message = fmt.Sprintf("Probe %s target down: %s", probeName, cause)
	return resp
}
