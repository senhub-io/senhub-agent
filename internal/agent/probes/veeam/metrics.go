package veeam

import (
	"math"
	"time"

	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/sanitize"
	"senhub-agent.go/probesdk/tags"
)

// bottleneckMapping maps Veeam ESessionBottleneckType strings to the
// numeric codes our PRTG lookup expects (senhub.veeam.bottleneck.ovl).
// Strings outside this set are NOT silently turned into 0 — the probe
// drops the bottleneck metric for that scrape and logs the unknown name,
// so an API change shows up loudly instead of being masked.
var bottleneckMapping = map[string]float32{
	"None":          0,
	"NotDefined":    0,
	"Source":        1,
	"SourceProxy":   1,
	"SourceNetwork": 1,
	"SourceWan":     1,
	"Proxy":         2,
	"Network":       3,
	"Throttling":    3,
	"Target":        4,
	"TargetProxy":   4,
	"TargetNetwork": 4,
	"TargetWan":     4,
	"TargetDisk":    4,
}

// Job status numeric values exposed via the senhub.veeam.job_status lookup.
// Order matters for the priority rules below (see computeJobStatusValue).
const (
	jobStatusNeverRun float32 = 0 // job exists and is enabled but has never executed
	jobStatusSuccess  float32 = 1
	jobStatusWarning  float32 = 2
	jobStatusFailed   float32 = 3
	jobStatusRunning  float32 = 4
	jobStatusStale    float32 = 5 // last run is older than the operator-configured window
)

// jobResultValue maps a Veeam ESessionResult string to the corresponding
// job-status numeric value. Used when the job has run and we already know
// it's neither Running nor Stale.
func jobResultValue(result string) float32 {
	switch result {
	case "None":
		return jobStatusNeverRun
	case "Success":
		return jobStatusSuccess
	case "Warning":
		return jobStatusWarning
	case "Failed":
		return jobStatusFailed
	default:
		return jobStatusNeverRun
	}
}

// computeJobStatusValue produces the effective status value PRTG sees per job,
// applying these priority rules (highest precedence first):
//
//  1. Running → return Running (4) regardless of past results
//  2. No LastRun at all → NeverRun (0) — job is enabled but never executed
//  3. LastResult=Failed → Failed (3) — failures keep priority even when stale,
//     so an old failure is never hidden behind a "stale" warning
//  4. LastRun older than the operator window → Stale (5)
//  5. Otherwise map LastResult (Success / Warning)
//
// The priority on Failed (rule 3 before rule 4) is the key contract: if a job
// failed and then stopped running entirely, the operator still sees the failure
// — not a "stale" warning that would suggest the only problem is freshness.
func computeJobStatusValue(js jobState, now time.Time, hoursToCheck int) float32 {
	if js.Status == "Running" {
		return jobStatusRunning
	}
	if js.LastRun == nil {
		return jobStatusNeverRun
	}
	if js.LastResult == "Failed" {
		return jobStatusFailed
	}
	cutoff := now.Add(-time.Duration(hoursToCheck) * time.Hour)
	if js.LastRun.Before(cutoff) {
		return jobStatusStale
	}
	return jobResultValue(js.LastResult)
}

// buildJobStateOverviewMetrics aggregates job states by type and produces
// summary metrics. Every enabled job is counted exactly once, classified
// according to the same priority rules as computeJobStatusValue: Running →
// Failed → Stale → (Success/Warning) → NeverRun. The classification follows
// the per-job rule so the overview totals reconcile with the per-job channels.
//
// In particular, total = success + warning + failed + running + stale +
// never_run for every job_type. There is no longer a "hidden bucket" of
// jobs whose last run aged out of the window — those land in `stale` instead
// of disappearing, so operators can spot a backup cadence drift.
func buildJobStateOverviewMetrics(states []jobState, hoursToCheck int, now time.Time) []datapoint.DataPoint {
	type jobTypeStats struct {
		total    int
		success  int
		warning  int
		failed   int
		running  int
		stale    int
		neverRun int
	}

	statsByType := make(map[string]*jobTypeStats)

	for _, js := range states {
		if js.Status == "Disabled" {
			continue
		}
		jt := js.Type
		if _, ok := statsByType[jt]; !ok {
			statsByType[jt] = &jobTypeStats{}
		}

		statsByType[jt].total++
		switch computeJobStatusValue(js, now, hoursToCheck) {
		case jobStatusRunning:
			statsByType[jt].running++
		case jobStatusFailed:
			statsByType[jt].failed++
		case jobStatusStale:
			statsByType[jt].stale++
		case jobStatusSuccess:
			statsByType[jt].success++
		case jobStatusWarning:
			statsByType[jt].warning++
		case jobStatusNeverRun:
			statsByType[jt].neverRun++
		}
	}

	var points []datapoint.DataPoint
	for jt, stats := range statsByType {
		typeTags := []tags.Tag{
			{Key: "metric_type", Value: "overview"},
			{Key: "job_type", Value: jt},
		}
		points = append(points,
			datapoint.DataPoint{Name: "veeam_jobs_total", Timestamp: now, Value: float32(stats.total), Tags: typeTags},
			datapoint.DataPoint{Name: "veeam_jobs_success", Timestamp: now, Value: float32(stats.success), Tags: typeTags},
			datapoint.DataPoint{Name: "veeam_jobs_warning", Timestamp: now, Value: float32(stats.warning), Tags: typeTags},
			datapoint.DataPoint{Name: "veeam_jobs_failed", Timestamp: now, Value: float32(stats.failed), Tags: typeTags},
			datapoint.DataPoint{Name: "veeam_jobs_running", Timestamp: now, Value: float32(stats.running), Tags: typeTags},
			datapoint.DataPoint{Name: "veeam_jobs_stale", Timestamp: now, Value: float32(stats.stale), Tags: typeTags},
			datapoint.DataPoint{Name: "veeam_jobs_never_run", Timestamp: now, Value: float32(stats.neverRun), Tags: typeTags},
		)
	}

	return points
}

