package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// diskStatusCollector reads per-disk-unit stats from QSYS2.SYSDISKSTAT.
// Emits one DataPoint set per physical unit with tags unit_number,
// disk_type, resource_name, asp_number. This is the equivalent of
// WRKDSKSTS and gives an operator a disk-by-disk view of the LPAR.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-sysdiskstat
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type diskStatusCollector struct{}

func (diskStatusCollector) Name() string  { return "disk_status" }
func (diskStatusCollector) IsEvent() bool { return false }

func (diskStatusCollector) SQL() string {
	return "SELECT ASP_NUMBER, UNIT_NUMBER, DISK_TYPE, RESOURCE_NAME, RESOURCE_STATUS, UNIT_STORAGE_CAPACITY, UNIT_SPACE_AVAILABLE_GB, PERCENT_USED, ELAPSED_PERCENT_BUSY, ELAPSED_READ_REQUESTS, ELAPSED_WRITE_REQUESTS, ELAPSED_DATA_READ, ELAPSED_DATA_WRITTEN FROM QSYS2.SYSDISKSTAT"
}

func (diskStatusCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("SYSDISKSTAT returned no rows")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)*7+1)

	for _, row := range res.Rows {
		unit := trimmedCell(row, idx, "UNIT_NUMBER")
		if unit == "" {
			continue
		}
		tags := []tags.Tag{
			hostTag,
			{Key: "unit_number", Value: unit},
			{Key: "asp_number", Value: trimmedCell(row, idx, "ASP_NUMBER")},
			{Key: "disk_type", Value: trimmedCell(row, idx, "DISK_TYPE")},
			{Key: "resource_name", Value: trimmedCell(row, idx, "RESOURCE_NAME")},
			{Key: "status", Value: trimmedCell(row, idx, "RESOURCE_STATUS")},
		}

		metrics := []struct {
			column string
			metric string
		}{
			{"UNIT_STORAGE_CAPACITY", "ibmi.disk.capacity_bytes"},
			{"UNIT_SPACE_AVAILABLE_GB", "ibmi.disk.available_gb"},
			{"PERCENT_USED", "ibmi.disk.percent_used"},
			{"ELAPSED_PERCENT_BUSY", "ibmi.disk.percent_busy"},
			{"ELAPSED_READ_REQUESTS", "ibmi.disk.read_requests"},
			{"ELAPSED_WRITE_REQUESTS", "ibmi.disk.write_requests"},
			{"ELAPSED_DATA_READ", "ibmi.disk.data_read_bytes"},
			{"ELAPSED_DATA_WRITTEN", "ibmi.disk.data_written_bytes"},
		}
		for _, m := range metrics {
			v, ok := parseFloatCell(row, idx, m.column)
			if !ok {
				continue
			}
			points = append(points, datapoint.DataPoint{
				Name:      m.metric,
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.disk.units_total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})
	return points, nil
}
