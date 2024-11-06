package configuration

// LocalConfiguration is an interface for local configuration.
// Local configuration is read from a local file,
// environment variables and cli arguments.

type LocalConfiguration interface {
	GetAuthenticationKey() string
	GetServerUrl() string
}

type localConfiguration struct {
	AuthenticationKey string
	ServerUrl         string
}

func NewLocalConfiguration() LocalConfiguration {
	// TODO For now configuration is static
	// This should be read from arguments, environment variables and/or a local file
	return &localConfiguration{
		AuthenticationKey: "default_key",
		ServerUrl:         "https://nats.sensorfactory.eu:8443",
	}
}

func (l localConfiguration) GetAuthenticationKey() string {
	return l.AuthenticationKey
}
func (l localConfiguration) GetServerUrl() string {
	return l.ServerUrl
}