// jobSecondsSinceNeverRun is the sentinel emitted for veeam_job_seconds_since
// when a job has never executed. PRTG/Grafana consumers treat a negative
// duration as "no measurement available" — distinct from the legitimate 0s
// (just finished) and from any positive elapsed time.
const jobSecondsSinceNeverRun float32 = -1

// buildJobStateDetailMetrics produces per-job metrics from consolidated job
// states. Every enabled job emits a `veeam_job_status` datapoint regardless of
// how recently it ran — that's the single channel PRTG consumes to colour the
// job in dashboards. The historical bug where a job aged out of
// `hours_to_check` simply disappeared from the metrics is fixed here: the
// status now becomes Stale (5) so the channel keeps emitting and the operator
// sees the cadence problem instead of an empty channel.
//
// Session-progress data (bytes, bottleneck) only emits when the Veeam API
// supplies it — for stale jobs this typically means "no fresh figures", which
// is correct: stale jobs shouldn't backfill old bytes counters as if they were
// current.
func buildJobStateDetailMetrics(states []jobState, hoursToCheck int, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	for _, js := range states {
		if js.Status == "Disabled" {
			continue
		}
		// Two distinct metric_type tags split the per-job data into:
		//   - jobs_status  : the single "Job Status" channel per job. This is
		//                    what the consolidated PRTG sensor consumes — one
		//                    coloured channel per job, no clutter.
		//   - jobs_detail  : everything else (time since last run, bytes,
		//                    bottleneck, object count). Deep-dive material for
		//                    Grafana or a dedicated "Veeam Performance" sensor.
		//
		// Splitting at tag-level (rather than per-metric naming convention)
		// keeps the Sensor Builder UX clean: the operator picks "Job Status"
		// as a single chip and gets exactly six channels for six jobs.
		statusTags := []tags.Tag{
			{Key: "metric_type", Value: "jobs_status"},
			{Key: "job_name", Value: js.Name},
			{Key: "job_type", Value: js.Type},
		}
		detailTags := []tags.Tag{
			{Key: "metric_type", Value: "jobs_detail"},
			{Key: "job_name", Value: js.Name},
			{Key: "job_type", Value: js.Type},
		}

		// Status channel — always emitted, see computeJobStatusValue for
		// the priority rules (Running > NeverRun > Failed > Stale > result).
		points = append(points, datapoint.DataPoint{
			Name:      "veeam_job_status",
			Timestamp: now,
			Value:     computeJobStatusValue(js, now, hoursToCheck),
			Tags:      statusTags,
		})

		// Time since last run in seconds. Always emitted; -1 indicates
		// "never run" so dashboards can distinguish "just finished" (0) from
		// "no data" (-1). sanitize.Duration handles both nil and the Go
		// zero time (0001-01-01) — a non-nil pointer to the zero time was
		// the source of the historical 540 442-day spike observed at a
		// production deployment before this guard existed.
		secondsSince := jobSecondsSinceNeverRun
		if sec, ok := sanitize.Duration(js.LastRun, now); ok {
			secondsSince = sec
		}
		points = append(points, datapoint.DataPoint{
			Name: "veeam_job_seconds_since", Timestamp: now, Value: secondsSince, Tags: detailTags,
		})

		// Objects count is part of the job definition (not the last session),
		// so it's available even for stale or never-run jobs. Clamped to
		// MaxInt32 to keep PRTG happy on jobs with billions of file objects
		// (typically a sign of an API anomaly rather than a real count).
		if objCount, _ := sanitize.CountInt32(int64(js.ObjectsCount)); true {
			points = append(points, datapoint.DataPoint{
				Name: "veeam_job_objects_count", Timestamp: now, Value: objCount, Tags: detailTags,
			})
		}

		// Session progress metrics (available for running or recently completed jobs)
		if js.SessionProgress != nil {
			sp := js.SessionProgress

			// Bottleneck. Unknown strings (API evolution, wire corruption)
			// map to 0 (= None) so the PRTG channel always has a value —
			// an empty channel looks like an agent failure to operators.
			// The probe still surfaces unknown strings via WARN logs (see
			// veeam.go collection wrapper) so an API drift shows up in the
			// log even though the dashboard stays sane.
			val, _ := sanitize.EnumValue(sp.Bottleneck, bottleneckMapping)
			points = append(points, datapoint.DataPoint{
				Name: "veeam_job_bottleneck", Timestamp: now, Value: val, Tags: detailTags,
			})

			// Data sizes in bytes. CountInt32 clamps to MaxInt32 so a
			// runaway uint64 cannot reach the channel as an over-32-bit
			// value that PRTG would refuse with "Valeur hors des limites".
			if sp.ProcessedSize != nil {
				v, _ := sanitize.Bytes(int64(*sp.ProcessedSize))
				points = append(points, datapoint.DataPoint{
					Name: "veeam_job_processed_bytes", Timestamp: now, Value: v, Tags: detailTags,
				})
			}
			if sp.ReadSize != nil {
				v, _ := sanitize.Bytes(int64(*sp.ReadSize))
				points = append(points, datapoint.DataPoint{
					Name: "veeam_job_read_bytes", Timestamp: now, Value: v, Tags: detailTags,
				})
			}
			if sp.TransferredSize != nil {
				v, _ := sanitize.Bytes(int64(*sp.TransferredSize))
				points = append(points, datapoint.DataPoint{
					Name: "veeam_job_transferred_bytes", Timestamp: now, Value: v, Tags: detailTags,
				})
			}
		}
	}

	return points
}

