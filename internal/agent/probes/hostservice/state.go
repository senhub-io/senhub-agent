// Package hostservice holds what the probes of OS service supervisors share:
// one series, so a systemd unit and a Windows service that are in the same
// situation report the same value under the same name.
package hostservice

// RunningMetric is the series every supervisor probe emits per watched
// service: 1 when it runs, 0 otherwise. It remains the measure to alert on.
// Since the entity model validation of 28/10, the coarse status is also
// carried as a graph attribute (StatusKey): a service with automatic start
// that is stopped must be visible in the graph, and a series alone is not
// part of an entity.
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

// StatusKey is the entity attribute key for the coarse service status.
// It is a bare NUE key because consumers recognize the status by its exact
// spelling, and it is never omitted: an absent status is unreadable.
const StatusKey = "status"

// StatusSystemd maps a systemd ActiveState to the coarse status. Any value
// outside the known states, including empty, maps to unknown.
func StatusSystemd(activeState string) string {
	switch activeState {
	case "active", "reloading", "refreshing":
		return "running"
	case "inactive", "maintenance":
		return "stopped"
	case "activating":
		return "starting"
	case "deactivating":
		return "stopping"
	case "failed":
		return "failed"
	}
	return "unknown"
}

// SCM current states from SERVICE_STATUS dwCurrentState.
const (
	scmStopped         = 1
	scmStartPending    = 2
	scmStopPending     = 3
	scmContinuePending = 5
	scmPausePending    = 6
	scmPaused          = 7
)

// StatusWindows maps a SCM current state to the coarse status. There is no
// failed state under Windows; anything unrecognized maps to unknown.
func StatusWindows(scmState int) string {
	switch scmState {
	case scmStopped:
		return "stopped"
	case scmStartPending, scmContinuePending:
		return "starting"
	case scmStopPending, scmPausePending:
		return "stopping"
	case scmRunning:
		return "running"
	case scmPaused:
		return "paused"
	}
	return "unknown"
}
