package main

import (
	"context"
	"fmt"
	"github.com/kardianos/service"
	"log"
	"os"
	"os/signal"
	"senhub-agent.go/internal/agent"
	agentCliArgs "senhub-agent.go/internal/agent/cliArgs"
	"syscall"
	"time"
)

type program struct {
	agent agent.Agent
	done  chan bool
	args  *agentCliArgs.ParsedArgs // Changed from CliArgs to ParsedArgs
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

func main() {
	// Show help if no arguments or help is requested
	if len(os.Args) <= 1 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		showHelp()
		return
	}

	// Check if we have service-related commands
	command := os.Args[1]
	switch command {
	case "install", "uninstall", "start", "stop", "status", "run":
		// Parse all remaining arguments for the service
		var authKey, serverUrl string
		for i := 2; i < len(os.Args); i++ {
			if os.Args[i] == "--authentication-key" && i+1 < len(os.Args) {
				authKey = os.Args[i+1]
				i++
			} else if os.Args[i] == "--server-url" && i+1 < len(os.Args) {
				serverUrl = os.Args[i+1]
				i++
			}
		}
		handleServiceCommand(command, authKey, serverUrl)
		return
	default:
		// If not a service command, let agent parse args normally
		args := agentCliArgs.MustParse()
		runAgent(args)
	}
}

func handleServiceCommand(command, authKey, serverUrl string) {
	svcConfig := &service.Config{
		Name:        "senhub-agent", // Nom unifié pour Windows et Linux
		DisplayName: "SenHub Agent", // Nom affiché dans les interfaces
		Description: "SenHub Agent Service for monitoring and management",
		Executable:  os.Args[0],
		Arguments:   []string{"--authentication-key", authKey},
		Option: map[string]interface{}{
			"SystemdScript": true,
			"Restart":       "always",
			"User":          "senhub",
			"LogOutput":     true,
			"ServiceName":   "senhub-agent.service", // Pour Linux
		},
	}

	if serverUrl != "" {
		svcConfig.Arguments = append(svcConfig.Arguments, "--server-url", serverUrl)
	}

	prg := &program{
		done: make(chan bool, 1),
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	logger, err := s.Logger(nil)
	if err != nil {
		log.Fatal(err)
	}

	switch command {
	case "install":
		if authKey == "" {
			log.Fatal("Authentication key is required. Use: --authentication-key YOUR_KEY")
		}
		err = s.Install()
		if err == nil {
			logger.Info("Service installed successfully")
		}
	case "uninstall":
		err = s.Uninstall()
		if err == nil {
			logger.Info("Service uninstalled successfully")
		}
	case "start":
		err = s.Start()
		if err == nil {
			logger.Info("Service started successfully")
		}
	case "stop":
		err = s.Stop()
		if err == nil {
			logger.Info("Service stopped successfully")
		}
	case "status":
		status, err := s.Status()
		if err == nil {
			statusText := getServiceStatusText(status)
			fmt.Printf("Service status: %s\n", statusText)
		}
	}

	if err != nil {
		logger.Error(err)
		log.Fatal(err)
	}
}

func runAgent(args *agentCliArgs.ParsedArgs) {
	svcConfig := &service.Config{
		Name:        "SenHubService",
		DisplayName: "SenHub Agent Service",
		Description: "SenHub Agent Service for monitoring and management",
	}

	prg := &program{
		done: make(chan bool, 1),
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	logger, err := s.Logger(nil)
	if err != nil {
		log.Fatal(err)
	}

	if service.Interactive() {
		log.Println("Running in interactive mode")
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		go func() {
			<-ctx.Done()
			logger.Info("Graceful shutdown initiated, press Ctrl+C again to force")
			if err := s.Stop(); err != nil {
				logger.Error("Error stopping service: ", err)
			}
		}()
	}

	logger.Info("Starting service")
	if err := s.Run(); err != nil {
		logger.Error("Error running service: ", err)
		log.Fatal(err)
	}

	<-prg.done
	logger.Info("Graceful shutdown complete")
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
    run         Run in console mode

Agent Options:
    --authentication-key KEY   Authentication key for the service (required)
    --server-url URL          Server URL (optional)
    --verbose                 Enable verbose logging

Examples:
    %s install --authentication-key "your-key"
    %s start
    %s status
    %s --authentication-key "your-key" --server-url "http://example.com"

`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}
