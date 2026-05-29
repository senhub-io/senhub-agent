package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// outputQueueCollector reports per-outq spool file counts from
// QSYS2.OUTPUT_QUEUE_INFO. PUB400 carries thousands of user outqs, most
// of them empty, so the collector filters to queues with at least one
// spooled file to keep the time series cardinality sane.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-output-queue-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type outputQueueCollector struct{}

func (outputQueueCollector) Name() string  { return "output_queue" }
func (outputQueueCollector) IsEvent() bool { return false }

func (outputQueueCollector) SQL() string {
	return `SELECT OUTPUT_QUEUE_NAME, OUTPUT_QUEUE_LIBRARY_NAME, OUTPUT_QUEUE_STATUS, NUMBER_OF_FILES, NUMBER_OF_WRITERS FROM QSYS2.OUTPUT_QUEUE_INFO WHERE NUMBER_OF_FILES > 0`
}

func (outputQueueCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	// Zero rows is valid (no non-empty outqs). We still emit an
	// aggregate zero-count datapoint so downstream dashboards have a
	// sample to plot, but we do NOT treat it as a parse error.
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	var points []datapoint.DataPoint
	var totalFiles float64
	var totalQueues int

	for _, row := range res.Rows {
		name, present, _ := requireCell(row, idx, "OUTPUT_QUEUE_NAME")
		if !present {
			continue
		}
		library, _, _ := requireCell(row, idx, "OUTPUT_QUEUE_LIBRARY_NAME")
		status, _, _ := requireCell(row, idx, "OUTPUT_QUEUE_STATUS")

		tags := []tags.Tag{
			hostTag,
			{Key: "queue_name", Value: strings.TrimSpace(name)},
			{Key: "queue_library", Value: strings.TrimSpace(library)},
			{Key: "queue_status", Value: strings.TrimSpace(status)},
		}

		if v, ok := parseFloatCell(row, idx, "NUMBER_OF_FILES"); ok {
			totalFiles += v
			totalQueues++
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.output_queue.files_count",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "NUMBER_OF_WRITERS"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.output_queue.writers_count",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	// Per-host aggregate — always emitted so the time series is
	// continuous even when no outq currently holds spooled files.
	points = append(points,
		datapoint.DataPoint{
			Name:      "ibmi.output_queue.nonempty_total",
			Timestamp: ts,
			Value:     float32(totalQueues),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.output_queue.spooled_files_total",
			Timestamp: ts,
			Value:     float32(totalFiles),
			Tags:      []tags.Tag{hostTag},
		},
	)

	if len(points) == 0 {
		return nil, fmt.Errorf("OUTPUT_QUEUE_INFO produced no usable datapoints")
	}
	return points, nil
}
