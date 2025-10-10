package cliArgs

import (
	"log"
	"os"
	"strings"

	"github.com/alexflint/go-arg"
)

// Those variables are set by the build system
var (
	Version        string
	CommitHash     string
	BuildTime      string
	GoVersion      string
	Env            string
	ProductionURL  string
	DevelopmentURL string
)

// defaultServerURL returns the default server URL based on the environment
func defaultServerURL() string {
	if Env == "development" {
		log.Printf("Debug: Using development URL: %s", DevelopmentURL)
		return DevelopmentURL
	}
	log.Printf("Debug: Using production URL: %s", ProductionURL)
	return ProductionURL
}

type CliArgs struct {
	Version *VersionSubcommandArgs `arg:"subcommand:version" help:"Print version information and exit"`
	Agent   *StartSubcommandArgs   `arg:"subcommand:start" help:"Start the agent (default)"`
	Update  *UpdateSubcommandArgs  `arg:"subcommand:update" help:"Update the agent"`
}

type VersionSubcommandArgs struct{}
type UpdateSubcommandArgs struct {
	Version           string `arg:"positional,required" help:"Version to update to"`
	AuthenticationKey string `arg:"--authentication-key,env:SENHUB_KEY" help:"The authentication key for the agent"`
	RegistryUrl       string `arg:"--registry-url" help:"URL of the registry to use"`
	ServerUrl         string `arg:"--server-url,env:SENHUB_SERVER_URL" help:"The URL of senhub server to connect to"`
	Verbose           bool   `arg:"-v,--verbose" help:"Enable verbose logging"`
	DryRun            bool   `arg:"-d,--dry-run" help:"Do not perform the update, only print the new version"`
}

type StartSubcommandArgs struct {
	AuthenticationKey     string            `arg:"--authentication-key,env:SENHUB_KEY" help:"The authentication key for the agent"`
	ServerUrl             string            `arg:"--server-url,env:SENHUB_SERVER_URL" help:"The URL of senhub server to connect to"`
	Verbose               bool              `arg:"-v,--verbose" help:"Enable verbose logging"`
	DebugModules          string            `arg:"--debug-modules" help:"Enable debug logging only for specific modules (comma-separated: strategy.http,cache,probe.redfish)"`
	DebugLogShipperUrl    string            `arg:"--debug-log-shipper-url,env:SENHUB_DEBUG_LOG_SHIPPER_URL" help:"URL of remote endpoint for shipping debug logs"`
	DebugLogShipperTags   map[string]string `arg:"--debug-log-shipper-tags,env:SENHUB_DEBUG_LOG_SHIPPER_TAGS" help:"Tags to add to debug log entries (format: key1=value1,key2=value2)"`
	DebugLogShipperBuffer int               `arg:"--debug-log-shipper-buffer,env:SENHUB_DEBUG_LOG_SHIPPER_BUFFER" help:"Buffer size for debug log shipper"`

	// Offline mode options
	Offline    bool   `arg:"--offline" help:"Run in offline mode with local configuration"`
	ConfigPath string `arg:"--config-path" help:"Path to local configuration file (default: ./agent-config.yaml)"`

	// HTTPS options for offline mode
	EnableHttps   bool   `arg:"--enable-https" help:"Enable HTTPS for HTTP strategy"`
	HttpsPort     int    `arg:"--https-port" help:"HTTPS port (default: 8443)"`
	HttpsHosts    string `arg:"--https-hosts" help:"Comma-separated hostnames for certificate SAN (default: localhost,127.0.0.1)"`
	CertFile      string `arg:"--cert-file" help:"Path to custom TLS certificate file"`
	KeyFile       string `arg:"--key-file" help:"Path to custom TLS private key file"`
	MinTlsVersion string `arg:"--min-tls-version" help:"Minimum TLS version (1.2, 1.3) (default: 1.2)"`
}

type ParsedArgs struct {
	AuthenticationKey     string
	ServerUrl             string
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

	// Offline mode options
	Offline    bool
	ConfigPath string

	// HTTPS options
	EnableHttps   bool
	HttpsPort     int
	HttpsHosts    []string
	CertFile      string
	KeyFile       string
	MinTlsVersion string
}

func GetVersionInfo() map[string]string {
	return map[string]string{
		"version":    Version,
		"commitHash": CommitHash,
		"buildTime":  BuildTime,
		"goVersion":  GoVersion,
		"env":        Env,
		"defaultURL": defaultServerURL(),
	}
}

