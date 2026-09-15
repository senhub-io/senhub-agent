package agentstate

import "sync"

// Strategy start failures live here so the exposition bridges can read
// them without importing the data_store (same reason as the OTLP
// counters in this package).
//
// A strategy that is configured but refuses to start is the agent's
// quietest failure mode: the process stays up, the remaining strategies
// keep working, and the only trace is one ERR line at boot. That is how
// an agent ran for 24 minutes shipping nothing over OTLP while its HTTP
// endpoints looked perfectly healthy (#826). Recording the failure as
// state — not just as a log — lets it reach a metric, the CLI status and
// the dashboard.

// StrategyFailure describes why a configured strategy is not running.
// Reason is a stable enum so dashboards can pivot on it; Detail carries
// the operator-facing message (already redacted by the caller).
type StrategyFailure struct {
	Reason string
	Detail string
}

// Failure reasons. Keep the set small and stable.
const (
	StrategyFailureUnknownType   = "unknown_type"
	StrategyFailureCreate        = "create"
	StrategyFailureInvalidConfig = "invalid_config"
	StrategyFailureStart         = "start"
)

var strategyFailures = struct {
	mu sync.RWMutex
	m  map[string]StrategyFailure
}{m: map[string]StrategyFailure{}}

// RecordStrategyFailure marks a configured strategy as not running. A
// second failure for the same name overwrites the first: only the
// current state matters.
func RecordStrategyFailure(name, reason, detail string) {
	if name == "" {
		name = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	strategyFailures.mu.Lock()
	_, again := strategyFailures.m[name]
	strategyFailures.m[name] = StrategyFailure{Reason: reason, Detail: detail}
	strategyFailures.mu.Unlock()
	if !again {
		RecordEvent(EventError, EventKindOutput, name, "not running ("+reason+"): "+detail)
	}
}

// ClearStrategyFailure drops the failure state for a strategy that is
// now running. Called on every successful start so a fixed
// configuration stops alerting without an agent restart.
func ClearStrategyFailure(name string) {
	strategyFailures.mu.Lock()
	_, was := strategyFailures.m[name]
	delete(strategyFailures.m, name)
	strategyFailures.mu.Unlock()
	if was {
		RecordEvent(EventInfo, EventKindOutput, name, "running again")
	}
}

// PruneStrategyFailures drops failures for strategies that are no
// longer in the configuration at all. Without this, deleting a broken
// strategy fragment would leave the agent reporting it as failing
// forever.
func PruneStrategyFailures(configured []string) {
	keep := make(map[string]bool, len(configured))
	for _, name := range configured {
		keep[name] = true
	}
	strategyFailures.mu.Lock()
	for name := range strategyFailures.m {
		if !keep[name] {
			delete(strategyFailures.m, name)
		}
	}
	strategyFailures.mu.Unlock()
}

// GetStrategyFailures returns a snapshot copy of the current failures,
// keyed by strategy name. Empty means every configured strategy is
// running.
func GetStrategyFailures() map[string]StrategyFailure {
	strategyFailures.mu.RLock()
	defer strategyFailures.mu.RUnlock()
	out := make(map[string]StrategyFailure, len(strategyFailures.m))
	for k, v := range strategyFailures.m {
		out[k] = v
	}
	return out
}

// ResetStrategyFailuresForTest clears the state so tests in other
// packages can assert on a known baseline.
func ResetStrategyFailuresForTest() {
	strategyFailures.mu.Lock()
	strategyFailures.m = map[string]StrategyFailure{}
	strategyFailures.mu.Unlock()
}
