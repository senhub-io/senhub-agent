package veeam

import (
	"math"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// jobStatusValue maps a Veeam session result string to a numeric value
// for the veeam_job_status metric
func jobStatusValue(result string) float32 {
	switch result {
	case "None":
		return 0
	case "Success":
		return 1
	case "Warning":
		return 2
	case "Failed":
		return 3
	default:
		return 0
	}
}

// jobStateValue returns a numeric value for a session state
func jobStateValue(state string) float32 {
	if state == "Working" {
		return 4 // Running
	}
	return jobStatusValue("")
}

// buildJobOverviewMetrics aggregates jobs by type and produces summary metrics
// based on latest session results within the configured time window
func buildJobOverviewMetrics(jobs []job, sessionsByJob map[string][]session, hoursToCheck int, now time.Time) []datapoint.DataPoint {
	cutoff := now.Add(-time.Duration(hoursToCheck) * time.Hour)
	type jobTypeStats struct {
		total   int
		success int
		warning int
		failed  int
		running int
	}

	statsByType := make(map[string]*jobTypeStats)

	for _, j := range jobs {
		if j.IsDisabled {
			continue
		}
		jt := j.Type
		if _, ok := statsByType[jt]; !ok {
			statsByType[jt] = &jobTypeStats{}
		}
		statsByType[jt].total++

		sessions := sessionsByJob[j.ID]
		if len(sessions) > 0 {
			latest := sessions[0]
			// Skip sessions older than the configured time window
			refTime := latest.EndTime
			if refTime.IsZero() {
				refTime = latest.CreationTime
			}
			if !refTime.IsZero() && refTime.Before(cutoff) {
				continue
			}
			// Check if the job is currently running
			if latest.State == "Working" {
				statsByType[jt].running++
			} else {
				switch latest.Result {
				case "Success":
					statsByType[jt].success++
				case "Warning":
					statsByType[jt].warning++
				case "Failed":
					statsByType[jt].failed++
				}
			}
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
		)
	}

	return points
}

// buildJobDetailMetrics produces per-job metrics from the latest session
func buildJobDetailMetrics(jobs []job, sessionsByJob map[string][]session, hoursToCheck int, now time.Time) []datapoint.DataPoint {
	cutoff := now.Add(-time.Duration(hoursToCheck) * time.Hour)
	var points []datapoint.DataPoint

	for _, j := range jobs {
		if j.IsDisabled {
			continue
		}
		jobTags := []tags.Tag{
			{Key: "metric_type", Value: "jobs"},
			{Key: "job_name", Value: j.Name},
			{Key: "job_type", Value: j.Type},
		}

		sessions := sessionsByJob[j.ID]
		if len(sessions) == 0 {
			continue
		}

		latest := sessions[0]

		// Skip sessions older than the configured time window
		sessionRef := latest.EndTime
		if sessionRef.IsZero() {
			sessionRef = latest.CreationTime
		}
		if !sessionRef.IsZero() && sessionRef.Before(cutoff) {
			continue
		}

		// Status
		status := jobStatusValue(latest.Result)
		if latest.State == "Working" {
			status = 4 // Running
		}
		points = append(points, datapoint.DataPoint{
			Name: "veeam_job_status", Timestamp: now, Value: status, Tags: jobTags,
		})

		// Duration in minutes (only if session has ended)
		if !latest.EndTime.IsZero() && latest.EndTime.After(latest.CreationTime) {
			durationMin := latest.EndTime.Sub(latest.CreationTime).Minutes()
			points = append(points, datapoint.DataPoint{
				Name: "veeam_job_duration_min", Timestamp: now, Value: float32(durationMin), Tags: jobTags,
			})
		}

		// Hours since last session
		refTime := latest.EndTime
		if refTime.IsZero() {
			refTime = latest.CreationTime
		}
		hoursSince := now.Sub(refTime).Hours()
		points = append(points, datapoint.DataPoint{
			Name: "veeam_job_hours_since", Timestamp: now, Value: float32(hoursSince), Tags: jobTags,
		})
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

		points = append(points,
			datapoint.DataPoint{Name: "veeam_repo_total_gb", Timestamp: now, Value: float32(r.CapacityGB), Tags: repoTags},
			datapoint.DataPoint{Name: "veeam_repo_used_gb", Timestamp: now, Value: float32(r.UsedSpaceGB), Tags: repoTags},
			datapoint.DataPoint{Name: "veeam_repo_free_gb", Timestamp: now, Value: float32(r.FreeGB), Tags: repoTags},
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

	// License status
	var statusVal float32
	switch lic.Status {
	case "Valid":
		statusVal = 0
	case "Warning":
		statusVal = 1
	case "Expired":
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

		var statusVal float32
		if p.IsDisabled {
			statusVal = 0
			disabledCount++
		} else {
			statusVal = 1
			enabledCount++
		}

		points = append(points,
			datapoint.DataPoint{Name: "veeam_proxy_status", Timestamp: now, Value: statusVal, Tags: proxyTags},
			datapoint.DataPoint{Name: "veeam_proxy_max_tasks", Timestamp: now, Value: float32(p.MaxTaskCount), Tags: proxyTags},
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
