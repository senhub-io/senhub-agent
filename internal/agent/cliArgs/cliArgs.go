// Package cliArgs parses the senhub-agent command line.
//
// Mode model (0.2.0+): the agent runs offline only. The following CLI
// flags were removed in 0.2.0 and now produce a parse error if passed:
//
//	--offline           (was: enable offline mode; offline is now the only mode)
//	--authentication-key  (was: agent identity key — now read from the
//	                       config file only, generated at install time)
//	--server-url          (was: SaaS configuration server URL — no longer
//	                       reached; the cloud intake URL used by the
//	                       senhub data-push strategy is build-time
//	                       injected via ldflags)
//
// Operators who still pass any of these flags from a systemd unit or
// Windows service ExecStart MUST update those before upgrading to
// 0.2.0 — Go's go-arg parser rejects unknown flags at startup.
package cliArgs

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexflint/go-arg"
)

// Build-injected variables (set via ldflags from the Makefile).
var (
	Version        string
	CommitHash     string
	BuildTime      string
	GoVersion      string
	Env            string
	ProductionURL  string
	DevelopmentURL string
)

// CloudIntakeURL returns the cloud intake URL appropriate for the
// build environment. The senhub data-push strategy uses this when an
// operator wires it up in strategies.d/; offline-only mode means the
// URL is fixed at build time, not selectable at runtime.
func CloudIntakeURL() string {
	if Env == "development" {
		return DevelopmentURL
	}
	return ProductionURL
}

type CliArgs struct {
	Version *VersionSubcommandArgs `arg:"subcommand:version" help:"Print version information and exit"`
	Agent   *StartSubcommandArgs   `arg:"subcommand:start" help:"Start the agent (default)"`
	Update  *UpdateSubcommandArgs  `arg:"subcommand:update" help:"Update the agent"`
	License *LicenseSubcommandArgs `arg:"subcommand:license" help:"Manage agent license"`
}

type VersionSubcommandArgs struct{}

type LicenseSubcommandArgs struct {
	Activate *LicenseActivateArgs `arg:"subcommand:activate" help:"Activate a license"`
	Show     *LicenseShowArgs     `arg:"subcommand:show" help:"Show current license information"`
	Remove   *LicenseRemoveArgs   `arg:"subcommand:remove" help:"Remove current license"`
}

type LicenseActivateArgs struct {
	LicenseCode string `arg:"positional,required" help:"License code from Sensor Factory"`
	ConfigPath  string `arg:"--config-path" help:"Path to configuration file"`
	Verbose     bool   `arg:"-v,--verbose" help:"Enable verbose logging"`
}

type LicenseShowArgs struct {
	ConfigPath string `arg:"--config-path" help:"Path to configuration file"`
	Verbose    bool   `arg:"-v,--verbose" help:"Enable verbose logging"`
}

type LicenseRemoveArgs struct {
	ConfigPath string `arg:"--config-path" help:"Path to configuration file"`
	Verbose    bool   `arg:"-v,--verbose" help:"Enable verbose logging"`
	Force      bool   `arg:"-f,--force" help:"Skip confirmation prompt"`
}

type UpdateSubcommandArgs struct {
	Version     string `arg:"positional,required" help:"Version to update to"`
	RegistryUrl string `arg:"--registry-url" help:"URL of the registry to use"`
	Verbose     bool   `arg:"-v,--verbose" help:"Enable verbose logging"`
	DryRun      bool   `arg:"-d,--dry-run" help:"Do not perform the update, only print the new version"`
}

type StartSubcommandArgs struct {
	Verbose               bool              `arg:"-v,--verbose" help:"Enable verbose logging"`
	Filter                string            `arg:"--filter" help:"Filter debug logs to matching modules with prefix matching (e.g., 'probe' matches probe.veeam, probe.citrix). Implies --verbose."`
	DebugModules          string            `arg:"--debug-modules" help:"[deprecated: use --filter] Enable debug logging only for specific modules (comma-separated)"`
	DebugLogShipperUrl    string            `arg:"--debug-log-shipper-url,env:SENHUB_DEBUG_LOG_SHIPPER_URL" help:"URL of remote endpoint for shipping debug logs"`
	DebugLogShipperTags   map[string]string `arg:"--debug-log-shipper-tags,env:SENHUB_DEBUG_LOG_SHIPPER_TAGS" help:"Tags to add to debug log entries (format: key1=value1,key2=value2)"`
	DebugLogShipperBuffer int               `arg:"--debug-log-shipper-buffer,env:SENHUB_DEBUG_LOG_SHIPPER_BUFFER" help:"Buffer size for debug log shipper"`

	ConfigPath string `arg:"--config-path" help:"Path to the agent configuration file"`

	ServiceUser string `arg:"--user" help:"System user the installed Linux service runs as (default: senhub; use root to keep the legacy root unit)"`

	// HTTPS options for the HTTP strategy
	EnableHttps   bool   `arg:"--enable-https" help:"Enable HTTPS for HTTP strategy"`
	HttpsPort     int    `arg:"--https-port" help:"HTTPS port (default: 8443)"`
	HttpsHosts    string `arg:"--https-hosts" help:"Comma-separated hostnames for certificate SAN (default: localhost,127.0.0.1)"`
	CertFile      string `arg:"--cert-file" help:"Path to custom TLS certificate file"`
	KeyFile       string `arg:"--key-file" help:"Path to custom TLS private key file"`
	MinTlsVersion string `arg:"--min-tls-version" help:"Minimum TLS version (1.2, 1.3) (default: 1.2)"`
}

