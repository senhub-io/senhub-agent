package auto_update

import (
	"context"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

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
