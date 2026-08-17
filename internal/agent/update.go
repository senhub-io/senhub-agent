package agent

import (
	"fmt"
	"os"
	"sort"

	goversion "github.com/hashicorp/go-version"
	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/logger"
)

// UpdateOption customises the update command.
type UpdateOption func(*updateOptions)

type updateOptions struct {
	afterInstall func(newBinary string) (string, error)
}

// AfterInstall registers a hook run once the new binary is on disk and
// before the operator-facing summary is printed. It receives the path the
// new binary was written to and returns an extra path it reconciled, or
// "" when it had nothing to do.
//
// The caller in app uses it to propagate the new binary to the
// systemd-managed service copy: only that package knows about units, and
// it cannot be called from here (app imports this package, not the
// reverse). A failing hook fails the command — an update that left the
// service on the old binary did not do what the operator asked (#723).
func AfterInstall(hook func(newBinary string) (string, error)) UpdateOption {
	return func(o *updateOptions) { o.afterInstall = hook }
}

// UpdateAgent handles the "update" CLI command.
// With --list: lists available versions.
// With a version argument: installs that version.
// Without arguments: checks for newer version and prompts.
func UpdateAgent(args *cliArgs.ParsedArgs, opts ...UpdateOption) {
	log := logger.NewLogger(args)

	options := updateOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	updater := auto_update.NewAutoUpdate(auto_update.AutoUpdateConfig{
		Logger: log,
		DryRun: args.DryRun,
		// This is the operator running `sudo senhub-agent update`, the only
		// path allowed to install the binary on Linux (#794).
		OperatorDriven: true,
	})

	// Read include_beta from config file
	includeBeta := readIncludeBetaFromConfig(args.ConfigPath)

	// Also include beta if explicitly requesting a beta version
	if args.WantedVersion != "" && auto_update.IsBetaVersion(args.WantedVersion) {
		includeBeta = true
	}

	// --list: show available versions
	if args.WantedVersion == "list" {
		listVersions(updater, includeBeta, log)
		return
	}

	// Explicit version requested
	if args.WantedVersion != "" {
		installVersion(updater, args, log, options)
		return
	}

	// Default: check for new version
	checkAndPrompt(updater, includeBeta, log)
}

func listVersions(updater auto_update.AutoUpdate, includeBeta bool, log *logger.Logger) {
	fmt.Println()
	fmt.Println("Fetching available versions...")
	fmt.Println()

	// Fetch stable
	stable, err := updater.ListAvailableVersions(false)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch versions")
		os.Exit(1)
	}

	// Fetch beta only if enabled
	var beta []auto_update.VersionMetadata
	if includeBeta {
		beta, err = updater.ListAvailableVersions(true)
		if err != nil {
			log.Debug().Err(err).Msg("Failed to fetch beta versions")
		}
	}

	// Sort stable versions
	sortVersions(stable)

	fmt.Printf("  Current version: %s\n\n", cliArgs.Version)

	fmt.Println("  Stable releases:")
	for _, v := range stable {
		if v.Name == "latest" {
			continue
		}
		marker := "  "
		if v.Version == cliArgs.Version {
			marker = "* "
		}
		fmt.Printf("    %s%s\n", marker, v.Version)
	}

	// Extract beta-only versions
	stableSet := make(map[string]bool)
	for _, v := range stable {
		stableSet[v.Version] = true
	}
	var betaOnly []auto_update.VersionMetadata
	for _, v := range beta {
		if !stableSet[v.Version] && v.Name != "latest" {
			betaOnly = append(betaOnly, v)
		}
	}

	if len(betaOnly) > 0 {
		sortVersions(betaOnly)
		fmt.Println()
		fmt.Println("  Beta releases:")
		for _, v := range betaOnly {
			marker := "  "
			if v.Version == cliArgs.Version {
				marker = "* "
			}
			fmt.Printf("    %s%s\n", marker, v.Version)
		}
	}

	fmt.Println()
	fmt.Println("  To install: senhub-agent update <version>")
	fmt.Println()
}

