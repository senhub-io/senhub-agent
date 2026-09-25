package app

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
)

// reportNagiosFile checks the operator's nagios.yaml the way the agent
// loads it at start: a file it would refuse is an error, a check that can
// only answer UNKNOWN on this platform is a warning. Without it an
// operator learned of a refused file from the log after a restart, while
// the shipped checks were served in its place.
func reportNagiosFile(configPath string, errorCount, warnings int) (int, int) {
	report, found := http.CheckNagiosFile(configPath)
	if !found {
		return errorCount, warnings
	}
	if report.Err != nil {
		fmt.Printf("  [ERROR] Nagios checks %s: %v (the agent would serve the shipped checks instead)\n", report.Path, report.Err)
		return errorCount + 1, warnings
	}
	fmt.Printf("  [OK]   Nagios checks: %s\n", report.Path)
	for _, w := range report.Warnings {
		fmt.Printf("  [WARN] Nagios %s\n", w)
	}
	return errorCount, warnings + len(report.Warnings)
}
