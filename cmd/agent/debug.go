// Debug and status utilities
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/kardianos/service"
	"senhub-agent.go/internal/agent/cliArgs"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
)

func showDebugModules() {
	modules := agentLogger.GetAvailableModules()

	fmt.Printf("Available Debug Modules (%d modules):\n\n", len(modules))

	// Group modules by category
	categories := map[string][]string{
		"Core Services":      {},
		"Data Strategies":    {},
		"System Probes":      {},
		"Application Probes": {},
		"Platform Specific":  {},
		"Sub-modules":        {},
	}

	// Categorize modules
	for _, module := range modules {
		switch {
		case strings.HasPrefix(module, "strategy."):
			categories["Data Strategies"] = append(categories["Data Strategies"], module)
		case module == "pdh.windows":
			categories["Platform Specific"] = append(categories["Platform Specific"], module)
		case module == "probe.redfish.client":
			categories["Sub-modules"] = append(categories["Sub-modules"], module)
		case module == "probe.webapp" || module == "probe.gateway" ||
			module == "probe.syslog" || module == "probe.event" ||
			module == "probe.otel" || module == "probe.redfish":
			categories["Application Probes"] = append(categories["Application Probes"], module)
		case strings.HasPrefix(module, "probe."):
			categories["System Probes"] = append(categories["System Probes"], module)
		default:
			categories["Core Services"] = append(categories["Core Services"], module)
		}
	}

	// Display categorized modules
	categoryOrder := []string{"Core Services", "Data Strategies", "System Probes", "Application Probes", "Platform Specific", "Sub-modules"}

	for _, category := range categoryOrder {
		if len(categories[category]) > 0 {
			fmt.Printf("📂 %s:\n", category)
			for _, module := range categories[category] {
				fmt.Printf("   • %s\n", module)
			}
			fmt.Println()
		}
	}

	fmt.Println("Usage Examples:")
	fmt.Printf("   # Debug specific module:\n")
	fmt.Printf("   %s run --debug-modules strategy.http\n", os.Args[0])
	fmt.Printf("\n   # Debug multiple modules:\n")
	fmt.Printf("   %s run --debug-modules strategy.http,cache,probe.redfish\n", os.Args[0])
	fmt.Printf("\n   # Debug with offline mode:\n")
	fmt.Printf("   %s run --offline --debug-modules strategy.http,cache\n", os.Args[0])
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

	// Try to get detailed status from running agent first (via HTTP)
	agentKey := ""
	if args != nil {
		agentKey = args.AuthenticationKey

		// Try to get agent key from config file if not provided
		if agentKey == "" {
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

	// Try HTTP endpoint first (for running agent with HTTP strategy)
	if agentKey != "" {
		if systemStatus, err := statusHelper.GetDetailedStatusFromHTTP(agentKey, 8080); err == nil {
			// Successfully got status from running agent
			fmt.Print(formatter.FormatSystemStatus(*systemStatus))
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

	// Determine agent mode - use safe detection that doesn't crash
	agentMode := "online" // Default assumption for status checks
	if args != nil {
		if args.Offline {
			// Explicit offline flag takes precedence
			agentMode = "offline"
		} else {
			// Check for config file existence to determine mode
			configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
			if err == nil {
				if _, err := os.Stat(configPath); err == nil {
					// Config file exists - likely offline mode
					agentMode = "offline"
				} else if args.AuthenticationKey != "" {
					// No config but has auth key - online mode
					agentMode = "online"
				}
			}
		}
	}
	statusService.SetAgentMode(agentMode)

	// Note: Without actual probe/cache data, we'll get basic system info
	// In a real deployment, this would connect to the running agent's internal state
	systemStatus := statusService.GetSystemStatus()

	// Try to enhance with agent key if available
	if args != nil {
		agentKey := args.AuthenticationKey

		// Try to get agent key from config file if not provided
		if agentKey == "" {
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
		}
	}

	return systemStatus, nil
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