func MustParse() *ParsedArgs {
	var args CliArgs
	parsedEnv := Env
	if parsedEnv != "development" {
		parsedEnv = "production"
	}

	// Attempt to parse arguments as subcommand
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
			// No subcommand was provided.
			// Attempt to parse arguments as start command.
			var startArgs StartSubcommandArgs
			arg.MustParse(&startArgs)
			return parsedArgsFromStartArgs(&startArgs, parsedEnv)
		default:
			p.WriteUsage(os.Stdout)
			os.Exit(1)
		}
	}

	switch {
	case args.Version != nil && Version != "":
		log.Printf("Version: %s", Version)
		if parsedEnv == "development" {
			log.Printf("Development build")
		}
		os.Exit(0)
	case args.Version != nil:
		log.Printf("Development version: %s", CommitHash)
		if parsedEnv == "development" {
			log.Printf("Development build")
		}
		os.Exit(0)
	case args.Agent != nil:
		return parsedArgsFromStartArgs(args.Agent, parsedEnv)
	case args.Update != nil:
		return parsedArgsFromUpdateArgs(args.Update, parsedEnv)
	default:
		// No subcommand was provided.
		p.Fail("Run with --help for usage information.")
		os.Exit(1)
	}
	return nil
}

func parsedArgsFromStartArgs(args *StartSubcommandArgs, environment string) *ParsedArgs {
	// If ServerUrl is not specified, use default value (unless offline mode)
	serverUrl := args.ServerUrl
	if serverUrl == "" && !args.Offline {
		serverUrl = defaultServerURL()
		// Note: Default server URL not set for this environment
	}

	// Parse debug modules from comma-separated string
	var debugModules []string
	if args.DebugModules != "" {
		debugModules = strings.Split(args.DebugModules, ",")
		// Trim whitespace from each module
		for i, module := range debugModules {
			debugModules[i] = strings.TrimSpace(module)
		}
	}

	// Parse HTTPS hosts from comma-separated string
	var httpsHosts []string
	if args.HttpsHosts != "" {
		httpsHosts = strings.Split(args.HttpsHosts, ",")
		// Trim whitespace from each host
		for i, host := range httpsHosts {
			httpsHosts[i] = strings.TrimSpace(host)
		}
	} else {
		// Default hosts for certificate SAN
		httpsHosts = []string{"localhost", "127.0.0.1"}
	}

	// Set default config path if not specified
	configPath := args.ConfigPath
	if configPath == "" {
		configPath = "./agent-config.yaml"
	}

	// Set default HTTPS port if not specified
	httpsPort := args.HttpsPort
	if httpsPort == 0 {
		httpsPort = 8443
	}

	// Set default minimum TLS version
	minTlsVersion := args.MinTlsVersion
	if minTlsVersion == "" {
		minTlsVersion = "1.2"
	}

	return &ParsedArgs{
		AuthenticationKey:     args.AuthenticationKey,
		ServerUrl:             serverUrl,
		Verbose:               args.Verbose,
		DebugModules:          debugModules,
		Env:                   environment,
		Version:               Version,
		CommitHash:            CommitHash,
		DebugLogShipperUrl:    args.DebugLogShipperUrl,
		DebugLogShipperTags:   args.DebugLogShipperTags,
		DebugLogShipperBuffer: args.DebugLogShipperBuffer,

		// Offline mode options
		Offline:    args.Offline,
		ConfigPath: configPath,

		// HTTPS options
		EnableHttps:   args.EnableHttps,
		HttpsPort:     httpsPort,
		HttpsHosts:    httpsHosts,
		CertFile:      args.CertFile,
		KeyFile:       args.KeyFile,
		MinTlsVersion: minTlsVersion,
	}
}

func parsedArgsFromUpdateArgs(args *UpdateSubcommandArgs, environment string) *ParsedArgs {
	// If ServerUrl is not specified, use default value
	serverUrl := args.ServerUrl
	if serverUrl == "" {
		serverUrl = defaultServerURL()
		// Note: Default server URL not set for this environment
	}

	return &ParsedArgs{
		AuthenticationKey: args.AuthenticationKey,
		ServerUrl:         serverUrl,
		UpdateRegistryUrl: args.RegistryUrl,
		Verbose:           args.Verbose,
		Env:               environment,
		Version:           Version,
		WantedVersion:     args.Version,
		CommitHash:        CommitHash,
		DryRun:            args.DryRun,
	}
}
