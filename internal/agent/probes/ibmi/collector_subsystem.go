package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// subsystemCollector reports current and maximum job counts per active
// subsystem from QSYS2.SUBSYSTEM_INFO. Only subsystems with STATUS =
// 'ACTIVE' are selected to keep cardinality reasonable on busy systems
// where dozens of inactive subsystem descriptions may coexist.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-subsystem-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type subsystemCollector struct{}

func (subsystemCollector) Name() string  { return "subsystem" }
func (subsystemCollector) IsEvent() bool { return false }

func (subsystemCollector) SQL() string {
	return `SELECT SUBSYSTEM_DESCRIPTION, SUBSYSTEM_DESCRIPTION_LIBRARY, STATUS, CURRENT_ACTIVE_JOBS, MAXIMUM_ACTIVE_JOBS, CONTROLLING_SUBSYSTEM FROM QSYS2.SUBSYSTEM_INFO WHERE STATUS = 'ACTIVE'`
}

func (subsystemCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		// Zero rows is legitimate — no active subsystems — but unusual
		// enough that we still want to flag it for the caller. The
		// probe runner surfaces this as a parse error in the health
		// counters without taking the probe down.
		return nil, fmt.Errorf("SUBSYSTEM_INFO WHERE STATUS='ACTIVE' returned no rows")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	var points []datapoint.DataPoint
	for _, row := range res.Rows {
		name, present, _ := requireCell(row, idx, "SUBSYSTEM_DESCRIPTION")
		if !present {
			continue
		}
		library, _, _ := requireCell(row, idx, "SUBSYSTEM_DESCRIPTION_LIBRARY")
		controlling, _, _ := requireCell(row, idx, "CONTROLLING_SUBSYSTEM")

		tags := []tags.Tag{
			hostTag,
			{Key: "subsystem", Value: strings.TrimSpace(name)},
			{Key: "library", Value: strings.TrimSpace(library)},
			{Key: "controlling", Value: strings.TrimSpace(controlling)},
		}

		if v, ok := parseFloatCell(row, idx, "CURRENT_ACTIVE_JOBS"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.subsystem.active_jobs_count",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		// MAXIMUM_ACTIVE_JOBS is often NULL on IBM i when the subsystem
		// has no cap (*NOMAX). Skip gracefully rather than emit zero.
		if v, ok := parseFloatCell(row, idx, "MAXIMUM_ACTIVE_JOBS"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.subsystem.max_active_jobs",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	if len(points) == 0 {
		return nil, fmt.Errorf("SUBSYSTEM_INFO produced no usable datapoints")
	}
	return points, nil
}
