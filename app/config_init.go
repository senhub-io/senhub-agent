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

// initConfigArgs holds the provisionable fields `config init` accepts.
type initConfigArgs struct {
	configPath   string
	license      string
	otlpEndpoint string
	tags         map[string]string
}

// parseInitConfigArgs parses the flags after `config init`. A value-taking
// flag whose value is missing, or an unrecognised flag, is a hard error:
// this command runs on unattended install paths (MSI silent install) where
// nobody reads the output. Silently skipping a typo'd `--licence` or a
// dropped value would leave the fleet host running Free-tier / untagged
// while reporting success. Kept pure (returns an error, no os.Exit) so it
// is unit-testable.
func parseInitConfigArgs(argv []string) (initConfigArgs, error) {
	out := initConfigArgs{tags: map[string]string{}}
	value := func(i *int) (string, error) {
		flag := argv[*i]
		if *i+1 >= len(argv) {
			return "", fmt.Errorf("flag %q needs a value", flag)
		}
		*i++
		return argv[*i], nil
	}
	for i := 0; i < len(argv); i++ {
		var err error
		switch argv[i] {
		case "--config-path":
			out.configPath, err = value(&i)
		case "--license":
			out.license, err = value(&i)
		case "--tags":
			var raw string
			if raw, err = value(&i); err == nil {
				out.tags = parseTagList(raw)
			}
		case "--otlp-endpoint":
			out.otlpEndpoint, err = value(&i)
		default:
			return out, fmt.Errorf("unknown flag %q", argv[i])
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func initConfig(argv []string) {
	opts, err := parseInitConfigArgs(argv)
	if err != nil {
		fatalf("config init: %v", err)
	}
	configPath := opts.configPath
	license := opts.license
	otlpEndpoint := opts.otlpEndpoint
	tags := opts.tags

	if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
		configPath = resolved
	}
	if configPath == "" {
		fatalf("config init: could not resolve a config path")
	}

	// Idempotent: an existing config (multi-file agent.yaml or a legacy
	// monolithic file) is preserved verbatim.
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Configuration already present at %s — leaving it unchanged.\n", configPath)
		return
	}

	args := &cliArgs.ParsedArgs{ConfigPath: configPath}
	if err := generateConfiguration(args); err != nil {
		fatalf("config init: %v", err)
	}

	if err := configuration.ApplyInstallOverrides(configPath, license, tags); err != nil {
		fatalf("config init: applying provisioned fields: %v", err)
	}

	if err := configuration.WriteOTLPStrategyFragment(filepath.Dir(configPath), otlpEndpoint); err != nil {
		fatalf("config init: writing OTLP strategy: %v", err)
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
