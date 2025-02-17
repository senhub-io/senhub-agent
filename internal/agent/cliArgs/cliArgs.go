package cliArgs

import (
	"log"
	"os"

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
	AuthenticationKey string `arg:"required,--authentication-key,env:SENHUB_KEY" help:"The authentication key for the agent"`
	ServerUrl         string `arg:"--server-url,env:SENHUB_SERVER_URL" help:"The URL of senhub server to connect to"`
	Verbose           bool   `arg:"-v,--verbose" help:"Enable verbose logging"`
}

type ParsedArgs struct {
	AuthenticationKey string
	ServerUrl         string
	UpdateRegistryUrl string
	Verbose           bool
	Env               string
	Version           string
	WantedVersion     string
	CommitHash        string
	DryRun            bool
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
	// If ServerUrl is not specified, use default value
	serverUrl := args.ServerUrl
	if serverUrl == "" {
		serverUrl = defaultServerURL()
		if serverUrl == "" {
			log.Printf("Warning: Default server URL is not set for environment %s", environment)
		}
	}

	return &ParsedArgs{
		AuthenticationKey: args.AuthenticationKey,
		ServerUrl:         serverUrl,
		Verbose:           args.Verbose,
		Env:               environment,
		Version:           Version,
		CommitHash:        CommitHash,
	}
}

func parsedArgsFromUpdateArgs(args *UpdateSubcommandArgs, environment string) *ParsedArgs {
	// If ServerUrl is not specified, use default value
	serverUrl := args.ServerUrl
	if serverUrl == "" {
		serverUrl = defaultServerURL()
		if serverUrl == "" {
			log.Printf("Warning: Default server URL is not set for environment %s", environment)
		}
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
