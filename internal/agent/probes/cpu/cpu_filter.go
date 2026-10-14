package cpu

// shouldCollectCPUCounter decides whether a Windows per-core CPU counter path
// should be collected. _Total and empty instances (global/system counters)
// always pass. For true per-core instances, only processor_time is kept by
// default; per_core_details=true restores the user, privileged, interrupt and
// DPC per-core breakdown, which is everything it collected per core before.
func shouldCollectCPUCounter(metricName, instance string, perCoreDetails bool) bool {
	if instance == "" || instance == "_Total" {
		return true
	}

	switch metricName {
	case "processor_time":
		return true
	case "user_time", "privileged_time", "interrupt_time", "dpc_time":
		return perCoreDetails
	default:
		// The per-core system counters were never collected per core.
		return false
	}
}