func installVersion(updater auto_update.AutoUpdate, args *cliArgs.ParsedArgs, log *logger.Logger, options updateOptions) {
	fmt.Printf("Updating from %s to %s...\n", cliArgs.Version, args.WantedVersion)

	// Resolve our own path BEFORE the update. selfupdate replaces the
	// binary by renaming the running file out of the way and writing the
	// new one at this path, so afterwards os.Executable() (via
	// /proc/self/exe) resolves to the renamed — and usually already
	// deleted — old inode, not to the new release.
	self, exeErr := os.Executable()
	if exeErr != nil {
		log.Error().Err(exeErr).Msg("Cannot resolve the running executable")
		os.Exit(1)
	}

	updated, err := updater.Update(args.WantedVersion, args.UpdateRegistryUrl)
	if err != nil {
		log.Error().Err(err).Msg("Update failed")
		os.Exit(1)
	}

	// The hook runs even when the binary was already at the wanted version:
	// that is exactly the state a host lands in once the service copy has
	// self-updated past the CLI one, and re-running `update` is how an
	// operator repairs it. A dry run writes nothing, so it must not
	// propagate the still-current binary either.
	reconciled := ""
	if options.afterInstall != nil && !args.DryRun {
		reconciled, err = options.afterInstall(self)
		if err != nil {
			log.Error().Err(err).Msg("Updating the systemd-managed service binary failed")
			os.Exit(1)
		}
	}

	switch {
	case updated && reconciled != "":
		fmt.Println("Update installed:")
		fmt.Printf("  %s (CLI)\n", self)
		fmt.Printf("  %s (systemd service)\n", reconciled)
		fmt.Println("Restart the agent to use the new version (MSI-managed installs restart automatically).")
	case updated:
		fmt.Printf("Update installed: %s\n", self)
		fmt.Println("Restart the agent to use the new version (MSI-managed installs restart automatically).")
	case reconciled != "":
		fmt.Printf("Already up to date; the systemd service binary was stale and has been refreshed:\n  %s\n", reconciled)
		fmt.Println("Restart the agent to use the new version.")
	default:
		fmt.Println("Already up to date.")
	}
}

func checkAndPrompt(updater auto_update.AutoUpdate, includeBeta bool, log *logger.Logger) {
	newer, err := updater.CheckForNewVersion(includeBeta)
	if err != nil {
		log.Error().Err(err).Msg("Version check failed")
		os.Exit(1)
	}

	if newer == nil {
		fmt.Printf("Agent %s is up to date.\n", cliArgs.Version)
		return
	}

	fmt.Printf("Current version:   %s\n", cliArgs.Version)
	fmt.Printf("Available version: %s\n", newer.Version)
	fmt.Println()
	fmt.Printf("Run 'senhub-agent update %s' to install.\n", newer.Version)
}

// readIncludeBetaFromConfig reads include_beta from the YAML config file
func readIncludeBetaFromConfig(configPath string) bool {
	if configPath == "" {
		configPath = "./agent-config.yaml"
	}
	data, err := os.ReadFile(configPath) // #nosec G304 - CLI tool reads user config
	if err != nil {
		return false
	}
	var raw struct {
		AutoUpdate struct {
			IncludeBeta bool `yaml:"include_beta"`
		} `yaml:"auto_update"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false
	}
	return raw.AutoUpdate.IncludeBeta
}

func sortVersions(versions []auto_update.VersionMetadata) {
	sort.Slice(versions, func(i, j int) bool {
		vi, err1 := goversion.NewVersion(versions[i].Version)
		vj, err2 := goversion.NewVersion(versions[j].Version)
		if err1 != nil || err2 != nil {
			return versions[i].Version < versions[j].Version
		}
		return vi.LessThan(vj)
	})
}
