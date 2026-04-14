// Package sensor handles the configuration and lifecycle of monitoring probes
package sensor

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
)

// validProbeNameRegex matches URL-safe probe names: letters, digits, hyphens, underscores
var validProbeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// isValidProbeName checks if a probe name is safe for use in HTTP URLs
func isValidProbeName(name string) bool {
	return name != "" && validProbeNameRegex.MatchString(name)
}

// Sensor defines interface for starting and stopping probes
type Sensor interface {
	// GetName returns service identifier
	GetName() string
	// Start launches configured probes
	Start(chan struct{}) error
	// Shutdown gracefully stops probes
	Shutdown(context.Context) error
}

type sensor struct {
	startedProbes    []*probes.ProbePoller
	addDataPoint     data_store.AddCallback
	configProvider   configuration.ConfigurationProvider
	moduleLogger     *logger.ModuleLogger
	licenseValidator license.Validator
	license          *license.License
}

// NewSensor creates a new Sensor instance
func NewSensor(
	addDataPoint data_store.AddCallback,
	configProvider configuration.ConfigurationProvider,
	baseLogger *logger.Logger,
) Sensor {
	// Create module-specific logger for sensor service
	moduleLogger := logger.NewModuleLogger(baseLogger, "sensor")

	// Initialize license validator with embedded RSA public key
	var licenseValidator license.Validator
	jwtValidator, err := license.GetDefaultValidator(7) // 7-day grace period
	if err != nil {
		moduleLogger.Error().
			Err(err).
			Msg("Failed to initialize license validator - entering safe mode (free tier only)")
		// SECURITY: No fallback to MockValidator in production
		// If RSA public key fails to load, agent runs in free tier mode only
		licenseValidator = nil
	} else {
		licenseValidator = jwtValidator
	}

	// Try to load and validate license from configuration
	var validatedLicense *license.License
	config := configProvider.GetConfiguration()
	agentKey := config.Agent.AuthenticationKey
	if config.Agent.License != "" && licenseValidator != nil {
		moduleLogger.Info().Msg("License token found in configuration, validating...")
		lic, err := licenseValidator.ValidateLicense(config.Agent.License)
		if err != nil {
			moduleLogger.Warn().
				Err(err).
				Msg("⚠️ Invalid license token - only free tier probes will be available")
		} else if agentKey != "" && !license.VerifyBinding(config.Agent.License, agentKey, lic) {
			moduleLogger.Warn().
				Msg("⚠️ License is not bound to this agent key - only free tier probes will be available")
		} else {
			validatedLicense = lic
			tierName := string(lic.Tier)
			moduleLogger.Info().
				Str("tier", tierName).
				Bool("expired", lic.IsExpired).
				Time("expires_at", lic.ExpiresAt).
				Msg("✅ License validated successfully")

			if lic.IsExpired {
				if licenseValidator.IsInGracePeriod(lic) {
					gracePeriodEnd := lic.ExpiresAt.Add(time.Duration(lic.GracePeriodDays) * 24 * time.Hour)
					moduleLogger.Warn().
						Time("grace_period_ends", gracePeriodEnd).
						Msg("⚠️ License expired but in grace period")
				} else {
					moduleLogger.Error().Msg("❌ License expired and grace period ended - only free tier probes available")
					validatedLicense = nil // Disable license if expired outside grace period
				}
			}
		}
	} else {
		moduleLogger.Info().Msg("No license configured - using free tier (cpu, memory, logicaldisk, network)")
	}

	return &sensor{
		startedProbes:    []*probes.ProbePoller{},
		addDataPoint:     addDataPoint,
		configProvider:   configProvider,
		moduleLogger:     moduleLogger,
		licenseValidator: licenseValidator,
		license:          validatedLicense,
	}
}

func (s *sensor) GetName() string {
	return "Sensor"
}

