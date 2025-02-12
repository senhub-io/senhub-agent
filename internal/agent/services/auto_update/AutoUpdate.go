package auto_update

import (
	"context"

	"github.com/hashicorp/go-version"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

var DEFAULT_REGISTRY_URL = "https://eu-west-1.intake.senhub.io/dl/"

// Register an event on remote config change
// This function checks for update and applies the update if required

type AutoUpdate interface {
	GetName() string
	Start(quitChannel chan struct{}) error
	Shutdown(ctx context.Context) error
}

type AutoUpdateConfig struct {
	RemoteConfig *configuration.RemoteConfiguration
	Logger       *logger.Logger
}

type autoUpdate struct {
	remoteConfig *configuration.RemoteConfiguration
	logger       *logger.Logger
}

func NewAutoUpdate(config AutoUpdateConfig) AutoUpdate {
	localLogger := config.Logger.With().Str("service", "auto_update").Logger()

	return &autoUpdate{
		remoteConfig: config.RemoteConfig,
		logger:       &localLogger,
	}
}

func (a *autoUpdate) GetName() string {
	return "AutoUpdate"
}

func (a *autoUpdate) Start(quitChannel chan struct{}) error {
	return nil
}

func (a *autoUpdate) Shutdown(ctx context.Context) error {
	return nil
}

func (a *autoUpdate) shouldUpdate() bool {
	expectedVersionStr := a.remoteConfig.GetConfiguration().Agent.Version
	currentVersionStr := cliArgs.Version

	if expectedVersionStr == "" || expectedVersionStr == "latest" {
		// Assume this is the latest version
		// Fetch version from the registry
		expectedVersionStr = cliArgs.Version
	}

	constraint, err := version.NewConstraint(expectedVersionStr)
	if err != nil {
		a.logger.Error().
			Str("expected_version", expectedVersionStr).
			Err(err).
			Msg("Failed to parse version constraint")

		// Unable to parse version constraint
		// Assume no update required
		return false
	}

	currentVersion, err := version.NewVersion(currentVersionStr)
	if err != nil {
		a.logger.Error().
			Str("current_version", currentVersionStr).
			Err(err).
			Msg("Failed to parse current version")
		return false
	}

	if !constraint.Check(currentVersion) {
		a.logger.Info().
			Str("current_version", currentVersionStr).
			Str("expected_version", expectedVersionStr).
			Msg("Update required")
		return true
	}
	return false
}