// ParsedArgs is the unified runtime view of the CLI arguments + any
// values loaded from the configuration file at boot.
//
// Fields:
//   - AuthenticationKey is populated by the agent from the config
//     file (or the install path) — NOT from a CLI flag.
//   - UpdateRegistryUrl is the only registry-side URL still set via
//     CLI; it gates `agent update` to a custom registry for testing.
type ParsedArgs struct {
	AuthenticationKey     string
	UpdateRegistryUrl     string
	Verbose               bool
	DebugModules          []string
	Env                   string
	Version               string
	WantedVersion         string
	CommitHash            string
	DryRun                bool
	DebugLogShipperUrl    string
	DebugLogShipperTags   map[string]string
	DebugLogShipperBuffer int

	ConfigPath string

	// ServiceUser is the system user the installed Linux service runs
	// as. Empty means the installer default (the dedicated senhub
	// user); "root" restores the pre-0.2.3 root unit.
	ServiceUser string

	// HTTPS options
	EnableHttps   bool
	HttpsPort     int
	HttpsHosts    []string
	CertFile      string
	KeyFile       string
	MinTlsVersion string

	// Status command flags. Only meaningful when command == "status";
	// set by main.go before the service handler dispatches.
	ShowOTLP bool
}

func GetVersionInfo() map[string]string {
	return map[string]string{
		"version":    Version,
		"commitHash": CommitHash,
		"buildTime":  BuildTime,
		"goVersion":  GoVersion,
		"env":        Env,
		"defaultURL": CloudIntakeURL(),
	}
}

// PrintVersion prints version information to stdout without timestamps
func PrintVersion() {
	if Version != "" {
		if CommitHash != "" {
			fmt.Printf("Version: %s (commit: %s)\n", Version, CommitHash)
		} else {
			fmt.Printf("Version: %s\n", Version)
		}
	} else if CommitHash != "" {
		fmt.Printf("Development version (commit: %s)\n", CommitHash)
	} else {
		fmt.Println("Version information not available")
	}

	if Env == "development" {
		fmt.Println("Environment: Development")
	}
}

// GetAbsoluteConfigPath returns an absolute path for the configuration
// file. Resolution order:
//
//  1. If configPath is absolute, return it (cleaned).
//  2. If configPath is empty, use the OS-canonical default
//     (/etc/senhub-agent/agent.yaml on Linux, %ProgramData%\SenHub\agent.yaml
//     on Windows, /usr/local/etc/senhub-agent/agent.yaml on macOS).
//  3. Otherwise, resolve relative to the binary directory — this
//     historical behaviour is kept for callers that still pass a bare
//     filename like "agent-config.yaml".
//
// Path traversal (`..`) is rejected explicitly.
func GetAbsoluteConfigPath(configPath string) (string, error) {
	if strings.Contains(configPath, "..") {
		return "", fmt.Errorf("path traversal not allowed in config path")
	}

	if configPath == "" {
		return canonicalConfigPath(), nil
	}

	if filepath.IsAbs(configPath) {
		return filepath.Clean(configPath), nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}

	binDir := filepath.Dir(execPath)
	absolutePath := filepath.Join(binDir, configPath)
	cleanPath := filepath.Clean(absolutePath)

	if !strings.HasPrefix(cleanPath, binDir) {
		return "", fmt.Errorf("config path must be within binary directory")
	}

	return cleanPath, nil
}

