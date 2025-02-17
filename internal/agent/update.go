package agent

import (
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
)

func UpdateAgent(args *cliArgs.ParsedArgs) {
	logger := logger.NewLogger(args)

	logger.Debug().Any("args", args).Msg("Start update with args")

	agentConfiguration := configuration.NewAgentConfiguration(
		args.AuthenticationKey,
		args.ServerUrl,
		logger,
	)

	senhubServer := server.NewServer(
		agentConfiguration.GetAuthenticationKey(),
		agentConfiguration.GetServerUrl(),
		logger,
	)

	remoteConfiguration := configuration.NewRemoteConfiguration(
		senhubServer,
		logger,
	)

	updater := auto_update.NewAutoUpdate(auto_update.AutoUpdateConfig{
		RemoteConfig: remoteConfiguration,
		Logger:       logger,
		DryRun:       args.DryRun,
	})

	// Ensure the configuration is available
	// This is not required given configuration comes from CLI args
	// err := remoteConfiguration.UpdateSync()

	updater.Update(args.WantedVersion, args.UpdateRegistryUrl)
}
