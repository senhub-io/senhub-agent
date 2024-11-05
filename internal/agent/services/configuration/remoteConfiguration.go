package configuration

import "context"

// RemoteConfiguration is an interface for remote configuration.
// Remote configuration is read periodically from the server.

type RemoteConfiguration interface {
	GetName() string
	Start() error
	Shutdown(context.Context) error
}

type remoteConfiguration struct {
}

func NewRemoteConfiguration() RemoteConfiguration {
	return &remoteConfiguration{}
}

func (c remoteConfiguration) GetName() string {
	return "RemoteConfiguration"
}

func (c remoteConfiguration) Start() error {
	return nil
}
func (c remoteConfiguration) Shutdown(ctx context.Context) error {
	return nil
}