// ParseStartArgs parses a slice of CLI flag tokens as if they came
// after `install` or `run`. Used by the service-command dispatcher
// when the leading subcommand is a service verb rather than `start`
// — the top-level parser needs an explicit subcommand, so we call
// the start parser directly. Empty input is valid and yields a
// ParsedArgs filled with sensible defaults.
func ParseStartArgs(flags []string) *ParsedArgs {
	parsedEnv := Env
	if parsedEnv != "development" {
		parsedEnv = "production"
	}

	var startArgs StartSubcommandArgs
	p, err := arg.NewParser(arg.Config{}, &startArgs)
	if err != nil {
		log.Fatalf("failed to create start args parser: %v", err)
	}
	if parseErr := p.Parse(flags); parseErr != nil {
		if parseErr == arg.ErrHelp {
			p.WriteHelp(os.Stdout)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error parsing arguments: %v\n", parseErr)
		os.Exit(1)
	}
	return parsedArgsFromStartArgs(&startArgs, parsedEnv)
}

func MustParse() *ParsedArgs {
	var args CliArgs
	parsedEnv := Env
	if parsedEnv != "development" {
		parsedEnv = "production"
	}

	p, err := arg.NewParser(arg.Config{}, &args)
	if err != nil {
		log.Fatalf("there was an error in the definition of the Go struct: %v", err)
	}

	err = p.Parse(os.Args[1:])
	if err != nil {
		switch {
		case err == arg.ErrHelp:
			p.WriteHelp(os.Stdout)
			os.Exit(0)
		case p.Subcommand() == nil:
			// No subcommand was provided. Attempt to parse arguments
			// as start command (all fields optional).
			var startArgs StartSubcommandArgs
			sp, spErr := arg.NewParser(arg.Config{}, &startArgs)
			if spErr != nil {
				log.Fatalf("failed to create start args parser: %v", spErr)
			}
			// Parse errors are deliberately tolerated here: every
			// StartSubcommandArgs field is optional, so a bare
			// `senhub-agent` (no subcommand, no flags) must still
			// yield a usable ParsedArgs. The discard makes the
			// intent explicit for the linter (SA9003).
			if parseErr := sp.Parse(os.Args[1:]); parseErr != nil && parseErr != arg.ErrHelp {
				_ = parseErr
			}
			return parsedArgsFromStartArgs(&startArgs, parsedEnv)
		default:
			p.WriteUsage(os.Stdout)
			os.Exit(1)
		}
	}

	switch {
	case args.Version != nil:
		PrintVersion()
		os.Exit(0)
	case args.Agent != nil:
		return parsedArgsFromStartArgs(args.Agent, parsedEnv)
	case args.Update != nil:
		return parsedArgsFromUpdateArgs(args.Update, parsedEnv)
	default:
		p.Fail("Run with --help for usage information.")
		os.Exit(1)
	}
	return nil
}

func parsedArgsFromStartArgs(args *StartSubcommandArgs, environment string) *ParsedArgs {
	// Parse filter modules (--filter takes precedence, --debug-modules kept for compat)
	var debugModules []string
	if args.Filter != "" {
		debugModules = strings.Split(args.Filter, ",")
		for i, module := range debugModules {
			debugModules[i] = strings.TrimSpace(module)
		}
	} else if args.DebugModules != "" {
		debugModules = strings.Split(args.DebugModules, ",")
		for i, module := range debugModules {
			debugModules[i] = strings.TrimSpace(module)
		}
	}

	// --filter implies --verbose
	verbose := args.Verbose
	if len(debugModules) > 0 {
		verbose = true
	}

	// Parse HTTPS hosts from comma-separated string
	var httpsHosts []string
	if args.HttpsHosts != "" {
		httpsHosts = strings.Split(args.HttpsHosts, ",")
		for i, host := range httpsHosts {
			httpsHosts[i] = strings.TrimSpace(host)
		}
	} else {
		httpsHosts = []string{"localhost", "127.0.0.1"}
	}

	// Set defaults
	configPath := args.ConfigPath
	httpsPort := args.HttpsPort
	if httpsPort == 0 {
		httpsPort = 8443
	}
	minTlsVersion := args.MinTlsVersion
	if minTlsVersion == "" {
		minTlsVersion = "1.2"
	}

	return &ParsedArgs{
		Verbose:               verbose,
		DebugModules:          debugModules,
		Env:                   environment,
		Version:               Version,
		CommitHash:            CommitHash,
		DebugLogShipperUrl:    args.DebugLogShipperUrl,
		DebugLogShipperTags:   args.DebugLogShipperTags,
		DebugLogShipperBuffer: args.DebugLogShipperBuffer,

		ConfigPath: configPath,

		ServiceUser: args.ServiceUser,

		EnableHttps:   args.EnableHttps,
		HttpsPort:     httpsPort,
		HttpsHosts:    httpsHosts,
		CertFile:      args.CertFile,
		KeyFile:       args.KeyFile,
		MinTlsVersion: minTlsVersion,
	}
}

func parsedArgsFromUpdateArgs(args *UpdateSubcommandArgs, environment string) *ParsedArgs {
	return &ParsedArgs{
		UpdateRegistryUrl: args.RegistryUrl,
		Verbose:           args.Verbose,
		Env:               environment,
		Version:           Version,
		WantedVersion:     args.Version,
		CommitHash:        CommitHash,
		DryRun:            args.DryRun,
	}
}

// canonicalConfigPath returns the OS-canonical default location for
// the agent's configuration file. Used by GetAbsoluteConfigPath when
// no explicit path is provided.
func canonicalConfigPath() string {
	// canonicalConfigPath is implemented per-platform in
	// paths_<goos>.go to avoid sprinkling runtime.GOOS conditionals
	// through this resolution path.
	return canonicalConfigPathForOS()
}
