package app

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"senhub-agent.go/internal/agent/services/configuration"
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
	if len(args) == 0 || args[0] != "show" {
		fmt.Fprintln(os.Stderr, "Usage: agent key show [--config-path <path>]")
		os.Exit(2)
	}
	cfgPath, err := secretConfigFile(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cfg, err := configuration.LoadFromDisk(cfgPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if cfg.Agent.Key == "" {
		fmt.Fprintln(os.Stderr, "Error: no agent key configured")
		os.Exit(1)
	}
	// The agent key is a bearer token (sealable as ${secret:agent.key});
	// warn when it is being written somewhere other than a terminal, the
	// same safeguard `secret get` applies to a revealed secret value.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "warning: writing the agent key to a non-terminal")
	}
	fmt.Println(cfg.Agent.Key)
}
