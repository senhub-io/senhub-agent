// Debug and status utilities
package app

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
)

func showDebugModules() {
	exe := os.Args[0]

	fmt.Println("Available debug filters:")
	fmt.Println()
	fmt.Println("  Probes:")
	fmt.Println("    probe                 All probes")
	fmt.Println("    probe.veeam           Veeam Backup & Replication")
	fmt.Println("    probe.citrix          Citrix Virtual Apps & Desktops")
	fmt.Println("    probe.netscaler       Citrix NetScaler / ADC")
	fmt.Println("    probe.redfish         Redfish hardware monitoring")
	fmt.Println("    probe.cpu             CPU usage")
	fmt.Println("    probe.memory          Memory usage")
	fmt.Println("    probe.network         Network interfaces")
	fmt.Println("    probe.logicaldisk     Disk usage")
	fmt.Println("    probe.webapp          Web application monitoring")
	fmt.Println("    probe.loadwebapp      Web application load testing")
	fmt.Println("    probe.gateway         Gateway connectivity")
	fmt.Println("    probe.wifi            WiFi signal strength")
	fmt.Println("    probe.syslog          Syslog collector")
	fmt.Println("    probe.event           Event collector")
	fmt.Println("    probe.otel            OpenTelemetry collector")
	fmt.Println()
	fmt.Println("  Agent:")
	fmt.Println("    sensor                Probe lifecycle management")
	fmt.Println("    configuration         Configuration loading & watching")
	fmt.Println("    strategy              All output strategies (http, prtg, senhub)")
	fmt.Println("    strategy.http         HTTP API & web UI")
	fmt.Println("    transformer           Metric transformation & lookups")
	fmt.Println("    data_store            Data routing")
	fmt.Println("    service.auto_update   Auto-update system")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s run --filter probe.veeam              # one probe\n", exe)
	fmt.Printf("  %s run --filter probe                    # all probes\n", exe)
	fmt.Printf("  %s run --filter probe.veeam,strategy.http # combine filters\n", exe)
	fmt.Println()
}

