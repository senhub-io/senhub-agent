package auto_update

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
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
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/validators"
)

var (
	DEFAULT_REGISTRY_URL            = "https://eu-west-1.intake.senhub.io/"
	VERSION_METADATA_LIST_PATH      = "/releases/releases.json"
	VERSION_METADATA_LIST_BETA_PATH = "/releases/beta/releases.json"
	VERSION_METADATA_PATH           = "/download/%s/metadata.json"
	VERSION_BINARY_PATH             = "/download/%s/%s"
	DEFAULT_UPDATE_CHECK_INTERVAL   = 1 * time.Hour
)

// ConfigSource defines interface for auto-update configuration access
// This allows auto-update to work with both local and remote configurations
type ConfigSource interface {
	// GetConfiguration returns the agent configuration data
	GetConfiguration() configuration.ConfigurationData
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
	if a.scheduler == nil {
		return nil
	}
	if err := (*a.scheduler).Shutdown(ctx); err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to shutdown scheduler")
		return fmt.Errorf("failed to shutdown auto-update scheduler: %w", err)
	}
	return nil
}

func (a *autoUpdate) onConfigChange(string) {
	if err := a.PeriodicalCheckForUpdate(); err != nil {
		a.logger.Error().
			Err(err).
			Msg("Failed to check for update during config change")
	}
	// Recreate scheduler if interval changed (without re-registering callback)
	if a.scheduler != nil && (*a.scheduler).GetInterval() != a.GetUpdateCheckInterval() {
		if err := (*a.scheduler).Shutdown(context.Background()); err != nil {
			a.logger.Error().
				Err(err).
				Msg("Failed to shutdown scheduler during config change")
		}
		a.createScheduler()
		if err := (*a.scheduler).Start(nil); err != nil {
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

	// Auto-update used to fire on any version-string mismatch — including
	// downgrades. An agent running a beta would silently revert to the
	// latest prod when the update server's "latest" alias resolved to a
	// release older than the running beta. shouldUpdateTo enforces
	// strict-greater-than per semver, with pre-release ordering honoured.
	ok, err := shouldUpdateTo(currentVersionStr, expectedVersion)
	if err != nil {
		a.logger.Warn().
			Str("current_version", currentVersionStr).
			Str("expected_version", expectedVersion).
			Err(err).
			Msg("Auto-update skipped: cannot parse version for comparison")
		return false, nil
	}
	if !ok {
		a.logger.Info().
			Str("current_version", currentVersionStr).
			Str("expected_version", expectedVersion).
			Msg("Auto-update skipped: expected version is not newer than current")
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

// doUpdate downloads the per-platform release ZIP, extracts the agent
// binary from inside it and applies the upgrade via selfupdate.Apply
// (atomic rename). Pre-0.2.0 the registry served raw binaries directly;
// 0.2.0+ wraps them in OS-named ZIPs so the file name matches what an
// operator downloading from the GitHub release would see, and so the
// release matrix stops shipping duplicate raw + zipped variants.
func (a *autoUpdate) doUpdate(downloadURL string) error {
	if a.dryRun {
		a.logger.Info().Msg("Dry run: Skipping update")
		return nil
	}

	if !strings.HasPrefix(downloadURL, "https://") {
		return fmt.Errorf("unsafe URL scheme - only HTTPS URLs are allowed: %s", downloadURL)
	}

	a.logger.Info().
		Str("download_url", downloadURL).
		Msg("Downloading update archive")

	// Use the retry-wrapped client (a.httpClient), not http.DefaultClient:
	// the default client has no timeout, so a CDN that accepts the
	// connection but never sends bytes would hang this goroutine
	// forever — and since the caller os.Exit(0)s on success, a hung
	// download silently stalls the hourly update check with no log.
	resp, err := a.httpClient.Get(downloadURL) // #nosec G107 - URL is validated for HTTPS scheme above
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download update: HTTP %d %s from %s",
			resp.StatusCode, resp.Status, downloadURL)
	}

	// The release pipeline serves ZIPs as application/zip; some CDNs
	// (GitHub's release storage in particular) report
	// application/octet-stream. Reject html / json which would
	// indicate we landed on an error page rather than the archive.
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/json") {
		return fmt.Errorf("invalid content type for archive download: %s (expected zip/octet-stream, got %s)",
			contentType, downloadURL)
	}

	// Buffer the whole download in memory so we can open it as a ZIP.
	// Release archives weigh ~10 MB compressed; that's an acceptable
	// peak. Refuse anything > 200 MB as a sanity check.
	const maxArchiveSize = 200 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize+1))
	if err != nil {
		return fmt.Errorf("reading update archive body: %w", err)
	}
	if int64(len(body)) > maxArchiveSize {
		return fmt.Errorf("update archive too large (> %d bytes)", maxArchiveSize)
	}
	if len(body) < 1024*1024 {
		return fmt.Errorf("update archive too small: %d bytes (expected at least 1MB)", len(body))
	}

	// Verify the detached minisign signature of the archive BEFORE
	// parsing it as a ZIP: integrity must not rest on TLS to a
	// config-settable registry URL (#266). A missing or invalid
	// signature is either a mis-published release or a tampered
	// artifact — refuse loudly either way.
	signature, err := a.fetchSignature(downloadURL + ".minisig")
	if err != nil {
		agentstate.IncrementUpdateRejected("signature_unavailable")
		return fmt.Errorf("update REJECTED — signature unavailable: %w", err)
	}
	if err := verifyArchiveSignature(body, signature); err != nil {
		agentstate.IncrementUpdateRejected("signature_invalid")
		return fmt.Errorf("update REJECTED — %w", err)
	}
	a.logger.Info().Msg("Update archive signature verified")

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("opening update archive as zip: %w", err)
	}

	// The archive ships exactly one entry: the agent binary. On
	// Windows it's named `senhub-agent.exe`; everywhere else
	// `senhub-agent`. We accept either to be tolerant of an
	// upgrade-from-different-OS-arch packaging mistake.
	var entry *zip.File
	for _, f := range zr.File {
		if f.Name == "senhub-agent" || f.Name == "senhub-agent.exe" {
			entry = f
			break
		}
	}
	if entry == nil {
		names := make([]string, 0, len(zr.File))
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		return fmt.Errorf("update archive does not contain senhub-agent binary; found entries: %v", names)
	}

	a.logger.Info().
		Str("content_type", contentType).
		Int("archive_size", len(body)).
		Uint64("binary_size", entry.UncompressedSize64).
		Msg("Update archive download validation passed, applying update")

	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("opening binary entry inside archive: %w", err)
	}
	defer rc.Close()

	if err := selfupdate.Apply(rc, selfupdate.Options{}); err != nil {
		return fmt.Errorf("failed to apply update: %w", err)
	}

	a.logger.Info().Msg("Update applied successfully")
	return nil
}

