package configuration

// LocalConfiguration is an interface for local configuration.
// Local configuration is read from a local file,
// environment variables and cli arguments.

type AgentConfiguration interface {
	GetAuthenticationKey() string
	GetServerUrl() string
}

type agentConfiguration struct {
	AuthenticationKey string
	ServerUrl         string
}

func NewAgentConfiguration(
	AuthenticationKey string,
	ServerUrl string,
) AgentConfiguration {
	return &agentConfiguration{
		AuthenticationKey: AuthenticationKey,
		ServerUrl:         ServerUrl,
	}
}

func (l *agentConfiguration) GetAuthenticationKey() string {
	return l.AuthenticationKey
}
func (l *agentConfiguration) GetServerUrl() string {
	return l.ServerUrl
}
