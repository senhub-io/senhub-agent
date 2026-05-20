package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// jvmCollector reports per-JVM heap, thread and GC metrics from
// QSYS2.JVM_INFO. Many IBM i production workloads run Java (WAS,
// JBoss, Domino, custom apps) and a JVM going into GC thrash or
// running out of heap is a silent outage pattern.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-jvm-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type jvmCollector struct{}

func (jvmCollector) Name() string  { return "jvm" }
func (jvmCollector) IsEvent() bool { return false }

func (jvmCollector) SQL() string {
	return "SELECT JOB_NAME, JOB_USER, PROCESS_ID, JAVA_THREAD_COUNT, TOTAL_GC_TIME, GC_CYCLE_NUMBER, INITIAL_HEAP_SIZE, CURRENT_HEAP_SIZE, IN_USE_HEAP_SIZE, MAX_HEAP_SIZE, MALLOC_MEMORY_SIZE, BIT_MODE FROM QSYS2.JVM_INFO"
}

func (jvmCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		// No JVMs running is a legitimate state. Emit a zero
		// aggregate so the series stays continuous.
		return []datapoint.DataPoint{{
			Name:      "ibmi.jvm.instances_total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{{Key: "host", Value: host}},
		}}, nil
	}

	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*8+1)

	for _, row := range res.Rows {
		jobName := trimmedCell(row, idx, "JOB_NAME")
		if jobName == "" {
			continue
		}
		jobUser := trimmedCell(row, idx, "JOB_USER")
		bitMode := trimmedCell(row, idx, "BIT_MODE")

		tags := []tags.Tag{
			hostTag,
			{Key: "job_name", Value: jobName},
			{Key: "job_user", Value: jobUser},
			{Key: "bit_mode", Value: bitMode},
		}

		metrics := []struct {
			column string
			metric string
		}{
			{"JAVA_THREAD_COUNT", "ibmi.jvm.thread_count"},
			{"TOTAL_GC_TIME", "ibmi.jvm.gc_time_ms"},
			{"GC_CYCLE_NUMBER", "ibmi.jvm.gc_cycles_total"},
			{"INITIAL_HEAP_SIZE", "ibmi.jvm.heap_initial_bytes"},
			{"CURRENT_HEAP_SIZE", "ibmi.jvm.heap_current_bytes"},
			{"IN_USE_HEAP_SIZE", "ibmi.jvm.heap_in_use_bytes"},
			{"MAX_HEAP_SIZE", "ibmi.jvm.heap_max_bytes"},
			{"MALLOC_MEMORY_SIZE", "ibmi.jvm.malloc_bytes"},
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
		Name:      "ibmi.jvm.instances_total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("jvm produced no datapoints")
	}
	return points, nil
}
