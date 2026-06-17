// senhub-agent/internal/agent/services/configuration/localConfiguration_watcher.go
package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchConfigFile monitors the configuration file AND the multi-file
// fragment directories (probes.d/, strategies.d/) for changes. Any
// event from either source triggers a reload; the loader re-merges
// fragments and the observers fire if the merged data changed.
//
// Events that come from inside the fragment directories carry the
// fragment file path in event.Name. We filter out dotfiles and
// `*.disabled` entries (the loader ignores them anyway) so a
// `mv 00-host.yaml 00-host.yaml.disabled` triggers exactly one
// useful reload rather than two noisy ones.
func (lc *LocalConfiguration) watchConfigFile() {
	defer lc.watcherWG.Done()
	lc.logger.Debug().Msg("Started configuration file watching goroutine")

	for {
		select {
		case <-lc.stopCh:
			lc.logger.Debug().Msg("Configuration file watching stopped (shutdown)")
			return

		case <-lc.quitChannel:
			lc.logger.Debug().Msg("Configuration file watching stopped")
			return

		case event, ok := <-lc.watcher.Events:
			if !ok {
				lc.logger.Debug().Msg("File watcher events channel closed")
				return
			}

			if shouldIgnoreEvent(event) {
				lc.logger.Debug().
					Str("event", event.String()).
					Msg("Configuration event ignored (dotfile / .disabled)")
				continue
			}

			lc.logger.Debug().
				Str("event", event.String()).
				Msg("Configuration event received")

			isMainFile := event.Name == lc.configPath

			// Handle various file change events
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Chmod) {
				lc.logger.Info().
					Str("path", event.Name).
					Str("event_type", event.Op.String()).
					Bool("main_file", isMainFile).
					Msg("Configuration changed, reloading...")

				// Small delay to ensure file write is complete
				time.Sleep(200 * time.Millisecond)

				// For events on the MAIN file only: an editor that
				// deletes-then-recreates the file briefly produces a
				// missing-file state. Skip the reload — attemptRewatch
				// covers that path.
				if isMainFile {
					if _, err := os.Stat(lc.configPath); os.IsNotExist(err) {
						lc.logger.Warn().Msg("Configuration file was deleted, skipping reload")
						continue
					}
				}

				if err := lc.reloadConfiguration(); err != nil {
					lc.logger.Error().
						Err(err).
						Msg("Failed to reload configuration")
				} else {
					lc.logger.Info().Msg("Configuration reloaded successfully")
				}
			} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				if isMainFile {
					// Editor delete-then-recreate dance on the main
					// file. Try to re-add the watch after the file
					// reappears.
					lc.logger.Warn().
						Str("event_type", event.Op.String()).
						Msg("Configuration file was removed or renamed, attempting to re-watch...")
					lc.watcherWG.Add(1)
					go lc.attemptRewatch()
				} else {
					// Fragment file removed or renamed. The directory
					// watch is still active; reload so the operator
					// sees the new effective configuration immediately.
					lc.logger.Info().
						Str("path", event.Name).
						Str("event_type", event.Op.String()).
						Msg("Fragment file removed/renamed, reloading...")
					time.Sleep(200 * time.Millisecond)
					if err := lc.reloadConfiguration(); err != nil {
						lc.logger.Error().Err(err).Msg("Failed to reload configuration after fragment change")
					}
				}
			}

		case err, ok := <-lc.watcher.Errors:
			if !ok {
				lc.logger.Debug().Msg("File watcher errors channel closed")
				return
			}
			lc.logger.Warn().
				Err(err).
				Msg("Configuration file watcher error")
		}
	}
}

