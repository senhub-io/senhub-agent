// Service command handling - install, uninstall, start, stop, status, update
package app

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/kardianos/service"
	"senhub-agent.go/internal/agent/cliArgs"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

func handleServiceCommand(command string, args *cliArgs.ParsedArgs) {
	// Build the ExecStart arguments for the installed service: pass
	// --config-path with the resolved absolute path so the service
	// finds the file regardless of working directory.
	executablePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting executable path: %v\n", err)
		os.Exit(1)
	}
	workingDir := filepath.Dir(executablePath)

	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting absolute config path: %v\n", err)
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
			"LogOutput":             true,
			"ServiceName":           "senhub-agent.service",
			"SystemdScript":         true,
			"Restart":               "always",
			"RestartSec":            "10",
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

	// On Linux the installer emits the hardened unit shipped in the
	// .deb/.rpm packages (User=senhub, capabilities dropped — #223)
	// instead of an implicit root unit. `install --user root` keeps
	// the legacy root unit for operators whose probes need blanket
	// root (e.g. icmp_check raw sockets without granting CAP_NET_RAW).
	serviceUser := args.ServiceUser
	if serviceUser == "" {
		serviceUser = defaultServiceUser
	}
	if runtime.GOOS == "linux" && serviceUser != rootServiceUser {
		svcConfig.Option["SystemdScript"] = hardenedSystemdScript(serviceUser)

		// Stage the binary where the unprivileged daemon can replace it in
		// place during auto-update (#571): a service-user-owned dir under the
		// StateDirectory, writable + outside ProtectSystem=full. The unit's
		// ExecStart then points there.
		if command == "install" {
			// The dedicated user must exist before installManagedBinary
			// chowns the staged binary to it, and before systemd
			// validates the unit's User= (#575).
			if userErr := ensureServiceUser(serviceUser); userErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", userErr)
				fmt.Fprintln(os.Stderr, "Re-run with '--user root' to install the legacy root service if a dedicated user cannot be created.")
				os.Exit(1)
			}

			// ExecStart MUST point at the staged /var/lib binary, never
			// at the installer's invocation path (which may be /tmp and
			// vanish, leaving systemd with 203/EXEC — #576). A staging
			// failure is fatal: a unit written with the temp path would
			// crash-loop, which is worse than aborting the install.
			managed, err := installManagedBinary(executablePath, serviceUser)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: could not stage the managed binary to %s: %v\n", managedBinaryDir, err)
				fmt.Fprintln(os.Stderr, "The service was NOT installed (a unit pointing at the installer's temp path would fail to start).")
				fmt.Fprintln(os.Stderr, "Re-run with '--user root' to install the legacy root service if the binary cannot be staged.")
				os.Exit(1)
			}
			svcConfig.Executable = managed
			svcConfig.WorkingDirectory = managedBinaryDir
		}
	}

	prg := &program{
		done: make(chan bool, 1),
		args: args,
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case "install":
		// The dedicated user must exist before systemd validates the
		// unit. A creation failure aborts the install: registering a
		// unit whose User= cannot be resolved produces a service that
		// never starts, which is worse than a clear error here.
		if userErr := ensureServiceUser(serviceUser); userErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", userErr)
			fmt.Fprintln(os.Stderr, "Re-run with '--user root' to install the legacy root service if a dedicated user cannot be created.")
			os.Exit(1)
		}
		err = s.Install()
		if err == nil {
			fmt.Println("Service installed successfully")

			// Always generate the local configuration at install time
			if err := generateConfiguration(args); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to generate configuration: %v\n", err)
			} else {
				fmt.Printf("Configuration generated: %s\n", configPath)
				if args.EnableHttps {
					fmt.Printf("HTTPS certificates generated in %s\n", filepath.Join(filepath.Dir(configPath), "certs"))
					fmt.Printf("\nAccess your agent at: https://localhost:%d/web/{agentkey}/dashboard\n", args.HttpsPort)
				} else {
					fmt.Printf("\nAccess your agent at: http://localhost:8080/web/{agentkey}/dashboard\n")
				}
			}

			// The installer runs as root but the daemon does not; the
			// generated 0600 config (and certs/logs) must belong to
			// the service user or the first start fails on a read.
			if chownErr := chownServiceTree(serviceUser, configPath, args.EnableHttps); chownErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to hand install files to user %q: %v\n", serviceUser, chownErr)
				fmt.Printf("Fix manually with: chown %s:%s %s (and the log/certs directories the install created)\n", serviceUser, serviceUser, configPath)
			}

			fmt.Printf("\nYou can now start the service with:\n    %s start\n", os.Args[0])
		}
	case "uninstall":
		// Confirm the destructive cleanup BEFORE touching the running
		// service. Answering "n" (or a non-TTY stdin, which resolves to
		// abort) must leave monitoring exactly as it was — running, config
		// and certs and logs intact. Stopping first and prompting after
		// left the service DOWN on a cancelled uninstall; --yes skips the
		// prompt for unattended removal.
		if !args.Yes {
			fmt.Println("Uninstall will remove the agent configuration file, the certs/ directory, and log files/directories.")
			fmt.Print("Proceed? [y/N] ")
			if !readYesConfirmation() {
				fmt.Println("Uninstall cancelled; nothing was removed.")
				return
			}
		}

		// Confirmed: stop a running service before removing it.
		status, err := s.Status()
		if err == nil && status == service.StatusRunning {
			fmt.Println("Stopping service before uninstall...")
			err = s.Stop()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error stopping service: %v\n", err)
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
		// Heal a pre-0.2.x Windows registration whose command line
		// lacks the `run` subcommand before attempting the start —
		// otherwise the binary prints usage, exits, and the SCM
		// reports a connect timeout (#309).
		migrateLegacyServiceRegistration()
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
			fmt.Fprintf(os.Stderr, "Error checking service status: %v\n", statusErr)
			os.Exit(1)
		}

		fmt.Printf("Current service status: %s\n", getServiceStatusText(status))

		// Stop the service if it's running
		if status == service.StatusRunning {
			fmt.Println("Stopping service...")
			err = s.Stop()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error stopping service: %v\n", err)
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
		migrateLegacyServiceRegistration() // see the "start" case (#309)
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
		// The agent expects to find its YAML configuration on disk; the
		// install path generates a default one if missing, so a
		// missing file here means run was invoked before install.
		//
		// We check the RESOLVED path (configPath), not args.ConfigPath
		// — the latter is empty when the operator runs `agent run`
		// with no --config-path flag, and os.Stat("") always returns
		// ENOENT, which would reject a perfectly valid install at the
		// OS-canonical location.
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: Configuration file not found: %s\n", configPath)
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
		fatalf("%v", err)
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