// buildRepositoryMetrics produces capacity metrics for each repository
func buildRepositoryMetrics(repos []repository, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	for _, r := range repos {
		repoTags := []tags.Tag{
			{Key: "metric_type", Value: "repositories"},
			{Key: "repo_name", Value: r.Name},
		}

		// Convert GB to bytes for PRTG native BytesMemory auto-scaling
		const gbToBytes = 1024 * 1024 * 1024
		points = append(points,
			datapoint.DataPoint{Name: "veeam_repo_capacity", Timestamp: now, Value: float32(r.CapacityGB * gbToBytes), Tags: repoTags},
			datapoint.DataPoint{Name: "veeam_repo_used", Timestamp: now, Value: float32(r.UsedSpaceGB * gbToBytes), Tags: repoTags},
			datapoint.DataPoint{Name: "veeam_repo_free", Timestamp: now, Value: float32(r.FreeGB * gbToBytes), Tags: repoTags},
		)

		// Free percentage
		freePct := float32(0)
		if r.CapacityGB > 0 {
			freePct = float32(r.FreeGB / r.CapacityGB * 100)
		}
		points = append(points, datapoint.DataPoint{
			Name: "veeam_repo_free_pct", Timestamp: now, Value: freePct, Tags: repoTags,
		})
	}

	return points
}

// buildLicenseMetrics produces license-related metrics
func buildLicenseMetrics(lic *licenseInfo, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	// License status (EInstalledLicenseStatus: Valid, Expired, Invalid)
	var statusVal float32
	switch lic.Status {
	case "Valid":
		statusVal = 0
	case "Expired":
		statusVal = 1
	case "Invalid":
		statusVal = 2
	default:
		statusVal = 2
	}
	licTags := []tags.Tag{{Key: "metric_type", Value: "license"}}

	points = append(points, datapoint.DataPoint{
		Name: "veeam_license_status", Timestamp: now, Value: statusVal, Tags: licTags,
	})

	// Days left
	daysLeft := lic.ExpirationDate.Sub(now).Hours() / 24
	if daysLeft < 0 {
		daysLeft = 0
	}
	points = append(points, datapoint.DataPoint{
		Name: "veeam_license_days_left", Timestamp: now, Value: float32(math.Floor(daysLeft)), Tags: licTags,
	})

	// Instance counters
	licensed := lic.InstanceLicenseSummary.LicensedInstancesNumber
	used := lic.InstanceLicenseSummary.UsedInstancesNumber
	remaining := licensed - used
	if remaining < 0 {
		remaining = 0
	}

	points = append(points,
		datapoint.DataPoint{Name: "veeam_license_instances_total", Timestamp: now, Value: float32(licensed), Tags: licTags},
		datapoint.DataPoint{Name: "veeam_license_instances_used", Timestamp: now, Value: float32(used), Tags: licTags},
		datapoint.DataPoint{Name: "veeam_license_instances_remaining", Timestamp: now, Value: float32(remaining), Tags: licTags},
	)

	return points
}

