package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/kardianos/service"
	"senhub-agent.go/internal/agent"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
)

type program struct {
	agent agent.Agent
	done  chan bool
	args  *cliArgs.ParsedArgs
}

func (p *program) Start(s service.Service) error {
	// Initialize the agent with stored CLI args
	if p.args != nil {
		p.agent = agent.NewAgentWithArgs(p.args)
	} else {
		p.agent = agent.NewAgent()
	}
	go p.run()
	return nil
}

func (p *program) Stop(s service.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.agent.Shutdown(ctx); err != nil {
		log.Printf("Agent forced to shutdown with error: %v", err)
	}
	p.done <- true
	return nil
}

func (p *program) run() {
	if err := p.agent.Start(); err != nil {
		log.Printf("agent error: %s", err)
		return
	}
}

// checkPrivileges verifies if the program is running with the required privileges
func checkPrivileges() error {
	if runtime.GOOS == "darwin" {
		return nil
	}
	if runtime.GOOS == "windows" {
		// Check for administrator privileges on Windows
		_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		if err != nil {
			return fmt.Errorf("this program must be run with administrator privileges. Please right-click and select 'Run as administrator'")
		}
	} else {
		// Check for root privileges on Unix-like systems
		currentUser, err := user.Current()
		if err != nil {
			return fmt.Errorf("unable to determine current user: %v", err)
		}

		if currentUser.Uid != "0" {
			return fmt.Errorf("this program must be run with root privileges. Please use 'sudo' or run as root")
		}
	}
	return nil
}

func main() {
	// Check privileges first
	if err := checkPrivileges(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Show help if no arguments or help is requested
	if len(os.Args) <= 1 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		showHelp()
		return
	}

	// If first argument is a service command
	command := os.Args[1]
	switch command {
	case "debug-modules-list":
		showDebugModules()
		return
	case "update":
		args := cliArgs.MustParse()
		agent.UpdateAgent(args)
		return
	case "install", "uninstall", "start", "stop", "restart", "status", "run":
		// For simple commands without required args, handle directly
		if command == "start" || command == "stop" || command == "restart" || command == "status" || command == "uninstall" {
			handleServiceCommand(command, &cliArgs.ParsedArgs{})
			return
		}

		// For commands requiring args, parse remaining arguments
		serviceArgs := make([]string, 0)
		if len(os.Args) > 2 {
			serviceArgs = os.Args[2:]
		}

		// For install and run commands, we need authentication key OR offline mode
		if (command == "install" || command == "run") && len(serviceArgs) == 0 {
			showHelp()
			return
		}

		// Parse remaining args as start arguments
		os.Args = append([]string{os.Args[0]}, serviceArgs...)
		args := cliArgs.MustParse()
		handleServiceCommand(command, args)
		return
	default:
		// If command is not recognized or no arguments provided, show help
		if len(os.Args) <= 1 {
			showHelp()
			return
		}

		// Try to parse arguments for direct agent execution
		args := cliArgs.MustParse()
		if args == nil {
			showHelp()
			return
		}

		// Auto-detect offline mode if no mode specified but config file exists
		if !args.Offline && args.AuthenticationKey == "" {
			configPath := args.ConfigPath
			if configPath == "" {
				configPath = "./agent-config.yaml"
			}
			if _, err := os.Stat(configPath); err == nil {
				fmt.Printf("📋 Detected offline configuration file: %s\n", configPath)
				fmt.Printf("🔄 Automatically switching to offline mode\n")
				args.Offline = true
				args.ConfigPath = configPath
			}
		}

		runAgent(args)
	}
}

