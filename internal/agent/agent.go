// Package agent manages the core agent lifecycle and services orchestration.
//
// Configuration is read from local YAML files (the `agent.yaml` +
// `probes.d/` + `strategies.d/` multi-file layout, or a single file).
// Data-push strategies (senhub, otlp, http, prtg, …) send metrics to
// whichever back-end the operator wires up in `strategies.d/`.
package agent

import (
	"context"
	"os"

	agentCliArgs "senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/sensor"
)

// Service defines interface for agent services lifecycle
type Service interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

// Agent defines interface for main agent operations
type Agent interface {
	Start() error
	Shutdown(context.Context) error
}

type agent struct {
	startedServices    *[]Service
	messageChannel     chan struct{}
	logger             *logger.Logger
	agentConfiguration configuration.AgentConfiguration
	localConfiguration *configuration.LocalConfiguration
	store              data_store.DataStore
	sensors            sensor.Sensor
	updater            auto_update.AutoUpdate
	// exitFn is called by handleStartError with exit code 1. It defaults to
	// os.Exit; tests inject a no-op to capture the call without aborting.
	exitFn func(int)
}

// NewAgent initializes a new agent using CLI arguments parsed from os.Args.
func NewAgent() Agent {
	args := agentCliArgs.MustParse()
	return NewAgentWithArgs(args)
}

// NewAgentWithArgs initializes a new agent with explicit CLI arguments.
//
// The bring-up wires the local configuration loader, the data store
// (which fans out to the configured strategies) and the sensor pool.
// Auto-update is enabled when the loaded configuration's auto_update
// block has Enabled = true.
func NewAgentWithArgs(args *agentCliArgs.ParsedArgs) Agent {
	logger := logger.NewLogger(args)
	logger.Debug().Any("args", args).Msg("Agent configuration")
	logger.Info().Msg("Initializing agent")

	localConfiguration := configuration.NewLocalConfiguration(args, logger)

	agentKey := localConfiguration.GetAgentKey()
	if agentKey == "" {
		// LocalConfiguration generates a UUID on first start; this
		// branch only fires if `agent run` is invoked before
		// `agent install` (so the configuration file does not yet
		// exist on disk). Downstream code expects a non-empty value
		// so we substitute a clearly recognisable placeholder.
		agentKey = "pending"
	}

	agentConfiguration := configuration.NewAgentConfigurationWithLocal(
		agentKey,
		localConfiguration,
		logger,
	)

	store := data_store.NewDataStore(
		agentConfiguration,
		localConfiguration,
		logger,
	)

	sensors := sensor.NewSensor(
		store.GetCallback(),
		localConfiguration,
		logger,
	)

	var updater auto_update.AutoUpdate
	autoUpdateConfig := localConfiguration.GetAutoUpdateConfig()
	if autoUpdateConfig.Enabled {
		logger.Info().
			Str("url", autoUpdateConfig.URL).
			Bool("enabled", autoUpdateConfig.Enabled).
			Msg("Auto-update enabled")

		// Fail loud, not silent: under the hardened non-root unit a
		// root-owned binary can never be replaced in place, so every
		// hourly cycle would fail at write time with a permission error
		// (#377). Surface one clear diagnostic at startup instead.
		if warn := auto_update.CheckBinaryReplaceable(); warn != "" {
			logger.Warn().Msg(warn)
		}

		updater = auto_update.NewAutoUpdate(auto_update.AutoUpdateConfig{
			ConfigSource: localConfiguration,
			Logger:       logger,
			DryRun:       false,
		})
	} else {
		logger.Info().Msg("Auto-update disabled")
	}

	return agent{
		startedServices:    &[]Service{},
		messageChannel:     make(chan struct{}),
		logger:             logger,
		agentConfiguration: agentConfiguration,
		localConfiguration: localConfiguration,
		store:              store,
		sensors:            sensors,
		updater:            updater,
		exitFn:             os.Exit,
	}
}

func (a agent) Start() error {
	servicesToStart := []Service{
		a.localConfiguration,
		a.store,
		a.sensors,
	}
	if a.updater != nil {
		a.logger.Info().Msg("Adding auto-updater to services")
		servicesToStart = append(servicesToStart, a.updater)
	}

	var errors []error
	for _, service := range servicesToStart {
		a.logger.Debug().
			Str("service", service.GetName()).
			Msg("Starting service")

		if err := service.Start(a.messageChannel); err != nil {
			a.logger.Error().
				Str("service", service.GetName()).
				Err(err).
				Msg("Failed to start service")
			errors = append(errors, err)
		} else {
			a.logger.Info().
				Str("service", service.GetName()).
				Msg("Service started")
			*a.startedServices = append(*a.startedServices, service)
		}
	}

	if len(errors) > 0 {
		a.handleStartError()
	}

	// Passive version check (non-blocking, log only). The auto-updater
	// (when wired) performs the active upgrade; this only logs the
	// drift between the embedded build tag and the latest release.
	a.checkForNewVersionAtStartup()

	return nil
}

// checkForNewVersionAtStartup logs if a newer release is available on
// the configured update URL. Diagnostic only — the active upgrade is
// driven by the auto-updater service (when enabled in config).
func (a agent) checkForNewVersionAtStartup() {
	if a.updater == nil {
		// No updater configured — instantiate a one-shot dry-run
		// checker so we can still log drift.
		updater := auto_update.NewAutoUpdate(auto_update.AutoUpdateConfig{
			Logger: a.logger,
			DryRun: true,
		})
		a.doVersionCheck(updater)
		return
	}
	a.doVersionCheck(a.updater)
}

func (a agent) doVersionCheck(updater auto_update.AutoUpdate) {
	includeBeta := false
	if a.localConfiguration != nil {
		includeBeta = a.localConfiguration.GetAutoUpdateConfig().IncludeBeta
	}

	newer, err := updater.CheckForNewVersion(includeBeta)
	if err != nil {
		a.logger.Debug().Err(err).Msg("Version check failed (non-critical)")
		return
	}
	if newer != nil {
		a.logger.Warn().
			Str("current", agentCliArgs.Version).
			Str("available", newer.Version).
			Msg("A newer version is available. Run 'senhub-agent update --list' to see all versions.")
	} else {
		a.logger.Info().
			Str("version", agentCliArgs.Version).
			Msg("Agent is up to date")
	}
}

func (a agent) Shutdown(ctx context.Context) error {
	close(a.messageChannel)

	// Tear down in reverse start order: sensors (producers) before the
	// data store (consumer), so that the final collection cycle drains
	// cleanly before the store closes its strategies.
	services := *a.startedServices
	var errors []error
	for i := len(services) - 1; i >= 0; i-- {
		service := services[i]
		a.logger.Debug().
			Str("service", service.GetName()).
			Msg("Shutting down service")

		if err := service.Shutdown(ctx); err != nil {
			a.logger.Error().
				Str("service", service.GetName()).
				Err(err).
				Msg("Failed to shut down service")
			errors = append(errors, err)
		} else {
			a.logger.Info().
				Str("service", service.GetName()).
				Msg("Service shut down")
		}
	}

	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

func (a agent) handleStartError() {
	// Exit non-zero so the service manager (systemd, SCM) knows the
	// start failed and does not re-fork indefinitely on a permanent
	// config error. Sending SIGINT to self exits 0 on Linux, which
	// defeats Restart=always and StartLimitBurst protection.
	a.logger.Error().Msg("One or more services failed to start; exiting with error")
	if a.exitFn != nil {
		a.exitFn(1)
	} else {
		os.Exit(1)
	}
}
