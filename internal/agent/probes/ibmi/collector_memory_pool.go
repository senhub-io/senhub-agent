package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// memoryPoolCollector reports per-pool memory sizes and thread counts
// from QSYS2.MEMORY_POOL_INFO. Pool sizes are returned by IBM i in
// megabytes as decimal strings (e.g. "10240.00").
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-memory-pool-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
// POOL_NAME is returned padded with trailing spaces (e.g. "*MACHINE  "),
// so tags are trimmed before emission.
type memoryPoolCollector struct{}

func (memoryPoolCollector) Name() string  { return "memory_pool" }
func (memoryPoolCollector) IsEvent() bool { return false }

func (memoryPoolCollector) SQL() string {
	return `SELECT SYSTEM_POOL_ID, POOL_NAME, CURRENT_SIZE, RESERVED_SIZE, DEFINED_SIZE, CURRENT_THREADS, CURRENT_INELIGIBLE_THREADS FROM QSYS2.MEMORY_POOL_INFO`
}

func (memoryPoolCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("MEMORY_POOL_INFO returned no rows")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	metrics := []struct {
		column string
		metric string
	}{
		{"CURRENT_SIZE", "ibmi.memory_pool.current_size_mb"},
		{"RESERVED_SIZE", "ibmi.memory_pool.reserved_size_mb"},
		{"DEFINED_SIZE", "ibmi.memory_pool.defined_size_mb"},
		{"CURRENT_THREADS", "ibmi.memory_pool.current_threads"},
		{"CURRENT_INELIGIBLE_THREADS", "ibmi.memory_pool.ineligible_threads"},
	}

	var points []datapoint.DataPoint
	for _, row := range res.Rows {
		poolID, present, _ := requireCell(row, idx, "SYSTEM_POOL_ID")
		if !present {
			continue
		}
		poolName, _, _ := requireCell(row, idx, "POOL_NAME")

		tags := []tags.Tag{
			hostTag,
			{Key: "pool_id", Value: strings.TrimSpace(poolID)},
			{Key: "pool_name", Value: strings.TrimSpace(poolName)},
		}

		for _, m := range metrics {
			if v, ok := parseFloatCell(row, idx, m.column); ok {
				points = append(points, datapoint.DataPoint{
					Name:      m.metric,
					Timestamp: ts,
					Value:     float32(v),
					Tags:      tags,
				})
			}
		}
	}

	if len(points) == 0 {
		return nil, fmt.Errorf("MEMORY_POOL_INFO produced no usable datapoints")
	}
	return points, nil
}