func handleServiceCommand(command string, args *cliArgs.ParsedArgs) {
	// Check for required auth key when installing (unless offline mode)
	if command == "install" && args.AuthenticationKey == "" && !args.Offline {
		fmt.Println("Error: Authentication key is required for installation (or use --offline)")
		fmt.Printf("\nUsage: %s install --authentication-key YOUR_KEY\n", os.Args[0])
		fmt.Printf("    or: %s install --offline\n", os.Args[0])
		os.Exit(1)
	}

	// Build service arguments based on mode
	var serviceArgs []string

	if args.Offline {
		// Offline mode: only add basic offline parameters
		// All other configuration (HTTPS, ports, etc.) will be read from config file
		serviceArgs = append(serviceArgs, "--offline")
		if args.ConfigPath != "" {
			serviceArgs = append(serviceArgs, "--config-path", args.ConfigPath)
		}
	} else {
		// Online mode: add authentication key and server URL
		serviceArgs = append(serviceArgs, "--authentication-key", args.AuthenticationKey)
		if args.ServerUrl != "" {
			serviceArgs = append(serviceArgs, "--server-url", args.ServerUrl)
		}
	}

	// Add common optional arguments
	if args.Verbose {
		serviceArgs = append(serviceArgs, "--verbose")
	}
	if len(args.DebugModules) > 0 {
		serviceArgs = append(serviceArgs, "--debug-modules", strings.Join(args.DebugModules, ","))
	}

	// Get the directory where the agent binary is located
	executablePath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error getting executable path: %v\n", err)
		os.Exit(1)
	}
	workingDir := filepath.Dir(executablePath)

	// Convert config path to absolute if it's relative
	configPath := args.ConfigPath
	if configPath == "" {
		configPath = "./agent-config.yaml"
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(workingDir, configPath)
	}

	// Update service arguments with absolute paths
	if args.Offline {
		// Update the config path argument to absolute path
		for i, arg := range serviceArgs {
			if arg == "--config-path" && i+1 < len(serviceArgs) {
				serviceArgs[i+1] = configPath
				break
			}
		}
	}

	svcConfig := &service.Config{
		Name:             "senhub-agent",
		DisplayName:      "SenHub Agent",
		Description:      "SenHub Agent Service for monitoring and management",
		Executable:       executablePath,
		Arguments:        serviceArgs,
		WorkingDirectory: workingDir,
		Option: map[string]interface{}{
			"LogOutput":             true,
			"User":                  "root",
			"ServiceName":           "senhub-agent.service",
			"SystemdScript":         true,
			"Restart":               "always",
			"RestartSec":            "10",
			"StartLimitIntervalSec": "0",
			"StartLimitBurst":       "0",
			"OnFailure":             "restart",
			"RecoveryActions":       []string{"restart", "restart", "restart", "restart", "none"},
			"RecoveryCallback":      "",
			"ResetPeriod":           86400,
			"RestartDelay":          10000,
			"Actions":               []string{"restart"},
		},
	}

	prg := &program{
		done: make(chan bool, 1),
		args: args,
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case "install":
		err = s.Install()
		if err == nil {
			fmt.Println("Service installed successfully")

			// Generate offline configuration if in offline mode
			if args.Offline {
				if err := generateOfflineConfiguration(args); err != nil {
					fmt.Printf("Warning: Failed to generate offline configuration: %v\n", err)
				} else {
					fmt.Printf("✅ Offline configuration generated: %s\n", args.ConfigPath)
					if args.EnableHttps {
						fmt.Printf("✅ HTTPS certificates generated in ./certs/\n")
						fmt.Printf("\nAccess your agent at: https://localhost:%d/web/{agentkey}/dashboard\n", args.HttpsPort)
					} else {
						fmt.Printf("\nAccess your agent at: http://localhost:8080/web/{agentkey}/dashboard\n")
					}
				}
			}

			fmt.Printf("\nYou can now start the service with:\n    %s start\n", os.Args[0])
		}
	case "uninstall":
		// Try to stop first
		status, err := s.Status()
		if err == nil && status == service.StatusRunning {
			fmt.Println("Stopping service before uninstall...")
			err = s.Stop()
			if err != nil {
				fmt.Printf("Error stopping service: %v\n", err)
				os.Exit(1)
			}
			time.Sleep(2 * time.Second)
		}

		// Uninstall the service
		err = s.Uninstall()
		if err == nil {
			fmt.Println("Service uninstalled successfully")

			// Clean up files and directories
			cleanupFiles(args)
		}
	case "start":
		err = s.Start()
		if err == nil {
			fmt.Println("Service started successfully")
		}
	case "stop":
		err = s.Stop()
		if err == nil {
			fmt.Println("Service stopped successfully")
		}
	case "restart":
		// First check if service is running
		status, statusErr := s.Status()
		if statusErr != nil {
			fmt.Printf("Error checking service status: %v\n", statusErr)
			os.Exit(1)
		}

		fmt.Printf("Current service status: %s\n", getServiceStatusText(status))

		// Stop the service if it's running
		if status == service.StatusRunning {
			fmt.Println("Stopping service...")
			err = s.Stop()
			if err != nil {
				fmt.Printf("Error stopping service: %v\n", err)
				os.Exit(1)
			}

			// Wait and verify the service has stopped
			fmt.Println("Waiting for service to stop...")
			maxWaitTime := 10 * time.Second
			waitInterval := 500 * time.Millisecond
			elapsed := time.Duration(0)

			for elapsed < maxWaitTime {
				time.Sleep(waitInterval)
				elapsed += waitInterval

				currentStatus, _ := s.Status()
				if currentStatus == service.StatusStopped {
					fmt.Println("Service stopped successfully")
					break
				}

				if elapsed >= maxWaitTime {
					fmt.Println("Warning: Service may not have stopped completely, proceeding anyway...")
				}
			}
		} else if status == service.StatusStopped {
			fmt.Println("Service is already stopped")
		}

		// Start the service
		fmt.Println("Starting service...")
		err = s.Start()
		if err == nil {
			fmt.Println("Service restarted successfully")

			// Verify the service started
			time.Sleep(1 * time.Second)
			finalStatus, _ := s.Status()
			fmt.Printf("Final service status: %s\n", getServiceStatusText(finalStatus))
		}
	case "status":
		showEnhancedStatus(s, args)
	case "run":
		// Intelligent mode detection with backward compatibility
		// This function detects the appropriate mode (online/offline) based on:
		// 1. Configuration file content (mode: online/offline)
		// 2. Authentication key availability (CLI vs config file)
		// 3. Legacy fallback for backward compatibility
		args.Offline = agent.DetectAgentMode(args)

		// Check if configuration file exists when running in offline mode
		if args.Offline {
			if _, err := os.Stat(args.ConfigPath); os.IsNotExist(err) {
				fmt.Printf("Error: Configuration file not found: %s\n", args.ConfigPath)
				fmt.Printf("\nIn offline mode, you must first install the agent to generate the configuration:\n")
				fmt.Printf("    %s install --offline\n", os.Args[0])
				fmt.Printf("\nThen you can run the agent:\n")
				fmt.Printf("    %s run --offline\n", os.Args[0])
				os.Exit(1)
			}
		}

		runAgent(args)
		return
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func runAgent(args *cliArgs.ParsedArgs) {
	// Configure logging based on verbose flag
	if args.Verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
		log.Println("Verbose logging enabled")
	}

	// Create logger early for better logging
	appLogger := agentLogger.NewLogger(args)

	svcConfig := &service.Config{
		Name:        "SenHubService",
		DisplayName: "SenHub Agent Service",
		Description: "SenHub Agent Service for monitoring and management",
	}

	prg := &program{
		done: make(chan bool, 1),
		args: args,
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	svcLogger, err := s.Logger(nil)
	if err != nil {
		appLogger.Fatal().Err(err).Msg("Failed to create service logger")
	}

	// Interactive mode (run command or direct execution)
	if service.Interactive() {
		appLogger.Info().Msg("Running in interactive mode")

		// Start agent directly
		if err := prg.Start(s); err != nil {
			appLogger.Error().Err(err).Msg("Failed to start agent")
			os.Exit(1)
		}

		// Setup signal handling
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			appLogger.Info().Str("signal", sig.String()).Msg("Received signal, initiating shutdown")
			if err := prg.Stop(s); err != nil {
				appLogger.Error().Err(err).Msg("Error stopping service")
			}
		}()

		// Wait for completion
		<-prg.done
		appLogger.Info().Msg("Agent stopped")
		return
	}

	// Normal service mode
	if err := svcLogger.Info("Starting service"); err != nil {
		appLogger.Warn().Err(err).Msg("Failed to log service start message")
	}
	if err := s.Run(); err != nil {
		if logErr := svcLogger.Error("Error running service: ", err); logErr != nil {
			appLogger.Warn().Err(logErr).Msg("Failed to log service error")
		}
		appLogger.Fatal().Err(err).Msg("Service failed to run")
	}
}

