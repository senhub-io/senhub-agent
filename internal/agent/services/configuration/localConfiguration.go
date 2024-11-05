package configuration

// LocalConfiguration is an interface for local configuration.
// Local configuration is read from a local file,
// environment variables and cli arguments.

type LocalConfiguration interface {
}

type localConfiguration struct {
}

func NewLocalConfiguration() LocalConfiguration {
	return &localConfiguration{}
}
