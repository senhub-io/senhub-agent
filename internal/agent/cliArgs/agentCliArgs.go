package agentCliArgs

import (
	"log"
	"os"

	"github.com/alexflint/go-arg"
)

// Those variables are set by the build system
var (
	version     = "n/a"
	commit_hash = "n/a"
	env         = "n/a"
)

type CliArgs struct {
	Version *VersionSubcommandArgs `arg:"subcommand:version" help:"Print version information and exit"`
	Agent   *StartSubcommandArgs   `arg:"subcommand:start" help:"Start the agent (default)"`
	Update  *UpdateSubcommandArgs  `arg:"subcommand:update" help:"Update the agent"`
	Verbose bool                   `arg:"-v,--verbose" help:"Enable verbose logging"`
}

type VersionSubcommandArgs struct{}
type UpdateSubcommandArgs struct{}

type StartSubcommandArgs struct {
	AuthenticationKey string `arg:"required,--authentication-key,env:SENHUB_KEY" help:"The authentication key for the agent"`
	ServerUrl         string `arg:"--server-url,env:SENHUB_SERVER_URL" default:"https://eu-west-1.intake.senhub.io" help:"The URL of senhub server to connect to"`
}

type ParsedArgs struct {
	AuthenticationKey string
	ServerUrl         string
	Verbose           bool
	// Can be production or development
	Env string
}

func MustParse() *ParsedArgs {
	var args CliArgs

	parsedEnv := env
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
			return parsedArgsFromStartArgs(&startArgs, parsedEnv, false)

		default:
			p.WriteUsage(os.Stdout)
			os.Exit(1)
		}
	}

	switch {
	case args.Version != nil && version != "":
		log.Printf("Version: %s", version)
		if parsedEnv == "development" {
			log.Printf("Development build")
		}
		os.Exit(0)
	case args.Version != nil:
		log.Printf("Development version: %s", commit_hash)
		if parsedEnv == "development" {
			log.Printf("Development build")
		}
		os.Exit(0)
	case args.Agent != nil:
		return parsedArgsFromStartArgs(args.Agent, parsedEnv, args.Verbose)
	case args.Update != nil:
		p.FailSubcommand("Update subcommand is not implemented yet.", "update")
		os.Exit(1)
	default:
		// No subcommand was provided.
		p.Fail("Run with --help for usage information.")
		os.Exit(1)
	}

	return nil
}

func parsedArgsFromStartArgs(args *StartSubcommandArgs, environment string, verbose bool) *ParsedArgs {
	return &ParsedArgs{
		AuthenticationKey: args.AuthenticationKey,
		ServerUrl:         args.ServerUrl,
		Env:               environment,
		Verbose:           verbose,
	}
}
