package auto_update

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"

	"github.com/hashicorp/go-version"
	"github.com/minio/selfupdate"
	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

var (
	DEFAULT_REGISTRY_URL       = "https://eu-west-1.intake-dev.senhub.io/"
	VERSION_METADATA_LIST_PATH = "/releases/releases.json"
	VERSION_METADATA_PATH      = "/download/%s/metadata.json"
	VERSION_BINARY_PATH        = "/download/%s/%s"
)

// Register an event on remote config change
// This function checks for update and applies the update if required

type AutoUpdate interface {
	GetName() string
	Start(quitChannel chan struct{}) error
	Shutdown(ctx context.Context) error
	Update(expectedVersion string, registryUrl ...string) error
}

type AutoUpdateConfig struct {
	RemoteConfig *configuration.RemoteConfiguration
	Logger       *logger.Logger
}

type autoUpdate struct {
	remoteConfig *configuration.RemoteConfiguration
	logger       *logger.Logger
	httpClient   *http.Client
}

func NewAutoUpdate(config AutoUpdateConfig) AutoUpdate {
	localLogger := config.Logger.With().Str("service", "auto_update").Logger()

	httpClient := httpretry.NewDefaultClient(
		httpretry.WithMaxRetryCount(3),
	)

	return &autoUpdate{
		remoteConfig: config.RemoteConfig,
		logger:       &localLogger,
		httpClient:   httpClient,
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

func (a *autoUpdate) Update(expectedVersionStr string, registryUrl ...string) error {
	var registry string
	if len(registryUrl) > 0 {
		registry = registryUrl[0]
	}

	expectedVersion := a.getExpectedVersion(
		expectedVersionStr,
		a.GetRegistryUrl(registry),
	)

	currentVersionStr := cliArgs.Version
	if expectedVersion == "" || expectedVersion == cliArgs.Version {
		a.logger.Info().Msg("No update required")
		return nil
	}

	a.logger.Info().
		Str("current_version", currentVersionStr).
		Str("expected_version", expectedVersion).
		Msg("Update required")

	binaryUrl, err := a.GetBinaryUrl(registry, expectedVersion)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to generate binary URL")
		return err
	}

	a.logger.Debug().
		Str("binary_url", binaryUrl).
		Msg("Downloading binary")

	err = doUpdate(binaryUrl)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to update binary")
	}

	return nil
}

func doUpdate(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	err = selfupdate.Apply(resp.Body, selfupdate.Options{})
	return err
}

func (a *autoUpdate) GetRegistryUrl(registryUrl string) string {
	if registryUrl == "" {
		return DEFAULT_REGISTRY_URL
	}
	return registryUrl
}

func (a *autoUpdate) getExpectedVersionFromConfig() string {
	expectedVersion := a.remoteConfig.GetConfiguration().Agent.Version
	registryUrl := a.remoteConfig.GetConfiguration().Agent.RegistryUrl

	return a.getExpectedVersion(expectedVersion, registryUrl)
}

func (a *autoUpdate) getExpectedVersion(expectedVersionStr string, registryUrl string) string {
	currentVersionStr := cliArgs.Version
	registryUrl = a.GetRegistryUrl(registryUrl)

	if expectedVersionStr == "" {
		return currentVersionStr
	}

	// In case expected version is an alias, try to get latest version
	expectedVersionMetadata, err := fetchVersionMetadata(
		a.httpClient,
		registryUrl,
		expectedVersionStr,
	)
	if err != nil {
		fmt.Println(err)
	}

	// Given there is a matching version metadata, use the version from the
	// metadata
	if expectedVersionMetadata != nil {
		fmt.Println(expectedVersionMetadata.Version)
		// There is an exact match
		return expectedVersionMetadata.Version
	}

	constraint, err := version.NewConstraint(expectedVersionStr)
	if err != nil {
		a.logger.Error().
			Str("expected_version", expectedVersionStr).
			Err(err).
			Msg("Failed to parse version constraint")

		// Unable to parse version constraint
		// Assume no update required
		return currentVersionStr
	}

	currentVersion, err := version.NewVersion(currentVersionStr)
	if err != nil {
		a.logger.Error().
			Str("current_version", currentVersionStr).
			Err(err).
			Msg("Failed to parse current version")
		return currentVersionStr
	}

	if constraint.Check(currentVersion) {
		return currentVersionStr
	}

	a.logger.Info().
		Str("current_version", currentVersionStr).
		Str("expected_version", expectedVersionStr).
		Msg("Update required")

	metadata, err := FetchBestMatchingVersion(
		a.httpClient,
		registryUrl,
		constraint,
	)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to fetch best matching version")
		return currentVersionStr
	}

	if metadata == nil {
		return currentVersionStr
	}
	return metadata.Version
}

func (a *autoUpdate) getBinaryNameForOptions(os, arch string) string {
	suffix := ""
	if os == "windows" {
		suffix = ".exe"
	}

	return fmt.Sprintf("senhub-agent_%s_%s%s", os, arch, suffix)
}

func (a *autoUpdate) GetBinaryUrl(
	registryUrl string,
	version string,
) (string, error) {
	arch := runtime.GOARCH
	os := runtime.GOOS
	registryUrl = a.GetRegistryUrl(registryUrl)

	filename := a.getBinaryNameForOptions(os, arch)
	return url.JoinPath(
		registryUrl,
		fmt.Sprintf(VERSION_BINARY_PATH, FormatVersionForUrl(version), filename),
	)
}
