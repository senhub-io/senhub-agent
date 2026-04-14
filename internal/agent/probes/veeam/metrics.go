package veeam

import (
	"math"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// jobResultValue maps a Veeam ESessionResult string to a numeric value
func jobResultValue(result string) float32 {
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

// buildJobStateOverviewMetrics aggregates job states by type and produces summary metrics
func buildJobStateOverviewMetrics(states []jobState, hoursToCheck int, now time.Time) []datapoint.DataPoint {
	cutoff := now.Add(-time.Duration(hoursToCheck) * time.Hour)
	type jobTypeStats struct {
		total   int
		success int
		warning int
		failed  int
		running int
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

		// Skip jobs with no run or last run outside window
		if js.LastRun == nil || js.LastRun.Before(cutoff) {
			if js.Status != "Running" {
				continue
			}
		}

		statsByType[jt].total++
		if js.Status == "Running" {
			statsByType[jt].running++
		} else {
			switch js.LastResult {
			case "Success":
				statsByType[jt].success++
			case "Warning":
				statsByType[jt].warning++
			case "Failed":
				statsByType[jt].failed++
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

// buildJobStateDetailMetrics produces per-job metrics from consolidated job states
func buildJobStateDetailMetrics(states []jobState, hoursToCheck int, now time.Time) []datapoint.DataPoint {
	cutoff := now.Add(-time.Duration(hoursToCheck) * time.Hour)
	var points []datapoint.DataPoint

	for _, js := range states {
		if js.Status == "Disabled" {
			continue
		}
		jobTags := []tags.Tag{
			{Key: "metric_type", Value: "jobs"},
			{Key: "job_name", Value: js.Name},
			{Key: "job_type", Value: js.Type},
		}

		// Skip jobs with no run or last run outside window (unless running)
		if js.LastRun == nil || js.LastRun.Before(cutoff) {
			if js.Status != "Running" {
				continue
			}
		}

		// Status: use Running (4) if actively running, otherwise map LastResult
		status := jobResultValue(js.LastResult)
		if js.Status == "Running" {
			status = 4
		}
		points = append(points, datapoint.DataPoint{
			Name: "veeam_job_status", Timestamp: now, Value: status, Tags: jobTags,
		})

		// Time since last run in seconds
		if js.LastRun != nil {
			secondsSince := now.Sub(*js.LastRun).Seconds()
			points = append(points, datapoint.DataPoint{
				Name: "veeam_job_seconds_since", Timestamp: now, Value: float32(secondsSince), Tags: jobTags,
			})
		}

		// Objects count
		points = append(points, datapoint.DataPoint{
			Name: "veeam_job_objects_count", Timestamp: now, Value: float32(js.ObjectsCount), Tags: jobTags,
		})

		// Session progress metrics (available for running or recently completed jobs)
		if js.SessionProgress != nil {
			sp := js.SessionProgress

			// Bottleneck (as numeric: 0=None, 1=Source, 2=Proxy, 3=Network, 4=Target)
			points = append(points, datapoint.DataPoint{
				Name: "veeam_job_bottleneck", Timestamp: now, Value: bottleneckValue(sp.Bottleneck), Tags: jobTags,
			})

			// Data sizes in bytes
			if sp.ProcessedSize != nil {
				points = append(points, datapoint.DataPoint{
					Name: "veeam_job_processed_bytes", Timestamp: now, Value: float32(*sp.ProcessedSize), Tags: jobTags,
				})
			}
			if sp.ReadSize != nil {
				points = append(points, datapoint.DataPoint{
					Name: "veeam_job_read_bytes", Timestamp: now, Value: float32(*sp.ReadSize), Tags: jobTags,
				})
			}
			if sp.TransferredSize != nil {
				points = append(points, datapoint.DataPoint{
					Name: "veeam_job_transferred_bytes", Timestamp: now, Value: float32(*sp.TransferredSize), Tags: jobTags,
				})
			}
		}
	}

	return points
}

// bottleneckValue maps ESessionBottleneckType to a numeric value
func bottleneckValue(bottleneck string) float32 {
	switch bottleneck {
	case "None", "NotDefined":
		return 0
	case "Source", "SourceProxy", "SourceNetwork", "SourceWan":
		return 1
	case "Proxy":
		return 2
	case "Network", "Throttling":
		return 3
	case "Target", "TargetProxy", "TargetNetwork", "TargetWan", "TargetDisk":
		return 4
	default:
		return 0
	}
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
