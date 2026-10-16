// Package hostservice holds what the probes of OS service supervisors share:
// one state scale, so a systemd unit and a Windows service that are in the
// same situation report the same value under the same series name.
package hostservice

// StateMetric is the series every supervisor probe emits per watched service.
// The state is a measurement, not a graph attribute: a service that flaps
// would otherwise write an entity event at each change, forever, and an alert
// would run at the graph producer's cadence instead of the probe's.
const StateMetric = "host.service.state"

// State values. Running is the only healthy value for a service the
// supervisor is supposed to start by itself.
const (
	Stopped  = 0
	Running  = 1
	Starting = 2
	Stopping = 3
	Failed   = 4
	Paused   = 5
	Other    = 6
)

// FromSystemd maps a unit's ActiveState.
func FromSystemd(activeState string) int {
	switch activeState {
	case "active", "reloading", "refreshing":
		return Running
	case "inactive":
		return Stopped
	case "activating":
		return Starting
	case "deactivating":
		return Stopping
	case "failed":
		return Failed
	default:
		return Other
	}
}

// Windows SCM states (SERVICE_STATUS dwCurrentState).
const (
	scmStopped         = 1
	scmStartPending    = 2
	scmStopPending     = 3
	scmRunning         = 4
	scmContinuePending = 5
	scmPausePending    = 6
	scmPaused          = 7
)

// FromWindows maps a service's SCM current state.
func FromWindows(scmState int) int {
	switch scmState {
	case scmRunning:
		return Running
	case scmStopped:
		return Stopped
	case scmStartPending, scmContinuePending:
		return Starting
	case scmStopPending, scmPausePending:
		return Stopping
	case scmPaused:
		return Paused
	default:
		return Other
	}
}
