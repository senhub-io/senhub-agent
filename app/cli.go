package app

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

// linuxCommandNeedsRoot reports whether a subcommand genuinely needs
// root on Linux. Only the service-lifecycle commands do: they register
// or control a systemd unit and own the on-disk install. The
// long-running daemon (`run`) does NOT — it relies on filesystem
// ownership of its config/state/log paths (set up at install time as
// the dedicated `senhub` user) and on targeted capabilities for the
// few probes that need one, so it runs least-privilege as an
// unprivileged user. See issue #223 and the hardened systemd unit
// (packaging/systemd/senhub-agent.service, User=senhub).
func linuxCommandNeedsRoot(command string) bool {
	switch command {
	case "install", "uninstall", "start", "stop", "restart":
		return true
	default:
		return false
	}
}

// checkPrivileges verifies the program has the privileges its
// subcommand requires.
//
//   - darwin: never gated.
//   - windows: service-affecting commands need administrator. (The
//     daemon path is unchanged here; #223 scopes the non-root work to
//     Linux.)
//   - linux: only service-lifecycle commands need root; `run` and the
//     rest run unprivileged, gaining access through path ownership and
//     per-probe capabilities rather than blanket root.
func checkPrivileges(command string) error {
	if runtime.GOOS == "darwin" {
		return nil
	}
	if runtime.GOOS == "windows" {
		// Check for administrator privileges on Windows
		_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		if err != nil {
			return fmt.Errorf("this program must be run with administrator privileges. Please right-click and select 'Run as administrator'")
		}
		return nil
	}

	// Linux: relax the blanket root requirement to the commands that
	// actually need it.
	if !linuxCommandNeedsRoot(command) {
		return nil
	}

	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("unable to determine current user: %v", err)
	}
	if currentUser.Uid != "0" {
		return fmt.Errorf("the %q command manages the system service and must be run with root privileges. Please use 'sudo' or run as root", command)
	}
	return nil
}

// hasArg reports whether one of the os.Args (skipping argv[0]) is
// exactly `name`. Used for the small set of view-flags that the
// simple-command code path (status, etc.) recognises before the
// service handler dispatches.
func hasArg(name string) bool {
	for _, a := range os.Args[1:] {
		if a == name {
			return true
		}
	}
	return false
}

// readOnlyCommand reports whether the invocation is a diagnostic /
// inspection subcommand that should bypass the privilege check.
//
// These commands only read state (config files, embedded version
// string, license JWT) and emit to stdout — they don't touch the
// service, the network, the log directory, or the binary itself, so
// running them as a non-root user must not fail. Without this
// exemption, every CI smoke test, every contributor checking
// `agent version` from a clone, and every operator running
// `agent config check` before sudo'ing into the install would hit
// the privilege gate.
func readOnlyCommand(args []string) bool {
	if len(args) <= 1 {
		return true // showHelp() — informational only
	}
	switch args[1] {
	case "--help", "-h", "help", "--version", "version", "debug-modules-list":
		return true
	case "config":
		if len(args) > 2 && (args[2] == "check" || args[2] == "show") {
			return true
		}
	}
	if c, ok := extraCommands[args[1]]; ok {
		return c.ReadOnly
	}
	return false
}

// knownTopLevelArgs is the closed set of subcommands and flags the
// agent accepts as os.Args[1]. Anything outside this set is treated
// as a user error rather than silently falling through to the
// default `run` path — that fallthrough was the failure mode in
// issue #134 where `senhub-agent --version` quietly spawned a
// second agent process.
var knownTopLevelArgs = map[string]struct{}{
	"--help": {}, "-h": {}, "help": {},
	"--version": {}, "version": {},
	"debug-modules-list": {},
	"config":             {},
	"license":            {},
	"db-monitoring":      {},
	"update":             {},
	"install":            {}, "uninstall": {},
	"start": {}, "stop": {}, "restart": {},
	"status": {}, "run": {},
}

