package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/user"
	"runtime"
	"time"

	"github.com/kardianos/service"
	"senhub-agent.go/internal/agent"
	"senhub-agent.go/internal/agent/cliArgs"
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
	case "license":
		handleLicenseCommand()
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

		// For install command without args, show help
		if command == "install" && len(serviceArgs) == 0 {
			showHelp()
			return
		}

		// Parse remaining args as start arguments
		os.Args = append([]string{os.Args[0]}, serviceArgs...)
		args := cliArgs.MustParse()

		// If no authentication key provided, try to load from config file
		if args.AuthenticationKey == "" && !args.Offline {
			// Use absolute path based on binary location (fixes Windows Service issue)
			configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
			if err != nil {
				// Fallback to provided path if absolute path resolution fails
				configPath = args.ConfigPath
				if configPath == "" {
					configPath = "./agent-config.yaml"
				}
			}
			if key, err := extractAgentKeyFromConfig(configPath); err == nil {
				args.AuthenticationKey = key
				fmt.Printf("✅ Authentication key loaded from %s\n", configPath)
			}
		}

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

		// If no authentication key provided, try to load from config file
		if args.AuthenticationKey == "" && !args.Offline {
			// Use absolute path based on binary location (fixes Windows Service issue)
			configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
			if err != nil {
				// Fallback to provided path if absolute path resolution fails
				configPath = args.ConfigPath
				if configPath == "" {
					configPath = "./agent-config.yaml"
				}
			}

			// Try to extract authentication key from config
			if key, err := extractAgentKeyFromConfig(configPath); err == nil {
				args.AuthenticationKey = key
				fmt.Printf("✅ Authentication key loaded from %s\n", configPath)
			} else if _, statErr := os.Stat(configPath); statErr == nil {
				// Config file exists but no key found - might be offline mode
				fmt.Printf("📋 Detected offline configuration file: %s\n", configPath)
				fmt.Printf("🔄 Automatically switching to offline mode\n")
				args.Offline = true
				args.ConfigPath = configPath
			}
		}

		runAgent(args)
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
                                           (optional if present in config file)
    --server-url URL                       Server URL (optional)
    --config-path PATH                     Path to configuration file (default: ./agent-config.yaml)
    --verbose                              Enable verbose logging (debug level for all key modules)
    --debug-modules module1,module2        Enable debug logging only for specific modules

Offline Mode Options:
    --offline                              Run in offline mode with local configuration

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

    Online Mode (with config file):
    %s run                                          # Auth key loaded from agent-config.yaml
    %s run --config-path /etc/agent/config.yaml    # Auth key loaded from custom path

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

`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}
