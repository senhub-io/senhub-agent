package agentstate

import "sync"

// OpAMP client state, read by the self-metric bridge and the status
// channel. The client lives in its own package; keeping its counters here
// is what lets the exposition bridges read them without importing it.

// OpAMP error reasons. Keep the set small and stable: dashboards pivot on it.
const (
	OpAMPErrorConnect         = "connect"
	OpAMPErrorServer          = "server"
	OpAMPErrorReport          = "report"
	OpAMPErrorEffectiveConfig = "effective_config"
)

// OpAMPErrorReasons is every reason the errors counter is seeded with.
var OpAMPErrorReasons = []string{OpAMPErrorConnect, OpAMPErrorServer, OpAMPErrorReport, OpAMPErrorEffectiveConfig}

var opampState = struct {
	mu        sync.Mutex
	enabled   bool
	connected bool
	sent      uint64
	errors    map[string]uint64
}{errors: map[string]uint64{}}

// SetOpAMPEnabled records whether the OpAMP client is configured on. Turning
// it on declares every error reason at zero, so an alert on the counter
// does not stay silent until the first error.
func SetOpAMPEnabled(on bool) {
	opampState.mu.Lock()
	defer opampState.mu.Unlock()
	opampState.enabled = on
	if !on {
		opampState.connected = false
		return
	}
	for _, r := range OpAMPErrorReasons {
		if _, ok := opampState.errors[r]; !ok {
			opampState.errors[r] = 0
		}
	}
}

// SetOpAMPConnected records the connection state of the OpAMP client.
func SetOpAMPConnected(connected bool) {
	opampState.mu.Lock()
	opampState.connected = connected
	opampState.mu.Unlock()
}

// IncrementOpAMPMessagesSent counts one report handed to the OpAMP client.
func IncrementOpAMPMessagesSent() {
	opampState.mu.Lock()
	opampState.sent++
	opampState.mu.Unlock()
}

// IncrementOpAMPErrors counts one OpAMP error by reason.
func IncrementOpAMPErrors(reason string) {
	if reason == "" {
		reason = "unknown"
	}
	opampState.mu.Lock()
	opampState.errors[reason]++
	opampState.mu.Unlock()
}

// OpAMPSnapshot is a copy of the OpAMP client state.
type OpAMPSnapshot struct {
	Enabled      bool
	Connected    bool
	MessagesSent uint64
	Errors       map[string]uint64
}

// GetOpAMPState returns a copy of the OpAMP client state.
func GetOpAMPState() OpAMPSnapshot {
	opampState.mu.Lock()
	defer opampState.mu.Unlock()
	out := OpAMPSnapshot{
		Enabled:      opampState.enabled,
		Connected:    opampState.connected,
		MessagesSent: opampState.sent,
		Errors:       make(map[string]uint64, len(opampState.errors)),
	}
	for k, v := range opampState.errors {
		out.Errors[k] = v
	}
	return out
}

// ResetOpAMPForTest clears the state. Test-only.
func ResetOpAMPForTest() {
	opampState.mu.Lock()
	opampState.enabled, opampState.connected, opampState.sent = false, false, 0
	opampState.errors = map[string]uint64{}
	opampState.mu.Unlock()
}