// showEnhancedStatus displays enhanced status information using the status service
func showEnhancedStatus(svc service.Service, args *cliArgs.ParsedArgs) {
	// Create logger for status operations
	logger := agentLogger.NewLogger(&cliArgs.ParsedArgs{Verbose: false})

	// Create status helper and formatter
	statusHelper := status.NewStatusHelper(logger)
	formatter := status.NewCLIFormatter()

	// Get basic service status
	serviceStatus, err := statusHelper.GetServiceStatus(svc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking service status: %v\n", err)
		return
	}

	// Capitalize first letter for display
	displayStatus := strings.ToUpper(serviceStatus[:1]) + serviceStatus[1:]
	fmt.Printf("Service status: %s\n\n", displayStatus)

	// If service is not running, show basic info only
	if serviceStatus != "running" {
		fmt.Println("Agent service is not running.")
		fmt.Println("Start the service with: " + os.Args[0] + " start")
		return
	}

	// Try to get detailed status from running agent first (via HTTP).
	// The authentication key always comes from the configuration file
	// in 0.2.0+ — the CLI flag was removed with the legacy remote-config loader.
	agentKey := ""
	// Why the running agent could not be asked. Printed before the local
	// view, which otherwise reads as the agent's own answer.
	keyProblem := ""
	reachProblem := ""
	if args != nil {
		// Read agent key from config file
		{
			// Use absolute path based on binary location (fixes Windows Service issue)
			configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
			if err != nil {
				// Fallback to provided path if absolute path resolution fails
				configPath = args.ConfigPath
				if configPath == "" {
					configPath = "./agent-config.yaml"
				}
			}

			if extractedKey, err := extractAgentKeyFromConfig(configPath); err == nil {
				agentKey = extractedKey
			} else {
				keyProblem = fmt.Sprintf("the agent key could not be read from %s (%v)", configPath, err)
			}
		}
	}

	// Resolve config path once for reuse
	configPath := ""
	if args != nil {
		if resolved, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath); err == nil {
			configPath = resolved
		}
	}

	// Try HTTP endpoint first (for running agent with HTTP strategy)
	if agentKey != "" {
		httpPort := resolveHTTPStrategyPort(configPath)
		systemStatus, err := statusHelper.GetDetailedStatusFromHTTP(agentKey, httpPort)
		if err != nil {
			reachProblem = fmt.Sprintf("the running agent did not answer on port %d (%v)", httpPort, err)
		}
		if err == nil {
			// Enrich with dashboard URL from config
			if configPath != "" {
				systemStatus.Connection.DashboardURL = buildDashboardURL(configPath, agentKey)
			}
			// Successfully got status from running agent
			fmt.Print(formatter.FormatSystemStatus(*systemStatus))

			// --otlp adds an OTLP self-metric block after the standard view.
			// Failure here is non-fatal: the standard status already printed.
			if args != nil && args.ShowOTLP {
				if info, err := statusHelper.GetOTLPInfoFromHTTP(agentKey, httpPort); err == nil {
					fmt.Print("\n")
					fmt.Print(formatter.FormatOTLPInfo(info))
				} else {
					fmt.Fprintf(os.Stderr, "\nNote: could not fetch OTLP info (%v)\n", err)
				}
			}
			return
		}
		// HTTP failed, fall back to direct method
		// Note: this happens when the HTTP strategy is not enabled, or the
		// agent is not listening on the resolved port
	}

	// The local view describes this process, not the daemon: it cannot
	// say which outputs are running or whether the configuration is
	// watched. Saying so, and why, is the difference between a degraded
	// answer and a wrong one.
	if notice := daemonUnreachableNotice(keyProblem, reachProblem); notice != "" {
		fmt.Print(notice)
	}

	// Fallback: Get system status directly using StatusService (no HTTP dependency)
	systemStatus, err := getSystemStatusDirect(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Note: Could not get system status (%v), showing minimal status\n\n", err)

		// Minimal fallback status
		basicHealth := status.HealthInfo{
			Status:    "unknown",
			Timestamp: time.Now(),
			Message:   "Service is running but status unavailable",
		}

		basicAgent := status.AgentInfo{
			Version:   "unknown",
			Commit:    "unknown",
			GoVersion: runtime.Version(),
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
		}

		fmt.Print(formatter.FormatBasicStatus(basicHealth, basicAgent))
		return
	}

	// Display full system status
	fmt.Print(formatter.FormatSystemStatus(systemStatus))
}

// getSystemStatusDirect gets system status directly using StatusService (no HTTP dependency)
func getSystemStatusDirect(args *cliArgs.ParsedArgs) (status.SystemStatus, error) {
	// Handle nil args case
	if args == nil {
		args = &cliArgs.ParsedArgs{}
	}

	// Create a completely silent logger for the status service (no output during status command)
	silentArgs := &cliArgs.ParsedArgs{
		Verbose: false,
	}
	logger := agentLogger.NewLogger(silentArgs)

	// Try to get version and commit information
	version := cliArgs.Version
	commit := cliArgs.CommitHash
	if version == "" {
		version = "development"
	}

	// Format commit hash for display (take first 8 chars if longer than 8 and looks like a git hash)
	// Avoid truncating non-hash values like "latest-dev"
	if len(commit) > 8 && isGitHash(commit) {
		commit = commit[:8]
	}

	// Create status service
	statusService := status.NewStatusService(logger, version, commit)

	// Note: Without actual probe/cache data, we'll get basic system info
	// In a real deployment, this would connect to the running agent's internal state
	systemStatus := statusService.GetSystemStatus()

	// Try to enhance with the agent key extracted from the config file.
	if args != nil {
		agentKey := ""

		{
			// Use absolute path based on binary location (fixes Windows Service issue)
			configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
			if err != nil {
				// Fallback to provided path if absolute path resolution fails
				configPath = args.ConfigPath
				if configPath == "" {
					configPath = "./agent-config.yaml"
				}
			}

			if extractedKey, err := extractAgentKeyFromConfig(configPath); err == nil {
				agentKey = extractedKey
			}
		}

		// Update connection info with agent key source
		if agentKey != "" {
			systemStatus.Connection.Source = "Configuration file"
			systemStatus.Connection.Status = "Available"

			// Build dashboard URL from config
			configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
			if err == nil {
				systemStatus.Connection.DashboardURL = buildDashboardURL(configPath, agentKey)
			}
		}
	}

	return systemStatus, nil
}

