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

func NewLocalConfiguration(
	AuthenticationKey string,
	ServerUrl string,
) LocalConfiguration {
	return &localConfiguration{
		AuthenticationKey: AuthenticationKey,
		ServerUrl:         ServerUrl,
	}
}

func (l *localConfiguration) GetAuthenticationKey() string {
	return l.AuthenticationKey
}
func (l *localConfiguration) GetServerUrl() string {
	return l.ServerUrl
}
