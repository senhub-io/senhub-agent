package ibmi

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// aspCollector reads per-ASP storage metrics from QSYS2.ASP_INFO. Each
// row in the result set becomes its own set of DataPoints tagged with
// the ASP number and type.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-asp-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
// TOTAL_CAPACITY and *_CAPACITY_* are documented in megabytes.
type aspCollector struct{}

func (aspCollector) Name() string  { return "asp" }
func (aspCollector) IsEvent() bool { return false }

func (aspCollector) SQL() string {
	return `SELECT ASP_NUMBER, ASP_TYPE, RDB_NAME, NUMBER_OF_DISK_UNITS, TOTAL_CAPACITY, TOTAL_CAPACITY_AVAILABLE, OVERFLOW_STORAGE, STORAGE_THRESHOLD_PERCENTAGE FROM QSYS2.ASP_INFO`
}

func (aspCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("ASP_INFO returned no rows")
	}
	idx := columnIndex(res.Columns)

	type metricSpec struct {
		column string
		metric string
	}
	metrics := []metricSpec{
		{"NUMBER_OF_DISK_UNITS", "ibmi.asp.disk_units_count"},
		{"TOTAL_CAPACITY", "ibmi.asp.total_capacity_mb"},
		{"TOTAL_CAPACITY_AVAILABLE", "ibmi.asp.available_capacity_mb"},
		{"OVERFLOW_STORAGE", "ibmi.asp.overflow_storage_mb"},
		{"STORAGE_THRESHOLD_PERCENTAGE", "ibmi.asp.storage_threshold_percent"},
	}

	hostTag := tags.Tag{Key: "host", Value: host}
	var points []datapoint.DataPoint
	for _, row := range res.Rows {
		aspNumber, present, err := requireCell(row, idx, "ASP_NUMBER")
		if err != nil || !present {
			continue
		}
		aspType, _, _ := requireCell(row, idx, "ASP_TYPE")
		rdbName, _, _ := requireCell(row, idx, "RDB_NAME")

		tags := []tags.Tag{
			hostTag,
			{Key: "asp_number", Value: strings.TrimSpace(aspNumber)},
			{Key: "asp_type", Value: strings.TrimSpace(aspType)},
			{Key: "rdb_name", Value: strings.TrimSpace(rdbName)},
		}

		// Derived metric: % used = (TOTAL - AVAILABLE) / TOTAL * 100.
		// Computed first so a broken TOTAL column does not silently
		// produce misleading percentages.
		total, totalOK := parseFloatCell(row, idx, "TOTAL_CAPACITY")
		avail, availOK := parseFloatCell(row, idx, "TOTAL_CAPACITY_AVAILABLE")
		if totalOK && availOK && total > 0 {
			usedPercent := (total - avail) / total * 100
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.asp.used_percent",
				Timestamp: ts,
				Value:     float32(usedPercent),
				Tags:      tags,
			})
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

	if len(points) == 0 {
		return nil, fmt.Errorf("ASP_INFO produced no usable datapoints")
	}
	return points, nil
}

// parseFloatCell is a small helper shared by the per-row collectors:
// given a row and a column name, return the parsed float and an ok flag
// that is false for missing columns, NULLs, or unparseable strings.
// Parse errors are intentionally silent — the caller has already
// decided the column is optional by calling this helper.
func parseFloatCell(row []*string, idx map[string]int, column string) (float64, bool) {
	raw, present, err := requireCell(row, idx, column)
	if err != nil || !present {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
