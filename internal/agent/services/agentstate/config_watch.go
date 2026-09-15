package agentstate

import "sync"

// The agent watches its configuration file so an edit is picked up
// without a restart. That watch can fail to start for reasons that have
// nothing to do with the configuration: on Linux inotify has a per-user
// instance quota, and a host running k3s can hold most of it.
//
// Treating that as a fatal start meant the agent exited, systemd
// restarted it five times, gave up, and the host stopped being
// monitored — because a convenience could not start (#850). The agent
// now runs without it, and this is how the operator finds out: a
// metric, the CLI status and the daemon's status payload, not one WARN
// line in a log the host may no longer be shipping.

// ConfigWatchState describes why the configuration is not being
// watched. Reason is a stable enum; Detail carries the operator-facing
// message.
type ConfigWatchState struct {
	Reason string
	Detail string
}

// Reasons the watch is not running. Keep the set small and stable.
const (
	ConfigWatchUnavailable = "watcher_unavailable" // the kernel refused a watcher (inotify quota, unsupported fs)
	ConfigWatchNotWatched  = "path_not_watched"    // the watcher exists but a path could not be added
)

var configWatch = struct {
	mu    sync.RWMutex
	state *ConfigWatchState
}{}

// RecordConfigWatchDisabled marks the configuration as unwatched.
func RecordConfigWatchDisabled(reason, detail string) {
	if reason == "" {
		reason = "unknown"
	}
	RecordEvent(EventWarn, EventKindConfig, "watch", "configuration watch disabled ("+reason+"): "+detail)
	configWatch.mu.Lock()
	configWatch.state = &ConfigWatchState{Reason: reason, Detail: detail}
	configWatch.mu.Unlock()
}

// ClearConfigWatchDisabled marks the configuration as watched again.
// Called on every successful start, so a restart that gets its watcher
// stops reporting without anything else to do.
func ClearConfigWatchDisabled() {
	configWatch.mu.Lock()
	configWatch.state = nil
	configWatch.mu.Unlock()
}

// GetConfigWatchDisabled returns the current degraded state, or nil
// when the configuration is being watched.
func GetConfigWatchDisabled() *ConfigWatchState {
	configWatch.mu.RLock()
	defer configWatch.mu.RUnlock()
	if configWatch.state == nil {
		return nil
	}
	snapshot := *configWatch.state
	return &snapshot
}