// fetchSignature downloads the detached minisign signature published
// next to the release archive (<archive-url>.minisig).
func (a *autoUpdate) fetchSignature(signatureURL string) ([]byte, error) {
	resp, err := a.httpClient.Get(signatureURL) // #nosec G107 - derived from the HTTPS-validated archive URL
	if err != nil {
		return nil, fmt.Errorf("downloading signature: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading signature: HTTP %d %s from %s",
			resp.StatusCode, resp.Status, signatureURL)
	}

	const maxSignatureSize = 64 * 1024
	sig, err := io.ReadAll(io.LimitReader(resp.Body, maxSignatureSize))
	if err != nil {
		return nil, fmt.Errorf("reading signature body: %w", err)
	}
	return sig, nil
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

// getBinaryNameForOptions returns the asset filename to download for
// the given OS/arch. 0.2.0+ ships ZIP archives named
// `senhub-agent-<os>-<arch>.zip`; the binary inside the archive is
// always `senhub-agent` (or `senhub-agent.exe` on Windows). Pre-0.2.0
// shipped raw binaries with an `_os_arch` suffix; the auto-updater
// will not be able to upgrade from a pre-0.2.0 install to a 0.2.0
// release using the registry directly — operators must replace the
// binary manually for that one transition.
func (a *autoUpdate) getBinaryNameForOptions(goos, goarch string) string {
	return fmt.Sprintf("senhub-agent-%s-%s.zip", goos, goarch)
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
