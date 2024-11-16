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

type AgentCliArgs struct {
	AuthenticationKey string `arg:"--authentication-key,env:SENHUB_KEY"`
	ServerUrl         string `arg:"--server-url,env:SENHUB_SERVER_URL" default:"https://nats.sensorfactory.eu:8443"`
	ShowVersion       bool   `arg:"-v,--version"`
}

type AgentConfiguration struct {
	AuthenticationKey string
	ServerUrl         string
}

func MustParse() AgentConfiguration {
	var args AgentCliArgs
	arg.MustParse(&args)

	config := AgentConfiguration{
		AuthenticationKey: args.AuthenticationKey,
		ServerUrl:         args.ServerUrl,
	}

	if args.ShowVersion && version != "" {
		log.Printf("Version: %s", version)
		os.Exit(0)
	}

	if args.ShowVersion && commit_hash != "" {
		log.Printf("Development version: %s", commit_hash)
		os.Exit(0)
	}

	// Validate configuration
	if config.AuthenticationKey == "" {
		log.Fatalf("missing required argument: --authentication-key")
		os.Exit(1)
	}

	return config
}