func getServiceStatusText(status service.Status) string {
	switch status {
	case service.StatusUnknown:
		return "Unknown"
	case service.StatusRunning:
		return "Running"
	case service.StatusStopped:
		return "Stopped"
	default:
		return fmt.Sprintf("Unknown Status (%d)", int(status))
	}
}

func showHelp() {
	fmt.Printf(`Usage: %s [command] [options]

Service Commands:
    install              Install the service (requires --authentication-key OR --offline)
    uninstall            Remove the service
    start                Start the service
    stop                 Stop the service
    restart              Restart the service (stop then start)
    status               Show service status
    version              Show agent version
    run                  Run in console mode (requires --authentication-key OR --offline)
    update               Update the agent to given version (default: latest)
    debug-modules-list   List all available debug modules

Agent Options:
    --authentication-key KEY                Authentication key for the service
    --server-url URL                       Server URL (optional)
    --verbose                              Enable verbose logging (debug level for all key modules)
    --debug-modules module1,module2        Enable debug logging only for specific modules

Offline Mode Options:
    --offline                              Run in offline mode with local configuration
    --config-path PATH                     Path to local configuration file (default: ./agent-config.yaml)

HTTPS/TLS Options (for offline mode):
    --enable-https                         Enable HTTPS for HTTP strategy
    --https-port PORT                      HTTPS port (default: 8443)
    --https-hosts HOST1,HOST2              Hostnames for certificate SAN (default: localhost,127.0.0.1)
    --cert-file PATH                       Path to custom TLS certificate file
    --key-file PATH                        Path to custom TLS private key file
    --min-tls-version VERSION              Minimum TLS version (1.2, 1.3) (default: 1.2)

Debug Log Shipper Options:
    --debug-log-shipper-url URL            URL of remote log collection endpoint
    --debug-log-shipper-tags tags          Custom tags for logs (format: key1=value1,key2=value2)
    --debug-log-shipper-buffer SIZE        Buffer size for logs before sending (default: 100)

Examples:
    Online Mode:
    %s install --authentication-key "your-key"
    %s run --authentication-key "your-key" --server-url "http://example.com"
    %s run --authentication-key "your-key" --verbose --debug-modules strategy.http,cache
    
    Offline Mode:
    %s install --offline
    %s install --offline --enable-https --https-hosts "agent.company.com,192.168.1.100"
    %s install --offline --enable-https --cert-file /path/to/cert.pem --key-file /path/to/key.pem
    %s run --offline --config-path /etc/senhub-agent/config.yaml
    %s run --offline --enable-https --verbose
    
    Service Management:
    %s start
    %s status
    %s update latest

`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}

// generateOfflineConfiguration creates the offline configuration file and certificates
func generateOfflineConfiguration(args *cliArgs.ParsedArgs) error {
	// Import the configuration package
	appLogger := agentLogger.NewLogger(args)

	// Create local configuration instance
	localConfig := configuration.NewLocalConfiguration(args, appLogger)

	// Start the local configuration to trigger creation
	quitChannel := make(chan struct{})
	defer close(quitChannel)

	if err := localConfig.Start(quitChannel); err != nil {
		return fmt.Errorf("failed to create configuration: %w", err)
	}

	return nil
}

// cleanupFiles removes configuration files, logs, and certificates during uninstall
func cleanupFiles(args *cliArgs.ParsedArgs) {
	var filesToRemove []string
	var dirsToRemove []string

	// Configuration file
	configPath := args.ConfigPath
	if configPath == "" {
		configPath = "./agent-config.yaml"
	}
	if _, err := os.Stat(configPath); err == nil {
		filesToRemove = append(filesToRemove, configPath)
	}

	// Certificate directory
	certsDir := "./certs"
	if _, err := os.Stat(certsDir); err == nil {
		dirsToRemove = append(dirsToRemove, certsDir)
	}

	// Log files and directory
	logPaths := []string{
		"/Library/Logs/SenHub",          // macOS
		"/var/log/senhub-agent",         // Linux
		"C:\\ProgramData\\SenHub\\Logs", // Windows
		"./logs",                        // Local logs if any
	}

	for _, logPath := range logPaths {
		if _, err := os.Stat(logPath); err == nil {
			dirsToRemove = append(dirsToRemove, logPath)
		}
	}

	// Remove files
	for _, file := range filesToRemove {
		if err := os.Remove(file); err != nil {
			fmt.Printf("Warning: Could not remove %s: %v\n", file, err)
		} else {
			fmt.Printf("✅ Removed: %s\n", file)
		}
	}

	// Remove directories
	for _, dir := range dirsToRemove {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Printf("Warning: Could not remove directory %s: %v\n", dir, err)
		} else {
			fmt.Printf("✅ Removed directory: %s\n", dir)
		}
	}

	if len(filesToRemove) == 0 && len(dirsToRemove) == 0 {
		fmt.Println("✅ No additional files to clean up")
	} else {
		fmt.Printf("\n🧹 Cleanup completed - removed %d files and %d directories\n",
			len(filesToRemove), len(dirsToRemove))
	}
}