func Main() {
	// `--version` short-circuit: print version + exit, BEFORE any
	// subcommand dispatch or privilege gate. The pre-0.2.x agent had
	// no such handling — `senhub-agent --version` fell through to
	// `run` and silently spawned a second agent, surfacing as a
	// "bind: address already in use" only after the second process
	// raced the systemd-managed one for the listener (issue #134).
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		cliArgs.PrintVersion()
		return
	}

	// Show help if no arguments or help is requested. Help is
	// information-only so it runs even without root — placed before
	// checkPrivileges so a fresh checkout / CI smoke test sees usage
	// without hitting the gate.
	if len(os.Args) <= 1 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		showHelp()
		return
	}

	// Reject unknown top-level args. Pre-0.2.x the parser silently
	// fell through to `run`, which is how `--version` (and any
	// typo'd flag) ended up spawning an agent. We now exit non-zero
	// with a clear error so a wrong invocation can never
	// accidentally start a second monitoring process.
	_, known := knownTopLevelArgs[os.Args[1]]
	_, registered := extraCommands[os.Args[1]]
	if !known && !registered {
		fmt.Fprintf(os.Stderr, "Error: unknown command or flag %q\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "Run with --help for usage information.")
		os.Exit(2)
	}

	// If first argument is a service command
	command := os.Args[1]

	// Privilege gate runs before the subcommand dispatch, EXCEPT for
	// diagnostic commands enumerated in readOnlyCommand. On Linux only
	// service-lifecycle commands now require root; the daemon (`run`)
	// runs least-privilege as the dedicated `senhub` user, reaching its
	// config/state/log paths through ownership and any probe-specific
	// capability rather than blanket root (issue #223). On Windows
	// service-affecting commands still require administrator.
	// Diagnostic subcommands (version, config check, config show) don't
	// touch the service or its paths, so they stay ungated for usability
	// (CI smoke checks, contributor onboarding).
	if !readOnlyCommand(os.Args) {
		if err := checkPrivileges(command); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	}

	// Registered (out-of-core) subcommands take precedence over the
	// built-in switch. The enterprise build wires its `ibmi` command
	// here; the core dispatch stays unaware of it.
	if cmd, ok := extraCommands[command]; ok {
		cmd.Run()
		return
	}

	switch command {
	case "debug-modules-list":
		showDebugModules()
		return
	case "config":
		if len(os.Args) > 2 && os.Args[2] == "check" {
			configPath := ""
			if len(os.Args) > 3 {
				configPath = os.Args[3]
			}
			if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
				configPath = resolved
			}
			checkConfig(configPath)
			return
		}
		if len(os.Args) > 2 && os.Args[2] == "show" {
			// agent config show [--raw|--resolved|--redact] [path]
			showConfig(os.Args[3:])
			return
		}
		if len(os.Args) > 2 && os.Args[2] == "migrate" {
			// agent config migrate [path]
			// Convert a legacy monolithic agent-config.yaml into the
			// 0.2.x+ multi-file layout (agent.yaml + probes.d/ +
			// strategies.d/), with a timestamped backup of the
			// original. Idempotent: if the file is already multi-file
			// (no top-level probes:/storage: blocks), the command
			// reports "nothing to do" and exits 0.
			configPath := ""
			if len(os.Args) > 3 {
				configPath = os.Args[3]
			}
			if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
				configPath = resolved
			}
			migrateConfig(configPath)
			return
		}
		showHelp()
		return
	case "license":
		handleLicenseCommand()
		return
	case "db-monitoring":
		handleDbMonitoringCommand()
		return
	case "update":
		// Parse update sub-arguments: update [--list | <version>]
		args := &cliArgs.ParsedArgs{
			Version:    cliArgs.Version,
			CommitHash: cliArgs.CommitHash,
		}
		if len(os.Args) > 2 {
			subArg := os.Args[2]
			if subArg == "--list" || subArg == "-l" {
				args.WantedVersion = "list"
			} else {
				args.WantedVersion = subArg
			}
		}
		agent.UpdateAgent(args)
		return
	case "install", "uninstall", "start", "stop", "restart", "status", "run":
		// Commands that take no positional args: dispatched directly.
		// `status` carries the optional --otlp view flag.
		if command == "start" || command == "stop" || command == "restart" || command == "status" || command == "uninstall" {
			args := &cliArgs.ParsedArgs{}
			if command == "status" {
				args.ShowOTLP = hasArg("--otlp")
			}
			handleServiceCommand(command, args)
			return
		}

		// `install` and `run` accept optional flags (--config-path,
		// --enable-https, …). In 0.2.0+ they also work with no args
		// — install auto-generates a UUID agent key, run uses the
		// OS-canonical default config path. Pre-0.2.0 the install
		// path forced the user to choose between --offline and
		// --authentication-key; that gate is gone.
		//
		// We parse the remaining args via `start`-subcommand parsing
		// rather than calling MustParse() directly, because the
		// top-level parser expects an explicit subcommand and would
		// fail on the empty input that an arg-less `install` / `run`
		// produces.
		serviceArgs := []string{}
		if len(os.Args) > 2 {
			serviceArgs = os.Args[2:]
		}
		args := cliArgs.ParseStartArgs(serviceArgs)
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

		runAgent(args)
	}
}

