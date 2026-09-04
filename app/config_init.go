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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
)

// initConfigArgs holds the provisionable fields `config init` accepts.
type initConfigArgs struct {
	configPath   string
	license      string
	otlpEndpoint string
	otlpProtocol string
	tags         map[string]string
	httpPort     int
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
		case "--otlp-protocol":
			out.otlpProtocol, err = value(&i)
		case "--http-port":
			var raw string
			if raw, err = value(&i); err == nil {
				out.httpPort, err = parseHTTPPort(raw)
			}
		default:
			return out, fmt.Errorf("unknown flag %q", argv[i])
		}
		if err != nil {
			return out, err
		}
	}
	// Validate the OTLP inputs BEFORE any file is written. config init runs
	// on unattended installs, so a bad value must fail at parse — not after
	// a partial config already sits on disk, where a corrected rerun hits
	// the idempotency guard and silently never provisions OTLP (audit M4/m12).
	if err := validateOTLPArgs(out.otlpEndpoint, out.otlpProtocol); err != nil {
		return out, err
	}
	return out, nil
}

// parseHTTPPort reads the --http-port value. An empty value means the
// default: the MSI always passes the flag, with the HTTP_PORT property
// expanded to nothing when the operator set none.
func parseHTTPPort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("--http-port must be a number in 1-65535, got %q", raw)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("--http-port must be in 1-65535, got %d", port)
	}
	return port, nil
}

// validateOTLPArgs rejects an invalid protocol, a protocol without an
// endpoint, and an endpoint carrying characters that would break out of the
// generated YAML (whitespace/newline injects sibling keys, '#' truncates,
// '{' bricks parsing — audit M3).
func validateOTLPArgs(endpoint, protocol string) error {
	if protocol != "" && protocol != "grpc" && protocol != "http" {
		return fmt.Errorf("--otlp-protocol must be grpc or http, got %q", protocol)
	}
	if protocol != "" && endpoint == "" {
		return fmt.Errorf("--otlp-protocol requires --otlp-endpoint")
	}
	if endpoint != "" && strings.IndexFunc(endpoint, badEndpointRune) >= 0 {
		return fmt.Errorf("--otlp-endpoint %q contains whitespace or an invalid character; expected host:port", endpoint)
	}
	return nil
}

// badEndpointRune reports whether a rune is illegal in an OTLP endpoint:
// any control char or space (0x00-0x20), or a YAML metacharacter that could
// break out of the `endpoint:` scalar.
func badEndpointRune(r rune) bool {
	return r <= ' ' || r == '#' || r == '{' || r == '}' || r == '"' || r == '\''
}

func initConfig(argv []string) {
	opts, err := parseInitConfigArgs(argv)
	if err != nil {
		fatalf("config init: %v", err)
	}
	configPath := opts.configPath
	license := opts.license
	otlpEndpoint := opts.otlpEndpoint
	otlpProtocol := opts.otlpProtocol
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
		// Still ensure the OTLP fragment on an existing config: a prior run
		// could have generated the config but not yet written the fragment.
		// Idempotent — WriteOTLPStrategyFragment no-ops when 10-otlp.yaml
		// already exists (audit M4).
		if err := configuration.WriteOTLPStrategyFragment(filepath.Dir(configPath), otlpEndpoint, otlpProtocol); err != nil {
			fatalf("config init: writing OTLP strategy: %v", err)
		}
		return
	}

	// Refuse a port the strategy cannot bind before anything is written.
	// This command runs where nobody watches the output, so a silent
	// half-success here becomes a service that runs and answers nothing.
	httpPort := opts.httpPort
	if httpPort == 0 {
		httpPort = defaultHTTPPort
	}
	if err := checkHTTPPortFree(defaultHTTPBindAddress, httpPort); err != nil {
		fatalf("config init: %v", err)
	}

	args := &cliArgs.ParsedArgs{ConfigPath: configPath, HttpPort: opts.httpPort}
	if err := generateConfiguration(args); err != nil {
		fatalf("config init: %v", err)
	}

	if err := configuration.ApplyInstallOverrides(configPath, license, tags); err != nil {
		fatalf("config init: applying provisioned fields: %v", err)
	}

	if err := configuration.WriteOTLPStrategyFragment(filepath.Dir(configPath), otlpEndpoint, otlpProtocol); err != nil {
		fatalf("config init: writing OTLP strategy: %v", err)
	}

	fmt.Printf("Configuration created at %s\n", configPath)
	fmt.Printf("  http: %s\n", net.JoinHostPort(defaultHTTPBindAddress, strconv.Itoa(httpPort)))
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
