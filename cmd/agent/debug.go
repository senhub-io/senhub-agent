// Debug and status utilities
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/kardianos/service"
	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/cliArgs"
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
		fmt.Printf("Error checking service status: %v\n", err)
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
	// in 0.2.0+ — the CLI flag was removed alongside online mode.
	agentKey := ""
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
		if systemStatus, err := statusHelper.GetDetailedStatusFromHTTP(agentKey, 8080); err == nil {
			// Enrich with dashboard URL from config
			if configPath != "" {
				systemStatus.Connection.DashboardURL = buildDashboardURL(configPath, agentKey)
			}
			// Successfully got status from running agent
			fmt.Print(formatter.FormatSystemStatus(*systemStatus))

			// --otlp adds an OTLP self-metric block after the standard view.
			// Failure here is non-fatal: the standard status already printed.
			if args != nil && args.ShowOTLP {
				if info, err := statusHelper.GetOTLPInfoFromHTTP(agentKey, 8080); err == nil {
					fmt.Print("\n")
					fmt.Print(formatter.FormatOTLPInfo(info))
				} else {
					fmt.Printf("\nNote: could not fetch OTLP info (%v)\n", err)
				}
			}
			return
		}
		// HTTP failed, fall back to direct method
		// Note: This happens when HTTP strategy is not enabled or agent is not listening on port 8080
	}

	// Fallback: Get system status directly using StatusService (no HTTP dependency)
	systemStatus, err := getSystemStatusDirect(args)
	if err != nil {
		fmt.Printf("Note: Could not get system status (%v), showing minimal status\n\n", err)

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

	// Offline is the only supported mode in 0.2.0+.
	statusService.SetAgentMode("offline")

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

// buildDashboardURL constructs the dashboard URL from the agent configuration file
func buildDashboardURL(configPath string, agentKey string) string {
	if agentKey == "" {
		return ""
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	// Parse YAML to extract HTTP strategy params
	var config struct {
		Strategies []struct {
			Type   string                 `yaml:"type"`
			Params map[string]interface{} `yaml:"params"`
		} `yaml:"strategies"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return ""
	}

	// Find HTTP strategy and extract port/https settings
	for _, s := range config.Strategies {
		if s.Type != "http" {
			continue
		}

		port := 8080
		scheme := "http"

		if p, ok := s.Params["port"]; ok {
			switch v := p.(type) {
			case int:
				port = v
			case float64:
				port = int(v)
			}
		}

		if https, ok := s.Params["enable_https"]; ok {
			if enabled, ok := https.(bool); ok && enabled {
				scheme = "https"
				if port == 8080 {
					port = 8443
				}
			}
		}

		return fmt.Sprintf("%s://localhost:%d/web/%s/dashboard", scheme, port, agentKey)
	}

	return ""
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
