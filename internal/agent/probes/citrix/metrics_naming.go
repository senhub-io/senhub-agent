package citrix

// Simplified metric naming constants for Citrix probe
// Format: {measurement}_{detail} - clear and concise without redundant prefixes/suffixes
// Each metric name clearly indicates what item is being measured

const (
	// Machine Infrastructure Metrics
	MetricMachinesTotal        = "machines_total"
	MetricMachinesRegistered   = "machines_registered"
	MetricMachinesUnregistered = "machines_unregistered"
	MetricMachinesFaulty       = "machines_faulty"
	MetricMachinesMaintenance  = "machines_maintenance"
	MetricMachinesFaultyTotal = "machines_faulty_total"

	// Session Metrics
	MetricSessionsConnected    = "sessions_connected"
	MetricSessionsDisconnected = "sessions_disconnected"

	// Logon Performance Metrics
	MetricLogonDurationAvg1h  = "logon_duration_avg_1h"
	MetricLogonDurationTotal  = "logon_duration_total"
	MetricLogonSessionsOpened = "logon_sessions_opened"

	// Logon Phase Breakdown Metrics (dynamic with phase name)
	// Format: logon_{phase_name}
	MetricLogonPhasePrefix = "logon_"
	MetricLogonPhaseSuffix = ""

	// User Experience Metrics
	MetricUXExcellent = "ux_excellent"
	MetricUXGood      = "ux_good"
	MetricUXFair      = "ux_fair"
	MetricUXPoor      = "ux_poor"

	// Load Index Metrics
	MetricLoadIndexEffective = "load_index_effective"
	MetricLoadIndexCpu       = "load_index_cpu"
	MetricLoadIndexMemory    = "load_index_memory"
	MetricLoadIndexDisk      = "load_index_disk"
	MetricLoadIndexNetwork   = "load_index_network"
	MetricLoadIndexSessions  = "load_index_sessions"
	MetricLoadOverloaded     = "load_overloaded_machines"

	// License Metrics
	MetricLicenseSessionsActive     = "license_sessions_active"
	MetricLicensePeakConcurrent     = "license_peak_concurrent"
	MetricLicenseUniqueUsers        = "license_unique_users"
	MetricLicenseGraceSessionsLeft  = "license_grace_sessions_left"
	MetricLicenseGracePeriodActive  = "license_grace_period_active"
	MetricLicenseGraceHoursLeft     = "license_grace_hours_left"

	// Connection Failure Metrics
	MetricFailuresTotal  = "failures_total"
	MetricFailuresPrefix = "failures_"

	// Site Health Metrics
	MetricSiteHealthScore = "health_score"
)

// GetLogonPhaseMetricName returns the metric name for a logon phase
func GetLogonPhaseMetricName(phaseName string) string {
	return MetricLogonPhasePrefix + phaseName + MetricLogonPhaseSuffix
}

// GetFailureCategoryMetricName returns the metric name for a failure category
func GetFailureCategoryMetricName(categoryName string) string {
	return MetricFailuresPrefix + categoryName
}
