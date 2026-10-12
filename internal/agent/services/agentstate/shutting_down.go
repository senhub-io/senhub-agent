package agentstate

import "sync/atomic"

var shuttingDown atomic.Bool

// MarkShuttingDown records that the agent was asked to stop. It is called
// before any service is told to stop, so a subprocess that the service
// manager terminated at the same moment (systemd KillMode=control-group
// signals the whole cgroup at once) can be told apart from one that died.
func MarkShuttingDown() { shuttingDown.Store(true) }

// ShuttingDown reports whether a stop was requested.
func ShuttingDown() bool { return shuttingDown.Load() }
