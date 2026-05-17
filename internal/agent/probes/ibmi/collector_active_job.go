package ibmi

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// activeJobCollector is the single source of truth for per-job metrics.
// It issues one SELECT against QSYS2.ACTIVE_JOB_INFO() ordered by
// ELAPSED_CPU_PERCENTAGE DESC with a generous FETCH FIRST limit that
// covers every reasonable LPAR workload (200 rows). From that result
// set the Parse path emits two kinds of DataPoints:
//
//  1. **Aggregates**: job counts grouped by (JOB_TYPE × JOB_STATUS)
//     and by SUBSYSTEM. Cardinality is bounded and stable because the
//     enum values come from IBM (SYS/BCH/INT/PRT/..., ACTIVE/MSGW/
//     LCKW/...).
//  2. **Top-N details**: the first 20 rows (the heaviest CPU
//     consumers) are expanded into per-job DataPoints carrying the
//     job identity in tags — job_name, user_name, subsystem.
//
// The single-SQL strategy saves one full round-trip compared to two
// separate collectors, and avoids any risk of time drift between an
// "aggregate" and a "top-N" snapshot of the same LPAR.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-active-job-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect:
// ACTIVE_JOB_INFO() returns 125 columns — we only SELECT the ~10 we
// actually need so the bridge JSON payload stays manageable.
type activeJobCollector struct{}

func (activeJobCollector) Name() string  { return "active_job" }
func (activeJobCollector) IsEvent() bool { return false }

// activeJobRowLimit caps the number of rows retrieved per cycle. A
// busy LPAR typically runs 500–2000 active jobs but we don't need
// all of them — the aggregate view counts correctly up to the cap,
// and the top-N pulls the biggest consumers out of the first slice.
const activeJobRowLimit = 200

// topJobLimit is how many high-CPU jobs we emit per-job DataPoints for.
// 20 is a deliberate trade-off: big enough to make a good demo and
// cover the "top" workload, small enough that cardinality stays sane
// across many cycles.
const topJobLimit = 20

func (activeJobCollector) SQL() string {
	return fmt.Sprintf(
		"SELECT JOB_NAME, JOB_USER, SUBSYSTEM, JOB_TYPE, JOB_STATUS, RUN_PRIORITY, THREAD_COUNT, TEMPORARY_STORAGE, CPU_TIME, ELAPSED_CPU_PERCENTAGE, ELAPSED_CPU_TIME, TOTAL_DISK_IO_COUNT, CLIENT_IP_ADDRESS, FUNCTION, ELAPSED_TOTAL_DISK_IO_COUNT, ELAPSED_PAGE_FAULT_COUNT FROM TABLE(QSYS2.ACTIVE_JOB_INFO()) AS X ORDER BY ELAPSED_CPU_PERCENTAGE DESC FETCH FIRST %d ROWS ONLY",
		activeJobRowLimit,
	)
}