// SyncConfiguration synchronizes probes with current configuration
func (s *sensor) SyncConfiguration() error {
	s.moduleLogger.Info().Msg("Starting configuration synchronization")

	// Re-validate license from current configuration (may have been loaded after initial startup)
	config := s.configProvider.GetConfiguration()
	if s.license == nil && config.Agent.License != "" && s.licenseValidator != nil {
		lic, err := s.licenseValidator.ValidateLicense(config.Agent.License)
		if err != nil {
			s.moduleLogger.Warn().Err(err).Msg("⚠️ Invalid license token during sync")
		} else {
			isExpired := lic.IsExpired && !s.licenseValidator.IsInGracePeriod(lic)
			if !isExpired {
				s.license = lic
				s.moduleLogger.Info().
					Str("tier", string(lic.Tier)).
					Strs("probes", lic.AuthorizedProbes).
					Msg("✅ License validated during configuration sync")
			}
		}
	}

	validProbeIds := []string{}
	probeConfigs := config.Probes

	s.moduleLogger.Info().
		Int("config_probes", len(probeConfigs)).
		Int("running_probes", len(s.startedProbes)).
		Msg("Configuration sync status")

	// Pre-validation: Check probe names
	seenNames := make(map[string]bool)
	for i, probeConfig := range probeConfigs {
		// Validate probe name is URL-safe (used in HTTP endpoints like /prtg/{name})
		if !isValidProbeName(probeConfig.Name) {
			s.moduleLogger.Error().
				Str("probe_name", probeConfig.Name).
				Msg("🚫 CONFIGURATION ERROR: Probe name must contain only letters, digits, hyphens and underscores (no spaces or special characters) — skipping probe")
			probeConfigs[i].Name = "" // Mark for skipping
			continue
		}
		if seenNames[probeConfig.Name] {
			s.moduleLogger.Warn().
				Str("probe_name", probeConfig.Name).
				Msg("⚠️ CONFIGURATION ERROR: Duplicate probe name in configuration - only the first instance will be used")
		}
		seenNames[probeConfig.Name] = true
	}

	// Phase 1: Start new probes
	processedNames := make(map[string]bool)
	for _, probeConfig := range probeConfigs {
		// Skip probes with invalid names (marked in pre-validation)
		if probeConfig.Name == "" {
			continue
		}
		// Skip duplicates (already warned in pre-validation)
		if processedNames[probeConfig.Name] {
			s.moduleLogger.Debug().
				Str("probe_name", probeConfig.Name).
				Msg("⏭️ Skipping duplicate probe name")
			continue
		}
		processedNames[probeConfig.Name] = true

		probeId := probes.GenerateProbeId(probeConfig)
		validProbeIds = append(validProbeIds, probeId)
		probeLogger := s.getLoggerForProbe(probeConfig)

		// Check if probe is already running (by ID)
		probeExists := false
		for _, startedProbe := range s.startedProbes {
			if startedProbe.ProbeId == probeId {
				probeExists = true
				break
			}
		}

		// Only start probe if it doesn't exist
		if !probeExists {
			s.moduleLogger.Info().
				Str("probe_id", probeId).
				Str("probe_name", probeConfig.Name).
				Any("probe_params", probeConfig.Params).
				Msg("Starting new probe")

			err := s.startProbe(probeConfig, nil)
			if err != nil {
				probeLogger.Error().Err(err).Msgf("Error starting probe")
			} else {
				s.moduleLogger.Info().
					Str("probe_id", probeId).
					Str("probe_name", probeConfig.Name).
					Msg("✅ Probe started successfully")
			}
		} else {
			s.moduleLogger.Debug().
				Str("probe_id", probeId).
				Str("probe_name", probeConfig.Name).
				Msg("Probe already running, skipping")
		}
	}

	// Phase 2: Stop removed probes
	activeProbes := []*probes.ProbePoller{}
	stoppedCount := 0

	for _, startedProbe := range s.startedProbes {
		found := false
		for _, validProbeId := range validProbeIds {
			if startedProbe.ProbeId == validProbeId {
				found = true
				break
			}
		}
		if found {
			// Keep active probes
			activeProbes = append(activeProbes, startedProbe)
		} else {
			// Shutdown and remove probe
			s.moduleLogger.Info().
				Str("probe_id", startedProbe.ProbeId).
				Str("probe_name", startedProbe.Probe.GetName()).
				Msg("Stopping removed probe")

			err := startedProbe.Shutdown(context.Background())
			if err != nil {
				s.moduleLogger.Error().
					Str("probe_id", startedProbe.ProbeId).
					Str("probe_name", startedProbe.Probe.GetName()).
					Err(err).
					Msg("Error stopping probe")
			} else {
				s.moduleLogger.Info().
					Str("probe_id", startedProbe.ProbeId).
					Str("probe_name", startedProbe.Probe.GetName()).
					Msg("🛑 Probe stopped successfully")
				stoppedCount++
			}
		}
	}

	// Update the slice to contain only active probes
	s.startedProbes = activeProbes

	s.moduleLogger.Info().
		Int("probes_started", len(validProbeIds)-len(activeProbes)+stoppedCount).
		Int("probes_stopped", stoppedCount).
		Int("probes_active", len(activeProbes)).
		Msg("Configuration synchronization completed")

	return nil
}

