// IBM i runtime diagnostic. `senhub-agent ibmi check` reads the merged
// configuration, looks for every ibmi probe, and reports which runtime
// (native binary or JRE) the agent would use at startup — without
// actually spawning the bridge subprocess. Operators run it after
// deploying the agent to confirm the runtime is reachable; CI gates
// can run it against a staged config to catch drift.
package app

import (
	"fmt"
	"os"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

// The ibmi verb registers itself rather than being hardcoded in the
// dispatch switch, so it can move to the senhub-agent-enterprise module
// (alongside the ibmi probe and its jt400 bridge) without the core CLI
// referencing it. ReadOnly is left false to preserve the existing
// privilege-gated behaviour.
func init() {
	RegisterCommand(ExtraCommand{Name: "ibmi", Run: handleIBMICommand})
}

// handleIBMICommand dispatches the `senhub-agent ibmi <subcommand>`
// verbs. Only `check` is wired today; the slot is here so future
// sub-verbs (e.g. `ibmi smoke` for a live connection test) can be
// added without re-laying the routing.
func handleIBMICommand() {
	if len(os.Args) < 3 {
		showIBMIHelp()
		return
	}
	switch os.Args[2] {
	case "check":
		configPath := defaultConfigPathOrFlag(os.Args[3:])
		ibmiCheck(configPath)
	case "--help", "-h", "help":
		showIBMIHelp()
	default:
		fmt.Printf("Unknown ibmi subcommand: %s\n\n", os.Args[2])
		showIBMIHelp()
		os.Exit(2)
	}
}

func showIBMIHelp() {
	exe := os.Args[0]
	fmt.Println("IBM i probe diagnostics")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("    check [path]   Verify the runtime (native binary or JRE)")
	fmt.Println("                   each ibmi probe in the merged configuration")
	fmt.Println("                   would resolve at startup. Does NOT contact")
	fmt.Println("                   the IBM i host.")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("    [path]         Configuration file (default: ./agent-config.yaml)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("    %s ibmi check\n", exe)
	fmt.Printf("    %s ibmi check /etc/senhub-agent/agent.yaml\n", exe)
}

// defaultConfigPathOrFlag picks the first positional argument as the
// config path, falling back to the project's standard default. We
// don't accept flags here — keeps the CLI surface tiny and the
// behaviour identical to `config check`.
func defaultConfigPathOrFlag(args []string) string {
	for _, a := range args {
		if a != "" && a[0] != '-' {
			return a
		}
	}
	return "./agent-config.yaml"
}

// ibmiCheck loads the merged configuration, iterates every ibmi
// probe, resolves its runtime, and prints a human-readable report.
// Exits 0 on full success, 1 if any probe failed to resolve, 2 on
// I/O / parse errors before even reaching the resolver.
func ibmiCheck(configPath string) {
	fmt.Printf("Checking IBM i runtime for configuration: %s\n\n", configPath)

	silentArgs := &cliArgs.ParsedArgs{Verbose: false}
	baseLog := agentLogger.NewLogger(silentArgs)
	log := agentLogger.NewModuleLogger(baseLog, "ibmi.check")

	data, err := configuration.LoadForShow(configPath, configuration.ShowResolved, log)
	if err != nil {
		fmt.Printf("  [ERROR] Failed to load configuration: %v\n", err)
		os.Exit(2)
	}

	ibmiProbes := []configuration.ProbeConfig{}
	for _, p := range data.Probes {
		if p.Type == "ibmi" {
			ibmiProbes = append(ibmiProbes, p)
		}
	}
	if len(ibmiProbes) == 0 {
		fmt.Println("  No ibmi probes configured. Nothing to check.")
		fmt.Println("  Add a probe with `type: ibmi` to probes.d/ or the legacy probes: list.")
		return
	}

	allOK := true
	for i, probe := range ibmiProbes {
		fmt.Printf("Probe %d/%d: %s (type=ibmi)\n", i+1, len(ibmiProbes), probe.Name)
		cfg := buildBridgeConfigFromProbe(probe)
		res := bridge.ResolveRuntime(cfg)
		if res.Err != nil {
			allOK = false
			// The error string already contains the candidate list,
			// so we don't repeat it here.
			fmt.Printf("  [FAIL] %v\n", res.Err)
		} else {
			fmt.Printf("  [OK]   mode=%s  source=%s\n", res.Mode, res.Source)
			switch res.Mode {
			case bridge.RuntimeModeNative:
				fmt.Printf("         native_runner: %s\n", res.NativeRunner)
			case bridge.RuntimeModeJava:
				fmt.Printf("         java_bin:      %s\n", res.JavaBin)
				fmt.Printf("         runner_dir:    %s\n", res.RunnerDir)
			}
			// On success only show non-selected candidates, useful
			// when an operator wants to know what other paths the
			// resolver inspected before picking this one.
			if len(res.Tried) > 0 {
				fmt.Println("         also examined:")
				for _, t := range res.Tried {
					fmt.Printf("           - %s\n", t)
				}
			}
		}
		fmt.Println()
	}

	if !allOK {
		fmt.Println("One or more probes have no usable IBM i runtime.")
		fmt.Println("See docs/admin-guide/IBMI-RUNTIME.md for deployment options.")
		os.Exit(1)
	}
	fmt.Printf("%d probe(s) have a usable runtime.\n", len(ibmiProbes))
}

// buildBridgeConfigFromProbe extracts the bridge-related YAML fields
// from a ProbeConfig.Params map into a bridge.Config the resolver
// understands. We map only the keys the resolver cares about
// (native_runner, bridge_runner_dir, java_home) — credentials and
// timeouts are not relevant for runtime detection.
func buildBridgeConfigFromProbe(probe configuration.ProbeConfig) bridge.Config {
	params := probe.Params
	if params == nil {
		return bridge.Config{}
	}
	getString := func(key string) string {
		if v, ok := params[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	return bridge.Config{
		NativeRunner: getString("native_runner"),
		JavaHome:     getString("java_home"),
		RunnerDir:    getString("bridge_runner_dir"),
	}
}
