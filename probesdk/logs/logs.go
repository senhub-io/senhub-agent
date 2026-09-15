// Package logs is the public mirror of the agent's log rail
// (senhub-agent.go/internal/agent/services/agentstate): the record a
// probe publishes and the call that publishes it. A probe that produces
// logs builds a Record per line and calls Publish; the strategies that
// consume logs (OTLP among them) receive it. Probe packages from the
// separate senhub-agent-enterprise module use this mirror because Go
// forbids importing senhub-agent.go/internal/... across module boundaries.
package logs

import "senhub-agent.go/internal/agent/services/agentstate"

// Record is one log record on the rail, OTel-shaped without the SDK.
type Record = agentstate.LogRecord

// Severity mirrors the OTel SeverityNumber range.
type Severity = agentstate.LogSeverity

const (
	SeverityUnspecified = agentstate.LogSeverityUnspecified
	SeverityTrace       = agentstate.LogSeverityTrace
	SeverityDebug       = agentstate.LogSeverityDebug
	SeverityInfo        = agentstate.LogSeverityInfo
	SeverityWarn        = agentstate.LogSeverityWarn
	SeverityError       = agentstate.LogSeverityError
	SeverityFatal       = agentstate.LogSeverityFatal
)

// Publish fans the record out to every log consumer it routes to. Set
// Record.TargetStrategies from the probe's LogTargets() so the operator's
// per-probe log routing applies; empty broadcasts.
func Publish(rec Record) { agentstate.PublishLog(rec) }
