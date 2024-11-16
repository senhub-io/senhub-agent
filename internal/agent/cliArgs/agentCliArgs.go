package agentCliArgs

import (
	"log"
	"os"

	"github.com/alexflint/go-arg"
)

var (
	version     = "n/a"
	commit_hash = "n/a"
)

type CliArgs struct {
	Version *VersionSubcommandArgs `arg:"subcommand:version"`
	Agent   *StartSubcommandArgs   `arg:"subcommand:start"`
}

type VersionSubcommandArgs struct{}

type StartSubcommandArgs struct {
	AuthenticationKey string `arg:"required,--authentication-key,env:SENHUB_KEY"`
	ServerUrl         string `arg:"--server-url,env:SENHUB_SERVER_URL" default:"https://eu-west-1.intake.senhub.io"`
}

func MustParse() *StartSubcommandArgs {
	var args CliArgs

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
			return &startArgs
		default:
			p.WriteUsage(os.Stdout)
			os.Exit(1)
		}
	}

	switch {
	case args.Version != nil && version != "":
		log.Printf("Version: %s", version)
		os.Exit(0)
	case args.Version != nil:
		log.Printf("Development version: %s", commit_hash)
		os.Exit(0)
	case args.Agent != nil:
		return args.Agent
	default:
		log.Fatalf("unexpected error")
		os.Exit(1)
	}

	return nil
}
