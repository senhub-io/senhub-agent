package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// hardwareResourceCollector aggregates QSYS2.HARDWARE_RESOURCE_INFO
// by RESOURCE_CATEGORY × STATUS. Per-resource rows would be noisy
// (hundreds of devices on a real LPAR) and mostly static — the
// signal worth watching is the *count* of resources in each status
// bucket per category, so we push the GROUP BY to IBM i's side and
// bound the result at a handful of rows.
//
// This collector is the functional equivalent of the "lines /
// controllers / devices" check that monitoring products have had
// forever. `QSYS2.LINE_INFO` does not exist as a SQL Service in
// 7.5 (verified on PUB400 on 2026-04-17 — SQL0204), but
// HARDWARE_RESOURCE_INFO covers the same ground more broadly:
// communications adapters, workstation controllers, processors,
// storage adapters, LAN adapters, etc.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-hardware-resource-info
type hardwareResourceCollector struct{}

func (hardwareResourceCollector) Name() string  { return "hardware_resource" }
func (hardwareResourceCollector) IsEvent() bool { return false }

func (hardwareResourceCollector) SQL() string {
	return "SELECT RESOURCE_CATEGORY, STATUS, COUNT(*) AS RESOURCE_COUNT FROM QSYS2.HARDWARE_RESOURCE_INFO GROUP BY RESOURCE_CATEGORY, STATUS"
}

func (hardwareResourceCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.hardware_resource.total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)+2)
	var total, nonOperational float64

	for _, row := range res.Rows {
		category := trimmedCell(row, idx, "RESOURCE_CATEGORY")
		if category == "" {
			category = "<unknown>"
		}
		status := trimmedCell(row, idx, "STATUS")
		if status == "" {
			status = "<unknown>"
		}
		v, ok := parseFloatCell(row, idx, "RESOURCE_COUNT")
		if !ok {
			continue
		}
		total += v
		// Anything other than OPERATIONAL counts as a degraded
		// resource. The explicit non_operational_total aggregate
		// makes it trivial to build an alert without iterating the
		// per-status series.
		if !strings.EqualFold(status, "OPERATIONAL") {
			nonOperational += v
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.hardware_resource.count",
			Timestamp: ts,
			Value:     float32(v),
			Tags: []tags.Tag{
				hostTag,
				{Key: "category", Value: category},
				{Key: "status", Value: status},
			},
		})
	}

	points = append(points,
		datapoint.DataPoint{
			Name:      "ibmi.hardware_resource.total",
			Timestamp: ts,
			Value:     float32(total),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.hardware_resource.non_operational_total",
			Timestamp: ts,
			Value:     float32(nonOperational),
			Tags:      []tags.Tag{hostTag},
		},
	)

	if len(points) == 0 {
		return nil, fmt.Errorf("HARDWARE_RESOURCE_INFO produced no usable datapoints")
	}
	return points, nil
}
