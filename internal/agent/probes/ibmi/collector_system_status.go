package ibmi

import (
	"fmt"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// systemStatusCollector reads a single-row snapshot of LPAR-wide CPU,
// storage and job counters from QSYS2.SYSTEM_STATUS_INFO. This was the
// original (and only) collector in Sprint 1; Sprint 2 refactors it to
// fit the collector interface without changing the emitted metrics.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-system-status-info
type systemStatusCollector struct{}

func (systemStatusCollector) Name() string  { return "system_status" }
func (systemStatusCollector) IsEvent() bool { return false }

func (systemStatusCollector) SQL() string {
	return `SELECT ELAPSED_CPU_USED, CONFIGURED_CPUS, CURRENT_CPU_CAPACITY, MAIN_STORAGE_SIZE, SYSTEM_ASP_USED, TOTAL_JOBS_IN_SYSTEM FROM QSYS2.SYSTEM_STATUS_INFO`
}

// systemStatusMetric describes a single column → metric mapping. Kept as
// a package-level variable so tests can inspect the catalogue without
// reproducing the list.
type systemStatusMetric struct {
	column string
	metric string
}

var systemStatusMetrics = []systemStatusMetric{
	{"ELAPSED_CPU_USED", "ibmi.cpu.elapsed_used_percent"},
	{"CONFIGURED_CPUS", "ibmi.cpu.configured_count"},
	{"CURRENT_CPU_CAPACITY", "ibmi.cpu.current_capacity"},
	{"MAIN_STORAGE_SIZE", "ibmi.memory.main_storage_kb"},
	{"SYSTEM_ASP_USED", "ibmi.asp.system_used_percent"},
	{"TOTAL_JOBS_IN_SYSTEM", "ibmi.jobs.total_count"},
}

func (systemStatusCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("SYSTEM_STATUS_INFO returned no rows")
	}
	row := res.Rows[0]
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	var points []datapoint.DataPoint
	var parseErrors []string

	for _, m := range systemStatusMetrics {
		raw, present, err := requireCell(row, idx, m.column)
		if err != nil {
			// Column is missing from the result set — this is a schema
			// change worth surfacing. We don't fail the whole collector
			// because a single missing column shouldn't take down the
			// other five, but we do accumulate the error for the caller.
			parseErrors = append(parseErrors, err.Error())
			continue
		}
		if !present {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			parseErrors = append(parseErrors,
				fmt.Sprintf("column %q unparseable: %q", m.column, raw))
			continue
		}
		points = append(points, datapoint.DataPoint{
			Name:      m.metric,
			Timestamp: ts,
			Value:     float32(v),
			Tags:      []tags.Tag{hostTag},
		})
	}

	// Only return a hard error if we got literally zero datapoints.
	// Otherwise we let the probe emit what we managed to parse and
	// surface the warnings through the logger.
	if len(points) == 0 && len(parseErrors) > 0 {
		return nil, fmt.Errorf("all columns unparseable: %v", parseErrors)
	}
	return points, nil
}
