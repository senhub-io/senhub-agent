package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// spooledFileCollector reports spool backlog from
// QSYS2.SPOOLED_FILE_INFO. Per-file details would explode cardinality
// on a busy LPAR (thousands of rows), so the collector aggregates
// server-side: one row per STATUS with count + oldest CREATION_TIMESTAMP.
// From that the Parse path emits three families of DataPoints:
//
//   - ibmi.spooled_file.count_by_status (per-status gauge)
//   - ibmi.spooled_file.oldest_age_seconds_by_status (per-status gauge)
//   - ibmi.spooled_file.total / ibmi.spooled_file.oldest_age_seconds
//     (host-wide aggregates, useful for alerting without summing tags)
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-spooled-file-info
//
// Column CREATION_TIMESTAMP verified on PUB400 (IBM i 7.5, 2026-04-17).
// Using MIN(CREATION_TIMESTAMP) per group so the oldest file surfaces
// even when younger ones dominate the count.
type spooledFileCollector struct{}

func (spooledFileCollector) Name() string  { return "spooled_file" }
func (spooledFileCollector) IsEvent() bool { return false }

func (spooledFileCollector) SQL() string {
	// SPOOLED_FILE_INFO is a table function in IBM i 7.5, not a view.
	// `FROM QSYS2.SPOOLED_FILE_INFO` raises SQL0204 on PUB400 (2026-04-18
	// live check) — the correct call syntax is `FROM TABLE(...())`.
	return "SELECT STATUS, COUNT(*) AS FILE_COUNT, MIN(CREATION_TIMESTAMP) AS OLDEST_CREATED FROM TABLE(QSYS2.SPOOLED_FILE_INFO()) AS X GROUP BY STATUS"
}

func (spooledFileCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	// Empty result = no spooled files system-wide. Emit a zero-valued
	// total so dashboards stay continuous.
	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.spooled_file.total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)*2+2)
	var totalFiles float64
	// oldestGlobal is the minimum across statuses — zero means "not
	// yet observed". We iterate raw strings and convert once at the
	// end to avoid mixing parse paths.
	oldestGlobal := time.Time{}

	for _, row := range res.Rows {
		status := trimmedCell(row, idx, "STATUS")
		if status == "" {
			status = "<unknown>"
		}
		tags := []tags.Tag{hostTag, {Key: "status", Value: status}}

		if v, ok := parseFloatCell(row, idx, "FILE_COUNT"); ok {
			totalFiles += v
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.spooled_file.count_by_status",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}

		rawTs, present, _ := requireCell(row, idx, "OLDEST_CREATED")
		if !present || rawTs == "" {
			continue
		}
		oldest, err := time.ParseInLocation(ibmiTimestampLayout, rawTs, time.Local)
		if err != nil {
			// SPOOLED_FILE_INFO may return timestamps without the
			// microsecond fraction on older PTF levels. Fall back
			// to the short layout before giving up.
			oldest, err = time.ParseInLocation(ibmiTimestampShortLayout, rawTs, time.Local)
			if err != nil {
				continue
			}
		}
		age := ts.Sub(oldest).Seconds()
		if age < 0 {
			age = 0
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.spooled_file.oldest_age_seconds_by_status",
			Timestamp: ts,
			Value:     float32(age),
			Tags:      tags,
		})
		if oldestGlobal.IsZero() || oldest.Before(oldestGlobal) {
			oldestGlobal = oldest
		}
	}

	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.spooled_file.total",
		Timestamp: ts,
		Value:     float32(totalFiles),
		Tags:      []tags.Tag{hostTag},
	})
	if !oldestGlobal.IsZero() {
		age := ts.Sub(oldestGlobal).Seconds()
		if age < 0 {
			age = 0
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.spooled_file.oldest_age_seconds",
			Timestamp: ts,
			Value:     float32(age),
			Tags:      []tags.Tag{hostTag},
		})
	}

	if len(points) == 0 {
		return nil, fmt.Errorf("SPOOLED_FILE_INFO produced no usable datapoints")
	}
	return points, nil
}
