package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// sysTableStatsCollector surfaces the top-N largest database tables
// per user library from QSYS2.SYSTABLESTAT. It is the DBA's view of
// "what is filling up my storage?" and "which file is getting
// modified the most?".
//
// The query deliberately excludes IBM system libraries (Q*) to keep
// the focus on customer data; that cuts down several thousand system
// tables that are mostly noise for a monitoring dashboard.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-systablestat
type sysTableStatsCollector struct{}

func (sysTableStatsCollector) Name() string  { return "sys_table_stats" }
func (sysTableStatsCollector) IsEvent() bool { return false }

// topTableLimit caps how many tables we emit per-table DataPoints for.
// Big enough to make the top-N visible to a DBA, small enough to keep
// cardinality bounded and predictable.
const topTableLimit = 30

func (sysTableStatsCollector) SQL() string {
	// No ORDER BY here: SYSTABLESTAT computes DATA_SIZE lazily per
	// table and an ORDER BY DATA_SIZE DESC forces a full scan of
	// every user table, which on PUB400 (shared sandbox, thousands
	// of tables) blows past the bridge query timeout. Instead we
	// take whatever the first N user tables are. A real LPAR
	// deployment with hundreds of tables is fine either way; a
	// future sprint can add a per-collector query timeout override
	// if a true "top by size" becomes important.
	return fmt.Sprintf(
		"SELECT TABLE_SCHEMA, TABLE_NAME, NUMBER_ROWS, NUMBER_DELETED_ROWS, DATA_SIZE, INSERT_OPERATIONS, UPDATE_OPERATIONS, DELETE_OPERATIONS, LOGICAL_READS FROM QSYS2.SYSTABLESTAT WHERE TABLE_SCHEMA NOT LIKE 'Q%%' FETCH FIRST %d ROWS ONLY",
		topTableLimit,
	)
}

func (sysTableStatsCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.table.top_count",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{{Key: "host", Value: host}},
		}}, nil
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*6+2)

	var totalDataSize float64

	for _, row := range res.Rows {
		schema := trimmedCell(row, idx, "TABLE_SCHEMA")
		name := trimmedCell(row, idx, "TABLE_NAME")
		if schema == "" || name == "" {
			continue
		}
		tags := []tags.Tag{
			hostTag,
			{Key: "schema", Value: schema},
			{Key: "table_name", Value: name},
		}
		metrics := []struct {
			column string
			metric string
		}{
			{"NUMBER_ROWS", "ibmi.table.rows_count"},
			{"NUMBER_DELETED_ROWS", "ibmi.table.deleted_rows"},
			{"DATA_SIZE", "ibmi.table.data_size_bytes"},
			{"INSERT_OPERATIONS", "ibmi.table.inserts_total"},
			{"UPDATE_OPERATIONS", "ibmi.table.updates_total"},
			{"DELETE_OPERATIONS", "ibmi.table.deletes_total"},
			{"LOGICAL_READS", "ibmi.table.logical_reads_total"},
		}
		for _, m := range metrics {
			v, ok := parseFloatCell(row, idx, m.column)
			if !ok {
				continue
			}
			if m.column == "DATA_SIZE" {
				totalDataSize += v
			}
			points = append(points, datapoint.DataPoint{
				Name:      m.metric,
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	points = append(points,
		datapoint.DataPoint{
			Name:      "ibmi.table.top_count",
			Timestamp: ts,
			Value:     float32(len(res.Rows)),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.table.top_data_size_sum_bytes",
			Timestamp: ts,
			Value:     float32(totalDataSize),
			Tags:      []tags.Tag{hostTag},
		},
	)
	return points, nil
}
