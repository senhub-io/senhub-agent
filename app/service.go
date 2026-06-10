// Service command handling - install, uninstall, start, stop, status, update
package app

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
	"senhub-agent.go/internal/agent/cliArgs"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

func handleServiceCommand(command string, args *cliArgs.ParsedArgs) {
	// Build the ExecStart arguments for the installed service.
	// Offline is the only supported mode (0.2.0+); we always pass
	// --config-path with the resolved absolute path so the service
	// finds the file regardless of working directory.
	executablePath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error getting executable path: %v\n", err)
		os.Exit(1)
	}
	workingDir := filepath.Dir(executablePath)

	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fmt.Printf("Error getting absolute config path: %v\n", err)
		if args.ConfigPath == "" {
			configPath = filepath.Join(workingDir, "agent-config.yaml")
		} else if !filepath.IsAbs(args.ConfigPath) {
			configPath = filepath.Join(workingDir, args.ConfigPath)
		} else {
			configPath = args.ConfigPath
		}
	}

	serviceArgs := []string{"run", "--config-path", configPath}

	if args.Verbose {
		serviceArgs = append(serviceArgs, "--verbose")
	}
	if len(args.DebugModules) > 0 {
		serviceArgs = append(serviceArgs, "--debug-modules", strings.Join(args.DebugModules, ","))
	}

	svcConfig := &service.Config{
		Name:             "senhub-agent",
		DisplayName:      "SenHub Agent",
		Description:      "SenHub Agent Service for monitoring and management",
		Executable:       executablePath,
		Arguments:        serviceArgs,
		WorkingDirectory: workingDir,
		Option: map[string]interface{}{
			"LogOutput":     true,
			"User":          "root",
			"ServiceName":   "senhub-agent.service",
			"SystemdScript": true,
			"Restart":       "always",
			"RestartSec":    "10",
			// Bounded restart storm: 5 failures within 5 minutes stop
			// the unit instead of looping every 10s forever. Paired
			// with Start() now exiting non-zero on boot failure, a
			// permanent misconfiguration surfaces as a FAILED unit
			// rather than an infinite silent crash loop (#265).
			"StartLimitIntervalSec": "300",
			"StartLimitBurst":       "5",
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

			// Always generate the local configuration at install time
			// (offline is the only mode in 0.2.0+).
			if err := generateOfflineConfiguration(args); err != nil {
				fmt.Printf("Warning: Failed to generate configuration: %v\n", err)
			} else {
				fmt.Printf("✅ Configuration generated: %s\n", configPath)
				if args.EnableHttps {
					fmt.Printf("✅ HTTPS certificates generated in ./certs/\n")
					fmt.Printf("\nAccess your agent at: https://localhost:%d/web/{agentkey}/dashboard\n", args.HttpsPort)
				} else {
					fmt.Printf("\nAccess your agent at: http://localhost:8080/web/{agentkey}/dashboard\n")
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
		// Offline is the only supported mode (0.2.0+). The agent
		// expects to find its YAML configuration on disk; the
		// install path generates a default one if missing, so a
		// missing file here means run was invoked before install.
		//
		// We check the RESOLVED path (configPath), not args.ConfigPath
		// — the latter is empty when the operator runs `agent run`
		// with no --config-path flag, and os.Stat("") always returns
		// ENOENT, which would reject a perfectly valid install at the
		// OS-canonical location.
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Printf("Error: Configuration file not found: %s\n", configPath)
			fmt.Printf("\nInstall the agent first to generate the configuration:\n")
			fmt.Printf("    %s install\n", os.Args[0])
			fmt.Printf("\nThen you can run the agent:\n")
			fmt.Printf("    %s run\n", os.Args[0])
			os.Exit(1)
		}

		// Pin the resolved absolute path so every downstream consumer
		// (logger, LocalConfiguration) sees the same file the
		// existence check above validated.
		args.ConfigPath = configPath
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