func (activeJobCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("ACTIVE_JOB_INFO returned no rows")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	// --- Aggregates -----------------------------------------------------
	//
	// Two group-by dimensions are computed at the same time:
	//   - (job_type × status)   → surface the MSGW/LCKW/ACTIVE mix
	//   - subsystem             → workload breakdown
	//
	// A standalone total count is also emitted so dashboards can
	// divide per-subsystem values by the overall total without
	// summing the tagged series themselves.
	typeStatusCounts := make(map[[2]string]int, 16)
	subsystemCounts := make(map[string]int, 16)
	total := 0

	for _, row := range res.Rows {
		jobType := trimmedCell(row, idx, "JOB_TYPE")
		status := trimmedCell(row, idx, "JOB_STATUS")
		subsystem := trimmedCell(row, idx, "SUBSYSTEM")
		if subsystem == "" {
			subsystem = "<system>"
		}
		typeStatusCounts[[2]string{jobType, status}]++
		subsystemCounts[subsystem]++
		total++
	}

	points := make([]datapoint.DataPoint, 0, len(typeStatusCounts)+len(subsystemCounts)+topJobLimit*9+1)

	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.jobs.active_total",
		Timestamp: ts,
		Value:     float32(total),
		Tags:      []tags.Tag{hostTag},
	})

	for key, count := range typeStatusCounts {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.jobs.count_by_status",
			Timestamp: ts,
			Value:     float32(count),
			Tags: []tags.Tag{
				hostTag,
				{Key: "job_type", Value: key[0]},
				{Key: "status", Value: key[1]},
			},
		})
	}

	for subsystem, count := range subsystemCounts {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.jobs.count_by_subsystem",
			Timestamp: ts,
			Value:     float32(count),
			Tags: []tags.Tag{
				hostTag,
				{Key: "subsystem", Value: subsystem},
			},
		})
	}

	// --- Top-N details --------------------------------------------------
	//
	// The rows come back already sorted by ELAPSED_CPU_PERCENTAGE
	// DESC (see SQL()), so the first topJobLimit rows are the
	// heaviest CPU consumers. We emit four metrics per top job:
	// cpu percentage, elapsed cpu time, temp storage and disk IO.
	limit := topJobLimit
	if len(res.Rows) < limit {
		limit = len(res.Rows)
	}

	topMetrics := []struct {
		column string
		metric string
	}{
		{"ELAPSED_CPU_PERCENTAGE", "ibmi.job.elapsed_cpu_percent"},
		{"ELAPSED_CPU_TIME", "ibmi.job.elapsed_cpu_ms"},
		{"CPU_TIME", "ibmi.job.cpu_time_ms"},
		{"TEMPORARY_STORAGE", "ibmi.job.temp_storage_mb"},
		{"TOTAL_DISK_IO_COUNT", "ibmi.job.total_disk_io"},
		{"ELAPSED_TOTAL_DISK_IO_COUNT", "ibmi.job.elapsed_total_disk_io"},
		{"ELAPSED_PAGE_FAULT_COUNT", "ibmi.job.elapsed_page_faults"},
		{"THREAD_COUNT", "ibmi.job.thread_count"},
		{"RUN_PRIORITY", "ibmi.job.run_priority"},
	}

	for i := 0; i < limit; i++ {
		row := res.Rows[i]
		jobName := trimmedCell(row, idx, "JOB_NAME")
		jobUser := trimmedCell(row, idx, "JOB_USER")
		subsystem := trimmedCell(row, idx, "SUBSYSTEM")
		if subsystem == "" {
			subsystem = "<system>"
		}
		jobType := trimmedCell(row, idx, "JOB_TYPE")
		status := trimmedCell(row, idx, "JOB_STATUS")

		// FUNCTION and CLIENT_IP_ADDRESS are only populated for a
		// subset of jobs (respectively: jobs currently executing a
		// named program/command, and jobs driven by a remote client
		// like QZDASOINIT / HTTP listeners). Adding them as tags only
		// when non-empty keeps the cardinality bounded on quiet LPARs.
		jobFunction := trimmedCell(row, idx, "FUNCTION")
		clientIP := trimmedCell(row, idx, "CLIENT_IP_ADDRESS")

		jobTags := []tags.Tag{
			hostTag,
			{Key: "job_name", Value: jobName},
			{Key: "job_user", Value: jobUser},
			{Key: "subsystem", Value: subsystem},
			{Key: "job_type", Value: jobType},
			{Key: "status", Value: status},
		}
		if jobFunction != "" {
			jobTags = append(jobTags, tags.Tag{Key: "function", Value: jobFunction})
		}
		if clientIP != "" {
			jobTags = append(jobTags, tags.Tag{Key: "client_ip", Value: clientIP})
		}

		for _, m := range topMetrics {
			v, ok := parseFloatCell(row, idx, m.column)
			if !ok {
				continue
			}
			points = append(points, datapoint.DataPoint{
				Name:      m.metric,
				Timestamp: ts,
				Value:     float32(v),
				Tags:      jobTags,
			})
		}
	}

	// Cap-hit signal: if we retrieved exactly activeJobRowLimit rows
	// the aggregate counts may be undercounts of reality, and the
	// operator should be nudged to bump the SQL cap.
	capHit := float32(0)
	if len(res.Rows) >= activeJobRowLimit {
		capHit = 1
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.jobs.topn_cap_hit",
		Timestamp: ts,
		Value:     capHit,
		Tags:      []tags.Tag{hostTag},
	})

	// Deterministic ordering — makes tests stable.
	sort.SliceStable(points, func(i, j int) bool {
		if points[i].Name != points[j].Name {
			return points[i].Name < points[j].Name
		}
		return tagsString(points[i].Tags) < tagsString(points[j].Tags)
	})
	return points, nil
}

// trimmedCell returns the column value as a trimmed string, or empty
// string if the column is missing or NULL. It's a small convenience
// wrapper around requireCell for the very common case of "give me
// this cell as a string, I don't care about absent-vs-null".
func trimmedCell(row []*string, idx map[string]int, column string) string {
	s, present, err := requireCell(row, idx, column)
	if err != nil || !present {
		return ""
	}
	return strings.TrimSpace(s)
}

// tagsString serialises a tag slice into a stable string for
// deterministic sort ordering. Only used inside Parse, not shipped
// to downstream strategies.
func tagsString(tags []tags.Tag) string {
	var sb strings.Builder
	for _, t := range tags {
		sb.WriteString(t.Key)
		sb.WriteByte('=')
		sb.WriteString(t.Value)
		sb.WriteByte(';')
	}
	return sb.String()
}
