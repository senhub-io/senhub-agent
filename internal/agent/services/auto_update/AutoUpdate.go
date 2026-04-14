package auto_update

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/minio/selfupdate"
	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/configParser"
	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/validators"
)

var (
	DEFAULT_REGISTRY_URL               = "https://eu-west-1.intake.senhub.io/"
	VERSION_METADATA_LIST_PATH         = "/releases/releases.json"
	VERSION_METADATA_LIST_BETA_PATH    = "/releases/beta/releases.json"
	VERSION_METADATA_PATH              = "/download/%s/metadata.json"
	VERSION_BINARY_PATH                = "/download/%s/%s"
	DEFAULT_UPDATE_CHECK_INTERVAL      = 1 * time.Hour
)

// ConfigSource defines interface for auto-update configuration access
// This allows auto-update to work with both local and remote configurations
type ConfigSource interface {
	// GetConfiguration returns the agent configuration data
	GetConfiguration() configuration.RemoteConfigurationData
	// OnConfigChanged registers a callback for configuration changes
	OnConfigChanged(callback func(string))
}

// Register an event on remote config change
// This function checks for update and applies the update if required

type AutoUpdate interface {
	GetName() string
	Start(quitChannel chan struct{}) error
	Shutdown(ctx context.Context) error
	Update(expectedVersion string, registryUrl ...string) (bool, error)
	CheckForNewVersion(includeBeta bool) (*VersionMetadata, error)
	ListAvailableVersions(includeBeta bool) ([]VersionMetadata, error)
}

type AutoUpdateConfig struct {
	ConfigSource ConfigSource
	Logger       *logger.Logger
	DryRun       bool
}

type autoUpdate struct {
	configSource ConfigSource
	logger       *logger.ModuleLogger
	httpClient   *http.Client
	scheduler    *periodic_scheduler.PeriodicScheduler
	dryRun       bool
}

func NewAutoUpdate(config AutoUpdateConfig) AutoUpdate {
	// Create module-specific logger for auto-update service
	moduleLogger := logger.NewModuleLogger(config.Logger, "service.auto_update")

	httpClient := httpretry.NewDefaultClient(
		httpretry.WithMaxRetryCount(3),
	)

	return &autoUpdate{
		configSource: config.ConfigSource,
		logger:       moduleLogger,
		httpClient:   httpClient,
		dryRun:       config.DryRun,
	}
}

func (a *autoUpdate) GetName() string {
	return "AutoUpdate"
}

func (a *autoUpdate) createScheduler() {
	var scheduler periodic_scheduler.PeriodicScheduler
	if a.scheduler != nil {
		scheduler := *a.scheduler
		// shutdown existing scheduler
		if err := scheduler.Shutdown(context.Background()); err != nil {
			a.logger.Error().
				Err(err).
				Msg("Failed to shutdown existing scheduler")
		}
	}
	scheduler = periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          a.GetUpdateCheckInterval(),
		MaxRetries:        3,
		ExecuteOnStart:    true,
		ExecuteOnShutdown: false,
		Execute:           a.PeriodicalCheckForUpdate,
	}, a.logger.Logger)

	a.scheduler = &scheduler
}

func (a *autoUpdate) Start(quitChannel chan struct{}) error {
	a.configSource.OnConfigChanged(a.onConfigChange)

	a.createScheduler()
	if err := (*a.scheduler).Start(quitChannel); err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to start scheduler")
		return fmt.Errorf("failed to start auto-update scheduler: %w", err)
	}

	return nil
}

func (a *autoUpdate) Shutdown(ctx context.Context) error {
	scheduler := *a.scheduler
	if scheduler != nil {
		if err := scheduler.Shutdown(ctx); err != nil {
			a.logger.Error().
				Err(err).
				Msg("Failed to shutdown scheduler")
			return fmt.Errorf("failed to shutdown auto-update scheduler: %w", err)
		}
	}
	return nil
}

