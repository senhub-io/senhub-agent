package ibmi

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// collector is the seam between a single named SQL service and the
// DataPoints it produces. Every collector owns exactly one query and the
// logic to decode its rows into DataPoints. The probe composes a slice of
// collectors and executes them serially in Collect().
//
// Keeping this interface minimal is deliberate: no per-collector timeout,
// no per-collector interval, no knowledge of the probe's state. That's
// the job of the probe runner (runCollector below), which owns timing,
// logging and health bookkeeping.
//
// A collector may produce either regular metric DataPoints (routed to
// the default metric strategies — senhub/prtg/http) or event DataPoints
// (routed to the event strategy which streams them to a log sink).
// The distinction is carried by IsEvent(): the probe's Collect groups
// points by this flag and invokes the framework callback with two
// different StrategyRouters — see ibmiProbe.Collect.
//
// Stateful event collectors (which keep a watermark across cycles) use
// pointer receivers; stateless metric collectors use value receivers.
// The interface does not care.
type collector interface {
	// Name is the stable identifier used in health metric tags and log
	// fields. It must be lowercase with underscores (e.g. "system_status").
	Name() string

	// SQL returns the single-line SQL statement to execute. The bridge
	// protocol does not accept embedded newlines, so callers must keep
	// their text on a single line. Stateful collectors may return a
	// different string on every call (e.g. to embed a watermark).
	SQL() string

	// Parse converts a successful query result into DataPoints. The host
	// argument is the LPAR hostname from probe configuration; Parse
	// implementations should attach it as a tag. The ts argument is the
	// timestamp shared across all DataPoints emitted in the same Collect
	// cycle — using a single timestamp per cycle keeps metric batches
	// correlated when consumed by downstream strategies. Stateful
	// collectors may mutate their internal state from Parse (e.g.
	// advance a watermark).
	Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error)

	// IsEvent reports whether this collector produces event DataPoints
	// (routed to the event strategy) rather than metric DataPoints
	// (routed to senhub/prtg/http). Defaults for all metric collectors
	// is false.
	IsEvent() bool
}

// eventRouter is a minimal StrategyRouter that sends to the event
// strategy only. The probe passes this router as the second argument
// to its OnDataPoints callback for batches of event DataPoints.
//
// At integration time this becomes a simple inline anonymous struct
// with the same method — senhub-agent's data_store.StrategyRouter is
// a one-method interface, so any type satisfying GetTargetStrategies
// works without imports.
type eventRouter struct{}

func (eventRouter) GetTargetStrategies() []string { return []string{"event"} }

// collectorState accumulates per-collector health counters for the whole
// lifetime of the probe. The probe reads this state at the end of every
// Collect cycle to emit self-health DataPoints.
type collectorState struct {
	successTotal     uint64
	failureTotal     uint64
	lastDuration     time.Duration
	lastSuccessAt    time.Time
	lastErrorMessage string
	lastExecutionAt  time.Time
}

// runCollector executes a single collector with the probe's query timeout
// and updates its health state. It never returns an error: a collector
// failing is an expected operational condition, not a fatal one. The
// return value is the slice of DataPoints the caller should append to
// the aggregated output — nil on failure.
func (p *ibmiProbe) runCollector(ctx context.Context, c collector, state *collectorState, ts time.Time) []datapoint.DataPoint {
	queryCtx, cancel := context.WithTimeout(ctx, p.cfg.QueryTimeout)
	defer cancel()

	started := time.Now()
	res, err := p.executor.Query(queryCtx, c.SQL())
	duration := time.Since(started)

	state.lastExecutionAt = started
	state.lastDuration = duration

	if err != nil {
		state.failureTotal++
		state.lastErrorMessage = err.Error()
		p.logger.Warn().
			Err(err).
			Str("collector", c.Name()).
			Dur("duration", duration).
			Msg("collector query failed")
		return nil
	}

	points, err := c.Parse(res, p.cfg.Host, ts)
	if err != nil {
		state.failureTotal++
		state.lastErrorMessage = err.Error()
		p.logger.Warn().
			Err(err).
			Str("collector", c.Name()).
			Msg("collector parse failed")
		return nil
	}

	state.successTotal++
	state.lastSuccessAt = started
	state.lastErrorMessage = ""

	// Derive counter-based rates/deltas. The deltaStore lives on the
	// probe (not per-collector) so identical metric names coming from
	// distinct rows (per-job, per-subsystem, ...) are each keyed by
	// their full tag set and tracked independently.
	if p.deltas != nil && len(counterMetrics) > 0 {
		var derived []datapoint.DataPoint
		for _, dp := range points {
			if !counterMetrics[dp.Name] {
				continue
			}
			derived = append(derived, p.deltas.Derive(dp)...)
		}
		if len(derived) > 0 {
			points = append(points, derived...)
		}
	}

	p.logger.Debug().
		Str("collector", c.Name()).
		Dur("duration", duration).
		Int("points", len(points)).
		Msg("collector ok")
	return points
}

// buildHealthDataPoints emits four DataPoints per collector summarising
// its state since the probe started. Values are snapshots taken at ts so
// every health point in a cycle shares the same timestamp as the metrics
// cycle they describe.
func (p *ibmiProbe) buildHealthDataPoints(ts time.Time) []datapoint.DataPoint {
	hostTag := tags.Tag{Key: "host", Value: p.cfg.Host}
	out := make([]datapoint.DataPoint, 0, len(p.collectorStates)*4)
	for name, state := range p.collectorStates {
		tags := []tags.Tag{hostTag, {Key: "collector", Value: name}}
		out = append(out,
			datapoint.DataPoint{
				Name:      "ibmi.collector.success_total",
				Timestamp: ts,
				Value:     float32(state.successTotal),
				Tags:      tags,
			},
			datapoint.DataPoint{
				Name:      "ibmi.collector.failure_total",
				Timestamp: ts,
				Value:     float32(state.failureTotal),
				Tags:      tags,
			},
			datapoint.DataPoint{
				Name:      "ibmi.collector.last_duration_ms",
				Timestamp: ts,
				Value:     float32(state.lastDuration.Milliseconds()),
				Tags:      tags,
			},
		)
		if !state.lastSuccessAt.IsZero() {
			out = append(out, datapoint.DataPoint{
				Name:      "ibmi.collector.last_success_timestamp",
				Timestamp: ts,
				Value:     float32(state.lastSuccessAt.Unix()),
				Tags:      tags,
			})
		}
	}
	return out
}

// columnIndex builds a map from column name to row index. The IBM i
// driver returns column labels in the exact case they were requested
// with, so collector Parse implementations look them up by the same
// SELECT column name they emitted.
func columnIndex(columns []string) map[string]int {
	idx := make(map[string]int, len(columns))
	for i, name := range columns {
		idx[name] = i
	}
	return idx
}

// requireCell fetches a cell from a row by column name and returns an
// error if the column is missing entirely from the result set. A NULL
// value (present but nil) is returned as ("", true) — the caller decides
// whether to treat it as a skip or as an error.
func requireCell(row []*string, idx map[string]int, column string) (string, bool, error) {
	pos, ok := idx[column]
	if !ok {
		return "", false, fmt.Errorf("column %q missing from result", column)
	}
	if pos >= len(row) {
		return "", false, fmt.Errorf("column %q index %d out of bounds", column, pos)
	}
	if row[pos] == nil {
		return "", true, nil
	}
	return *row[pos], true, nil
}