// shouldIgnoreEvent returns true for events the loader will not act
// on: dotfiles (e.g. an editor's `.swp`), `*.disabled` fragments, and
// non-YAML files. fsnotify on a directory fires for every entry —
// without this filter, an editor saving its swap file would trigger
// a reload-and-no-op cycle on every keystroke.
func shouldIgnoreEvent(event fsnotify.Event) bool {
	base := filepath.Base(event.Name)
	if strings.HasPrefix(base, ".") {
		return true
	}
	if strings.HasSuffix(base, ".disabled") {
		return true
	}
	// The main config file may be agent.yaml or agent-config.yaml or
	// any path the operator chose — we can't filter on extension at
	// the top level. But fragment files MUST be .yaml/.yml; anything
	// else dropped in probes.d/ or strategies.d/ would be ignored
	// by the loader. Filter them here so an editor's `~` backup
	// doesn't cause noise.
	dir := filepath.Base(filepath.Dir(event.Name))
	if dir == "probes.d" || dir == "strategies.d" {
		ext := strings.ToLower(filepath.Ext(base))
		if ext != ".yaml" && ext != ".yml" {
			return true
		}
	}
	return false
}

// reloadConfiguration reloads the configuration and notifies observers
func (lc *LocalConfiguration) reloadConfiguration() error {
	// Store previous configuration for comparison
	previousData := *lc.snapshot()

	// Load new configuration
	if err := lc.loadConfiguration(); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	currentData := *lc.snapshot()

	// Check if configuration actually changed
	if lc.hasConfigurationChanged(previousData, currentData) {
		lc.logger.Info().
			Any("old_storage", previousData.Storage).
			Any("new_storage", currentData.Storage).
			Any("old_probes", previousData.Probes).
			Any("new_probes", currentData.Probes).
			Msg("Configuration changes detected, notifying observers")

		// Notify all observers about the configuration change
		lc.eventNotifier.NotifyObservers("Configuration file changed")
	} else {
		lc.logger.Info().Msg("Configuration file changed but content is identical")
	}

	return nil
}

// hasConfigurationChanged compares two configurations for differences
func (lc *LocalConfiguration) hasConfigurationChanged(old, new LocalConfigurationData) bool {
	// Use reflect.DeepEqual for comprehensive comparison
	// This detects ALL changes including:
	// - Added/removed probes or storage
	// - Modified parameters (interval, url, etc.)
	// - Modified probe types
	// - Modified storage configuration
	// - Cache configuration changes

	if !reflect.DeepEqual(old.Storage, new.Storage) {
		lc.logger.Debug().Msg("Storage configuration changed")
		return true
	}

	if !reflect.DeepEqual(old.Probes, new.Probes) {
		lc.logger.Debug().Msg("Probes configuration changed")
		return true
	}

	if !reflect.DeepEqual(old.Cache, new.Cache) {
		lc.logger.Debug().Msg("Cache configuration changed")
		return true
	}

	// Auto-update config changes don't require reload of probes/storage
	// but we could handle them separately if needed

	return false
}

// attemptRewatch tries to re-add the configuration file to the watcher
// This handles editors that delete and recreate files during save operations
func (lc *LocalConfiguration) attemptRewatch() {
	defer lc.watcherWG.Done()
	maxRetries := 5
	retryDelay := 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		// Wait for the file to be recreated — abortable so Shutdown
		// never waits half a rewatch cycle (joinable lifecycle, #268).
		select {
		case <-lc.stopCh:
			return
		case <-time.After(retryDelay):
		}

		// Check if file exists
		if _, err := os.Stat(lc.configPath); err == nil {
			// File exists, try to add it back to watcher
			if err := lc.watcher.Add(lc.configPath); err != nil {
				lc.logger.Warn().
					Err(err).
					Int("attempt", i+1).
					Msg("Failed to re-watch configuration file")
			} else {
				lc.logger.Info().
					Int("attempt", i+1).
					Msg("Successfully re-added configuration file to watcher")

				// File is back, try to reload configuration
				if err := lc.reloadConfiguration(); err != nil {
					lc.logger.Error().
						Err(err).
						Msg("Failed to reload configuration after re-watch")
				} else {
					lc.logger.Info().Msg("Configuration reloaded successfully after re-watch")
				}
				return
			}
		} else {
			lc.logger.Debug().
				Int("attempt", i+1).
				Msg("Configuration file not yet recreated, retrying...")
		}

		// Increase delay for next retry
		retryDelay *= 2
	}

	lc.logger.Error().
		Int("max_retries", maxRetries).
		Msg("Failed to re-watch configuration file after maximum retries")
}