func (a *autoUpdate) onConfigChange(string) {
	if err := a.PeriodicalCheckForUpdate(); err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to check for update during config change")
	}
	// In case interval config changed, recreate the scheduler
	scheduler := *a.scheduler
	if scheduler != nil && scheduler.GetInterval() != a.GetUpdateCheckInterval() {
		if err := scheduler.Shutdown(context.Background()); err != nil {
			a.logger.Error().
				Err(err).
				Msg("Failed to shutdown scheduler during config change")
		}
		a.createScheduler()
		if err := a.Start(nil); err != nil {
			a.logger.Error().
				Err(err).
				Msg("Failed to restart scheduler during config change")
		}
	}
}

func (a *autoUpdate) Update(expectedVersionStr string, registryUrl ...string) (bool, error) {
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
		return false, nil
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
		return false, err
	}

	a.logger.Debug().
		Str("binary_url", binaryUrl).
		Msg("Downloading binary")

	err = a.doUpdate(binaryUrl)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to update binary")
		return false, err
	}

	return true, nil
}

func (a *autoUpdate) doUpdate(url string) error {
	if a.dryRun {
		a.logger.Info().Msg("Dry run: Skipping update")
		return nil
	}

	// Validate URL safety
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("unsafe URL scheme - only HTTPS URLs are allowed: %s", url)
	}

	a.logger.Info().
		Str("download_url", url).
		Msg("Downloading update binary")

	resp, err := http.Get(url) // #nosec G107 - URL is validated for HTTPS scheme above
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	// Validate HTTP response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download update: HTTP %d %s from %s",
			resp.StatusCode, resp.Status, url)
	}

	// Validate Content-Type (should be application/octet-stream or similar for binaries)
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/json") {
		return fmt.Errorf("invalid content type for binary download: %s (expected binary, got %s)",
			contentType, url)
	}

	// Validate Content-Length (binary should be several MB)
	contentLength := resp.ContentLength
	if contentLength > 0 && contentLength < 1024*1024 { // Less than 1MB is suspicious
		return fmt.Errorf("binary too small: %d bytes (expected at least 1MB)", contentLength)
	}

	a.logger.Info().
		Str("content_type", contentType).
		Int64("content_length", contentLength).
		Msg("Binary download validation passed, applying update")

	err = selfupdate.Apply(resp.Body, selfupdate.Options{})
	if err != nil {
		return fmt.Errorf("failed to apply update: %w", err)
	}

	a.logger.Info().Msg("Update applied successfully")
	return nil
}

func (a *autoUpdate) PeriodicalCheckForUpdate() error {
	expectedVersion := a.configSource.GetConfiguration().Agent.Version
	registryUrl := a.configSource.GetConfiguration().Agent.RegistryUrl

	updateApplied, err := a.Update(
		expectedVersion,
		registryUrl,
	)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to update")

		return err
	}

	if updateApplied {
		// Now that the binary is updated, exit the process to restart the service.
		a.logger.Info().Msg("Exiting to apply update")
		os.Exit(0)
	}

	return nil
}

func (a *autoUpdate) GetRegistryUrl(registryUrl string) string {
	if registryUrl == "" {
		return DEFAULT_REGISTRY_URL
	}
	return registryUrl
}

func (a *autoUpdate) GetUpdateCheckInterval() time.Duration {
	rawValue := a.configSource.GetConfiguration().Agent.UpdateCheckInterval
	if rawValue == nil {
		return DEFAULT_UPDATE_CHECK_INTERVAL
	}
	if !validators.IsDuration(rawValue) {
		a.logger.Error().
			Interface("update_check_interval", rawValue).
			Msg("Failed to parse update check interval")
		return DEFAULT_UPDATE_CHECK_INTERVAL
	}

	updateCheckInterval, err := configParser.ParseDuration(rawValue)
	if err != nil {
		a.logger.Error().
			Interface("update_check_interval", rawValue).
			Err(err).
			Msg("Failed to parse update check interval")
		return DEFAULT_UPDATE_CHECK_INTERVAL
	}
	return updateCheckInterval
}