// buildProxyMetrics produces proxy status and aggregate metrics
func buildProxyMetrics(proxies []proxy, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	totalProxies := 0
	enabledCount := 0
	disabledCount := 0

	for _, p := range proxies {
		totalProxies++
		proxyTags := []tags.Tag{
			{Key: "metric_type", Value: "proxies"},
			{Key: "proxy_name", Value: p.Name},
		}

		// status: 0=disabled, 1=enabled+offline, 2=enabled+online
		var statusVal float32
		if p.IsDisabled {
			statusVal = 0
			disabledCount++
		} else {
			enabledCount++
			if p.IsOnline {
				statusVal = 2
			} else {
				statusVal = 1
			}
		}

		points = append(points,
			datapoint.DataPoint{Name: "veeam_proxy_status", Timestamp: now, Value: statusVal, Tags: proxyTags},
		)
	}

	// Aggregate proxy metrics
	proxyAggTags := []tags.Tag{{Key: "metric_type", Value: "proxies"}}
	points = append(points,
		datapoint.DataPoint{Name: "veeam_proxies_total", Timestamp: now, Value: float32(totalProxies), Tags: proxyAggTags},
		datapoint.DataPoint{Name: "veeam_proxies_enabled", Timestamp: now, Value: float32(enabledCount), Tags: proxyAggTags},
		datapoint.DataPoint{Name: "veeam_proxies_disabled", Timestamp: now, Value: float32(disabledCount), Tags: proxyAggTags},
	)

	return points
}

// buildBackupObjectMetrics produces metrics for protected backup objects
func buildBackupObjectMetrics(objects []backupObject, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	totalObjects := 0
	failedObjects := 0

	for _, obj := range objects {
		totalObjects++
		objTags := []tags.Tag{
			{Key: "metric_type", Value: "protected_objects"},
			{Key: "object_name", Value: obj.Name},
			{Key: "object_type", Value: obj.Type},
			{Key: "platform", Value: obj.PlatformName},
		}

		points = append(points, datapoint.DataPoint{
			Name: "veeam_object_restore_points", Timestamp: now, Value: float32(obj.RestorePointsCount), Tags: objTags,
		})

		var failedVal float32
		if obj.LastRunFailed {
			failedVal = 1
			failedObjects++
		}
		points = append(points, datapoint.DataPoint{
			Name: "veeam_object_last_run_failed", Timestamp: now, Value: failedVal, Tags: objTags,
		})
	}

	// Aggregates
	aggTags := []tags.Tag{{Key: "metric_type", Value: "protected_objects"}}
	points = append(points,
		datapoint.DataPoint{Name: "veeam_objects_total", Timestamp: now, Value: float32(totalObjects), Tags: aggTags},
		datapoint.DataPoint{Name: "veeam_objects_failed", Timestamp: now, Value: float32(failedObjects), Tags: aggTags},
	)

	return points
}

// buildManagedServerMetrics produces infrastructure server status metrics
func buildManagedServerMetrics(servers []managedServer, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	totalServers := 0
	availableCount := 0
	unavailableCount := 0

	for _, srv := range servers {
		totalServers++
		srvTags := []tags.Tag{
			{Key: "metric_type", Value: "infrastructure"},
			{Key: "server_name", Value: srv.Name},
			{Key: "server_type", Value: srv.Type},
		}

		var statusVal float32
		if srv.Status == "Available" {
			statusVal = 1
			availableCount++
		} else {
			statusVal = 0
			unavailableCount++
		}

		points = append(points, datapoint.DataPoint{
			Name: "veeam_server_status", Timestamp: now, Value: statusVal, Tags: srvTags,
		})
	}

	// Aggregates
	aggTags := []tags.Tag{{Key: "metric_type", Value: "infrastructure"}}
	points = append(points,
		datapoint.DataPoint{Name: "veeam_servers_total", Timestamp: now, Value: float32(totalServers), Tags: aggTags},
		datapoint.DataPoint{Name: "veeam_servers_available", Timestamp: now, Value: float32(availableCount), Tags: aggTags},
		datapoint.DataPoint{Name: "veeam_servers_unavailable", Timestamp: now, Value: float32(unavailableCount), Tags: aggTags},
	)

	return points
}
