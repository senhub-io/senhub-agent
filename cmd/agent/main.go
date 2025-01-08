package main

import (
	"context"
	"fmt"
	"github.com/kardianos/service"
	"log"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"senhub-agent.go/internal/agent"
	agentCliArgs "senhub-agent.go/internal/agent/cliArgs"
	"syscall"
	"time"
)

type program struct {
	agent agent.Agent
	done  chan bool
	args  *agentCliArgs.ParsedArgs
}

func (p *program) Start(s service.Service) error {
	// Initialize the agent with stored CLI args
	p.agent = agent.NewAgent() // This should use the args stored in your agent package
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
	case "install", "uninstall", "start", "stop", "status", "run":
		// For simple commands without required args, handle directly
		if command == "start" || command == "stop" || command == "status" || command == "uninstall" {
			handleServiceCommand(command, &agentCliArgs.ParsedArgs{})
			return
		}

		// For commands requiring args, parse remaining arguments
		serviceArgs := make([]string, 0)
		if len(os.Args) > 2 {
			serviceArgs = os.Args[2:]
		}

		// For install and run commands, we need authentication key
		if (command == "install" || command == "run") && len(serviceArgs) == 0 {
			showHelp()
			return
		}

		// Parse remaining args as start arguments
		os.Args = append([]string{os.Args[0]}, serviceArgs...)
		args := agentCliArgs.MustParse()
		handleServiceCommand(command, args)
		return
	default:
		// If command is not recognized or no arguments provided, show help
		if len(os.Args) <= 1 {
			showHelp()
			return
		}

		// Try to parse arguments for direct agent execution
		args := agentCliArgs.MustParse()
		if args == nil {
			showHelp()
			return
		}
		runAgent(args)
	}
}

func handleServiceCommand(command string, args *agentCliArgs.ParsedArgs) {
	// Check for required auth key when installing
	if command == "install" && args.AuthenticationKey == "" {
		fmt.Println("Error: Authentication key is required for installation")
		fmt.Printf("\nUsage: %s install --authentication-key YOUR_KEY\n", os.Args[0])
		os.Exit(1)
	}

	svcConfig := &service.Config{
		Name:        "senhub-agent",
		DisplayName: "SenHub Agent",
		Description: "SenHub Agent Service for monitoring and management",
		Executable:  os.Args[0],
		Arguments:   []string{"--authentication-key", args.AuthenticationKey},
		Option: map[string]interface{}{
			"SystemdScript": true,
			"Restart":       "always",
			"User":          "senhub",
			"LogOutput":     true,
			"ServiceName":   "senhub-agent.service",
		},
	}

	// Add optional arguments to service config
	if args.ServerUrl != "" {
		svcConfig.Arguments = append(svcConfig.Arguments, "--server-url", args.ServerUrl)
	}
	if args.Verbose {
		svcConfig.Arguments = append(svcConfig.Arguments, "--verbose")
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
		err = s.Uninstall()
		if err == nil {
			fmt.Println("Service uninstalled successfully")
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
	case "status":
		status, err := s.Status()
		if err == nil {
			fmt.Printf("Service status: %s\n", getServiceStatusText(status))
		}
	case "run":
		runAgent(args)
		return
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func runAgent(args *agentCliArgs.ParsedArgs) {
	// Configure logging based on verbose flag
	if args.Verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
		log.Println("Verbose logging enabled")
	}

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

	logger, err := s.Logger(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Interactive mode (run command or direct execution)
	if service.Interactive() {
		log.Println("Running in interactive mode")

		// Start agent directly
		if err := prg.Start(s); err != nil {
			logger.Error("Failed to start agent: ", err)
			log.Fatal(err)
		}

		// Setup signal handling
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			log.Printf("Received signal %v, initiating shutdown...", sig)
			if err := prg.Stop(s); err != nil {
				logger.Error("Error stopping service: ", err)
			}
		}()

		// Wait for completion
		<-prg.done
		log.Println("Agent stopped")
		return
	}

	// Normal service mode
	logger.Info("Starting service")
	if err := s.Run(); err != nil {
		logger.Error("Error running service: ", err)
		log.Fatal(err)
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
    install     Install the service (requires --authentication-key)
    uninstall   Remove the service
    start       Start the service
    stop        Stop the service
    status      Show service status
    run         Run in console mode (requires --authentication-key)

Agent Options:
    --authentication-key KEY   Authentication key for the service (required)
    --server-url URL          Server URL (optional)
    --verbose                 Enable verbose logging

Examples:
    %s install --authentication-key "your-key"
    %s start
    %s status
    %s run --authentication-key "your-key" --server-url "http://example.com"

`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}