func (a *autoUpdate) getExpectedVersionFromConfig() string {
	expectedVersion := a.configSource.GetConfiguration().Agent.Version
	registryUrl := a.configSource.GetConfiguration().Agent.RegistryUrl

	return a.getExpectedVersion(expectedVersion, registryUrl)
}

func (a *autoUpdate) getExpectedVersion(expectedVersionStr string, registryUrl string) string {
	currentVersionStr := cliArgs.Version
	registryUrl = a.GetRegistryUrl(registryUrl)

	if expectedVersionStr == "" {
		return currentVersionStr
	}

	// Handle "latest" alias: fetch the highest version from the registry
	if expectedVersionStr == "latest" {
		a.logger.Info().Msg("Version alias 'latest' requested, fetching from registry")
		bestMatch, err := FetchBestMatchingVersion(
			a.httpClient,
			registryUrl,
			version.MustConstraints(version.NewConstraint(">= 0")),
		)
		if err != nil {
			a.logger.Warn().Err(err).Msg("Failed to fetch latest version from registry, skipping update")
			return currentVersionStr
		}
		if bestMatch == nil {
			a.logger.Warn().Msg("No versions found in registry")
			return currentVersionStr
		}
		a.logger.Info().Str("latest_version", bestMatch.Version).Msg("Resolved 'latest' to version")
		return bestMatch.Version
	}

	// In case expected version is an exact match, try to get its metadata
	expectedVersionMetadata, err := fetchVersionMetadata(
		a.httpClient,
		registryUrl,
		expectedVersionStr,
	)
	if err != nil {
		a.logger.Debug().Err(err).
			Str("expected_version", expectedVersionStr).
			Msg("Failed to fetch version metadata, trying as constraint")
	}

	// Given there is a matching version metadata, use the version from the metadata
	if expectedVersionMetadata != nil {
		a.logger.Debug().
			Str("resolved_version", expectedVersionMetadata.Version).
			Msg("Exact version match found in registry")
		return expectedVersionMetadata.Version
	}

	// Special handling for beta versions which don't parse as constraints
	if IsBetaVersion(expectedVersionStr) {
		a.logger.Info().
			Str("expected_version", expectedVersionStr).
			Msg("Detected beta version as target")
		return expectedVersionStr
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
	formattedVersion := FormatVersionForUrl(version)

	// Always use the same download path pattern, regardless of beta or not
	downloadPath := fmt.Sprintf(VERSION_BINARY_PATH, formattedVersion, filename)

	return url.JoinPath(registryUrl, downloadPath)
}

// CheckForNewVersion checks if a newer version is available without installing.
// Returns the latest available version metadata if newer than current, nil otherwise.
func (a *autoUpdate) CheckForNewVersion(includeBeta bool) (*VersionMetadata, error) {
	registryUrl := a.GetRegistryUrl("")
	versions, err := FetchAllVersions(a.httpClient, registryUrl, includeBeta)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch versions: %w", err)
	}

	latest := GetLatestVersion(versions)
	if latest == nil {
		return nil, nil
	}

	currentVer, err := version.NewVersion(cliArgs.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to parse current version %s: %w", cliArgs.Version, err)
	}

	latestVer, err := version.NewVersion(latest.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to parse latest version %s: %w", latest.Version, err)
	}

	if latestVer.GreaterThan(currentVer) {
		return latest, nil
	}

	return nil, nil
}

// ListAvailableVersions returns all versions available for update
func (a *autoUpdate) ListAvailableVersions(includeBeta bool) ([]VersionMetadata, error) {
	registryUrl := a.GetRegistryUrl("")
	return FetchAllVersions(a.httpClient, registryUrl, includeBeta)
}
