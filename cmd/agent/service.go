// Service command handling - install, uninstall, start, stop, status, update
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kardianos/service"
	"senhub-agent.go/internal/agent"
	"senhub-agent.go/internal/agent/cliArgs"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

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