func (s *sensor) Start(quitChannel chan struct{}) error {
	s.moduleLogger.Info().Msg("Starting sensor")
	if err := s.SyncConfiguration(); err != nil {
		return fmt.Errorf("failed to sync configuration: %w", err)
	}

	s.moduleLogger.Info().Msg("Starting sensor service")
	s.configProvider.OnConfigChanged(func(string) {
		if err := s.SyncConfiguration(); err != nil {
			s.moduleLogger.Error().Err(err).Msg("Failed to sync configuration on config change")
		}
	})
	return nil
}

func (s *sensor) getLoggerForProbe(probeConfig configuration.ProbeConfig) *logger.Logger {
	// Return the base logger for probe constructor compatibility
	// Probes will create their own ModuleLogger from this base logger
	return s.moduleLogger.Logger
}

func (s *sensor) startProbe(probeConfig configuration.ProbeConfig, quitChannel chan struct{}) error {
	probeId := probes.GenerateProbeId(probeConfig)

	for _, startedProbe := range s.startedProbes {
		if startedProbe.ProbeId == probeId {
			return nil
		}
	}

	// License validation: Check if probe is authorized
	probeType := probeConfig.Type
	if probeType == "" {
		// Fallback to name if type is not set (for backward compatibility)
		probeType = probeConfig.Name
	}

	// If licenseValidator is nil (safe mode), only allow free tier probes
	if s.licenseValidator == nil {
		freeTierProbes := license.GetFreeTierProbes()
		isFreeTier := false
		for _, freeProbe := range freeTierProbes {
			if freeProbe == probeType {
				isFreeTier = true
				break
			}
		}
		if !isFreeTier {
			s.moduleLogger.Warn().
				Str("probe_type", probeType).
				Str("probe_name", probeConfig.Name).
				Strs("free_tier_probes", freeTierProbes).
				Msg("🚫 License validator unavailable - only free tier probes allowed")
			return fmt.Errorf("probe %q requires a valid license validator", probeType)
		}
		// Free tier probe - allow it to continue startup
	} else {
		// Normal license validation with validator
		if !s.licenseValidator.IsProbeAuthorized(s.license, probeType) {
			// Get list of free tier probes for helpful error message
			freeTierProbes := license.GetFreeTierProbes()
			s.moduleLogger.Warn().
				Str("probe_type", probeType).
				Str("probe_name", probeConfig.Name).
				Strs("free_tier_probes", freeTierProbes).
				Msg("🚫 Probe not authorized by license - skipping (upgrade license to enable)")
			return fmt.Errorf("probe %q requires a valid license", probeType)
		}
	}

	// Probe is authorized - continue with probe initialization
	probePoller, err := probes.NewProbePoller(
		probeConfig,
		s.getLoggerForProbe(probeConfig),
		s.addDataPoint,
	)
	if err != nil {
		return fmt.Errorf("Failed to create probe poller: %w", err)
	}

	s.startedProbes = append(s.startedProbes, probePoller)
	return probePoller.Start(quitChannel)
}

func (s *sensor) Shutdown(ctx context.Context) error {
	s.moduleLogger.Info().Msg("Shutting down sensor")

	for _, probePoller := range s.startedProbes {
		s.moduleLogger.Debug().
			Str("probe_name", probePoller.GetName()).
			Msg("Shutting down sensor")

		err := probePoller.Shutdown(ctx)
		if err != nil {
			s.moduleLogger.Error().
				Err(err).
				Str("probe_name", probePoller.GetName()).
				Msg("Error shutting down probe")
		}
	}
	return nil
}
