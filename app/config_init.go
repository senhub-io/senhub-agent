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
	licensepkg "senhub-agent.go/internal/agent/services/license"
)

// initConfigArgs holds the provisionable fields `config init` accepts.
type initConfigArgs struct {
	configPath   string
	license      string
	otlpEndpoint string
	otlpProtocol string
	tags         map[string]string
	httpPort     int
	licenseFile  string
	licenseDir   string
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
		case "--license-file":
			out.licenseFile, err = value(&i)
		case "--license-dir":
			out.licenseDir, err = value(&i)
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
		// A kept configuration is not re-seeded (the agent owns its config,
		// the installer does not rewrite it). Two things still deserve a
		// word, because the installer runs before the service starts and
		// this print is the only trace the operator gets:
		//   - a port asked for on this run that the kept config ignores,
		//     which is exactly the reinstall-over-kept-config surprise;
		//   - a kept port that is already taken.
		if _, port := resolveHTTPStrategyEndpoint(configPath); port > 0 {
			if opts.httpPort != 0 && opts.httpPort != port {
				fmt.Printf("Warning: --http-port %d was ignored; the existing configuration keeps port %d. Change it with 'senhub-agent config set http.port %d'.\n", opts.httpPort, port, opts.httpPort)
			}
			if portErr := checkHTTPPortFree(defaultHTTPBindAddress, port); portErr != nil {
				fmt.Printf("Warning: %v\n", portErr)
			}
		}
		// Still ensure the OTLP fragment on an existing config: a prior run
		// could have generated the config but not yet written the fragment.
		// Idempotent — WriteOTLPStrategyFragment no-ops when 10-otlp.yaml
		// already exists (audit M4).
		if err := configuration.WriteOTLPStrategyFragment(filepath.Dir(configPath), otlpEndpoint, otlpProtocol); err != nil {
			fatalf("config init: writing OTLP strategy: %v", err)
		}
		return
	}

	// Read the licence file before anything is written: an unreadable path
	// must fail with nothing on disk. A configuration left behind by a
	// failed install is picked up as "already present" by the next one,
	// which then ignores the port and licence it was given.
	licenseFile := opts.licenseFile
	if licenseFile == "" && opts.licenseDir != "" {
		found, findErr := findLicenseInDir(opts.licenseDir)
		if findErr != nil {
			fatalf("config init: %v", findErr)
		}
		if found == "" {
			fmt.Fprintf(os.Stderr, "Note: no licence file (*.jwt) in %s; installing on the Free tier. A licence can be added later from the web console.\n", opts.licenseDir)
		}
		licenseFile = found
	}
	license, err := resolveLicenseInput(opts.license, licenseFile)
	if err != nil {
		fatalf("config init: %v", err)
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

	// A licence is bound to this machine's agent key. At install the key
	// has just been generated, so a licence provisioned on the command
	// line can only match one obtained for this exact key. Warn on a
	// mismatch rather than fail: the agent still runs (Free tier), and an
	// unattended install must not abort on a licence that can be fixed
	// later with `license activate`.
	if license != "" {
		warnLicenseBinding(configPath, license)
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

// findLicenseInDir returns the licence file to use from a folder the
// operator picked in the installer: license.jwt when present, else the
// single *.jwt there. No file is not an error (the operator may have
// left the default folder, meaning Free tier); several files are, since
// guessing which customer's licence to install is worse than asking.
// The lookup is not recursive: the installer's folder picker can land on
// a drive root, and walking it would be slow and surprising.
func findLicenseInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading licence folder %s: %w", dir, err)
	}
	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.EqualFold(name, "license.jwt") {
			return filepath.Join(dir, name), nil
		}
		if strings.EqualFold(filepath.Ext(name), ".jwt") {
			candidates = append(candidates, filepath.Join(dir, name))
		}
	}
	switch len(candidates) {
	case 0:
		return "", nil
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("%d licence files (*.jwt) in %s; keep only the one to install, or name it license.jwt", len(candidates), dir)
	}
}

// resolveLicenseInput returns the licence to provision: the content of
// licenseFile when given (a file is what a customer receives; a token is
// too long to type), else the inline token. An unreadable file is an
// error; an empty file falls back to the inline token.
func resolveLicenseInput(inline, licenseFile string) (string, error) {
	if licenseFile == "" {
		return inline, nil
	}
	data, err := os.ReadFile(licenseFile) // #nosec G304 - operator-provided path
	if err != nil {
		return "", fmt.Errorf("reading --license-file %s: %w", licenseFile, err)
	}
	if fromFile := strings.TrimSpace(string(data)); fromFile != "" {
		return fromFile, nil
	}
	return inline, nil
}

// warnLicenseBinding prints a warning when the provisioned licence is not
// bound to the agent key just generated for this install. It never fails
// the command; the boot-time check and `license activate` enforce it.
func warnLicenseBinding(configPath, jwt string) {
	validator, err := licensepkg.GetDefaultValidator(7)
	if err != nil {
		return
	}
	lic, err := validator.ValidateLicense(jwt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: the provisioned licence could not be validated: %v\n", err)
		return
	}
	agentKey, err := extractAgentKeyFromConfig(configPath)
	if err != nil || agentKey == "" {
		return
	}
	if !licensepkg.VerifyBinding("", agentKey, lic) {
		fmt.Fprintf(os.Stderr, "Warning: the provisioned licence is issued for another agent (%q), not this one (%q); the agent will run on the Free tier until a valid licence is activated.\n", lic.Subject, agentKey)
	}
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