// buildDashboardURL constructs the dashboard URL from the agent
// configuration, whichever layout it uses.
func buildDashboardURL(configPath string, agentKey string) string {
	if agentKey == "" {
		return ""
	}
	scheme, port := resolveHTTPStrategyEndpoint(configPath)
	return fmt.Sprintf("%s://localhost:%d/web/%s/dashboard", scheme, port, agentKey)
}

// isGitHash checks if a string looks like a git commit hash (hex characters only)
func isGitHash(s string) bool {
	if len(s) < 7 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// validateConfigPath validates that the config path is safe to read

// resolveHTTPStrategyPort finds the port the running agent's HTTP
// strategy listens on, so `status` reaches a daemon configured on
// anything other than the default.
func resolveHTTPStrategyPort(configPath string) int {
	_, port := resolveHTTPStrategyEndpoint(configPath)
	return port
}

// resolveHTTPStrategyEndpoint returns the scheme and port of the HTTP
// strategy as configured on disk, falling back to plain HTTP on 8080
// when the configuration cannot be read.
//
// It goes through the real configuration loader rather than parsing the
// main file by hand: an operator running the multi-file layout keeps the
// http strategy in strategies.d/, invisible to a `strategies:` lookup in
// agent.yaml. Status silently fell back to the degraded local view on
// every such host, which also hid the dead-output report (#826), and
// every printed console address named 8080 whatever the file said.
func resolveHTTPStrategyEndpoint(configPath string) (scheme string, port int) {
	scheme, port = "http", defaultHTTPPort
	if configPath == "" {
		return scheme, port
	}
	cfg, err := configuration.LoadFromDisk(configPath, nil)
	if err != nil {
		return scheme, port
	}
	for _, storage := range cfg.Storage {
		if storage.Name != "http" {
			continue
		}
		switch v := storage.Params["port"].(type) {
		case int:
			if v > 0 {
				port = v
			}
		case float64:
			if v > 0 {
				port = int(v)
			}
		}
		if tlsEnabledParam(storage.Params["tls"]) {
			scheme = "https"
		}
		return scheme, port
	}
	return scheme, port
}

// tlsEnabledParam reads `tls.enabled` from a strategy parameter block.
// The block arrives with string keys from the runtime parser and with
// interface keys straight from the yaml.v2 loader; both are read.
func tlsEnabledParam(v interface{}) bool {
	switch m := v.(type) {
	case map[string]interface{}:
		enabled, _ := m["enabled"].(bool)
		return enabled
	case map[interface{}]interface{}:
		enabled, _ := m["enabled"].(bool)
		return enabled
	}
	return false
}

// daemonUnreachableNotice explains why the local view is about to be
// printed instead of the running agent's own state. Empty when the
// daemon answered. The key problem comes first: it is the one the
// operator can act on, and it is what makes the port unreachable.
func daemonUnreachableNotice(keyProblem, reachProblem string) string {
	why := keyProblem
	if why == "" {
		why = reachProblem
	}
	if why == "" {
		return ""
	}
	return fmt.Sprintf("The running agent could not be asked: %s.\nWhat follows is what this command sees on its own. It is not the service's state: it cannot say which outputs are running, nor whether the configuration is watched.\n\n", why)
}
