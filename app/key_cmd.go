package app

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/cliexit"
)

// `agent key show` reveals the agent key — the bearer token an operator needs to
// reach the web UI and to configure PRTG/Nagios scrapers. It loads the config and
// prints the RESOLVED key, so it works whether the key is still inline or has
// been sealed into the store as ${secret:agent.key}. Not ReadOnly: resolving a
// sealed key reads the root-owned store, so it runs behind the privilege gate.
func init() {
	RegisterCommand(ExtraCommand{Name: "key", ReadOnly: false, Run: runKeyCommand})
}

func runKeyCommand() {
	args := os.Args[2:]
	if len(args) == 0 || (args[0] != "show" && args[0] != "instance-id") {
		fmt.Fprintln(os.Stderr, "Usage: agent key show|instance-id [--config-path <path>]")
		os.Exit(cliexit.Failure)
	}
	cfgPath, err := secretConfigFile(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cliexit.Failure)
	}
	cfg, err := configuration.LoadFromDisk(cfgPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cliexit.Failure)
	}
	if cfg.Agent.Key == "" {
		fmt.Fprintln(os.Stderr, "Error: no agent key configured")
		os.Exit(cliexit.Failure)
	}
	// The instance id is what the agent's telemetry and its entity carry
	// as service.instance.id. It is derived one way from the key and is
	// not a credential, so it prints without the non-terminal warning.
	if args[0] == "instance-id" {
		fmt.Println(configuration.AgentInstanceID(cfg.Agent.Key))
		return
	}

	// The agent key is a bearer token (sealable as ${secret:agent.key});
	// warn when it is being written somewhere other than a terminal, the
	// same safeguard `secret get` applies to a revealed secret value.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "warning: writing the agent key to a non-terminal")
	}
	fmt.Println(cfg.Agent.Key)
}
