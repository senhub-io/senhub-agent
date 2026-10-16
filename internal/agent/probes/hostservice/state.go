// Package hostservice holds what the probes of OS service supervisors share:
// one series, so a systemd unit and a Windows service that are in the same
// situation report the same value under the same name.
package hostservice

// RunningMetric is the series every supervisor probe emits per watched
// service: 1 when it runs, 0 otherwise. Whether a service runs is a
// measurement, not a graph attribute: a service that flaps would otherwise
// write an entity event at each change, forever, and an alert reading it
// would run at the graph producer's cadence instead of the probe's.
//
// A boolean rather than a state scale: "running == 0" then catches every
// way of not running, a failed service included, without a mapping table.
// The detailed state stays in each probe's own series under detail: full.
const RunningMetric = "host.service.running"

// RunningSystemd reports whether a unit's ActiveState counts as running.
func RunningSystemd(activeState string) float64 {
	switch activeState {
	case "active", "reloading", "refreshing":
		return 1
	}
	return 0
}

// scmRunning is SERVICE_RUNNING (SERVICE_STATUS dwCurrentState).
const scmRunning = 4

// RunningWindows reports whether a service's SCM current state is running.
func RunningWindows(scmState int) float64 {
	if scmState == scmRunning {
		return 1
	}
	return 0
}