func showHelp() {
	exe := os.Args[0]

	// Try to show the console URL if config is available
	consoleURL := ""
	configPath, err := cliArgs.GetAbsoluteConfigPath("")
	if err == nil {
		if key, err := extractAgentKeyFromConfig(configPath); err == nil && key != "" {
			consoleURL = fmt.Sprintf("http://127.0.0.1:8080/web/%s/dashboard", key)
		}
	}

	fmt.Println("SenHub Agent - Infrastructure Monitoring Agent")
	fmt.Printf("Version: %s (%s)\n", cliArgs.Version, cliArgs.CommitHash)
	if consoleURL != "" {
		fmt.Printf("Console: %s\n", consoleURL)
	}
	fmt.Println()

	fmt.Printf(`Usage: %s [command] [options]

Service Commands:
    install              Install as system service (auto-generates a UUID agent key)
    uninstall            Remove the system service
    start                Start the service
    stop                 Stop the service
    restart              Restart the service
    status               Show service and probe status
    status --otlp        Also show OTLP pipeline self-metrics
    run                  Run interactively in console mode

License Commands:
    license show         Show current license information
    license activate     Activate a license from a JWT token
    license remove       Remove current license (revert to free tier)

Other Commands:
    version              Show agent version
    update               Check for new versions
    update --list        List all available versions (stable + beta)
    update <version>     Install a specific version
    config check [path]   Validate configuration (covers fragments under
                          probes.d/ and strategies.d/ if present)
    config show [opts]    Print merged + resolved configuration as YAML
                            --resolved            env/file references substituted, secrets in cleartext
                            --raw                 references preserved as written
                            --redact              substituted but secrets masked (default)
                            [path]                config file path
    config migrate [path] Convert a legacy monolithic agent-config.yaml
                          into the 0.2.x multi-file layout
                          (agent.yaml + probes.d/ + strategies.d/) with
                          a timestamped backup. Idempotent.
    debug-modules-list    List available debug log modules

Agent Options:
    --config-path PATH                     Path to the agent configuration file.
                                           Default per OS:
                                             Linux   /etc/senhub-agent/agent.yaml
                                             Windows %%ProgramData%%\SenHub\agent.yaml
                                             macOS   /usr/local/etc/senhub-agent/agent.yaml
    --verbose, -v                          Enable debug logging for all modules
    --filter module1,module2               Filter debug logs by module prefix (implies --verbose)
    --debug-modules module1,module2        [deprecated] Use --filter instead

HTTPS/TLS Options:
    --enable-https                         Enable HTTPS on the HTTP strategy
    --https-port PORT                      HTTPS port (default: 8443)
    --https-hosts HOST1,HOST2              Hostnames for auto-generated certificate SAN
    --cert-file PATH                       Custom TLS certificate file
    --key-file PATH                        Custom TLS private key file
    --min-tls-version VERSION              Minimum TLS version: 1.2 or 1.3 (default: 1.2)

Examples:
    %s install                                      # Install service + generate config
    %s run                                          # Run interactively (uses default config path)
    %s run --verbose                                # Run with debug output
    %s license show                                 # Check license status
    %s status                                       # Show running status
    %s update latest                                # Update to latest version

Note: the pre-0.2.0 flags --offline, --authentication-key and --server-url
were removed. Offline is the only mode; the agent key is generated at
install time and persisted in the config file.

`, exe, exe, exe, exe, exe, exe, exe)
}
