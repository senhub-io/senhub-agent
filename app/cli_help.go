package app

import (
	"fmt"
	"io"
)

// helpRequested reports whether a help flag appears before the argument
// terminator. Flags after "--" are operands, not options.
func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

// selfHelpCommands are commands whose own parser prints help and exits
// before performing any action.
var selfHelpCommands = map[string]struct{}{
	"license":   {},
	"update":    {},
	"install":   {},
	"uninstall": {},
	"start":     {},
	"stop":      {},
	"restart":   {},
	"status":    {},
	"run":       {},
}

// interceptsHelp reports whether the shared CLI intercepts help for the
// command, as opposed to the command's own parser handling it.
func interceptsHelp(command string) bool {
	_, selfHelp := selfHelpCommands[command]
	return !selfHelp
}

const configUsageText = `Usage: agent config <subcommand> [options]

Subcommands:
    config init [opts]    Create the default offline configuration if none
                          exists (idempotent). Accepts --config-path,
                          --http-port <n>, --auto-update 1|0, --license <jwt>, --tags k=v,...,
                          --otlp-endpoint, --zabbix-server host:port,
                          --zabbix-host-metadata, --json, --ok-if-unchanged;
                          refuses a port already in use; exits 3 when the
                          configuration was already there
    config set <key> <v>  Change one setting (http.port, http.bind_address);
                          exits 3 when it already holds the value
    config check [path]   Validate configuration (covers fragments under
                          probes.d/ and strategies.d/ if present);
                          --json prints the result as one JSON object
    config show [opts]    Print merged + resolved configuration as YAML
                            --resolved            env/file references substituted, secrets in cleartext
                            --raw                 references preserved as written
                            --redact              substituted but secrets masked (default)
                            --json                wrap the output in one JSON object
                            [path]                config file path
    config migrate [path] Convert a legacy monolithic agent-config.yaml
                          into the 0.2.x multi-file layout
                          (agent.yaml + probes.d/ + strategies.d/) with
                          a timestamped backup. Idempotent.
`

var configSubUsageText = map[string]string{
	"init": `Usage: agent config init [opts]

Create the default offline configuration if none exists (idempotent).
Accepts --config-path, --http-port <n>, --auto-update 1|0, --license <jwt>, --tags k=v,...,
--otlp-endpoint, --zabbix-server host:port, --zabbix-host-metadata,
--json, --ok-if-unchanged; refuses a port already in use; exits 3 when
the configuration was already there.
`,
	"set": `Usage: agent config set <key> <v>

Change one setting (http.port, http.bind_address); exits 3 when it
already holds the value.
`,
	"check": `Usage: agent config check [path]

Validate configuration (covers fragments under probes.d/ and
strategies.d/ if present); --json prints the result as one JSON object.
`,
	"show": `Usage: agent config show [opts] [path]

Print merged + resolved configuration as YAML.
    --resolved    env/file references substituted, secrets in cleartext
    --raw         references preserved as written
    --redact      substituted but secrets masked (default)
    --json        wrap the output in one JSON object
`,
	"migrate": `Usage: agent config migrate [path]

Convert a legacy monolithic agent-config.yaml into the 0.2.x multi-file
layout (agent.yaml + probes.d/ + strategies.d/) with a timestamped
backup. Idempotent.
`,
}

const doctorUsageText = `Usage: agent doctor [options]

Diagnose the install, configuration, outputs, probes and host in one pass.
    --json              Print the result as one JSON object
    --config-path PATH  Path to the agent configuration file
Exits 1 on a warning, 2 on a failure.
`

const consoleUsageText = `Usage: agent console [options]

Open the web console in the browser.
    --print    Show the address only instead of opening a browser
`

const refreshUnitUsageText = `Usage: agent refresh-unit [options]

Rewrite the installed systemd unit with the current hardening directives
while preserving the validated User=/Group= and ExecStart. Linux/systemd
only.
    --yes    apply without confirmation prompt
`

// commandUsage returns the usage writer for a top-level command.
func commandUsage(command string, args []string) func(io.Writer) {
	switch command {
	case "config":
		if len(args) > 2 {
			if sub, ok := configSubUsageText[args[2]]; ok {
				return func(w io.Writer) {
					fmt.Fprint(w, sub)
					fmt.Fprintln(w, "Run 'agent config -h' for all config subcommands.")
				}
			}
		}
		return func(w io.Writer) {
			fmt.Fprint(w, configUsageText)
		}
	case "doctor":
		return func(w io.Writer) {
			fmt.Fprint(w, doctorUsageText)
		}
	case "console":
		return func(w io.Writer) {
			fmt.Fprint(w, consoleUsageText)
		}
	case "refresh-unit":
		return func(w io.Writer) {
			fmt.Fprint(w, refreshUnitUsageText)
		}
	case "secret":
		return writeSecretUsage
	case "key":
		return func(w io.Writer) {
			fmt.Fprintln(w, keyUsage)
		}
	case "zabbix":
		return func(w io.Writer) {
			fmt.Fprint(w, zabbixUsage)
		}
	}
	if c, ok := extraCommands[command]; ok && c.Usage != nil {
		return c.Usage
	}
	return func(io.Writer) {
		// showHelp prints to stdout.
		showHelp()
	}
}

// interceptHelp handles `<command> ... -h|--help` before dispatch. It
// returns true when it wrote usage and the command must not run.
func interceptHelp(args []string, out io.Writer) bool {
	if len(args) < 3 {
		return false
	}
	command := args[1]
	if !interceptsHelp(command) {
		return false
	}
	if !helpRequested(args[2:]) {
		return false
	}
	commandUsage(command, args)(out)
	return true
}
