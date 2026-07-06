package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
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
		// handleStartError already calls os.Exit(1) before Start returns
		// an error on misconfiguration. This path is a defence-in-depth
		// fallback for callers that override exitFn (tests) or for future
		// code that makes handleStartError non-fatal.
		log.Printf("agent error: %s", err)
		os.Exit(1)
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
	case "install", "uninstall", "start", "stop", "restart", "refresh-unit":
		return true
	case "update":
		// `update <version>` overwrites the on-disk binary, which the
		// help text (showUpdateHelp) states needs the same privileges as
		// the service commands. Gating it here makes that promise real:
		// otherwise the update proceeds and fails mid-flight on the
		// binary write with a raw permission error. The pure-read
		// `update --list` / `--help` forms are exempted in
		// readOnlyCommand and never reach this gate.
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
		if !isElevated() {
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

// fatalf prints a user-facing failure to stderr and exits non-zero. It
// is the CLI's single fatal-error path: command handlers use it instead
// of log.Fatalf so a failure reads as a plain "Error: ..." line on
// stderr, consistent with the rest of the CLI, rather than a
// timestamped log line (the default logger runs with LstdFlags).
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

// readYesConfirmation reads a single interactive answer from stdin and
// reports whether it is an affirmative ("y"/"Y"). An empty line — which
// is what fmt.Scanln returns on EOF or a non-TTY stdin — resolves to
// false, so a destructive command run non-interactively without a
// bypass flag aborts (safe default) rather than proceeding.
func readYesConfirmation() bool {
	var answer string
	// A scan error (EOF, empty input) leaves answer == "" and
	// answerIsYes returns false: the caller aborts, which is the safe
	// outcome for a destructive action.
	_, _ = fmt.Scanln(&answer)
	return answerIsYes(answer)
}

// answerIsYes is the pure decision behind readYesConfirmation: only an
// explicit "y"/"Y" (ignoring surrounding whitespace) is affirmative.
// Empty input — EOF or a non-TTY stdin — is NOT affirmative, so a
// destructive command aborts by default.
func answerIsYes(answer string) bool {
	answer = strings.TrimSpace(answer)
	return answer == "y" || answer == "Y"
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

// parseUpdateArg interprets the single positional argument to the
// `update` subcommand. `--help`/`-h`/`help` request usage (wantHelp
// true, no version); `--list`/`-l` selects the version-listing mode;
// anything else is taken as an explicit target version. Extracted as a
// pure function so the dispatch — which used to treat `update --help`
// as a version literal and try to install "--help" — is unit-testable.
func parseUpdateArg(subArg string) (wantedVersion string, wantHelp bool) {
	switch subArg {
	case "--help", "-h", "help":
		return "", true
	case "--list", "-l":
		return "list", false
	default:
		return subArg, false
	}
}

// showUpdateHelp prints usage for the `update` subcommand.
func showUpdateHelp() {
	exe := os.Args[0]
	fmt.Printf(`Usage: %s update [--list | <version>]

Check for, list, or install agent updates.

    %s update              Check for a newer version and install it
    %s update --list       List all available versions (stable + beta)
    %s update <version>    Install a specific version (e.g. %s update 0.4.1)

Updating replaces the running binary and requires the same privileges
as the service commands (root on Linux, administrator on Windows).
`, exe, exe, exe, exe, exe)
}

// parseUpdateCommand interprets the arguments after the `update` verb
// (os.Args[2:]). It resolves the three invocation shapes:
//
//   - `update` (no args)                 → check for and install the newest release
//   - `update --list` / `-l`             → list available versions (no binary write)
//   - `update <version> [--registry-url URL] [--dry-run] [--verbose]`
//
// `--help` / `-h` / `help` set wantHelp. Anything malformed — an unknown
// flag, or flags without the required <version> positional — returns a
// non-nil error so the dispatch rejects it instead of, as the old code
// did, treating a stray flag as a version literal and pushing it to the
// registry. Kept pure (no os.Exit) so it is unit-testable.
func parseUpdateCommand(argv []string) (parsed *cliArgs.ParsedArgs, wantHelp bool, err error) {
	base := &cliArgs.ParsedArgs{
		Version:    cliArgs.Version,
		CommitHash: cliArgs.CommitHash,
	}
	if len(argv) == 0 {
		return base, false, nil
	}

	// Mode selectors that carry no <version> positional.
	switch argv[0] {
	case "--help", "-h", "help":
		return nil, true, nil
	case "--list", "-l":
		if len(argv) > 1 {
			return nil, false, fmt.Errorf("--list takes no further arguments")
		}
		base.WantedVersion = "list"
		return base, false, nil
	}

	// Otherwise a concrete `<version> [flags]` install: parse with the
	// canonical UpdateSubcommandArgs so --registry-url / --dry-run are
	// honoured and unknown flags are rejected.
	var ua cliArgs.UpdateSubcommandArgs
	p, perr := arg.NewParser(arg.Config{Program: "agent update"}, &ua)
	if perr != nil {
		return nil, false, fmt.Errorf("building update parser: %w", perr)
	}
	if perr := p.Parse(argv); perr != nil {
		if perr == arg.ErrHelp {
			return nil, true, nil
		}
		return nil, false, perr
	}
	base.WantedVersion = ua.Version
	base.UpdateRegistryUrl = ua.RegistryUrl
	base.DryRun = ua.DryRun
	if ua.Verbose {
		base.Verbose = true
	}
	return base, false, nil
}

// stripFlags returns argv with every occurrence of the given (boolean,
// value-less) flags removed. Used to hand a clean slice to the start-args
// parser, which does not know the CLI's view/confirm flags (--otlp,
// --yes) and would reject them as unknown.
func stripFlags(argv []string, drop ...string) []string {
	dropSet := make(map[string]struct{}, len(drop))
	for _, d := range drop {
		dropSet[d] = struct{}{}
	}
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		if _, ok := dropSet[a]; ok {
			continue
		}
		out = append(out, a)
	}
	return out
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
	case "update":
		// `update --help` and `update --list` are informational only —
		// usage text or a registry read, no binary replacement — so they
		// must not hit the privilege gate. A real `update <version>` (or a
		// bare `update`, which installs the newest release) does replace
		// the binary and stays gated.
		if len(args) > 2 {
			switch args[2] {
			case "--help", "-h", "help", "--list", "-l":
				return true
			}
		}
	case "db-monitoring":
		// db-monitoring only generates SQL to stdout (its `init`
		// subcommand) or prints help — it never touches the service, the
		// filesystem or the database, so requiring administrator on
		// Windows was an over-broad gate that blocked an operator drafting
		// a monitoring grant.
		return true
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
	"refresh-unit": {},
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
	// without hitting the gate. The bare word `help` is handled here too:
	// it is a known top-level arg but matches no dispatch case, so without
	// this short-circuit it fell through to the tolerant start-args parse
	// and silently spawned a second agent (issue #134's failure mode).
	if len(os.Args) <= 1 || os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help" {
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
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
		if len(os.Args) > 2 && os.Args[2] == "init" {
			// agent config init [--config-path <path>] [--license <jwt>]
			//                    [--tags k=v,k2=v2]
			// Create the default offline configuration for an unattended
			// install (MSI silent install), then apply the provisionable
			// fields. Idempotent: leaves an existing config untouched.
			initConfig(os.Args[3:])
			return
		}
		// A typo'd subcommand (`config ini`) must not "succeed" with exit 0
		// and generic help — a scripted provisioning step would read that as
		// done without creating anything. Unknown top-level verbs already
		// exit 2; the config subcommand does too. A bare `config` with no
		// subcommand still prints help (exit 0).
		if len(os.Args) > 2 {
			fmt.Fprintf(os.Stderr, "Error: unknown config subcommand %q\n", os.Args[2])
			fmt.Fprintln(os.Stderr, "Run with --help for usage information.")
			os.Exit(2)
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
		// Parse the full update invocation, not just os.Args[2]: the
		// documented --registry-url / --dry-run flags must reach the
		// updater, and an unknown flag must be rejected rather than taken
		// as a version literal (the `--help` misfire family, #134).
		parsed, wantHelp, err := parseUpdateCommand(os.Args[2:])
		if wantHelp {
			showUpdateHelp()
			return
		}
		if err != nil {
			fatalf("update: %v", err)
		}
		agent.UpdateAgent(parsed)
		return
	case "refresh-unit":
		runRefreshUnit()
		return
	case "install", "uninstall", "start", "stop", "restart", "status", "run":
		// Commands that take no positional args: dispatched directly.
		// `status` carries the optional --otlp view flag; `uninstall` the
		// --yes confirmation-bypass flag. Both must still honour
		// --config-path so a custom-path install is stopped/uninstalled
		// against the right file (otherwise cleanupFiles resolves the
		// DEFAULT path and leaves the custom config/certs behind).
		if command == "start" || command == "stop" || command == "restart" || command == "status" || command == "uninstall" {
			// --otlp / --yes are view/confirm flags the start parser does
			// not know; strip them before parsing so it does not reject
			// them, then set the corresponding fields explicitly.
			args := cliArgs.ParseStartArgs(stripFlags(os.Args[2:], "--otlp", "--yes"))
			if command == "status" {
				args.ShowOTLP = hasArg("--otlp")
			}
			if command == "uninstall" {
				args.Yes = hasArg("--yes")
			}
			handleServiceCommand(command, args)
			return
		}

		// `install` and `run` accept optional flags (--config-path,
		// --enable-https, …). In 0.2.0+ they also work with no args
		// — install auto-generates a UUID agent key, run uses the
		// OS-canonical default config path. Pre-0.2.0 the install
		// path also works with no flags now.
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
    install              Install as system service (auto-generates a UUID agent key).
                         On Linux the service runs as the dedicated 'senhub' user
                         under a hardened systemd unit.
    install --user USER  Service user for the Linux unit (default: senhub;
                         use 'root' to keep the legacy root unit)
    uninstall            Remove the system service (prompts before deleting
                         config/certs/logs; pass --yes to skip the prompt)
    uninstall --yes      Remove the system service without confirmation
    start                Start the service
    stop                 Stop the service
    restart              Restart the service
    status               Show service and probe status
    status --otlp        Also show OTLP pipeline self-metrics
    run                  Run interactively in console mode
    refresh-unit         Refresh the installed systemd unit to the version
                         embedded in this binary (Linux only; requires root)

License Commands:
    license show         Show current license information
    license activate     Activate a license from a JWT token
    license remove       Remove current license (revert to free tier)

Other Commands:
    version              Show agent version
    update               Check for new versions
    update --list        List all available versions (stable + beta)
    update <version>     Install a specific version
    config init [opts]    Create the default offline configuration if none
                          exists (idempotent). Accepts --config-path,
                          --license <jwt>, --tags k=v,..., --otlp-endpoint
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

Secret Store Commands:
    secret set <name>    Store/replace a secret (hidden prompt, stdin, or
                         --from-file); referenced from config as ${secret:name}
    secret get <name>    Print a secret value (deliberate reveal)
    secret list          List secret names (never values)
    secret rm <name>     Delete a secret
    secret migrate       Move inline plaintext secrets from config into the store
    secret wire-unit     (Linux/systemd-creds) regenerate the unit credential drop-in
    secret status        Show the active backend and store location
    key show             Print the configured agent key

Database Helper Commands:
    db-monitoring init   Generate least-privilege SQL to provision a
                         monitoring user (--engine mysql|postgresql --user NAME)

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

Note: the agent key is generated at install time and persisted in the
config file.

`, exe, exe, exe, exe, exe, exe, exe)
}
