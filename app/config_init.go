// `agent config init` — create a default configuration for an unattended
// install (MSI silent install, scripted provisioning). It generates the
// standard offline multi-file layout if none exists, then applies the few
// provisionable fields an installer can pass: the license token (paid probe
// tiers) and host global tags. There is NO cloud backend or auth token to seed
// — the agent is offline by default; pushing to a collector is a separate OTLP
// strategy the operator adds later.
//
// Idempotent: if a configuration already exists at the target path it is left
// untouched, so a repair/reinstall never clobbers operator config.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
)

func initConfig(argv []string) {
	configPath := ""
	license := ""
	otlpEndpoint := ""
	tags := map[string]string{}

	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--config-path":
			if i+1 < len(argv) {
				configPath = argv[i+1]
				i++
			}
		case "--license":
			if i+1 < len(argv) {
				license = argv[i+1]
				i++
			}
		case "--tags":
			if i+1 < len(argv) {
				tags = parseTagList(argv[i+1])
				i++
			}
		case "--otlp-endpoint":
			if i+1 < len(argv) {
				otlpEndpoint = argv[i+1]
				i++
			}
		}
	}

	if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
		configPath = resolved
	}
	if configPath == "" {
		fmt.Fprintln(os.Stderr, "config init: could not resolve a config path")
		os.Exit(1)
	}

	// Idempotent: an existing config (multi-file agent.yaml or a legacy
	// monolithic file) is preserved verbatim.
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Configuration already present at %s — leaving it unchanged.\n", configPath)
		return
	}

	args := &cliArgs.ParsedArgs{ConfigPath: configPath}
	if err := generateConfiguration(args); err != nil {
		fmt.Fprintf(os.Stderr, "config init: %v\n", err)
		os.Exit(1)
	}

	if err := configuration.ApplyInstallOverrides(configPath, license, tags); err != nil {
		fmt.Fprintf(os.Stderr, "config init: applying provisioned fields: %v\n", err)
		os.Exit(1)
	}

	if err := configuration.WriteOTLPStrategyFragment(filepath.Dir(configPath), otlpEndpoint); err != nil {
		fmt.Fprintf(os.Stderr, "config init: writing OTLP strategy: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Configuration created at %s\n", configPath)
	if license != "" {
		fmt.Println("  license: set")
	}
	if len(tags) > 0 {
		fmt.Printf("  global_tags: %d\n", len(tags))
	}
	if otlpEndpoint != "" {
		fmt.Printf("  otlp endpoint: %s\n", otlpEndpoint)
	}
	fmt.Printf("  probes: %s\n", filepath.Join(filepath.Dir(configPath), "probes.d"))
}

// parseTagList turns "k1=v1,k2=v2" into a map. Blank entries and entries
// without '=' are skipped; keys/values are trimmed.
func parseTagList(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}
