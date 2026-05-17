package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// watchInfoCollector reports the set of active `STRWCH` watch
// sessions via QSYS2.WATCH_INFO. Watches are IBM i's mechanism for
// triggering a program when a specific message, LIC log entry, or
// PAL entry appears — they're the closest equivalent to a Unix
// `tail -f | grep | exec`. Tracking how many watches are running,
// who started them and when, gives an operator visibility into
// the active monitoring stack on the LPAR.
//
// **Opt-in collector.** QSYS2.WATCH_INFO requires the reading user
// to have at least `*USE` authority on the watch object, which the
// PGMR profile on PUB400 does not carry (verified 2026-04-17 —
// SQL0443 returned). The collector sits in allKnownCollectors() and
// is activated via `enabled_collectors: [..., watch_info]`.
//
// We deliberately do NOT expose controls for starting or stopping
// watches — the probe stays strictly read-only (ADR-002).
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-watch-info
type watchInfoCollector struct{}

func newWatchInfoCollector() *watchInfoCollector { return &watchInfoCollector{} }

func (*watchInfoCollector) Name() string  { return "watch_info" }
func (*watchInfoCollector) IsEvent() bool { return false }

func (*watchInfoCollector) SQL() string {
	return "SELECT SESSION_ID, WATCH_PROGRAM_NAME, WATCH_PROGRAM_LIBRARY, ORIGIN_JOB, SESSION_START_TIMESTAMP FROM QSYS2.WATCH_INFO"
}

func (*watchInfoCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.watch.active_total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	// Per-session rows give operational visibility of individual
	// watches (who started them, how long they've been running).
	// A busy LPAR typically has a handful — the cardinality is
	// naturally bounded by the number of concurrent watchers.
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*2+1)

	for _, row := range res.Rows {
		sessionID := trimmedCell(row, idx, "SESSION_ID")
		if sessionID == "" {
			continue
		}
		program := trimmedCell(row, idx, "WATCH_PROGRAM_NAME")
		library := trimmedCell(row, idx, "WATCH_PROGRAM_LIBRARY")
		originJob := trimmedCell(row, idx, "ORIGIN_JOB")

		tags := []tags.Tag{
			hostTag,
			{Key: "session_id", Value: sessionID},
			{Key: "program", Value: program},
			{Key: "program_library", Value: library},
			{Key: "origin_job", Value: originJob},
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.watch.session_active",
			Timestamp: ts,
			Value:     1,
			Tags:      tags,
		})
		rawStart, present, _ := requireCell(row, idx, "SESSION_START_TIMESTAMP")
		if present && rawStart != "" {
			if started, err := time.ParseInLocation(ibmiTimestampLayout, rawStart, time.Local); err == nil {
				age := ts.Sub(started).Seconds()
				if age < 0 {
					age = 0
				}
				points = append(points, datapoint.DataPoint{
					Name:      "ibmi.watch.session_age_seconds",
					Timestamp: ts,
					Value:     float32(age),
					Tags:      tags,
				})
			}
		}
	}

	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.watch.active_total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("WATCH_INFO produced no usable datapoints")
	}
	return points, nil
}