// showDebugModules displays all available debug modules
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
			configPath := args.ConfigPath
			if configPath == "" {
				configPath = "./agent-config.yaml"
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

	// Format commit hash for display (take first 7 chars if longer)
	if len(commit) > 7 {
		commit = commit[:7]
	}

	// Create status service
	statusService := status.NewStatusService(logger, version, commit)

	// Determine agent mode using the same logic as the agent initialization
	agentMode := "online" // Default assumption for status checks
	if args != nil && args.AuthenticationKey != "" {
		// Use the authoritative agent mode detection only if we have some context
		isOffline := agent.DetectAgentMode(args)
		if isOffline {
			agentMode = "offline"
		}
	} else if args != nil && args.Offline {
		agentMode = "offline"
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
			configPath := args.ConfigPath
			if configPath == "" {
				configPath = "./agent-config.yaml"
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

// validateConfigPath validates that the config path is safe to read
func validateConfigPath(configPath string) error {
	// Convert to absolute path for consistent validation
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Only allow .yaml and .yml extensions
	ext := strings.ToLower(filepath.Ext(absPath))
	if ext != ".yaml" && ext != ".yml" {
		return fmt.Errorf("config file must have .yaml or .yml extension, got: %s", ext)
	}

	// Ensure the path doesn't contain directory traversal attempts
	cleanPath := filepath.Clean(absPath)
	if cleanPath != absPath {
		return fmt.Errorf("path contains directory traversal attempts")
	}

	// Only allow config files in current directory or subdirectories (no parent directory access)
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Check if the file is within the working directory or its subdirectories
	relPath, err := filepath.Rel(workingDir, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("config file must be within the current working directory or its subdirectories")
	}

	return nil
}

// extractAgentKeyFromConfig attempts to extract agent key from local config file
func extractAgentKeyFromConfig(configPath string) (string, error) {
	// Validate the config path for security
	if err := validateConfigPath(configPath); err != nil {
		return "", fmt.Errorf("invalid config path: %w", err)
	}

	// This is a simplified version - in practice, we'd properly parse the YAML
	// #nosec G304 - path is validated by validateConfigPath function
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}

	// Simple string search for agent key (not ideal, but functional)
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "key:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(strings.Trim(parts[1], "\""))
				if key != "" {
					return key, nil
				}
			}
		}
	}

	return "", fmt.Errorf("agent key not found in config file")
}
